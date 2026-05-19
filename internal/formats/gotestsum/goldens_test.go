package gotestsum_test

import (
	"testing"

	"github.com/vail130/distill-ai/internal/formats"
	"github.com/vail130/distill-ai/internal/formats/gotestsum"
)

func TestGotestsum_Goldens(t *testing.T) {
	formats.RunGoldens(t, gotestsum.Format{}, "testdata")
}

func TestGotestsum_FixtureCount(t *testing.T) {
	formats.FixtureCount(t, "testdata", 5)
}
