package main

// Format and envelope-stripper registration via side-effect imports.
// Each imported package's init() function calls formats.Register or
// envelope.Register, populating the relevant global registry before
// main() runs.
//
// To add a new format to the binary:
//   1. Implement it under internal/formats/<name>/.
//   2. Add a blank import line below.
//
// To add a new envelope stripper:
//   1. Implement it under internal/envelope/<name>/.
//   2. Add a blank import line below.
//
// Keep entries sorted alphabetically within each group so future
// drift is obvious in code review.

import (
	// generic is the regex-driven fallback Format. The detector
	// uses it whenever no specific format scores above
	// event.ConfidenceMinDetect.
	_ "github.com/vail130/distill-ai/internal/formats/generic"

	// gotest parses `go test` output. Detect anchors on
	// `--- FAIL:`, `FAIL\t<pkg>`, and `=== RUN`.
	_ "github.com/vail130/distill-ai/internal/formats/gotest"

	// jest parses jest output. Detect anchors on the `●` failure
	// bullet, `FAIL`/`PASS` per-file headers with a test-file path
	// token, and the `Tests:` summary line.
	_ "github.com/vail130/distill-ai/internal/formats/jest"

	// pytest parses pytest output. Detect anchors on the
	// `=== test session starts ===` and `=== FAILURES ===` banners.
	_ "github.com/vail130/distill-ai/internal/formats/pytest"

	// github-actions strips the GitHub Actions workflow envelope
	// (timestamps, group markers, error/warning directives) before
	// format detection runs.
	_ "github.com/vail130/distill-ai/internal/envelope/githubactions"

	// gitlab-ci strips the GitLab CI job envelope (section markers,
	// CRLF line endings) and surfaces the job-failure marker as a
	// signal Event before format detection runs.
	_ "github.com/vail130/distill-ai/internal/envelope/gitlabci"
)
