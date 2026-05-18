package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/formats"
	genericfmt "github.com/vail130/distill-ai/internal/formats/generic"
)

// ensureGenericRegistered re-registers the real generic Format,
// in case an earlier test in this package reset the global
// registry via formats.ResetForTest. Other tests in the package
// expect a clean registry; this helper restores the production
// state before exercising the run-command plumbing tests.
func ensureGenericRegistered(t *testing.T) {
	t.Helper()
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	formats.Register(genericfmt.Format{})
}

// genericTestInput is a small fixture that exercises every code path
// the M9.4 CLI plumbing touches: one warning line, one error line,
// and enough context to verify the --context flag changes the
// emitted Context slice size.
const genericTestInput = `info line 1
info line 2
info line 3
info line 4
WARN: slow query
info line 5
info line 6
info line 7
ERROR: connection refused
info line 8
info line 9
info line 10
info line 11
`

// TestRun_SeverityFiltersWarnings — without --keep-warnings the
// default --severity=error suppresses WARN events.
func TestRun_SeverityFiltersWarnings(t *testing.T) {
	ensureGenericRegistered(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"generic"}, strings.NewReader(genericTestInput), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	if strings.Contains(stdout.String(), "WARN: slow query") {
		t.Errorf("default run emitted WARN event; should be filtered to error-only.\nstdout:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "ERROR: connection refused") {
		t.Errorf("default run dropped ERROR event:\n%s", stdout.String())
	}
}

// TestRun_KeepWarningsEndToEnd — --keep-warnings emits both errors
// and warnings.
func TestRun_KeepWarningsEndToEnd(t *testing.T) {
	ensureGenericRegistered(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"--keep-warnings", "generic"}, strings.NewReader(genericTestInput), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	for _, want := range []string{"WARN: slow query", "ERROR: connection refused"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("--keep-warnings stdout missing %q:\n%s", want, stdout.String())
		}
	}
}

// TestRun_SeverityFlagAcceptsWarn — --severity=warn emits warnings
// and errors. Equivalent to --keep-warnings; documents the
// precedence rule that an explicit --severity wins over the default.
func TestRun_SeverityFlagAcceptsWarn(t *testing.T) {
	ensureGenericRegistered(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"--severity=warn", "generic"}, strings.NewReader(genericTestInput), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	for _, want := range []string{"WARN: slow query", "ERROR: connection refused"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("--severity=warn missing %q:\n%s", want, stdout.String())
		}
	}
}

// TestRun_SeverityFlagInvalidValue — a non-recognised --severity
// value is a build-time error. The wording is checked because
// users see it in CI.
func TestRun_SeverityFlagInvalidValue(t *testing.T) {
	ensureGenericRegistered(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"--severity=bogus", "generic"}, strings.NewReader(genericTestInput), &stdout, &stderr)
	if code != ExitError {
		t.Fatalf("exit code = %d, want %d; stderr=%q", code, ExitError, stderr.String())
	}
	if !strings.Contains(stderr.String(), "invalid --severity") {
		t.Errorf("stderr missing diagnostic for bad --severity; got %q", stderr.String())
	}
}

// TestRun_ContextLinesHonoured — --context=1 narrows the pre/post
// context window to one line each. The default (3) emits a wider
// window; the assertion is that --context=1 explicitly produces
// fewer context lines per Event than the default.
func TestRun_ContextLinesHonoured(t *testing.T) {
	ensureGenericRegistered(t)
	// JSON output makes the context easy to count.
	var withDefault, withOne bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"--output=json", "generic"}, strings.NewReader(genericTestInput), &withDefault, &stderr)
	if code != 0 {
		t.Fatalf("default --context: exit %d; stderr=%q", code, stderr.String())
	}
	stderr.Reset()
	code = run([]string{"--context=1", "--output=json", "generic"}, strings.NewReader(genericTestInput), &withOne, &stderr)
	if code != 0 {
		t.Fatalf("--context=1: exit %d; stderr=%q", code, stderr.String())
	}
	// The exact JSON shape isn't pinned here (covered by the
	// JSON encoder tests). We check the relative length: with
	// --context=1 the JSON output should be shorter than the
	// default (--context=3) because each Event carries fewer
	// context lines.
	if len(withOne.Bytes()) >= len(withDefault.Bytes()) {
		t.Errorf("--context=1 output (%d bytes) should be smaller than default (%d bytes); --context wiring may be broken",
			len(withOne.Bytes()), len(withDefault.Bytes()))
	}
}

// TestRun_ExplainHonoursSeverityFilter — the explain subcommand
// shares the buildParseOpts path; it must therefore also honour
// --severity / --keep-warnings.
func TestRun_ExplainHonoursSeverityFilter(t *testing.T) {
	ensureGenericRegistered(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"explain", "generic"}, strings.NewReader(genericTestInput), &stdout, &stderr)
	if code == ExitError {
		t.Fatalf("unexpected error exit; stderr=%q", stderr.String())
	}
	// Explain output annotates every Event with kept/dropped;
	// only the error Event should appear because warnings are
	// filtered at parse time before reaching the explain log.
	if strings.Contains(stdout.String(), "WARN: slow query") {
		t.Errorf("explain emitted WARN event line under default severity; should be filtered:\n%s", stdout.String())
	}
}

// TestParseOpts_FromFlags — a unit test for buildParseOpts that
// exercises the corner cases of the flag → ParseOpts mapping
// without spinning up the full CLI.
func TestParseOpts_FromFlags(t *testing.T) {
	cases := []struct {
		name     string
		fl       runFlags
		wantMin  string // event.Severity as string, "" for unset
		wantKeep bool
		wantCtx  int
	}{
		{"defaults", runFlags{}, "", false, 0},
		{"keep-warnings", runFlags{keepWarnings: true}, "", true, 0},
		{"severity=warn", runFlags{severity: "warn"}, "warn", false, 0},
		{"severity=error+keep", runFlags{severity: "error", keepWarnings: true}, "error", true, 0},
		{"context=5", runFlags{context: 5}, "", false, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts, err := buildParseOpts(&c.fl)
			if err != nil {
				t.Fatalf("buildParseOpts: %v", err)
			}
			if string(opts.MinSeverity) != c.wantMin {
				t.Errorf("MinSeverity = %q, want %q", opts.MinSeverity, c.wantMin)
			}
			if opts.KeepWarnings != c.wantKeep {
				t.Errorf("KeepWarnings = %v, want %v", opts.KeepWarnings, c.wantKeep)
			}
			if opts.ContextLines != c.wantCtx {
				t.Errorf("ContextLines = %d, want %d", opts.ContextLines, c.wantCtx)
			}
		})
	}
}
