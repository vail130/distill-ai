---
name: distill-output
description: Dogfood the local `distill-ai` binary on this project's own command output. Use whenever a test run, build, lint, or other command produces enough output that the interesting failures are buried — pipe it through `./bin/distill-ai` (or the integration-test binary) instead of reading thousands of lines. Forces the maintainer (you) to feel every gap in the tool before customers do.
---

# distill-output

This is `distill-ai`'s repo. We use the tool we are building on our own
command output, so every rough edge surfaces during normal development
instead of in a downstream user's terminal. Dogfooding is the cheapest
honest test we have.

## When to use

- Any time `make test` / `go test ./...` produces more than ~30 lines.
- Any time `make lint` reports more than a couple of findings.
- Any time you're about to copy a long failure block into a chat
  window or ticket — distill it first.
- Any time you change a format parser and want to see the resulting
  Event stream on a real fixture.

Do **not** use this skill when:

- You need the raw stream (e.g., debugging the distiller itself with
  `-v` / `--verbose`). Run the command bare.
- The command runs under 1 second and emits under 30 lines. The
  overhead of the extra pipeline is not worth the saving.

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
**M8**. The **generic fallback** landed in **M9.1** (registration +
detect floor) and **M9.2** (severity-anchored scanner). Real
command output now distills end-to-end via the generic fallback
when no specific format claims it. `cmd | distill-ai` is the
canonical invocation: it reads stdin, autodetects the format, and
distils to stdout.

The remaining gap is the **specific format set**. Until M10/M11/M12
ship gotest/pytest/jest, every invocation falls back to `generic`
— which now extracts `ERROR` / `FATAL` / `panic:` / `Exception:` /
`Traceback ` / `WARN` lines as Events with surrounding context.
M9.3 adds multi-line traceback / panic block extraction with
parsed stack frames; M9.4 wires `--severity` / `--keep-warnings`.
Use `--strict` to turn the fallback into a hard error (exit 2)
for CI.

The full surface today is enumerated in the manifest below.

<!-- BEGIN cli-surface -->
```surface
subcommands: completions, detect, explain, list-formats, run, version
flags: --help, --version, -h, --auto, --keep-vendor, --dedupe, --no-dedupe, --output, --output-streaming, --budget, --no-footer, --strict, --tokenizer, --list-formats
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
   Traceback / FATAL lines with surrounding context. M9.3 will add
   multi-line block extraction for tracebacks and panics; M9.4
   adds `--severity` / `--keep-warnings` plumbing.

## Recipes

Every recipe below is written for the surface as it is today.
Recipes that depend on a not-yet-registered format are labelled
with the milestone that unblocks them.

### Inspect the CLI surface

```sh
./bin/distill-ai --help
./bin/distill-ai run --help
./bin/distill-ai explain --help
```

The `run` and `explain` help pages enumerate the full flag set with
behaviour notes, including the flags whose plumbing lands in
M8.2.x follow-up commits.

### Check what the detector thinks of a fixture

```sh
./bin/distill-ai detect test/integration/testdata/fixtures/gotest-fail.input
echo "exit: $?"
```

After M9.1 this prints `format: generic` with
`fellback_to_generic: true` on stdout and exits 1, because no
specific format is registered to claim gotest output yet. Once M10
ships, the expected output flips to `format: gotest` with
`confidence: 1.00` and exit 0. Use `--strict` to turn the "fell
back to generic" path into a hard error (exit 2).

### Distil this project's own `go test` output (once M10 lands)

```sh
make test 2>&1 | ./bin/distill-ai
```

This is the canonical dogfooding loop: every test run becomes a
real-world distill-ai input. M10 ships gotest specifically to make
this loop work; gaps in the parser surface the moment you run
`make test`. Pre-M10 the same command falls back to the generic
scanner (M9.1+); M9.2 fills in the scanner so the fallback emits
useful events. The same shape works for any tool's output —
`kubectl logs`, an application log — and falls back to the
regex-driven generic scanner when no specific format claims it.

### Distil a pytest run (once M11 lands)

```sh
pytest -v 2>&1 | ./bin/distill-ai pytest --output=markdown
```

Useful when working on `internal/formats/pytest/` — feed your own
fixture, see what comes out, iterate. The explicit `pytest`
argument skips autodetect; drop it to let the detector pick.

### Dry-run a pipeline with `explain`

```sh
./bin/distill-ai explain captured.log
```

Emits one diagnostic line per event saying whether it was kept and,
if dropped, why (`severity-filter`, `budget`, `dedupe-evicted`,
`vendor-collapsed`). Useful when a particular `--budget` setting is
silently pruning events you expected to see. After M9.1 the dry-run
succeeds for any input (falls back to `generic` when nothing
specific matches); until M9.2 fills in the scanner, the dry-run
will simply report zero kept events.

### Constrain output to a token budget

```sh
cmd 2>&1 | ./bin/distill-ai --budget=2000 --tokenizer=heuristic
```

Drops or truncates lower-severity events to fit. Exit code 3 if any
event was dropped or truncated. The heuristic estimator is
zero-dep; switch to `--tokenizer=tiktoken` when you need exact
counts for OpenAI / Claude context windows.

### Compare the distilled output against the golden

```sh
./bin/distill-ai detect testdata/case-XX.input \
  | diff - testdata/case-XX.expected.detect
```

The integration test suite at `test/integration/` runs a stronger
form of this: the suite compiles the binary, runs it against every
real fixture, and diffs against the committed expected output.

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
