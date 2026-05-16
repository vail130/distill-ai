# AGENTS.md

Guidance for AI coding agents (and humans) working on `distill-ai`.

Read [ARCHITECTURE.md](./ARCHITECTURE.md) before making non-trivial
changes. The design decisions there are deliberate and most of them have
already-rejected alternatives behind them.

## Project shape

- **Language:** Go.
- **Binary:** single static binary, `cmd/distill-ai/`.
- **Purpose:** Unix filter. stdin → distilled stdout. No network, no
  state, no daemon.
- **Consumers:** humans piping command output before pasting into chat,
  and AI coding agents invoking it via their Bash tool.

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

## Code style

### Go conventions

- No blank lines inside functions.
- Struct and map literals: one field per line.
- Comments: short, single-line where possible. Explain *why*, not *what*.
  If a comment needs a paragraph, the code probably needs a rename or a
  refactor instead.
- Errors wrap with `fmt.Errorf("context: %w", err)`. No bare `return err`
  at API boundaries.
- No `panic` outside of `init()` or genuine programmer errors.
- `context.Context` is the first parameter of any function that does I/O
  or spawns goroutines.

### Dependencies

Adding a dependency requires justification in the commit message. The
[dependency list in ARCHITECTURE.md](./ARCHITECTURE.md#dependencies) is
the allow-list. New entries need:

1. A concrete reason the stdlib won't do.
2. Confirmation that no existing dependency covers it.
3. Verification that it doesn't pull in CGo or transitively bloat the
   binary.

Default answer to "should I add this dep?" is **no**.

### Testing

- Every format has golden-file tests under
  `internal/formats/<name>/testdata/`.
- Pattern: `case-NN.input` + `case-NN.expected`. Test runner walks the
  directory, runs the parser on each input, diffs against expected.
- Update goldens with `go test -update ./...` (must be implemented in
  the test harness; see existing fixtures for the pattern).
- Streaming tests use a `slowReader` that emits bytes with controlled
  delay, asserting events appear before EOF.
- No mocks. Use real fixture data.
- Determinism is a property test: run twice, byte-diff the output.

### Commits

- Imperative subject, prefixed with the relevant component:
  `pytest: collapse vendor frames in tracebacks`
- Body explains *why* the change was needed, not *what* the diff shows.
- Each commit independently builds and passes tests.
- One logical change per commit. Don't bundle a format addition with a
  refactor.

## Adding a new format

1. Create `internal/formats/<name>/<name>.go`.
2. Implement the `Format` interface (`Name`, `Detect`, `Parse`).
3. Register in `init()`: `formats.Register(&Format{})`.
4. Add fixtures under `internal/formats/<name>/testdata/`. Minimum five
   cases: clean run, single failure, multiple failures, mixed
   warnings+errors, edge case (empty/truncated input).
5. Add to format list in README and ARCHITECTURE if user-facing.
6. Verify `distill-ai detect <fixture>` picks your format over `generic`.

Don't touch other formats' code. The point of the plugin model is
isolation.

## Adding a flag

Before adding a flag, answer:

1. Is there an existing flag that could be generalised instead?
2. Can it be inferred from context (autodetect, sensible default)?
3. Will an agent ever use it, or only a human?
4. Does it interact safely with `--budget` and streaming?

The current flag set is intentionally small (~15). Each addition is a
maintenance burden. Default: don't add it.

## Performance

- Streaming throughput target: ≥50 MB/sec on a single core for the
  heuristic estimator and a typical format parser.
- Cold-start latency: ≤20 ms (default), ≤120 ms with `--tokenizer=tiktoken`.
- Memory: bounded. Dedupe LRU has a configurable cap; no unbounded
  buffers anywhere.

If a change regresses any of these, the commit message must justify it.

## Output stability

The `json` output schema is a public API. Breaking changes require:

1. A version bump in the schema (`"schema_version": 2`).
2. A deprecation period for the old schema.
3. Migration notes in the changelog.

The `text` output is human-targeted and can evolve more freely, but
golden tests will catch unintended changes. Update the goldens
deliberately, not reflexively.

## When in doubt

- Re-read the design principles in [ARCHITECTURE.md](./ARCHITECTURE.md#design-principles).
- If a feature request doesn't fit them, push back rather than bend the
  design.
- Prefer fewer features, better executed. The value of this tool is in
  what it *doesn't* include as much as what it does.
