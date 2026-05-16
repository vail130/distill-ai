# TODO

Implementation roadmap for `distill-ai`. Tasks are grouped by milestone
and ordered roughly by dependency. Tick items as they land.

See [ARCHITECTURE.md](./ARCHITECTURE.md) for the design that drives this
list and [AGENTS.md](./AGENTS.md) for code/commit conventions.

## Scoping format

Each milestone is split into sub-items. Each sub-item has:

- **Definition of Done (DoD):** what must be true for the box to be ticked.
- **Tests:** the tests that must exist when the item lands. Per the
  [alignment rule](./.opencode/rules/alignment.md)
  these ship in the same commit as the code.
- **Docs:** the docs that must update when the item lands. Same rule.

Each milestone ends with **exit criteria** — a milestone-level drift
check before the milestone is marked complete (see
[alignment.md § Enforcement](./.opencode/rules/alignment.md#enforcement)).

Milestones M1–M3 are scoped this way today. M4–M16 will be scoped
before their respective branches open, to avoid premature detail.

---

## M0 — Project scaffolding

- [x] `go.mod` with module path `github.com/vail130/distill-ai`
- [x] Go version pin (1.26)
- [x] `cmd/distill-ai/main.go` minimal entry point
- [x] Top-level `Makefile` with `build`, `test`, `lint`, `install`, `tidy`, `bench`, `release-dry-run`
- [x] `.golangci.yml` linter config (v2 schema)
- [x] GitHub Actions: build + test + lint on push (linux/darwin/windows matrix)
- [x] Release workflow: cross-compile linux/darwin/windows × amd64/arm64 via goreleaser
- [x] `goreleaser` config for tagged releases
- [ ] Decide and document binary distribution: Homebrew tap, GitHub Releases, `go install` (deferred to M16)

---

## M1 — Core types & interfaces ✅

Foundation milestone: define the data model and plugin contract that
every later milestone consumes. Cross-references
[ARCHITECTURE.md § Core types](./ARCHITECTURE.md#core-types) and
[docs/formats/SCHEMA.md](./docs/formats/SCHEMA.md).

Each item below lists Definition of Done (DoD), required tests, and
required doc updates per the
[alignment rule](./.opencode/rules/alignment.md).

### M1.1 — `internal/event/event.go`: core types ✅

Define the data model every format emits and every encoder consumes.

- **DoD:**
  - `Event` struct with all fields from
    [ARCHITECTURE.md § Core types](./ARCHITECTURE.md#core-types) and
    JSON tags matching
    [SCHEMA.md § Event object](./docs/formats/SCHEMA.md#event-object).
  - `Severity` is a string-typed type with constants `SeverityError`,
    `SeverityWarn`, `SeverityInfo`. `String()` and `ParseSeverity(s)`
    methods total over the enum.
  - `Location` struct with `File`, `Line`, `Column`. Pointer to allow
    nil for events without source location.
  - `StackFrame` struct with `File`, `Line`, `Function`, `Vendor`.
  - `Confidence` is `float64` 0.0–1.0; constants for thresholds:
    `ConfidenceMinDetect = 0.6`.
  - `ParseOpts` struct (placeholder for now; M2/M3/M5 will populate).
  - All exported symbols have godoc.
- **Tests** (`internal/event/event_test.go`):
  - `TestSeverity_String`: every constant round-trips.
  - `TestParseSeverity`: every known string parses; unknown returns
    error.
  - `TestEvent_JSONRoundTrip`: marshal then unmarshal a fully-populated
    `Event`, byte-equal compare.
  - `TestEvent_JSONSchemaMatchesDoc`: verify every JSON tag in the
    struct matches a field in `docs/formats/SCHEMA.md` (string-match the
    doc; fails the build if the doc and struct drift).
  - `TestLocation_OptionalColumn`: zero-value `Column` marshals as
    `null` per schema.
  - `TestStackFrame_VendorBool`: round-trip with `Vendor=true` and
    `Vendor=false`.
- **Docs:**
  - Godoc on every exported symbol.
  - If field semantics differ from
    [SCHEMA.md](./docs/formats/SCHEMA.md), update the schema doc in the
    same commit.

### M1.2 — `internal/formats/format.go`: plugin interface ✅

Define the contract every format implements.

- **DoD:**
  - `Format` interface with `Name() string`, `Detect(sample []byte) Confidence`,
    `Parse(ctx context.Context, r io.Reader, opts ParseOpts) (<-chan event.Event, error)`.
  - Interface methods have godoc that doubles as the format-author
    spec: what `Detect` may assume about `sample`, what `Parse` may
    assume about `ctx`/`r`, what guarantees the channel offers
    (closed on EOF, error or context cancellation; never blocks
    indefinitely).
  - `ParseOpts` struct: re-exported / aliased from `internal/event`
    if event.ParseOpts is used, otherwise defined here. Decide once
    in this commit and stay consistent.
- **Tests** (`internal/formats/format_test.go`):
  - `TestFormat_InterfaceContract`: compile-time check that a tiny
    in-test `fakeFormat` satisfies `Format`.
  - `ExampleFormat`: runnable godoc example showing the minimum
    implementation.
- **Docs:**
  - Godoc per above.
  - Cross-link from
    [CONTRIBUTING.md § Adding a format](./CONTRIBUTING.md#adding-a-format)
    to the `Format` godoc.

### M1.3 — `internal/formats/registry.go`: format registry ✅

Thread-safe registration so formats self-register via `init()`.

- **DoD:**
  - `Register(f Format)` adds to a package-level map; panics on
    duplicate name (programmer error caught at startup, not runtime).
  - `Get(name string) (Format, bool)` lookup.
  - `All() []Format` returns a deterministically sorted snapshot
    (alphabetical by `Name`) so output ordering is reproducible.
  - Thread-safe: protected by `sync.RWMutex`.
  - Zero exported state; only the functions above.
- **Tests** (`internal/formats/registry_test.go`):
  - `TestRegistry_RegisterAndGet`.
  - `TestRegistry_DuplicateRegisterPanics`.
  - `TestRegistry_AllIsSorted`: register out of order, assert
    alphabetical.
  - `TestRegistry_AllIsSnapshot`: mutating the returned slice does not
    affect the registry.
  - `TestRegistry_ConcurrentAccess`: `go test -race` covers; explicit
    test that 100 concurrent `Get`/`All` calls don't deadlock.
- **Docs:**
  - Godoc on `Register`, `Get`, `All`.
  - Update
    [ARCHITECTURE.md § Format plugin contract](./ARCHITECTURE.md#format-plugin-contract)
    with concrete `Register()` example if the API differs from the
    sketch already there.

### M1.4 — `pkg/distill/`: stub public package ✅

Reserve the public library API surface so M14's work doesn't have to
restructure internal imports.

- **DoD:**
  - `pkg/distill/distill.go` exists with package doc.
  - Re-exports the types consumers will use:
    `Event = event.Event`, `Severity = event.Severity`,
    `Format = formats.Format`, etc. as type aliases.
  - No new exported functions yet — that's M14.
- **Tests:**
  - `pkg/distill/distill_test.go` with a compile-only test that
    imports the package and uses each re-exported type.
- **Docs:**
  - Package godoc explaining "this is the stable library API; see
    ARCHITECTURE.md § Library API".
  - Mention in
    [ARCHITECTURE.md § Library API](./ARCHITECTURE.md#package-layout)
    that `pkg/distill` exists as type aliases until M14.

### M1 exit criteria

- All four sub-items ticked.
- `make check` clean.
- M1 milestone drift check: every exported symbol in `internal/event/`,
  `internal/formats/`, `pkg/distill/` has godoc;
  `docs/formats/SCHEMA.md` field list matches `Event` struct tags;
  ARCHITECTURE.md Core Types section matches the actual types.

---

## M2 — Pipeline plumbing

Wire detect → parse → dedupe → collapse → budget → emit as a
goroutine pipeline with backpressure. Cross-references
[ARCHITECTURE.md § Pipeline](./ARCHITECTURE.md#pipeline).

M2 builds on M1; nothing in M2 should land before M1 ships.

### M2.1 — `internal/pipeline/pipeline.go`: orchestration skeleton

Define the pipeline shape with stub stages (no-op pass-through).

- **DoD:**
  - `Pipeline` struct with configurable stages.
  - `Run(ctx context.Context, r io.Reader, w io.Writer) error` is the
    main entry.
  - Each stage runs in its own goroutine, channels connect them.
  - Channel sizes are configurable (default: 16) for backpressure
    tuning.
  - `context.Context` cancellation propagates to every stage; stages
    drain in-flight events then exit.
  - Single-format mode for now (skip autodetect; that's M3).
- **Tests** (`internal/pipeline/pipeline_test.go`):
  - `TestPipeline_PassThrough`: identity format → identity encoder,
    assert output equals input.
  - `TestPipeline_ContextCancellation`: cancel mid-stream, assert
    `Run` returns `context.Canceled` and no goroutines leak (use
    `goleak`-style check or runtime.NumGoroutine snapshot).
  - `TestPipeline_StageErrorPropagates`: a stage returning an error
    cancels downstream stages and `Run` returns that error.
  - `TestPipeline_Backpressure`: slow consumer doesn't crash the
    producer; bounded memory.
- **Docs:**
  - Godoc on `Pipeline`, `Run`, each stage type.
  - Update
    [ARCHITECTURE.md § Pipeline](./ARCHITECTURE.md#pipeline) if the
    real shape differs from the sketch.

### M2.2 — Property tests: determinism & streaming

Promote the design's two big invariants to enforceable tests.

- **DoD:**
  - `TestPipeline_Determinism`: feed the same fixture twice, byte-
    compare both outputs.
  - `TestPipeline_StreamingEmitsBeforeEOF`: feed input through a
    `slowReader` that emits one chunk every 50ms; assert at least one
    event is emitted before EOF.
  - Helper `slowReader` lives in `internal/testutil/` (new package) so
    M9–M12 format tests can reuse it.
- **Tests:** the property tests above are themselves the deliverable.
- **Docs:**
  - Document `slowReader` in `internal/testutil/`.
  - Reference these tests from
    [testing.md](./.opencode/rules/testing.md).

### M2.3 — Backpressure & goroutine safety audit

Before M3 lands more stages, prove the existing skeleton doesn't leak.

- **DoD:**
  - No goroutine leak on any test (race + leak detector clean).
  - Memory bounded under adversarial input: synthetic 10GB input
    through a stub pipeline uses < 50MB resident.
- **Tests:**
  - `TestPipeline_NoGoroutineLeak` using `runtime.NumGoroutine` before
    and after `Run`.
  - `TestPipeline_BoundedMemory` (benchmark, not in normal test run):
    pipe a large synthetic stream, sample memory.
- **Docs:**
  - Add a note to
    [performance.md](./.opencode/rules/performance.md) on the bounded-
    memory invariant and how it's verified.

### M2 exit criteria

- All three sub-items ticked.
- `make check` clean, no race detector hits, no goroutine leaks.
- Pipeline can run a stub format end-to-end on a real file. Performance
  not yet measured; that's M16.

---

## M3 — Format autodetection

Read a sample, ask every registered format `Detect()`, pick the winner,
hand the rest of the stream to that format's `Parse()`.
Cross-references
[ARCHITECTURE.md § Autodetection](./ARCHITECTURE.md#autodetection).

### M3.1 — `internal/detect/detect.go`: detection engine

- **DoD:**
  - `Detect(ctx, r io.Reader) (chosen formats.Format, sample []byte, err error)`.
  - Reads first 4KB via `TeeReader` so the sample isn't consumed.
  - Calls `Detect(sample)` on every registered format in parallel
    (bounded errgroup).
  - Returns the highest-confidence format ≥ `ConfidenceMinDetect`
    (0.6). Ties broken by specificity (any format beats `generic`,
    documented in code with reasoning).
  - Sample bytes returned so the pipeline can prepend them to the
    stream before handing to `Parse`.
- **Tests** (`internal/detect/detect_test.go`):
  - `TestDetect_HighConfidenceWins`: two fake formats, asserts the
    higher one is picked.
  - `TestDetect_GenericLosesTies`: equal-confidence specific format
    beats `generic`.
  - `TestDetect_BelowThresholdFallsBackToGeneric`: max confidence
    < 0.6 → falls back to `generic`.
  - `TestDetect_EmptyInput`: empty reader returns `generic` (or
    documented error).
  - `TestDetect_BinaryInput`: random bytes don't crash any detector.
  - `TestDetect_SingleByteInput`: truncated input is handled.
  - `TestDetect_SampleNotConsumed`: bytes returned + remaining reader
    concatenate to the original input.
- **Docs:**
  - Godoc on `Detect`.
  - Update
    [ARCHITECTURE.md § Autodetection](./ARCHITECTURE.md#autodetection)
    with the concrete tie-breaking rule and the sample-size constant.

### M3.2 — `--strict` mode

- **DoD:**
  - When the detector falls back to `generic` and `--strict` is set,
    return an error that the CLI maps to exit code 2.
  - Otherwise (default) silently fall back to `generic`.
  - Flag wiring lands in M8; this milestone only adds the option to
    `DetectOpts`.
- **Tests:**
  - `TestDetect_StrictReturnsErrorOnLowConfidence`.
  - `TestDetect_NonStrictFallsBack`.
- **Docs:**
  - Add `--strict` to README's flag list (placeholder behaviour, full
    wiring in M8).
  - Add to `ARCHITECTURE.md` flag list with exit-code mapping.

### M3.3 — `distill-ai detect FILE` subcommand

Expose the detector standalone so users (and tests) can ask "what is
this?" without running a full pipeline.

- **DoD:**
  - Subcommand prints the chosen format name, confidence, sample
    bytes consumed, and runner-up format with its confidence.
  - Exit code 0 on detection ≥ threshold, exit code 1 otherwise.
- **Tests:**
  - `TestDetectCmd_PrintsExpectedFormat`: feed a known fixture, parse
    stdout.
  - `TestDetectCmd_HelpfulOutputOnUnknown`: ambiguous input still
    produces useful diagnostics.
- **Docs:**
  - README: usage example for `detect`.
  - Update `--help` text and `cmd/distill-ai` subcommand list.

### M3 exit criteria

- All three sub-items ticked.
- M3 milestone drift check: README usage examples include
  `distill-ai detect`; ARCHITECTURE.md autodetection section matches
  the code; SCHEMA.md unaffected (detection doesn't change output
  schema).
- At least two real format detectors exist as test scaffolds (won't
  ship; deleted in M9–M12 when real formats arrive).

---

## M4 — Token estimation

- [ ] `internal/tokens/estimate.go`: `Estimator` interface
- [ ] Heuristic estimator: word + symbol counting, +10% safety margin
- [ ] Heuristic estimator benchmarks (target: ≥100 MB/sec)
- [ ] Tiktoken estimator: lazy-init `cl100k_base` vocab
- [ ] `--tokenizer=heuristic|tiktoken` flag wiring
- [ ] Accuracy tests: golden corpus with known GPT-4 token counts, assert ±15% heuristic / exact tiktoken
- [ ] Document accuracy expectations in `--help` text

---

## M5 — Event processing

- [ ] `internal/event/dedupe.go`: bounded LRU keyed by `hash(title + location)`
- [ ] `--dedupe-window=N` flag wiring
- [ ] Stream-mode dedupe: emit collapsed `Count: N` periodically
- [ ] Batch-mode dedupe: post-process before emit
- [ ] `internal/event/collapse.go`: stack frame collapsing
- [ ] Vendor-frame detection: configurable patterns per language
  - Python: `site-packages/`, `dist-packages/`, stdlib paths
  - Node: `node_modules/`
  - Go: `GOROOT`, `vendor/`, `pkg/mod/`
  - Java/JVM: package prefixes (`java.`, `sun.`, `org.junit.`)
- [ ] `--keep-vendor` flag wiring
- [ ] Frame collapse tests per language

---

## M6 — Budget enforcement

- [ ] `internal/pipeline/budget.go`: greedy emit by severity until budget hit
- [ ] Single-event-exceeds-budget: truncate body, mark `truncated: true`
- [ ] Footer always emitted (~30 token reserve)
- [ ] Exit code 3 when budget forces drops
- [ ] Tests: assert output never exceeds `--budget=N` by more than estimator margin
- [ ] Tests: footer present even when all events dropped

---

## M7 — Output encoders

- [ ] `internal/output/text.go`: default compact format
- [ ] `internal/output/json.go`: stable schema; bounded → JSON, streaming → ndjson
- [ ] `internal/output/markdown.go`: headings + fenced blocks
- [ ] Footer rendering per format
- [ ] `--no-footer` flag wiring
- [ ] Schema versioning constant + tests
- [ ] Golden output tests for all three formats

---

## M8 — CLI surface

- [ ] `cmd/distill-ai/flags.go`: cobra flag definitions
- [ ] `cmd/distill-ai/run.go`: wires flags → pipeline opts
- [ ] Positional `FORMAT` + `FILE...` parsing
- [ ] Stdin/file input handling (multi-file = concatenated stream)
- [ ] Exit code mapping (0/1/2/3) per ARCHITECTURE
- [ ] `--help` text matches ARCHITECTURE flag list
- [ ] `--version` from build-time ldflags
- [ ] `-v` / `--verbose` writes pipeline diagnostics to stderr

### Subcommands

- [ ] `list-formats`: prints registered formats with version/source
- [ ] `detect FILE`: prints chosen format + confidence + runner-up
- [ ] `explain FILE`: dry-run; annotates kept/dropped/why
- [ ] `completions [bash|zsh|fish]`: generate shell completion
- [ ] `version`: build version + commit + date

---

## M9 — Generic format (fallback)

- [ ] `internal/formats/generic/generic.go`: regex-based error/warning detection
- [ ] Heuristics: lines matching `ERROR|FATAL|panic|Exception|Traceback`, severity keywords
- [ ] Context capture: N lines before/after match
- [ ] Confidence: always returns low value (loses to specific formats)
- [ ] Fixtures: 10+ cases covering mixed/unknown log shapes

---

## M10 — pytest format

- [ ] `internal/formats/pytest/pytest.go`
- [ ] `Detect`: `=== FAILURES ===`, `=== test session starts ===` markers
- [ ] Parse failure blocks: test ID, assertion, file:line, source context
- [ ] Parse error blocks (collection errors, fixture failures)
- [ ] Parse short test summary info section
- [ ] Skip passing tests entirely
- [ ] Handle `-v` and non-`-v` output shapes
- [ ] Handle `--tb=short` / `--tb=long` / `--tb=line`
- [ ] Fixtures: clean run, single fail, multi fail, errors, parametrised, xfail/xpass, warnings, collection error

---

## M11 — jest format

- [ ] `internal/formats/jest/jest.go`
- [ ] `Detect`: `●` markers, `FAIL` / `PASS` line prefixes
- [ ] Parse failure blocks: test path, description, diff, stack
- [ ] Snapshot diff handling (multi-line, structured)
- [ ] Handle `--verbose` and default output
- [ ] Coverage table suppression
- [ ] Fixtures: clean, single fail, snapshot mismatch, multiple suites, console.log noise

---

## M12 — go test format

- [ ] `internal/formats/gotest/gotest.go`
- [ ] `Detect`: `--- FAIL:`, `FAIL\t<pkg>` markers
- [ ] Parse `--- FAIL: TestName (Xs)` blocks
- [ ] Parse panic blocks (separate event kind)
- [ ] Parse build failures (separate event kind)
- [ ] Handle `-json` mode (already structured, but distill removes noise)
- [ ] Handle `-v` and non-`-v`
- [ ] Race detector output: extract race report as single event
- [ ] Fixtures: pass, single fail, panic, build error, race, subtests, table-driven

---

## M13 — Config file support

- [ ] `internal/config/config.go`: load `.distill-ai.toml` from CWD upward, then `~/.config/distill-ai/config.toml`
- [ ] Precedence: CLI flag > project config > user config > default
- [ ] Per-format config sections override format defaults
- [ ] Custom regex-based format registration via `[[formats.custom.NAME]]`
- [ ] Config validation with clear errors
- [ ] Tests: precedence, override, malformed config

---

## M14 — Library API

- [ ] `pkg/distill/distill.go`: exported `Distill(ctx, r, opts) (<-chan Event, error)`
- [ ] Stable public API; document in package godoc
- [ ] Examples in `pkg/distill/example_test.go`
- [ ] Mark internal packages as such; nothing leaks except `pkg/distill`

---

## M15 — Documentation

- [ ] `man/distill-ai.1` man page generated from cobra
- [ ] README usage examples expanded with real fixtures
- [ ] `docs/formats/` per-format docs: what's detected, what's dropped, example I/O
- [ ] `docs/integration-claude-code.md`: how to wire into Claude Code
- [ ] `docs/integration-opencode.md`: how to wire into opencode AGENTS.md
- [ ] `docs/integration-ci.md`: piping CI output through distill-ai for failure summaries
- [ ] CHANGELOG.md with semantic versioning

---

## M16 — v1.0 release prep

- [ ] All M0–M15 complete or explicitly deferred
- [ ] `go test ./...` clean, `golangci-lint run` clean
- [ ] Cross-compile verified on linux/darwin/windows × amd64/arm64
- [ ] Binary size budget: ≤6 MB stripped (with tiktoken)
- [ ] Cold-start latency budget: ≤20 ms (heuristic), ≤120 ms (tiktoken)
- [ ] Throughput budget: ≥50 MB/sec single core
- [ ] Tag `v1.0.0`, run `goreleaser`, publish

---

## v1.1 — more log / test formats (post-launch)

- [ ] `k8s` format: kubectl logs, structured + unstructured
- [ ] `json` format: generic JSON-per-line logs (Zap, slog, Bunyan, Pino)
- [ ] `npm`/`yarn`/`pnpm` install/build output
- [ ] `cargo` test/build output
- [ ] `rspec` format
- [ ] `mocha` format

> Compiler / build-error formats (rustc, tsc, gcc) live in
> [M21](#m21--compiler--build-error-formats) under v1.3 — they
> overlap with code distillation conceptually and ship in that
> sequence.

---

## v1.2 — MCP server

- [ ] `distill-ai mcp` subcommand: expose tool over MCP stdio transport
- [ ] Tool: `sift(command, format?) -> distilled_output`
- [ ] Tool: `sift_file(path, format?) -> distilled_output`
- [ ] Document setup for Claude Desktop, opencode, Continue, etc.
- [ ] Integration tests against a real MCP client

---

## v1.3 — Code distillation

Extend distill-ai from "distil logs / test output / stack traces" to
"distil source code too." Same `Event` / `Format` / pipeline machinery
as M1–M16; each language becomes a Format whose `Detect` matches
files by extension or shebang and whose `Parse` walks an AST instead
of scanning lines. New `Kind` values land in
[`docs/formats/SCHEMA.md`](./docs/formats/SCHEMA.md): `package`,
`import`, `type_def`, `func_sig`, `method_sig`, `field`, `const`.

Architectural decision recorded in
[ADR-0001](./docs/decisions/0001-reject-cgo-tree-sitter-prefer-wasm.md):
CGo tree-sitter is rejected; WASM tree-sitter via wazero is the
multi-language path. Go-only (M17) uses the stdlib first to avoid any
dependency until the design proves itself.

Each milestone below ships scoped (DoD, tests, docs) before its
branch opens, per the
[scoping convention](#scoping-format).

### M17 — Source-code distillation (Go-only)

- [ ] `internal/formats/gocode/`: Go source as a Format using
      `go/parser` from the stdlib
- [ ] New `Kind` values in SCHEMA.md and `docs/formats/gocode.md`:
      `package`, `import`, `type_def`, `func_sig`, `method_sig`,
      `const`, `var_decl`
- [ ] `--input=code` or `distill-ai code <file>` CLI surface (decide
      at scoping time)
- [ ] Dogfood: `distill-ai code ./...` produces a useful repo summary
      of this codebase
- [ ] Per-event token cost ≤ 20 tokens for a typical signature

### M18 — Multi-language code distillation (WASM tree-sitter)

- [ ] Add `wazero` dependency, justified per
      [dependencies rule](./.opencode/rules/dependencies.md)
- [ ] `internal/codeparse/`: WASM grammar loader, query helpers
- [ ] Languages: Python, TypeScript, JavaScript, Rust as Formats
- [ ] Resolve the binary-size tradeoff captured in
      [ADR-0001](./docs/decisions/0001-reject-cgo-tree-sitter-prefer-wasm.md)
      § Consequences: either revise the size budget upward for the
      single `distill-ai` binary or split a `distill-ai-code` binary
- [ ] Performance budget revisit: WASM is ~2–3× slower than native
      tree-sitter; document the floor in
      [performance rule](./.opencode/rules/performance.md)

### M19 — Agent-read wrapper

- [ ] CLI mode that takes a file/dir and emits the distilled view
      first, full content on demand
- [ ] Integrate as an MCP tool exposed via `distill-ai mcp` (M14 /
      v1.2): `read_distilled(path)` returns symbol summary;
      `read_full(path, ranges?)` returns verbatim bytes
- [ ] Document the agent-side workflow in
      `docs/integration-agent-reads.md` (how Claude Code / opencode
      can be configured to prefer the distilled read)
- [ ] Depends on M17 (Go), ideally M18 (other languages)

### M20 — AST-aware diff distillation

- [ ] Take a unified diff (or `git diff` output) and parse the
      before/after of each hunk through the relevant language Format
- [ ] Emit symbol-level `Event`s: `function Foo signature changed`,
      `import added`, `type X moved`, `method Y deleted`
- [ ] Non-code text diffs fall back to line-level distillation
- [ ] Subsumes the backlog `--diff` idea for source files; log diffs
      still use the original line-level approach
- [ ] Depends on M17/M18

### M21 — Compiler / build-error formats

- [ ] `rustc` / `cargo` output as a Format
- [ ] `tsc` output as a Format
- [ ] `go build` output as a Format (currently overlaps with `gotest`;
      decide whether to merge or split)
- [ ] `gcc` / `clang` output as a Format
- [ ] Independent of M17–M20 architecturally; this is "more formats"
      in the v1.1 sense, but listed here because compiler errors
      reference source positions and benefit from the same per-event
      structure code distillation defines

---

## Backlog (no milestone)

- [ ] Plugin loading from `~/.config/distill-ai/plugins/*.so` (Go plugins are
      fragile; evaluate WASM as alternative before committing)
- [ ] `--watch` mode: re-distill a file when it changes
- [ ] Coloured text output when stdout is a TTY (off by default; LLM consumers want plain)
- [ ] Profile-guided optimisation build
- [ ] Fuzz testing per format parser (`go test -fuzz`)
- [ ] Benchmarks in CI with regression detection
- [ ] `--diff` mode: distill two log files and show only the delta (useful for "what changed between this run and the last passing one?")
- [ ] Format-specific `--summary-only` modes (e.g., "just the failure count and titles")

---

## Explicitly out of scope

These are listed so they don't accidentally creep into the backlog:

- Interactive TUI
- Log shipping / aggregation
- Persistent caching (consider as separate sibling tool)
- Network features of any kind
- Auto-detection of target LLM model
- Built-in support for every conceivable log format (use generic + custom config)
