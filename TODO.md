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

## M2 — Pipeline plumbing ✅

Wire detect → parse → dedupe → collapse → budget → emit as a
goroutine pipeline with backpressure. Cross-references
[ARCHITECTURE.md § Pipeline](./ARCHITECTURE.md#pipeline).

M2 builds on M1; nothing in M2 should land before M1 ships.

### M2.1 — `internal/pipeline/pipeline.go`: orchestration skeleton ✅

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

### M2.2 — Property tests: determinism & streaming ✅

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

### M2.3 — Backpressure & goroutine safety audit ✅

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

## M3 — Format autodetection ✅

Read a sample, ask every registered format `Detect()`, pick the winner,
hand the rest of the stream to that format's `Parse()`.
Cross-references
[ARCHITECTURE.md § Autodetection](./ARCHITECTURE.md#autodetection).

### M3.1 — `internal/detect/detect.go`: detection engine ✅

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

### M3.2 — `--strict` mode ✅

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

### M3.3 — `distill-ai detect FILE` subcommand ✅

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

## M4 — Token estimation ✅

Estimate the token cost of an event's text so the budget enforcer
(M6) can pack the output to a target size. Two estimators ship: a
fast zero-dep heuristic (default) and an opt-in BPE tokenizer for
exact counts on OpenAI / Claude models.

Cross-references
[ARCHITECTURE.md § Token estimation](./ARCHITECTURE.md#token-estimation).
The asymmetric design principle — underestimating is worse than
overestimating because it can overflow the consumer's context window
— shapes both estimators: the default heuristic biases toward
overestimation with a built-in safety margin.

### M4.1 — `internal/tokens/estimate.go`: Estimator interface and heuristic ✅

- **DoD:**
  - `Estimator` interface with `Estimate(s string) int`.
  - `Heuristic` implementation: word count × 1.3 + symbol-run count,
    multiplied by a configurable safety margin (default +10%).
  - `Default()` factory returns a `Heuristic` pre-configured with the
    +10% margin.
  - Constants `WordTokenRatio = 1.3` and `DefaultSafetyMargin = 0.10`
    so the design is reviewable without re-reading the implementation.
  - Zero dependencies. Pure stdlib.
- **Tests** (`internal/tokens/estimate_test.go`):
  - `TestHeuristic_EmptyString`: returns 0.
  - `TestHeuristic_PureASCIIWords`: a known sentence has a known
    rough count within ±15%.
  - `TestHeuristic_SymbolHeavyCode`: a Go snippet with brackets,
    semicolons, and operators scores higher than its word count
    alone would suggest.
  - `TestHeuristic_OverestimatesByDefault`: feed a corpus where we
    know the actual tiktoken count, assert heuristic ≥ true count
    most of the time (i.e., safety margin works as intended).
  - `TestHeuristic_SafetyMarginZero`: with margin 0, the result
    matches the raw word+symbol count.
  - `TestHeuristic_DeterministicAcrossCalls`: same input × 100 calls
    → identical result every time.
- **Docs:**
  - Godoc on `Estimator`, `Heuristic`, `Default`,
    `WordTokenRatio`, `DefaultSafetyMargin`.
  - Update
    [ARCHITECTURE.md § Token estimation](./ARCHITECTURE.md#token-estimation)
    if the constants or shape differ from the sketch there.

### M4.2 — Throughput benchmark for Heuristic ✅

- **DoD:**
  - `BenchmarkHeuristic_Estimate` reports MB/sec via `b.SetBytes`.
  - Target: ≥ 100 MB/sec on a typical laptop (Apple M-series or
    modern x86 laptop). Lower is OK — the budget enforcer calls
    this once per event, not per byte — but the benchmark exists so
    future regressions are visible.
  - Bench runs as part of `make bench`, not the default test suite.
- **Tests:** the benchmark is the deliverable. No assertion;
  performance gates are agreed at M16 release prep.
- **Docs:**
  - Note the benchmark in
    [performance.md](./.opencode/rules/performance.md) so it joins
    the project's set of throughput targets.

### M4.3 — Tiktoken estimator (opt-in, embedded BPE) ✅

- **DoD:**
  - `Tiktoken()` factory returns an `Estimator` backed by the
    `cl100k_base` vocabulary.
  - Lazy initialisation: the BPE tables are loaded on the first
    `Estimate` call, not at process start, so the binary's cold-start
    latency budget (M16) only pays the cost when `--tokenizer=tiktoken`
    is selected.
  - Offline-only: the BPE vocab is embedded via the
    `tiktoken-go-loader` offline loader. **Zero network access** even
    on first init; this is enforced by `tiktoken.SetBpeLoader` at
    package init, before any code path can reach the default
    network-loader.
  - Adds two dependencies: `github.com/pkoukk/tiktoken-go` and
    `github.com/pkoukk/tiktoken-go-loader`. Both pure Go, MIT, no
    CGo. Justified in the commit per
    [dependencies.md](./.opencode/rules/dependencies.md).
  - Returns an `Estimator` whose error path is `init failure` only;
    once initialised, `Estimate` is infallible (returns 0 on impossible
    inputs rather than erroring, matching the `Heuristic` shape).
- **Tests** (`internal/tokens/tiktoken_test.go`):
  - `TestTiktoken_KnownCounts`: a small fixture corpus with known
    GPT-4 (cl100k_base) token counts. Exact match required.
  - `TestTiktoken_EmptyString`: returns 0.
  - `TestTiktoken_LazyInitOnce`: 100 concurrent `Estimate` calls
    succeed without a race on the init path (`sync.Once`).
  - `TestTiktoken_NoNetwork`: in-process check that
    `tiktoken.SetBpeLoader` was called with an offline loader at
    package init; if a future refactor removed the call, the
    test fails before any user ever hit a runtime download.
- **Docs:**
  - Godoc on `Tiktoken` explaining the lazy-init, embedded-vocab,
    cl100k_base scope (OpenAI exact, Claude ~95%, Llama/Gemini ~85%).
  - Update
    [ARCHITECTURE.md § Token estimation](./ARCHITECTURE.md#token-estimation)
    if the API shape differs from the sketch.
  - Add `tiktoken-go` and `tiktoken-go-loader` to
    [ARCHITECTURE.md § Dependencies](./ARCHITECTURE.md#dependencies).

### M4.4 — `Tokenizer` config option ✅

The CLI flag wiring is M8 work; this milestone just adds the option to
the shared config struct so M6 (budget enforcer) can consume it.

- **DoD:**
  - A new `Tokenizer` field on whatever shared options the pipeline
    is going to accept (TBD by M4 implementation time; could live
    on `pipeline.Pipeline` or on a new `pipeline.Options`).
  - Values: `"heuristic"` (default) and `"tiktoken"`.
  - A helper `tokens.ByName(name) (Estimator, error)` so the CLI in
    M8 can just pass the string through and get an `Estimator`.
- **Tests:**
  - `TestByName_Heuristic`, `TestByName_Tiktoken`, `TestByName_Unknown`
    in `internal/tokens/`.
- **Docs:**
  - Mention the flag in
    [ARCHITECTURE.md flag list](./ARCHITECTURE.md#flags) if it isn't
    already listed there (it is, from the original design).

### M4 exit criteria

- All four sub-items ticked.
- `make check` clean, no race hits.
- `make bench` runs the heuristic benchmark; its result is logged in
  the commit body for the future M16 reference.
- M4 milestone drift check: ARCHITECTURE token-estimation section and
  the implementation agree on constants, factory names, and the
  network-free guarantee; dependencies allow-list in ARCHITECTURE
  includes both tiktoken deps; performance.md lists the heuristic
  throughput benchmark.

---

## M5 — Event processing

Two complementary noise-reduction passes that turn the raw Event
stream into something an LLM can actually use: dedupe identical
events that fire in tight loops, and collapse vendor / runtime stack
frames that occupy space without carrying signal.

Cross-references
[ARCHITECTURE.md § Streaming behaviour](./ARCHITECTURE.md#streaming-behaviour)
(dedupe shape) and
[ARCHITECTURE.md § Pipeline](./ARCHITECTURE.md#pipeline) (where the
two stages plug in).

Both passes are pipeline `Stage` implementations; the `Pipeline`
shape from M2 does not change. Each item below lists Definition of
Done (DoD), required tests, and required doc updates per the
[alignment rule](./.opencode/rules/alignment.md).

### M5.1 — `internal/event/dedupe.go`: bounded LRU dedupe

Collapse identical Events into a single Event with `Count > 1` so a
flaky test that fires 4,000 times doesn't blow the budget.

- **DoD:**
  - `Deduper` struct holding a bounded LRU keyed by an Event's
    signature, where `Signature(Event) string` is
    `hash(Title + "\x00" + Location.File + ":" + Location.Line)`.
    A nil `Location` hashes as `Title` alone. The hash function is
    FNV-64a from `hash/fnv` — stdlib, allocation-free, sufficient
    for collision resistance at the window sizes we run at.
  - LRU implemented with a `container/list` doubly-linked list plus
    a `map[string]*list.Element`. No third-party LRU dependency; the
    pattern is ~50 lines.
  - `New(window int) *Deduper` constructs a Deduper with a fixed
    capacity. `window <= 0` is treated as "off" — the Deduper still
    satisfies the API but every Event passes through with `Count=1`.
  - `Observe(ev Event) (out Event, emit bool)` is the streaming
    entry point. On first sight of a signature: store the Event with
    its index, return `(ev with Count=1, true)`. On a duplicate
    within the window: bump the stored Event's `Count`, move it to
    the front of the LRU, return `(_, false)` so the caller drops
    the duplicate from the stream.
  - `Flush() []Event` returns all in-flight Events with their final
    `Count` values, in LRU recency order (most-recently-seen first,
    matching the order in which they were "promoted"). Used by
    batch-mode callers and by the streaming flush hook below.
  - `DedupeStage` implements `pipeline.Stage`. Each incoming Event
    is fed through `Observe`; emitted Events flow through unchanged.
    On `in` close, `DedupeStage` calls `Flush` and emits each Event
    whose `Count > 1` and whose first-emit had `Count == 1` — i.e.,
    only the *follow-up* count gets flushed, so consumers see a
    single Event per signature with the final `Count`. (This means
    the stream emits the Event eagerly with `Count=1` for latency,
    then re-emits with the corrected `Count` at flush. M5.3
    documents this two-emit pattern in the encoder contract.)
  - Concurrency: `Deduper` is **not** goroutine-safe; one
    `DedupeStage` owns one `Deduper` from a single goroutine. The
    pipeline already provides that constraint.
  - Zero dependencies beyond `container/list` and `hash/fnv`.
- **Tests** (`internal/event/dedupe_test.go`):
  - `TestDeduper_FirstSightEmits`: an Event seen once round-trips
    with `Count=1, emit=true`.
  - `TestDeduper_DuplicateBumpsCount`: same signature twice → second
    `Observe` returns `emit=false`; `Flush` reports `Count=2`.
  - `TestDeduper_DistinctTitlesDoNotCollide`: two Events with the
    same Location but different Titles emit separately.
  - `TestDeduper_DistinctLocationsDoNotCollide`: two Events with the
    same Title but different file:line emit separately.
  - `TestDeduper_NilLocationHashesAsTitleOnly`: two Events with
    `Location=nil` and identical Title collapse; one with a Title
    that happens to contain the would-be hash separator does not
    collide with anything (signature is hashed, not concatenated
    verbatim).
  - `TestDeduper_EvictionOldestOut`: window=3, observe 4 distinct
    signatures, then re-observe the first — it should re-emit as
    new (not as a duplicate) because it was evicted.
  - `TestDeduper_WindowZeroDisables`: window=0, every Event passes
    through with `emit=true, Count=1`; `Flush` returns no events.
  - `TestDeduper_FlushOrder`: LRU recency order, most-recent first.
  - `TestDedupeStage_StreamingPassthrough`: stage in a pipeline,
    feed 100 unique events, assert all 100 emerge in order with
    `Count=1`.
  - `TestDedupeStage_StreamingDeduplicates`: feed 10 distinct + 10
    of the same Event, assert 11 events emerge (10 originals +
    1 final flush carrying `Count=11` for the duplicated one).
  - `TestDedupeStage_ContextCancellation`: cancel mid-stream, stage
    drains and exits, no goroutine leak.
- **Docs:**
  - Godoc on `Deduper`, `New`, `Observe`, `Flush`, `Signature`,
    `DedupeStage`. Explain the two-emit pattern (emit eagerly with
    `Count=1`, re-emit on flush with final `Count`) on `DedupeStage`
    so encoder authors expect to see the same signature twice.
  - Update
    [ARCHITECTURE.md § Streaming behaviour](./ARCHITECTURE.md#streaming-behaviour)
    if the flush cadence or `Count` semantics differ from the
    sketch.
  - Add a `dedupe` row to the schema doc's `count` field if the
    two-emit pattern is observable on the JSON output. (Currently
    it's not — the encoder is M7 — but the schema must agree once
    M7 lands; record the expectation in `docs/formats/SCHEMA.md`.)

### M5.2 — `internal/event/collapse.go`: stack frame collapse

Mark vendor frames in a stack and collapse contiguous runs of them,
reducing 30-frame Java stacks to "3 user frames + 27 vendor
frames collapsed".

- **DoD:**
  - `VendorPattern` is a compiled regex paired with a human-readable
    label. A small package-level slice of default patterns covers:
    - **Python:** `site-packages/`, `dist-packages/`, paths under
      `/usr/lib/python\d+/`, `<frozen importlib.*>`.
    - **Node:** `node_modules/`.
    - **Go:** `GOROOT` prefix (matched as `/src/runtime/` and
      stdlib paths under `/src/<single-segment>/`), `/vendor/`,
      `pkg/mod/` for the module cache.
    - **JVM:** Function prefixes `java.`, `javax.`, `sun.`, `jdk.`,
      `org.junit.`, `org.gradle.`.
  - `Classify(frame StackFrame) bool` returns whether any default
    pattern matches `frame.File` or `frame.Function`. Pure function;
    no global state beyond the compiled regex slice.
  - `ClassifyFrames(frames []StackFrame) []StackFrame` returns a
    new slice with `Vendor` populated. Does not mutate input. (The
    parsers in M9–M12 may already populate `Vendor`; ClassifyFrames
    overwrites it. This is intentional — the collapse stage is the
    single source of truth for vendor classification, so format
    authors don't have to keep regex tables in sync.)
  - `Collapse(frames []StackFrame, keepVendor bool) (out []StackFrame, collapsed int)`:
    - With `keepVendor=true`: returns `frames` unchanged (after
      re-classification via `ClassifyFrames`), `collapsed=0`.
    - With `keepVendor=false`: walks the slice; contiguous runs of
      `Vendor=true` frames are removed entirely; `collapsed` is the
      total count removed. Leading or trailing vendor runs are
      collapsed the same as middle runs.
    - Edge cases: all-vendor stack → empty `out` and
      `collapsed=len(frames)`; all-user stack → unchanged.
  - `CollapseStage` implements `pipeline.Stage`. For each Event,
    rebuilds `Frames` via `Collapse` and sets `FramesCollapsed` to
    the returned count. Events without `Frames` pass through
    untouched. Reads `KeepVendor` from a struct field set by the
    pipeline wiring.
  - Per-pattern compile happens once at package init; runtime cost
    is O(frames × patterns), constant time per frame.
- **Tests** (`internal/event/collapse_test.go`):
  - `TestClassify_Python_SitePackages`: a Python frame from
    `/.../site-packages/requests/api.py` → `Vendor=true`.
  - `TestClassify_Python_Stdlib`: a frame from
    `/usr/lib/python3.11/json/decoder.py` → `Vendor=true`.
  - `TestClassify_Node_Modules`: a Node frame whose file contains
    `/node_modules/` → `Vendor=true`.
  - `TestClassify_Go_Stdlib`: a Go frame from `runtime/proc.go` →
    `Vendor=true`.
  - `TestClassify_Go_PkgMod`: a Go frame from
    `~/go/pkg/mod/github.com/...` → `Vendor=true`.
  - `TestClassify_JVM_JavaPrefix`: a JVM frame with
    `Function="java.util.ArrayList$Itr.next"` → `Vendor=true`.
  - `TestClassify_UserCode_NotVendor`: a user-app frame from
    `internal/api/handler.go` (or `app/views.py`, etc.) →
    `Vendor=false`.
  - `TestCollapse_MiddleVendorRun`: `[user, vendor, vendor, user]` →
    `[user, user]`, `collapsed=2`.
  - `TestCollapse_LeadingTrailingVendorRuns`: a stack that starts
    and ends with vendor runs → only the interior user frames
    survive.
  - `TestCollapse_AllVendor`: every frame vendor → empty out,
    `collapsed=len(input)`.
  - `TestCollapse_AllUser`: no vendor frames → unchanged.
  - `TestCollapse_KeepVendor`: `keepVendor=true` → `out` matches
    input modulo `Vendor` reclassification; `collapsed=0`.
  - `TestCollapseStage_EventWithoutFrames`: Event.Frames=nil passes
    through with `FramesCollapsed=0`.
  - `TestCollapseStage_KeepVendorRespected`: stage with
    `KeepVendor=true` preserves all frames and reports 0.
  - `TestCollapseStage_ContextCancellation`: cancel mid-stream,
    stage drains and exits.
- **Docs:**
  - Godoc on every exported symbol (`VendorPattern`, `Classify`,
    `ClassifyFrames`, `Collapse`, `CollapseStage`, `DefaultPatterns`).
  - Update
    [ARCHITECTURE.md § Pipeline](./ARCHITECTURE.md#pipeline) if the
    stage shape differs from the M2 sketch.
  - Add the pattern catalogue (Python / Node / Go / JVM) to a new
    `docs/formats/vendor-frames.md` so format-author docs from
    M9–M12 can link to it. Per the alignment rule, the doc lands in
    the same commit as the patterns.

### M5.3 — Wire DedupeStage and CollapseStage into Pipeline options

Connect the two stages to the pipeline's configuration so the CLI
(M8) only has to pass flag values through.

- **DoD:**
  - New `pipeline.Options` struct exposing:
    - `DedupeWindow int` (0 = off).
    - `KeepVendor bool`.
    - Existing `BufferSize int`.
  - A constructor `pipeline.Build(src Source, sink Sink, opts Options) *Pipeline`
    that returns a `Pipeline` with `[CollapseStage, DedupeStage]`
    pre-wired in the documented order (collapse first, so dedupe
    sees the final frame shape).
  - The wire order is asserted by a comment in `Build` and by
    `TestBuild_StageOrder`.
  - Existing `Pipeline{Source, Stages, Sink}` field-level
    construction still works for tests that need to substitute
    custom stages.
- **Tests** (`internal/pipeline/build_test.go`):
  - `TestBuild_DefaultsAreSafe`: zero `Options` produces a
    pipeline whose stages are no-op-equivalent (`DedupeWindow=0`
    → dedupe pass-through, `KeepVendor=false` but no frames in
    test events → collapse pass-through).
  - `TestBuild_DedupeAndCollapseChainTogether`: feed an event with
    a long stack and three duplicates; assert dedupe collapses to
    `Count=4` and the surviving Event's `FramesCollapsed > 0`.
  - `TestBuild_StageOrder`: collapse runs before dedupe so the
    dedupe signature reflects the post-collapse frame layout
    (matters for events whose Title is derived from a frame).
- **Docs:**
  - Godoc on `Options` and `Build`.
  - Update
    [ARCHITECTURE.md § Pipeline](./ARCHITECTURE.md#pipeline) with a
    note that `Build` is the supported constructor and field-level
    construction is reserved for tests.
  - README is not yet updated — flags ship in M8 — but a sentence
    in `docs/formats/SCHEMA.md` confirms that
    `count > 1` and `frames_collapsed > 0` may appear on the same
    Event after M5.

### M5 exit criteria

- All three sub-items ticked.
- `make check` clean; no race hits; no goroutine leaks.
- M5 milestone drift check: ARCHITECTURE.md pipeline section names
  the two real stages (no longer "stub"); schema doc references
  dedupe `count` and collapse `frames_collapsed` semantics; the
  new `docs/formats/vendor-frames.md` exists and is linked from
  ARCHITECTURE.
- `--dedupe-window=N` and `--keep-vendor` flags are **not** wired in
  M5; that's M8. The pipeline options exist so M8 only has to pass
  flag values through.

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
