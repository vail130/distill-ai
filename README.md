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

## Usage

```bash
# Common case: autodetect format from stdin
pytest 2>&1 | distill-ai

# Explicit format (faster, skips detection)
pytest 2>&1 | distill-ai pytest

# Streaming
kubectl logs -f my-pod | distill-ai k8s

# Fit output to a token budget
pytest 2>&1 | distill-ai --budget=2000

# JSON output for tooling
pytest 2>&1 | distill-ai --output=json | jq .

# Markdown output for pasting into chat
pytest 2>&1 | distill-ai --output=markdown
```

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
