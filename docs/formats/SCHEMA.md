# JSON output schema

This page documents the schema emitted by `distill-ai --output=json`.
This schema is a public API. Breaking changes follow the rules in
[output-stability rule](../../.opencode/rules/output-stability.md).

**Current schema version: `1`**

## Top-level object (batch mode)

When input is bounded (file or finite stdin), `distill-ai` emits a
single JSON object:

```json
{
  "schema_version": 1,
  "format": "pytest",
  "events": [ /* Event objects */ ],
  "summary": { /* Summary object */ }
}
```

## Streaming mode (ndjson)

When input is unbounded (`tail -f`, live pipe), output switches to
newline-delimited JSON, one event per line:

```
{"schema_version":1,"format":"pytest","event":{...}}
{"schema_version":1,"format":"pytest","event":{...}}
{"schema_version":1,"format":"pytest","summary":{...}}
```

Each line is a self-contained JSON object. The final line is a
`summary` object with no `event` key, emitted when the input closes.

## Event object

```json
{
  "severity": "error",
  "kind": "test_failure",
  "title": "AssertionError: expected 302, got 200",
  "location": {
    "file": "tests/api/test_auth.py",
    "line": 47,
    "column": null
  },
  "body": [
    "AssertionError: expected 302, got 200",
    "  at test_auth.py:47"
  ],
  "context": [
    "    response = client.post(\"/login\", data=creds)",
    "    assert response.status_code == 302",
    ">   assert response.headers[\"location\"] == \"/dashboard\""
  ],
  "frames": [
    {
      "file": "tests/api/test_auth.py",
      "line": 47,
      "function": "test_login_redirect",
      "vendor": false
    }
  ],
  "frames_collapsed": 8,
  "count": 1,
  "truncated": false,
  "metadata": {
    "test_id": "tests/api/test_auth.py::test_login_redirect"
  }
}
```

### Field reference

| Field              | Type        | Required | Description                                            |
|--------------------|-------------|----------|--------------------------------------------------------|
| `severity`         | string enum | yes      | One of `error`, `warn`, `info`.                        |
| `kind`             | string      | yes      | Format-specific event type (see below).                |
| `title`            | string      | yes      | One-line human-readable summary.                       |
| `location`         | object      | no       | Source location, if extractable. `null` when unknown.  |
| `location.file`    | string      | yes¹     | File path as it appeared in the input.                 |
| `location.line`    | integer     | yes¹     | 1-indexed line number.                                 |
| `location.column`  | integer\|null | no    | 1-indexed column, if available.                        |
| `body`             | string[]    | yes      | Relevant verbatim lines from the input.                |
| `context`          | string[]    | no       | Surrounding lines (controlled by `--context=N`).       |
| `frames`           | object[]    | no       | Structured stack frames, if extractable.               |
| `frames[].file`    | string      | yes²     | Frame file path.                                       |
| `frames[].line`    | integer     | yes²     | Frame line number.                                     |
| `frames[].function`| string      | no       | Function/method name.                                  |
| `frames[].vendor`  | boolean     | yes²     | `true` if frame was identified as vendor / library code. |
| `frames_collapsed` | integer     | yes      | Number of vendor frames omitted. `0` if none / `--keep-vendor`. |
| `count`            | integer     | yes      | Dedupe count. `1` for unique events; >1 when deduped.  |
| `truncated`        | boolean     | yes      | `true` if `--budget` forced body truncation.           |
| `metadata`         | object      | no       | Format-specific extra fields (string → string map).    |

¹ Required if the parent `location` object is present.
² Required if the parent `frames[]` entry is present.

### Severity values

- `error` — the input indicated a failure (test failed, panic, exception, 5xx).
- `warn` — non-fatal but notable (deprecation, skipped test with reason, timeout retry).
- `info` — neutral notable events (used sparingly; most info is dropped).

### Kind values

`kind` is format-specific. Currently emitted values:

**pytest**: `test_failure`, `test_error`, `collection_error`, `warning`
**jest**: `test_failure`, `snapshot_mismatch`, `suite_error`
**gotest**: `test_failure`, `panic`, `build_failure`, `race_condition`
**generic**: `error`, `warning`, `exception`, `panic`

Per-format kind values are documented in each format's
`docs/formats/<name>.md`.

## Summary object

```json
{
  "input_lines": 8432,
  "output_lines": 24,
  "events_found": 5,
  "events_emitted": 3,
  "events_deduped": 1,
  "events_dropped_budget": 1,
  "frames_collapsed": 47,
  "estimated_tokens": 340,
  "estimator": "heuristic",
  "exit_code": 0
}
```

### Field reference

| Field                   | Type    | Description                                       |
|-------------------------|---------|---------------------------------------------------|
| `input_lines`           | integer | Total lines consumed from input.                  |
| `output_lines`          | integer | Total lines written to stdout.                    |
| `events_found`          | integer | Events detected by the parser.                    |
| `events_emitted`        | integer | Events actually written to output.                |
| `events_deduped`        | integer | Events collapsed into a `count > 1` entry.        |
| `events_dropped_budget` | integer | Events dropped by `--budget` enforcement.         |
| `frames_collapsed`      | integer | Total vendor frames removed across all events.    |
| `estimated_tokens`      | integer | Estimated output token count.                     |
| `estimator`             | string  | Estimator used: `heuristic` or `tiktoken`.        |
| `exit_code`             | integer | Final exit code (0, 1, 2, or 3).                  |

The summary object is always present in JSON output. `--no-footer`
suppresses the human-readable footer block in the `text` and
`markdown` encoders only; for JSON, the summary is part of the
schema and is not optional. Tooling that wants a JSON object
without a summary should ignore the field instead of toggling the
flag.

In streaming (ndjson) mode the summary appears on the final line as
its own top-level object, after every `event` line; in batch mode
it lives under the `summary` key of the single top-level object.

## Versioning

- `schema_version` is the first field of every top-level object so
  consumers can route on it cheaply.
- Additive changes (new optional fields, new `kind` values, new
  severities) **do not** bump the version. Consumers must ignore unknown
  fields.
- Removing or renaming a field, or changing a field's type or semantics,
  bumps `schema_version` and the project's major version.
- Deprecated fields remain in output for one major version cycle with a
  noted deprecation in the changelog.

## Example: parsing in Go

```go
type Output struct {
    SchemaVersion int     `json:"schema_version"`
    Format        string  `json:"format"`
    Events        []Event `json:"events"`
    Summary       Summary `json:"summary"`
}

var out Output
if err := json.NewDecoder(r).Decode(&out); err != nil { /* ... */ }
if out.SchemaVersion != 1 {
    return fmt.Errorf("unsupported schema version: %d", out.SchemaVersion)
}
```

## Example: parsing ndjson stream in jq

```bash
distill-ai --output=json | jq -c 'select(.event.severity == "error") | .event.title'
```
