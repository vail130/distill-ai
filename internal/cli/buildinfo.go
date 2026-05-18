package cli

// Build-time variables. Populated via SetBuildInfo from
// cmd/distill-ai/main.go (which receives them via -ldflags from the
// Makefile and goreleaser). Default values are the placeholders the
// pre-M16.1 binary used when run via `go run` or an unstripped
// `go build` without ldflag overrides.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)
