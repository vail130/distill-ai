# gotest format

The `gotest` format parses the output of the Go test runner — the
default reporter as well as the `-v` verbose reporter and the
`-json` machine-readable reporter. It is the first specific (non-
generic) format `distill-ai` ships, chosen first because it is the
format this very project emits on every `make test`. The dogfood
loop closes inside the project.

## Detection model

`gotest.Detect` raises confidence on the markers below, scored as
"clear" (`1.0`) or "fuzzy" (`0.8`) per the milestone scope in
[TODO.md § M10.1](../../TODO.md#m101--internalformatsgotestgotestgo-skeleton--detect):

| Marker (anchored at start-of-line)                | Score |
|---------------------------------------------------|-------|
| `--- FAIL: `                                      | `1.0` |
| `FAIL\t<pkg>` where `<pkg>` has `/` or `.`        | `1.0` |
| `=== RUN   ` (verbose reporter)                   | `1.0` |
| `goroutine N [state]:` plus a `.go:NNN` reference | `0.8` |
| Anything else                                     | `0.0` |

The `FAIL\t<pkg>` rule deliberately requires a Go-package-shaped
token (an import path with `/`, or a dotted identifier sequence) so
that bare `FAIL` lines from other tools — mocha's terse reporter,
generic CI prose, `FAIL: rebooting <host>` — do not claim the
format. The package-token guard is tested by
`TestGotest_DetectFailRequiresPackageToken`.

The `0.8` fuzzy score catches bare panics emitted by `go run` (or
by `go test` when the test binary panics before any test header
emits). The combined requirement — a `goroutine N [state]:` header
**and** a `.go:NNN` reference — keeps the fuzzy match from
claiming non-Go runtime dumps that happen to mention the word
"goroutine".

## What gets extracted

M10.1 ships only the skeleton: `Parse` returns an
immediately-closed channel so the autodetect → parse path is
exercised end-to-end while the scanner is under construction.

M10.2–M10.4 fill in the parser:

- `--- FAIL:` failure blocks → `Kind="test_failure"`.
- Panic blocks → `Kind="panic"`, with structured Frames extracted
  from the goroutine dump.
- `go build` / `go vet` errors emitted before tests run →
  `Kind="build_failure"`.
- Race-detector reports (the `==================` block) →
  `Kind="race_condition"`.

The set is finalised in M10.5 with the canonical fixture set.

## What gets dropped

Passing tests (`--- PASS:`, `=== RUN`/`=== PAUSE`/`=== CONT` lines
between failures), skipped tests (`--- SKIP:`), per-test `t.Logf`
output between passing tests, the `PASS` / `FAIL` summary line,
and the final `exit status N` line are dropped on the floor. The
parser never emits Events for any of these.

## Reporter modes

Three gotest reporter shapes the parser handles, decided at detection
time and uniformly thereafter:

- **Default reporter** — failure blocks only, no per-test framing.
- **`-v` reporter** — adds `=== RUN`/`=== PAUSE`/`=== CONT`/`--- PASS`
  lines that the parser drops. The Events emitted are identical to
  the default reporter for the same logical failures.
- **`-json` reporter** (`go test -json`) — structured one-JSON-per-line
  output. M10.4 maps each `Action` to the right kind:
  `fail` → `test_failure`, `output` → buffered into in-progress
  failure body, others → dropped.

`-bench` output is out of scope for v1; it lands in v1.1 if demand
surfaces.
