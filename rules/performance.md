# Performance budgets

distill-ai is a Unix filter that sits in the hot path of every command
its consumer runs. It must be fast and bounded.

## Hard targets

- **Streaming throughput:** ≥ 50 MB/sec on a single core, measured with
  the heuristic estimator and a typical format parser.
- **Heuristic-estimator throughput:** ≥ 100 MB/sec single-core,
  zero allocations per call. Verified by `BenchmarkHeuristic_Estimate`
  in `internal/tokens/`. The budget enforcer (M6) calls Estimate once
  per Event so per-call latency matters more than throughput in
  practice, but the throughput number is the easier comparison point
  across machines. Run with `make bench`.
- **Cold-start latency:** ≤ 20 ms with the heuristic estimator,
  ≤ 120 ms with `--tokenizer=tiktoken`. Measured from process start to
  first byte of output for a no-op input.
- **Binary size:** ≤ 6 MB stripped (the v1.0 target). Tiktoken vocab
  is the dominant contributor; see ARCHITECTURE.md § Token estimation.
- **Memory:** bounded. Dedupe LRU has a configurable cap. No unbounded
  buffers anywhere — neither full-input buffering nor unbounded
  channel queues. Verified by `TestPipeline_BoundedMemory_PeakSampling`
  (M2.3): pipes 8 MB of synthetic input through a discarding sink
  while a goroutine samples `runtime.MemStats.HeapAlloc` at 1 ms
  intervals; peak live heap must stay under a 16 MB ceiling. The
  theoretical bound is `BufferSize × (len(Stages) + 1) × sizeof(Event)`
  plus the parser's scratch buffer (≤ 64 KB for a `bufio.Scanner`),
  well under the ceiling for any realistic pipeline shape.

## Soft expectations

- **No goroutine leaks.** Every test runs under `-race`; M2 adds an
  explicit goroutine-count check.
- **No allocations in hot loops** unless justified. `pprof` is the
  arbiter, not intuition.

## When you regress these

If a change regresses any of the hard targets, the commit message must
justify it: what was traded for what, and whether the trade is
permanent or part of a planned sequence of changes.

Casual regressions ("this is 10% slower but the code is cleaner") are
not acceptable without that justification.
