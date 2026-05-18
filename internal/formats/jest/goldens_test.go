package jest_test

import (
	"testing"

	"github.com/vail130/distill-ai/internal/formats"
	"github.com/vail130/distill-ai/internal/formats/jest"
)

// TestJest_Goldens runs every *.input under testdata/ through
// jest.Format{}.Parse and diffs the emitted Event stream
// (marshalled as JSON) against the matching *.expected file.
// Run with
// `DISTILL_AI_UPDATE_GOLDENS=1 go test ./internal/formats/jest`
// to regenerate goldens after a deliberate parser change.
func TestJest_Goldens(t *testing.T) {
	formats.RunGoldens(t, jest.Format{}, "testdata")
}

// TestJest_FixtureCount pins the v1 fixture set. M12.5 ships
// exactly eight fixtures; future drift (a deleted fixture, or one
// added without updating SCHEMA.md / docs/formats/jest.md) fails
// loudly.
func TestJest_FixtureCount(t *testing.T) {
	formats.FixtureCount(t, "testdata", 8)
}
