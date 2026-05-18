# distill-ai

**Turn noisy command output into structured Events for LLM consumption.**
Unix filter. No proxy, no network, no state. JSON output is a versioned
public API.

```bash
pytest 2>&1 | distill-ai
```

`distill-ai` parses the noisy output of test runs, application logs, and
stack traces, then emits a compact stream of typed Events: severity, kind,
location, frames, body, context. Pipe it to an AI coding agent, paste it
into chat, or consume the JSON from your own tooling.

## Why

A 50,000-line `pytest` run typically contains 200 useful lines: the actual
failures with their assertion messages, file:line locations, and the
context that explains them. The other 49,800 lines are passing-test noise,
vendor stack frames, deprecation warnings, and build chatter — and a
coding agent pays for every one of those lines in input tokens, spends
seconds parsing the noise before it can reason about the real failure,
and frequently latches onto the wrong error because the real one is
buried.

`distill-ai` sits in the pipe between the command and the agent, extracts
the signal, and discards the noise:

```
  raw command output (50,000 lines, ~250,000 tokens)
                       │
                       ▼
            ┌─────────────────────┐
            │ autodetect format   │
            │ parse → Events      │
            │ collapse vendor     │
            │ dedupe identical    │
            │ enforce --budget    │
            └─────────────────────┘
                       │
                       ▼
  distilled (24 lines, ~340 tokens)
  [1] ERROR AssertionError: expected 302, got 200
    at tests/api/test_auth.py:47
  [2] ERROR KeyError: 'session_id'
    at auth/views.py:112  (×4)
  ---
  distilled 50,000 lines → 24 lines (340 tokens)
  dropped: 0 events, 3 deduped, 8 vendor frames
```

## What distill-ai is, and is not

distill-ai is a **format-aware structured event extractor** for command
output. It is **not** a general CLI proxy or command wrapper.

|                                  | distill-ai                                | proxy/wrapper tools                     |
| -------------------------------- | ----------------------------------------- | --------------------------------------- |
| **Position**                     | Unix filter — sits in a pipe              | Wraps the command (`tool git status`)   |
| **Forks the underlying command** | Never                                     | Always                                  |
| **Works on `tail -f`, log files, library use** | Yes                         | No (their model is fork-exec-capture)   |
| **Output**                       | Versioned structured Events (JSON schema) | Compressed text                         |
| **State / network / telemetry**  | None. Ever.                               | Varies                                  |
| **Format model**                 | Per-format parser emitting typed Events   | Per-command filter emitting text        |

If your problem is "my agent runs 50 different shell tools and they're
all noisy", a proxy/wrapper tool like [rtk] or [snip] is probably what
you want. If your problem is "I need an agent — or my own tooling — to
programmatically reason about test failures, stack traces, and logs as
typed events with locations and severities", distill-ai is what you
want. The two are complementary.

[rtk]: https://github.com/rtk-ai/rtk
[snip]: https://github.com/edouard-claude/snip

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
- `pytest` (M11) — parses pytest output: `=== FAILURES ===` and
  `=== ERRORS ===` blocks, the four `--tb` shapes (`long`,
  `short`, `line`, `native`), warnings summary, parametrised test
  IDs, collection-phase failures. Emits `test_failure`,
  `test_error`, `collection_error`, and `warning` Events with
  structured stack frames. Honours `--keep-warnings` and
  `--severity`. Documented at
  [docs/formats/pytest.md](./docs/formats/pytest.md).
- `jest` (planned, M12) — test-runner format with structured
  failure / snapshot-mismatch / suite-error extraction.

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

distill-ai does not ship per-agent hooks or `init` subcommands; it
deliberately stays out of the command-execution path (see
[ADR-0003](./docs/decisions/0003-position-vs-rtk-and-snip.md)). The
agent integration pattern is **documentation**: instruct the agent via
its project rules file to pipe through `distill-ai` when running noisy
commands.

Add the following block to `AGENTS.md`, `CLAUDE.md`, `.cursorrules`,
`GEMINI.md`, or your agent's equivalent project-rules file:

```markdown
When running tests, build commands, or tailing logs, pipe through
distill-ai to keep the context window lean:

  pytest 2>&1 | distill-ai
  npm test 2>&1 | distill-ai
  go test ./... 2>&1 | distill-ai
  cargo test 2>&1 | distill-ai
  kubectl logs <pod> | distill-ai --dedupe

For a strict token cap (e.g., fit a CI run in 2000 tokens):
  pytest 2>&1 | distill-ai --budget=2000

To inspect what distill-ai dropped before trusting it:
  pytest 2>&1 | distill-ai explain
```

The agent reads its rules file at session start and applies the pattern
to every applicable command for the rest of the session.

For opencode, the project ships
[`skills/distill-ai/SKILL.md`](./skills/distill-ai/SKILL.md) — a
self-contained, agent-agnostic skill that loads automatically when
output volume warrants it. It assumes only that `distill-ai` is on
`PATH` and is reusable verbatim outside this repo. A sibling
[`skills/distill-ai-dev/SKILL.md`](./skills/distill-ai-dev/SKILL.md)
covers the in-repo dogfooding loop for contributors. Per-agent
integration recipes for Claude Code, Cursor, Copilot, Codex, Gemini,
Windsurf, and Cline are planned for v1.6 (see
[TODO.md § M29](./TODO.md#m29--per-agent-integration-recipes-documentation)).

### Library use

`distill-ai` also exposes a stable Go API at `pkg/distill/` for tools
that want to consume Events programmatically rather than via the CLI.
The library API is intentionally minimal; see
[ARCHITECTURE.md § Package layout](./ARCHITECTURE.md#package-layout)
for the M14 / M15 milestones that promote it from type aliases to a
streaming entry point.

## Design principles

distill-ai's shape is deliberate. The seven principles in
[ARCHITECTURE.md § Design principles](./ARCHITECTURE.md#design-principles)
shape every decision:

1. **Unix-pipe-native.** stdin → stdout is the default path.
2. **Zero-config common case.** Autodetection picks the format.
3. **Deterministic output.** Same input → same output, byte for byte.
4. **Streaming-first.** Never require buffering the full input.
5. **Format plugins, not hardcoded formats.** Adding a format = one Go
   file. The format registers itself.
6. **Honest about what it dropped.** A footer summarises every
   collapse, dedupe, and drop.
7. **No network. Ever.** No telemetry, no updates, no remote lookups.
   The tool is usable in air-gapped CI and regulated environments
   without additional review.

See [ADR-0003](./docs/decisions/0003-position-vs-rtk-and-snip.md) for
the explicit position decision against the proxy/wrapper model.

## Status

Pre-1.0. The v1.0 contract (`pytest`, `jest`, `gotest`, `generic`) is
recorded in
[ADR-0002](./docs/decisions/0002-v1.0-scope-and-post-v1.0-roadmap.md);
see [TODO.md](./TODO.md) for the milestone status. See
[ARCHITECTURE.md](./ARCHITECTURE.md) for the design and
[AGENTS.md](./AGENTS.md) for contribution guidance.

## Inspiration and prior art

distill-ai is shaped by — and explicitly differentiates from — two
existing tools in the adjacent niche:

- **[rtk]** (Rust Token Killer, 49k+ stars). The dominant
  general-purpose solution for reducing agent token consumption. CLI
  proxy / wrapper model. 100+ hand-tuned filters. Auto-rewrite hooks
  for 13+ AI coding agents.
- **[snip]** (Go, ~240 stars). An rtk alternative whose
  differentiation is declarative YAML filters: 126 of them, composed
  from 19 pipeline actions.

Both are excellent at the breadth play — the agent-shell-loop case
where 50 different commands need filters. distill-ai is intentionally
narrower: depth-first format-aware parsing that produces typed Events
with locations and severities, suitable for programmatic reasoning by
agents or downstream tools. See
[ADR-0003](./docs/decisions/0003-position-vs-rtk-and-snip.md) for the
full comparison and the position decision.

## License

See [LICENSE](./LICENSE).
