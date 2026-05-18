package main

// Format registration via side-effect imports. Each registered
// format's init() function calls formats.Register, populating the
// global registry before main() runs.
//
// To add a new format to the binary:
//   1. Implement it under internal/formats/<name>/.
//   2. Add a blank import line below.
//
// Keep entries sorted alphabetically by format name so future drift
// is obvious in code review.

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
)
