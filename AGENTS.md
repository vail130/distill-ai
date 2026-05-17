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

## Rules

Project-wide rules live in [`.opencode/rules/`](./.opencode/rules) and
are loaded into every opencode session via `opencode.json`. Browse the
specific topic when you need it:

- **[alignment.md](./.opencode/rules/alignment.md)** — Docs + tests
  stay in the same commit as the code they describe. The most-cited
  rule in the project; read this first.
- **[code-style.md](./.opencode/rules/code-style.md)** — Go
  conventions: no blank lines in funcs, error wrapping, context order,
  comments.
- **[testing.md](./.opencode/rules/testing.md)** — How we write tests:
  golden fixtures, no mocks, property tests for determinism and
  streaming.
- **[commits.md](./.opencode/rules/commits.md)** — Commit message
  format, component prefixes, amend-vs-follow-up policy.
- **[dependencies.md](./.opencode/rules/dependencies.md)** — Default
  no. New deps need a justification in the commit message.
- **[flag-policy.md](./.opencode/rules/flag-policy.md)** — Adding a
  CLI flag is a one-way door. Check the questions before doing it.
- **[performance.md](./.opencode/rules/performance.md)** — Throughput,
  latency, memory, and binary-size budgets. Regressions need
  justification.
- **[output-stability.md](./.opencode/rules/output-stability.md)** —
  JSON schema is a public API; breaking changes need a version bump.

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
