# ADR-0001: Reject CGo tree-sitter; prefer WASM via wazero

**Status:** Accepted
**Date:** 2026-05-16
**Context milestone:** M17 / M18 (source-code distillation, pre-implementation)

## Context

Code distillation (M17 onward) requires parsing source code across
multiple languages — at minimum Go, Python, TypeScript, JavaScript,
Rust. The natural tool for this is [tree-sitter][ts]: it parses
incrementally, recovers gracefully from syntax errors (critical for
agent-supplied partial code), and has community grammars for ~150
languages.

Tree-sitter is a C library. Using it from Go means choosing one of
several integration paths, each with consequences for distill-ai's
hard constraints:

- **No network** (ARCHITECTURE.md design principle).
- **Single static binary** distributed via `goreleaser` and `go install`.
- **Cross-compile** linux/darwin/windows × amd64/arm64 in CI.
- **Binary size ≤ 6 MB** stripped (M16 v1.0 target).
- **`CGO_ENABLED=0`** in `.goreleaser.yaml` for portability.
- **No CGo or heavyweight dependencies** (AGENTS.md → dependencies rule).

The choice is not "tree-sitter or no tree-sitter" — it's "if
tree-sitter, how?"

[ts]: https://tree-sitter.github.io/tree-sitter/

## Decision

For multi-language code distillation (M18 and later), use **WASM
tree-sitter grammars run via [wazero][wz]**, a pure-Go WASM runtime.

For Go-only code distillation (M17), use the standard library's
`go/parser` and `go/ast`. No new dependencies. This is the first thing
shipped and lets us dogfood the code-distillation pipeline on this
repository before committing to the multi-language path.

[wz]: https://wazero.io/

## Consequences

**Positive:**

- Static binary stays static. No CGo, no libc concerns, no
  cross-compilation toolchain per target.
- `go install github.com/vail130/distill-ai/cmd/distill-ai@vX.Y.Z`
  keeps working.
- `goreleaser` config does not change.
- Cross-platform support is automatic — WASM is the same everywhere.
- Tree-sitter's incremental parsing and error recovery transfer to
  WASM grammars; partial / malformed code still parses.

**Negative:**

- WASM tree-sitter is **2–3× slower** than native. For an agent-side
  tool processing files on demand this is acceptable; for batch
  whole-repo indexing it may not be. Re-evaluate if a batch-indexing
  use case emerges.
- Each grammar `.wasm` is **0.5–2 MB**. Five embedded grammars would
  push the binary well past the 6 MB v1.0 budget. M18 must therefore
  either:
  1. Embed a small default set (Go-only at first, then 1–2 most-used
     languages), and lazy-fetch other grammars on first use to
     `~/.cache/distill-ai/grammars/` — **but** the lazy-fetch path
     touches the network and the project has a "no network ever" hard
     rule. So lazy-fetch is **not** an option without an ADR
     superseding the network rule.
  2. Embed all supported grammars and revise the binary size budget
     upward (e.g. ≤ 15 MB) for the code-distillation feature, OR
  3. Ship `distill-ai-code` as a separately-distributed binary so the
     core `distill-ai` binary keeps the 6 MB budget. Users who don't
     need code distillation don't download the grammars.
  The choice between (2) and (3) is deferred to M18 scoping.
- `wazero` adds a dependency. It is well-maintained (Tetrate),
  CGo-free, pure-Go, and on its own merits passes the
  [dependency policy](../../.opencode/rules/dependencies.md). Confirm
  at M18 time.

## Alternatives considered

### CGo tree-sitter (`github.com/tree-sitter/go-tree-sitter`)

What it gives: native speed (~2–3× faster than WASM), the
canonical integration, the smallest grammar binary footprint
because compiled C is denser than WASM.

Why rejected: requires `CGO_ENABLED=1`. This is in direct conflict
with the project's static-binary constraint, breaks
`go install`, makes cross-compilation in `goreleaser` painful
(need cross-compilers + libc per target), and pulls musl/glibc
concerns into the release pipeline. The performance win is not
worth losing portability for a tool whose users may install it
on any of six platform/arch combinations from a single artifact.

### Stdlib parser per language

What it gives: pure Go, no new deps, smallest possible binary
delta per language.

Why rejected (for multi-language): Go has a native parser; Python,
TypeScript, JavaScript, and Rust do not have well-maintained pure-Go
parsers. Each non-Go language would be weeks of bespoke work, with
inconsistent error-recovery behaviour and no shared query language.
For multi-language distillation the per-language cost is
prohibitive. **For Go only (M17), this is the right choice** and
that's what M17 uses.

### Subprocess to external tools (`gopls`, `pyright`, `tsc --noEmit`)

What it gives: best-in-class parsing per language, no parser code
to maintain in distill-ai.

Why rejected: too slow (process spawn per file), too fragile
(version drift across user environments), and requires the user
have each tool installed. Violates the "Just Works" principle that
makes distill-ai useful in an agent's hot path.

### Pure-Go parsers from third-party projects

What it gives: pure Go, no CGo.

Why rejected: quality varies dramatically across languages.
Partial AST coverage for most. No unifying query language, so each
language needs its own extraction logic. Maintenance burden over
time as upstream projects evolve.

## Re-evaluation criteria

This decision should be revisited if any of the following changes:

- `wazero` performance improves to within 1.5× of native
  tree-sitter (currently 2–3× behind).
- Tree-sitter publishes pure-Go bindings (unlikely; the C
  implementation is the canonical one).
- The project explicitly drops the static-binary constraint (would
  itself need an ADR).
- A WASM grammar registry / lazy-load protocol is standardised that
  doesn't require ad-hoc network fetches (could open option 1 above).
