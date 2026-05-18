# ADR-0003: Position vs. rtk and snip

**Status:** Accepted
**Date:** 2026-05-17
**Context milestone:** Ongoing (post-M11 scoping)

## Context

Two existing tools occupy the adjacent niche of "reduce LLM token
consumption from command output":

- **[rtk](https://github.com/rtk-ai/rtk)** (Rust Token Killer) —
  Rust, 49.4k stars, 100+ hand-tuned filters, integrated with 13
  AI coding agents via auto-rewrite hooks. The dominant
  general-purpose solution.
- **[snip](https://github.com/edouard-claude/snip)** (Go, ~240
  stars) — explicitly an rtk alternative. Same proxy/wrapper
  model. Its differentiation is "filters are YAML data, not
  compiled code": 126 declarative filters + 19 composable pipeline
  actions.

Both tools answer the same question with the same architecture:
**they are CLI proxies.** The agent invokes `rtk git status` or
`snip git status` (manually, via shell alias, or transparently via
an agent hook). The proxy forks the underlying command, captures
its output, applies a filter, and emits the compressed version.

distill-ai answers a different question with a different
architecture:

- **CLI position:** Unix filter, not a proxy. `pytest 2>&1 |
  distill-ai`. Never forks a command. Reads from stdin or a file.
- **Output:** versioned, structured Events (`docs/formats/SCHEMA.md`),
  not compressed text. JSON shape is a public API. Format,
  severity, location, frames, count, truncated, metadata —
  reasoned about as data.
- **State:** none. ARCHITECTURE.md design principle #7: "No
  network. Ever." No SQLite tracking, no telemetry, no analytics
  dashboard.
- **Format model:** Go plugin per format with `Detect` /
  `Parse(ctx, r, opts) <-chan Event`. Streaming is a property test;
  determinism is a property test; bounded memory is verified by a
  peak-sampling test.

As of M11 the v1.0 scope (`pytest`, `jest`, `gotest`, `generic`)
covers ~70% of agent-debugging use cases but leaves rtk and snip
unchallenged on the wider shell-loop optimisation space. This ADR
records the position decision: **what distill-ai competes for, what
it does not, and which features from rtk and snip the project will
adopt** as discrete, scoped milestones (see TODO.md § v1.6).

## Decision

### distill-ai stays a Unix filter, not a proxy

The proxy/wrapper model is a different product. Implementing it
would mean forking commands, managing exit codes and stderr, and
maintaining a per-tool integration surface across 13 agents. Two
incumbents with multi-year head starts already own that lane.
distill-ai cannot win it and should not try.

What distill-ai competes for is a narrower, defensible claim:

> If you want to **programmatically reason about** test failures,
> stack traces, and structured logs — as Events with locations and
> severities, in a pipe, in a library, in a streaming context, with
> deterministic output — use distill-ai.

This claim is unoccupied. rtk's `rtk pytest` emits compressed
human-readable text ("FAILED: 2/15 tests"). distill-ai's pytest
format emits typed Events with `kind: "test_failure"`, `location:
{file, line}`, structured frames, severity ordering. Different
products. They can coexist; the worst realistic case is that
distill-ai is invoked from inside an rtk filter, which is fine.

### Design principles that do not move

The following principles are reaffirmed against the temptation to
match rtk's surface area:

1. **No network, no telemetry, no state.** (ARCHITECTURE.md
   principle #7.) rtk's opt-in aggregate telemetry produces real
   adoption signal, and we lose that signal by refusing it. The
   refusal is the point: the tool is usable in regulated industries,
   air-gapped CI, and security-sensitive contexts without
   additional review.
2. **One format = one Go plugin.** snip's "filters are data" YAML
   model is genuinely faster for contributors, but distill-ai's
   Events are typed, not strings — a YAML pipeline that emits
   structured `test_failure` Events with file:line frames is not
   meaningfully simpler than a Go parser. The fixture suite per
   format is the real contributor barrier, not the language.
3. **Streaming is a hard invariant.** rtk is fundamentally batch
   (fork → exec → capture → filter → emit). distill-ai works on
   `tail -f`, on existing log files, on CI streams piped from
   upstream — none of which rtk's model serves. This is
   distill-ai's structural advantage; keep it.
4. **Determinism is a property test, not a hope.** Every Format
   and every Sink runs `TestPipeline_Determinism` and
   `TestSinks_DeterministicForFixedInput`. Caching, golden tests,
   and agent reproducibility depend on this.

### Features to adopt from rtk and snip

Five features from the comparison are valuable enough to schedule
as discrete milestones. Each preserves the principles above; each
addresses a real gap the comparison surfaced. They land as
**v1.6 — Filter-engine parity** (see TODO.md), after v1.0–v1.5 ship
the typed-Event story.

1. **`discover` subcommand** — scan a directory tree or stdin
   stream (or a CI log archive) for log shapes distill-ai's
   existing formats would have distilled. Report the would-have-
   been savings. Stateless; reads what the user points it at.
   Direct equivalent to `rtk discover` / `snip discover` minus the
   shell-history scrape (which would require state).
2. **`--ultra-compact` output preset** — densest possible
   representation for agents that want to fit a lot in a small
   window. One line per Event: severity, kind, location, title.
   No body, no context. A preset, not a new Sink — composes with
   `--output=text|json|markdown`.
3. **Drop-side log on `--budget`** — when BudgetStage drops events
   to fit the budget, write the dropped events to a side file the
   caller can opt into reading. Mirrors rtk's "tee on failure"
   idea inverted (we keep the *dropped* content, not the *failed*
   command's full output). Exit code 3 is the current signal;
   adding a concrete artifact lets agents re-read the dropped
   detail without re-running the command.
4. **Per-agent integration recipes** — documentation, not code.
   Ship a curated set of agent-side recipes for Claude Code,
   opencode, Cursor, Copilot, Codex, Gemini: how to instruct the
   agent to invoke `distill-ai` on its command output. Mirrors
   rtk's `rtk init --agent <name>` and snip's `snip init --agent
   <name>` without becoming a proxy or shipping per-agent hooks.
   The existing opencode skill (`skills/distill-ai-dev/`)
   is the first one; the milestone formalises the pattern.
5. **TOML custom-format extension hook** — already sketched in
   ARCHITECTURE.md as `[[formats.custom.myapp]]` in
   `.distill-ai.toml` but unimplemented. Closes the breadth gap
   for long-tail tools where regex-level filtering is sufficient.
   Does not replace the format-plugin model for typed formats;
   custom formats emit Events with `Kind = "match"` and best-effort
   metadata. Lands after M14 (config) ships, so the config plumbing
   is in place.

### Features NOT to adopt

For the record, so future contributors do not re-propose them:

- **Proxy / wrapper mode.** Already covered above. Different
  product.
- **Telemetry, even opt-in.** Principle #7 is more valuable than
  the adoption signal.
- **A SQLite tracking DB / `gain` dashboard.** State is a tax. The
  user can pipe `distill-ai --output=json` to their own analytics.
- **Hand-tuned filters for 100 random commands.** Treadmill rtk
  and snip will always win. distill-ai's leverage is per-format
  depth, not per-command breadth.
- **Per-agent hook installation (`distill-ai init --agent X`).**
  That puts us in the proxy lane and forces us to maintain hook
  shapes across 13+ agents. Recipes (item 4 above) are documentation
  the agents read, not code distill-ai ships.

## Consequences

### Good

- **Position is now defensible against direct comparison.**
  Reviewers asking "why not just use rtk?" have a written answer.
- **Scope creep has a defined boundary.** The five adopted features
  are bounded; anything outside them is now a deliberate departure
  from this ADR.
- **The "no state, no network" rule is anchored against pressure to
  match rtk's analytics features.**
- **The format-plugin model is anchored against pressure to match
  snip's YAML-filter ergonomics.**

### Bad

- **The breadth gap is real and will widen.** rtk ships 100+
  commands; snip ships 126. distill-ai at v1.0 ships 4 formats.
  Most agent shell calls will not have a distill-ai format. The
  TOML custom-format hook (feature 5) is the partial mitigation.
- **Contribution barrier remains higher than snip's.** Writing a
  Go Format with fixtures and property tests is more work than
  writing a YAML filter. The format plugin doc
  (`docs/formats/<name>.md`) and the `internal/formats/testing.go`
  harness reduce the work, but don't eliminate it.
- **No adoption signal without telemetry.** The project will need
  to rely on stars, forks, downstream issues, and qualitative
  feedback rather than aggregate command-usage data.

### Now harder

- Justifying any feature that adds state, network, or telemetry.
- Justifying any feature that puts distill-ai in the command-
  execution path.

### Now easier

- Saying "no" to ad-hoc feature requests that drift toward the
  proxy model.
- Scoping v1.6 — the five features are pre-decided.

## Alternatives considered

### Alternative A: Pivot to the proxy model

**What it gives you.** Direct competition with rtk and snip on
their own terms. Auto-rewrite hooks across 13 agents. The
adoption-signal benefit of being in the shell path.

**Why we rejected it.** Two well-funded incumbents already own
this lane. rtk has 49.4k stars and multi-year integration depth;
snip has 126 declarative filters and a contribution model
distill-ai cannot match. The probability of winning is low and the
opportunity cost is the typed-Event differentiation distill-ai
already has.

### Alternative B: Add a YAML filter engine alongside the Go formats

**What it gives you.** snip-style contributor experience: drop a
YAML file in `~/.config/distill-ai/filters/` and a new format works.
Could close the breadth gap quickly.

**Why we rejected it.** Two engines doubles the maintenance
surface (test harnesses, documentation, schema, drift guards). The
typed-Event model is the differentiator; a YAML engine that emits
strings or weakly-typed Events dilutes the public API. The TOML
custom-format hook (feature 5) addresses the breadth gap without
introducing a parallel filter engine.

### Alternative C: Adopt rtk's auto-rewrite hook for one agent (opencode)

**What it gives you.** Lower-friction adoption for opencode users:
the hook rewrites `pytest` to `pytest 2>&1 | distill-ai` before
execution. Tested in a single agent first, no commitment to the
other 12.

**Why we rejected it.** The hook is the thin end of the proxy
wedge. Once it ships for one agent the request to ship it for
Claude Code, Cursor, Copilot, Codex, etc. follows. Each new agent
adds a hook surface to maintain. Per-agent recipes (feature 4
above) are documentation the agents read, which the project does
not have to maintain in lockstep with upstream hook APIs.

### Alternative D: Add opt-in telemetry like rtk's

**What it gives you.** Aggregate usage signal: which formats
matter, which fixtures need work, what real-world inputs look
like.

**Why we rejected it.** ARCHITECTURE.md principle #7 is the
project's strongest single design commitment. Once telemetry
exists — even opt-in — every downstream user who can't ship
telemetry has to verify it's off, audit the network code, and
re-verify on every release. The cost is asymmetric: the user
carries it forever, the project gets aggregate counts. Stars,
issues, and qualitative feedback are the substitute.

## Cross-references

- [ARCHITECTURE.md § Design principles](../../ARCHITECTURE.md#design-principles)
  — the seven principles this ADR reaffirms.
- [ARCHITECTURE.md § Out of scope (v1)](../../ARCHITECTURE.md#out-of-scope-v1)
  — the existing out-of-scope list; this ADR extends it implicitly.
- [TODO.md § v1.6 — Filter-engine parity](../../TODO.md#v16--filter-engine-parity-post-launch)
  — where the five adopted features live as scoped (but not yet
  scoped-in-detail) milestones.
- [ADR-0002](./0002-v1.0-scope-and-post-v1.0-roadmap.md) — the v1.0
  scope and v1.1–v1.5 roadmap this ADR appends v1.6 to.
