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

M11.2 ships the `=== FAILURES ===` block scanner. One Event per
failure block, with `Severity=error` and `Kind=test_failure`. M11.3
will add `test_error` and `collection_error`; M11.4 will add stack
frame extraction, `--tb` shape handling (`long`, `short`, `line`,
`native`), and warning Events. The four pytest kinds are documented
in [SCHEMA.md § Kind values](./SCHEMA.md#kind-values).

### `test_failure` Event shape

The scanner advances through a four-state machine — session,
failure-header, failure-body, summary — over a `bufio.Scanner`. It
drops every line until it sees the `=== FAILURES ===` banner, then
anchors a block at each `___ test_id ___` underlined header, then
gathers body lines until the next underlined header, the
`=== short test summary info ===` banner, or any other `=== ... ===`
section divider.

| Field                  | Source                                                                                                                  |
|------------------------|-------------------------------------------------------------------------------------------------------------------------|
| `severity`             | `error`.                                                                                                                |
| `kind`                 | `test_failure`.                                                                                                         |
| `title`                | The first `E   <message>` line in the body — pytest's convention for the assertion / exception detail. Falls back to the trimmed test ID when no `E   ` line is present (e.g. a bare `raise` with `--tb=line`). |
| `location`             | The last `path:line:` line in the body (pytest prints this at the bottom of each failure summary). The path must contain `/` or end in `.py` so unrelated `host:port` shapes don't match. `nil` when no path-shaped line is present. |
| `body`                 | The verbatim block lines from the `___ test_id ___` header onward.                                                      |
| `metadata.test_id`     | The test name parsed from the `___ test_id ___` header. Captures the full bracketed form for parametrised tests (e.g. `test_login[case_a-302]`). |

### Example

Input — a single failure block from a pytest run:

```
=================================== FAILURES ===================================
_______________________________ test_login_redirect _______________________________

    def test_login_redirect():
        creds = {"u": "alice", "p": "secret"}
        response = client.post("/login", data=creds)
        assert response.status_code == 302
>       assert response.headers["location"] == "/dashboard"
E       AssertionError: expected '/dashboard', got '/login?next=/'

tests/test_auth.py:47: AssertionError
=========================== short test summary info ============================
FAILED tests/test_auth.py::test_login_redirect - AssertionError
```

Distilled Event:

- **severity:** `error`
- **kind:** `test_failure`
- **title:** `AssertionError: expected '/dashboard', got '/login?next=/'`
- **location:** `tests/test_auth.py:47`
- **metadata.test_id:** `test_login_redirect`
- **body:** the block lines from `___ test_login_redirect ___` to the
  `tests/test_auth.py:47: AssertionError` summary.

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
