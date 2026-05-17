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

### Block extraction (traceback / panic)

When the scanner anchors a `traceback` or `panic` Event, it
switches into **block mode**: subsequent lines extend `Event.Body`
until the kind's continuation rule fails, `maxBlockLines = 100`
is hit (the final Body line becomes `... [block truncated]`), or
EOF arrives. After the block closes, `Event.Frames` is populated
by parsing the captured Body, and the trailing-context window
applies *after* the block, not after the anchor.

Continuation patterns by kind:

| Kind        | Continues on                                              |
|-------------|------------------------------------------------------------|
| `traceback` | Indented lines (`^\s`), Python frames (`^\s+File "`), JVM frames (`^\s+at `), JVM `... N more` tail, blank lines, dedented exception-message lines (`^TypeName(Error\|Exception\|Warning):`) |
| `panic`     | `goroutine N [...]:`, hex-address tails (`^\s*0x...`), any indented line, blank lines, repeated `panic: `, signal subheaders (`^\[signal ...`), Go call lines (`pkg.Func(args)` or `(*T).method(args)`) |

Frame extractors per kind:

- **`traceback`** (Python): `File "PATH", line N, in FUNC` →
  `StackFrame{File, Line, Function}`.
- **`traceback`** (JVM): `at pkg.Class.method(File.java:N)` →
  `StackFrame{File, Line, Function}`. The two extractors run
  over the same Body so a mixed-language input would still
  produce frames in source order.
- **`panic`** (Go): the function name comes from a non-indented
  `pkg.Func(args)` line; the file and line come from the
  immediately-following indented `path:line +0xOFFSET` tail.

Title re-derivation:

- **`traceback`** Title is re-set to the last non-blank Body
  line after the block closes — the actual exception message
  (`KeyError: 'foo'`, `ValueError: ...`).
- **`panic`** Title stays as the original `panic: <message>`
  line.

## Filtering semantics

The generic scanner honours two `formats.ParseOpts` fields fed by
the CLI's `--severity` and `--keep-warnings` flags:

- `MinSeverity` — empty value defaults to `event.SeverityError`.
- `KeepWarnings` — when true, drops the effective minimum to
  `event.SeverityWarn`.

Precedence (read top-down; first matching rule wins):

| `MinSeverity` | `KeepWarnings` | Effective minimum |
|---------------|----------------|--------------------|
| any           | true           | `warn` (unless MinSeverity is lower than warn — see below) |
| `info`        | false          | `info` (explicit setting wins; emits errors + warnings) |
| `warn`        | false          | `warn` |
| `error` / "" | false           | `error` |

An anchor line whose severity is below the effective minimum is
**dropped at the anchor stage** rather than emitted as an Event,
but the line still slides into the pre-context ring. Surviving
Events therefore see filtered anchors as ordinary context lines.
This matches the rule "drop the anchor, keep the surrounding lines
as context."

### Example

Input:

```text
WARN: low memory
ERROR: thing broke
```

With defaults: one Event for `ERROR: thing broke`; the `WARN:` line
appears in its `context`.

With `--keep-warnings`: two Events, one for each anchor.

With `--severity=warn`: same as `--keep-warnings`.

With `--severity=bogus`: the CLI rejects the flag with exit 2 and a
`invalid --severity` diagnostic.

## Fixtures

(M9.5 ships ten canonical fixtures under
`internal/formats/generic/testdata/`. Each is enumerated here with
a one-line description.)
