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
  in `internal/pipeline/property_test.go`.
- **Streaming:** events emit before EOF, not buffered until EOF.
  Enforced by `TestPipeline_StreamingEmitsBeforeEOF`, which uses
  `testutil.SlowReader` to feed bytes at a measurable interval and
  asserts that the first Event reaches the Sink before the entire
  input could possibly have arrived.
- **Schema-version stability:** JSON output round-trips through the
  documented schema (enforced today by
  `TestEvent_JSONSchemaMatchesDoc`).

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
