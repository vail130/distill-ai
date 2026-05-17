# generic format

The `generic` format is the **regex-driven fallback** parser. The
detector picks it whenever no specific format scores above
[`event.ConfidenceMinDetect`](../../internal/event/event.go) (0.6).

It cannot do what `pytest` / `jest` / `gotest` do — it has no
test-runner semantics, no structured frame extraction beyond best-
effort `file:line:` matches. It exists so that piping arbitrary log
output through `distill-ai` yields something rather than nothing: a
sequence of severity-bucketed Events anchored to `ERROR`, `FATAL`,
`panic`, `Exception`, `Traceback`, and friends, with N lines of
surrounding context.

## Detection model

`generic` is **excluded from the detector's candidate set** up
front (see [`internal/detect`](../../internal/detect/detect.go) §
`GenericFormatName`). It is never compared against a specific
format on ties — the detector reserves it for the fallback path
after every specific format has scored below threshold.

For that reason `Detect` is deliberately a low floor:

| Sample contents                              | Confidence |
|----------------------------------------------|------------|
| At least one line matching the catalogue     | `0.1`      |
| Anything else (innocuous prose, binary, etc.)| `0.0`      |

`0.1` is well below `ConfidenceMinDetect` (`0.6`), and stays there
by design. Future contributors must not inflate this constant to
try to "win" ties; the detector's exclusion already enforces that
generic always loses to a specific format.

The cheap detect-time scan looks for any of these markers anywhere
in the 4 KiB sample:

```
ERROR    FATAL    WARN  WARNING
panic:   Exception:    Traceback <space>
Error:   Warning:
```

The real, richer catalogue used at parse time lives in M9.2.

## What gets extracted

After M9.2 the scanner runs line-by-line through a `bufio.Scanner`
and:

- Emits one `Event` per line that matches the severity catalogue.
- Severity and `Kind` come from the matched pattern. Kinds emitted:
  `error_line`, `warning_line`, `traceback`, `panic`, `exception`.
- `Title` is the matched line with ANSI escape sequences stripped
  and trailing whitespace trimmed.
- `Body` keeps the original line verbatim — ANSI included — so
  the user sees what actually arrived.
- `Context` carries up to N lines before and after the anchor
  (default 3 each, configurable via `ParseOpts.ContextLines` —
  the CLI `--context=N` plumbing lands later). Lines that
  themselves match the catalogue are still included as context;
  the scanner does not deduplicate adjacent matches into a single
  Event.
- `Location` is best-effort: if the anchor line contains a
  `path:line(:col)?` pair where the path has at least one `/` or
  `\` separator, the parser populates `Event.Location`. Bare
  `host:port` pairs are not treated as paths.
- `Frames` is populated for `traceback` and `panic` Events when
  M9.3 lands; for every other Kind it stays nil today.

### Catalogue (v1)

The catalogue is evaluated in order; first match wins. Listed
specific kinds first so e.g. `Traceback ` outranks generic
`Error:`.

| Pattern (regex)                | Kind            | Severity |
|--------------------------------|-----------------|----------|
| `\bTraceback `                 | `traceback`     | error    |
| `^panic:`                      | `panic`         | error    |
| `\bException:`                 | `exception`     | error    |
| `\bERROR\b`                    | `error_line`    | error    |
| `\bFATAL\b`                    | `error_line`    | error    |
| `(?i)\bcaused by:`             | `error_line`    | error    |
| `\bError:`                     | `error_line`    | error    |
| `\bWARN(?:ING)?\b`             | `warning_line`  | warn     |
| `\bDeprecation\b`              | `warning_line`  | warn     |
| `^W\d{4}:`                     | `warning_line`  | warn     |
| `\bWarning:`                   | `warning_line`  | warn     |

The catalogue is matched against an ANSI-stripped copy of the
line, so a coloured anchor like `\x1b[31mERROR\x1b[0m: thing`
still anchors. Body keeps the original (coloured) line.

## What gets dropped

- `info`-level scanning is **deliberately empty** in v1. Healthy
  stdout has too much INFO noise to be worth scanning. Hooking it
  up is backlog work. Set `--severity=info` to widen the filter
  once M9.4 lands; without `info` catalogue entries, the result is
  the same as `warn`.
- Lines that match no catalogue entry are dropped unless they
  fall inside another Event's context window.
- ANSI escape sequences are stripped from `Title` but kept in
  `Body`. Consumers reading `Body` raw will see colour codes.

## Example I/O

Input:

```text
info: connecting
info: ready
ERROR: connection to db:5432 refused
debug: retrying
debug: gave up
```

Resulting Event (JSON view):

```json
{
  "severity": "error",
  "kind": "error_line",
  "title": "ERROR: connection to db:5432 refused",
  "location": null,
  "body": ["ERROR: connection to db:5432 refused"],
  "context": [
    "info: connecting",
    "info: ready",
    "debug: retrying",
    "debug: gave up"
  ]
}
```

The bare `db:5432` does not register as a `Location` because the
heuristic requires at least one `/` or `\` in the path segment.

Block extraction for `traceback` / `panic` (multi-line body, with
parsed frames) lands in M9.3.

## Filtering semantics

(M9.4 wires `--severity` and `--keep-warnings`. This section
documents the precedence rules — `MinSeverity` vs `KeepWarnings`,
and the rule that filtering happens inside the parser so a
filtered anchor's context is freed for the next Event.)

## Fixtures

(M9.5 ships ten canonical fixtures under
`internal/formats/generic/testdata/`. Each is enumerated here with
a one-line description.)
