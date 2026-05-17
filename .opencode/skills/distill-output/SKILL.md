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
**M8** (latest commit at time of writing). `cmd | distill-ai` is the
canonical invocation: it reads stdin, autodetects the format, and
distils to stdout. `run` is the default subcommand so the explicit
form `cmd | distill-ai run` is equivalent and only needed when you
want positional `FORMAT` / `FILE...` arguments.

The remaining gap is the **format set**. Until M9 ships the
generic fallback and M10/M11/M12 ship gotest/pytest/jest, every
`distill-ai` invocation against real input fails with exit code 2
(`distill-ai run: no format matched stdin`) because no specific
format is registered in the production binary. The integration test
suite deliberately pins this gap so M9 / M10 / M11 / M12 each
surface as a visible behaviour change.

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
  stdout. Today fails with exit 2 ("no format matched") because no
  specific format is yet registered; once M9 lands, the same command
  will return distilled output via the generic fallback.
- `./bin/distill-ai run [FORMAT] [FILE...]` — explicit form. Useful
  when you want to pass multiple files, force a specific format, or
  bypass autodetection with `--auto=false`.
- `./bin/distill-ai detect FILE` — print which format wins
  detection, with confidence, sample size, and runner-up. Accepts
  `-` for stdin.
- `./bin/distill-ai explain [FORMAT] [FILE...]` — dry-run mode:
  emit one `kept` / `dropped:<reason>` line per event without
  writing distilled output. Useful when `--budget` aggressively
  prunes events you expected to see.
- `./bin/distill-ai list-formats` — list every registered format
  with version and source. Pre-M9 this prints an empty list (header
  only) because nothing is registered in the production binary.
- `./bin/distill-ai completions [bash|zsh|fish|powershell]` —
  generate a shell completion script.
- `./bin/distill-ai version` — print version, commit, build date.
- `./bin/distill-ai --version` / `--help` — the standard
  affordances; `--version` is equivalent to the subcommand.

The dogfooding loop today (until M9 lands):

1. Run the noisy command, capture to a tempfile.
2. Use `detect` to confirm the format autodetector picks the right
   thing (it won't yet — exit 1, "no format matched" — that's the
   M9/M10/M11/M12 gap).
3. Read the captured file directly until specific formats arrive.

Once M9 ships the generic fallback, step 2 collapses to
`noisy-command | ./bin/distill-ai` returning a distilled stream.

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

Today this prints a no-match diagnostic to stderr and exits 1
because gotest isn't registered yet. Once M10 ships, the expected
output flips to `format: gotest` with `confidence: 1.00` on stdout
and exit 0. Use `--strict` to turn the post-M9 "fell back to
generic" path into a hard error (exit 2).

### Distil this project's own `go test` output (once M10 lands)

```sh
make test 2>&1 | ./bin/distill-ai
```

This is the canonical dogfooding loop: every test run becomes a
real-world distill-ai input. M10 ships gotest specifically to make
this loop work; gaps in the parser surface the moment you run
`make test`. Pre-M10 the same command falls back to generic (post-M9)
or exits 2 (pre-M9). The same shape works for any tool's output —
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
silently pruning events you expected to see. Today the same gap
applies — pre-M9 there are no formats, so the dry-run reports the
same "no format matched" error as `run`.

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
