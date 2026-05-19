# pytest format

The `pytest` format parses output from the pytest test runner —
the default reporter and the four `--tb` shapes (`long`, `short`,
`line`, `native`). It is the second specific (non-generic) format
`distill-ai` ships, after `gotest`. Pytest is the most-used non-Go
test runner in the agent-debugging ecosystem; shipping it second
also cross-checks that the shared format-test harness extracted in
M10.5 generalises beyond Go's shape.

## Event kinds emitted

| `kind`              | `severity` | Emitted when                                                       |
|---------------------|------------|--------------------------------------------------------------------|
| `test_failure`      | `error`    | A `=== FAILURES ===` block (any `--tb` shape).                     |
| `test_error`        | `error`    | A `=== ERRORS ===` block (fixture error, in-test error).           |
| `collection_error`  | `error`    | A collection-phase failure (e.g. a syntax error in a test module). |
| `warning`           | `warn`     | A `=== warnings summary ===` entry (with `--keep-warnings`).       |

The kind list is mirrored in
[`docs/formats/SCHEMA.md` § Kind values](./SCHEMA.md#kind-values)
and pinned by `TestPytest_DocumentedKindsMatchEmitted` in the
integration suite.

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
adds the `=== ERRORS ===` section with two kinds: `test_error` for
per-test fixture / setup failures, and `collection_error` for
import-time / conftest failures that prevented tests from running.
M11.4 adds stack-frame extraction (`File "..."` and short-form
`path:line: in func` lines), warning Events from
`=== warnings summary ===`, and honours
`opts.MinSeverity` / `opts.KeepWarnings`. The four pytest kinds
are documented in
[SCHEMA.md § Kind values](./SCHEMA.md#kind-values).

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

### `test_error` and `collection_error` Event shapes

The `=== ERRORS ===` banner opens a section that hosts two
distinct event populations. The classification rule is:

| Trigger                                                       | Kind               |
|---------------------------------------------------------------|--------------------|
| `=== ERRORS ===` appears *before* any `=== FAILURES ===`      | `collection_error` |
| `=== ERRORS ===` appears *after* `=== FAILURES ===`           | `test_error`       |
| Underline of the form `___ ERROR collecting <path> ___`       | `collection_error` (overrides the section-order rule) |

The reasoning: pytest emits ERRORS-before-FAILURES when tests
never ran (collection phase failed); it emits ERRORS-after-FAILURES
when fixture setup failed mid-run. The per-block underline always
wins because pytest occasionally interleaves collection errors
mid-run when `--continue-on-collection-errors` is set.

The Event shape is otherwise identical to `test_failure`. Two
differences:

- `metadata.test_id` is **absent** on `collection_error` Events
  (there is no individual test in scope). It is present on
  `test_error` Events.
- When a `collection_error` block has no `path:line:` summary
  line (a truncated import-time error), `Location.File` is
  populated from the `ERROR collecting <path>` underline so
  consumers can still link to the offending file.

### Stack frames

`Event.Frames` is populated for any failure / error block whose
body contains traceback frames. Two shapes are recognised:

| pytest reporter | Frame shape recognised                              |
|-----------------|-----------------------------------------------------|
| `--tb=long`     | `File "<path>", line N, in <func>` (Python long)    |
| `--tb=native`   | Same Python long form, flush-left                   |
| `--tb=short`    | `<path>:<line>: in <func>` (compact)                |
| `--tb=line`     | One-line summary; no frame data — `Frames` is `nil` |

When at least one matching line is found, the parser builds a
`StackFrame` per match with `Vendor=false`. The M5
`CollapseStage` re-populates `Vendor` from its pattern catalogue
(site-packages, dist-packages, stdlib paths) so consumers running
with `--keep-vendor=false` see only user-code frames.

### Warning Events

Pytest emits a `=== warnings summary ===` section near the end of
a run for every captured Python warning. The scanner anchors a
warning Event on each unindented header line under the banner —
both the `<path>.py:<line>` and `<path>.py::<test_id>` shapes are
recognised — and folds the indented continuation lines (typically
the `WarningClass: message` detail) into the body.

| Field      | Source                                                                                   |
|------------|------------------------------------------------------------------------------------------|
| `severity` | `warn`.                                                                                  |
| `kind`     | `warning`.                                                                               |
| `title`    | The `<WarningClass>: <message>` form derived from a body line; falls back to the first non-header line. |
| `location` | `<path>.py:<line>` parsed from the unindented header.                                    |
| `body`     | The header plus indented continuation lines.                                             |

**Warnings are dropped by default.** The parser only emits warning
Events when either `ParseOpts.KeepWarnings` is true or
`ParseOpts.MinSeverity` is explicitly set to `warn` or `info`.
The precedence rule matches the generic format: an explicit
`MinSeverity` always wins over `KeepWarnings=false`.

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

## Fixtures

The v1 fixture set lives under
[`internal/formats/pytest/testdata/`](../../internal/formats/pytest/testdata/).
Eight fixtures, pinned by `TestPytest_FixtureCount`:

| Fixture                     | Exercises                                                                                |
|-----------------------------|------------------------------------------------------------------------------------------|
| `clean.input`               | All-green session — scanner emits zero Events.                                           |
| `single-fail.input`         | One `=== FAILURES ===` block with the canonical `--tb=long` shape.                       |
| `multi-fail.input`          | Three failures in one session; ordering and per-test attribution.                        |
| `parametrised.input`        | Parametrised test failures with the bracketed parameter IDs.                             |
| `xfail-xpass.input`         | `xfail` / `xpassed` markers — parser distinguishes from real failures.                   |
| `collection-error.input`    | A collection-phase syntax error before any test runs.                                    |
| `errors.input`              | `=== ERRORS ===` block from a fixture error.                                             |
| `warnings.input`            | `=== warnings summary ===` entries; exercises the `--keep-warnings` / `MinSeverity` opt-in. |

Regenerate goldens after a deliberate parser change:

```sh
DISTILL_AI_UPDATE_GOLDENS=1 go test ./internal/formats/pytest/
```

The harness is shared with gotest and jest via
`internal/formats.RunGoldens`.
