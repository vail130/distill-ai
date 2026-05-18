# jest format

The `jest` format parses the output of the jest test runner — the
default reporter as well as the `--verbose` reporter and the
`--ci` / no-ANSI reporter mode. It is the third specific (non-
generic) format `distill-ai` ships, after `gotest` (M10) and
`pytest` (M11). Jest covers the JavaScript / TypeScript niche that
pytest fills for Python and gotest fills for Go.

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

M12.1 ships only the skeleton: `Parse` returns an immediately-closed
channel so the autodetect → parse path is exercised end-to-end
while the scanner is under construction.

M12.2–M12.4 fill in the parser:

- `●`-anchored failure blocks → `Kind="test_failure"`.
- `expect(...).toMatchSnapshot(...)` / `toMatchInlineSnapshot(...)`
  blocks with a `Snapshot:` / `Received:` diff →
  `Kind="snapshot_mismatch"`, with `Metadata["snapshot_kind"]`
  distinguishing file-backed from inline snapshots.
- Top-of-file suite failures (`● Test suite failed to run` and the
  bare-file `●` variant) → `Kind="suite_error"`.
- Frame extraction from the indented `at <fn> (<path>:<line>:<col>)`
  lines at the foot of each failure block.

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
