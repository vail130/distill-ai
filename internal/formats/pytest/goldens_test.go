package pytest_test

import (
	"testing"

	"github.com/vail130/distill-ai/internal/formats"
	"github.com/vail130/distill-ai/internal/formats/pytest"
)

// TestPytest_Goldens runs every *.input under testdata/ through
// pytest.Format{}.Parse and diffs the emitted Event stream
// (marshalled as JSON) against the matching *.expected file. Run
// with `DISTILL_AI_UPDATE_GOLDENS=1 go test ./internal/formats/pytest`
// to regenerate goldens after a deliberate parser change.
func TestPytest_Goldens(t *testing.T) {
	formats.RunGoldens(t, pytest.Format{}, "testdata")
}

// TestPytest_FixtureCount pins the v1 fixture set. M11.5 ships
// exactly eight fixtures; future drift (a deleted fixture, or one
// added without updating SCHEMA.md / docs/formats/pytest.md) fails
// loudly.
func TestPytest_FixtureCount(t *testing.T) {
	formats.FixtureCount(t, "testdata", 8)
}
