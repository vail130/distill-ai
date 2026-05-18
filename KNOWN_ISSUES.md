# Known issues

Tracked drift between the interface specifications (ARCHITECTURE.md,
SCHEMA.md, godoc, scoped milestones in TODO.md) and the implementation
or scoped plan. None of these block the current milestone; each one
has a recommended landing point so the fix isn't lost.

Format: one issue per heading, with **Observed**, **Why it matters**,
**Owning milestone**, and **Recommendation**. Tick the issue off by
deleting it once the recommendation lands.

## 1. `Source` interface mid-stream error contract is broken

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
