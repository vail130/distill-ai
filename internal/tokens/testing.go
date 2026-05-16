package tokens

// TestLoaderConfigured exposes the unexported loaderConfigured probe
// to external test packages so TestTiktoken_NoNetwork can run from
// internal/tokens_test. Production code must not call this — there
// is no use for it outside the build-time no-network guarantee
// check.
//
// Named TestLoaderConfigured (not LoaderConfigured) so the package's
// public API at godoc.org doesn't appear to include a "configured
// loader?" check; the name advertises the test-only purpose.
func TestLoaderConfigured() bool { return loaderConfigured() }
