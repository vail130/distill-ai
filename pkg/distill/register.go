package distill

// Format and envelope-stripper registration via side-effect imports.
// Importing pkg/distill brings the full v1 format set into the
// global registry: generic, gotest, jest, pytest, plus the
// github-actions and gitlab-ci envelope strippers. A library
// caller therefore gets the same default behaviour as the CLI
// without having to enumerate every internal/formats/* package.
//
// Side-effect imports are by design: the formats and envelope
// strippers register themselves via init() so the registry is
// populated before any code reaches Distill. Removing one of
// these imports drops the corresponding format from autodetect
// and from the `Get` lookup path.
//
// Library callers who want a stripped-down format set should build
// their own binary that imports the formats they want directly,
// bypassing this convenience package. The CLI does the same: it
// imports an equivalent set from cmd/distill-ai/register.go.

import (
	// generic is the regex-driven fallback Format. The detector
	// uses it whenever no specific format scores above
	// event.ConfidenceMinDetect.
	_ "github.com/vail130/distill-ai/internal/formats/generic"

	// gotest parses `go test` output.
	_ "github.com/vail130/distill-ai/internal/formats/gotest"

	// jest parses jest output.
	_ "github.com/vail130/distill-ai/internal/formats/jest"

	// pytest parses pytest output.
	_ "github.com/vail130/distill-ai/internal/formats/pytest"

	// github-actions strips the GitHub Actions workflow envelope.
	_ "github.com/vail130/distill-ai/internal/envelope/githubactions"

	// gitlab-ci strips the GitLab CI job envelope.
	_ "github.com/vail130/distill-ai/internal/envelope/gitlabci"
)
