# Architectural Decision Records

This directory holds short documents recording **architectural decisions
that were made and the alternatives that were rejected**. The goal is to
keep future contributors (and future-us) from re-litigating decisions
without new information.

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
