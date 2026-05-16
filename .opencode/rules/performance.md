# Performance budgets

distill-ai is a Unix filter that sits in the hot path of every command
its consumer runs. It must be fast and bounded.

## Hard targets

- **Streaming throughput:** ≥ 50 MB/sec on a single core, measured with
  the heuristic estimator and a typical format parser.
- **Cold-start latency:** ≤ 20 ms with the heuristic estimator,
  ≤ 120 ms with `--tokenizer=tiktoken`. Measured from process start to
  first byte of output for a no-op input.
- **Binary size:** ≤ 6 MB stripped (the v1.0 target). Tiktoken vocab
  is the dominant contributor; see ARCHITECTURE.md § Token estimation.
- **Memory:** bounded. Dedupe LRU has a configurable cap. No unbounded
  buffers anywhere — neither full-input buffering nor unbounded
  channel queues. Verified by `TestPipeline_BoundedMemory` (M2.3) on
  synthetic 10GB input.

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
