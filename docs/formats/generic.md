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

(Once M9.2 / M9.3 / M9.4 land. M9.1 ships the skeleton: a
registered `Format` whose `Parse` returns an immediately-closed
channel, so the detector's fallback path can be exercised
end-to-end while the scanner is being written.)

- One `Event` per line that matches the severity catalogue.
- Severity and `Kind` come from the matched pattern. Kinds emitted:
  `error_line`, `warning_line`, `traceback`, `panic`, `exception`.
- `Title` is the matched line with leading ANSI escapes stripped.
- `Body` keeps the original line(s) verbatim — ANSI included — so
  the user sees what actually arrived.
- `Context` carries up to N lines before and after the anchor
  (default 3 each). Lines that themselves match the catalogue are
  still included as context; the scanner does not deduplicate
  adjacent matches into a single Event.
- `Location` is best-effort: if the anchor line contains a
  `path:line` pair where the path has at least one `/` or `\`
  separator, the parser populates `Event.Location`. Bare
  `host:port` pairs are not treated as paths.
- `Frames` is populated for `traceback` and `panic` Events when
  M9.3 lands; for every other Kind it stays nil.

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

(Filled in during M9.2 / M9.3 / M9.5. For now this section is a
placeholder so future contributors know where the example goes.)

## Filtering semantics

(M9.4 wires `--severity` and `--keep-warnings`. This section
documents the precedence rules — `MinSeverity` vs `KeepWarnings`,
and the rule that filtering happens inside the parser so a
filtered anchor's context is freed for the next Event.)

## Fixtures

(M9.5 ships ten canonical fixtures under
`internal/formats/generic/testdata/`. Each is enumerated here with
a one-line description.)
