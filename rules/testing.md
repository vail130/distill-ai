# Testing conventions

See [alignment.md](./alignment.md) for the rules on *when* tests are
required. The points below are about *how* we write them.

## Format tests

- Every format has golden-file tests under
  `internal/formats/<name>/testdata/`.
- Pattern: `case-NN.input` + `case-NN.expected`. Test runner walks the
  directory, runs the parser on each input, diffs against expected.
- Update goldens with `go test -update ./...`. The `-update` flag must
  be implemented in the test harness; follow the pattern in existing
  format tests.

## Streaming tests

- Streaming tests use a `slowReader` that emits bytes with controlled
  delay, asserting events appear before EOF.
- The `slowReader` helper lives in `internal/testutil/` (added in M2)
  so format tests can reuse it.

## Mocks

- **No mocks.** Use real fixture data.
- Stub formats (`stubFormat`, `fakeFormat`) for plumbing tests are
  acceptable when they implement the real interface and emit real
  Event values.

## Property tests

Invariants that must hold across every format and every output encoder:

- **Determinism:** same input twice → byte-identical output. Enforced
  by `TestPipeline_Determinism` and `TestPipeline_DeterminismFromBytes`
  in `internal/pipeline/property_test.go`, and by
  `TestSinks_DeterministicForFixedInput` in
  `internal/output/property_test.go` for each output encoder.
- **Streaming:** events emit before EOF, not buffered until EOF.
  Enforced by `TestPipeline_StreamingEmitsBeforeEOF`, which uses
  `testutil.SlowReader` to feed bytes at a measurable interval and
  asserts that the first Event reaches the Sink before the entire
  input could possibly have arrived. The encoders are themselves
  exercised by `TestSinks_StreamingEmitsBeforeEOF` (note: `JSONSink`
  with `Streaming=false` is buffered by design — the schema
  commits to a single top-level object — and is excluded).
- **Footer toggle:** `--no-footer` (a.k.a. `NoFooter` on each Sink) is
  uniformly honoured by text/markdown encoders and is a documented
  no-op on the JSON encoder, where the summary is part of the schema.
  Enforced by `TestSinks_NoFooterFlagHonoured` and
  `TestSinks_FooterReflectsCounters`.
- **Schema-version stability:** JSON output round-trips through the
  documented schema (enforced today by
  `TestEvent_JSONSchemaMatchesDoc`, by
  `TestJSONSink_SchemaVersionMatchesDoc` (which cross-checks
  `output.SchemaVersion` against the SCHEMA.md "Current schema version"
  line), and by `TestJSONSink_SummarySchemaMatchesDoc` (which pins
  every JSON tag on the summary struct to a documented row in
  SCHEMA.md so additive counter fields can't ship undocumented).
- **No goroutine leak on cancellation:** every Sink must exit cleanly
  when its context is cancelled mid-stream. Enforced across the Sink
  set by `TestSinks_NoGoroutineLeakOnCancellation` in
  `internal/output/property_test.go`, which drives each Sink through
  20 cancellation iterations and asserts NumGoroutine returns to
  baseline. Catches the class of bug where a future Sink spawns
  internal goroutines (a buffered batcher, a parallel encoder) and
  forgets to wire ctx through.

These are not optional. Property tests are part of the contract.

When adding a new Format or Stage, write its determinism and streaming
property tests using the same shape: a `Pipeline` with the new
component wired in, run twice for determinism, fed through `SlowReader`
for streaming. Both should be a few lines each because the helpers
(`collectSink`, `timingSink`, `SlowReader`) handle the plumbing.

## Concurrency

- Run with `-race` (the default in `make test`).
- Tests that touch package-level state (e.g., the format registry)
  must `t.Cleanup(formats.ResetForTest)` to keep cases isolated.
