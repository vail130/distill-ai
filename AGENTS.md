# AGENTS.md

Guidance for AI coding agents (and humans) working on `distill-ai`.

Read [ARCHITECTURE.md](./ARCHITECTURE.md) before making non-trivial
changes. The design decisions there are deliberate and most of them
have already-rejected alternatives behind them.

## Project shape

- **Language:** Go.
- **Binary:** single static binary, `cmd/distill-ai/`.
- **Purpose:** Unix filter. stdin → distilled stdout. No network, no
  state, no daemon.
- **Consumers:** humans piping command output before pasting into
  chat, and AI coding agents invoking it via their Bash tool.

## What this tool is, and is not

It **is** a format-aware log/test/stack-trace summariser optimised for
LLM context windows.

It is **not**:

- A general log viewer (use `lnav`).
- A log shipper or aggregator.
- A syntax highlighter.
- A regex engine for end users.
- A networked service.

If a change pulls the tool toward any of those, it's the wrong change.

## Project layout for AI assets

Two kinds of AI assets live in this repo. They are placed at the
top level so every agent — opencode, Claude Code, Cursor, Gemini,
Codex, or a script with no agent — can find them at predictable
paths without hardcoded knowledge of a specific tool's conventions.

| Directory                      | Purpose                                                              | Audience                                         |
| ------------------------------ | -------------------------------------------------------------------- | ------------------------------------------------ |
| [`rules/`](./rules)            | Project-wide rules. Loaded as system instructions every session.     | Anyone working *on* distill-ai (this repo).      |
| [`skills/distill-ai-dev/`](./skills/distill-ai-dev) | Dogfooding skill. How to use the locally built binary on this repo's own command output, debug parsers, keep the CLI-surface manifest aligned. | Anyone working *on* distill-ai (this repo).      |
| [`skills/distill-ai/`](./skills/distill-ai) | Consumer skill. How to pipe noisy command output through `distill-ai` from any agent's Bash tool. Reusable outside this repo. | Anyone *using* `distill-ai` as a downstream tool. |

The two skills are deliberately separate. `distill-ai-dev` references
milestones, internal file paths, and the `./bin/distill-ai` build
output; `distill-ai` is agent-agnostic and assumes only that the
binary is on `PATH`.

### How agents pick them up

- **Rules.** `opencode.json` has `instructions: ["rules/*.md"]`, so
  opencode loads every rule into the session prompt automatically.
  Other agents should read this AGENTS.md and follow the per-topic
  links in the section below.
- **Skills.** Opencode auto-discovers skills under `.opencode/skills/`.
  To keep skills at the top level *and* auto-load in opencode, this
  repo ships a symlink: `.opencode/skills → ../skills`. The symlink is
  checked into git. Don't replace it with a copy.
- **Other agents.** Most agents (Claude Code, Cursor, Codex, Gemini,
  etc.) read this `AGENTS.md` at session start. If you're writing an
  agent that doesn't, point it at `rules/*.md` for rules and
  `skills/*/SKILL.md` for skills.

## Rules

Project-wide rules live in [`rules/`](./rules). Browse the specific
topic when you need it:

- **[alignment.md](./rules/alignment.md)** — Docs + tests stay in the
  same commit as the code they describe. The most-cited rule in the
  project; read this first.
- **[code-style.md](./rules/code-style.md)** — Go conventions: no blank
  lines in funcs, error wrapping, context order, comments.
- **[testing.md](./rules/testing.md)** — How we write tests: golden
  fixtures, no mocks, property tests for determinism and streaming.
- **[commits.md](./rules/commits.md)** — Commit message format,
  component prefixes, amend-vs-follow-up policy.
- **[dependencies.md](./rules/dependencies.md)** — Default no. New
  deps need a justification in the commit message.
- **[flag-policy.md](./rules/flag-policy.md)** — Adding a CLI flag is
  a one-way door. Check the questions before doing it.
- **[performance.md](./rules/performance.md)** — Throughput, latency,
  memory, and binary-size budgets. Regressions need justification.
- **[output-stability.md](./rules/output-stability.md)** — JSON schema
  is a public API; breaking changes need a version bump.

For humans, the same rules are reachable via [CONTRIBUTING.md](./CONTRIBUTING.md).

## Adding a new format

See [CONTRIBUTING.md § Adding a format](./CONTRIBUTING.md#adding-a-format)
for the contributor workflow, including the minimum-fixture requirement
and the per-format doc obligation.

## Known issues

[`KNOWN_ISSUES.md`](./KNOWN_ISSUES.md) tracks drift between the
interface specs (ARCHITECTURE.md, SCHEMA.md, godoc, scoped TODO
items) and the implementation or scoped plan. Read it before
starting work on any open milestone — several issues are scoped to
land inside specific sub-items (e.g., the `--severity` /
`--keep-warnings` CLI plumbing lands inside M9.4; `--max-events` and
`--passthrough` ownership lands inside a proposed M9.6) and the
milestone exit criteria assume those fixes are bundled in.

## When in doubt

- Re-read the design principles in [ARCHITECTURE.md](./ARCHITECTURE.md#design-principles).
- If a feature request doesn't fit them, push back rather than bend
  the design.
- Prefer fewer features, better executed. The value of this tool is
  in what it *doesn't* include as much as what it does.
