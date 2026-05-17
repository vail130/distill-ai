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

## The "before specific formats" gap

The end-to-end pipeline CLI (the `run` verb) landed in **M8.2**. The
`run` subcommand is the default — `cmd | distill-ai` is the canonical
invocation — and most flags from
[ARCHITECTURE.md § Flags](../../../ARCHITECTURE.md#flags) are wired.

The remaining gap is the **format set**. Until M9 ships the generic
fallback, every `distill-ai` invocation against real input fails with
exit code 2 ("no format matched") because no specific format is
registered in the production binary. The integration test suite
deliberately pins this gap so M9 / M10 / M11 / M12 each surface as a
visible behaviour change.

The full surface today is enumerated in the manifest below.

<!-- BEGIN cli-surface -->
```surface
subcommands: detect, list-formats, run
flags: --help, --version, -h, --auto, --keep-vendor, --dedupe, --no-dedupe, --output, --output-streaming, --budget, --no-footer, --strict, --tokenizer, --list-formats
```
<!-- END cli-surface -->
<!-- BEGIN cli-surface-future -->
```surface
subcommands: explain, completions, version
```
<!-- END cli-surface-future -->

That manifest is **machine-parsed by the integration test suite**
(see `TestSkill_DocumentsCurrentCLISurface` in
`test/integration/integration_test.go`). When the CLI grows — M8
adds `run`, `list-formats`, `explain`, `completions`, `version` as
subcommands and ~15 new flags — update the manifest in the same
commit that wires the surface. The test fails loudly otherwise.

Detailed forms today:

- `./bin/distill-ai` — read stdin, autodetect, distil to stdout
  (currently fails with exit 2 until M9 ships the generic fallback).
- `./bin/distill-ai run FORMAT FILE...` — explicit format and file
  inputs.
- `./bin/distill-ai detect FILE` — detect the format of a single file.
- `./bin/distill-ai detect -`    — same, reading stdin.
- `./bin/distill-ai --version`
- `./bin/distill-ai --help`

The dogfooding loop today (M9–M12 still in flight):

1. Run the noisy command, capture to a tempfile.
2. Use `detect` to confirm the format autodetector picks the right
   thing.
3. Read the captured file directly if `detect` produced the wrong
   verdict — the file is your test fixture for the format parser.

Once M9–M12 land specific formats, this collapses to
`noisy-command | ./bin/distill-ai`.

## Recipes

### Distil `go test` output (post-M8)

```sh
make test 2>&1 | ./bin/distill-ai gotest --output=text
```

Today (pre-M8) the equivalent is:

```sh
make test > /tmp/distill-ai-test.log 2>&1 || true
./bin/distill-ai detect /tmp/distill-ai-test.log
# Then inspect /tmp/distill-ai-test.log directly until M8 wires `run`.
```

### Distil a pytest run (once M10 lands)

```sh
pytest -v 2>&1 | ./bin/distill-ai pytest --output=markdown
```

Useful when working on `internal/formats/pytest/` — feed your own
fixture, see what comes out, iterate.

### Check what the detector thinks of a fixture

```sh
./bin/distill-ai detect internal/formats/pytest/testdata/single-fail.input
```

Expected output is `format: pytest` with `confidence: 1.00`. If the
confidence drops, the detector regressed.

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
