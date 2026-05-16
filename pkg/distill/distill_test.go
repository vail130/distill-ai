package distill_test

import (
	"testing"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
	"github.com/vail130/distill-ai/pkg/distill"
)

// assignAliases verifies at compile time that each public alias
// resolves to the underlying internal type. If a future refactor
// shadows an alias with a distinct named type, the assignments below
// fail to compile (Go does not allow implicit conversion between
// distinct named types) and the build breaks before tests run.
//
// Explicit type declarations on the LHS are deliberate: removing them
// (as the QF1011/ST1023 lints would prefer) defeats the purpose of
// the check, because inference would silently accept a shadowed type.
// .golangci.yml suppresses those two lints for this file.
//
//nolint:unused // referenced only by the compiler, by design
func assignAliases() {
	var _ event.Event = distill.Event{}
	var _ event.Severity = distill.SeverityError
	var _ event.Location = distill.Location{}
	var _ event.StackFrame = distill.StackFrame{}
	var _ event.Confidence = distill.ConfidenceMinDetect
	var _ formats.Format
	var _ formats.ParseOpts = distill.ParseOpts{}
	// Symmetric direction: internal → public.
	var _ distill.Event = event.Event{}
	var _ distill.Severity = event.SeverityWarn
	var _ distill.ParseOpts = formats.ParseOpts{}
}

func TestAliasesUsable(t *testing.T) {
	ev := distill.Event{
		Severity: distill.SeverityWarn,
		Kind:     "test",
		Title:    "ok",
		Body:     []string{"ok"},
		Count:    1,
	}
	if ev.Severity != distill.SeverityWarn {
		t.Errorf("alias usage broken: got %q", ev.Severity)
	}
}

func TestSeverityConstantsExported(t *testing.T) {
	want := map[distill.Severity]string{
		distill.SeverityError: "error",
		distill.SeverityWarn:  "warn",
		distill.SeverityInfo:  "info",
	}
	for sev, str := range want {
		if string(sev) != str {
			t.Errorf("Severity %v stringifies as %q, want %q", sev, sev, str)
		}
	}
}
