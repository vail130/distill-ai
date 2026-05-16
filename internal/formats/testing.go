package formats

// ResetForTest clears the global registry. It is intended only for use
// from tests that need a clean registry — usually in combination with
// t.Cleanup(formats.ResetForTest) — and lives in a regular .go file
// rather than a _test.go file so test packages outside internal/formats
// can call it.
//
// Production code must not call this. The registry is populated at
// init() time by format implementations registering themselves; reset
// at runtime would silently disable every format in the binary.
func ResetForTest() { reset() }
