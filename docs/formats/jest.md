# jest format

The `jest` format parses the output of the jest test runner — the
default reporter as well as the `--verbose` reporter and the
`--ci` / no-ANSI reporter mode. It is the third specific (non-
generic) format `distill-ai` ships, after `gotest` (M10) and
`pytest` (M11). Jest covers the JavaScript / TypeScript niche that
pytest fills for Python and gotest fills for Go.

## Event kinds emitted

| `kind`               | `severity` | Emitted when                                                                                  |
|----------------------|------------|-----------------------------------------------------------------------------------------------|
| `test_failure`       | `error`    | A `●` failure block whose body is an ordinary assertion / exception.                          |
| `snapshot_mismatch`  | `error`    | A `●` failure block containing `expect(...).toMatchSnapshot(...)` or `toMatchInlineSnapshot`. |
| `suite_error`        | `error`    | A `● Test suite failed to run` block (a test file's top-level `require` / `import` failure).  |

The kind list is mirrored in
[`docs/formats/SCHEMA.md` § Kind values](./SCHEMA.md#kind-values)
and pinned by `TestJest_DocumentedKindsMatchEmitted` in the
integration suite.

## Detection model

`jest.Detect` raises confidence on the markers below, scored as
"clear" (`1.0`) or "fuzzy" (`0.8`) per the milestone scope in
[TODO.md § M12.1](../../TODO.md#m121--internalformatsjestjestgo-skeleton--detect):

| Marker (anchored at start-of-line)                       | Score |
|----------------------------------------------------------|-------|
| `● ` bullet (optional leading whitespace)                | `1.0` |
| `FAIL <path>` where `<path>` is a test-file path         | `1.0` |
| `PASS <path>` where `<path>` is a test-file path         | `1.0` |
| `Tests:` summary plus a `jest` mention or `.test`/`.spec`| `0.8` |
| Anything else                                            | `0.0` |

A token after `FAIL` or `PASS` counts as a path when it contains a
`/` or `\`, or when it ends in one of jest's default test-file
suffixes: `.test.js`, `.test.ts`, `.test.jsx`, `.test.tsx`,
`.spec.js`, `.spec.ts`, `.spec.jsx`, `.spec.tsx`. The path-token
guard mirrors gotest's package-token guard from M10.1: unrelated
tools printing a bare `FAIL` line do not raise the score. Verified
by `TestJest_DetectFailRequiresPathToken`.

The `0.8` fuzzy score handles truncated tails of a long run, where
the per-file headers and `●` blocks have already scrolled past. The
combined requirement — a `Tests:` line **and** a corroborating
mention of `jest` or a test-file suffix elsewhere — keeps the fuzzy
match from claiming unrelated output that happens to contain the
word "Tests:". The corroborator-alone case (just "jest" in prose)
also fails to claim the format, verified by
`TestJest_DetectFuzzyJestWordAloneNotEnough`.

## What gets extracted

M12.2 ships the `●` block scanner. Each failure block emits one
Event with:

- `Severity = SeverityError`.
- `Kind = "test_failure"`.
- `Title` derived by walking body lines for, in order:
    1. an `expect(...).toBe(...)`-style assertion call,
    2. an `Expected: <value>` line (jest's structured diff render),
    3. an `Error: <msg>` / `<Class>Error: <msg>` line,
    4. the trimmed test-path text from the `●` header.

  ANSI escape sequences are stripped from Title before precedence
  matching so coloured default-reporter output and plain CI output
  produce the same Title. Body retains the escapes verbatim.
- `Location` = the first `at <fn> (<path>:<line>:<col>)` or
  `at <path>:<line>:<col>` stack-frame line, populated as
  `{File, Line, Column}`. Nil when no stack frame appears in the
  block (deliberately suppressed output, jest's `--noStackTrace`
  flag).
- `Body` = the verbatim block lines from the `●` header through
  the block terminator, ANSI escapes intact.
- `Metadata["test_id"]` = the test-path text from the `●` header,
  with the Unicode chevron `›` (U+203A) normalised to ASCII `>` so
  the value is grep-able from any terminal or editor.
- `Metadata["suite_file"]` = the file path from the most recent
  `FAIL <path>` per-file header in scope.

Block terminators: the next `●` header, the next `FAIL`/`PASS`
per-file header, a `Test Suites:` or `Tests:` summary line, or EOF.

M12.3 promotes the Event Kind from `test_failure` to
`snapshot_mismatch` when a block contains an
`expect(received).toMatchSnapshot(...)` or
`expect(received).toMatchInlineSnapshot(...)` call:

- `Title = "Snapshot mismatch: <name>"` when jest printed a
  `Snapshot name: \`<name>\`` line (file-backed snapshots);
  the generic `"Snapshot mismatch"` form is used otherwise (the
  inline variant, which jest does not name per call).
- `Metadata["snapshot_kind"]` distinguishes `"file"` from
  `"inline"` snapshots.
- The diff lines (`- Snapshot`, `+ Received`, the `-`-prefixed
  and `+`-prefixed body) are preserved verbatim in `Body`. No
  parsing or normalisation of the diff — encoders can render it
  as-is or run their own diff alignment.
- A hard cap of `maxSnapshotLines = 200` keeps memory bounded
  under adversarial inline-snapshot diffs. When the cap fires,
  the last Body line is the sentinel
  `"... [snapshot truncated]"` and
  `Metadata["snapshot_truncated"] = "true"` flags the case for
  downstream consumers. The cap parallels the M9.3 / M10.3
  block-overflow handling.

M12.4 populates `Event.Frames` from every indented `at` line in
the captured block — both the `at <fn> (<path>:<line>:<col>)`
and the bare `at <path>:<line>:<col>` shape — in source order.
Each `StackFrame` has `File`, `Line`, and (when present)
`Function` populated; the parser leaves `Vendor` false so the M5
CollapseStage's `node_modules/` pattern catalogue is the single
source of truth for vendor classification. Frames is `nil`
(not an empty slice) when no `at` line appears so encoders see
a consistent "no frames" signal.

M12.4 also promotes the Event Kind to `suite_error` when the
`●` header is jest's canonical `Test suite failed to run`
phrasing, or when the header text is exactly the suite file path
(the bare-file `●` form a test file's top-level `require` /
`import` failure emits). Suite errors carry `suite_file` but
not `test_id` because there is no individual test to attribute
the failure to.

The reporter modes are unified by content-anchored detection:

- **Default reporter** — the canonical shape M12.2 / M12.3
  target.
- **`--verbose` reporter** — adds `✓` / `✗` per-test indicator
  lines that the scanner drops before any failure block opens.
  Verified by `TestJest_ParseVerboseSameAsTerse`: the verbose
  and terse forms of the same logical failure produce Events
  with identical Title, Kind, and `test_id` (the only Body
  difference is the extra indicator lines, which the scanner
  drops).
- **`--ci` / no-ANSI reporter** — the ANSI strip is a no-op;
  line wrapping is handled because the state machine keys off
  content markers (`●`, `FAIL`, `Snapshot:`) and not column
  positions. Verified by `TestJest_ParseCIReporterModeNoANSI`.

The JSON reporter (`--json` / `--reporters=jest-json`) remains
out of scope for v1.

The set is finalised in M12.5 with the canonical fixture set.

## What gets dropped

Passing tests (`✓` lines under `--verbose`, the `PASS` per-file
headers), `console.log` output between tests, coverage tables under
`--coverage`, the final timing line, and the per-suite summary
lines (`Test Suites:` / `Snapshots:` / `Time:`) are dropped on the
floor. The parser never emits Events for any of these.

## Reporter modes

Three jest reporter shapes the parser handles, decided at detection
time and uniformly thereafter:

- **Default reporter** — `●` failure blocks, per-file `FAIL`/`PASS`
  headers. M12.2 targets this canonical shape.
- **`--verbose` reporter** — adds `✓` / `✗` per-test indicator lines
  before the summary. The parser still anchors on `●` markers and
  ignores the per-test indicator lines.
- **`--ci` / `--reporters=default` CI mode** — drops colours
  (no ANSI), wraps lines differently. The ANSI strip is a no-op;
  line wrapping is handled because the state machine keys off
  content markers (`●`, `FAIL`, `Snapshot:`), not column positions.

The JSON reporter (`--json` / `--reporters=jest-json`) is out of
scope for v1 — it's a different format (structured JSON, no
terminal output). M12 documents the gap; v1.1 can pick it up if
demand surfaces.

## Fixtures

The v1 fixture set lives under
[`internal/formats/jest/testdata/`](../../internal/formats/jest/testdata/).
Eight fixtures, pinned by `TestJest_FixtureCount`:

| Fixture                          | Exercises                                                                                            |
|----------------------------------|------------------------------------------------------------------------------------------------------|
| `clean.input`                    | All-green default-reporter output — scanner emits zero Events.                                       |
| `single-fail.input`              | One `●` failure block with the canonical assertion + stack-frame shape.                              |
| `multi-suite-fail.input`         | Failures across multiple suite files; `metadata.suite_file` attribution.                             |
| `snapshot-mismatch.input`        | A `toMatchSnapshot` block — `snapshot_mismatch` kind, file-backed `metadata.snapshot_kind`.          |
| `inline-snapshot-mismatch.input` | A `toMatchInlineSnapshot` block — inline `metadata.snapshot_kind`.                                   |
| `suite-error.input`              | A `● Test suite failed to run` block from a top-level `require` failure — `suite_error` kind.        |
| `verbose.input`                  | `--verbose` reporter output; per-test `✓` / `✗` indicators dropped.                                  |
| `console-log-noise.input`        | `console.log` output between tests; the scanner drops it without affecting the surrounding Events.   |

Regenerate goldens after a deliberate parser change:

```sh
DISTILL_AI_UPDATE_GOLDENS=1 go test ./internal/formats/jest/
```

The harness is shared with gotest and pytest via
`internal/formats.RunGoldens`.
