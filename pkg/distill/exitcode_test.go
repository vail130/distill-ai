package distill_test

import (
	"testing"

	"github.com/vail130/distill-ai/pkg/distill"
)

// TestExitCodeFromSummary_NilSummary asserts the documented
// nil-receiver behaviour: returns ExitNoEvents rather than
// panicking. Mirrors the Summary.ForcedDrops nil-safe contract.
func TestExitCodeFromSummary_NilSummary(t *testing.T) {
	if got := distill.ExitCodeFromSummary(nil); got != distill.ExitNoEvents {
		t.Errorf("ExitCodeFromSummary(nil) = %d, want %d (ExitNoEvents)",
			got, distill.ExitNoEvents)
	}
}

// TestExitCodeFromSummary_NoEvents asserts the empty-pipeline arm:
// zero EventsEmitted and no budget drops → ExitNoEvents.
func TestExitCodeFromSummary_NoEvents(t *testing.T) {
	s := &distill.Summary{EventsEmitted: 0}
	if got := distill.ExitCodeFromSummary(s); got != distill.ExitNoEvents {
		t.Errorf("ExitCodeFromSummary = %d, want %d", got, distill.ExitNoEvents)
	}
}

// TestExitCodeFromSummary_CleanRun asserts the happy path: at
// least one Event, no forced drops → ExitOK.
func TestExitCodeFromSummary_CleanRun(t *testing.T) {
	s := &distill.Summary{EventsEmitted: 3}
	if got := distill.ExitCodeFromSummary(s); got != distill.ExitOK {
		t.Errorf("ExitCodeFromSummary = %d, want %d (ExitOK)",
			got, distill.ExitOK)
	}
}

// TestExitCodeFromSummary_BudgetForcedDrops asserts the documented
// precedence: ExitPartial wins over ExitNoEvents. A budget that
// drops every event still produces ExitPartial, not ExitNoEvents,
// because "budget dropped everything" is meaningfully different
// from "input was clean."
func TestExitCodeFromSummary_BudgetForcedDrops(t *testing.T) {
	s := &distill.Summary{
		EventsEmitted:       0,
		EventsDroppedBudget: 5,
	}
	if got := distill.ExitCodeFromSummary(s); got != distill.ExitPartial {
		t.Errorf("ExitCodeFromSummary = %d, want %d (ExitPartial)",
			got, distill.ExitPartial)
	}
}

// TestExitCodeFromSummary_BudgetTruncations asserts the
// EventsTruncated arm of ExitPartial. Truncations and drops both
// trigger the same code, mirroring Summary.ForcedDrops.
func TestExitCodeFromSummary_BudgetTruncations(t *testing.T) {
	s := &distill.Summary{
		EventsEmitted:   3,
		EventsTruncated: 1,
	}
	if got := distill.ExitCodeFromSummary(s); got != distill.ExitPartial {
		t.Errorf("ExitCodeFromSummary = %d, want %d", got, distill.ExitPartial)
	}
}

// TestExitCodeConstants_StableValues pins the documented numeric
// values. A consumer's bash script depends on these being 0/1/2/3;
// a future refactor must NOT renumber them.
func TestExitCodeConstants_StableValues(t *testing.T) {
	cases := map[int]int{
		distill.ExitOK:       0,
		distill.ExitNoEvents: 1,
		distill.ExitError:    2,
		distill.ExitPartial:  3,
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("exit-code constant drift: got %d, want %d", got, want)
		}
	}
}
