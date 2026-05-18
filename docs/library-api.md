# Library API (`pkg/distill`)

This document covers the Go library surface that ships at
`github.com/vail130/distill-ai/pkg/distill`. The CLI binary is the
primary delivery vehicle — most users invoke `distill-ai` directly —
but for code that wants to embed the distillation pipeline in
another Go program (a custom CLI, a long-running server, an MCP
server, an editor integration), the library API is the supported
extension point.

## When to use the library vs the binary

| Use case                                            | Path           |
| --------------------------------------------------- | -------------- |
| One-off pipe in a shell or CI script                | CLI binary     |
| Wrapping `os/exec` in another tool just to pipe through `distill-ai` | Library |
| Long-running process that distils many streams      | Library        |
| Need programmatic access to individual `Event` values | Library      |
| Tight integration with a test harness or coverage tool | Library     |
| Embedding inside an MCP server (v1.2)               | Library        |

If you're shelling out to the binary from another Go program just
to get distilled output, the library is faster, easier to error-
check, and gives you both the encoded output (via an `io.Writer`)
*and* a programmatic `Event` channel.

## Hello world

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/vail130/distill-ai/pkg/distill"
)

func main() {
    events, summary, err := distill.Distill(context.Background(), os.Stdin,
        distill.Options{Writer: os.Stdout},
    )
    if err != nil {
        log.Fatal(err)
    }
    // Drain the channel — we don't want programmatic events here,
    // just the encoded output through os.Stdout.
    for range events {
    }
    summary.Wait()
    os.Exit(distill.ExitCodeFromSummary(summary))
}
```

That program is a working drop-in for `distill-ai run` with the
default text encoder. Replace `os.Stdin` / `os.Stdout` with any
`io.Reader` / `io.Writer` to embed it in a larger system.

## Surface

The library API has one function and four types you actually
interact with:

| Symbol                  | Role                                               |
| ----------------------- | -------------------------------------------------- |
| `Distill`               | The streaming entry point.                         |
| `Options`               | Every knob, mirrored from the CLI flags.           |
| `Summary`               | Run-level counters, populated after the pipeline drains. |
| `OutputFormat`          | Typed string for the four shipped encoders.        |
| `ExitCodeFromSummary`   | Maps a `*Summary` onto the CLI's 0/1/2/3 contract. |

Plus a handful of type aliases (`Event`, `Severity`, `Location`,
`StackFrame`, `Confidence`, `Format`, `ParseOpts`) and severity
constants. Everything else under `internal/` is implementation
detail that may change without a version bump; do not import it.

The `pkg/distill/internal/orchestrator` subpackage hosts the bridge
between `Options` and `internal/pipeline`. Go's `internal/`
visibility rule keeps it unreachable from outside the `pkg/distill`
subtree — you'll see a build error if you try to import it.

## Streaming model

`Distill` returns a `<-chan Event` plus a `*Summary`. The channel
publishes every Event the pipeline emits **post-stages** (after
collapse, dedupe, budget, and max-events) but **pre-encoding**, so
the values you receive are the same Events the encoder Sink writes
to `opts.Writer`.

The channel is buffered (16 Events). A slow consumer applies
backpressure to the entire pipeline. The channel closes when the
pipeline drains — whether because EOF was reached, the context was
cancelled, or an error occurred.

Three consumption patterns:

```go
// Pattern 1: only care about the encoded output.
events, summary, err := distill.Distill(ctx, r, opts)
for range events {
}
summary.Wait()
```

```go
// Pattern 2: programmatic access to Events, ignore the Writer.
opts.Writer = io.Discard
events, summary, err := distill.Distill(ctx, r, opts)
for ev := range events {
    if ev.Severity == distill.SeverityError {
        log.Printf("error in %s: %s", ev.Location.File, ev.Title)
    }
}
summary.Wait()
```

```go
// Pattern 3: both. Write encoded output to one place, consume
// Events for routing or alerting in parallel.
events, summary, err := distill.Distill(ctx, r,
    distill.Options{Writer: encodedOutput, Format: "gotest"})
for ev := range events {
    if ev.Kind == "panic" {
        alerter.Alert(ev)
    }
}
summary.Wait()
```

The Writer is the primary deliverable; the channel is the
secondary view.

## Summary timing

`Summary` fields are valid **only after** one of:

- `summary.Wait()` returns.
- The channel returned by `summary.Done()` closes.

Reading Summary fields before either signal is a race — the
internal bookkeeping goroutine copies the orchestrator's snapshot
into the public fields after the pipeline drains, and Go's race
detector will flag the access.

The simplest correct pattern is "drain channel, then Wait":

```go
events, summary, err := distill.Distill(ctx, r, opts)
if err != nil { return err }
for range events { ... }      // drain (or consume) Events
summary.Wait()                // synchronise on Summary populate
return distill.ExitCodeFromSummary(summary)
```

Use `summary.Done()` when you want to multiplex Summary readiness
with other channels in a `select`:

```go
select {
case <-summary.Done():
    handleSummary(summary)
case <-time.After(30 * time.Second):
    log.Print("distill timed out")
case <-ctx.Done():
    return ctx.Err()
}
```

## Error model

`Distill` returns a non-nil `error` only for **setup failures**:

| Error                          | Cause                                   |
| ------------------------------ | --------------------------------------- |
| `ErrNilWriter`                 | `Options.Writer` is nil.                |
| `ErrUnknownTokenizer`          | `Options.Tokenizer` is not "heuristic" or "tiktoken". |
| `ErrUnknownOutput`             | `Options.Output` is not one of the documented `OutputFormat` constants. |
| `ErrUnknownFormat`             | `Options.Format` names a format not in the registry. |
| `ErrUnknownStripEnvelope`      | `Options.StripEnvelope` names a stripper not in the registry. |

Use `errors.Is` to test for any of these. On a non-nil error,
the channel and Summary are nil; nothing to drain, nothing to
clean up.

Mid-stream parser problems do **not** surface as a Distill return
value. Per the project's resolution in
[KNOWN_ISSUES.md § 1](../KNOWN_ISSUES.md), parsers convert
recoverable issues into best-effort Events and continue. Callers
that want to detect such degradations should inspect the Summary
or parse `OutputJSONStreaming` and look at the trailer's
`exit_code` field.

## Exit-code mapping

Most binaries built on top of the library want the CLI's
exit-code semantics. The helper does the right thing:

```go
events, summary, err := distill.Distill(ctx, r, opts)
if err != nil {
    return distill.ExitError    // 2 — setup failure
}
for range events { }
summary.Wait()
return distill.ExitCodeFromSummary(summary)
```

| Code | Constant         | Meaning                                       |
| ---- | ---------------- | --------------------------------------------- |
| 0    | `ExitOK`         | Pipeline ran cleanly; ≥1 Event emitted.       |
| 1    | `ExitNoEvents`   | Pipeline ran cleanly; 0 Events emitted.       |
| 2    | `ExitError`      | Setup failure; Distill returned non-nil error. |
| 3    | `ExitPartial`    | BudgetStage forced drops or truncations.      |

`ExitPartial` wins over `ExitNoEvents` — a budget that drops every
Event is meaningfully different from clean input that happened to
have no failures.

## Config files

**The library API does not load `.distill-ai.toml`.** Config files
are a CLI concern. A library caller composes its own `Options`
struct; if you want config-file support in your library wrapper,
you implement that wrapper yourself (read TOML, merge with CLI-
style overrides, populate `Options`). The
[`config` package](../internal/config/) is `internal/` and not
importable from outside the project.

This is deliberate: a library shouldn't take ownership of "where
does the user's config live" on behalf of its consumer. The CLI
binary makes those choices; another binary built on top of the
library may want different rules (project-rooted vs user-rooted,
env vars, etc.) and should be free to make them.

## Migrating from `os/exec` of the binary

If you're currently shelling out to `distill-ai`:

```go
// Before: os/exec
cmd := exec.Command("distill-ai", "run", "--format=gotest")
cmd.Stdin = testOutput
cmd.Stdout = distilled
cmd.Stderr = os.Stderr
if err := cmd.Run(); err != nil { ... }
```

The library equivalent removes the fork+exec, the PATH-resolution
dance, and the stderr-parsing-for-errors fragility:

```go
// After: pkg/distill
events, summary, err := distill.Distill(context.Background(), testOutput,
    distill.Options{Writer: distilled, Format: "gotest"},
)
if err != nil {
    return err
}
for range events { }
summary.Wait()
if !ok { /* whatever your error handling is */ }
```

The benefits:

- No subprocess. Saves a fork+exec per invocation; matters at
  high call rates.
- Errors are typed and `errors.Is`-able. No grep-stderr-for-
  patterns.
- Programmatic access to individual Events. The CLI gives you
  encoded bytes only; the library gives you the structured
  Events.
- Cancellation via `context.Context` is first-class. No SIGKILL
  required.

## Versioning

The public surface follows Semantic Versioning. Breaking changes
require a major-version bump (e.g., `v1` → `v2`). Additive changes
(new fields on `Options`, new `OutputFormat` constants, new
`Summary` counters) do **not** bump major; consumers should ignore
unknown fields and tolerate new constants.

Pin to a major version in your `go.mod`:

```
require github.com/vail130/distill-ai v1
```

Review [CHANGELOG.md](../CHANGELOG.md) before bumping; the
`Unreleased` and `[1.x.0]` sections call out anything that
affects the library API explicitly.

## Format and envelope registration

Importing `pkg/distill` brings the v1 format set (`generic`,
`gotest`, `jest`, `pytest`) and envelope strippers
(`github-actions`, `gitlab-ci`) into the global registry via
side-effect imports in `pkg/distill/register.go`. You get the
same default behaviour as the CLI without enumerating each
package.

If you want a stripped-down format set (a smaller binary, faster
startup, no Python dependency for the pytest format's regex
catalogue), build your own binary that imports only the formats
you need:

```go
package main

import (
    _ "github.com/vail130/distill-ai/internal/formats/gotest" // gotest only
    "github.com/vail130/distill-ai/pkg/distill"
)
```

Note that `internal/formats/...` packages are `internal/`-scoped
relative to the `distill-ai` module — Go will accept the import
from your binary because your binary is part of the same module
boundary, but it would not work from a third-party module. This
is the supported escape hatch for callers willing to manage their
own format set; the supported default is "import `pkg/distill`
and get everything."

## See also

- [README.md § Embedding in Go](../README.md#embedding-in-go) — the
  one-line pitch and a minimal example.
- [ARCHITECTURE.md § Library API](../ARCHITECTURE.md#package-layout) —
  the design intent.
- [CHANGELOG.md](../CHANGELOG.md) — every API change tracked here.
- [docs/formats/SCHEMA.md](./formats/SCHEMA.md) — the Event and
  Summary JSON schemas (also the shape of the Go structs).
