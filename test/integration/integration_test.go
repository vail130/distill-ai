// Package integration runs end-to-end tests against the compiled
// distill-ai binary. Unlike the in-process tests under cmd/distill-ai/,
// these tests fork the binary as a subprocess so they exercise the
// real CLI surface a user (or an AI agent) sees: argv parsing, exit
// codes, stdout / stderr separation, signal handling.
//
// The harness builds the binary once per `go test` invocation
// (TestMain) and reuses it across every test, so the per-test cost
// is one fork+exec rather than a build.
//
// Fixtures live under testdata/fixtures/ as raw .input files (the
// kind of bytes a user would pipe into the binary). Expected output
// lives under testdata/golden/ as *.contains.txt files: one
// substring per non-empty line, all of which must appear in the
// captured stream. This shape tolerates volatile bits (commit SHAs,
// build dates, absolute paths) while still pinning the meaningful
// content.
//
// Byte-exact golden matching will be added when M8+ ships the
// distilled output encoders, whose output is stable across machines.
//
// Update goldens with `go test ./test/integration/ -update` (a
// contains-list still has to be edited by hand; the flag exists so
// future exact-match goldens can be regenerated mechanically).
package integration_test

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// updateGolden, when true, rewrites every golden file from the test
// run rather than asserting against it. Set via `go test -update`.
var updateGolden = flag.Bool("update", false, "rewrite golden files from current output")

// binPath is the absolute path to the compiled distill-ai binary,
// produced once by TestMain and shared by every test.
var binPath string

// TestMain compiles the binary into a temp directory and stashes the
// path in binPath. Returning a non-zero exit code from TestMain
// surfaces a build failure before any individual test runs.
func TestMain(m *testing.M) {
	flag.Parse()
	path, cleanup, err := buildBinary()
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration: build failed: %v\n", err)
		os.Exit(2)
	}
	binPath = path
	code := m.Run()
	cleanup()
	os.Exit(code)
}

// buildBinary compiles ./cmd/distill-ai with -race so the integration
// suite catches concurrency bugs the unit tests' in-process harness
// would miss. The binary lands in a temp dir; the returned cleanup
// removes it.
func buildBinary() (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "distill-ai-integration-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("mktemp: %w", err)
	}
	// Windows: `go build -o foo` produces `foo` without an .exe
	// suffix only on non-Windows. On Windows the produced file is
	// `foo.exe` and exec'ing a path missing the suffix fails. Spell
	// the suffix explicitly so the path we keep matches what go
	// build wrote.
	bin := "distill-ai"
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	out := filepath.Join(dir, bin)
	repoRoot, err := findRepoRoot()
	if err != nil {
		os.RemoveAll(dir)
		return "", func() {}, fmt.Errorf("find repo root: %w", err)
	}
	cmd := exec.Command("go", "build", "-race", "-o", out, "./cmd/distill-ai")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1") // -race requires cgo
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		os.RemoveAll(dir)
		return "", func() {}, fmt.Errorf("go build: %w\n%s", err, stderr.String())
	}
	return out, func() { os.RemoveAll(dir) }, nil
}

// findRepoRoot walks up from this test file's directory until it
// finds a go.mod. Tests should not assume the CWD at exec time.
var (
	repoRootOnce sync.Once
	repoRootVal  string
	repoRootErr  error
)

func findRepoRoot() (string, error) {
	repoRootOnce.Do(func() {
		// Walk upward from the test package directory until go.mod
		// appears. The integration package itself lives at
		// test/integration/ so two parents up is normal, but we
		// don't hard-code that — symlinks and shadow workspaces
		// might shift the path.
		dir, err := os.Getwd()
		if err != nil {
			repoRootErr = fmt.Errorf("getwd: %w", err)
			return
		}
		for {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				repoRootVal = dir
				return
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				repoRootErr = fmt.Errorf("no go.mod found above %q", dir)
				return
			}
			dir = parent
		}
	})
	return repoRootVal, repoRootErr
}

// runResult captures everything the harness asserts on.
type runResult struct {
	stdout   string
	stderr   string
	exitCode int
}

// runBinary forks the binary with the given argv and optional stdin
// content. It enforces a hard timeout so a misbehaving binary
// (deadlock, infinite loop) doesn't hang the test suite.
func runBinary(t *testing.T, stdin string, argv ...string) runResult {
	t.Helper()
	cmd := exec.Command(binPath, argv...) //nolint:gosec // G204 path is from our own build step
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start binary: %v", err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		code := 0
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				code = exitErr.ExitCode()
			} else {
				t.Fatalf("wait: %v", err)
			}
		}
		return runResult{
			stdout:   stdout.String(),
			stderr:   stderr.String(),
			exitCode: code,
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("binary timed out after 30s; argv=%v stdin=%q\nstdout so far:\n%s\nstderr so far:\n%s",
			argv, stdin, stdout.String(), stderr.String())
		return runResult{} // unreachable
	}
}

// readFixture loads a raw input file from testdata/fixtures/.
func readFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", "fixtures", name)
	b, err := os.ReadFile(path) //nolint:gosec // G304 path is test-local
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

// assertContainsGolden checks that every line in
// testdata/golden/<name>.contains.txt appears as a substring of got.
// Empty lines in the golden are skipped so contributors can group
// expectations visually.
func assertContainsGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".contains.txt")
	if *updateGolden {
		// We can't auto-write a "contains" golden from a single
		// run because we don't know which substrings the contributor
		// wants to anchor on. Instead, emit a hint to stderr.
		t.Logf("update: golden %s is a contains-list; edit it by hand. Current output:\n%s", path, got)
		return
	}
	b, err := os.ReadFile(path) //nolint:gosec // G304 path is test-local
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	for i, want := range strings.Split(string(b), "\n") {
		want = strings.TrimRight(want, "\r")
		if want == "" {
			continue
		}
		if !strings.Contains(got, want) {
			t.Errorf("output missing substring (line %d of %s):\n  want: %q\n  got:\n%s", i+1, path, want, got)
		}
	}
}

// writeTempFixture writes content to a temp file inside t.TempDir()
// and returns the path. Used when a test needs an input file (the
// detect subcommand accepts a path argument).
func writeTempFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.input")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// -----------------------------------------------------------------
// Tests
// -----------------------------------------------------------------

func TestBinary_VersionPrintsBuildInfo(t *testing.T) {
	got := runBinary(t, "", "--version")
	if got.exitCode != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", got.exitCode, got.stderr)
	}
	assertContainsGolden(t, "version", got.stdout)
	if got.stderr != "" {
		t.Errorf("stderr should be empty on --version; got %q", got.stderr)
	}
}

func TestBinary_HelpPrintsUsage(t *testing.T) {
	got := runBinary(t, "", "--help")
	if got.exitCode != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", got.exitCode, got.stderr)
	}
	assertContainsGolden(t, "help", got.stdout)
}

// TestBinary_ShortFlagsAccepted pins the post-M8.2 short-flag set.
// `-h` is cobra-managed help. `-v` shifted from --version to --verbose
// in M8.2; with empty stdin the run path falls back to the generic
// format (M9.1) and emits the "no events found" tail with exit code
// 1, which proves the flag is wired to the verbose path of the run
// command rather than to --version.
func TestBinary_ShortFlagsAccepted(t *testing.T) {
	t.Run("-h", func(t *testing.T) {
		got := runBinary(t, "", "-h")
		if got.exitCode != 0 {
			t.Fatalf("exit = %d for -h, want 0; stderr=%q", got.exitCode, got.stderr)
		}
	})
	t.Run("-v", func(t *testing.T) {
		got := runBinary(t, "", "-v")
		// Empty stdin + generic fallback → no events emitted.
		// run() maps the post-pipeline summary to ExitNoEvents (1).
		if got.exitCode != 1 {
			t.Fatalf("exit = %d for -v, want 1 (empty stdin → fallback → no events); stderr=%q stdout=%q",
				got.exitCode, got.stderr, got.stdout)
		}
		// -v wires verbose; the verbose path prints format/source
		// to stderr so the user can see which format was picked.
		if !strings.Contains(got.stderr, "format=generic") {
			t.Errorf("-v stderr did not mention format=generic; got %q", got.stderr)
		}
	})
}

// TestBinary_NoArgsRunsPipeline pins the M8.2 behaviour: invoking
// the binary with no arguments runs the pipeline against stdin. With
// empty stdin the pipeline falls back to the generic format (M9.1)
// and exits 1 (no events found). Once specific formats land (M10
// pytest, M11 jest, M12 gotest) and stdin carries real input the
// exit will become 0; the empty-stdin path stays at 1. The pre-M8.2
// "no args → print help" path lives on as `distill-ai --help`.
func TestBinary_NoArgsRunsPipeline(t *testing.T) {
	got := runBinary(t, "")
	if got.exitCode != 1 {
		t.Fatalf("exit = %d, want 1 (empty stdin → generic fallback → no events); stderr=%q stdout=%q",
			got.exitCode, got.stderr, got.stdout)
	}
	// The run command prints the pipeline summary to stdout when
	// no events emerge; "no events found" is the documented
	// human-readable tail.
	if !strings.Contains(got.stdout, "no events found") {
		t.Errorf("no-args stdout did not mention 'no events found'; got %q", got.stdout)
	}
}

// TestBinary_UnknownPositionalTreatedAsFile pins the M8.2 routing
// rule at the integration boundary. An unknown positional flows to
// the run command's input resolver, which tries to open it as a
// file and produces an OS-level file-not-found diagnostic on
// stderr. Cobra still reports `--unknown-flag` as an "unknown
// flag"; that's covered by the in-process TestRoot_UnknownFlagExitsTwo.
//
// The exact wording of the not-found error is OS-specific (Unix
// says "no such file or directory"; Windows says "The system
// cannot find the file specified."), so we anchor on the portable
// "open <name>" prefix from os.Open's wrapped error.
func TestBinary_UnknownPositionalTreatedAsFile(t *testing.T) {
	got := runBinary(t, "", "definitely-not-a-real-file")
	if got.exitCode != 2 {
		t.Errorf("exit = %d, want 2; stdout=%q stderr=%q", got.exitCode, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stderr, "open ") || !strings.Contains(got.stderr, "definitely-not-a-real-file") {
		t.Errorf("stderr should mention 'open <name>'; got %q", got.stderr)
	}
}

// TestBinary_UnknownFlagExitsTwo pins the cobra unknown-flag path.
func TestBinary_UnknownFlagExitsTwo(t *testing.T) {
	got := runBinary(t, "", "--definitely-not-a-real-flag")
	if got.exitCode != 2 {
		t.Errorf("exit = %d, want 2; stderr=%q", got.exitCode, got.stderr)
	}
	if !strings.Contains(got.stderr, "unknown flag") {
		t.Errorf("stderr should mention 'unknown flag'; got %q", got.stderr)
	}
}

func TestBinary_DetectMissingFileExitsTwo(t *testing.T) {
	got := runBinary(t, "", "detect")
	if got.exitCode != 2 {
		t.Errorf("exit = %d, want 2; stdout=%q stderr=%q", got.exitCode, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stderr, "missing FILE") {
		t.Errorf("stderr missing diagnostic; got %q", got.stderr)
	}
}

func TestBinary_DetectNonexistentFileExitsTwo(t *testing.T) {
	got := runBinary(t, "", "detect", "/nonexistent/path/should/not/exist")
	if got.exitCode != 2 {
		t.Errorf("exit = %d, want 2; stdout=%q stderr=%q", got.exitCode, got.stdout, got.stderr)
	}
}

func TestBinary_DetectTooManyArgsExitsTwo(t *testing.T) {
	got := runBinary(t, "", "detect", "a", "b")
	if got.exitCode != 2 {
		t.Errorf("exit = %d, want 2; stderr=%q", got.exitCode, got.stderr)
	}
}

// Detection against real fixtures. After M9.1, every detection
// either matches a specific format or falls back to the generic
// format (exit 1, fellback_to_generic=true). The pre-M9 "no format
// matched" stderr diagnostic only appears under --strict.
//
// When specific formats land (M10 pytest, M11 jest, M12 gotest),
// replace these tests with positive-detection assertions.
func TestBinary_DetectEmptyInput(t *testing.T) {
	path := writeTempFixture(t, readFixture(t, "empty.input"))
	got := runBinary(t, "", "detect", path)
	if got.exitCode != 1 {
		t.Errorf("exit = %d, want 1 (fellback to generic); stdout=%q stderr=%q", got.exitCode, got.stdout, got.stderr)
	}
	assertContainsGolden(t, "detect-fallback-generic", got.stdout)
}

func TestBinary_DetectPlaintextInput(t *testing.T) {
	path := writeTempFixture(t, readFixture(t, "plaintext.input"))
	got := runBinary(t, "", "detect", path)
	if got.exitCode != 1 {
		t.Errorf("exit = %d, want 1 (fellback to generic); stdout=%q stderr=%q", got.exitCode, got.stdout, got.stderr)
	}
	assertContainsGolden(t, "detect-fallback-generic", got.stdout)
}

// pytest and gotest fixtures exist today but no specific format is
// registered, so they also fall back to the generic format. The
// fixtures contain ERROR / panic markers, so generic.Detect returns
// confidenceFloor — but the detector still treats that as below
// threshold and routes the input through the fallback path. M10 /
// M12 will replace this with a positive-match assertion against
// pytest/gotest.
func TestBinary_DetectPytestFixtureFallsThrough(t *testing.T) {
	path := writeTempFixture(t, readFixture(t, "pytest-fail.input"))
	got := runBinary(t, "", "detect", path)
	if got.exitCode != 1 {
		t.Errorf("exit = %d, want 1 (pre-M10 falls back to generic); stdout=%q stderr=%q", got.exitCode, got.stdout, got.stderr)
	}
}

func TestBinary_DetectStdinDash(t *testing.T) {
	got := runBinary(t, readFixture(t, "plaintext.input"), "detect", "-")
	if got.exitCode != 1 {
		t.Errorf("exit = %d, want 1; stdout=%q stderr=%q", got.exitCode, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stdout, "stdin") {
		t.Errorf("expected stdin to be named on stdout source line; got %q", got.stdout)
	}
}

// TestBinary_DetectStdoutAndStderrSeparated checks the Unix-filter
// invariant: parseable detection output goes to stdout. After M9.1
// the generic fallback always produces a parseable result, so
// stdout carries the format/confidence/source key:value lines and
// stderr stays empty.
func TestBinary_DetectStdoutAndStderrSeparated(t *testing.T) {
	path := writeTempFixture(t, readFixture(t, "plaintext.input"))
	got := runBinary(t, "", "detect", path)
	// After M9.1: fallback path emits key:value lines on stdout.
	if got.stdout == "" {
		t.Errorf("detect-fallback should write key:value lines to stdout; stdout empty (stderr=%q)", got.stderr)
	}
	if got.stderr != "" {
		t.Errorf("detect-fallback should leave stderr empty; got %q", got.stderr)
	}
}

// TestBinary_DetectStrictRejectsFallback pins the --strict semantics
// against the generic fallback. With --strict, even though the
// generic format is registered, a sample whose specific formats all
// score below threshold returns exit 2 with the "no format matched"
// diagnostic on stderr. This is what CI users invoke to make
// ambiguous input a build break.
func TestBinary_DetectStrictRejectsFallback(t *testing.T) {
	path := writeTempFixture(t, readFixture(t, "plaintext.input"))
	got := runBinary(t, "", "detect", "--strict", path)
	if got.exitCode != 2 {
		t.Fatalf("exit = %d, want 2 (--strict suppresses fallback); stdout=%q stderr=%q",
			got.exitCode, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stderr, "no format matched") {
		t.Errorf("strict-mode stderr did not mention 'no format matched'; got %q", got.stderr)
	}
}

// TestBinary_LargeStdinDoesNotHang feeds 1 MiB of pseudo-random
// bytes via stdin and asserts the binary terminates within the
// harness's 30s timeout. Today the detect subcommand reads a
// bounded sample so it should be near-instant; the test exists so
// a future regression that introduces unbounded reading is caught
// by the existing timeout, not by an unhappy CI sitting at 60
// minutes.
func TestBinary_LargeStdinDoesNotHang(t *testing.T) {
	const size = 1 << 20 // 1 MiB
	// A reproducible-ish payload — content doesn't matter, only size.
	var sb strings.Builder
	sb.Grow(size)
	for sb.Len() < size {
		sb.WriteString("plain text without any severity markers whatsoever.\n")
	}
	got := runBinary(t, sb.String(), "detect", "-")
	// Exit code is whatever — we only care that it terminated.
	_ = got
}

// -----------------------------------------------------------------
// Skill drift guard
// -----------------------------------------------------------------

// TestSkill_DocumentsCurrentCLISurface enforces the rule that the
// .opencode/skills/distill-output/SKILL.md skill always names every
// subcommand and top-level flag the compiled binary actually
// supports.
//
// The skill carries a `cli-surface` fenced block that this test
// parses, plus an optional `cli-surface-future` block for verbs
// announced but not yet wired (M8's `run`, `list-formats`,
// `explain`, `completions`, `version` are the canonical example).
//
// The test asserts three invariants:
//
//  1. Every subcommand in the manifest is recognised by the binary
//     (stderr does NOT contain "unknown subcommand"). The exit code
//     may be anything — `detect` with no args exits 2 with
//     "missing FILE", which still proves the verb is wired.
//  2. Every top-level flag in the manifest is accepted by the
//     binary with exit code 0. Today this is `--help`, `--version`,
//     `-h`, `-v`; that set grows in M8.
//  3. Every subcommand mentioned in `--help` output appears in
//     either the manifest's `subcommands:` line or its
//     `cli-surface-future` block.
//
// When the binary grows a new subcommand or flag, update the
// manifest in the same commit. The test fails loudly otherwise.
func TestSkill_DocumentsCurrentCLISurface(t *testing.T) {
	manifest := readSkillManifest(t)
	helpOutput := runBinary(t, "", "--help")
	if helpOutput.exitCode != 0 {
		t.Fatalf("--help exit = %d; cannot verify drift", helpOutput.exitCode)
	}
	t.Run("manifest_subcommands_are_wired", func(t *testing.T) {
		for _, sub := range manifest.subcommands {
			sub := sub
			t.Run(sub, func(t *testing.T) {
				got := runBinary(t, "", sub)
				if strings.Contains(got.stderr, "unknown subcommand") {
					t.Errorf("manifest names subcommand %q but binary reports it as unknown; stderr=%q",
						sub, got.stderr)
				}
			})
		}
	})
	t.Run("manifest_flags_are_wired", func(t *testing.T) {
		// The test asserts the flag is *recognised* by cobra
		// (stderr does not contain "unknown flag"), not that the
		// flag succeeds end-to-end — most run flags require valid
		// input plus a registered format to reach exit 0, neither
		// of which is true in the bare-binary test environment.
		// Special-case --help and --version, which are documented
		// to exit 0 in isolation.
		isInfoFlag := map[string]bool{
			"--help":    true,
			"-h":        true,
			"--version": true,
		}
		for _, fl := range manifest.flags {
			fl := fl
			t.Run(fl, func(t *testing.T) {
				got := runBinary(t, "", fl)
				if strings.Contains(got.stderr, "unknown flag") {
					t.Errorf("manifest names flag %q but cobra reports it as unknown; stderr=%q",
						fl, got.stderr)
				}
				if isInfoFlag[fl] && got.exitCode != 0 {
					t.Errorf("manifest names info flag %q but binary exited %d; stderr=%q",
						fl, got.exitCode, got.stderr)
				}
			})
		}
	})
	t.Run("help_does_not_introduce_undocumented_subcommands", func(t *testing.T) {
		// Parse subcommand mentions out of the help text. Today's
		// help format has lines like "  distill-ai detect FILE  ...";
		// extract every distinct word that follows "distill-ai" in
		// such a line. Skip words that are themselves flags or
		// known argv placeholders (FILE, ARG, etc.).
		mentioned := extractSubcommandsFromHelp(helpOutput.stdout)
		known := stringSet(manifest.subcommands)
		future := stringSet(manifest.future)
		for word := range mentioned {
			if known[word] || future[word] {
				continue
			}
			t.Errorf("--help mentions %q as a subcommand but the skill manifest does not. "+
				"Add it to the cli-surface or cli-surface-future block in "+
				".opencode/skills/distill-output/SKILL.md.", word)
		}
	})
}

// skillManifest is the parsed form of the cli-surface blocks in
// .opencode/skills/distill-output/SKILL.md.
type skillManifest struct {
	subcommands []string
	flags       []string
	future      []string
}

// readSkillManifest locates SKILL.md via the repo root, parses the
// fenced surface block(s), and returns the manifest. Fatals if the
// file or expected blocks are missing — both are conditions the
// alignment rule has already been violated by.
func readSkillManifest(t *testing.T) skillManifest {
	t.Helper()
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	path := filepath.Join(root, ".opencode", "skills", "distill-output", "SKILL.md")
	raw, err := os.ReadFile(path) //nolint:gosec // path is repo-local
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}
	body := string(raw)
	surface := extractSurfaceBlock(body, "cli-surface", true /* required */, t)
	future := extractSurfaceBlock(body, "cli-surface-future", false, t)
	m := skillManifest{
		subcommands: parseManifestLine(surface, "subcommands"),
		flags:       parseManifestLine(surface, "flags"),
		future:      parseManifestLine(future, "subcommands"),
	}
	if len(m.subcommands) == 0 {
		t.Fatalf("manifest's cli-surface block has no subcommands; raw block:\n%s", surface)
	}
	if len(m.flags) == 0 {
		t.Fatalf("manifest's cli-surface block has no flags; raw block:\n%s", surface)
	}
	return m
}

// extractSurfaceBlock pulls the body of a fenced ```surface``` block
// nested between BEGIN/END HTML-comment markers. The marker pattern
// is `<!-- BEGIN name -->` ... `<!-- END name -->` so future skills
// can carry multiple manifest blocks without ambiguity.
//
// Returns "" when the block is absent. With required=true, missing
// fails the test.
func extractSurfaceBlock(body, name string, required bool, t *testing.T) string {
	t.Helper()
	begin := "<!-- BEGIN " + name + " -->"
	end := "<!-- END " + name + " -->"
	bi := strings.Index(body, begin)
	if bi < 0 {
		if required {
			t.Fatalf("skill missing required block %q", name)
		}
		return ""
	}
	ei := strings.Index(body[bi:], end)
	if ei < 0 {
		t.Fatalf("skill block %q has BEGIN but no END marker", name)
	}
	region := body[bi+len(begin) : bi+ei]
	// Inside region, find the ```surface fenced block. We accept
	// only the canonical fence tag so contributors can't paste
	// `surface` directives into prose by accident.
	fenceOpen := "```surface"
	fenceClose := "```"
	fi := strings.Index(region, fenceOpen)
	if fi < 0 {
		t.Fatalf("skill block %q has BEGIN/END markers but no ```surface fenced body", name)
	}
	rest := region[fi+len(fenceOpen):]
	ci := strings.Index(rest, fenceClose)
	if ci < 0 {
		t.Fatalf("skill block %q has unterminated ```surface fence", name)
	}
	return strings.TrimSpace(rest[:ci])
}

// parseManifestLine reads `key: a, b, c` from the surface block and
// returns the comma-separated items, trimmed. Empty key → empty
// slice.
func parseManifestLine(block, key string) []string {
	prefix := key + ":"
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		rest := strings.TrimSpace(line[len(prefix):])
		if rest == "" {
			return nil
		}
		raw := strings.Split(rest, ",")
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	}
	return nil
}

// extractSubcommandsFromHelp scans the Usage section of help output
// for subcommand invocation lines and returns the set of subcommand
// names found.
//
// The parser is deliberately strict: it only inspects lines between
// a "Usage:" header and the next blank-line-followed-by-non-indented
// line (the next top-level section, e.g., "Flags:"). Within that
// region it extracts lowercase-ASCII words appearing immediately
// after `distill-ai ` on indented lines. This excludes:
//
//   - URLs and prose elsewhere in the help text ("See https://.../distill-ai for ...").
//   - The first-line description ("distill-ai — compress logs...").
//   - The pipeline form ("cmd | distill-ai") which has no following
//     subcommand.
//   - Argv placeholders (FILE, ARG) which are uppercase.
//   - Flags (start with `-`).
//
// When the help format changes substantially, update both this
// parser and the manifest in the same commit.
func extractSubcommandsFromHelp(help string) map[string]struct{} {
	out := map[string]struct{}{}
	inUsage := false
	for _, line := range strings.Split(help, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "Usage:"):
			inUsage = true
			continue
		case inUsage && trimmed != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t"):
			// New top-level section (e.g., "Flags:") ends Usage.
			inUsage = false
		}
		if !inUsage {
			continue
		}
		// Require the canonical "distill-ai <subcmd>" shape: the
		// substring `distill-ai ` must be preceded by start-of-line
		// or whitespace (rejecting matches inside URLs).
		const sentinel = "distill-ai "
		idx := strings.Index(line, sentinel)
		if idx < 0 {
			continue
		}
		if idx > 0 {
			prev := line[idx-1]
			if prev != ' ' && prev != '\t' {
				continue
			}
		}
		rest := line[idx+len(sentinel):]
		word := rest
		if cut := strings.IndexFunc(rest, isHelpDelimiter); cut >= 0 {
			word = rest[:cut]
		}
		word = strings.TrimSpace(word)
		if word == "" || strings.HasPrefix(word, "-") {
			continue
		}
		if !isLowerASCII(word) {
			continue
		}
		out[word] = struct{}{}
	}
	return out
}

func isHelpDelimiter(r rune) bool {
	return r == ' ' || r == '\t' || r == '|' || r == '<' || r == '>'
}

func isLowerASCII(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && r != '-' && r != '_' {
			return false
		}
	}
	return s != ""
}

func stringSet(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[s] = true
	}
	return m
}
