package generic_test

import (
	"context"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
)

// TestGeneric_ParseMinSeverityError — default opts emit only error
// Events. Warnings present in the input are not Events; they remain
// available as context for the surviving error Events.
func TestGeneric_ParseMinSeverityError(t *testing.T) {
	input := "WARN: low memory\nERROR: thing broke\nWARN: still low\n"
	ch, _ := getGeneric(t).Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
	got := drain(ch)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1: %v", len(got), eventTitles(got))
	}
	if got[0].Severity != event.SeverityError {
		t.Errorf("Severity = %q, want error", got[0].Severity)
	}
	if got[0].Title != "ERROR: thing broke" {
		t.Errorf("Title = %q, want \"ERROR: thing broke\"", got[0].Title)
	}
}

// TestGeneric_ParseKeepWarnings — KeepWarnings=true emits both
// errors and warnings.
func TestGeneric_ParseKeepWarnings(t *testing.T) {
	input := "WARN: low memory\nERROR: thing broke\nWARN: still low\n"
	ch, _ := getGeneric(t).Parse(context.Background(),
		strings.NewReader(input),
		formats.ParseOpts{KeepWarnings: true})
	got := drain(ch)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3 (1 error + 2 warnings): %v", len(got), eventTitles(got))
	}
	wantTitles := []string{"WARN: low memory", "ERROR: thing broke", "WARN: still low"}
	for i, w := range wantTitles {
		if got[i].Title != w {
			t.Errorf("Events[%d].Title = %q, want %q", i, got[i].Title, w)
		}
	}
}

// TestGeneric_ParseMinSeverityInfoEmitsWarnings — even though the
// v1 catalogue has no info patterns, setting MinSeverity=info does
// not suppress warnings. Documents the precedence rule that an
// explicit MinSeverity wins over the KeepWarnings=false default.
func TestGeneric_ParseMinSeverityInfoEmitsWarnings(t *testing.T) {
	input := "WARN: low memory\nERROR: thing broke\n"
	ch, _ := getGeneric(t).Parse(context.Background(),
		strings.NewReader(input),
		formats.ParseOpts{MinSeverity: event.SeverityInfo})
	got := drain(ch)
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2: %v", len(got), eventTitles(got))
	}
	if got[0].Severity != event.SeverityWarn || got[1].Severity != event.SeverityError {
		t.Errorf("severities = %q, %q; want warn, error", got[0].Severity, got[1].Severity)
	}
}

// TestGeneric_ParseFilterBeforeContext — a warning anchor inside an
// error's context window doesn't become its own Event when warnings
// are filtered, but the line still appears in the error Event's
// Context. Proves filtering happens "drop the anchor but keep the
// surrounding lines as context" rather than "skip the line
// entirely".
func TestGeneric_ParseFilterBeforeContext(t *testing.T) {
	input := strings.Join([]string{
		"info a",
		"info b",
		"WARN: warning inside context",
		"ERROR: the real failure",
		"info c",
		"info d",
		"info e",
	}, "\n") + "\n"
	ch, _ := getGeneric(t).Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
	got := drain(ch)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1: %v", len(got), eventTitles(got))
	}
	ev := got[0]
	if ev.Title != "ERROR: the real failure" {
		t.Errorf("Title = %q, want \"ERROR: the real failure\"", ev.Title)
	}
	// The WARN line must appear in the pre-context window even
	// though it was filtered as an anchor.
	foundWarnInContext := false
	for _, c := range ev.Context {
		if c == "WARN: warning inside context" {
			foundWarnInContext = true
			break
		}
	}
	if !foundWarnInContext {
		t.Errorf("WARN line should appear in Context even when filtered as an anchor; Context=%q", ev.Context)
	}
}

// TestGeneric_ParseEffectiveMinSeverityPrecedence — verifies the
// precedence matrix in effectiveMinSeverity:
//
//	MinSeverity unset  + KeepWarnings=false → error-only
//	MinSeverity unset  + KeepWarnings=true  → error + warn
//	MinSeverity=error  + KeepWarnings=false → error-only
//	MinSeverity=error  + KeepWarnings=true  → error + warn
//	MinSeverity=warn   + KeepWarnings=false → error + warn
//	MinSeverity=info   + KeepWarnings=false → error + warn (explicit wins)
func TestGeneric_ParseEffectiveMinSeverityPrecedence(t *testing.T) {
	cases := []struct {
		name      string
		opts      formats.ParseOpts
		wantCount int
	}{
		{"unset+false", formats.ParseOpts{}, 1},
		{"unset+true", formats.ParseOpts{KeepWarnings: true}, 2},
		{"error+false", formats.ParseOpts{MinSeverity: event.SeverityError}, 1},
		{"error+true", formats.ParseOpts{MinSeverity: event.SeverityError, KeepWarnings: true}, 2},
		{"warn+false", formats.ParseOpts{MinSeverity: event.SeverityWarn}, 2},
		{"info+false", formats.ParseOpts{MinSeverity: event.SeverityInfo}, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			input := "WARN: w\nERROR: e\n"
			ch, _ := getGeneric(t).Parse(context.Background(), strings.NewReader(input), c.opts)
			got := drain(ch)
			if len(got) != c.wantCount {
				t.Errorf("case %s: got %d events, want %d (%v)", c.name, len(got), c.wantCount, eventTitles(got))
			}
		})
	}
}
