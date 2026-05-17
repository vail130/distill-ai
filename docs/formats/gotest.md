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
block, with `Severity=error` and `Kind=test_failure`. Future
sub-items extend the kind set: M10.3 adds `panic` and
`build_failure`; M10.4 adds `race_condition` and stack frame
extraction.

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
  output. M10.4 maps each `Action` to the right kind:
  `fail` → `test_failure`, `output` → buffered into in-progress
  failure body, others → dropped.

`-bench` output is out of scope for v1; it lands in v1.1 if demand
surfaces.
