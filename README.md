# distill-ai

Distill logs, stack traces, and test output for LLM consumption.

`distill-ai` is a Unix-pipe-native CLI that parses noisy command output —
test runs, application logs, stack traces — and emits a compact, structured
summary suitable for pasting into a chat with an AI coding agent, or for the
agent itself to consume when it runs commands via its Bash tool.

Most agent-debugging sessions waste 90%+ of input tokens on log noise:
passing tests, vendor stack frames, repeated warnings, build chatter.
`distill-ai` removes that noise before it hits the context window.

## Why

When you ask Claude Code, opencode, or any other agent to fix a failing
test, the agent typically reads the entire command output. A 50,000-line
pytest run might contain 200 useful lines. You pay for all 50,000 in input
tokens, the agent spends seconds parsing noise before reasoning, and it
often latches onto the wrong error because the real one is buried.

`distill-ai` solves this by sitting in the pipe between the command and
the agent:

```bash
pytest 2>&1 | distill-ai
```

It autodetects the format, extracts the actual failures (with relevant
context and source locations), collapses vendor stack frames, deduplicates
repeated errors, and emits a compact summary. The agent gets signal, not
noise.

## Supported formats

- `generic` (M9) — regex-driven fallback. Recognises ERROR / FATAL /
  WARN markers, Python tracebacks, Go panics, and JVM stack dumps.
  Used whenever no specific format claims the input. Documented at
  [docs/formats/generic.md](./docs/formats/generic.md).
- `gotest` (M10) — parses `go test` output: `--- FAIL:` blocks,
  package summaries, `=== RUN` headers, bare goroutine panics,
  build failures, race-detector reports, and `go test -json` mode.
  Emits `test_failure`, `panic`, `build_failure`, and
  `race_condition` Events with structured stack frames. Documented
  at [docs/formats/gotest.md](./docs/formats/gotest.md).
- `pytest` (planned, M11), `jest` (planned, M12) — test-runner formats
  with structured failure / panic / build-failure extraction.

Use `distill-ai list-formats` to see what's wired into your binary,
and `distill-ai detect FILE` to ask the autodetector which format it
picks for a given input.

## Usage

```bash
# Common case: autodetect format from stdin
pytest 2>&1 | distill-ai

# Explicit format (faster, skips detection)
pytest 2>&1 | distill-ai pytest

# Run against one or more files explicitly
distill-ai run pytest failure.log
distill-ai run failure.log         # autodetect

# Streaming
kubectl logs -f my-pod | distill-ai

# Fit output to a token budget
pytest 2>&1 | distill-ai --budget=2000

# JSON output for tooling
pytest 2>&1 | distill-ai --output=json | jq .

# Markdown output for pasting into chat
pytest 2>&1 | distill-ai --output=markdown

# Verbose: see which format the detector picked, on stderr
pytest 2>&1 | distill-ai -v

# Identify a file's format without running the full pipeline
distill-ai detect pytest-output.log
distill-ai detect -          # read stdin

# List every format the binary knows about
distill-ai list-formats

# Dry-run: see what's kept vs dropped, no distilled output
distill-ai explain pytest-output.log
distill-ai explain --budget=500 pytest-output.log  # see what budget drops

# Print version, commit, build date
distill-ai version
distill-ai --version           # same info, single line

# Shell completion (bash | zsh | fish | powershell)
source <(distill-ai completions bash)
```

### Exit codes

| Code | Meaning                                                              |
| ---- | -------------------------------------------------------------------- |
| `0`  | Success: at least one event was emitted.                             |
| `1`  | Success but no events found (input was clean).                       |
| `2`  | Error: bad flags, IO error, or autodetect failed under `--strict`.   |
| `3`  | Partial: ran successfully but dropped or truncated to fit `--budget`. |

### Integration with coding agents

The highest-leverage usage is via the agent's project instructions. Add to
your `AGENTS.md` (or `CLAUDE.md`):

```markdown
When running tests or tailing logs, pipe through distill-ai:
  pytest 2>&1 | distill-ai
  npm test 2>&1 | distill-ai
  go test ./... 2>&1 | distill-ai
  kubectl logs <pod> | distill-ai --dedupe
```

The agent will then invoke `distill-ai` automatically on every command,
keeping its context window lean across the whole session.

## Status

Early development. See [ARCHITECTURE.md](./ARCHITECTURE.md) for the design
and [AGENTS.md](./AGENTS.md) for contribution guidance.

## License

See [LICENSE](./LICENSE).
