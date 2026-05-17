# Known issues

Tracked drift between the interface specifications (ARCHITECTURE.md,
SCHEMA.md, godoc, scoped milestones in TODO.md) and the implementation
or scoped plan. None of these block the current milestone; each one
has a recommended landing point so the fix isn't lost.

Format: one issue per heading, with **Observed**, **Why it matters**,
**Owning milestone**, and **Recommendation**. Tick the issue off by
deleting it once the recommendation lands.

## 1. `--max-events` and `--passthrough` have no owning milestone

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

## 2. `Source` interface mid-stream error contract is broken

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
