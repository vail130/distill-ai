# Known issues

Tracked drift between the interface specifications (ARCHITECTURE.md,
SCHEMA.md, godoc, scoped milestones in TODO.md) and the implementation
or scoped plan. None of these block the current milestone; each one
has a recommended landing point so the fix isn't lost.

Format: one issue per heading, with **Observed**, **Why it matters**,
**Owning milestone**, and **Recommendation**. Tick the issue off by
deleting it once the recommendation lands.

## 1. `generic` kind values disagree between SCHEMA.md and M9.2

**Observed.** [`docs/formats/SCHEMA.md`](./docs/formats/SCHEMA.md) line
114 lists generic kinds as `error, warning, exception, panic`.
[`TODO.md`](./TODO.md) M9.2's DoD lists `error_line, warning_line,
traceback, panic, exception`. The two sets don't match (`error` vs
`error_line`, `warning` vs `warning_line`, and `traceback` is missing
from the schema entirely).

**Why it matters.** Schema-driven JSON consumers route on `kind`. If
the parser ships M9.2's names and SCHEMA.md retains the older ones,
every consumer that uses the documented vocabulary breaks silently
(unknown kinds → dropped events). M9.5's exit criteria says "the
parser's kinds match SCHEMA.md" but doesn't say which side gives.

**Owning milestone.** M9.5.

**Recommendation.** Adopt the M9.2 names (they're more descriptive —
`error_line` distinguishes the per-line marker from a format's
structured `test_error`). Add an explicit M9.5 DoD bullet: "update
SCHEMA.md's generic kind list to match M9.2 (`error_line`,
`warning_line`, `traceback`, `panic`, `exception`); landed in the
same commit that registers the parser."

## 2. `events_truncated` is unwired even though M6 produces it

**Observed.** `BudgetCounters` in
[`internal/pipeline/budget.go`](./internal/pipeline/budget.go)
populates `EventsTruncated`. M6.3's `ForcedDrops()` returns true on
truncations *or* drops, and exit code 3 fires on either. But
[`SCHEMA.md` § Summary field reference](./docs/formats/SCHEMA.md#field-reference)
(lines 138–149) only lists `events_dropped_budget`. Truncations are
silently folded into the drop count (or omitted from the summary
entirely — verify against `JSONSink` output).

**Why it matters.** JSON consumers can't distinguish "the body was
shortened to fit" (still useful, content preserved) from "the event
was dropped entirely" (information loss). The two cases have
different operational meanings — a budget that truncates a lot but
drops nothing is well-tuned; a budget that drops a lot is too tight.

**Owning milestone.** Backfill. Cheap and additive — bump in the
next docs-and-tests commit.

**Recommendation.** Add `events_truncated` to SCHEMA.md as an
additive field (no `schema_version` bump per the additive-change
rule), wire `JSONSink` to emit it from `BudgetCounters.EventsTruncated`,
and extend `TestSinks_FooterReflectsCounters` to cover the field.

## 3. `ParseOpts` is missing fields M8 already accepts on the CLI

**Observed.** Today's
[`internal/formats/format.go`](./internal/formats/format.go) `ParseOpts`
carries `ContextLines` and `KeepVendor` only. The `run` and `explain`
help text registers `--max-events`, `--keep-warnings`, `--severity`,
`--passthrough`, `--context` with "Plumbing lands in M8.2.x" notes.
The follow-up commits never landed; the flags are accepted but inert.

M9.4 plans to add `MinSeverity` and `KeepWarnings` to `ParseOpts`
but doesn't acknowledge the existing CLI-side gap. Post-M9 the
generic parser will honour those fields when called via the library
API but the `--keep-warnings` / `--severity` CLI flags will still not
thread through.

**Why it matters.** Inert flags are worse than absent flags: they
suggest the user can control behaviour they actually can't. The
manifest in
[`SKILL.md`](./.opencode/skills/distill-output/SKILL.md) lists every
flag the binary accepts; a flag that's wired to nothing still passes
the drift-guard test and ships looking complete.

**Owning milestone.** M9.4.

**Recommendation.** Add an explicit DoD bullet to M9.4: "the
existing `--severity`, `--keep-warnings`, `--context` flags now map
into `ParseOpts.MinSeverity`, `ParseOpts.KeepWarnings`,
`ParseOpts.ContextLines` end-to-end, verified by
`TestRun_KeepWarningsEndToEnd`, `TestRun_SeverityFiltersWarnings`,
and `TestRun_ContextLinesHonoured`."

## 4. `--max-events` and `--passthrough` have no owning milestone

**Observed.** Today the binary accepts `--max-events=N` and
`--passthrough` with help text saying "Plumbing lands in M8.2.x".
No scoped milestone (M9, M10, M11) has a DoD bullet that plumbs
either flag.

**Why it matters.** A flag is a one-way door
([`flag-policy.md`](./.opencode/rules/flag-policy.md)). Once shipped
in `--help`, removing it is a breaking change. Carrying an inert
flag indefinitely is the exact failure mode the policy exists to
prevent.

**Owning milestone.** Decide. Most natural fit:

- `--max-events`: M9 (generic produces enough events to need a cap).
  Implement as a pipeline `Stage` that counts emitted events and
  closes its output channel after N. Pipeline shape stays the same.
- `--passthrough`: M9 too — `--passthrough` is "if no events found,
  emit input unchanged", which only makes sense once a real parser
  could find zero events. Pre-M9 every invocation errors before
  any parser sees the input.

**Recommendation.** Either pick M9 (with a sub-item M9.6 covering
both flags + their tests + a SCHEMA.md note for `--passthrough`
behaviour), or remove the flags from help text and the SKILL.md
manifest until they have a real plan. Don't carry them forward
silently.

## 5. No goroutine-leak property test across Sinks

**Observed.**
[`internal/pipeline/pipeline_test.go`](./internal/pipeline/pipeline_test.go)
has `TestPipeline_NoGoroutineLeak` (M2.3). The three Sinks each
have their own context-cancellation tests, but there's no cross-Sink
property test asserting every Sink's internal goroutines terminate
on context cancellation.

**Why it matters.** `JSONSink` with `Streaming=false` buffers the
whole event stream by design. If the buffer's cancellation handling
regresses, the leak surfaces only under unusual `Run` shapes — long
inputs, cancelled mid-stream — that the existing per-Sink tests don't
exercise. The cost of catching it later is debugging a hang in
production rather than a test failure in CI.

**Owning milestone.** Backfill. Cheap.

**Recommendation.** Add `TestSinks_NoGoroutineLeakOnCancellation` to
[`internal/output/property_test.go`](./internal/output/property_test.go),
parallel to the pipeline test. Iterate over `[TextSink, JSONSink
(Streaming=true), JSONSink (Streaming=false), MarkdownSink]`, feed
each one via a `SlowReader`-driven Pipeline, cancel the context
mid-stream, assert `runtime.NumGoroutine()` returns to baseline within
a short timeout.

## 6. Integration suite has no positive-distillation test

**Observed.**
[`test/integration/integration_test.go`](./test/integration/integration_test.go)
proves the binary boots, parses argv, separates stdout/stderr, and
respects the SKILL.md manifest. It does **not** prove that
`cmd | distill-ai > out.txt` ever produces a non-empty `out.txt`.

After the M10/M11/M12 reordering (gotest first, pytest second, jest
third), each format milestone's `.5` sub-item now carries an explicit
`TestBinary_<Format>EndToEndProducesOutput` bullet — M10.5 for
gotest, M11.5 for pytest, M12.5 for jest. M9.5 (generic) is the
remaining gap: its DoD covers replacing the
`TestBinary_DetectGotest...FallsThrough` assertions but doesn't
explicitly add a positive-distillation test for generic-fallback
input.

**Why it matters.** The integration suite is the only test layer
that exercises argv → cobra → run → pipeline → sink end-to-end. The
first format that ships its happy path (M9 generic) needs to be
proved at that boundary, not just in unit tests. Drift between the
pipeline assembly logic in `cmd/distill-ai/run.go` and the in-process
unit tests is the kind of bug that integration tests exist to catch.

**Owning milestone.** M9.5 (the remaining gap; M10.5/M11.5/M12.5
already carry the bullet after the reordering).

**Recommendation.** Add to M9.5: `TestBinary_GenericEndToEndProducesOutput`
in `test/integration/integration_test.go`. Feed
`test/integration/testdata/fixtures/plaintext.input` (or a new
fixture with a `ERROR:` line) via stdin, assert exit 1 (because the
generic fallback produces output but might have no high-severity
events depending on the fixture) or 0 (with events) — pick the
fixture so the expected exit code is unambiguous — and a substring of
the expected Event title appears on stdout.

## 7. `Source` interface mid-stream error contract is broken

**Observed.**
[`Source.Source(ctx)`](./internal/pipeline/pipeline.go) returns
`(<-chan Event, error)`. The contract documented on
[`FormatSource.Source`](./internal/pipeline/pipeline.go) says "Errors
from `Format.Parse` propagate directly; the caller is responsible for
draining whatever events the parser emitted before the error."
[`Format.Parse`](./internal/formats/format.go) godoc says "Callers
must drain the channel before inspecting the error."

But `Pipeline.Run` only checks the error **before** starting the
relay goroutine. Once the channel is open, any error that arrives
from `Format.Parse` later in the run is silently dropped — there is
no second error return path on the channel.

**Why it matters.** Every parser shipped today is happy with
"emit-then-close on EOF / ctx-cancel; never error mid-stream"
because pytest / jest / generic all parse text-shaped input where
errors degrade to a best-effort Event rather than a hard stop.
M10's gotest is different: the `-json` mode (M10.4) consumes a
structured JSON-per-line protocol where a mid-stream JSON parse
error or a malformed build-failure block is a genuine reason to
surface an error that's currently invisible.

**Owning milestone.** M10.4 (gotest `-json` mode handling).

**Recommendation.** Two options:

1. **Narrow the contract.** Update `Format.Parse`'s godoc to "no
   streaming errors; close the channel and return early on
   unrecoverable failure. Convert non-fatal problems to a
   best-effort Event with `Severity=SeverityError` and continue."
   Matches what every existing parser does; aligns the spec with
   reality. Zero code change. M10.4's `-json` parse errors would
   emit a best-effort `Event{Kind:"json_parse_error"}` and continue
   to the next line.
2. **Widen the contract.** Add a side-channel error return — either
   a `<-chan error` alongside the event channel, or fold the error
   into a sentinel Event with a reserved `Kind`. Code change in
   `Pipeline.Run` and every Source implementation.

The first option is the lower-cost answer and probably the right one
for v1. Defer the decision to M10.4 implementation time; pre-decide
here that the default is option 1 unless M10.4 surfaces a concrete
need for option 2.
