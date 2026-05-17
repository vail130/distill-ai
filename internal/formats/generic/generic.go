// Package generic implements the regex-driven fallback Format. The
// detector picks this format under the reserved name "generic" when
// no specific Format scores above event.ConfidenceMinDetect (0.6).
//
// generic cannot do what pytest / jest / gotest do — it has no test-
// runner semantics, no structured frame extraction beyond best-effort
// file:line: matches. It exists so that piping arbitrary log output
// through distill-ai yields something rather than nothing: a sequence
// of severity-bucketed Events anchored to ERROR, FATAL, panic,
// Exception, Traceback, and friends, with N lines of surrounding
// context.
//
// # Detector invariant
//
// The detector excludes "generic" from the candidate set up front
// (see internal/detect § GenericFormatName), so generic's Detect is
// never compared against a specific format on ties. Detect therefore
// returns a deliberate low floor — confidenceFloor (0.1) — to
// communicate "we can probably find something useful" rather than
// "we recognise this format." 0.1 is intentionally below
// event.ConfidenceMinDetect (0.6); future contributors must not
// inflate it to try to make generic "win" a tie.
//
// See docs/formats/generic.md for the user-facing description of
// what the parser extracts and what it drops, and TODO.md § M9 for
// the milestone scope.
package generic

import (
	"context"
	"io"
	"regexp"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
)

// confidenceFloor is the Confidence value Detect returns when the
// sample contains at least one line matching the severity catalogue.
// Set well below event.ConfidenceMinDetect (0.6) so a specific format
// always wins; the detector reserves the generic format for the
// fallback path rather than scoring it as a normal candidate.
const confidenceFloor event.Confidence = 0.1

// detectPattern matches any line that looks like a severity-anchored
// log marker. It is intentionally cheap: the Detect contract requires
// a fast scan over the 4 KiB sample. The real catalogue used by Parse
// (M9.2) is richer; this is the minimal "is there any severity hit"
// probe.
//
// Patterns recognised: ERROR, FATAL, WARN/WARNING, panic:, Exception:,
// Traceback (with trailing space anchoring the Python "Traceback
// (most recent call last):" form), and "Error:" / "Warning:" prefixes.
var detectPattern = regexp.MustCompile(
	`(?m)^.*(?:\bERROR\b|\bFATAL\b|\bWARN(?:ING)?\b|\bpanic:|\bException:|\bTraceback |\bError:|\bWarning:).*$`,
)

// Format is the generic regex-driven fallback parser. Implements
// formats.Format. Registered under the reserved name "generic" at
// init() time.
type Format struct{}

// Name returns "generic" — the reserved name the detector looks up
// when falling back. Constant for the lifetime of the value.
func (Format) Name() string { return "generic" }

// Detect reports a low confidence floor (confidenceFloor, 0.1) when
// the sample contains at least one severity-anchored line, and 0
// otherwise. The floor is below event.ConfidenceMinDetect so the
// detector treats it as a "below threshold" result and exercises its
// fallback path — generic is excluded from the candidate set up
// front and only ever wins via that fallback. See package docs.
func (Format) Detect(sample []byte) event.Confidence {
	if detectPattern.Match(sample) {
		return confidenceFloor
	}
	return 0
}

// Parse is the entry point for the regex-driven scanner. M9.1 lands
// the skeleton: an immediately-closed channel with no error so the
// detector's fallback path can be exercised end-to-end. M9.2 fills
// in the severity-anchored scan loop; M9.3 adds traceback / panic
// block accumulation; M9.4 wires --severity / --keep-warnings.
//
// The signature already honours the contract documented on
// formats.Format.Parse: the channel is closed exactly once, the
// caller may drain after close, and r is not retained beyond return.
func (Format) Parse(_ context.Context, _ io.Reader, _ formats.ParseOpts) (<-chan event.Event, error) {
	ch := make(chan event.Event)
	close(ch)
	return ch, nil
}

// init registers Format under the reserved name so the binary picks
// it up by import side effect from cmd/distill-ai.
func init() { formats.Register(Format{}) }
