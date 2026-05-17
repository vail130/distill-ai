package generic_test

import (
	"testing"

	"github.com/vail130/distill-ai/internal/formats"
	"github.com/vail130/distill-ai/internal/formats/generic"
)

// TestGeneric_Goldens runs every *.input under testdata/ through
// generic.Format{}.Parse and diffs the emitted Event stream
// (marshalled as JSON) against the matching *.expected file. Run
// with `go test -update ./internal/formats/generic` to regenerate
// goldens after a deliberate parser change.
func TestGeneric_Goldens(t *testing.T) {
	formats.RunGoldens(t, generic.Format{}, "testdata")
}

// TestGeneric_FixtureCount pins the v1 fixture set. M9.5 ships
// exactly ten fixtures; future drift (a deleted fixture, or one
// added without updating CONTRIBUTING.md and SCHEMA.md) fails
// loudly.
func TestGeneric_FixtureCount(t *testing.T) {
	formats.FixtureCount(t, "testdata", 10)
}
