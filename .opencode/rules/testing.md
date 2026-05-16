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

- **Determinism:** same input twice → byte-identical output.
- **Streaming:** events emit before EOF, not buffered until EOF.
- **Schema-version stability:** JSON output round-trips through the
  documented schema (enforced today by
  `TestEvent_JSONSchemaMatchesDoc`).

These are not optional. Property tests are part of the contract.

## Concurrency

- Run with `-race` (the default in `make test`).
- Tests that touch package-level state (e.g., the format registry)
  must `t.Cleanup(formats.ResetForTest)` to keep cases isolated.
