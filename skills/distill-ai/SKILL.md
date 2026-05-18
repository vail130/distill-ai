---
name: distill-ai
description: Pipe noisy command output through `distill-ai` before reading it or pasting it into context. Use whenever a test run, build, lint, log tail, or other command emits more than ~30 lines and the interesting failures are buried in unrelated output. Keeps the context window lean and surfaces the actual errors. Do NOT use when you specifically need the raw stream (e.g., reproducing a flaky test ordering issue, debugging stdout/stderr interleaving).
---

# distill-ai

`distill-ai` is a Unix filter that turns noisy command output — test
runs, application logs, stack traces — into a compact stream of typed
Events optimised for an LLM context window. It is stateless, offline,
and stdin → stdout.

Read this skill once per session. The pattern below applies to **every
noisy command for the rest of the session**.

## When to pipe through distill-ai

Pipe through `distill-ai` when running:

- Test runners: `pytest`, `go test`, `npm test`, `cargo test`, `jest`,
  `mocha`, `rspec`, `phpunit`, etc.
- Build tools: `make`, `cargo build`, `npm run build`, `go build`,
  `mvn`, `gradle`, `webpack`, etc.
- Linters / static analysis: `golangci-lint`, `eslint`, `ruff`,
  `mypy`, `flake8`, `clippy`, etc.
- Log tails: `kubectl logs`, `journalctl`, `docker logs`, `tail -f`.
- Anything you would otherwise paste into chat or a ticket.

## When NOT to pipe through distill-ai

Run the command bare when:

- The output is under ~30 lines. The pipeline overhead is not worth
  the saving.
- You need the **raw stream order** (debugging stdout/stderr
  interleave, reproducing a flaky test ordering, inspecting ANSI
  colour escapes).
- The command is interactive (TTY-bound) — `distill-ai` reads stdin,
  not a pty.

## Canonical invocations

```sh
# The default: autodetect format from stdin, distil to stdout.
pytest 2>&1 | distill-ai
go test ./... 2>&1 | distill-ai
npm test 2>&1 | distill-ai
cargo test 2>&1 | distill-ai

# Tail a log with dedupe (collapses identical repeated lines).
kubectl logs -f my-pod | distill-ai --dedupe

# Fit output to a hard token budget. Exit code 3 if anything was
# dropped or truncated.
pytest 2>&1 | distill-ai --budget=2000

# JSON output for programmatic consumption.
pytest 2>&1 | distill-ai --output=json | jq .

# Markdown output for pasting into chat / a ticket.
pytest 2>&1 | distill-ai --output=markdown

# See what was kept vs dropped without writing distilled output.
# Useful when a --budget is silently pruning events you expected.
distill-ai explain pytest.log
```

Always include `2>&1` for commands that write errors to stderr
(`pytest`, `go test`, build tools). Otherwise stderr bypasses the
pipe and the agent sees the noise it was trying to avoid.

## Discovery

If you do not know which format `distill-ai` will pick for an
input, ask it:

```sh
distill-ai detect failure.log     # prints format, confidence, runner-up
distill-ai list-formats           # every format this binary knows about
distill-ai --help                 # full flag and subcommand surface
distill-ai run --help             # flags accepted in pipe mode
```

`detect -` reads stdin if you don't have the input as a file.

## Exit codes

| Code | Meaning                                                              |
| ---- | -------------------------------------------------------------------- |
| `0`  | Success: at least one event emitted.                                 |
| `1`  | Success but no events found (input was clean / no signal markers).   |
| `2`  | Error: bad flags, IO error, or autodetect failed under `--strict`.   |
| `3`  | Partial: ran successfully but dropped or truncated to fit `--budget`. |

Treat exit code `1` as **good news**: the command succeeded *and*
nothing worth distilling came back. It is not a failure of
`distill-ai`.

## Embedding in Go (library use)

If the calling agent is writing a Go program, prefer importing the
library directly over shelling out to the binary. The library is
at [`pkg/distill`](https://pkg.go.dev/github.com/vail130/distill-ai/pkg/distill):

```go
import "github.com/vail130/distill-ai/pkg/distill"

events, summary, err := distill.Distill(ctx, r,
    distill.Options{Writer: os.Stdout},
)
if err != nil {
    return err
}
for ev := range events {
    // optional: programmatic access to individual Events
}
summary.Wait()
os.Exit(distill.ExitCodeFromSummary(summary))
```

- The Event channel publishes structured Events; the Writer
  receives encoded output in parallel.
- Setup errors (nil Writer, unknown format, unknown tokenizer)
  surface synchronously via the returned `error`.
- `summary.Wait()` blocks until the Summary's fields are
  populated. Reading fields before Wait returns is a race.
- `distill.ExitCodeFromSummary(summary)` reproduces the CLI's
  exit-code semantics for binaries that want to replicate them.

Use the library when:

- The wrapping program is already in Go.
- You need typed errors (rather than parsing stderr).
- You want programmatic access to individual Events for routing
  or alerting.
- You care about fork+exec cost (e.g., distilling many short
  streams in a long-running process).

See [docs/library-api.md](../../docs/library-api.md) in the source
tree for the full reference: consumption patterns, Summary
timing, the `os/exec` migration guide, and format-set
customisation.

## What to do if the distilled output looks wrong

1. **Re-run with `--output=json`** and inspect the raw Event stream.
   Each event has `severity`, `kind`, `location`, `frames`, `body`,
   and `context` fields — easier to reason about than the
   default human-readable rendering.
2. **Re-run with `explain`** to see what was dropped and why
   (`severity-filter`, `budget`, `dedupe-evicted`, `vendor-collapsed`).
3. **Compare to the bare command output**. If the distilled stream is
   genuinely missing a real failure, that's a parser bug — file an
   issue against `distill-ai` with the input that triggered it.

When in doubt, fall back to the bare command for one run, decide
whether the distillation is helping, and then resume the pipeline.

## Not a substitute for

- **Reading code.** `distill-ai` summarises *output*, not source.
- **A debugger.** It extracts events; it doesn't step through them.
- **A general log viewer.** Use `lnav` for interactive exploration.
- **A regex tool.** Use `grep` / `rg` for ad-hoc pattern matching.
