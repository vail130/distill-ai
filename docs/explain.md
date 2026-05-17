# `distill-ai explain`

The `explain` subcommand is a dry-run mode: it runs the same
pipeline as `distill-ai run` but emits a per-event diagnostic line
instead of the distilled output. Use it to understand why a given
run produced the output it did — particularly when `--budget`
aggressively prunes events you expected to see.

## Output shape

Each line is one of:

```
kept   <SEVERITY> <title> [at file:line] [<dedupe-evicted=K>] [<vendor-collapsed=N>] [<truncated>]
dropped:<reason> <SEVERITY> <title> [at file:line]
```

The bracketed fragments on a `kept` line appear only when relevant:

- `<dedupe-evicted=K>` — the event's `Count` field was `K+1`, meaning
  the dedupe stage merged `K` later sightings of an identical event
  into this single emitted record.
- `<vendor-collapsed=N>` — the event's `FramesCollapsed` field
  reports `N` vendor stack frames were folded into a single
  collapse marker.
- `<truncated>` — the event's body was shortened by `--budget`
  enforcement.

## Drop reasons

Today the explain sink emits the following reasons:

| Reason            | Source                                    |
| ----------------- | ----------------------------------------- |
| `budget`          | `ExplainingBudgetStage` dropped or truncated the event to fit `--budget`. |
| `severity-filter` | The format's parser filtered the event below the requested minimum severity. Format-side filtering lands per format; today this reason is only emitted by formats that opt in (M9.4+). |

`dedupe-evicted` and `vendor-collapsed` are **not** drop reasons in
the explain log: the events emerged in a collapsed form, and the
counts are derived from the emitted event's fields. They appear
inline on `kept` lines, not as `dropped:` entries.

## Implementation

The explain path uses an instrumented variant of the standard
pipeline:

- `pipeline.BuildExplain` wires `CollapseStage` and `DedupeStage`
  identically to `pipeline.Build`. The only swap is that
  `BudgetStage` is replaced by `pipeline.ExplainingBudgetStage`,
  which records every drop / truncation to a shared
  `pipeline.ExplainLog` before discarding the event.
- `output.ExplainSink` consumes the emitted events, renders the
  `kept` lines inline, and dumps the log entries as `dropped:`
  lines after the input channel closes.

The non-explain code path (`Build`) is untouched, so explain
instrumentation imposes zero cost on normal `run` invocations.

## Exit codes

`explain` follows the same exit-code precedence as `run`:

- `ExitOK` (0) — at least one kept or dropped line was produced.
- `ExitNoEvents` (1) — the pipeline produced no events and no drops
  (clean input).
- `ExitError` (2) — argument parsing failed, autodetect refused
  under `--strict`, or the pipeline returned an error.
- `ExitPartial` (3) — `BudgetCounters.ForcedDrops()` is true (the
  budget caused drops or truncations).

## When to use

- Debugging a confusing distillation result: `--budget` may have
  silently removed the events you cared about.
- Tuning `--severity` or `--keep-warnings` against a known
  fixture: explain mode shows what each filter level produces.
- Authoring a new format: explain mode confirms the parser's
  emissions match what the format spec expects.

## Limitations

- `severity-filter` drops are reported only when the format opts in
  to populating the explain log; today no format does.
- The explain log records events at the moment they reach the
  budget stage, so any modifications by earlier stages (collapse,
  dedupe) are reflected in the recorded title and severity.
