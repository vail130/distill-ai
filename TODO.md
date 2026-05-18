# TODO

Implementation roadmap for `distill-ai`. Tasks are grouped by milestone
and ordered roughly by dependency. Tick items as they land.

See [ARCHITECTURE.md](./ARCHITECTURE.md) for the design that drives this
list, [AGENTS.md](./AGENTS.md) for code/commit conventions, and
[KNOWN_ISSUES.md](./KNOWN_ISSUES.md) for spec-vs-implementation drift
that is scoped to land inside specific milestone sub-items.

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

Milestones M1–M13 are scoped this way today, along with the three
post-v1.0 milestones M23, M24, and M25 that
[ADR-0002](./docs/decisions/0002-v1.0-scope-and-post-v1.0-roadmap.md)
introduces. Per the working agreement, **at least** the next three
open milestones are kept fully scoped at all times. As of M10's
completion, the open scoped set is M11 (pytest), M12 (jest), and
M13 (envelope). When the v1.0 release prep (M17) lands and the v1.1
branch opens, the next scoped three will be M23 (golangci-lint),
M24 (cargo-json), and M25 (Markdown outline) — already scoped below
so the v1.1 cut is ready when v1.0 ships. M14–M17 (the rest of
v1.0) and M18–M22 (v1.3 code distillation) remain sketched.

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
- [ ] Decide and document binary distribution: Homebrew tap, GitHub Releases, `go install` (deferred to M17)

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

Reserve the public library API surface so M15's work doesn't have to
restructure internal imports.

- **DoD:**
  - `pkg/distill/distill.go` exists with package doc.
  - Re-exports the types consumers will use:
    `Event = event.Event`, `Severity = event.Severity`,
    `Format = formats.Format`, etc. as type aliases.
  - No new exported functions yet — that's M15.
- **Tests:**
  - `pkg/distill/distill_test.go` with a compile-only test that
    imports the package and uses each re-exported type.
- **Docs:**
  - Package godoc explaining "this is the stable library API; see
    ARCHITECTURE.md § Library API".
  - Mention in
    [ARCHITECTURE.md § Library API](./ARCHITECTURE.md#package-layout)
    that `pkg/distill` exists as type aliases until M15.

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
  not yet measured; that's M17.

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
  performance gates are agreed at M17 release prep.
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
    latency budget (M17) only pays the cost when `--tokenizer=tiktoken`
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
  the commit body for the future M17 reference.
- M4 milestone drift check: ARCHITECTURE token-estimation section and
  the implementation agree on constants, factory names, and the
  network-free guarantee; dependencies allow-list in ARCHITECTURE
  includes both tiktoken deps; performance.md lists the heuristic
  throughput benchmark.

---

## M5 — Event processing ✅

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

### M5.1 — `internal/event/dedupe.go`: bounded LRU dedupe ✅

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
  - `Observe(ev Event) (evicted Event, hasEvicted bool)` is the
    streaming entry point. On first sight of a signature: store the
    Event with `Count=1` at the front of the LRU. If insertion
    pushes capacity over `window`, the oldest entry is evicted and
    returned with `hasEvicted=true`. On a duplicate within the
    window: bump the stored Event's `Count`, move it to the front
    of the LRU, return `hasEvicted=false`. Either way, the caller
    forwards the evicted Event (if any) downstream.
  - `Flush() []Event` returns all in-flight Events with their final
    `Count` values, in LRU recency order (oldest first, so the
    relative order in which entries first appeared is preserved).
    Used by `DedupeStage` to drain remaining entries when the
    upstream channel closes.
  - **Eviction-emit only.** The stage emits an Event downstream
    exactly once per signature: at LRU eviction, or at flush. No
    "two-emit pattern", no eager Count=1 then re-emit. Consumers
    therefore see one Event per signature with the final `Count`.
    The cost is per-event latency: an Event is delayed in the LRU
    until either `window` more distinct events arrive or the input
    closes. The `--dedupe-window=N` flag (wired in M8) controls
    that latency directly; window=0 disables dedupe entirely and
    every Event flows through unmodified with `Count=1`.
  - `DedupeStage` implements `pipeline.Stage`. For each incoming
    Event: call `Observe`; if it returned an evicted Event, forward
    that downstream. On `in` close, call `Flush` and forward each
    remaining Event downstream in the order returned.
  - Concurrency: `Deduper` is **not** goroutine-safe; one
    `DedupeStage` owns one `Deduper` from a single goroutine. The
    pipeline already provides that constraint.
  - Zero dependencies beyond `container/list` and `hash/fnv`.
- **Tests** (`internal/event/dedupe_test.go`):
  - `TestDeduper_FirstSightDoesNotEvict`: an Event seen once
    returns `hasEvicted=false`; `Flush` returns that Event with
    `Count=1`.
  - `TestDeduper_DuplicateBumpsCount`: same signature twice → both
    `Observe` calls return `hasEvicted=false`; `Flush` reports
    `Count=2`.
  - `TestDeduper_DistinctTitlesDoNotCollide`: two Events with the
    same Location but different Titles flush separately.
  - `TestDeduper_DistinctLocationsDoNotCollide`: two Events with the
    same Title but different file:line flush separately.
  - `TestDeduper_NilLocationHashesAsTitleOnly`: two Events with
    `Location=nil` and identical Title collapse; an Event whose
    Title literally contains the hash separator byte does not
    collide with a different Title/Location combination (signature
    is hashed, not concatenated verbatim).
  - `TestDeduper_EvictionEmitsOldest`: window=3, observe 4 distinct
    signatures — the 4th Observe call returns `hasEvicted=true`
    carrying the first-observed Event with `Count=1`.
  - `TestDeduper_ReObserveAfterEviction`: after an entry is evicted,
    re-observing the same signature treats it as new (its old
    `Count` is gone).
  - `TestDeduper_WindowZeroDisables`: window=0, every `Observe`
    returns `hasEvicted=true` carrying the input Event unchanged
    with `Count=1`; `Flush` returns no events.
  - `TestDeduper_FlushOrderOldestFirst`: insertion-order
    preservation; Flush returns entries in the order they first
    appeared.
  - `TestDedupeStage_PassthroughDistinct`: stage in a pipeline,
    feed 100 unique events with window=100, assert all 100 emerge
    in order with `Count=1` (none evicted in flight, all flushed).
  - `TestDedupeStage_DeduplicatesIdentical`: feed 10 distinct + 10
    copies of one of those Events, assert 10 events emerge each
    with the correct `Count` (one with `Count=11`, nine with
    `Count=1`).
  - `TestDedupeStage_ContextCancellation`: cancel mid-stream, stage
    drains and exits, no goroutine leak.
- **Docs:**
  - Godoc on `Deduper`, `New`, `Observe`, `Flush`, `Signature`,
    `DedupeStage`. Explain the eviction-emit contract on
    `DedupeStage` so encoder authors don't expect duplicate
    signatures.
  - Update
    [ARCHITECTURE.md § Streaming behaviour](./ARCHITECTURE.md#streaming-behaviour)
    so it documents eviction-emit (the existing line about
    "periodic dedupe flush every N events" is from an earlier
    design and needs updating in the same commit).

### M5.2 — `internal/event/collapse.go`: stack frame collapse ✅

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

### M5.3 — Wire DedupeStage and CollapseStage into Pipeline options ✅

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

## M6 — Budget enforcement ✅

Enforce a target output token count via `--budget=N`. The budget
stage sits at the tail of the pipeline (after Collapse and Dedupe,
before the Sink); it estimates each Event's token cost via a
`tokens.Estimator`, emits highest-severity Events first, truncates a
single Event's body when it alone exceeds the remaining budget, and
counts drops so the footer (M7) can report them. Exit code 3 is
reserved for "ran successfully but had to drop content."

Cross-references
[ARCHITECTURE.md § Budget enforcement](./ARCHITECTURE.md#budget-enforcement)
and [§ Token estimation](./ARCHITECTURE.md#token-estimation).
The asymmetric design from M4 applies: the heuristic overestimates,
so a `--budget=N` cap typically yields fewer than N real tokens.
That's deliberate — overshooting a model's context window is worse
than wasting headroom.

M6 builds on M5 (DedupeStage, CollapseStage) and M4 (`tokens.Estimator`,
`tokens.ByName`). Each item below lists Definition of Done (DoD),
required tests, and required doc updates per the
[alignment rule](./.opencode/rules/alignment.md).

### M6.1 — `internal/pipeline/budget.go`: BudgetStage with severity-priority emission ✅

Buffer events, sort by severity, emit until the budget would be
exceeded. Mark dropped events on the `Summary` (M7 work) via shared
counters carried through the stage.

- **DoD:**
  - `BudgetStage` implements `pipeline.Stage`. Configurable fields:
    - `Budget int` (target token cap; 0 means "no cap" → pass-through).
    - `Reserve int` (footer reserve; default 30, see M6.3).
    - `Estimator tokens.Estimator` (required when `Budget > 0`).
    - `Counters *BudgetCounters` (pointer to a struct the Sink reads
      after the pipeline drains; see DoD below).
  - **Eviction-emit timing.** BudgetStage is **buffered**: it reads
    its input fully before emitting, because severity-priority
    ordering can't be decided streaming. This is documented as the
    one stage that deliberately breaks streaming, justified because
    `--budget` only makes sense for bounded input. When `Budget == 0`
    the stage degrades to a streaming pass-through identical to
    `PassthroughStage`.
  - **Emission order** when `Budget > 0`: events emitted in
    descending severity (error → warn → info), then by their original
    arrival order within each severity bucket. The arrival order is
    captured by an incrementing sequence number assigned when the
    event is buffered, so the order is deterministic for identical
    inputs.
  - **Single-event truncation.** Before deciding to drop an event,
    BudgetStage asks: "would this event fit if its body were
    truncated to one line?" If yes — i.e., the Title + Location +
    one-line body fits in the remaining budget — the event is
    emitted with `Body` reduced to its first line plus a sentinel
    suffix line `"... [truncated by --budget]"`, and `Truncated=true`.
    If no — the event is dropped entirely.
  - **Counters.** A `BudgetCounters` struct (exported, zero-value
    safe, goroutine-unsafe — the Sink reads it only after the
    pipeline returns) holds:
    - `EventsBuffered int` — events the stage saw on input.
    - `EventsEmitted int` — events sent downstream.
    - `EventsDroppedBudget int` — events the budget forced out.
    - `EventsTruncated int` — events whose body was shortened.
    - `EstimatedTokens int` — total estimated tokens emitted
      (including the reserve; see M6.3).
  - **Footer reserve.** The stage subtracts `Reserve` from `Budget`
    before deciding what fits, so the Sink (M7) always has room for
    a summary line. With `Budget < Reserve` the stage emits no
    events and reports them all as dropped.
- **Tests** (`internal/pipeline/budget_test.go`):
  - `TestBudgetStage_ZeroBudgetIsPassthrough`: `Budget=0` → every
    input event flows out unchanged with no buffering delay (verify
    streaming via `testutil.SlowReader`-style timing assertion).
  - `TestBudgetStage_EmitsHighestSeverityFirst`: feed
    `[info, warn, error]` with a budget that fits only two events;
    assert `[error, warn]` emerge in that order.
  - `TestBudgetStage_DropsLowestFirst`: budget too tight for all;
    the dropped event is the lowest-severity one. Tie-break by
    arrival order: among same-severity events, later arrivals drop
    first.
  - `TestBudgetStage_TruncatesSingleOversizedEvent`: feed one Event
    whose estimated cost > `Budget - Reserve` but whose Title alone
    fits; assert it emerges with `Truncated=true`, `Body` contains
    one verbatim line plus the sentinel.
  - `TestBudgetStage_DropsUntruncatableEvent`: feed one Event whose
    Title alone exceeds budget; assert it is dropped and counted as
    `EventsDroppedBudget`, not `EventsTruncated`.
  - `TestBudgetStage_ReserveProtected`: with `Budget=100`,
    `Reserve=30`, the stage never emits more than ~70 tokens of
    estimated output (within the estimator's ±15% margin).
  - `TestBudgetStage_CountersAccurate`: feed a fixture with known
    expected counts, assert every `BudgetCounters` field matches.
  - `TestBudgetStage_DeterministicOrder`: same input twice → same
    emission sequence (property test; ties into M2.2 determinism
    invariant).
  - `TestBudgetStage_ContextCancellation`: cancel mid-stream
    (during the buffering phase), stage drains and exits, no
    goroutine leak.
- **Docs:**
  - Godoc on `BudgetStage`, `BudgetCounters`, the `Budget` /
    `Reserve` / `Estimator` / `Counters` fields. Document the
    buffering-vs-streaming tradeoff explicitly.
  - Update
    [ARCHITECTURE.md § Budget enforcement](./ARCHITECTURE.md#budget-enforcement)
    so it documents the truncation sentinel string and the reserve
    behaviour. The existing four-step sketch in ARCHITECTURE.md is
    the spec; this commit fleshes out the implementation.

### M6.2 — Wire BudgetStage into pipeline.Options and Build ✅

Make the budget controllable from the same `Options` value the CLI
(M8) and library callers (M15) already use.

- **DoD:**
  - `pipeline.Options` gains:
    - `Budget int` (token cap; 0 = no budget).
    - `Tokenizer string` (heuristic | tiktoken; passed through to
      `tokens.ByName`).
  - `Build(src, sink, opts)` wires the chain as
    `[CollapseStage, DedupeStage, BudgetStage]` when `Budget > 0`,
    and `[CollapseStage, DedupeStage]` when `Budget == 0`. The
    BudgetStage is inserted between the existing stages and the
    Sink, never reordered.
  - The `BudgetCounters` value is created by `Build`, attached to
    the returned Pipeline (via a new `Pipeline.BudgetCounters`
    field), and shared with the BudgetStage by pointer so the Sink
    can read it after `Run` returns.
  - When `Budget > 0` and `Estimator` would fail to construct
    (e.g., unknown tokenizer name), `Build` returns an error rather
    than a partially-wired Pipeline. Signature changes from
    `Build(...) *Pipeline` to `Build(...) (*Pipeline, error)`; all
    existing call sites updated in the same commit.
- **Tests** (`internal/pipeline/build_test.go`, extended):
  - `TestBuild_BudgetZeroOmitsBudgetStage`: `Options{}` → chain is
    `[CollapseStage, DedupeStage]`, no BudgetStage.
  - `TestBuild_BudgetSetIncludesBudgetStage`: `Options{Budget: 100}`
    → chain is `[CollapseStage, DedupeStage, BudgetStage]`, in that
    order.
  - `TestBuild_BudgetCountersExposed`: after `Run`, the Pipeline's
    `BudgetCounters` reflects what the BudgetStage observed.
  - `TestBuild_UnknownTokenizerErrors`: `Options{Budget: 100, Tokenizer: "ggml"}`
    → `Build` returns an error before any goroutine starts.
  - `TestBuild_TokenizerHeuristicByDefault`: empty `Tokenizer`
    string defaults to `"heuristic"`.
- **Docs:**
  - Godoc on the new `Options` fields and the new
    `Pipeline.BudgetCounters` field.
  - Mention the new chain shape and the `Build` error return in
    [ARCHITECTURE.md § Pipeline](./ARCHITECTURE.md#pipeline).

### M6.3 — Exit code 3 plumbing ✅

The CLI maps "BudgetStage dropped or truncated content" to exit
code 3. M6 prepares the signal; M8 reads it.

- **DoD:**
  - `BudgetCounters` gains a method
    `func (c *BudgetCounters) ForcedDrops() bool` that returns
    `EventsDroppedBudget > 0 || EventsTruncated > 0`.
  - Documented contract: any consumer that wants to honour exit
    code 3 calls `ForcedDrops()` on the Pipeline's `BudgetCounters`
    after `Run` returns. M8 will wire the CLI; M15 library callers
    can do the same.
  - No CLI work yet — flag parsing and exit-code mapping live in M8.
- **Tests:**
  - `TestBudgetCounters_ForcedDropsTrueOnDrops`.
  - `TestBudgetCounters_ForcedDropsTrueOnTruncations`.
  - `TestBudgetCounters_ForcedDropsFalseOnCleanRun`.
- **Docs:**
  - Godoc on `ForcedDrops`.
  - Update
    [ARCHITECTURE.md § Exit codes](./ARCHITECTURE.md#exit-codes) to
    name `BudgetCounters.ForcedDrops()` as the source-of-truth for
    exit code 3.

### M6 exit criteria

- All three sub-items ticked.
- `make check` clean; no race hits; no goroutine leaks.
- M6 milestone drift check: ARCHITECTURE.md budget-enforcement section
  documents the truncation sentinel, the reserve, and the
  `ForcedDrops()` contract; the new `Pipeline.BudgetCounters` field
  is mentioned in
  [ARCHITECTURE.md § Pipeline](./ARCHITECTURE.md#pipeline); SCHEMA.md
  already documents `truncated` and `events_dropped_budget` and so
  needs no change.
- `--budget=N` and `--tokenizer=...` flags are **not** wired in M6;
  that's M8. The pipeline option exists so M8 only has to pass flag
  values through.

---

## M7 — Output encoders ✅

Three Sinks that turn the Event stream into bytes a user (or an LLM)
can read: compact `text` (the default), schema-versioned `json` /
`ndjson`, and `markdown` for direct paste into chat. Each Sink owns
its own footer rendering; the `--no-footer` option suppresses the
footer line uniformly across all three.

Cross-references
[ARCHITECTURE.md § Output formats](./ARCHITECTURE.md#output-formats),
[docs/formats/SCHEMA.md](./docs/formats/SCHEMA.md) (the source of
truth for JSON), and
[output-stability rule](./.opencode/rules/output-stability.md) (JSON
is a public API).

M7 builds on M6 (`BudgetCounters` populates the summary; estimated
tokens come from the Estimator the BudgetStage already constructed).
The Sink reads counters and the BudgetStage's
`BudgetCounters.EstimatedTokens` after the pipeline returns. Each
item below lists Definition of Done (DoD), required tests, and
required doc updates per the
[alignment rule](./.opencode/rules/alignment.md).

### M7.1 — `internal/output/text.go`: default compact encoder ✅

Match the example output in
[ARCHITECTURE.md § Output formats § text](./ARCHITECTURE.md#output-formats).

- **DoD:**
  - `TextSink` implements `pipeline.Sink`. Configurable fields:
    - `Writer io.Writer` (required).
    - `NoFooter bool`.
    - `FormatName string` (the format that fed the pipeline; used
      in the header line "N events from <format>").
    - `Counters *pipeline.BudgetCounters` (optional; nil for
      pipelines without BudgetStage — the Sink computes the
      summary from its own running counts).
  - **Streaming.** Events render incrementally: each event writes
    its own block as soon as it arrives. The header line is
    deferred until the first event arrives (so the count is known)
    or replaced with a "no events found" line if input closes
    without any events.
  - **Per-event block shape** (lines):
    1. `[N] <SEVERITY> <Title>` — N is 1-indexed sequence number.
    2. `  at <file>:<line>` — only if `Location` is set.
    3. Body lines indented two spaces.
    4. `  context:` followed by indented context lines, only if
       `Context` is non-empty.
    5. `  ... K vendor frames collapsed` — only if
       `FramesCollapsed > 0`.
    6. `  (×K)` — only if `Count > 1`.
    7. `  [truncated by --budget]` — only if `Truncated == true`.
    8. Blank line.
  - **Footer** (skipped if `NoFooter`):
    - `---` separator.
    - `distilled <in_lines> lines → <out_lines> lines (<tokens> tokens)`.
    - `dropped: <budget_drops> events, <dedup_collapsed> deduped, <frames> vendor frames`.
    - Input line count comes from a `LineCounter` wrapper the Source
      installs around its `io.Reader`; expose a public
      `output.LineCounter` so the CLI can plug it in.
  - All counters degrade gracefully when their source isn't
    available (e.g., no BudgetStage → `dropped: 0 events`).
- **Tests** (`internal/output/text_test.go`):
  - Golden-file tests under `internal/output/testdata/text/`:
    `case-01.events.json` (a fixture serialisation of events) +
    `case-01.expected.txt`. Update with `go test -update ./...`.
    Minimum five cases: empty input, single event, multi-event,
    event with frames + context + count, event with truncated body.
  - `TestTextSink_NoFooterSuppressesFooter`.
  - `TestTextSink_StreamingEmitsBeforeEOF`: use
    `testutil.SlowReader` to drip events; first event block must
    appear before the source closes.
  - `TestTextSink_HandlesNilLocation`: event with no Location omits
    the `at file:line` line cleanly.
  - `TestTextSink_ContextCancellation`: cancel mid-stream, Sink
    returns context.Canceled, no goroutine leak.
- **Docs:**
  - Godoc on `TextSink`, `LineCounter`, every exported field.
  - Update [README.md](./README.md) once the CLI is wired in M8;
    M7 only needs godoc + the existing ARCHITECTURE example.

### M7.2 — `internal/output/json.go`: schema-versioned JSON / ndjson ✅

Match the schema in
[docs/formats/SCHEMA.md](./docs/formats/SCHEMA.md) exactly.

- **DoD:**
  - `JSONSink` implements `pipeline.Sink`. Configurable fields:
    - `Writer io.Writer` (required).
    - `NoFooter bool`.
    - `FormatName string`.
    - `Counters *pipeline.BudgetCounters` (optional).
    - `Streaming bool` — when true, emits ndjson (one JSON object
      per line, each carrying `schema_version`, `format`, and
      either `event` or `summary`); when false, accumulates events
      and emits a single batch JSON object at end of stream.
    - `EstimatorName string` — `"heuristic"` or `"tiktoken"`,
      reflected in `summary.estimator`.
    - `ExitCode int` — populated by the CLI (M8) after pipeline
      completion; included in `summary.exit_code`.
  - **Schema version constant.** A new `output.SchemaVersion = 1`
    constant (the only place this value lives). The
    `TestEvent_JSONSchemaMatchesDoc` test in `internal/event/`
    extended to also verify `output.SchemaVersion` matches the
    `Current schema version: 1` line in SCHEMA.md.
  - **Streaming mode.** Each event marshals to a self-contained
    line of the shape
    `{"schema_version":1,"format":"<name>","event":{...}}`; the
    final line is
    `{"schema_version":1,"format":"<name>","summary":{...}}`.
    Per SCHEMA.md.
  - **Batch mode.** Single top-level object with `schema_version`,
    `format`, `events: [...]`, `summary: {...}`.
  - **Mode selection.** M7 exposes `Streaming` as an explicit
    field. The CLI (M8) decides which to use: stdin pipe-into-tty
    use case → batch; stdin from a long-running tail → streaming.
    A future heuristic can auto-detect; for now M8 ships an
    `--output-streaming` flag or chooses batch by default.
  - **`null` Location.** When an event has `Location == nil`,
    JSON encodes `"location": null`. The struct tag is already
    `json:"location"` (no omitempty), so the existing event package
    handles this; M7 verifies it via a golden test.
  - **`metadata` map order.** Marshalled with sorted keys for
    determinism (use `json.Marshal` on a `map[string]string`
    which Go sorts deterministically). Asserted by a property test.
- **Tests** (`internal/output/json_test.go`):
  - Golden tests under `internal/output/testdata/json/` matching
    the same case set as the text encoder: same five fixtures →
    expected JSON. Plus three ndjson cases to exercise streaming
    mode.
  - `TestJSONSink_SchemaVersionMatchesDoc`: extends
    `TestEvent_JSONSchemaMatchesDoc` (or lives alongside it) to
    cross-check `output.SchemaVersion` against the SCHEMA.md
    "Current schema version" line.
  - `TestJSONSink_StreamingProducesNDJSON`: every line of output
    is a valid JSON object; final line has `summary`, prior lines
    have `event`.
  - `TestJSONSink_BatchProducesSingleObject`: output parses to
    one `Output{}` struct; `len(events)` matches input.
  - `TestJSONSink_MetadataSortedKeys`: feed an event with metadata
    `{c: ..., a: ..., b: ...}`; output order is `a, b, c`.
  - `TestJSONSink_NoFooterStillIncludesSummaryFieldsInBatch`: in
    batch mode, the summary object is part of the schema and is
    always emitted; `--no-footer` suppresses only the human-
    readable footer line in text/markdown. Document this asymmetry
    in godoc.
- **Docs:**
  - Godoc on `JSONSink`, `SchemaVersion`, every field.
  - Update SCHEMA.md only if M7 discovers a gap (the current spec
    is the contract). Document the text/markdown vs JSON
    `--no-footer` asymmetry under
    [SCHEMA.md § Summary object](./docs/formats/SCHEMA.md#summary-object).
  - Update [output-stability rule](./.opencode/rules/output-stability.md)
    if any drift-guard test name changes.

### M7.3 — `internal/output/markdown.go`: markdown encoder ✅

Same content as the text encoder, wrapped in markdown headings and
fenced blocks for direct paste into chat.

- **DoD:**
  - `MarkdownSink` implements `pipeline.Sink`. Same configurable
    fields as `TextSink`.
  - **Per-event block shape:**
    - `### [N] <SEVERITY> <Title>`.
    - Bullet list of metadata (location, count, truncation marker,
      collapsed-frame count).
    - Body wrapped in a fenced code block. Fence language derived
      from a format hint on the Sink (`FenceLang` field; default
      empty, no language).
    - Context wrapped in its own fenced block under a `**Context:**`
      bold label, only if non-empty.
  - **Header:** `# N events from <format>` once first event arrives.
  - **Footer:** `---` separator, then a bulleted summary list with
    the same fields as the text footer. Suppressed by `NoFooter`.
- **Tests** (`internal/output/markdown_test.go`):
  - Golden tests under `internal/output/testdata/markdown/`. Same
    five-case minimum as text and JSON.
  - `TestMarkdownSink_FenceLanguage`: setting `FenceLang="python"`
    produces ` ```python ` fences.
  - `TestMarkdownSink_NoFooter`.
  - `TestMarkdownSink_StreamingEmitsBeforeEOF` (parallel to text).
- **Docs:**
  - Godoc on `MarkdownSink`, `FenceLang`.

### M7.4 — Property tests across all three Sinks ✅

Promote the cross-cutting invariants to single-source-of-truth tests.

- **DoD:**
  - `TestSinks_DeterministicForFixedInput`: same `[]Event` fed to
    each Sink twice → byte-equal output both times. Runs against
    every Sink in the package.
  - `TestSinks_StreamingEmitsBeforeEOF`: same `testutil.SlowReader`
    helper from M2.2, repeated for each Sink in turn.
  - `TestSinks_NoFooterFlagHonoured`: feed identical input twice,
    once with `NoFooter=true` and once without; assert the output
    differs only by the footer block.
  - `TestSinks_FooterReflectsCounters`: synthesise a
    `BudgetCounters` value with known fields, run a fixture, parse
    the footer (JSON: directly; text/markdown: by line-matching
    the printed counters) and assert each counter is reflected.
- **Tests:** the property tests are the deliverable.
- **Docs:**
  - Reference these tests from
    [testing.md](./.opencode/rules/testing.md) under the existing
    property-test section, alongside the pipeline property tests.

### M7 exit criteria

- All four sub-items ticked.
- `make check` clean.
- M7 milestone drift check: SCHEMA.md `schema_version` matches
  `output.SchemaVersion`; every field in SCHEMA.md's event and
  summary tables is exercised by at least one JSON golden test;
  ARCHITECTURE.md output-formats example for the text encoder
  matches what `TextSink` actually emits.
- The CLI is **not** wired in M7; the Sinks are constructed and
  tested directly. M8 wires `--output`, `--no-footer`,
  `--output-streaming`.

---

## M8 — CLI surface ✅

Final user-facing surface: flag parsing, subcommand dispatch, file vs
stdin input, exit-code mapping. Everything M0–M7 built is hidden from
the user without this milestone.

Cross-references
[ARCHITECTURE.md § Flags](./ARCHITECTURE.md#flags),
[§ Exit codes](./ARCHITECTURE.md#exit-codes),
[flag-policy rule](./.opencode/rules/flag-policy.md) (a flag is a
one-way door — read this before suggesting any addition).

M8 builds on M3 (autodetect, the `detect` subcommand stub), M5
(dedupe and collapse Options), M6 (budget Options, `BudgetCounters`),
and M7 (Sinks). It introduces `cobra` as the CLI framework — see
M8.1 for the dependency justification. Each item below lists DoD,
required tests, and required doc updates per the
[alignment rule](./.opencode/rules/alignment.md).

### M8.1 — Adopt `cobra` for flag and subcommand handling ✅

The current `cmd/distill-ai/main.go` is a hand-rolled switch with two
verbs. M8's flag matrix and subcommand tree make a dependency
worthwhile.

- **DoD:**
  - Add `github.com/spf13/cobra` and `github.com/spf13/pflag`
    (cobra's transitive flag library) as dependencies. Justified
    in the commit body per
    [dependencies rule](./.opencode/rules/dependencies.md): the
    flag matrix in
    [ARCHITECTURE.md § Flags](./ARCHITECTURE.md#flags) has 17
    flags across four groups, plus six subcommands with their own
    flags. Hand-rolled `flag` package handling would not let us
    ship the `completions [bash|zsh|fish]` subcommand (which is a
    selling point per the original design) — cobra generates that
    for free. No CGo, MIT-licensed, both libraries already vendored
    by many Go CLIs (kubectl, helm, gh) so they're well-trodden.
  - Update [ARCHITECTURE.md § Dependencies](./ARCHITECTURE.md#dependencies)
    to list cobra and pflag.
  - Re-architect `cmd/distill-ai/main.go`:
    - `main()` calls `root.Execute()`.
    - `root` is a `*cobra.Command` defined in a new
      `cmd/distill-ai/root.go`.
    - The existing `detect` subcommand moves to its existing
      `detect.go` but adopts the cobra signature.
- **Tests** (`cmd/distill-ai/root_test.go`):
  - `TestRoot_HelpExitsZero`.
  - `TestRoot_UnknownSubcommandExitsTwo`.
  - `TestRoot_VersionPrintsBuildInfo`.
- **Docs:**
  - ARCHITECTURE dependencies update.
  - Godoc on the new exported pieces (likely none; main package).

### M8.2 — `distill-ai run` (the default pipeline command) ✅

The verb that distills. Default subcommand when none is given, so
`cmd | distill-ai` works without arguments.

- **DoD:**
  - `cmd/distill-ai/run.go` defines a `runCmd` cobra command that:
    1. Resolves input: positional `FILE...` if given, else stdin.
       Multi-file input is concatenated with a `\n` separator (each
       file's content emitted, then a newline, then the next file's
       content) so a single format detection covers the whole
       stream. If files have heterogeneous formats, the user should
       run distill-ai per file; M8 documents this limitation in
       `--help`.
    2. Resolves format: explicit positional `FORMAT` argument or
       `--auto` autodetect (the default). `--strict` is honoured.
    3. Constructs `pipeline.Options` from flags.
    4. Constructs a Sink from `--output` and `--no-footer`.
    5. Calls `pipeline.Build` and `Pipeline.Run`.
    6. Maps the result to an exit code per M8.3.
  - All flags listed in
    [ARCHITECTURE.md § Flags](./ARCHITECTURE.md#flags) are
    registered:
    - **Input/format:** `--auto` (default true), `--list-formats`,
      and positional `FORMAT`.
    - **Filtering:** `--keep-vendor`, `--keep-warnings`,
      `--severity`, `--max-events`, `--context`.
    - **Deduplication:** `--dedupe` (default on for streaming, off
      for batch — implemented as a `--dedupe-window` non-zero
      default in pipe mode, zero in batch mode), `--no-dedupe`,
      `--dedupe-window`.
    - **Output:** `--output`, `--budget`, `--no-footer`.
    - **Behaviour:** `--explain`, `--strict`, `--passthrough`,
      `--tokenizer`.
    - **Standard:** `-h` / `--help`, `-v` / `--verbose`,
      `--version`.
  - **`--max-events`, `--keep-warnings`, `--severity`, `--context`,
    `--explain`, `--passthrough`, `--list-formats`** are
    registered with proper godoc/help text but their pipeline
    plumbing lands in a follow-up commit within this milestone
    (M8.2.x) so reviewers can see flag wiring separately from
    feature wiring. Each gets its own commit body explaining
    where the option attaches inside `pipeline.Options`.
  - **`-v` / `--verbose`** writes pipeline diagnostics to stderr:
    chosen format, sample bytes consumed, per-stage event count
    on EOF. Nothing on stdout, ever.
  - **`--version`** uses the ldflag-injected `version`, `commit`,
    `date` from `main.go`.
- **Tests** (`cmd/distill-ai/run_test.go`):
  - `TestRun_StdinEndToEnd`: pipe a known fixture in, assert
    expected output and exit code 0.
  - `TestRun_FileInput`: same as above but from a tempfile.
  - `TestRun_MultiFileConcatenation`: two tempfiles, identical
    format, output matches `cat f1 f2 | distill-ai`.
  - `TestRun_ExplicitFormatBeatsAutodetect`: positional `FORMAT`
    overrides what autodetect would have chosen.
  - `TestRun_NoEventsExitsOne`: clean input produces exit code 1.
  - `TestRun_BudgetDropsExitsThree`: tight budget forces drops
    → exit code 3; the dropped count is in stderr when `-v`.
  - `TestRun_StrictUnknownFormatExitsTwo`: feed binary garbage
    with `--strict` → exit code 2.
  - `TestRun_VerboseWritesToStderr`: stdout is parseable distilled
    output; stderr has the diagnostic line.
  - `TestRun_HelpMatchesFlagList`: parse `--help` output, assert
    every flag listed in
    [ARCHITECTURE.md § Flags](./ARCHITECTURE.md#flags) appears in
    `--help` (drift guard). Update both in the same commit when a
    flag is added.
- **Docs:**
  - Godoc on every flag (cobra renders this into `--help`).
  - Update README.md usage section with two new examples:
    `pytest -v | distill-ai` and `distill-ai run failure.log`.
  - Update ARCHITECTURE.md flag list if any flag's behaviour
    differs from the original sketch (e.g., the multi-file
    concatenation rule).

### M8.3 — Exit-code mapping ✅

Make the four exit codes (0/1/2/3) authoritative.

- **DoD:**
  - A new `cmd/distill-ai/exitcode.go` with named constants:
    - `ExitOK = 0`
    - `ExitNoEvents = 1`
    - `ExitError = 2`
    - `ExitPartial = 3`
  - `runCmd` returns these as follows:
    - `2` if argument parsing or IO setup failed, **before** the
      pipeline runs.
    - `2` if `Pipeline.Run` returns a non-context-canceled error.
    - `1` if `Pipeline.Run` succeeds but the Sink reports zero
      events emitted (read via Sink counters — `TextSink` /
      `JSONSink` / `MarkdownSink` expose an `EventsEmitted()`
      method).
    - `3` if `Pipeline.Run` succeeds and
      `Pipeline.BudgetCounters.ForcedDrops()` is true.
    - `0` otherwise.
  - **Streaming vs batch.** In streaming JSON mode the summary
    line carries `exit_code`; M7's JSONSink takes the exit code
    from `JSONSink.ExitCode`. M8 wires the value after `Run`
    returns and before the Sink writes its trailer. Since
    streaming Sinks have already started writing, the exit code
    is finalised at end-of-stream — the Sink reserves it for the
    trailer.
  - `runCmd` returns an `int` (not `error`) so the cobra runner
    can map cleanly. Inside the function it converts errors to
    the right code via a small `mapError` helper.
- **Tests:**
  - `TestExitCode_NoEvents`.
  - `TestExitCode_BudgetForcedDrops`.
  - `TestExitCode_PipelineError`.
  - `TestExitCode_FlagParseError`.
- **Docs:**
  - Godoc on each constant referencing
    [ARCHITECTURE.md § Exit codes](./ARCHITECTURE.md#exit-codes).
  - Cross-link from ARCHITECTURE.md back to the constants so the
    spec and the implementation stay anchored.

### M8.4 — `list-formats` subcommand ✅

- **DoD:**
  - `distill-ai list-formats` prints one line per registered
    format, columns: name, version (always `"1"` for now;
    placeholder for future), source (path of the package the
    format is registered from, or `"builtin"` for in-tree
    formats). Output goes to stdout.
  - Sort by name (matches `formats.All()` deterministic order
    from M1.3).
  - Exit code 0 on success.
- **Tests** (`cmd/distill-ai/list_formats_test.go`):
  - `TestListFormats_OutputIncludesGeneric`: once `generic` lands
    in M9 it must appear; pre-M9 the test asserts at least the
    test-only `passthrough` format if any.
  - `TestListFormats_DeterministicOrder`: run twice, byte-equal
    output.
- **Docs:**
  - Help text.
  - README mention in the usage list.

### M8.5 — `detect FILE` subcommand (cobra adaptation) ✅

The detect subcommand exists from M3.3; M8 ports it onto the cobra
root and adds flags.

- **DoD:**
  - `distill-ai detect FILE` works as in M3.3.
  - `--strict` flag respected (exit 2 instead of falling back to
    `generic`).
  - Output unchanged from M3.3 so existing scripts keep working.
- **Tests:**
  - Existing M3.3 tests retained; add
    `TestDetectCmd_StrictExitsTwoOnLowConfidence`.
- **Docs:** Help text update; ARCHITECTURE.md detect subcommand
  section already documents the behaviour.

### M8.6 — `explain FILE` subcommand ✅

Dry-run mode: annotate kept/dropped/why without writing distilled
output. Built atop the existing pipeline with a special Sink that
serialises the decisions instead of the events.

- **DoD:**
  - `distill-ai explain FILE` reads input, runs the full pipeline,
    and emits a per-event diagnostic line:
    `kept` or `dropped:<reason>` plus the event Title and
    Location.
  - Reasons: `severity-filter`, `budget`, `dedupe-evicted`,
    `vendor-collapsed`.
  - A new `ExplainSink` in `internal/output/` (added in this
    commit, not M7 — `explain` is sufficiently specialised that
    it lives in M8 alongside the subcommand that uses it).
  - **Capturing drop reasons.** Each existing Stage emits a
    distinct event tag on its drops: BudgetStage on drop emits a
    sentinel "explain event" on a side-channel that ExplainSink
    drains; DedupeStage similarly on eviction (note: M5 currently
    forwards dedupe-evicted events with `Count` updated, not as
    "drops" — explain mode interprets a `Count > 1` event as
    "K-1 dedupe drops"). CollapseStage's drops are counted on
    each Event's `FramesCollapsed` so no side-channel needed.
  - **Stage instrumentation.** To avoid adding a side-channel that
    leaks into the non-explain code path, ExplainSink is fed by
    wrapping each Stage with an instrumented variant that records
    drops to a shared `ExplainLog` value. The wrappers live in
    `internal/pipeline/explain.go` and are only used by the
    explain command. Detailed plumbing decided at implementation
    time; the constraint is: zero impact on the non-explain code
    path.
- **Tests:**
  - `TestExplainCmd_AnnotatesDrops`: fixture with known drops
    produces expected annotated output.
  - `TestExplainCmd_NoFalseDrops`: clean input → every line says
    `kept`.
- **Docs:**
  - Help text.
  - README example.
  - `docs/explain.md` if the format needs more than the godoc
    can cover.

### M8.7 — `completions [bash|zsh|fish]` subcommand ✅

Cobra generates these for free; M8 only has to wire them.

- **DoD:**
  - `distill-ai completions bash` prints a bash completion script
    to stdout; similarly for zsh and fish.
  - Exit code 0 on success, 2 on unknown shell.
- **Tests:**
  - `TestCompletions_BashOutputIsNonEmpty` (don't pin to specific
    contents — cobra's output is its concern).
  - `TestCompletions_UnknownShellErrors`.
- **Docs:** Help text and a one-line README note.

### M8.8 — `version` subcommand ✅

Already covered by the top-level `--version` flag but exposed as a
subcommand too for consistency (some CLIs require this; cheap to
ship).

- **DoD:**
  - `distill-ai version` prints `version`, `commit`, `date` from
    ldflags, one per line.
  - Exit code 0.
- **Tests:**
  - `TestVersionCmd_PrintsBuildInfo`.
- **Docs:** Help text; mentioned in README under "Build info".

### M8 exit criteria

- All eight sub-items ticked.
- `make check` clean; binary builds on linux/darwin/windows ×
  amd64/arm64 (verified by the existing CI matrix, no new work
  needed).
- M8 milestone drift check: every flag listed in
  [ARCHITECTURE.md § Flags](./ARCHITECTURE.md#flags) is exercised
  by at least one test; every subcommand listed has its own test
  file; README's usage section names every subcommand the CLI
  ships; SCHEMA.md `summary.exit_code` field is wired end-to-end
  through `JSONSink.ExitCode`.
- M8 is the milestone after which `distill-ai` is end-to-end usable
  by a human or an agent. M9–M12 add formats; M13 adds the envelope
  stripper; M14 adds config; M15 promotes the library API; M16
  polishes docs.

---

## M9 — Generic format (fallback) ✅

The detector's safety net. When no specific format scores above
`event.ConfidenceMinDetect` (0.6), the detector falls back to a format
registered under the reserved name `"generic"` (see
[ARCHITECTURE.md § Autodetection](./ARCHITECTURE.md#autodetection)).
Today no such format exists — the `detect` subcommand returns
`ErrNoFormat` when nothing matches and the help text says "no generic
fallback is registered yet (lands in M9)." M9 closes that gap.

The generic format is a regex-driven scanner. It cannot do what
pytest / jest / gotest do — it has no test-runner semantics, no
structured frame extraction beyond best-effort `file:line:` matches.
It exists so that piping arbitrary log output through `distill-ai`
yields something rather than nothing: a sequence of severity-bucketed
Events anchored to `ERROR`, `FATAL`, `panic`, `Exception`,
`Traceback`, and friends, with N lines of surrounding context.

Cross-references
[ARCHITECTURE.md § Autodetection](./ARCHITECTURE.md#autodetection)
(falls back to `generic` by name),
[ARCHITECTURE.md § Format plugin contract](./ARCHITECTURE.md#format-plugin-contract)
(the Format interface),
[docs/formats/SCHEMA.md § Kind values](./docs/formats/SCHEMA.md#kind-values)
(new generic kind values land here).

M9 builds on M1 (`Format`, `formats.Register`), M3
(autodetection — `generic` must lose ties to every specific format
even at equal confidence; the detector already excludes `generic`
from the candidate set up front), M5 (StackFrame classification via
`ClassifyFrames`, when the parser opportunistically extracts a
frame), and M7 (the encoders that render generic Events). Each item
below lists Definition of Done, required tests, and required doc
updates per the [alignment rule](./.opencode/rules/alignment.md).

### M9.1 — `internal/formats/generic/generic.go`: skeleton + Detect ✅

Land the package, register it under the reserved name, implement
`Detect` as a deliberate low-floor returner. No parsing yet —
`Parse` returns an immediately-closed channel — so the detector's
fallback path can exercise the new format end-to-end before the
scanner arrives.

- **DoD:**
  - New package `internal/formats/generic` exporting `Format` (a
    value type implementing `formats.Format`).
  - `func init() { formats.Register(Format{}) }` so the registry
    picks it up at process start.
  - `Name() string` returns `"generic"` — the reserved name the
    detector looks up by string in
    [ARCHITECTURE.md § Autodetection](./ARCHITECTURE.md#autodetection).
  - `Detect(sample []byte) Confidence`:
    - Returns `confidenceFloor = 0.1` when the sample contains at
      least one line matching the package's `severityPattern`
      catalogue (defined in M9.2). The intent is "we can probably
      find *something* useful" rather than "we recognise this
      format." 0.1 is intentionally below
      `event.ConfidenceMinDetect` (0.6) so a specific format
      always wins.
    - Returns `0.0` otherwise.
    - The constant `confidenceFloor = 0.1` lives at package scope
      so reviewers can see the magic number without context-
      switching to the test.
  - `Parse(ctx, r, opts)` returns an immediately-closed channel
    with `nil` error for M9.1; M9.2–M9.4 fill it in. The detector
    can then resolve the fallback path against a real registered
    Format before the parser exists.
  - Per [ARCHITECTURE.md § Autodetection step 2](./ARCHITECTURE.md#autodetection),
    `generic` is excluded from the detector's candidate set, so its
    `Detect` is never compared against a specific format on ties.
    M9.1 documents this invariant in the package godoc so future
    contributors don't try to make `generic` "win" ties by
    inflating its confidence.
- **Tests** (`internal/formats/generic/generic_test.go`):
  - `TestGeneric_RegisteredAtInit`: import the package for its
    side effect, then call `formats.Get("generic")` and assert
    `(format, true)` and `format.Name() == "generic"`.
  - `TestGeneric_DetectFloorOnSeverityHit`: feed a sample with a
    single line containing `ERROR`; assert `Confidence == 0.1`.
  - `TestGeneric_DetectZeroOnNonMatch`: feed innocuous prose
    (`"Hello, world."`); assert `Confidence == 0.0`.
  - `TestGeneric_DetectBelowMinThreshold`: assert
    `confidenceFloor < event.ConfidenceMinDetect` at compile time
    via a test that fails when the constants drift apart.
  - `TestGeneric_ParseEmptyStub`: ensure `Parse` returns a closed
    channel without error so M3 fallback paths work end-to-end
    against the stubbed parser.
  - `TestDetect_FallbackUsesGenericFormat`: extend the existing
    detector test in `internal/detect/detect_test.go` so a sample
    that no specific format claims (low-entropy text) returns
    `FellBackToGeneric=true` with `Format.Name() == "generic"`,
    rather than the current `ErrNoFormat`.
- **Docs:**
  - Godoc on `Format`, `Detect`, `Parse`, `confidenceFloor`, and
    the "excluded from detector candidate set" invariant.
  - New `docs/formats/generic.md` with the section skeleton
    (intro, detection model, what's extracted, what's dropped,
    example I/O). M9.1 fills in intro + detection; M9.2–M9.4
    extend.
  - Update [README.md](./README.md) usage section to mention that
    `distill-ai` produces output for unknown log shapes via the
    `generic` fallback (M8 wires this end-to-end via the run
    command; M9 makes the fallback non-empty).
  - Update `cmd/distill-ai/detect.go` help text — the current
    hint string ("no generic fallback is registered yet (lands in
    M9)") becomes stale the moment M9.1 lands. Replace it with a
    note that low-confidence input still detects as `generic` by
    default and `--strict` is the way to reject it.

### M9.2 — Severity-anchored event scanner ✅

The core scan loop: read line-by-line, anchor an Event on every line
that matches the severity catalogue, capture N lines of context
before and after the anchor.

- **DoD:**
  - The parser is a `bufio.Scanner` over `r io.Reader`. No
    full-input buffering: a small rolling window
    (`contextLines * 2 + 1` lines) is the only in-memory state,
    bounded regardless of input size.
  - The default context size is `defaultContextLines = 3`.
    `ParseOpts` already carries a context-size field once M8 wires
    `--context=N`; until then M9 hard-codes the default and the
    opt-in field is read from `opts` when non-zero. The constant
    lives at package scope.
  - **Severity catalogue.** A package-level slice of
    `(pattern *regexp.Regexp, severity event.Severity, kind string)`
    triples covers the standard markers:
    - `error`: `ERROR`, `FATAL`, `panic:`, `Exception:`,
      `Traceback ` (the trailing space is intentional — anchors
      the Python "Traceback (most recent call last):" form),
      `\bcaused by:`, `Error:`.
    - `warn`: `WARN(ING)?`, `\bDeprecation`, `^W\d{4}:` (Python
      warning code form), `Warning:`.
    - `info`: deliberately empty in v1 — info-level scanning has
      too much noise from healthy stdout. Hooking it up is
      backlog work; ship without to keep the default quiet.
    - Each pattern is precompiled at package init.
    - Patterns are evaluated in catalogue order; first match wins,
      so `Traceback` (kind `traceback`) sorts above generic
      `Error:` (kind `error`).
  - **Kind values.** The scanner emits these kinds, each added to
    SCHEMA.md in M9.5: `error_line`, `warning_line`, `traceback`,
    `panic`, `exception`.
  - **Per-Event shape:**
    - `Severity` from the matched pattern.
    - `Kind` from the matched pattern.
    - `Title` = the matched line, trimmed of trailing whitespace,
      with leading ANSI escape sequences (`\x1b\[[0-9;]*m`)
      stripped. The strip happens once via a precompiled regex.
    - `Location` = best-effort extraction via a single
      `locationPattern` regex matching `<path>:<line>:?(\d+)?`
      anywhere on the anchor line. Path must contain at least one
      `/` or `\` to avoid false positives on host:port pairs. Nil
      if no match.
    - `Body` = the anchor line verbatim (no ANSI strip — the user
      can see what was emitted).
    - `Context` = up to `contextLines` lines before and
      `contextLines` lines after, in source order, joined into
      one slice. Lines that themselves match the severity
      catalogue are still included as context — the scanner does
      not deduplicate adjacent matches into a single Event.
    - `Frames`, `FramesCollapsed`, `Count`, `Metadata` left zero
      / nil. Dedupe is M5's job; frame extraction for `traceback`
      / `panic` blocks is M9.3.
  - **Streaming.** Each Event is forwarded as soon as its
    trailing-context window is full (i.e., `contextLines` lines
    after the anchor have been consumed) or the input closes.
    The parser never accumulates Events.
  - **Backpressure.** The channel returned by `Parse` is
    unbuffered (or buffered to 1 — decided at implementation
    time). The pipeline's BufferSize handles inter-stage
    queueing; the parser itself blocks on send so a slow
    downstream stage propagates backpressure naturally.
  - **Cancellation.** Each loop iteration checks `ctx.Done()`
    before reading the next line and before sending an Event.
- **Tests** (extends `generic_test.go`):
  - `TestGeneric_ParseSingleError`: input is five innocuous lines
    + one `ERROR: thing broke` + three innocuous lines; assert
    one Event with `Severity=error`, `Kind=error_line`, three
    lines of context before, three after.
  - `TestGeneric_ParseMultipleEvents`: feed a fixture with three
    distinct anchor lines spaced far enough apart that contexts
    don't overlap; assert three Events with non-overlapping
    `Context` slices.
  - `TestGeneric_ParseOverlappingContexts`: feed two anchor lines
    one apart; assert both Events emit and their `Context`
    slices may share lines (no deduplication of context across
    Events).
  - `TestGeneric_ParsePanicAndException`: feed a Go panic line
    and a Python `Exception: foo`; assert one Event each with
    `Kind=panic` and `Kind=exception` respectively.
  - `TestGeneric_ParseTracebackHeader`: feed a `Traceback (most
    recent call last):` line; assert one Event with
    `Kind=traceback`, severity error.
  - `TestGeneric_ParseExtractsLocation`: feed
    `ERROR foo.py:42: bad thing`; assert
    `Location={File:"foo.py", Line:42}`.
  - `TestGeneric_ParseLocationRequiresSlash`: feed
    `ERROR connection to db:5432 refused`; assert
    `Location == nil` (no slash → not a path).
  - `TestGeneric_ParseStripsANSIFromTitle`: feed
    `\x1b[31mERROR\x1b[0m: thing broke`; assert
    `Title == "ERROR: thing broke"`.
  - `TestGeneric_ParseBodyKeepsANSI`: same input as above; assert
    `Body[0]` retains the escape sequences so users see what
    actually arrived.
  - `TestGeneric_ParseInfoNotEmittedV1`: feed `INFO: starting
    server`; assert no Events emitted (info is empty in v1).
  - `TestGeneric_ParseStreaming`: use `testutil.SlowReader` to
    drip a fixture with three Events spread across the input;
    assert at least the first Event emerges before the source
    closes.
  - `TestGeneric_ParseDeterministic`: same input twice → byte-
    equal sequence of Events (property test, ties into the
    project's determinism invariant).
  - `TestGeneric_ParseBoundedMemory`: feed a 10MB synthetic
    stream of innocuous lines (no anchors); assert peak heap
    stays under a fixed ceiling — the same pattern as
    `TestPipeline_BoundedMemory_PeakSampling` from M2.3.
  - `TestGeneric_ParseContextCancellation`: cancel mid-stream;
    parser drains and exits; no goroutine leak.
- **Docs:**
  - Extend `docs/formats/generic.md`: the severity catalogue
    (table form), the kind values, an example input + Event
    output, the ANSI-strip rule, the location-extraction
    heuristic.
  - Add the new kinds (`error_line`, `warning_line`, `traceback`,
    `panic`, `exception`) to
    [SCHEMA.md § Kind values](./docs/formats/SCHEMA.md#kind-values)
    under a new `generic` section. Per the alignment rule, the
    schema doc lands in the same commit.

### M9.3 — Traceback / panic block accumulation ✅

`Kind=traceback` and `Kind=panic` Events are more useful when their
Body carries the full block, not just the anchor line. M9.3 extends
the scanner with a small block-accumulator that picks up
trailing lines until the block terminator.

- **DoD:**
  - When the scanner anchors a `traceback` or `panic` Event, it
    switches into block mode: subsequent lines are appended to
    the Event's `Body` until either:
    - A line that does not match the block's continuation regex.
    - The block exceeds `maxBlockLines = 100` lines (a hard cap
      to keep memory bounded under adversarial input).
    - EOF.
  - **Continuation regex per kind:**
    - `traceback`: lines matching `^\s+File "` (Python
      traceback indented frame), `^\s+at ` (JVM stack frame),
      `^\s*\.\.\. \d+ more$` (JVM "N more frames" tail), and
      anything matching `^\s` (an indented line; Python
      tracebacks indent the assertion and exception type).
    - `panic`: lines matching `^\s*goroutine \d+`, `^\s*0x[0-9a-f]+`
      (Go panic addresses), `^\s` (any indentation; Go panic
      stack lines are indented), and `^panic: ` repeats. Go
      panics often print `goroutine 1 [running]:` after `panic:`,
      so the accumulator continues across those.
  - **Frame extraction.** When the block terminates, a per-kind
    frame extractor runs over the captured Body:
    - `traceback`: regex match `File "(path)", line (\d+),
      in (func)` (Python) and `at (func)\((path):(\d+)\)`
      (JVM). One `StackFrame` per match.
    - `panic`: regex match `(path):(\d+)( \+0x[0-9a-f]+)?$`
      anchored at the end of a line. The function name is the
      preceding line's content trimmed of arguments
      (`func(0x123, 0x456)` → `func`).
    - Frames produced with `Vendor=false`; the M5 CollapseStage's
      `ClassifyFrames` repopulates `Vendor` after parse.
  - **`Title` re-derivation.** For `traceback` Events, after the
    block is captured, the Title is replaced with the last
    non-blank Body line (the actual exception message), matching
    the convention every specific format uses. For `panic`
    Events, the Title stays the original `panic: <message>` line.
  - **Streaming.** Block accumulation delays Event emission until
    the block terminates. The trailing-context window from M9.2
    still applies after the block: M9.3's Event includes the
    `contextLines` lines after the block ends, not after the
    anchor.
- **Tests:**
  - `TestGeneric_ParsePythonTracebackBlock`: a fixture with one
    Python traceback (anchor + 3 frame lines + final
    `KeyError:` line); assert Title is `KeyError: 'foo'`, Body
    contains every block line in order, `Frames` has 3 entries
    with correct File/Line/Function.
  - `TestGeneric_ParseGoPanicBlock`: a fixture with `panic:
    runtime error` + goroutine stack; assert Title is the panic
    message, Body contains the stack, `Frames` populated.
  - `TestGeneric_ParseJVMTracebackBlock`: an `Exception in
    thread "main" java.lang.NullPointerException` block; assert
    Title and Frames.
  - `TestGeneric_ParseBlockMaxLinesCap`: feed a 200-line indented
    block; assert Body has exactly `maxBlockLines` (=100) entries
    and a sentinel suffix `... [block truncated]` as the final
    Body line.
  - `TestGeneric_ParseBlockEndsOnDedent`: feed a traceback
    followed by a non-indented log line; assert the block ends
    cleanly and the next non-indented line becomes either
    context for the next Event or is discarded.
  - `TestGeneric_ParseBlockBoundedMemory`: feed a 1GB synthetic
    block of indented lines (no anchors after the first); assert
    peak heap stays under the same ceiling as M9.2 — proves the
    `maxBlockLines` cap.
- **Docs:**
  - Extend `docs/formats/generic.md`: side-by-side example of a
    Python traceback, a Go panic, and a JVM exception, each with
    the Event the scanner produces.

### M9.4 — Wire `--keep-warnings` and `--severity` filter semantics ✅

M9 is the first format whose v1 surface exposes both warnings and
errors. The `--keep-warnings` and `--severity` CLI flags (wired in
M8.2.x) need a defined interaction with the generic scanner.

- **DoD:**
  - The generic `Parse` reads two new fields from `opts`
    (`formats.ParseOpts` — extended to carry these in the same
    commit):
    - `MinSeverity event.Severity` — default
      `event.SeverityError`. Events whose severity is lower than
      `MinSeverity` are not emitted by the parser.
    - `KeepWarnings bool` — default false. When true, the
      effective minimum severity drops to `SeverityWarn`
      regardless of `MinSeverity`. This matches the
      [ARCHITECTURE.md § Flags](./ARCHITECTURE.md#flags) sketch
      where `--keep-warnings` is a one-shot bump for the common
      "errors only when errors exist, otherwise everything" case.
    - When `MinSeverity == SeverityInfo` and `KeepWarnings ==
      false`, the parser still emits warnings (the explicit
      `MinSeverity` wins over the default). Document the
      precedence in godoc.
  - **Filtering position.** Filtering happens inside the parser,
    not as a downstream Stage, because dropping a low-severity
    anchor line also frees its context window. This is the
    cheapest place to drop noise.
  - The specific formats (gotest in M10, pytest in M11, jest in
    M12, when they land) do not yet read these fields; M9.4 only
    wires them into `generic`. Future formats opt in by reading
    the same `ParseOpts` fields. SCHEMA.md notes this is a per-
    format opt-in, not a pipeline-wide guarantee, so consumers
    don't expect "this option always filters everything."
- **Tests:**
  - `TestGeneric_ParseMinSeverityError`: feed a fixture with one
    error and one warning, default opts; assert only the error
    Event emerges.
  - `TestGeneric_ParseKeepWarnings`: same fixture,
    `KeepWarnings=true`; assert both Events emerge.
  - `TestGeneric_ParseMinSeverityInfoEmitsWarnings`: even though
    v1 catalogue has no info patterns, asserting that setting
    `MinSeverity=SeverityInfo` doesn't suppress warnings; both
    error and warning Events emerge.
  - `TestGeneric_ParseFilterBeforeContext`: a warning anchor
    inside an error's context window does not become its own
    Event when warnings are filtered, but the line still appears
    in the error Event's `Context`. Proves filtering happens
    "drop the anchor but keep the surrounding lines as context"
    rather than "skip the line entirely."
- **Docs:**
  - Extend `docs/formats/generic.md` with the filtering
    semantics, including the `Context`-preservation rule above.
  - Note the per-format opt-in in
    [SCHEMA.md § Severity](./docs/formats/SCHEMA.md) (or a new
    section if one doesn't exist; decide at scoping time).

### M9.5 — Fixtures and ARCHITECTURE update ✅

Tie M9 off with the canonical fixture set per
[CONTRIBUTING.md § Adding a format](./CONTRIBUTING.md#adding-a-format)
and update the project's format-list / scope documents.

- **DoD:**
  - Ten fixtures under `internal/formats/generic/testdata/`,
    matching the v1 design's minimum-of-ten goal for the
    fallback:
    - `clean.input`: no severity hits, scanner emits nothing.
    - `single-error.input`: one `ERROR:` line in a sea of INFO.
    - `multi-error.input`: three errors at various points.
    - `python-traceback.input`: a clean Python traceback.
    - `go-panic.input`: a goroutine stack dump.
    - `jvm-exception.input`: a Java exception block.
    - `mixed-warn-error.input`: warnings and errors interleaved,
      exercises `--keep-warnings`.
    - `ansi-coloured.input`: lines with `\x1b[...m` colour codes,
      exercises the ANSI strip.
    - `nested-paths.input`: a line with `file.py:42:` plus a
      `host:port` pair, exercises the slash-required location
      heuristic.
    - `block-overflow.input`: a traceback longer than
      `maxBlockLines` to exercise the cap.
  - Each `.input` has a `.expected` companion in the JSON shape
    the format-test harness reads (the harness is the same one
    gotest uses in M10.5; extract it to `internal/formats/testing.go`
    so both formats share it). Per the alignment rule, the
    harness lands in the same commit it is first used by.
  - `formats.All()` (after the side-effect import) includes
    `generic` in alphabetical position — verified by
    `cmd/distill-ai/list_formats_test.go` once M8.4 lands.
  - ARCHITECTURE.md format list updated to mention `generic` as
    "shipped" rather than "fallback (not yet implemented)".
  - The `cmd/distill-ai/detect.go` help text fixed in M9.1
    rechecked here; the M9 milestone exit catches any drift if
    M9.1 and M9.5 land in separate commits.
- **Tests:**
  - `TestGeneric_Goldens`: harness walks `testdata/`, runs the
    parser on each `.input`, marshals Events to JSON, diffs
    against `.expected`. Run with `-update` to regenerate.
  - `TestGeneric_FixtureCount`: hard assertion that exactly the
    ten enumerated fixtures exist, so future drift is caught.
- **Docs:**
  - `docs/formats/generic.md` finalised: detection model, every
    parsed kind with an example, severity catalogue, the
    filtering semantics, the ten fixtures referenced by file
    name.
  - ARCHITECTURE.md updated per DoD.
  - README.md format list updated.

### M9 exit criteria

- All five sub-items ticked.
- `make check` clean; no race hits; no goroutine leaks.
- M9 milestone drift check: `formats.Get("generic")` returns the
  registered Format; `docs/formats/generic.md` exists and describes
  every Event kind the parser emits; SCHEMA.md's `generic` kind
  values list matches the parser's emitted kinds; the ten fixtures
  live under `internal/formats/generic/testdata/` with `*.input` +
  `*.expected` pairs; `cmd/distill-ai/detect.go` help text no
  longer claims "generic fallback is not yet registered"; the
  detector's `ErrNoFormat` return path now triggers only for
  `--strict` callers (the default path falls back to `generic`
  with `FellBackToGeneric=true`).
- The generic format is the safety net every other v1 format is
  measured against. If `cmd | distill-ai` produces zero Events on
  a real-world log shape that contains the word `ERROR`, M9 has
  a gap.

---

## M10 — gotest format ✅

The first real format parser, chosen ahead of pytest and jest because
gotest is the format this very project emits on every `make test` —
shipping it first turns distill-ai's own development loop into the
canonical dogfooding scenario. M10 implements `formats.Format` for
the Go test runner — detect by `--- FAIL:`, `FAIL\t<pkg>`, and
`=== RUN` markers; parse the `--- FAIL: TestName (Xs)` blocks the
default reporter emits; parse goroutine panic dumps as a distinct
`panic` Kind; parse `go vet` / build failures emitted before tests
run as `build_failure`; surface the race-detector report as a single
`race_condition` Event. Skip passing tests entirely.

Streaming-first per ARCHITECTURE.md § Pipeline: every Event is
forwarded as the trailing newline of its block is consumed; the
parser never buffers the whole input. The integration suite already
carries `test/integration/testdata/fixtures/gotest-fail.input`, so
M10.5's positive-distillation integration test (per
[KNOWN_ISSUES.md § 6](./KNOWN_ISSUES.md)) plugs in without a new
fixture grab.

Cross-references
[ARCHITECTURE.md § Format plugin contract](./ARCHITECTURE.md#format-plugin-contract),
[docs/formats/SCHEMA.md § Kind values](./docs/formats/SCHEMA.md#kind-values).
SCHEMA.md already names gotest's four Event kinds (`test_failure`,
`panic`, `build_failure`, `race_condition`); M10 makes them real.

M10 builds on M1 (`Format` interface, `formats.Register`), M3
(autodetection — gotest must return `Confidence=1.0` on a clear hit,
< 0.6 on ambiguous input), M5 (StackFrame classification — Go stacks
contain heavy `/src/runtime/`, `pkg/mod/`, and `/vendor/` runs that
`internal/event/collapse.go` already classifies as vendor; M10 only
extracts the frames), and M7 (the output encoders that render gotest
Events). Each item below lists Definition of Done, required tests,
and required doc updates per the
[alignment rule](./.opencode/rules/alignment.md).

### M10.1 — `internal/formats/gotest/gotest.go`: skeleton + Detect ✅

Land the package, register it, and implement `Format.Detect`. No
parsing yet — `Parse` returns an empty channel — so M3 autodetection
exercises the new format end-to-end before the heavy parser arrives.

- **DoD:**
  - New package `internal/formats/gotest` exporting `Format` (a
    value type implementing `formats.Format`).
  - `func init() { formats.Register(Format{}) }` so the registry
    picks it up automatically.
  - `Name() string` returns `"gotest"`.
  - `Detect(sample []byte) Confidence`:
    - `1.0` when the sample contains `--- FAIL: ` at the start of
      a line, or `^FAIL\t` followed by an importable Go package
      path (matching `\w+([./]\w+)*` after the tab).
    - `1.0` when the sample contains `^=== RUN   ` (gotest's
      `-v` mode header).
    - `0.8` when the sample contains a goroutine-dump header
      (`^goroutine \d+ \[\w+\]:`) plus a Go file path
      (`\.go:\d+`). This catches bare panics emitted by `go run`
      with no surrounding test output.
    - `0.0` otherwise.
    - The threshold constants live as package-level
      `confidenceClearMarker = 1.0`, `confidenceFuzzy = 0.8`
      mirroring the convention used by every other format.
  - **Confidence-tie precedence.** `FAIL\t` is generic enough that
    other Go tooling (e.g., `go vet`'s `FAIL` summary line) could
    emit similar shapes. The detect path requires the `FAIL` line
    to be tab-separated from a token that looks like a Go package
    path (contains `/` or matches `\w+(\.\w+)*`) before raising to
    `1.0`. Documented in the package godoc and verified by
    `TestGotest_DetectFailRequiresPackageToken`.
  - `Parse(ctx, r, opts)` returns an immediately-closed channel
    with `nil` error for M10.1; M10.2–M10.4 fill it in. The early
    stub lets autodetect and CLI plumbing work against gotest
    before the real parser lands.
- **Tests** (`internal/formats/gotest/gotest_test.go`):
  - `TestGotest_DetectFailMarker`: feed
    `--- FAIL: TestLogin (0.02s)`; assert `Confidence == 1.0`.
  - `TestGotest_DetectFailPackage`: feed
    `FAIL\tgithub.com/vail130/distill-ai/internal/event\t1.234s`;
    assert `Confidence == 1.0`.
  - `TestGotest_DetectRunHeader`: feed `=== RUN   TestFoo`; assert
    `Confidence == 1.0`.
  - `TestGotest_DetectFailRequiresPackageToken`: feed
    `FAIL: rebooting node`; assert `Confidence < 1.0`.
  - `TestGotest_DetectGoroutineDump`: feed a fragment beginning
    with `goroutine 1 [running]:` followed by a line referencing
    a `.go:42` location; assert `Confidence == 0.8`.
  - `TestGotest_DetectNegative`: feed a Python traceback; assert
    `Confidence == 0.0`.
  - `TestGotest_RegisteredAtInit`: import the package for its
    side effect, then call `formats.Get("gotest")` and assert
    `(format, true)`.
  - `TestGotest_ParseEmptyStub`: ensure `Parse` returns a closed
    channel without error so M3 detection paths work end-to-end
    against the stubbed parser.
- **Docs:**
  - Godoc on `Format`, `Detect`, `Parse`, and the confidence
    constants.
  - New `docs/formats/gotest.md` with the section skeleton (intro,
    detection markers, what's extracted, what's dropped, example
    I/O). M10.1 fills in detection + intro; M10.2–M10.4 extend.
  - Update [README.md](./README.md) format list to mention `gotest`
    as "shipped" once M10.5 lands; for M10.1 the entry says
    "detect-only, parser lands in M10.2".

### M10.2 — Parse `--- FAIL:` blocks (test_failure) ✅

The common case: `go test` (with or without `-v`). Each FAIL block
runs from a `--- FAIL: TestName (duration)` line to the next
`--- FAIL`/`--- PASS`/`--- SKIP`/`=== RUN`/blank-then-`FAIL\t`
delimiter. The parser emits one Event per block with
`Severity=SeverityError`, `Kind=test_failure`.

- **DoD:**
  - The parser is a `bufio.Scanner`-driven state machine over
    `r io.Reader`. No buffering of the whole input: as soon as a
    block's terminating delimiter is consumed, the Event is
    forwarded.
  - State machine states:
    `stateRunning` (initial; `=== RUN`, `=== PAUSE`, `=== CONT`,
    `--- PASS`, `--- SKIP` lines and per-test log output between
    them are discarded),
    `stateFailureHeader` (matching `--- FAIL: TestName (duration)`),
    `stateFailureBody` (accumulating body lines until the next
    block delimiter — another `--- FAIL`/`--- PASS`/`--- SKIP`,
    or `=== RUN`, or `FAIL\t<pkg>`, or `PASS` summary, or EOF),
    `stateSummary` (post-`FAIL\t<pkg>` and final `exit status N`;
    everything is discarded).
  - **Per-Event shape:**
    - `Severity = SeverityError`.
    - `Kind = "test_failure"`.
    - `Title` = the first body line that looks like a test
      assertion message — the conventional `<file>:<line>: <msg>`
      shape gotest uses for `t.Errorf`/`t.Fatalf`. Falls back to
      the trimmed `--- FAIL:` header line when no such message is
      found (rare; happens when the test panics within a
      subtest's goroutine).
    - `Location` = best-effort file:line extracted from the
      `<file>:<line>:` prefix on the assertion line. Path must
      contain `/` or end in `.go` so we don't match `host:port`
      pairs. Nil if no match.
    - `Body` = the verbatim block lines from the `--- FAIL:`
      header to the block terminator.
    - `Metadata["test_id"]` = the test name extracted from the
      `--- FAIL:` header (e.g., `TestLogin` or
      `TestLogin/subtest_returns_302`). Subtests use the
      forward-slash separator gotest itself emits.
    - `Metadata["package"]` = the most recent Go package name
      seen on a `=== RUN` header's `=== RUN   TestX` line or a
      `FAIL\tpkg` summary line, when known.
    - `Metadata["duration"]` = the duration string from the
      header (e.g., `0.02s`). Optional; absent for subtests that
      use the table-driven form without per-row durations.
    - `Frames` left nil in M10.2; frame extraction is M10.4.
  - **What's dropped.** Passing tests (`--- PASS:`), skipped
    tests (`--- SKIP:`), `=== RUN`/`=== PAUSE`/`=== CONT` lines
    not currently inside a failure block, per-test log output
    between passing tests, the `PASS` / `FAIL` summary line, and
    the final `exit status N` line. The parser never emits Events
    for any of these.
  - **Streaming.** Each Event is forwarded as soon as its block
    terminator is consumed.
  - **Backpressure.** The parser's send blocks on a slow
    downstream stage; pipeline `BufferSize` is the only buffer.
  - **Cancellation.** Each loop iteration checks `ctx.Done()`
    before reading the next line and before sending an Event.
- **Tests** (extends `gotest_test.go`):
  - `TestGotest_ParseSingleFailure`: fixture with one failing
    test; assert one Event with the expected title, test_id
    metadata, body, location.
  - `TestGotest_ParseMultiFailure`: three failures across two
    packages; three Events with correct `package` metadata on
    each.
  - `TestGotest_ParseSkipsPassing`: fixture with one pass and one
    fail; only the fail emits an Event.
  - `TestGotest_ParseSubtests`: fixture with table-driven
    subtests where two of N rows fail; assert one Event per
    failing subtest with `test_id` containing the subtest path
    (`TestParse/empty_input`, `TestParse/binary_input`).
  - `TestGotest_ParseDropsPerTestLogs`: fixture with `t.Logf`
    output interleaved between passing tests; assert no Event
    for the logs and the failure Event's Body doesn't pick up
    log lines from earlier tests.
  - `TestGotest_ParseStreaming`: use `testutil.SlowReader` to
    drip a multi-failure fixture; assert at least one Event
    arrives before the source closes.
  - `TestGotest_ParseDeterministic`: same input twice → byte-
    equal sequence of Events.
  - `TestGotest_ParseContextCancellation`: cancel mid-stream;
    parser drains and exits; no goroutine leak.
- **Docs:**
  - Extend `docs/formats/gotest.md`: example failure block
    (default reporter) + the Event it produces, the list of
    dropped artifacts (passes, skips, run headers, log lines),
    the subtest path convention.
  - No SCHEMA.md change — `test_failure` is already listed under
    gotest's kind values.

### M10.3 — Parse panic blocks and build failures ✅

Two distinct Event Kinds gotest emits outside the normal failure
flow: panics that escape a test goroutine, and build failures that
prevent tests from running at all. Each gets its own Kind so
downstream consumers can route on it.

- **DoD:**
  - **Panic blocks.** When the parser sees `^panic: ` (with or
    without an inside-failure-block context), it switches into
    `statePanicBody`. The block runs from the `panic:` line
    through the trailing `goroutine N [state]:` stack dump until
    either:
    - A line that doesn't match the panic-continuation pattern
      (`^\s` for indented stack lines, `^panic:` for chained
      panics from goroutines, `^goroutine \d+`, `^\t` for the
      `file.go:42 +0xNN` lines that follow each frame entry).
    - The hard cap `maxPanicLines = 200` (parallel to M9.3's
      block-overflow cap); excess lines replaced by a sentinel
      `Body` entry `... [panic block truncated]` and
      `Metadata["panic_truncated"] = "true"`.
    - EOF.
  - **Panic Event shape:**
    - `Severity = SeverityError`.
    - `Kind = "panic"`.
    - `Title` = the `panic:` line trimmed (e.g.,
      `panic: runtime error: index out of range [3] with length 2`).
    - `Location` = the first user-code frame's file:line — i.e.,
      the first frame whose path doesn't match the M5 vendor
      pattern catalogue's Go entries. Nil if every frame is
      vendor.
    - `Body` = the verbatim panic + goroutine dump lines.
    - `Metadata["test_id"]` populated when the panic occurred
      inside a failure block (the parser tracks the most recent
      `--- FAIL:` header); absent otherwise.
  - **Build failures.** When the parser sees a line matching the
    `go build` / `go vet` error shape — `^\S+\.go:\d+:\d+: ` at
    the top of input or after a `FAIL\t<pkg> [build failed]`
    summary — it emits an Event with `Kind="build_failure"`. One
    Event per distinct `.go:line:col:` location; the body is the
    full multi-line error message gotest prints (Go compiler
    errors often span 2–5 lines with context arrows).
  - **Build failure Event shape:**
    - `Severity = SeverityError`.
    - `Kind = "build_failure"`.
    - `Title` = the trimmed error message after the location
      prefix (e.g., `undefined: foo.Bar`).
    - `Location` = `{File, Line, Column}` parsed from the
      `.go:line:col:` prefix.
    - `Body` = the verbatim error and any continuation lines.
    - `Metadata["package"]` populated when a preceding
      `FAIL\t<pkg> [build failed]` line is in scope.
- **Tests:**
  - `TestGotest_ParsePanicTopLevel`: panic dump emitted without
    surrounding `--- FAIL:` (e.g., from `go run` or a test
    `TestMain` panic); one Event with `Kind="panic"`.
  - `TestGotest_ParsePanicInsideFailure`: panic emitted as part
    of a `--- FAIL:` block (e.g., `panic` during `t.Run`);
    asserts one Event with `Kind="panic"` and
    `test_id` populated. The surrounding `--- FAIL:` block's
    own test_failure Event is suppressed in favour of the more
    informative panic Event.
  - `TestGotest_ParsePanicMaxLines`: synthesise a 300-line
    goroutine dump; assert Body capped at 200, sentinel + flag
    present.
  - `TestGotest_ParseBuildFailure`: fixture with a `vet`/`build`
    error preventing tests from running; one Event per error
    location with `Kind="build_failure"`.
  - `TestGotest_ParseBuildFailureWithPackage`: fixture where the
    build error is followed by `FAIL\tpkg/path [build failed]`;
    assert `Metadata["package"]` is populated.
- **Docs:**
  - Extend `docs/formats/gotest.md`: side-by-side example of
    a panic block, a build failure, and the failure-vs-panic
    decision (panic inside `--- FAIL:` wins).
  - No SCHEMA.md change — `panic` and `build_failure` are
    already listed under gotest's kind values.

### M10.4 — Stack frames, race detector, reporter modes ✅

Populate `Event.Frames` from the goroutine dump, emit the
race-detector report as a single `race_condition` Event, and handle
the three reporter modes gotest emits.

- **DoD:**
  - **Frame extraction.** After a panic block terminates, a
    single regex pair sweeps the captured Body for goroutine
    frame pairs:
    `^(?P<fn>[\w./\-*()]+)\((?:0x[0-9a-f]+(?:,\s*)?)*\)$`
    (function name + arguments)
    followed by
    `^\t(?P<path>\S+\.go):(?P<line>\d+)(?:\s+\+0x[0-9a-f]+)?$`
    (indented file:line). The pair produces one `StackFrame`.
    `Vendor` is left false — the M5 CollapseStage's
    `ClassifyFrames` re-populates it via the Go entries in the
    vendor pattern catalogue (`/src/runtime/`, `pkg/mod/`,
    `/vendor/`).
  - **Frames are emitted only when at least one frame pair
    matches;** otherwise `Frames` stays nil. Matches the M11/M12
    rule.
  - **Race condition.** The race detector writes a distinctive
    block beginning with `==================` and ending with a
    second `==================`. M10.4 detects this block and
    emits one Event with:
    - `Severity = SeverityError`.
    - `Kind = "race_condition"`.
    - `Title` = the first non-divider line of the block, which
      gotest emits as `WARNING: DATA RACE` (a constant).
    - `Body` = the entire block, dividers included, capped at
      `maxRaceLines = 300` lines (race reports are larger than
      typical panics — they include two goroutines' worth of
      stack — so the cap is correspondingly larger).
    - `Frames` extracted from the two contained goroutine
      dumps (a race report contains two stacks; both are
      merged into one Frames slice, in source order, with
      `Metadata["race_goroutines"] = "2"` indicating the count
      for consumers that want to render them separately).
    - `Metadata["test_id"]` populated when the race fires inside
      a `--- FAIL:` block.
  - **Reporter modes:**
    - **Default reporter:** the canonical shape M10.2 targets.
    - **`-v` reporter:** adds `=== RUN`, `=== PAUSE`, `=== CONT`,
      `--- PASS` lines per test before any failure. The parser
      still anchors on `--- FAIL:` and drops the per-test
      indicator lines uniformly.
    - **`-json` mode:** structured JSON-per-line output
      (`{"Action":"fail",...}`). M10.4 detects this by looking
      for `^{"Time":` on the first non-blank input line, and
      parses each JSON line into an Event. The Action→Kind
      mapping is: `fail` → `test_failure`, `pass`/`skip` →
      dropped, `output` → buffered into the in-progress
      failure's Body, `bench` → dropped (v1 doesn't cover
      benchmarks; v1.1 backlog). Build failures in `-json` mode
      appear as `{"Action":"output","Output":"...","Test":""}`
      entries with the same `.go:line:col:` shape as
      non-`-json`, and the M10.3 build-failure logic applies.
- **Tests:**
  - `TestGotest_ParseExtractsFrames`: a panic fixture; assert the
    Event's `Frames` has at least one entry whose File/Line match
    the bottom user-code frame.
  - `TestGotest_ParseFramesEmptyOnTrivialPanic`: a `panic("msg")`
    fixture with no goroutine dump (constructed; gotest always
    emits one in practice, but defensive); assert `Frames == nil`
    and the Event still emits.
  - `TestGotest_ParseRaceCondition`: fixture from a real race
    detector run; one Event with `Kind="race_condition"`, two
    stacks in Frames, `race_goroutines == "2"`.
  - `TestGotest_ParseVerboseSameAsTerse`: same logical failure
    rendered once with `-v` and once without; the Events emitted
    are identical (modulo `=== RUN`/`--- PASS` lines that the
    parser drops).
  - `TestGotest_ParseJSONMode`: fixture from `go test -json`
    output; assert one Event per fail Action with the correct
    test_id, package, and Body assembled from the corresponding
    output Actions.
- **Docs:**
  - Extend `docs/formats/gotest.md`: the three reporter modes
    side by side, frame-extraction rule, when `Frames` is
    populated vs nil, the race-detector example, the `-json`
    mapping table.

### M10.5 — Fixtures, ARCHITECTURE update, and integration coverage ✅

Tie M10 off with the canonical fixture set per
[CONTRIBUTING.md § Adding a format](./CONTRIBUTING.md#adding-a-format),
the format-list update, and the first positive end-to-end
integration test (addresses
[KNOWN_ISSUES.md § 6](./KNOWN_ISSUES.md)).

- **DoD:**
  - Eight fixtures under `internal/formats/gotest/testdata/`,
    matching the v1 minimum for a real format:
    `clean.input` (all green, parser emits nothing),
    `single-fail.input`,
    `multi-fail.input` (two failures across one package),
    `subtests.input` (table-driven failure with subtest path),
    `panic.input` (panic block with goroutine dump),
    `race.input` (race-detector report),
    `build-failure.input` (`go vet`-style error before tests run),
    `json.input` (`go test -json` mode).
  - Each `.input` has a `.expected` companion in the JSON shape
    the shared format-test harness reads (the harness lands in
    M9.5 / `internal/formats/testing.go`; M10.5 is its first
    consumer).
  - The harness supports `go test -update ./...` to regenerate
    fixtures, matching the output-package pattern.
  - ARCHITECTURE.md format list updated to mention `gotest` as
    "shipped" rather than "planned".
  - `formats.All()` (after the side-effect import) includes
    gotest in alphabetical position — verified by extending
    `cmd/distill-ai/list_formats_test.go`.
  - README format list updated alongside ARCHITECTURE.
  - `.opencode/skills/distill-output/SKILL.md` gains a "Distil a
    gotest run" recipe replacing the existing pre-M9 form, per
    [CONTRIBUTING.md § Adding a format step 10](./CONTRIBUTING.md#adding-a-format).
    The recipe shows `make test 2>&1 | ./bin/distill-ai`
    producing distilled output — the canonical dogfood loop.
  - **Integration suite** gains a new test
    `TestBinary_GotestEndToEndProducesOutput` that feeds
    `test/integration/testdata/fixtures/gotest-fail.input` to
    the binary via stdin, asserts exit 0, and asserts a
    substring of the expected Event title appears on stdout.
    Closes [KNOWN_ISSUES.md § 6](./KNOWN_ISSUES.md) for gotest;
    M11.5 / M12.5 do the same for their formats.
  - The pre-M10 `TestBinary_DetectGotestFixtureFallsThrough`
    style assertion is replaced with a positive-detection
    assertion in the same commit.
- **Tests:**
  - `TestGotest_Goldens`: harness walks `testdata/`, runs the
    parser on each `.input`, marshals Events to JSON, diffs
    against `.expected`. Run with `-update` to regenerate.
  - `TestGotest_FixtureCount`: hard assertion that exactly the
    eight enumerated fixtures exist, so future drift is caught.
  - `TestBinary_GotestEndToEndProducesOutput` (integration
    suite, per the DoD).
- **Docs:**
  - `docs/formats/gotest.md` finalised: detection markers, every
    parsed event kind with an example, what's dropped, the
    eight fixtures referenced by file name, the `-json` mapping
    table.
  - ARCHITECTURE.md updated per DoD.
  - README.md format list updated.
  - SKILL.md recipe added.

### M10 exit criteria

- All five sub-items ticked.
- `make check` clean; no race hits; no goroutine leaks.
- M10 milestone drift check: `formats.Get("gotest")` returns the
  registered Format; `docs/formats/gotest.md` exists and describes
  every Event kind the parser emits; SCHEMA.md's gotest kind values
  list (`test_failure`, `panic`, `build_failure`,
  `race_condition`) matches the parser's emitted kinds exactly —
  no extras, no omissions; the eight fixtures live under
  `internal/formats/gotest/testdata/` with `*.input` + `*.expected`
  pairs; the SKILL.md gotest recipe is present; the integration
  suite's gotest end-to-end test passes.
- Gotest is the first format to ship end-to-end and the format the
  project dogfoods on its own test runs. M11 (pytest) and M12
  (jest) re-use the shape M10 establishes — by the time M11 lands
  the format-test harness, the docs scaffolding, and the SKILL.md
  recipe pattern are all proven.

---

## M11 — pytest format

The second real format parser, modelled on M10's gotest implementation
shape. M11 implements `formats.Format` for pytest — detect by terminal
markers, parse `=== FAILURES ===` and `=== ERRORS ===` blocks, emit
one Event per failure or collection error, skip passing tests entirely.
Pytest ships second because it's the most heavily-used non-Go test
format in the agent-debugging ecosystem; the project itself doesn't
emit pytest output, so the format also serves as the cross-check that
M10's harness extracted in `internal/formats/testing.go` actually
generalises beyond gotest's shape.

Streaming-first per ARCHITECTURE.md § Pipeline: every Event is
forwarded as the trailing newline of its block is consumed; the parser
never buffers the whole input.

Cross-references
[ARCHITECTURE.md § Format plugin contract](./ARCHITECTURE.md#format-plugin-contract),
[docs/formats/SCHEMA.md § Kind values](./docs/formats/SCHEMA.md#kind-values).
SCHEMA.md already names pytest's four Event kinds (`test_failure`,
`test_error`, `collection_error`, `warning`); M11 makes them real.

M11 builds on M1 (`Format` interface, `formats.Register`), M3
(autodetection — pytest must return `Confidence=1.0` on a clear hit,
< 0.6 on ambiguous input), M7 (the output encoders that render the
Events this format emits), M9 (generic remains the fallback if
pytest's confidence drops below 0.6), and M10 (the format-test
harness now living in `internal/formats/testing.go`, the SKILL.md
recipe pattern, the integration-suite end-to-end pattern from
`TestBinary_GotestEndToEndProducesOutput`). Each item below lists
Definition of Done, required tests, and required doc updates per the
[alignment rule](./.opencode/rules/alignment.md).

### M11.1 — `internal/formats/pytest/pytest.go`: skeleton + Detect ✅

Land the package, register it, and implement `Format.Detect`. No
parsing yet — `Parse` returns an empty channel — so M3 autodetection
exercises the new format end-to-end before the heavy parser arrives.

- **DoD:**
  - New package `internal/formats/pytest` exporting `Format` (a
    value type implementing `formats.Format`).
  - `func init() { formats.Register(Format{}) }` so the registry
    picks it up automatically.
  - `Name() string` returns `"pytest"`.
  - `Detect(sample []byte) Confidence`:
    - `1.0` when the sample contains `=== test session starts ===`
      or `=== FAILURES ===`.
    - `0.8` when the sample contains a `>` assertion line plus
      either `conftest.py` or `pytest.ini` fragments.
    - `0.0` otherwise.
    - The threshold constants live as package-level
      `confidenceClearMarker = 1.0`, `confidenceFuzzy = 0.8`
      matching the gotest precedent from M10.
  - `Parse(ctx, r, opts)` returns an immediately-closed channel
    with `nil` error for M11.1; M11.2–M11.4 fill it in. The early
    stub lets autodetect and CLI plumbing work against pytest
    before the real parser lands.
- **Tests** (`internal/formats/pytest/pytest_test.go`):
  - `TestPytest_DetectClearMarker`: feed the literal
    `=== test session starts ===` line; assert `Confidence == 1.0`.
  - `TestPytest_DetectFailuresMarker`: feed `=== FAILURES ===`;
    assert `1.0`.
  - `TestPytest_DetectFuzzy`: feed a chunk containing both
    `conftest.py` and an `>` assertion line; assert `0.8`.
  - `TestPytest_DetectNegative`: feed unrelated text (a Go test
    log, exercising the M10 disambiguation); assert `0.0`.
  - `TestPytest_RegisteredAtInit`: import the package for its side
    effect, then call `formats.Get("pytest")` and assert
    `(format, true)`.
  - `TestPytest_ParseEmptyStub`: ensure `Parse` returns a closed
    channel without error so M3 detection paths work end-to-end
    against the stubbed parser.
- **Docs:**
  - Godoc on `Format`, `Detect`, `Parse`, and the confidence
    constants.
  - New `docs/formats/pytest.md` with the section skeleton (intro,
    detection markers, what's extracted, what's dropped, example
    I/O). M11.1 fills in detection + intro; M11.2–M11.4 extend.
  - Update [README.md](./README.md) format list to mention
    `pytest` as "detect-only, parser lands in M11.2" for M11.1;
    M11.5 promotes it to "shipped".

### M11.2 — Parse `=== FAILURES ===` blocks (long-form traceback) ✅

The common case: `pytest --tb=long` (the default). Each FAILURE block
runs from the underlined test name to the next block delimiter; the
parser emits one Event per block with `Severity=SeverityError`,
`Kind=test_failure`.

- **DoD:**
  - The parser is a `bufio.Scanner`-driven state machine over
    `r io.Reader`. No buffering of the whole input: as soon as a
    block's terminating delimiter is consumed, the Event is
    forwarded.
  - State machine states:
    `stateSession` (pre-FAILURES, lines discarded),
    `stateFailureHeader` (matching `___ test_id ___` underline),
    `stateFailureBody` (accumulating body until next header /
    divider), `stateSummary` (`=== short test summary info ===`
    and beyond).
  - **Per-Event shape:**
    - `Severity = SeverityError`.
    - `Kind = "test_failure"`.
    - `Title` = the assertion / exception line (first non-blank
      line starting with `E   `, falling back to the last body
      line if no `E   ` line exists).
    - `Location` = the file:line printed in the traceback's
      bottom frame (`>   assert ...` line preceded by
      `path/to/file.py:LN:`).
    - `Body` = the verbatim block lines including the underlined
      test name, the assertion, and the traceback.
    - `Metadata["test_id"]` = the test ID parsed from the
      underlined header (e.g.,
      `tests/api/test_auth.py::test_login_redirect`).
    - Frames are **not** populated in M11.2; the bare traceback
      lines live in `Body`. Frame extraction is M11.4 alongside
      `--tb=short`.
  - **What's dropped.** All passing tests, dots, progress lines,
    and the `===` dividers between sections are dropped on the
    floor — they never produce Events.
- **Tests** (extends `pytest_test.go`):
  - `TestPytest_ParseSingleFailure`: a fixture with one failing
    test; assert one Event with the expected title, location, and
    body.
  - `TestPytest_ParseMultiFailure`: three failures; three Events,
    each with the correct test_id.
  - `TestPytest_ParseSkipsPassing`: a fixture with one pass and
    one fail; only the fail emits an Event.
  - `TestPytest_ParseStreaming`: use `testutil.SlowReader` to drip
    a multi-failure fixture; assert at least one Event arrives
    before the source closes.
  - `TestPytest_ParseDeterministic`: same input twice → byte-equal
    sequence of Events.
  - `TestPytest_ParseContextCancellation`: cancel mid-stream;
    parser drains and exits; no goroutine leak.
- **Docs:**
  - Extend `docs/formats/pytest.md`: example failure block + the
    Event it produces.
  - No SCHEMA.md change — `test_failure` is already listed under
    pytest's kind values.

### M11.3 — Parse `=== ERRORS ===` and collection errors ✅

Errors are pytest's term for failures before a test can run: fixture
errors, import errors, syntax errors in test modules, missing
conftest pieces.

- **DoD:**
  - When the state machine sees `=== ERRORS ===`, it transitions
    to a `stateErrorBody` shape parallel to `stateFailureBody`.
  - One Event per error block with:
    - `Severity = SeverityError`.
    - `Kind = "test_error"` for in-test errors like a fixture
      failure.
    - `Kind = "collection_error"` when the error is in the
      collection phase — detected by the surrounding marker
      `=== ERRORS ===` appearing **before** any
      `=== test session starts ===` failures section, or by the
      error block header mentioning "during collection".
    - `Title` = the error type and message (e.g.,
      `fixture 'db' not found`).
    - `Location` = the file:line printed in the error traceback.
    - `Body` = the verbatim error block.
    - `Metadata["test_id"]` populated when the error is per-test
      (absent for top-level collection errors).
- **Tests:**
  - `TestPytest_ParseFixtureError`: fixture-not-found case; one
    Event with `Kind="test_error"`.
  - `TestPytest_ParseCollectionError`: conftest.py syntax error;
    one Event with `Kind="collection_error"`, no `test_id`.
  - `TestPytest_ParseErrorAndFailureMix`: fixture with one error
    block and one failure block; two Events with the right Kinds.
- **Docs:**
  - Extend `docs/formats/pytest.md` with the collection-error
    example and the difference between `test_error` and
    `collection_error`.

### M11.4 — Stack frame extraction and `--tb` shape handling

Populate `Event.Frames` from the traceback and handle pytest's three
non-default `--tb` settings (`short`, `line`, `native`).

- **DoD:**
  - Frame extraction runs after the block-body accumulator captures
    the verbatim lines: a small regex matches `(path)(:\d+):.*`
    traceback lines and emits one `StackFrame` per match. The
    `Vendor` flag is left false — the M5 CollapseStage takes over
    from there. Frames are emitted only when at least one line
    matches; otherwise `Frames` stays nil. Matches the M10 rule.
  - `--tb=short` produces a compact `file:line: message`
    traceback; the parser detects it by the absence of indented
    continuation lines after the test header and still emits one
    frame.
  - `--tb=line` emits a single-line summary per failure. The
    parser falls back to `Body=[that line]` and `Frames=nil`.
  - `--tb=native` is treated identically to `--tb=long` — Python's
    own traceback shape — and the existing extraction works.
  - The state machine handles `-v` and non-`-v` shapes uniformly:
    the only difference is the test-collection lines at the top,
    which are discarded either way.
  - Warning blocks (`=== warnings summary ===`) are parsed into
    Events with `Severity=SeverityWarn`, `Kind="warning"`. Each
    warning is one Event; the warning's source-file location is
    extracted into `Location` when present.
- **Tests:**
  - `TestPytest_ParseExtractsFrames`: a default `--tb=long`
    fixture yields Events with `len(Frames) > 0`, the bottom
    frame being user code.
  - `TestPytest_ParseTbShort`: same logical failure as above but
    with `--tb=short` output; one Event with one frame.
  - `TestPytest_ParseTbLine`: `--tb=line` output; Event has
    `Frames=nil` and the one-line summary in `Body`.
  - `TestPytest_ParseVerboseSameAsTerse`: same fixture rendered
    once with `-v` and once without; the Events emitted are
    identical (modulo the test_id appearing in both paths).
  - `TestPytest_ParseWarnings`: a fixture with `--tb=long` plus a
    `=== warnings summary ===` block emits the failure Events
    first, then the warning Events.
- **Docs:**
  - Extend `docs/formats/pytest.md`: the four `--tb` shapes side
    by side, the rule for when Frames is populated vs nil, the
    warning handling.

### M11.5 — Fixtures, ARCHITECTURE update, and integration coverage

Tie M11 off with the canonical fixture set per
[CONTRIBUTING.md § Adding a format](./CONTRIBUTING.md#adding-a-format),
the format-list update, and the second positive end-to-end
integration test (mirroring M10.5; addresses
[KNOWN_ISSUES.md § 6](./KNOWN_ISSUES.md) for pytest).

- **DoD:**
  - Eight fixtures under `internal/formats/pytest/testdata/`:
    `clean.input` (no failures, parser emits nothing),
    `single-fail.input`, `multi-fail.input`, `errors.input`,
    `parametrised.input`, `xfail-xpass.input`, `warnings.input`,
    `collection-error.input`. Each has a `*.expected` companion
    in the JSON shape the shared format-test harness reads (the
    harness already lives in `internal/formats/testing.go` from
    M9.5 / M10.5).
  - The harness's `go test -update ./...` mode works for pytest
    out of the box because the harness is generic.
  - ARCHITECTURE.md format list updated to mention `pytest` as
    "shipped" rather than "planned".
  - `formats.All()` (after the side-effect import) includes
    pytest in alphabetical position — verified by extending
    `cmd/distill-ai/list_formats_test.go`.
  - README format list updated alongside ARCHITECTURE.
  - `.opencode/skills/distill-output/SKILL.md` gains a "Distil a
    pytest run" recipe paralleling the M10 gotest recipe, per
    [CONTRIBUTING.md § Adding a format step 10](./CONTRIBUTING.md#adding-a-format).
  - **Integration suite** gains a positive-detection test
    `TestBinary_PytestEndToEndProducesOutput` using the existing
    `test/integration/testdata/fixtures/pytest-fail.input`
    fixture. Closes
    [KNOWN_ISSUES.md § 6](./KNOWN_ISSUES.md) for pytest.
  - The pre-M11 `TestBinary_DetectPytestFixtureFallsThrough`
    assertion is replaced with a positive-detection assertion in
    the same commit.
- **Tests:**
  - `TestPytest_Goldens`: harness walks `testdata/`, runs the
    parser on each `.input`, marshals Events to JSON, diffs
    against `.expected`. Run with `-update` to regenerate.
  - `TestPytest_FixtureCount`: hard assertion that exactly the
    eight enumerated fixtures exist, so future drift is caught.
  - `TestBinary_PytestEndToEndProducesOutput` (integration
    suite, per the DoD).
- **Docs:**
  - `docs/formats/pytest.md` finalised: detection markers, every
    parsed event kind with an example, what's dropped, the eight
    fixtures referenced by file name.
  - ARCHITECTURE.md updated per DoD.
  - README.md format list updated.
  - SKILL.md recipe added.

### M11 exit criteria

- All five sub-items ticked.
- `make check` clean; no race hits; no goroutine leaks.
- M11 milestone drift check: `formats.Get("pytest")` returns the
  registered Format; `docs/formats/pytest.md` exists and
  describes every Event kind the parser emits; SCHEMA.md's pytest
  kind values list (`test_failure`, `test_error`,
  `collection_error`, `warning`) matches the parser's emitted
  kinds exactly — no extras, no omissions; the eight fixtures
  live under `internal/formats/pytest/testdata/` with `*.input` +
  `*.expected` pairs; the SKILL.md pytest recipe is present; the
  integration suite's pytest end-to-end test passes.
- M11 is the second format to ship end-to-end after gotest.
  Confirms the format-test harness extracted in M10.5 generalises
  beyond Go's shape — pytest's `=== FAILURES ===` block grammar
  and Python traceback structure are sufficiently different from
  gotest's `--- FAIL:` blocks that a passing M11 demonstrates the
  abstraction works.

---

## M12 — jest format

The third real format parser, modelled on M10 (gotest) and M11
(pytest). M12 implements `formats.Format` for jest — detect by jest's
distinctive `●` failure markers and `FAIL`/`PASS` line prefixes, parse
per-test failure blocks emitted by the default reporter and the
`--verbose` reporter, emit one Event per failure (test failure,
snapshot mismatch, or suite-level error), drop everything else
(passing tests, coverage tables, console.log noise) on the floor.
Streaming-first per ARCHITECTURE.md § Pipeline: every Event is
forwarded as the trailing terminator of its block is consumed; the
parser never buffers the whole input.

Cross-references
[ARCHITECTURE.md § Format plugin contract](./ARCHITECTURE.md#format-plugin-contract),
[docs/formats/SCHEMA.md § Kind values](./docs/formats/SCHEMA.md#kind-values).
SCHEMA.md already names jest's three Event kinds (`test_failure`,
`snapshot_mismatch`, `suite_error`); M12 makes them real.

M12 builds on M1 (`Format` interface, `formats.Register`), M3
(autodetection — jest must return `Confidence=1.0` on a clear marker,
< 0.6 on ambiguous input), M5 (StackFrame classification — jest
stacks contain heavy `node_modules/` runs that
`internal/event/collapse.go` already classifies as vendor; M12 only
extracts the frames), M7 (the encoders that render jest Events), M9
(generic remains the fallback if jest's confidence drops below 0.6),
and the shared format-test harness now in `internal/formats/testing.go`
(extracted in M9.5, exercised by M10.5 and M11.5). Each item below
lists Definition of Done, required tests, and required doc updates
per the [alignment rule](./.opencode/rules/alignment.md).

### M12.1 — `internal/formats/jest/jest.go`: skeleton + Detect

Land the package, register it, and implement `Format.Detect`. No
parsing yet — `Parse` returns an empty channel — so M3 autodetection
exercises the new format end-to-end before the heavy parser arrives.

- **DoD:**
  - New package `internal/formats/jest` exporting `Format` (a value
    type implementing `formats.Format`, matching the M10/M11 shape).
  - `func init() { formats.Register(Format{}) }` so the registry
    picks it up automatically.
  - `Name() string` returns `"jest"`.
  - `Detect(sample []byte) Confidence`:
    - `1.0` when the sample contains either of jest's distinctive
      markers on a single line:
      `^\s*● ` (the bullet that prefixes every failure block in the
      default reporter), or
      `^(PASS|FAIL) ` followed by a path matching jest's per-file
      summary lines (e.g., `FAIL src/auth.test.js`).
    - `0.8` when the sample contains a `Tests:` summary line
      matching `^Tests:\s+\d+ (passed|failed|skipped|total)`
      together with at least one mention of `jest` or a
      `.test.js`/`.test.ts`/`.spec.js`/`.spec.ts` filename.
    - `0.0` otherwise.
    - The constants live as package-level
      `confidenceClearMarker = 1.0` and `confidenceFuzzy = 0.8`
      matching the M10 / M11 precedent.
  - **Confidence-tie precedence.** Jest's `FAIL` prefix is generic
    enough that other test runners (e.g., `mocha --reporter min`)
    could emit similar lines. To avoid ties on ambiguous samples,
    the Detect path requires the `FAIL`/`PASS` line to be followed
    by a token that looks like a file path (contains `/` or `\` or
    ends in `.test.{js,ts,jsx,tsx}`/`.spec.{js,ts,jsx,tsx}`). This
    constraint is documented in the package godoc and verified by
    `TestJest_DetectFailRequiresPathToken`.
  - `Parse(ctx, r, opts)` returns an immediately-closed channel
    with `nil` error for M12.1; M12.2–M12.4 fill it in. The early
    stub lets autodetect and CLI plumbing work against jest before
    the real parser lands.
- **Tests** (`internal/formats/jest/jest_test.go`):
  - `TestJest_DetectBulletMarker`: feed a sample containing
    `  ● Auth › login redirects to dashboard`; assert
    `Confidence == 1.0`.
  - `TestJest_DetectFailWithPath`: feed `FAIL src/auth.test.js`;
    assert `Confidence == 1.0`.
  - `TestJest_DetectFailRequiresPathToken`: feed `FAIL: rebooting`
    (a plain `FAIL` line with no path token); assert
    `Confidence < 1.0` so the format doesn't claim arbitrary
    output.
  - `TestJest_DetectFuzzy`: feed `Tests: 1 failed, 2 passed, 3
    total` plus a fragment mentioning `auth.test.js`; assert
    `Confidence == 0.8`.
  - `TestJest_DetectNegative`: feed unrelated text (a Go build
    log); assert `Confidence == 0.0`.
  - `TestJest_RegisteredAtInit`: import the package for its side
    effect, then call `formats.Get("jest")` and assert
    `(format, true)`.
  - `TestJest_ParseEmptyStub`: ensure `Parse` returns a closed
    channel without error so M3 detection paths work end-to-end
    against the stubbed parser.
- **Docs:**
  - Godoc on `Format`, `Detect`, `Parse`, and the confidence
    constants.
  - New `docs/formats/jest.md` with the section skeleton (intro,
    detection markers, what's extracted, what's dropped, example
    I/O). M12.1 fills in detection + intro; M12.2–M12.4 extend.
  - Update [README.md](./README.md) format list to mention `jest`
    as "detect-only, parser lands in M12.2" for M12.1; M12.5
    promotes it to "shipped".

### M12.2 — Parse failure blocks (test_failure)

The common case: a default-reporter failure block runs from a `●`
line to the next `●` line, the next file's `PASS`/`FAIL` header, or
the trailing `Tests: ... summary` block. The parser emits one Event
per failure with `Severity=SeverityError`, `Kind=test_failure`.

- **DoD:**
  - The parser is a `bufio.Scanner`-driven state machine over
    `r io.Reader`. No buffering of the whole input: as soon as a
    block's terminating delimiter (next `●`, file header,
    `Test Suites:` summary, or EOF) is consumed, the in-flight
    Event is forwarded.
  - State machine states:
    `stateRunning` (initial; lines before the first `●` or `FAIL`
    are discarded), `stateFailureHeader` (consuming the
    `  ● Suite › Test name` line and any continuation lines that
    describe the failure path), `stateFailureBody` (accumulating
    body lines until the block terminator), `stateSummary`
    (everything after `Test Suites:` / `Tests:` lines is
    discarded).
  - **Per-Event shape:**
    - `Severity = SeverityError`.
    - `Kind = "test_failure"` for ordinary failures.
    - `Title` = the first non-blank body line that looks like an
      assertion message: `expect(...).toBe(...)`,
      `AssertionError`, `Error: ...`, or `Expected:`/`Received:`
      pair. Falls back to the trimmed `●` header line when no
      assertion is found.
    - `Location` = best-effort file:line extracted from the
      stack-frame block (the indented `at` lines at the bottom of
      the failure). Same heuristic as M9.2's location-extraction:
      path must contain `/` or `\` to avoid `host:port` collisions.
      Nil if no stack frame is present.
    - `Body` = the verbatim block lines from the `●` header to the
      block terminator, ANSI escape sequences stripped from the
      Title only (Body retains ANSI so the user sees what jest
      actually emitted; same rule as M9.2).
    - `Metadata["test_id"]` = the dot-joined test path extracted
      from the `●` header (e.g., `Auth › login redirects to
      dashboard` → `"Auth > login redirects to dashboard"`). The
      Unicode `›` is normalised to `>` so the value is grep-able.
    - `Metadata["suite_file"]` = the file path from the most
      recent `FAIL <path>` header, when known.
    - Frame extraction lives in M12.4; for M12.2 `Frames` stays
      nil.
  - **What's dropped.** Passing tests (`✓` lines, `PASS` headers,
    `console.log` output between tests, coverage tables under
    `--coverage`, the final timing line). The parser never emits
    Events for any of these.
  - **Streaming.** Each failure Event is forwarded as soon as its
    block terminator is consumed.
  - **Backpressure.** The parser's send blocks on a slow
    downstream stage; pipeline `BufferSize` is the only buffer.
  - **Cancellation.** Each loop iteration checks `ctx.Done()`
    before reading the next line and before sending an Event.
- **Tests** (extends `jest_test.go`):
  - `TestJest_ParseSingleFailure`: fixture with one failing test;
    assert one Event with the expected title, test_id metadata,
    body, location.
  - `TestJest_ParseMultiFailure`: three failures across two
    files; three Events with correct `suite_file` metadata on
    each.
  - `TestJest_ParseSkipsPassing`: fixture with one pass and one
    fail; only the fail emits an Event.
  - `TestJest_ParseDropsCoverageTable`: fixture with a coverage
    table appended after `Test Suites: 1 failed`; assert no
    coverage rows leak into any Event.
  - `TestJest_ParseDropsConsoleLog`: fixture with a `console.log`
    block between a passing test and a failing one; assert no
    Event for the `console.log` and the failure Event's Body
    doesn't pick up the log line.
  - `TestJest_ParseStreaming`: use `testutil.SlowReader` to drip a
    multi-failure fixture; assert at least one Event arrives
    before the source closes.
  - `TestJest_ParseDeterministic`: same input twice → byte-equal
    sequence of Events.
  - `TestJest_ParseStripsANSIFromTitle`: feed a `●` block whose
    assertion line is wrapped in red ANSI escapes; assert the
    Event's Title is the stripped form.
  - `TestJest_ParseContextCancellation`: cancel mid-stream;
    parser drains and exits; no goroutine leak.
- **Docs:**
  - Extend `docs/formats/jest.md`: example failure block (default
    reporter) + the Event it produces, the list of dropped
    artifacts (coverage, console.log, passing tests), the Unicode
    `›` → `>` normalisation rule.
  - No SCHEMA.md change — `test_failure` is already listed under
    jest's kind values.

### M12.3 — Snapshot mismatch handling (snapshot_mismatch)

Jest's snapshot diffs are multi-line, structured, and high-signal.
M12.3 detects them, emits a dedicated `snapshot_mismatch` kind so
downstream consumers can render them specially, and preserves the
diff in `Body` while extracting a short Title.

- **DoD:**
  - Inside `stateFailureBody`, when the accumulator sees the
    line `expect(received).toMatchSnapshot(...)` or
    `expect(received).toMatchInlineSnapshot(...)` followed by a
    `Snapshot:` / `Received:` pair, the Event's `Kind` is set to
    `"snapshot_mismatch"` instead of `"test_failure"`.
  - `Title` = `Snapshot mismatch: <Snapshot path>` when the
    snapshot is loaded from a file (jest prints the snapshot key
    on the `Snapshot name:` line); falls back to `Snapshot
    mismatch` when no name is printed (inline-snapshot case).
  - **Snapshot block accumulation.** After a `Snapshot:` line is
    matched, the accumulator captures every subsequent indented
    line (`+`-prefixed and `-`-prefixed diff lines plus context
    lines) until the indent drops back to the failure-block
    baseline or the block terminator fires. The captured diff
    becomes part of `Body` verbatim — no parsing or normalisation,
    so the downstream consumer can render or diff it as-is.
  - **`Metadata["snapshot_kind"]`** = `"file"` or `"inline"`,
    distinguishing `toMatchSnapshot` from `toMatchInlineSnapshot`.
  - **Max diff lines.** Hard cap at `maxSnapshotLines = 200` to
    keep memory bounded under adversarial input. When the cap
    fires, the last Body line is the sentinel `... [snapshot
    truncated]` (parallel to M9.3's block-overflow handling). A
    `Metadata["snapshot_truncated"] = "true"` field flags the
    case so encoders can render it. The cap is a package-level
    constant.
  - **Frames** are still extracted from the stack at the tail of
    the block (M12.4 wires the actual extraction); for M12.3 the
    Frames slice may be nil — snapshot mismatches usually have a
    stack pointing into jest internals which the M5 collapse
    stage handles.
- **Tests:**
  - `TestJest_ParseSnapshotMismatch`: fixture with a single
    `toMatchSnapshot` failure; assert
    `Kind="snapshot_mismatch"`, `metadata.snapshot_kind="file"`,
    Body contains every diff line.
  - `TestJest_ParseInlineSnapshotMismatch`: fixture with a
    `toMatchInlineSnapshot` failure; assert
    `metadata.snapshot_kind="inline"` and the Title falls back to
    the generic form.
  - `TestJest_ParseSnapshotMaxLines`: synthesise a fixture with a
    250-line snapshot diff; assert Body has exactly 200 lines
    plus the truncation sentinel, and
    `metadata.snapshot_truncated == "true"`.
  - `TestJest_ParseSnapshotAndOrdinaryFailure`: fixture with one
    snapshot mismatch and one ordinary assertion failure; assert
    two Events with the correct distinct Kinds in source order.
- **Docs:**
  - Extend `docs/formats/jest.md` with the snapshot section: how
    a snapshot block is recognised, file vs inline, the
    truncation rule, the `snapshot_kind` and
    `snapshot_truncated` metadata fields.
  - No SCHEMA.md change — `snapshot_mismatch` is already listed
    under jest's kinds. M12.3 ensures the parser emits it.

### M12.4 — Stack frame extraction, suite_error, and reporter modes

Populate `Event.Frames` from the trailing stack-frame block, emit
`suite_error` for failures that occur outside any test (top-level
imports, `beforeAll` hooks, file-load syntax errors), and handle the
`--verbose` reporter and CI reporter modes uniformly.

- **DoD:**
  - **Frame extraction.** After a block terminates (failure,
    snapshot, or suite error), a single regex sweeps the captured
    Body for lines matching
    `^\s+at\s+(?P<fn>[^(]+)\s+\((?P<path>[^:]+):(?P<line>\d+):(?P<col>\d+)\)$`
    (jest's standard frame shape) and
    `^\s+at\s+(?P<path>[^:]+):(?P<line>\d+):(?P<col>\d+)$` (no
    function name, common in async or bundled output). One
    `StackFrame` per match. `Vendor` is left false — the M5
    CollapseStage's `ClassifyFrames` re-populates it via the
    `node_modules/` pattern catalogue.
  - **Frames are emitted only when at least one line matches;**
    otherwise `Frames` stays nil. This matches the M10/M11 rule
    so encoders see consistent shape across formats.
  - **`suite_error` kind.** When the parser sees an `●` header
    whose path is the file itself (no test-name continuation) or
    the special heading `● Test suite failed to run`, the Event's
    `Kind` is set to `"suite_error"` rather than `"test_failure"`.
    `Metadata["test_id"]` is absent in this case (there is no
    individual test); `Metadata["suite_file"]` carries the file
    path.
  - **Reporter modes:**
    - **Default reporter:** the canonical shape M12.2 targets.
    - **`--verbose` reporter:** adds `✓` / `✗` lines per test
      before the summary; the parser still anchors on `●` markers
      and ignores the per-test indicator lines.
    - **`--ci` / `--reporters=default` CI mode:** drops colours
      (no ANSI), wraps lines differently. The ANSI strip is a
      no-op; line wrapping is handled because the state machine
      keys off content markers (`●`, `FAIL`, `Snapshot:`), not
      column positions. M12.4 captures fixtures for both modes
      so the test suite locks the behaviour.
    - **JSON reporter (`--json` / `--reporters=jest-json`)** is
      out of scope for v1 — it's a different format (structured
      JSON, no terminal output). M12 documents the gap; v1.1 can
      pick it up if demand surfaces.
- **Tests:**
  - `TestJest_ParseExtractsFrames`: a default-reporter failure
    fixture; assert the Event's `Frames` has at least one entry
    whose File/Line match the bottom traceback frame.
  - `TestJest_ParseFramesEmptyOnLineOnly`: a failure block whose
    stack has been suppressed (jest's `--noStackTrace`); assert
    `Frames == nil` and the failure Event still emits with its
    Title and Body.
  - `TestJest_ParseSuiteError`: fixture with a `● Test suite
    failed to run` block produced by a `require` error in the
    test file; assert one Event with `Kind="suite_error"`,
    `suite_file` populated, `test_id` absent.
  - `TestJest_ParseVerboseSameAsTerse`: same logical failure as
    `TestJest_ParseSingleFailure` but rendered with `--verbose`;
    assert the Events emitted are identical to the terse form
    (modulo content order — the verbose reporter adds `✓`/`✗`
    lines that the parser drops).
  - `TestJest_ParseCIReporter`: same fixture rendered in CI mode
    (no ANSI); assert identical Events to the default rendering.
- **Docs:**
  - Extend `docs/formats/jest.md`: the three reporter modes side
    by side, frame-extraction rule, when `Frames` is populated vs
    nil, the `suite_error` example, the explicit out-of-scope
    note for `--json`.

### M12.5 — Fixtures, ARCHITECTURE update, and integration coverage

Tie M12 off with the canonical fixture set per
[CONTRIBUTING.md § Adding a format](./CONTRIBUTING.md#adding-a-format),
the format-list update, and the third positive end-to-end
integration test (mirroring M10.5 and M11.5; addresses
[KNOWN_ISSUES.md § 6](./KNOWN_ISSUES.md) for jest).

- **DoD:**
  - Eight fixtures under `internal/formats/jest/testdata/`,
    matching the v1 design's minimum for a real format:
    `clean.input` (all green, parser emits nothing),
    `single-fail.input`,
    `multi-suite-fail.input` (two `FAIL` files, three failures
    total),
    `snapshot-mismatch.input` (file-based snapshot),
    `inline-snapshot-mismatch.input` (inline snapshot),
    `suite-error.input` (require-time failure),
    `verbose.input` (default failure but rendered with
    `--verbose` so the `✓`/`✗` lines exercise the parser's
    drop-rule),
    `console-log-noise.input` (failure with interleaved
    `console.log` output that must not leak into the Event).
  - Each `.input` has a `.expected` companion in the JSON shape
    the shared harness reads (the harness already lives in
    `internal/formats/testing.go` from M9.5).
  - The harness supports `go test -update ./...` to regenerate
    fixtures.
  - ARCHITECTURE.md format list updated to mention `jest` as
    "shipped" rather than "planned".
  - `formats.All()` (after the side-effect import) includes jest
    in alphabetical position — verified by extending
    `cmd/distill-ai/list_formats_test.go`.
  - README format list updated alongside ARCHITECTURE.
  - `.opencode/skills/distill-output/SKILL.md` gains a "Distil a
    jest run" recipe paralleling the M10/M11 recipes, per
    [CONTRIBUTING.md § Adding a format step 10](./CONTRIBUTING.md#adding-a-format).
  - **Integration suite** gains a `jest-fail.input` fixture (new
    — the integration testdata does not yet carry one) and a
    `TestBinary_JestEndToEndProducesOutput` test mirroring the
    M10 and M11 patterns. Closes
    [KNOWN_ISSUES.md § 6](./KNOWN_ISSUES.md) for jest.
- **Tests:**
  - `TestJest_Goldens`: harness walks `testdata/`, runs the
    parser on each `.input`, marshals Events to JSON, diffs
    against `.expected`. Run with `-update` to regenerate.
  - `TestJest_FixtureCount`: hard assertion that exactly the
    eight enumerated fixtures exist, so future drift is caught.
  - `TestBinary_JestEndToEndProducesOutput` (integration suite,
    per the DoD).
- **Docs:**
  - `docs/formats/jest.md` finalised: detection markers, every
    parsed event kind with an example, what's dropped, the
    `--json` out-of-scope note, the eight fixtures referenced by
    file name.
  - ARCHITECTURE.md updated per DoD.
  - README.md format list updated.
  - SKILL.md recipe added.

### M12 exit criteria

- All five sub-items ticked.
- `make check` clean; no race hits; no goroutine leaks.
- M12 milestone drift check: `formats.Get("jest")` returns the
  registered Format; `docs/formats/jest.md` exists and describes
  every Event kind the parser emits; SCHEMA.md's jest kind values
  list (`test_failure`, `snapshot_mismatch`, `suite_error`)
  matches the parser's emitted kinds exactly — no extras, no
  omissions; the eight fixtures live under
  `internal/formats/jest/testdata/` with `*.input` + `*.expected`
  pairs; the SKILL.md jest recipe is present; integration suite's
  jest end-to-end test passes.
- M12 completes the v1 specific-format set (gotest, pytest, jest).
  Combined with M9's generic fallback, every input shape the v1
  scope targets is now covered. M13 layers envelope handling on
  top so CI-wrapped versions of all three formats parse cleanly.

---

## M13 — Envelope stripper

A pre-processing decorator that strips wrapper-level metadata from
input bytes before format autodetection runs, and surfaces wrapper-
level signals as their own Events. The canonical use case is CI logs
— GitHub Actions, GitLab CI — where the real command output is
wrapped in per-line timestamps, group markers, and severity envelope
commands. The design is deliberately not CI-specific: anything that
decorates command output with per-line orchestrator metadata
(Docker buildkit prefixes, systemd `journal` envelope, `tee`-style
wrappers, future-unknown orchestrators) fits the same shape.

The envelope stripper sits **before** detection in the pipeline:
input → envelope detection → envelope stripping → format autodetect
→ Format.Parse. Inner-format detection runs against the cleaned
bytes, so a GitHub Actions log wrapping `go test` output detects as
gotest with `Confidence=1.0`, not as some new "github-actions"
format. Wrapper-level signals (a `##[error]` line outside any
group; a failed-step boundary) become Events with their own Kind so
downstream consumers can route on them.

Cross-references
[ARCHITECTURE.md § Autodetection](./ARCHITECTURE.md#autodetection)
(the stripper sits before the existing detector),
[ARCHITECTURE.md § Pipeline](./ARCHITECTURE.md#pipeline) (the
stripper is a new `Source` decorator concept, parallel to but
distinct from `Stage`),
[docs/formats/SCHEMA.md § Kind values](./docs/formats/SCHEMA.md#kind-values)
(M13 adds a new `envelope` kind family).

M13 builds on M1, M3, M5, M7, M9, M10, M11, M12. Each item below
lists Definition of Done, required tests, and required doc updates
per the [alignment rule](./.opencode/rules/alignment.md).

### M13.1 — `internal/envelope/envelope.go`: stripper interface and skeleton

Define the `Stripper` concept and land an empty default
implementation. Detection plumbing happens in M13.2; envelope-
specific implementations in M13.3 / M13.4.

- **DoD:**
  - New package `internal/envelope` exporting:
    - `Stripper` interface with three methods:
      `Name() string` (e.g., `"github-actions"`, `"gitlab-ci"`),
      `Detect(sample []byte) Confidence` (uses the same
      Confidence type from `internal/event`; ≥ 0.6 wins),
      `Strip(ctx, r io.Reader) (cleaned io.Reader, signals <-chan event.Event, err error)`.
      `Strip` returns a Reader yielding the input with envelope
      metadata removed, plus a channel of envelope-level Events
      the stripper synthesises (e.g., a `##[error]` line becomes
      one Event with `Kind="envelope_error"`).
    - `Register(s Stripper)` and `All() []Stripper` mirroring the
      `formats` registry shape. `Get(name)` for lookup. Same
      thread-safety (RWMutex) and alphabetical-sort guarantees.
    - A `Noop` Stripper that returns the input Reader unchanged
      and closes the signals channel immediately. Used as the
      explicit "no envelope" choice when `--strip-envelope=none`
      is passed.
  - **Decorator placement.** A new function
    `envelope.Wrap(ctx, r io.Reader, opts Options) (cleaned io.Reader, signals <-chan event.Event, chosen Stripper, err error)`
    runs envelope detection against the first 4 KiB of `r` (via
    the existing `TeeReader` sample pattern) and returns either
    the matching Stripper's `Strip` output or the `Noop` output.
    `Options` carries the user's `--strip-envelope` choice
    (`auto` | `none` | a specific name).
  - **Streaming.** `Strip` is required to be streaming: the
    returned Reader must produce cleaned output incrementally as
    input arrives. No full-input buffering. The signals channel
    is bounded (default capacity 16, matching the pipeline's
    `BufferSize`); a slow consumer applies backpressure to the
    stripper.
  - **Signal Events**. Envelope strippers emit Events with the
    new `Kind` values:
    - `envelope_error` — wrapper-level error signal (GitHub
      `##[error]`, GitLab `section_end` with non-zero exit).
    - `envelope_warning` — wrapper-level warning
      (`##[warning]`, similar GitLab patterns).
    - `envelope_step_failure` — a named job step / section
      ended with a non-zero exit code; Title carries the step
      name, Metadata["step"] and Metadata["exit_code"] are set.
    - All envelope signal Events have `Severity=SeverityError`
      for `envelope_error` and `envelope_step_failure`,
      `SeverityWarn` for `envelope_warning`.
  - **No format coupling.** The envelope package imports
    `internal/event` for the Event/Severity types only. It must
    not depend on `internal/formats` or `internal/detect`;
    wiring lives in `cmd/distill-ai/run.go` and
    `internal/pipeline/build.go` (M13.5).
- **Tests** (`internal/envelope/envelope_test.go`):
  - `TestEnvelope_RegisterAndGet`: stub Stripper registers and
    looks up.
  - `TestEnvelope_DuplicateRegisterPanics`: programmer-error
    guard parallel to `formats.Register`.
  - `TestEnvelope_AllIsSorted`: alphabetical determinism.
  - `TestEnvelope_NoopStripPassesThroughBytes`: `Noop.Strip`
    returns a Reader whose contents byte-equal the input.
  - `TestEnvelope_NoopSignalsChannelClosesImmediately`: the
    signals channel from `Noop.Strip` closes without sending
    any Events, and a downstream goroutine reading from it
    doesn't leak.
  - `TestEnvelope_WrapAutoChoosesHighestConfidence`: register
    two stub Strippers with different confidences against the
    same sample; assert the higher one is chosen.
  - `TestEnvelope_WrapNoneForcesNoop`: even with a Stripper
    that would claim the sample, `Options{Choice: "none"}`
    forces `Noop`.
- **Docs:**
  - Godoc on `Stripper`, `Wrap`, `Register`, `Noop`, `Options`,
    each envelope Event kind constant.
  - New `docs/envelope.md` with an overview: what envelopes are,
    why they're decorators not formats, the streaming contract,
    the signal-Event Kinds. M13.3 and M13.4 extend with
    per-implementation sections.
  - Update [ARCHITECTURE.md § Autodetection](./ARCHITECTURE.md#autodetection)
    to describe the new pre-detection step.
  - Update [docs/formats/SCHEMA.md § Kind values](./docs/formats/SCHEMA.md#kind-values):
    new section "Envelope kinds" listing `envelope_error`,
    `envelope_warning`, `envelope_step_failure` as additive
    kinds (no schema_version bump per the additive-change rule).

### M13.2 — `--strip-envelope` CLI flag and pipeline wiring

Expose envelope handling on the CLI and wire it into the pipeline
between input and detection. M13.2 lands the flag plus the no-op
behaviour change (no Strippers are registered yet, so the flag's
default is a no-op until M13.3).

- **DoD:**
  - New flag `--strip-envelope=auto|none|<name>` on the `run`
    and `explain` subcommands. Default `auto`.
  - Flag wiring in `cmd/distill-ai/run.go` constructs
    `envelope.Options{Choice: <flag value>}` and passes it to a
    new `envelope.Wrap` call **before** `detect.Detect`.
  - `pipeline.Build` (in `internal/pipeline/build.go`) gains a
    new optional `EnvelopeSignals <-chan event.Event` field on
    `Options`. When non-nil, `Build` constructs a fan-in stage
    that merges envelope signals with the format Parser's
    Events before either reaches the rest of the pipeline. The
    fan-in preserves arrival order across the two streams.
  - Envelope signals participate in the same downstream stages
    as parser Events (Collapse, Dedupe, Budget). Their Kinds
    (`envelope_error`, etc.) flow through to encoders without
    special-casing.
  - **`--from-ci` is NOT a flag** — the previous design draft
    used that name; the renamed `--strip-envelope=auto|...`
    flag is the only form. Documented in the flag help text and
    the CONTRIBUTING.md flag-policy reasoning.
  - The SKILL.md `cli-surface` manifest is updated in the same
    commit; the integration suite's
    `TestSkill_DocumentsCurrentCLISurface` test fails loudly
    otherwise.
- **Tests:**
  - `TestRun_StripEnvelopeAutoNoStrippers` (in
    `cmd/distill-ai/run_test.go`): with no registered Strippers,
    `--strip-envelope=auto` acts as `none`; the pipeline
    behaves identically to today.
  - `TestRun_StripEnvelopeNoneSkipsLookup`: explicit `none`
    short-circuits even if a future Stripper would have matched
    (uses a registered stub Stripper that would claim the
    sample).
  - `TestRun_StripEnvelopeNameSelectsExplicit`: explicit
    `<name>` bypasses detection and uses the named Stripper
    directly; unknown name returns exit 2.
  - `TestBuild_EnvelopeSignalsMergedIntoStream`: pipeline-level
    test that signals delivered on `Options.EnvelopeSignals`
    appear in the Sink's output stream alongside parser Events,
    in arrival order.
- **Docs:**
  - Update SKILL.md `cli-surface` manifest with
    `--strip-envelope`.
  - Update README.md flag list and the `run --help` text.
  - Update [ARCHITECTURE.md § Flags](./ARCHITECTURE.md#flags)
    with the new flag entry.
  - Extend `docs/envelope.md` with the CLI section.

### M13.3 — GitHub Actions stripper

Implement the first concrete `Stripper` for GitHub Actions logs.

- **DoD:**
  - New `internal/envelope/githubactions/` package with one file:
    `githubactions.go`. `func init() { envelope.Register(Stripper{}) }`.
  - `Name() string` returns `"github-actions"`.
  - `Detect(sample []byte) Confidence`:
    - `1.0` when the sample contains a line starting with
      `##[group]`, `##[error]`, `##[warning]`, `##[debug]`,
      `##[notice]`, or `::set-output ` (legacy).
    - `0.8` when the sample contains a per-line RFC3339-Z
      timestamp prefix matching `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z `
      on at least 3 of the first 10 non-blank lines.
    - `0.0` otherwise.
  - `Strip` performs three transformations:
    1. **Timestamp strip:** lines beginning with the
       RFC3339-Z prefix have the prefix removed.
    2. **Group folding:** `##[group]NAME` and `##[endgroup]`
       lines are removed from output. Group nesting is tracked
       in state (max depth 8, defensive cap).
    3. **Signal extraction:** `##[error]`, `##[warning]`,
       `##[notice]` lines produce envelope signal Events
       (not forwarded to the cleaned Reader) with the
       documented Kinds; the message after the marker becomes
       the Title.
  - **Step-failure detection.** A workflow step that fails
    emits `##[error]Process completed with exit code N.` as
    its terminal line. M13.3 maps this specific pattern to
    `envelope_step_failure` with
    `Metadata["exit_code"] = "N"`; the step name is recovered
    from the most recent `##[group]NAME` seen in scope.
  - ANSI escape sequences in input are left for the inner
    format to handle (every existing format already strips ANSI
    from Title where needed). The envelope stripper does not
    re-ANSI-strip.
- **Tests:**
  - `TestGHA_DetectGroupMarker`: clear group marker → 1.0.
  - `TestGHA_DetectTimestampHeuristic`: only timestamps, no
    workflow commands → 0.8.
  - `TestGHA_StripTimestamps`: input with timestamped lines →
    cleaned output has timestamps removed.
  - `TestGHA_StripGroupMarkers`: `##[group]/##[endgroup]`
    removed; group depth tracked.
  - `TestGHA_ErrorSignalEmitted`: `##[error]X` produces one
    envelope_error Event with Title `X`.
  - `TestGHA_StepFailureSignal`: a `Process completed with
    exit code 1.` line emits one `envelope_step_failure` Event
    with `exit_code="1"` and `step` set to the surrounding
    group name.
  - `TestGHA_StripPreservesInnerFormatBytes`: feed a real
    `gotest`-wrapped fixture; assert the cleaned output
    detects as gotest with `Confidence=1.0` via the same
    `detect.Detect` the production binary uses.
  - `TestGHA_StreamingStripsIncrementally`: use
    `testutil.SlowReader` to verify cleaned output appears
    before EOF.
- **Docs:**
  - Godoc on `Stripper`, `Detect`, `Strip`.
  - Extend `docs/envelope.md` with the GitHub Actions section:
    workflow command catalogue, what's stripped, what becomes
    a signal, example I/O.

### M13.4 — GitLab CI stripper

Parallel to M13.3 for GitLab CI logs.

- **DoD:**
  - New `internal/envelope/gitlabci/` package with one file:
    `gitlabci.go`. `func init() { envelope.Register(Stripper{}) }`.
  - `Name() string` returns `"gitlab-ci"`.
  - `Detect(sample []byte) Confidence`:
    - `1.0` when the sample contains a line matching
      `^section_start:\d+:[a-z0-9_]+\r?$` or
      `^section_end:\d+:[a-z0-9_]+\r?$` (GitLab's CR-terminated
      section envelope markers).
    - `0.8` when the sample contains ANSI colour escapes
      densely (≥ 5 distinct CSI sequences in the first 1 KiB)
      combined with a `Running with gitlab-runner ` line —
      the standard GitLab CI runner banner.
    - `0.0` otherwise.
  - `Strip` performs:
    1. **Section folding:** `section_start:NS:NAME` and
       `section_end:NS:NAME` lines removed; section names
       tracked to attribute step-failure signals.
    2. **CR strip:** lines ending in `\r\n` are normalised to
       `\n` (GitLab uses `\r` to overwrite progress indicators
       in interactive runners; the inner format usually doesn't
       care, but distill-ai's downstream encoders are happier
       with normalised line endings).
    3. **Signal extraction:** the line
       `ERROR: Job failed: exit code N` (the GitLab runner's
       canonical job-failure line) emits one
       `envelope_step_failure` Event with the current
       section's name and the exit code in metadata.
  - **No timestamp strip needed** — GitLab CI doesn't
    prefix per-line timestamps by default. If a runner config
    enables them, the inner format handles them (timestamps
    that happen to land in the input look like ordinary line
    prefixes to gotest / pytest / jest and don't anchor any
    Event).
- **Tests:**
  - `TestGitLab_DetectSectionMarker`: clear section marker → 1.0.
  - `TestGitLab_DetectRunnerBanner`: banner + dense ANSI → 0.8.
  - `TestGitLab_StripSectionMarkers`: section markers removed
    from cleaned output; section name tracked.
  - `TestGitLab_StripCRLF`: `\r\n` line endings normalised to
    `\n`.
  - `TestGitLab_JobFailureSignal`: `ERROR: Job failed: exit
    code 2` produces an `envelope_step_failure` Event with the
    current section name and `exit_code="2"`.
  - `TestGitLab_StripPreservesInnerFormatBytes`: feed a
    gotest-wrapped fixture; assert the cleaned output detects
    as gotest.
  - `TestGitLab_StreamingStripsIncrementally`: as M13.3's
    streaming test.
- **Docs:**
  - Godoc on `Stripper`, `Detect`, `Strip`.
  - Extend `docs/envelope.md` with the GitLab CI section.

### M13.5 — Fixtures, integration coverage, dogfood recipe

Tie M13 off with fixtures, the integration test, and the SKILL.md
recipe.

- **DoD:**
  - Six fixtures under `internal/envelope/testdata/`:
    `gha-gotest-fail.input` (GHA envelope wrapping a gotest
    failure),
    `gha-pytest-fail.input` (GHA + pytest),
    `gha-step-failure.input` (GHA + a clean inner stream that
    nevertheless ends with a step-failure marker),
    `gitlab-gotest-fail.input` (GitLab + gotest),
    `gitlab-pytest-fail.input` (GitLab + pytest),
    `gitlab-job-failure.input` (GitLab + clean inner + job
    failure marker).
  - Each `.input` has a `.expected` companion in the JSON shape
    the format-test harness reads. The harness is extended to
    accept an envelope-test mode that runs `envelope.Wrap`
    before parsing.
  - **Integration suite** gains
    `TestBinary_EnvelopeGHAGotestEndToEnd` and
    `TestBinary_EnvelopeGitLabGotestEndToEnd`: feed the
    wrapped gotest fixtures via stdin and assert the
    distilled output contains the expected inner Event Title
    plus the envelope_step_failure Title.
  - ARCHITECTURE.md updated: new section "Envelope handling"
    under or adjacent to "Autodetection".
  - README.md gains an "Envelopes" subsection in the format
    overview.
  - `.opencode/skills/distill-output/SKILL.md` gains a "Distil
    a GitHub Actions log" recipe and a "Distil a GitLab CI
    log" recipe, both showing `gh run view --log | ./bin/distill-ai`
    and `glab ci trace | ./bin/distill-ai` as the canonical
    forms.
- **Tests:**
  - `TestEnvelope_Goldens`: harness walks `testdata/`, runs
    `envelope.Wrap` + the relevant inner Format, marshals
    Events to JSON, diffs against `.expected`.
  - `TestEnvelope_FixtureCount`: exactly the six enumerated
    fixtures.
  - Integration tests per the DoD.
- **Docs:**
  - `docs/envelope.md` finalised: overview, the two shipped
    strippers, the signal Kinds, the six fixtures referenced
    by file name, examples of `gh run view --log` and `glab ci
    trace` invocations.
  - ARCHITECTURE.md updated per DoD.
  - README.md updated per DoD.
  - SKILL.md recipes added.

### M13 exit criteria

- All five sub-items ticked.
- `make check` clean; no race hits; no goroutine leaks.
- M13 milestone drift check: `envelope.Get("github-actions")` and
  `envelope.Get("gitlab-ci")` both return registered Strippers;
  `docs/envelope.md` exists and describes both; SCHEMA.md
  documents the three envelope kinds; the SKILL.md manifest
  includes `--strip-envelope`; the SKILL.md recipes cover both
  envelope sources; the integration suite's envelope tests pass.
- M13 makes `gh run view --log` and `glab ci trace` first-class
  distill-ai inputs without changing the format-author contract.
  Future envelope additions (CircleCI, Buildkite, Docker
  buildkit, systemd journal) follow the same pattern as M13.3 /
  M13.4: new package, register a `Stripper`, six fixtures, one
  integration test. No architectural change required.

---

## M14 — Config file support

- [ ] `internal/config/config.go`: load `.distill-ai.toml` from CWD upward, then `~/.config/distill-ai/config.toml`
- [ ] Precedence: CLI flag > project config > user config > default
- [ ] Per-format config sections override format defaults
- [ ] Custom regex-based format registration via `[[formats.custom.NAME]]`
- [ ] Config validation with clear errors
- [ ] Tests: precedence, override, malformed config

---

## M15 — Library API

- [ ] `pkg/distill/distill.go`: exported `Distill(ctx, r, opts) (<-chan Event, error)`
- [ ] Stable public API; document in package godoc
- [ ] Examples in `pkg/distill/example_test.go`
- [ ] Mark internal packages as such; nothing leaks except `pkg/distill`

---

## M16 — Documentation

- [ ] `man/distill-ai.1` man page generated from cobra
- [ ] README usage examples expanded with real fixtures
- [ ] `docs/formats/` per-format docs: what's detected, what's dropped, example I/O
- [ ] `docs/integration-claude-code.md`: how to wire into Claude Code
- [ ] `docs/integration-opencode.md`: how to wire into opencode AGENTS.md
- [ ] `docs/integration-ci.md`: piping CI output through distill-ai for failure summaries
- [ ] CHANGELOG.md with semantic versioning

---

## M17 — v1.0 release prep

- [ ] All M0–M16 complete or explicitly deferred
- [ ] `go test ./...` clean, `golangci-lint run` clean
- [ ] Cross-compile verified on linux/darwin/windows × amd64/arm64
- [ ] Binary size budget: ≤6 MB stripped (with tiktoken)
- [ ] Cold-start latency budget: ≤20 ms (heuristic), ≤120 ms (tiktoken)
- [ ] Throughput budget: ≥50 MB/sec single core
- [ ] Tag `v1.0.0`, run `goreleaser`, publish

---

## v1.1 — Static analysis & linting (post-launch)

The first post-v1.0 release. Theme:
[ADR-0002](./docs/decisions/0002-v1.0-scope-and-post-v1.0-roadmap.md)
— *structured findings about static artefacts*, distinct from the
runtime-failure theme of v1.0. Both Formats in this version consume
upstream-emitted JSON envelopes (`golangci-lint --out-format=json`,
`cargo ... --message-format=json`), so the marginal cost is the
lowest of any new-Format work the project has shipped.

The earlier "v1.1 grab-bag" (k8s logs, structured JSON logs,
npm/yarn/pnpm, rspec, mocha) moves to
[v1.5](#v15--more-log--test-formats-post-launch). The Rust pieces of
[M22](#m22--compiler--build-error-formats) move forward into M24
because they share clippy's JSON envelope and shipping them apart
makes no sense; M22 narrows to `tsc` / `gcc` / `clang`.

### M23 — `golangci-lint-json` + `go vet` format

A single Format that consumes both:

- `golangci-lint run --out-format=json` — multi-linter rollup, the
  canonical Go static-analysis pipeline.
- `go vet ./... 2>&1` — stdlib's built-in checks, line-oriented
  output, falls back to a regex parser within the same Format when
  the JSON envelope isn't present.

Detection prefers the JSON envelope (Confidence 1.0 on a clear
`{"Issues":[...` prefix or `{"Reports":[...`). Line-oriented `go vet`
output detects at 0.8 via `^# <pkg>` headers plus `<path>:<line>:<col>:`
diagnostic lines. The Format emits one `Event` per finding with
`Severity` derived from the linter's level, `Kind` set to the linter
name (`govet`, `staticcheck`, `revive`, `errcheck`, `ineffassign`,
etc.), `Location` populated from the JSON envelope, and `Body`
carrying the linter's message verbatim.

Cross-references
[ARCHITECTURE.md § Format plugin contract](./ARCHITECTURE.md#format-plugin-contract),
[docs/formats/SCHEMA.md § Kind values](./docs/formats/SCHEMA.md#kind-values)
(new kinds land here),
[ADR-0002](./docs/decisions/0002-v1.0-scope-and-post-v1.0-roadmap.md).

M23 builds on M1 (`Format`, `formats.Register`), M3 (autodetection),
M7 (encoders — all three render the new Events without modification),
M9 (generic remains the fallback if `golangci-lint`'s envelope changes
shape unexpectedly), and M10 (the format-test harness in
`internal/formats/testing.go` and the integration-suite end-to-end
pattern). Each item below lists Definition of Done, required tests,
and required doc updates per the
[alignment rule](./.opencode/rules/alignment.md).

#### M23.1 — Skeleton + Detect

- **DoD:**
  - New package `internal/formats/golintjson` exporting `Format`
    (value type, implements `formats.Format`).
  - `func init() { formats.Register(Format{}) }`.
  - `Name() string` returns `"golangci-lint"`.
  - `Detect(sample []byte) Confidence`:
    - `1.0` when the sample begins with `{"Issues":` or
      `{"Reports":` (the two stable top-level keys
      `golangci-lint --out-format=json` emits).
    - `0.8` when the sample contains `^# <importpath>$` lines
      followed by `^<path/with/slash>\.go:\d+:\d+: ` diagnostic
      lines — the `go vet ./...` shape.
    - `0.0` otherwise.
    - Threshold constants named `confidenceClearMarker = 1.0`,
      `confidenceFuzzy = 0.8` per the gotest/pytest precedent.
  - `Parse(ctx, r, opts)` returns an immediately-closed channel
    with `nil` error for M23.1; M23.2 and M23.3 fill it in.
- **Tests** (`internal/formats/golintjson/golintjson_test.go`):
  - `TestGolintJSON_DetectClearMarkerIssues`: feed `{"Issues":[]}`
    fragment; assert `1.0`.
  - `TestGolintJSON_DetectClearMarkerReports`: feed
    `{"Reports":[]}` fragment; assert `1.0`.
  - `TestGolintJSON_DetectGoVet`: feed two-line fragment
    `# example.com/m\n/path/to/file.go:12:5: assignment copies lock`;
    assert `0.8`.
  - `TestGolintJSON_DetectNegative`: feed gotest output, pytest
    output, plain prose; assert `0.0` for each.
  - `TestGolintJSON_RegisteredAtInit`: import for side effect,
    `formats.Get("golangci-lint")` returns `(format, true)`.
  - `TestGolintJSON_ParseEmptyStub`: `Parse` returns a closed
    channel without error so autodetection paths work end-to-end
    before the parser lands.
- **Docs:**
  - Godoc on `Format`, `Detect`, `Parse`, and the confidence
    constants.
  - New `docs/formats/golangci-lint.md` with section skeleton
    (intro, detection markers, what's extracted, what's dropped,
    example I/O). M23.1 fills intro + detection.
  - Update [README.md](./README.md) "Supported formats" with
    `golangci-lint` as "detect-only, parser lands in M23.2"
    when M23.1 lands.

#### M23.2 — Parse `golangci-lint` JSON envelope

- **DoD:**
  - Streaming JSON-array parser over `Issues[]`. Each issue
    becomes one `Event`:
    - `Severity = SeverityError` for `Severity:"error"`,
      `SeverityWarn` for `"warning"`, `SeverityInfo` for `"info"`
      and unknown.
    - `Kind = <linter_name>` (e.g., `"govet"`, `"staticcheck"`).
      The string lives verbatim in `Kind` and is added to
      [SCHEMA.md § Kind values](./docs/formats/SCHEMA.md#kind-values)
      under a `golangci-lint` open-set entry.
    - `Title = <Text>` from the JSON; if multi-line, the first
      line is `Title`, the rest joins `Body`.
    - `Location = {File: Pos.Filename, Line: Pos.Line, Column: Pos.Column}`.
    - `Body` carries the message plus the `SourceLines` array
      (the surrounding context `golangci-lint` already extracts).
    - `Metadata["linter"]` = linter name (duplicate of `Kind` for
      discoverability).
  - Streaming: the parser uses `json.Decoder` on `Issues[`, reads
    `[`, then decodes one object per call to `Decoder.Decode`,
    emits the `Event`, never buffers the whole array.
  - Schema-version drift guard: a `TestGolintJSON_SchemaShape` test
    fails the build if the upstream JSON shape (`Issues[].Pos.Filename`
    etc.) drifts. The fixture is the canonical schema reference.
- **Tests:**
  - `TestGolintJSON_ParseSingleIssue`: one-issue fixture from
    `testdata/case-01.input` → expected one `Event`, all fields
    populated.
  - `TestGolintJSON_ParseMultipleLinters`: fixture with
    govet, staticcheck, and revive findings interleaved; assert
    one Event per finding with the right `Kind`.
  - `TestGolintJSON_ParseSeverityMapping`: feed each of
    `"error"`, `"warning"`, `"info"`, and `""` (empty); assert
    the documented mapping.
  - `TestGolintJSON_ParseStreaming`: `testutil.SlowReader` drips
    the JSON array; assert the first Event emerges before EOF.
  - `TestGolintJSON_ParseEmptyIssues`: `{"Issues":[]}` → zero
    Events, channel closes cleanly.
  - `TestGolintJSON_ParseMalformedJSON`: truncated input →
    `Parse` closes the channel and returns the JSON error via
    a sentinel Event with `Severity=SeverityError`,
    `Kind="parse_error"`.
  - Golden tests under `internal/formats/golintjson/testdata/` per
    the M10 harness pattern. Minimum five cases: clean run
    (no issues), single govet finding, multi-linter, severity
    mix, malformed input.
- **Docs:**
  - Extend `docs/formats/golangci-lint.md` with the kind taxonomy,
    severity mapping, example input + output, the parse-error
    contract.
  - Add the open-set `Kind` entries to SCHEMA.md.

#### M23.3 — Parse line-oriented `go vet` output

- **DoD:**
  - Fallback line parser invoked when the JSON envelope wasn't
    detected (M23.1 returned `0.8` on the line-oriented shape).
  - State machine: `statePackageHeader` (matching `^# <path>$`,
    captures current package), `stateDiagnostic` (matching
    `^<path>:<line>:<col>: <message>$`).
  - One `Event` per diagnostic with
    `Severity = SeverityError`, `Kind = "govet"`,
    `Location = {File, Line, Column}`, `Body = [message]`,
    `Metadata["package"]` = the most recent package header.
  - Streaming: each Event forwards on the line after the
    diagnostic; no whole-input buffering.
- **Tests:**
  - `TestGoVet_ParseSingleDiagnostic`.
  - `TestGoVet_ParseMultiplePackages`: two `# pkg` headers, three
    diagnostics; assert `Metadata["package"]` is correct per
    Event.
  - `TestGoVet_ParseLocationParsing`: column field optional —
    handle both `file.go:12:5: msg` and `file.go:12: msg`.
  - `TestGoVet_ParseStreaming`.
  - Golden test cases under
    `internal/formats/golintjson/testdata/govet-*.input`.
- **Docs:**
  - Extend `docs/formats/golangci-lint.md` with the `go vet`
    section.
  - README "Supported formats" promotes the entry from
    "detect-only" to "shipped" once M23.3 lands.

### M23 exit criteria

- All three sub-items ticked.
- `make check` clean; no race hits.
- M23 milestone drift check: SCHEMA.md `golangci-lint` Kind
  section exists; README format list mentions the format as
  shipped; `docs/formats/golangci-lint.md` documents both the
  JSON and `go vet` paths; the integration test suite has at
  least one end-to-end `golangci-lint` fixture.

### M24 — `cargo-json` format (rustc / cargo build / cargo test / clippy)

Rust's `--message-format=json` envelope is the same shape across
`cargo build`, `cargo check`, `cargo test`, and `cargo clippy`. One
Format handles all four. The envelope is a stream of newline-
delimited JSON objects (`reason`-tagged), so the parser is a
streaming `json.Decoder` over `r`, one Event per
`compiler-message` / `compiler-artifact` / `test` reason as
relevant.

This milestone subsumes the Rust-related items previously listed
under [M22](#m22--compiler--build-error-formats) (rustc, cargo
output); M22 narrows to `tsc` / `gcc` / `clang`. See
[ADR-0002](./docs/decisions/0002-v1.0-scope-and-post-v1.0-roadmap.md)
for the rationale.

Cross-references
[ARCHITECTURE.md § Format plugin contract](./ARCHITECTURE.md#format-plugin-contract),
[docs/formats/SCHEMA.md § Kind values](./docs/formats/SCHEMA.md#kind-values).

M24 builds on M1, M3, M7, M9, M10, and on M23's JSON-envelope
parsing pattern. Each item below lists Definition of Done,
required tests, and required doc updates per the
[alignment rule](./.opencode/rules/alignment.md).

#### M24.1 — Skeleton + Detect

- **DoD:**
  - New package `internal/formats/cargojson` exporting `Format`.
  - `func init() { formats.Register(Format{}) }`.
  - `Name() string` returns `"cargo"`.
  - `Detect(sample []byte) Confidence`:
    - `1.0` when any line in the sample is a JSON object with a
      top-level `"reason"` field matching the cargo envelope
      vocabulary: `"compiler-message"`, `"compiler-artifact"`,
      `"build-script-executed"`, `"build-finished"`,
      `"test-started"`, `"test"`, `"suite"`. Detection scans
      up to the first newline boundary within the sample to
      avoid full-input parsing.
    - `0.8` when the sample is non-JSON but contains
      `^error\[E\d+\]: ` or `^warning: ` and at least one
      `--> <path>:<line>:<col>` indented reference (the
      line-oriented rustc default).
    - `0.0` otherwise.
    - Threshold constants `confidenceClearMarker = 1.0`,
      `confidenceFuzzy = 0.8`.
  - `Parse(ctx, r, opts)` returns an immediately-closed channel
    with `nil` error for M24.1.
- **Tests** (`internal/formats/cargojson/cargojson_test.go`):
  - `TestCargoJSON_DetectCompilerMessage`: feed one-line
    `{"reason":"compiler-message",...}`; assert `1.0`.
  - `TestCargoJSON_DetectTestSuite`: feed
    `{"reason":"suite","event":"started",...}`; assert `1.0`.
  - `TestCargoJSON_DetectLineOriented`: feed
    `error[E0308]: mismatched types\n  --> src/main.rs:3:5\n`;
    assert `0.8`.
  - `TestCargoJSON_DetectNegative`: gotest output, pytest output,
    plain prose — `0.0` each.
  - `TestCargoJSON_RegisteredAtInit`.
  - `TestCargoJSON_ParseEmptyStub`.
- **Docs:**
  - Godoc on `Format`, `Detect`, `Parse`, the confidence
    constants.
  - New `docs/formats/cargo.md` with the section skeleton.
  - README "Supported formats" gets a "detect-only, parser lands
    in M24.2" entry.

#### M24.2 — Parse `compiler-message` events (cargo build / cargo check / cargo clippy)

- **DoD:**
  - Streaming NDJSON parser. `json.Decoder` over `r`, one decode
    per line. Each decoded object is dispatched by `reason`:
    - `"compiler-message"`: emit one Event. `Severity` from
      `message.level` (`error` → SeverityError, `warning` →
      SeverityWarn, `note`/`help` → SeverityInfo).
      `Kind = "compiler_message"` for rustc messages,
      `Kind = "clippy_lint"` when `message.code.code` starts with
      `clippy::`. `Title = message.message`.
      `Location = {File, Line, Column}` from
      `message.spans[].file_name / line_start / column_start`,
      preferring the span with `is_primary = true`. `Body` joins
      `message.rendered` (rustc/clippy already produce a
      human-readable rendering — preserve it).
      `Metadata["code"]` = `message.code.code` if present
      (`E0308`, `clippy::needless_return`, etc.).
    - `"compiler-artifact"`, `"build-script-executed"`,
      `"build-finished"`: silently dropped (build chatter is the
      noise this Format exists to remove).
    - All other reasons: held over for M24.3 (test events).
  - Streaming: each Event forwards as its NDJSON line is consumed.
- **Tests:**
  - `TestCargoJSON_ParseRustcError`.
  - `TestCargoJSON_ParseClippyLint`: assert
    `Kind == "clippy_lint"` and `Metadata["code"]` carries the
    `clippy::*` code.
  - `TestCargoJSON_ParseSeverityMapping`: feed `error`,
    `warning`, `note`, `help`; assert the documented mapping.
  - `TestCargoJSON_ParseDropsArtifactNoise`: feed a fixture with
    20 `compiler-artifact` and 2 `compiler-message` events;
    assert exactly 2 Events emerge.
  - `TestCargoJSON_ParsePrimarySpan`: fixture has multiple
    spans; assert the primary one wins.
  - `TestCargoJSON_ParseStreaming`.
  - Golden tests under
    `internal/formats/cargojson/testdata/` per the M10 pattern.
    Minimum five cases: clean build, single error, multi-error,
    clippy mix, severity mix.
- **Docs:**
  - Extend `docs/formats/cargo.md` with the kind taxonomy,
    severity mapping, the artifact-drop policy, example I/O.
  - Add the new Kind values to SCHEMA.md under a `cargo` open-set
    section.

#### M24.3 — Parse `test` and `suite` events (cargo test)

- **DoD:**
  - The same parser handles `"reason":"test"` and
    `"reason":"suite"` lines:
    - `test` with `event:"started"`: silently dropped (start
      events are noise).
    - `test` with `event:"ok"`: silently dropped (passing tests
      are the noise we exist to remove).
    - `test` with `event:"failed"`: emit one Event.
      `Severity = SeverityError`, `Kind = "test_failure"`.
      `Title = "test <test_name> failed"`. `Body = stdout`
      (cargo test captures stdout per-test and includes it in
      the JSON). `Metadata["test_name"]` = the test's name.
    - `suite` with `event:"failed"`: emit a summary Event.
      `Severity = SeverityError`, `Kind = "test_suite_failure"`,
      `Title` reflecting failed/passed counts.
    - `suite` with `event:"ok"`: silently dropped.
  - When the input is a mix of `compiler-message` (M24.2) and
    `test` events (`cargo test` re-runs the build), both shapes
    coexist in one stream and both fire Events.
- **Tests:**
  - `TestCargoJSON_ParseTestFailed`: fixture with one failed
    test event; assert Event shape.
  - `TestCargoJSON_ParseTestPassing_NoEvent`: fixture with 100
    passing test events; assert zero Events.
  - `TestCargoJSON_ParseSuiteSummary`: fixture with a `suite`
    event reporting 5 passed / 2 failed; assert one
    `test_suite_failure` Event.
  - `TestCargoJSON_ParseBuildPlusTestStream`: realistic
    `cargo test` output with rebuild + test events; assert
    rebuild errors and test failures both emerge with the right
    `Kind`.
  - Golden test cases under
    `internal/formats/cargojson/testdata/test-*.input`.
- **Docs:**
  - Extend `docs/formats/cargo.md` with the test-event section.
  - README "Supported formats" promotes the entry from
    "detect-only" to "shipped" once M24.3 lands.

#### M24.4 — Parse line-oriented rustc output (fallback)

- **DoD:**
  - When detect returned `0.8` (no JSON envelope), parse the
    default rustc renderer output:
    - Multi-line block starting `^error\[E\d+\]: <msg>` or
      `^warning: <msg>` and containing one `--> <path>:<line>:<col>`
      reference.
    - One Event per block. `Severity` from the prefix
      (`error` / `warning`). `Kind = "compiler_message"`.
      `Title` = the first line. `Body` = the verbatim block.
      `Location` parsed from the first `--> path:line:col` line.
  - This path is a best-effort fallback for users who pipe
    `cargo build 2>&1` without `--message-format=json`. The JSON
    path is preferred; this path exists so the Format isn't a
    dead end when users forget the flag.
- **Tests:**
  - `TestCargoJSON_ParseLineOrientedError`.
  - `TestCargoJSON_ParseLineOrientedWarning`.
  - `TestCargoJSON_ParseLineOrientedMultipleBlocks`.
  - Golden cases under
    `internal/formats/cargojson/testdata/lineoriented-*.input`.
- **Docs:**
  - Extend `docs/formats/cargo.md` with a section on the
    line-oriented fallback and a recommendation to prefer
    `--message-format=json` for fidelity.

### M24 exit criteria

- All four sub-items ticked.
- `make check` clean; no race hits.
- M24 milestone drift check: SCHEMA.md `cargo` Kind section
  exists; README format list mentions the format as shipped;
  `docs/formats/cargo.md` documents JSON, line-oriented, and test
  paths; the integration test suite has at least one end-to-end
  `cargo` fixture each for build and test.

### v1.1 exit criteria

- M23 and M24 both shipped, tagged, and released.
- The static-analysis theme is documented in the v1.1 release
  notes.
- Before the v1.2 branch opens, re-check the
  [ADR-0002 re-evaluation criteria](./docs/decisions/0002-v1.0-scope-and-post-v1.0-roadmap.md#re-evaluation-criteria) —
  user signal since v1.0 may justify reordering v1.2 / v1.3.

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
as M1–M17; each language becomes a Format whose `Detect` matches
files by extension or shebang and whose `Parse` walks an AST instead
of scanning lines. New `Kind` values land in
[`docs/formats/SCHEMA.md`](./docs/formats/SCHEMA.md): `package`,
`import`, `type_def`, `func_sig`, `method_sig`, `field`, `const`.

Architectural decision recorded in
[ADR-0001](./docs/decisions/0001-reject-cgo-tree-sitter-prefer-wasm.md):
CGo tree-sitter is rejected; WASM tree-sitter via wazero is the
multi-language path. Go-only (M18) uses the stdlib first to avoid any
dependency until the design proves itself.

Each milestone below ships scoped (DoD, tests, docs) before its
branch opens, per the
[scoping convention](#scoping-format).

### M18 — Source-code distillation (Go-only)

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

### M19 — Multi-language code distillation (WASM tree-sitter)

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

### M20 — Agent-read wrapper

- [ ] CLI mode that takes a file/dir and emits the distilled view
      first, full content on demand
- [ ] Integrate as an MCP tool exposed via `distill-ai mcp` (M15 /
      v1.2): `read_distilled(path)` returns symbol summary;
      `read_full(path, ranges?)` returns verbatim bytes
- [ ] Document the agent-side workflow in
      `docs/integration-agent-reads.md` (how Claude Code / opencode
      can be configured to prefer the distilled read)
- [ ] Depends on M18 (Go), ideally M19 (other languages)

### M21 — AST-aware diff distillation

- [ ] Take a unified diff (or `git diff` output) and parse the
      before/after of each hunk through the relevant language Format
- [ ] Emit symbol-level `Event`s: `function Foo signature changed`,
      `import added`, `type X moved`, `method Y deleted`
- [ ] Non-code text diffs fall back to line-level distillation
- [ ] Subsumes the backlog `--diff` idea for source files; log diffs
      still use the original line-level approach
- [ ] Depends on M18/M19

### M22 — Compiler / build-error formats

Narrowed by
[ADR-0002](./docs/decisions/0002-v1.0-scope-and-post-v1.0-roadmap.md):
Rust (rustc / cargo / clippy) moved forward into
[M24](#m24--cargo-json-format-rustc--cargo-build--cargo-test--clippy)
under v1.1. `go build` errors are already handled by `gotest` (M10
emits `build_failure` Events). What remains here:

- [ ] `tsc` output as a Format
- [ ] `gcc` / `clang` output as a Format
- [ ] Independent of M18–M21 architecturally; listed here because
      compiler errors reference source positions and benefit from
      the same per-event structure code distillation defines

---

## v1.4 — Documentation formats

Per [ADR-0002](./docs/decisions/0002-v1.0-scope-and-post-v1.0-roadmap.md).
Markdown outline is the cheapest doc-format opportunity and pairs
naturally with the M20 agent-read wrapper (v1.3) that consumes it.
HTML readable-content extraction is a candidate but deferred to its
own design pass; not scoped here.

### M25 — Markdown outline format

A `markdown` Format that emits one Event per heading, capturing the
heading hierarchy of a Markdown document. Useful as a "what's in
this file" probe — particularly for the M20 agent-read wrapper,
which can serve the outline to an agent before any prose content.

Detection is the file's content shape rather than its filename:
`Format.Detect` returns `0.7` when a sample contains at least three
ATX headings (`^#{1,6} `) **and** has Markdown-typical formatting
(bullet lists, fenced code blocks, link syntax). It deliberately
loses to any specific format on ties because Markdown is a
container — a Markdown file containing a pytest log should detect
as pytest, not Markdown.

Cross-references
[ARCHITECTURE.md § Format plugin contract](./ARCHITECTURE.md#format-plugin-contract),
[docs/formats/SCHEMA.md § Kind values](./docs/formats/SCHEMA.md#kind-values).

M25 builds on M1, M3, M7, M9, and M10's testing patterns. Each
item lists Definition of Done, required tests, and required doc
updates per the
[alignment rule](./.opencode/rules/alignment.md).

#### M25.1 — Skeleton + Detect

- **DoD:**
  - New package `internal/formats/markdown` exporting `Format`.
  - `func init() { formats.Register(Format{}) }`.
  - `Name() string` returns `"markdown"`.
  - `Detect(sample []byte) Confidence`:
    - `0.7` when the sample contains ≥ 3 ATX-heading lines
      (`^#{1,6} ` with the trailing space) **and** at least one
      of: a fenced code block (`^```\w*`), a bullet-list line
      (`^[-*+] `), or a link `[text](url)` sequence. The
      composite check rejects log files that happen to contain
      lines starting with `#` (comments, shell history).
    - `0.0` otherwise.
    - Threshold constant `confidenceMarkdown = 0.7`.
    - **Deliberately below 1.0.** Markdown is a container format;
      any specific format (gotest, pytest, jest, cargo, etc.)
      contained inside a Markdown file should win the tie. The
      0.7 floor sits below their `confidenceClearMarker = 1.0`
      and above the `generic` floor (0.1).
  - `Parse(ctx, r, opts)` returns an immediately-closed channel
    with `nil` error for M25.1.
- **Tests** (`internal/formats/markdown/markdown_test.go`):
  - `TestMarkdown_DetectHeadingsPlusCodeFence`.
  - `TestMarkdown_DetectHeadingsPlusBullets`.
  - `TestMarkdown_DetectHeadingsPlusLinks`.
  - `TestMarkdown_DetectInsufficientHeadings`: two headings only
    → `0.0`.
  - `TestMarkdown_DetectNotShellHistory`: a file of `# command`
    lines with no formatting → `0.0`.
  - `TestMarkdown_DetectLosesToSpecific`: a pytest log embedded
    in a Markdown fixture detects as pytest, not markdown
    (depends on M11 being shipped; if M11 is still pending when
    M25.1 lands, use gotest as the embedded format).
  - `TestMarkdown_RegisteredAtInit`.
  - `TestMarkdown_ParseEmptyStub`.
- **Docs:**
  - Godoc on `Format`, `Detect`, `Parse`, the confidence
    constant.
  - New `docs/formats/markdown.md` with the section skeleton
    (intro, detection model, what's extracted — `heading` Event
    per heading — what's dropped, example I/O).
  - README "Supported formats" gets a "detect-only, parser lands
    in M25.2" entry when M25.1 lands.

#### M25.2 — Parse ATX and setext headings

- **DoD:**
  - Streaming scanner over `r`. Each ATX heading
    (`^(#{1,6}) (.+)$`) emits one Event:
    - `Severity = SeverityInfo`.
    - `Kind = "heading"`.
    - `Title` = the heading text (after the `#` prefix and the
      one mandatory space, trailing whitespace trimmed).
    - `Location = {File: <filename if known>, Line: <line>}` —
      the source filename comes from `opts.Filename` (a new
      `ParseOpts` field added at this milestone; the line comes
      from the scanner's running line counter.
    - `Body` = empty (the heading text is already in `Title`).
    - `Metadata["level"]` = the heading level as a decimal
      string ("1" through "6").
    - `Metadata["anchor"]` = the GitHub-flavoured anchor slug
      (lowercase, spaces → hyphens, strip non-alphanumeric
      except `-`).
  - Setext headings (`====` / `----` underlines on the line
    following the heading text) emit Events with `level=1` and
    `level=2` respectively. The scanner uses a one-line
    lookahead.
  - **Inside fenced code blocks**, lines starting with `#` are
    not headings. The scanner tracks the fence state with a
    boolean flag.
  - Streaming: each Event forwards as the heading line (or, for
    setext, the underline line) is consumed.
- **Tests:**
  - `TestMarkdown_ParseAtxHeadings`: fixture with H1–H6;
    assert six Events with the documented `Metadata["level"]`.
  - `TestMarkdown_ParseSetextHeadings`: fixture with `===` and
    `---` underlines; assert two Events with levels 1 and 2.
  - `TestMarkdown_ParseAnchor`: feed `## Hello, World!`; assert
    `Metadata["anchor"] == "hello-world"`.
  - `TestMarkdown_ParseFencedCodeBlock_NotHeading`: fixture with
    a heading-like line inside a `````` fence; assert zero
    Events.
  - `TestMarkdown_ParseStreaming`: `testutil.SlowReader` drips
    a fixture; first Event emerges before EOF.
  - `TestMarkdown_ParseLineNumberAccuracy`: feed a fixture with
    a heading on line 47; assert `Location.Line == 47`.
  - Golden tests under `internal/formats/markdown/testdata/`
    per the M10 harness pattern. Minimum five cases: ATX-only,
    setext-only, mixed ATX and setext, fenced-block guard,
    deeply nested (H5/H6).
- **Docs:**
  - Extend `docs/formats/markdown.md` with the heading taxonomy,
    fenced-block rule, anchor algorithm, example I/O.
  - Add the `heading` Kind value to SCHEMA.md.
  - README "Supported formats" promotes the entry to "shipped"
    once M25.2 lands.

### M25 exit criteria

- Both sub-items ticked.
- `make check` clean.
- M25 milestone drift check: SCHEMA.md `heading` Kind documented;
  README format list mentions Markdown as shipped; the
  integration test suite has at least one end-to-end Markdown
  fixture.
- Note: M25 ships outline-only. Other Markdown features (link
  inventory, code-block extraction, front-matter) are explicitly
  deferred — they need their own design and value-vs-cost case.

---

## v1.5 — More log / test formats (post-launch)

The displaced original v1.1 list, per
[ADR-0002](./docs/decisions/0002-v1.0-scope-and-post-v1.0-roadmap.md).
These items are not yet scoped; they will be scoped one-by-one
after v1.0 ships, in priority order determined by real usage
signal.

- [ ] `k8s` format: kubectl logs, structured + unstructured
- [ ] `json` format: generic JSON-per-line logs (Zap, slog, Bunyan, Pino)
- [ ] `npm`/`yarn`/`pnpm` install/build output
- [ ] `rspec` format
- [ ] `mocha` format

> The previous note about cargo / rustc / tsc / gcc has been
> resolved: cargo and rustc ship in
> [M24](#m24--cargo-json-format-rustc--cargo-build--cargo-test--clippy)
> under v1.1; tsc / gcc / clang remain in
> [M22](#m22--compiler--build-error-formats) under v1.3; `go build`
> errors are already handled by `gotest` (M10).

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
