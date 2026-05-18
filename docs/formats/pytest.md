# pytest format

The `pytest` format parses output from the pytest test runner —
the default reporter and the four `--tb` shapes (`long`, `short`,
`line`, `native`). It is the second specific (non-generic) format
`distill-ai` ships, after `gotest`. Pytest is the most-used non-Go
test runner in the agent-debugging ecosystem; shipping it second
also cross-checks that the shared format-test harness extracted in
M10.5 generalises beyond Go's shape.

## Detection model

`pytest.Detect` raises confidence on the markers below, scored as
"clear" (`1.0`) or "fuzzy" (`0.8`) per the milestone scope in
[TODO.md § M11.1](../../TODO.md#m111--internalformatspytestpytestgo-skeleton--detect):

| Marker (anchored at start-of-line)                | Score |
|---------------------------------------------------|-------|
| `=== test session starts ===`                     | `1.0` |
| `=== FAILURES ===`                                | `1.0` |
| `>   assert ...` plus `conftest.py` or `pytest.ini` | `0.8` |
| Anything else                                     | `0.0` |

The fuzzy rule requires **both** an assertion marker and a config-
file mention. Either signal alone is too generic — `>` is the
universal diff prefix for added lines and shows up in every quoted
patch, and `conftest.py` shows up in prose about pytest itself.
The combined requirement keeps the fuzzy match from claiming
arbitrary content.

## What gets extracted

M11.1 ships only detect + an empty-`Parse` skeleton. The real
scanner lands across M11.2-M11.4:

- **M11.2** — the `=== FAILURES ===` block scanner that emits one
  Event per failure with `Severity=error` and `Kind=test_failure`.
- **M11.3** — `=== ERRORS ===` and collection-error handling
  (kinds `test_error` and `collection_error`).
- **M11.4** — stack-frame extraction from tracebacks, `--tb` shape
  handling (`long`, `short`, `line`, `native`), and warning Events
  (kind `warning` with `Severity=warn`).

The four pytest kinds are documented in
[SCHEMA.md § Kind values](./SCHEMA.md#kind-values).

## What gets dropped

The scanner targets the failure path. Lines outside any block —
collection progress, the dot-per-test progress bar, the
`=== short test summary info ===` and final pass/fail counter line
— are never emitted as Events. The dropped artefacts are documented
in detail once the scanner lands in M11.2.

## Reporter modes

M11.4 will document side-by-side handling of `--tb=long`,
`--tb=short`, `--tb=line`, and `--tb=native`. M11.1 only ships the
detector; the four shapes are all detected the same way (the
session header and FAILURES banner are reporter-independent).
