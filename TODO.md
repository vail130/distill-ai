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

Milestones M1–M8 are scoped this way today. Per the working agreement,
the next three open milestones are kept fully scoped at all times — so
as M6 lands, M9 gets scoped; as M7 lands, M10 gets scoped; and so on.
M9–M16 are sketched but not yet scoped.

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
(M8) and library callers (M14) already use.

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
    after `Run` returns. M8 will wire the CLI; M14 library callers
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

## M7 — Output encoders

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

### M7.1 — `internal/output/text.go`: default compact encoder

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

### M7.2 — `internal/output/json.go`: schema-versioned JSON / ndjson

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

### M7.3 — `internal/output/markdown.go`: markdown encoder

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

### M7.4 — Property tests across all three Sinks

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

## M8 — CLI surface

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

### M8.1 — Adopt `cobra` for flag and subcommand handling

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

### M8.2 — `distill-ai run` (the default pipeline command)

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

### M8.3 — Exit-code mapping

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

### M8.4 — `list-formats` subcommand

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

### M8.5 — `detect FILE` subcommand (cobra adaptation)

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

### M8.6 — `explain FILE` subcommand

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

### M8.7 — `completions [bash|zsh|fish]` subcommand

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

### M8.8 — `version` subcommand

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
  by a human or an agent. M9–M12 add formats; M13 adds config;
  M14 promotes the library API; M15 polishes docs.

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
