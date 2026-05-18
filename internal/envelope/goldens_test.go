package envelope_test

import (
	"testing"

	"github.com/vail130/distill-ai/internal/envelope"
	"github.com/vail130/distill-ai/internal/envelope/githubactions"
	"github.com/vail130/distill-ai/internal/envelope/gitlabci"
	"github.com/vail130/distill-ai/internal/formats"
	"github.com/vail130/distill-ai/internal/formats/generic"
	"github.com/vail130/distill-ai/internal/formats/gotest"
	"github.com/vail130/distill-ai/internal/formats/pytest"
)

// TestEnvelope_Goldens walks internal/envelope/testdata/ and runs
// each *.input fixture through envelope.Wrap + the inner format's
// Parse. The harness compares against the matching *.expected
// file. Regenerate with DISTILL_AI_UPDATE_GOLDENS=1.
//
// The test explicitly re-registers strippers and formats rather
// than relying on init-time side-effect imports. Sibling tests in
// this package call envelope.ResetForTest / formats.ResetForTest
// via t.Cleanup; relying on init-time registration would leave the
// goldens harness running against an empty registry whenever the
// goldens test happens to run last.
func TestEnvelope_Goldens(t *testing.T) {
	registerForGoldens(t)
	envelope.RunGoldens(t, "testdata")
}

// TestEnvelope_FixtureCount pins the fixture set to exactly the
// six enumerated in the M13.5 DoD so future drift surfaces
// immediately.
func TestEnvelope_FixtureCount(t *testing.T) {
	envelope.FixtureCount(t, "testdata", 6)
}

// registerForGoldens populates both registries with the strippers
// and formats the goldens harness needs, restoring them on cleanup.
// Sibling tests in this package may have cleared the registries via
// t.Cleanup; this helper makes the goldens test independent of run
// order.
func registerForGoldens(t *testing.T) {
	t.Helper()
	envelope.ResetForTest()
	formats.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	t.Cleanup(formats.ResetForTest)
	envelope.Register(githubactions.Stripper{})
	envelope.Register(gitlabci.Stripper{})
	formats.Register(generic.Format{})
	formats.Register(gotest.Format{})
	formats.Register(pytest.Format{})
}
