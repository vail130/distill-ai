# gotestsum format

The `gotestsum` format parses the human-readable summary output from
[`gotest.tools/gotestsum`](https://github.com/gotestyourself/gotestsum)
and gotestsum-like wrappers around `go test -json`. It is separate from
`gotest`: canonical `go test` emits `--- FAIL:` blocks, while gotestsum
prints status lines, an `=== Failed` section, `=== FAIL:` blocks, and a
trailing `DONE ...` summary.

## Event kinds emitted

| `kind`          | `severity` | Emitted when                                                                 |
|-----------------|------------|------------------------------------------------------------------------------|
| `test_failure`  | `error`    | A concrete `=== FAIL:` block names a failed package/test.                    |
| `build_failure` | `error`    | A package-level `=== FAIL:` block reports a test-binary invocation failure.  |

The kind list is mirrored in
[`docs/formats/SCHEMA.md` § Kind values](./SCHEMA.md#kind-values).

## Detection model

`gotestsum.Detect` returns confidence `1.0` on any of these markers:

| Marker (anchored at start-of-line)       | Score |
|------------------------------------------|-------|
| `=== Failed`                             | `1.0` |
| `=== FAIL: <pkg-or-test>`                | `1.0` |
| `DONE N tests...`                        | `1.0` |
| `PASS|FAIL|SKIP <pkg>.<Test> (<dur>)`    | `1.0` |
| Anything else                            | `0.0` |

The detector deliberately does not claim canonical `go test` `--- FAIL:`
blocks; those remain owned by the `gotest` format.

## What gets extracted

Each `=== FAIL:` block emits one Event. The parser keeps the block body
verbatim and derives these fields:

| Field                 | Source                                                                 |
|-----------------------|------------------------------------------------------------------------|
| `severity`            | `error`.                                                               |
| `kind`                | `test_failure`, or `build_failure` for package-level flag/build errors. |
| `title`               | The first `file.go:line: message` body line, else the first body line. |
| `location`            | Parsed from `file.go:line[:column]: message` when present.             |
| `body`                | The `=== FAIL:` header plus following lines until the next block/DONE. |
| `metadata.package`    | Package path from `=== FAIL: <pkg> <Test>` or `<pkg>.<Test>`.          |
| `metadata.test_id`    | Test name when the block names one.                                    |
| `metadata.duration`   | Duration from the header when present.                                 |
| `metadata.subject`    | The full subject from the header.                                      |

If the input only contains a failing `DONE ...` summary and no concrete
`=== FAIL:` block, the parser emits one best-effort `test_failure` Event
with `metadata.summary_only = "true"`.

## What gets dropped

Passing and skipped status lines are dropped. The `=== Failed` section
banner and trailing `DONE ...` summary are dropped when concrete failure
blocks exist, because the Events already carry the actionable failure
body.

## Fixtures

Fixtures live under
[`internal/formats/gotestsum/testdata/`](../../internal/formats/gotestsum/testdata/):

| Fixture               | Exercises                                                       |
|-----------------------|-----------------------------------------------------------------|
| `clean.input`         | Passing/skipped status lines plus a clean summary.              |
| `single-fail.input`   | One failed package/test with a source location.                 |
| `flag-error.input`    | Package-level test-binary flag error mapped to `build_failure`. |
| `mixed-summary.input` | Pass/skip/fail status lines plus one concrete failure block.    |
| `realworld.input`     | Sanitised release-blocking gotestsum-like flag-error shape.     |

Regenerate goldens after a deliberate parser change:

```sh
DISTILL_AI_UPDATE_GOLDENS=1 go test ./internal/formats/gotestsum/
```
