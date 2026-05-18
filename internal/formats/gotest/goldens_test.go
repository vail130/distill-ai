package gotest_test

import (
	"testing"

	"github.com/vail130/distill-ai/internal/formats"
	"github.com/vail130/distill-ai/internal/formats/gotest"
)

// TestGotest_Goldens runs every *.input under testdata/ through
// gotest.Format{}.Parse and diffs the emitted Event stream
// (marshalled as JSON) against the matching *.expected file. Run
// with `DISTILL_AI_UPDATE_GOLDENS=1 go test ./internal/formats/gotest`
// to regenerate goldens after a deliberate parser change.
func TestGotest_Goldens(t *testing.T) {
	formats.RunGoldens(t, gotest.Format{}, "testdata")
}

// TestGotest_FixtureCount pins the v1 fixture set. M10.5 ships
// exactly eight fixtures; future drift (a deleted fixture, or one
// added without updating SCHEMA.md / docs/formats/gotest.md) fails
// loudly.
func TestGotest_FixtureCount(t *testing.T) {
	formats.FixtureCount(t, "testdata", 8)
}
