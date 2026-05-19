# Architectural Decision Records

This directory holds short documents recording **architectural decisions
that were made and the alternatives that were rejected**. The goal is to
keep future contributors (and future-us) from re-litigating decisions
without new information.

## Index

The full set of ADRs accepted to date. Statuses are mirrored from
each file's `**Status:**` line and pinned by
`TestADRIndex_ListsEveryADR` in the integration suite.

| ADR                                                                          | Status   | Decision                                                                                                                                                                              |
|------------------------------------------------------------------------------|----------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| [ADR-0001](./0001-reject-cgo-tree-sitter-prefer-wasm.md)                     | Accepted | Reject CGo tree-sitter; prefer WASM tree-sitter via wazero for multi-language source-code distillation (v1.3).                                                                        |
| [ADR-0002](./0002-v1.0-scope-and-post-v1.0-roadmap.md)                       | Accepted | Lock the v1.0 scope to four runtime-failure formats (gotest, pytest, jest, generic) and two CI envelope strippers; sequence post-v1.0 themes (static analysis, MCP, code, docs, etc). |
| [ADR-0003](./0003-position-vs-rtk-and-snip.md)                               | Accepted | Position distill-ai as a Unix filter producing typed Events, NOT a proxy/wrapper of arbitrary CLI tools (the rtk / snip model). No state, no network, no per-agent hooks.             |

A new ADR adds a row to this table in the same commit. The drift
guard fails if any ADR file under `docs/decisions/` (excluding this
README) is missing from the table, or if any table row points at a
file that doesn't exist.

## When to write an ADR

Write one when:

- A decision rules out a class of approaches that someone might
  reasonably try later (e.g. "we don't use CGo").
- A decision has long-running implications for the project's shape
  (binary size, dependency surface, public API).
- A decision was made after weighing two or more concrete options,
  and the reasoning would be useful to re-read.

Don't write one for:

- Routine implementation choices.
- Decisions captured already in
  [ARCHITECTURE.md](../../ARCHITECTURE.md) (design principles, scope,
  out-of-scope list) — those are the project-level decisions.

## Format

One file per decision, named `NNNN-short-slug.md`:

```
docs/decisions/
  README.md
  0001-reject-cgo-tree-sitter-prefer-wasm.md
  0002-...
```

Each ADR follows this skeleton:

```markdown
# ADR-NNNN: Title

**Status:** Accepted | Superseded by ADR-MMMM | Rejected
**Date:** YYYY-MM-DD
**Context milestone:** M__ (or "ongoing")

## Context

What's the problem? What constraints are in play? What forced the
decision?

## Decision

What did we decide? Be specific. Name the alternative(s) we picked.

## Consequences

What follows from this decision — both the good and the bad. What's
now harder? What's now easier? What new constraints does the project
inherit?

## Alternatives considered

Each alternative gets:

- A name
- What it gives you
- Why we rejected it

This is the most important section. Future contributors will reopen
the question; this is the evidence that says "we already looked at
this."
```

## Superseding an ADR

Don't delete ADRs. Mark the old one's status as "Superseded by
ADR-NNNN" and link to the replacement. The history is the point.
