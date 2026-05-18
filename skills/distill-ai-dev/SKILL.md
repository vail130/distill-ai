---
name: distill-ai-dev
description: Dogfood the local `distill-ai` binary while developing it. Use whenever a test run, build, lint, or other command in this repo produces enough output that the interesting failures are buried — pipe it through `./bin/distill-ai` (or the integration-test binary) instead of reading thousands of lines. Forces the maintainer (you) to feel every gap in the tool before customers do. For *using* distill-ai as a downstream tool, see the sibling `distill-ai` skill instead.
---

# distill-ai-dev

This is `distill-ai`'s repo. We use the tool we are building on our own
command output, so every rough edge surfaces during normal development
instead of in a downstream user's terminal. Dogfooding is the cheapest
honest test we have.

For **using** `distill-ai` as a downstream tool, see the sibling
[`distill-ai`](../distill-ai/SKILL.md) skill — it covers the general
invocation patterns without the repo-internal context. This skill
focuses on the development loop: building the binary, debugging
parsers, and keeping the CLI-surface manifest in sync.

## When to use this skill

- Any time `make test` / `go test ./...` produces more than ~30 lines.
- Any time `make lint` reports more than a couple of findings.
- Any time you change a format parser and want to see the resulting
  Event stream on a real fixture.
- Any time the CLI surface or a recipe-affecting behaviour changes —
  the manifest below has to move with the code (see the drift-guard
  section at the bottom).

Do **not** use this skill when:

- You need the raw stream (e.g., debugging the distiller itself with
  `-v` / `--verbose`). Run the command bare.
- The command runs under 1 second and emits under 30 lines. The
  overhead of the extra pipeline is not worth the saving.
- You're not working on `distill-ai` itself. Load the
  [`distill-ai`](../distill-ai/SKILL.md) skill instead.

## The binary

Build it once per session, or after any change under
`cmd/distill-ai/` or `internal/`:

```sh
make build           # produces ./bin/distill-ai
```

The integration test suite (see `test/integration/`) re-builds on
each run via `go test`; outside that path, `make build` is the single
source of truth.

## State of play

The full CLI surface — flags, subcommands, exit codes — landed in
**M8**. The **generic fallback** is complete as of **M9**, and
**gotest** is the first specific format to ship: **M10** lands
the parser end-to-end with `test_failure`, `panic`, `build_failure`,
and `race_condition` Event kinds plus structured stack frames and
`-json` reporter support. `make test 2>&1 | ./bin/distill-ai` is
now the canonical dogfooding loop for this project.

**M11** adds the **pytest** format: `=== FAILURES ===` and
`=== ERRORS ===` blocks, parametrised test IDs, four `--tb`
reporter shapes, the `=== warnings summary ===` section, and
structured stack frames. Emits `test_failure`, `test_error`,
`collection_error`, and `warning` Events. The filter rules from
`--keep-warnings` / `--severity` apply.

**M12** lands the **jest** format: the `●` failure-block bullet,
per-file `FAIL`/`PASS` headers, snapshot mismatches (file-backed
and `toMatchInlineSnapshot` forms), suite-level failures
(`● Test suite failed to run`), `--verbose` reporter ✓/✗
indicator lines, and the no-ANSI CI reporter mode. Emits
`test_failure`, `snapshot_mismatch`, and `suite_error` Events.
The Unicode chevron (`›`, U+203A) between suite and test name
is normalised to ASCII `>` in `metadata.test_id` so the value is
grep-able from any terminal.

With M12 shipped, every v1 specific format is live; only
`generic` remains as the safety net. `--strict` still works as
the CI switch to forbid generic fallback (exit 2 instead of
falling back).

Looking past v1.0: the post-v1.0 roadmap is recorded in
[ADR-0002](../../docs/decisions/0002-v1.0-scope-and-post-v1.0-roadmap.md).
v1.1 will add `golangci-lint` + `go vet` (M23) and `cargo`
(M24) Formats — both consume upstream-emitted JSON envelopes,
so dogfooding them once they ship will look like
`golangci-lint run --out-format=json | ./bin/distill-ai` and
`cargo build --message-format=json | ./bin/distill-ai`.
Update this section in the same commit that wires either Format.

The full surface today is enumerated in the manifest below.

<!-- BEGIN cli-surface -->
```surface
subcommands: completions, detect, explain, list-formats, run, version
flags: --help, --version, -h, --auto, --keep-vendor, --dedupe, --no-dedupe, --output, --output-streaming, --budget, --no-footer, --strict, --strip-envelope, --tokenizer, --list-formats
```
<!-- END cli-surface -->
<!-- BEGIN cli-surface-future -->
```surface
subcommands:
```
<!-- END cli-surface-future -->

That manifest is **machine-parsed by the integration test suite**
(see `TestSkill_DocumentsCurrentCLISurface` in
`test/integration/integration_test.go`). When the CLI grows — for
example when M9's generic fallback flips the default detection path
from "no format matched" to "fell back to generic", or when a new
flag is added in support of M9.4's filtering work — update the
manifest in the same commit that wires the surface. The test fails
loudly otherwise.

Invocation forms today:

- `cmd | ./bin/distill-ai` — read stdin, autodetect, distil to
  stdout. After M9.2 this returns distilled Events for any input
  containing `ERROR` / `panic` / `Exception` / `WARN` / etc.;
  inputs with no severity markers exit 1 with "no events found".
  Use `--strict` to reject low-confidence input.
- `./bin/distill-ai run [FORMAT] [FILE...]` — explicit form. Useful
  when you want to pass multiple files, force a specific format, or
  bypass autodetection with `--auto=false`.
- `./bin/distill-ai detect FILE` — print which format wins
  detection, with confidence, sample size, and runner-up. Accepts
  `-` for stdin. After M9.1 plaintext input falls back to `generic`
  on stdout (`fellback_to_generic: true`) rather than erroring on
  stderr.
- `./bin/distill-ai explain [FORMAT] [FILE...]` — dry-run mode:
  emit one `kept` / `dropped:<reason>` line per event without
  writing distilled output. Useful when `--budget` aggressively
  prunes events you expected to see.
- `./bin/distill-ai list-formats` — list every registered format
  with version and source. After M9.1 prints `generic` as the
  only registered builtin until M10/M11/M12 ship.
- `./bin/distill-ai completions [bash|zsh|fish|powershell]` —
  generate a shell completion script.
- `./bin/distill-ai version` — print version, commit, build date.
- `./bin/distill-ai --version` / `--help` — the standard
  affordances; `--version` is equivalent to the subcommand.

The dogfooding loop today:

1. Run the noisy command and pipe directly: `noisy-cmd 2>&1 |
   ./bin/distill-ai`.
2. Detection always resolves: either to a specific format (once
   M10/M11/M12 ship) or to the `generic` fallback.
3. The generic scanner extracts ERROR / WARN / panic / Exception /
   Traceback / FATAL lines with surrounding context. Multi-line
   blocks (Python tracebacks, Go panics, JVM stack dumps) capture
   the full block plus parsed stack frames in `Event.Frames`.
   M9.4 will add `--severity` / `--keep-warnings` plumbing.

## Dev recipes

These recipes are scoped to working **on** the parsers and CLI, not
general usage. Every example uses `./bin/distill-ai` — the locally
built binary, not whatever happens to be on `PATH`. That distinction
matters: a `make build` away from a parser change can silently leave
you testing yesterday's code.

For general-purpose invocation patterns (autodetect, JSON output,
budget, etc.) see the sibling [`distill-ai`](../distill-ai/SKILL.md)
skill. The recipes here cover only what's specific to developing the
tool.

### Inspect the locally-built CLI surface

```sh
./bin/distill-ai --help
./bin/distill-ai run --help
./bin/distill-ai explain --help
```

The `run` and `explain` help pages enumerate the full flag set with
behaviour notes, including flags whose plumbing lands in scoped
follow-up commits. If the help output doesn't match the manifest
below, one of them is wrong — see the drift-guard section.

### Check what the detector picks for a fixture

```sh
./bin/distill-ai detect test/integration/testdata/fixtures/gotest-fail.input
echo "exit: $?"
```

After M10 this prints `format: gotest` with `confidence: 1.00` and
exits 0. For input that no specific format claims, the detector
falls back to `generic` (`fellback_to_generic: true`, exit 1). Use
`--strict` to turn that fallback into a hard error (exit 2).
Useful when adding a new format: confirm it claims its own
fixtures and **doesn't** claim a sibling format's.

### Dogfood this project's own test output

```sh
make test 2>&1 | ./bin/distill-ai
```

The canonical loop. Every test run becomes a real-world
distill-ai input. Gaps in the gotest parser surface immediately
because they manifest in *your own terminal* before they manifest
in a customer's. If the distilled output omits a failure you can
see in the raw stream, that's a parser bug — fix it before moving
on.

### Iterate on a parser against a specific fixture

```sh
pytest -v 2>&1 | ./bin/distill-ai pytest --output=markdown
./bin/distill-ai run pytest test/integration/testdata/fixtures/pytest-fail.input
```

Useful when working on `internal/formats/pytest/` — feed your own
fixture, see what comes out, iterate. The explicit format argument
(`pytest`) skips autodetect, which is what you want when you're
testing the parser, not the detector. Drop it to exercise the
detector path.

`--keep-warnings` captures entries from the `=== warnings summary
===` section; warnings are dropped by default so the distilled
output stays focused on failures.

### Distil a jest run

```sh
npx jest 2>&1 | ./bin/distill-ai
npx jest --ci 2>&1 | ./bin/distill-ai jest --output=markdown
./bin/distill-ai run jest test/integration/testdata/fixtures/jest-fail.input
```

The CI form (`--ci`) drops ANSI escapes; the binary handles both
coloured and plain renderings identically. The explicit `jest`
positional skips autodetect. Useful when working on
`internal/formats/jest/`: feed a fixture, inspect the distilled
output, iterate.

Snapshot mismatches surface as `Kind=snapshot_mismatch` with the
diff lines preserved in the Event Body; file-backed and inline
forms are distinguished by `metadata.snapshot_kind`
(`"file"` vs `"inline"`). The Unicode chevron jest renders
between suite and test name is normalised to ASCII `>` in
`metadata.test_id` so grep / jq queries don't need to know how
to type U+203A.

### Distil a GitHub Actions log

```sh
gh run view --log              | ./bin/distill-ai
gh run view --log <run-id>     | ./bin/distill-ai --output=markdown
./bin/distill-ai run test/integration/testdata/fixtures/gha-gotest-fail.input
```

`gh run view --log` returns the raw workflow log with the
GHA wrapper still in place — per-line RFC3339-Z timestamps,
`##[group]NAME` / `##[endgroup]` markers, and the trailing
`##[error]Process completed with exit code N.` line on a
failing step. `--strip-envelope=auto` (the default) picks the
`github-actions` stripper, removes the metadata, and hands the
inner bytes to the format detector. A wrapped `go test` run
detects as `gotest` with `Confidence=1.0`, identical to a
bare-stdout run.

The step-failure marker becomes one
`Kind=envelope_step_failure` Event with `metadata.exit_code`
and (when the wrapping group is still open)
`metadata.step` set. `##[error]` / `##[warning]` / `##[notice]`
lines outside the step-failure pattern become
`envelope_error` / `envelope_warning` Events.

To opt out for an already-bare stdout: `--strip-envelope=none`.

### Distil a GitLab CI log

```sh
glab ci trace                  | ./bin/distill-ai
glab ci trace --job=test       | ./bin/distill-ai --output=json
./bin/distill-ai run test/integration/testdata/fixtures/gitlab-gotest-fail.input
```

`glab ci trace` streams the raw job log; the GitLab wrapper is
the `section_start:NS:NAME\r` / `section_end:NS:NAME\r` markers
that collapse spans in the UI, plus the runner's terminal
`ERROR: Job failed: exit code N` line on a failing job.
`--strip-envelope=auto` picks the `gitlab-ci` stripper, removes
the markers, and normalises the bare-`\r` line endings the
runner emits when it overwrites progress indicators.

The job-failure line becomes one
`Kind=envelope_step_failure` Event with `metadata.exit_code` and
(when the surrounding section is still open) `metadata.step`
set. Sections cleanly closed before the marker fires — the
common case — leave `metadata.step` empty.

### Compare distilled output against a golden file

```sh
./bin/distill-ai detect testdata/case-XX.input \
  | diff - testdata/case-XX.expected.detect
```

The integration test suite at `test/integration/` runs a stronger
form of this: the suite compiles the binary, runs it against every
real fixture, and diffs against the committed expected output. When
fixing a parser bug, add the failing input as a fixture **first**,
in the same commit as the fix (see the [alignment
rule](../../rules/alignment.md)).

## What to do when distill-ai fails

These are the failure modes you will hit while dogfooding:

- **Detection picks `generic` when a specific format exists.**
  Bug in the specific format's `Detect`. Look at
  `internal/formats/<name>/<name>.go`. Add a fixture and a regression
  test in the same commit.
- **Detection picks the wrong specific format.**
  Confidence levels are too close. Tweak the loser's `Detect` so its
  markers exclude the winner's input shape. Do not raise the winner's
  confidence above 1.0 (the constants in
  [event.go](../../internal/event/event.go) ban it).
- **The distilled output is missing events you expected.**
  Open the fixture in your editor. Walk through the parser's state
  machine by hand. Most parser bugs are state transitions that fire
  one line too early or too late. Add a per-event regression test
  before fixing.
- **The binary crashes or hangs.**
  Run it under `-race` (the integration suite does this; you can
  too via `go run ./cmd/distill-ai ...`). Goroutine leaks show up
  as the process not exiting; check the pipeline stages closed their
  channels.

## Drift guard

This skill is part of the documentation surface. The
[alignment rule](../../rules/alignment.md) names it explicitly: any
change to subcommands, top-level flags, or recipe-relevant behaviour
must land in the same commit that updates the manifest above.

Enforcement is mechanical, not honour-system:

1. **`TestSkill_DocumentsCurrentCLISurface`** in
   `test/integration/integration_test.go` parses the
   `cli-surface` block above, exec's the compiled binary, and
   asserts:
   - Every subcommand listed in the manifest is recognised by the
     binary (returns 0/1, not "unknown subcommand").
   - Every subcommand the binary reports in `--help` (other than
     entries in `cli-surface-future`, see below) appears in the
     manifest.
   - Every flag listed is accepted with exit code 0.
2. **Future surface.** When a milestone announces a verb before it
   ships (e.g., M8 mentions `run` in `--help` but doesn't wire it
   yet), add it to a sibling `cli-surface-future` block. The test
   accepts those as "documented future" and won't flag them missing
   from runtime behaviour. When the surface lands, move the entry
   from `future` to `surface` in the same commit.
3. **PR template.** The "Docs alignment" checklist has a box for
   this skill specifically. Reviewers reject PRs where new CLI
   surface or recipe-affecting behaviour lands without the matching
   manifest update.

If the test fails, fix the manifest — not the test — unless the
shipped behaviour itself is wrong.
