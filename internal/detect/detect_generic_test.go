package detect_test

import (
	"context"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/detect"
	"github.com/vail130/distill-ai/internal/formats"

	// Side-effect import: the test exercises the detector's
	// fallback-to-generic path using the real generic Format
	// rather than a fake, anchoring the wiring across packages.
	_ "github.com/vail130/distill-ai/internal/formats/generic"
)

// TestDetect_FallbackUsesGenericFormat — a sample that no specific
// format claims (low-entropy text) falls back to the real generic
// Format registered at init, with FellBackToGeneric=true. Pre-M9
// the same scenario returned ErrNoFormat; M9.1 closes that gap.
func TestDetect_FallbackUsesGenericFormat(t *testing.T) {
	// Don't ResetForTest here — we want the package-level init()
	// of internal/formats/generic to remain in effect so the
	// detector finds the real Format via the global registry.
	// (Other tests in this file that need isolation use
	// detect.Opts.Formats explicitly.)
	generic, ok := formats.Get(detect.GenericFormatName)
	if !ok {
		t.Fatalf("generic format not registered; expected the side-effect import to have wired it")
	}
	// Pass an explicit Formats list with only "generic" + one
	// specific pretend-format scoring below threshold, so the
	// detector exercises the fallback path deterministically.
	res, err := detect.Detect(context.Background(),
		strings.NewReader("Hello, world.\nThis is fine.\n"),
		detect.Opts{
			Formats: []formats.Format{
				&fakeFormat{name: "pytest", score: 0.4},
				generic,
			},
		},
	)
	if err != nil {
		t.Fatalf("Detect: %v; expected fallback to generic without error", err)
	}
	if !res.FellBackToGeneric {
		t.Fatalf("FellBackToGeneric = false; expected fallback")
	}
	if res.Format.Name() != detect.GenericFormatName {
		t.Errorf("Format.Name() = %q, want %q", res.Format.Name(), detect.GenericFormatName)
	}
}
