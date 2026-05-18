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

M10.2 ships the `--- FAIL:` block scanner. One Event per failure
block, with `Severity=error` and `Kind=test_failure`. M10.3 adds
two more kinds:

- **`panic`** — emitted when a `panic:` block fires, with the
  goroutine dump preserved verbatim in `Body`. When the panic
  is associated with a running test, `metadata.test_id` is set
  and the trailing `--- FAIL: TestName` is suppressed (the
  panic carries the diagnostic; the redundant `test_failure`
  would just be noise). Bounded by `maxPanicLines = 200` lines;
  beyond the cap, the final Body line becomes
  `... [panic block truncated]` and
  `metadata.panic_truncated = "true"`.
- **`build_failure`** — emitted for each `path/to/file.go:line:col:
  message` line gotest prints when compilation fails. Path,
  Line, Column populate `Location`. When the trailing
  `FAIL\t<pkg> [build failed]` summary is present, the line is
  consumed by the scanner (no `test_failure` is synthesised).

M10.4 adds:

- **`race_condition`** — emitted when a `==================`-framed
  race-detector report appears. `Title` is the canonical
  `WARNING: DATA RACE` line; `Body` retains the report verbatim
  including dividers; `Frames` carries entries from both
  goroutine stacks the report contains; `metadata.race_goroutines`
  is `"2"`. Bounded by `maxRaceLines = 300` lines with the same
  sentinel + `metadata.race_truncated` pattern as `panic`.
- **Structured stack frames** on `panic` and `race_condition`
  Events. Each `\tfile.go:line +0xOFFSET` tail line plus the
  preceding `pkg.Func(args)` line produces one `StackFrame`.
  Frames are emitted only when at least one pair matches; the
  M5 CollapseStage repopulates `Vendor` from the frame file.
- **`-json` reporter** support. When the first non-blank input
  line begins with `{"Time":`, the scanner dispatches to the
  JSON-line parser. Per-test `output` actions accumulate into a
  body buffer; `fail` actions emit a `test_failure` Event with
  the assembled body; `pass` / `skip` discard the buffer.
  Build-failure `output` actions (Test == "" plus a
  `path:line:col: msg` shape) emit `build_failure` Events
  directly. Per-package `fail` actions (Test == "") are
  swallowed.

### `test_failure` Event shape

The scanner anchors on `--- FAIL:` headers and groups them with
the preceding indented per-test output and any body lines emitted
before the next block delimiter (`--- FAIL`/`--- PASS`/`--- SKIP`,
`=== RUN`, `FAIL\t<pkg>`, `PASS`, or EOF).

| Field                  | Source                                                                                                                  |
|------------------------|-------------------------------------------------------------------------------------------------------------------------|
| `severity`             | `error`.                                                                                                                |
| `kind`                 | `test_failure`.                                                                                                         |
| `title`                | The assertion message — the `<file>:<line>: <msg>` shape gotest's `t.Errorf` / `t.Fatalf` emit. Falls back to the `--- FAIL:` header line when no assertion shape is found. |
| `location`             | `file.go:line` parsed from the assertion line. `nil` when no assertion line matched.                                    |
| `body`                 | The verbatim block lines: the assertion(s) plus the `--- FAIL:` header.                                                 |
| `metadata.test_id`     | The test name parsed from the `--- FAIL:` header, including subtest path (`TestParse/empty_input`).                     |
| `metadata.package`     | The Go import path from the trailing `FAIL\t<pkg>` summary line, when known.                                            |
| `metadata.duration`    | The duration string from the `--- FAIL:` header (e.g. `0.02s`).                                                         |

### Per-package buffering

Gotest emits the package import path only on the trailing
`FAIL\t<pkg>` summary line, after every per-test `--- FAIL:` block
in the package. The scanner buffers per-package failures and
flushes them when the package summary line is consumed, so each
emitted Event carries `metadata.package`. The buffer is bounded —
a Go package rarely has more than a handful of failing tests per
run — and across packages the scanner still streams: the first
package's events emerge as soon as that package's summary line
arrives, while the next package's tests are still being scanned.
This is the M10.2 trade-off documented in
[TODO.md § M10.2](../../TODO.md#m102--parse----fail-blocks-test_failure).

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
  output. The scanner detects `{"Time":` on the first non-blank input
  line and dispatches to the JSON parser. Per-test `output` actions
  accumulate body lines (with `=== RUN` / `--- PASS` / `--- FAIL`
  framing filtered out); `fail` actions emit one `test_failure`;
  `pass` / `skip` discard the buffer; per-package `fail` actions are
  swallowed. Build errors (Output with `Test == ""`) map to
  `build_failure` directly.

`-bench` output is out of scope for v1; it lands in v1.1 if demand
surfaces.

## Fixtures

The v1 fixture set lives under
[`internal/formats/gotest/testdata/`](../../internal/formats/gotest/testdata/).
Eight fixtures, pinned by `TestGotest_FixtureCount`:

| Fixture                  | Exercises                                                                  |
|--------------------------|----------------------------------------------------------------------------|
| `clean.input`            | All-green default-reporter output — scanner emits zero Events.             |
| `single-fail.input`      | One `--- FAIL:` block with a `file:line: msg` assertion.                   |
| `multi-fail.input`       | Two failures across one package; per-package buffering attribution.        |
| `subtests.input`         | Table-driven subtest failures with the slash-separated subtest path.       |
| `panic.input`            | A `panic:` block with goroutine dump, `[recovered]`, and `created by`.     |
| `race.input`             | A `==================`-framed race-detector report.                        |
| `build-failure.input`    | `path/to/file.go:line:col: message` errors before tests run.               |
| `json.input`             | `go test -json` reporter — JSON-per-line mode.                             |

Regenerate goldens after a deliberate parser change:

```sh
DISTILL_AI_UPDATE_GOLDENS=1 go test ./internal/formats/gotest/
```

The harness lives at `internal/formats.RunGoldens` so future
formats (pytest, jest) share the same fixture mechanics.
