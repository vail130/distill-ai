package formats

// ResetForTest exposes the unexported registry-clearing helper to
// external test packages. Only available when the test binary is
// built; never compiled into the production binary.
func ResetForTest() { reset() }
