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

## Documentation and test alignment (hard rule)

Documentation and tests are first-class deliverables, not afterthoughts.
**Every commit that changes code must also update the corresponding docs
and tests in the same commit.** PRs that ship code without aligned
docs/tests are blocked at review, not deferred to follow-up commits.

### Doc alignment

Each kind of change has a fixed set of docs that must move with it:

| Change                                          | Doc(s) that must update in the same commit              |
|-------------------------------------------------|---------------------------------------------------------|
| New / renamed / removed exported symbol         | Godoc on the symbol; ARCHITECTURE.md if it appears there |
| New / renamed / removed CLI flag or subcommand  | README.md usage section; `--help` text in `cmd/`         |
| New / changed JSON output field or kind value   | `docs/formats/SCHEMA.md`; bump `schema_version` if breaking |
| New format added                                | README format list; ARCHITECTURE format list; `docs/formats/<name>.md` |
| Design principle bent or scope changed          | ARCHITECTURE.md design principles / out-of-scope sections |
| Public package API change in `pkg/distill/`     | godoc; `pkg/distill/example_test.go`                     |
| Performance budget changed (binary size, latency, throughput) | AGENTS.md Performance section; commit-message justification |

If a change touches code that's described elsewhere and the description
doesn't change, that's also a doc bug — the doc has drifted.

### Test alignment

- **Every new exported function or method has at least one test.** No
  exceptions for "trivial" code; trivial code still has trivial tests.
- **Every change to existing exported behaviour ships a test that fails
  before the change and passes after.** State this in the commit body
  ("Test added: TestFoo covers the new branch" or "Existing
  TestBar now covers the regression").
- **Every format ships ≥5 golden fixtures.** Clean run, single failure,
  multi-failure, mixed warnings+errors, edge case. Detailed in
  [CONTRIBUTING.md § Adding a format](./CONTRIBUTING.md#adding-a-format).
- **Determinism is a property test on every format** (same input twice
  → byte-identical output). Not optional.
- **Streaming behaviour is a property test on every format** (events
  emit incrementally, not buffered until EOF) once streaming lands in M2.

### Enforcement

- **Hard gates (CI must pass):** `go test ./...`, `go vet`,
  `golangci-lint run`. `revive`'s `exported` rule fails the build on
  undocumented exported symbols.
- **PR template checklist** explicitly requires docs + tests boxes
  ticked. Reviewers reject PRs with code changes and no corresponding
  doc / test diffs.
- **Milestone exit review:** before closing a milestone, grep
  ARCHITECTURE.md / README.md for symbol names that no longer exist,
  diff `--help` against README's usage section, and verify SCHEMA.md
  matches `--output=json` output of every format. Caught drift is
  fixed before the milestone is ticked complete.

### If you genuinely can't align in one commit

Don't open the PR. Either:

1. Defer the code change until you can write the docs / tests, or
2. Split the work into a docs-first commit ("define interface and
   document it") followed by an implementation commit ("implement the
   documented interface, with tests"). Both land in the same PR.

"I'll do the docs after" or "tests next sprint" is the failure mode
this rule exists to prevent.

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

See [Documentation and test alignment](#documentation-and-test-alignment-hard-rule)
for the rules on *when* tests are required. The points below are about
*how* we write them.

- Every format has golden-file tests under
  `internal/formats/<name>/testdata/`.
- Pattern: `case-NN.input` + `case-NN.expected`. Test runner walks the
  directory, runs the parser on each input, diffs against expected.
- Update goldens with `go test -update ./...` (must be implemented in
  the test harness; see existing fixtures for the pattern).
- Streaming tests use a `slowReader` that emits bytes with controlled
  delay, asserting events appear before EOF.
- No mocks. Use real fixture data.
- Property tests for invariants: determinism (same input twice →
  byte-identical output), streaming (events emit before EOF),
  schema-version stability (output round-trips through the documented
  JSON schema).

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
