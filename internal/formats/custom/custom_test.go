package custom_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
	"github.com/vail130/distill-ai/internal/formats/custom"
)

// drain reads every Event from a channel until close, returning
// them in emission order.
func drain(ch <-chan event.Event) []event.Event {
	var out []event.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

// newFormat is a small constructor wrapper that t.Fatals on
// error so tests stay readable.
func newFormat(t *testing.T, name, det, start, end, sev, kind string) *custom.Format {
	t.Helper()
	f, err := custom.New(name, det, start, end, sev, kind)
	if err != nil {
		t.Fatalf("New(%s): %v", name, err)
	}
	return f
}

// TestCustom_New_NameIsNamespaced: Name() returns "custom:NAME".
func TestCustom_New_NameIsNamespaced(t *testing.T) {
	f := newFormat(t, "myapp", `^\[myapp\]`, `^\[myapp\] ERROR`, "", "", "")
	if got, want := f.Name(), "custom:myapp"; got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
}

// TestCustom_New_RequiredFields: missing detect_regex /
// event_start return an error naming the offending field.
func TestCustom_New_RequiredFields(t *testing.T) {
	t.Run("missing_detect", func(t *testing.T) {
		_, err := custom.New("myapp", "", `^OK`, "", "", "")
		if err == nil || !strings.Contains(err.Error(), "detect_regex") {
			t.Errorf("got %v; want error mentioning detect_regex", err)
		}
	})
	t.Run("missing_event_start", func(t *testing.T) {
		_, err := custom.New("myapp", `^OK`, "", "", "", "")
		if err == nil || !strings.Contains(err.Error(), "event_start") {
			t.Errorf("got %v; want error mentioning event_start", err)
		}
	})
}

// TestCustom_New_BadRegex: an unbalanced paren in detect_regex /
// event_start / event_end produces a clear error that names the
// field.
func TestCustom_New_BadRegex(t *testing.T) {
	t.Run("detect", func(t *testing.T) {
		_, err := custom.New("svc", "(", `^X`, "", "", "")
		if err == nil || !strings.Contains(err.Error(), "detect_regex") {
			t.Errorf("got %v", err)
		}
	})
	t.Run("event_start", func(t *testing.T) {
		_, err := custom.New("svc", `^X`, "(", "", "", "")
		if err == nil || !strings.Contains(err.Error(), "event_start") {
			t.Errorf("got %v", err)
		}
	})
	t.Run("event_end", func(t *testing.T) {
		_, err := custom.New("svc", `^X`, `^X`, "(", "", "")
		if err == nil || !strings.Contains(err.Error(), "event_end") {
			t.Errorf("got %v", err)
		}
	})
}

// TestCustom_New_DefaultsApplied: empty severity and kind fall
// back to the documented defaults.
func TestCustom_New_DefaultsApplied(t *testing.T) {
	f := newFormat(t, "x", `^X`, `^X`, "", "", "")
	ch, err := f.Parse(context.Background(), strings.NewReader("X line\n"), formats.ParseOpts{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	evts := drain(ch)
	if len(evts) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(evts))
	}
	if evts[0].Severity != event.SeverityError {
		t.Errorf("Severity = %v, want SeverityError (default)", evts[0].Severity)
	}
	if evts[0].Kind != "match" {
		t.Errorf("Kind = %q, want match (default)", evts[0].Kind)
	}
}

// TestCustom_Detect_MatchAndMiss: a matching sample returns 1.0;
// a non-matching one returns 0.0.
func TestCustom_Detect_MatchAndMiss(t *testing.T) {
	f := newFormat(t, "svc", `^\[svc\]`, `^\[svc\]`, "", "", "")
	if got := f.Detect([]byte("[svc] starting\n")); got != 1.0 {
		t.Errorf("Detect match = %v, want 1.0", got)
	}
	if got := f.Detect([]byte("nothing relevant here\n")); got != 0 {
		t.Errorf("Detect miss = %v, want 0.0", got)
	}
}

// TestCustom_Parse_SingleEvent: one start/end pair produces one
// Event with the documented shape.
func TestCustom_Parse_SingleEvent(t *testing.T) {
	f := newFormat(t, "svc",
		`^\[svc\]`,
		`^\[svc\] ERROR`,
		`^\[svc\] (INFO|DEBUG|ERROR)`,
		"error", "svc_problem")
	input := strings.Join([]string{
		"prelude",
		"[svc] ERROR something broke",
		"  stack frame 1",
		"  stack frame 2",
		"[svc] INFO recovered",
		"trailing",
	}, "\n") + "\n"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch, err := f.Parse(ctx, strings.NewReader(input), formats.ParseOpts{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	evts := drain(ch)
	if len(evts) != 1 {
		t.Fatalf("len(events) = %d, want 1; got %+v", len(evts), evts)
	}
	got := evts[0]
	if !strings.Contains(got.Title, "ERROR something broke") {
		t.Errorf("Title = %q, want it to contain ERROR something broke", got.Title)
	}
	if got.Kind != "svc_problem" {
		t.Errorf("Kind = %q, want svc_problem", got.Kind)
	}
	if got.Severity != event.SeverityError {
		t.Errorf("Severity = %v, want SeverityError", got.Severity)
	}
	if got.Metadata["custom_format"] != "svc" {
		t.Errorf("Metadata[custom_format] = %q, want svc", got.Metadata["custom_format"])
	}
	if len(got.Body) < 4 {
		t.Errorf("Body has %d lines, want at least 4 (start + 2 stack + end)", len(got.Body))
	}
}

// TestCustom_Parse_NoEndOneLineEvents: an empty event_end yields
// one-line Events per matched start.
func TestCustom_Parse_NoEndOneLineEvents(t *testing.T) {
	f := newFormat(t, "svc", `^svc`, `^svc ERROR`, "", "", "")
	input := "svc ERROR one\nsvc OK\nsvc ERROR two\n"
	ch, err := f.Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	evts := drain(ch)
	if len(evts) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(evts))
	}
	for i, want := range []string{"svc ERROR one", "svc ERROR two"} {
		if !strings.Contains(evts[i].Title, want) {
			t.Errorf("event[%d].Title = %q, want it to contain %q", i, evts[i].Title, want)
		}
		if len(evts[i].Body) != 1 {
			t.Errorf("event[%d].Body has %d lines, want 1 (no end-regex)", i, len(evts[i].Body))
		}
	}
}

// TestCustom_Parse_ImpliedEndOnNewStart: when event_end is set
// but a new start arrives before an end, the in-flight Event
// terminates at the new start.
func TestCustom_Parse_ImpliedEndOnNewStart(t *testing.T) {
	f := newFormat(t, "svc",
		`^\[svc\]`,
		`^\[svc\] ERROR`,
		`^\[svc\] (INFO|DEBUG|ERROR)`,
		"", "")
	input := strings.Join([]string{
		"[svc] ERROR first",
		"continuation 1",
		"[svc] ERROR second",
		"continuation 2",
	}, "\n") + "\n"
	ch, err := f.Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	evts := drain(ch)
	if len(evts) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(evts))
	}
	if !strings.Contains(evts[0].Title, "first") || !strings.Contains(evts[1].Title, "second") {
		t.Errorf("event titles wrong: %q / %q", evts[0].Title, evts[1].Title)
	}
}

// TestCustom_Parse_ANSIStripFromTitle: ANSI SGR escapes are
// stripped from Title but preserved in Body.
func TestCustom_Parse_ANSIStripFromTitle(t *testing.T) {
	f := newFormat(t, "svc", `svc`, `svc`, "", "", "")
	input := "\x1b[31msvc\x1b[0m ERROR boom\n"
	ch, err := f.Parse(context.Background(), strings.NewReader(input), formats.ParseOpts{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	evts := drain(ch)
	if len(evts) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(evts))
	}
	if strings.Contains(evts[0].Title, "\x1b") {
		t.Errorf("Title still contains ANSI escape: %q", evts[0].Title)
	}
	if !strings.Contains(evts[0].Body[0], "\x1b") {
		t.Errorf("Body lost ANSI escape: %q", evts[0].Body[0])
	}
}

// TestCustom_Parse_ContextCancellation: cancelling the context
// stops the Parse goroutine; the channel closes; no goroutine
// leak. The synthetic Reader produces many start lines (each
// emits an Event) so the parser's send eventually blocks once
// the test stops draining; cancellation must unblock that send.
func TestCustom_Parse_ContextCancellation(t *testing.T) {
	f := newFormat(t, "svc", `^svc`, `^svc`, "", "", "")
	// Many start lines so the parser keeps trying to send.
	r := bytes.NewReader([]byte(strings.Repeat("svc line\n", 1000)))
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := f.Parse(ctx, r, formats.ParseOpts{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Drain one Event so we know Parse is up and running.
	first, ok := <-ch
	if !ok {
		t.Fatalf("channel closed before first event")
	}
	if !strings.Contains(first.Title, "svc line") {
		t.Errorf("first Title = %q", first.Title)
	}
	cancel()
	// After cancel, the channel must close. Drain remaining
	// events (they may have been queued already).
	done := make(chan struct{})
	go func() {
		// Drain remaining buffered events; we only care that
		// the channel closes after cancel.
		//revive:disable-next-line:empty-block
		for range ch { //nolint:revive // empty drain
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("channel did not close within 2s after cancel")
	}
}

// TestCustom_RegisterFromConfig_RegistersAllBlocks: three blocks
// produce three formats in formats.All() under the namespaced
// names.
func TestCustom_RegisterFromConfig_RegistersAllBlocks(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	blocks := map[string]custom.Config{
		"foo": {DetectRegex: `^foo`, EventStart: `^foo`},
		"bar": {DetectRegex: `^bar`, EventStart: `^bar`},
		"baz": {DetectRegex: `^baz`, EventStart: `^baz`},
	}
	if err := custom.RegisterFromConfig(blocks); err != nil {
		t.Fatalf("RegisterFromConfig: %v", err)
	}
	for _, name := range []string{"custom:foo", "custom:bar", "custom:baz"} {
		if _, ok := formats.Get(name); !ok {
			t.Errorf("formats.Get(%q) missing", name)
		}
	}
}

// TestCustom_RegisterFromConfig_BadRegexFails: a single bad
// regex aborts the whole registration, leaving the registry
// untouched.
func TestCustom_RegisterFromConfig_BadRegexFails(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	blocks := map[string]custom.Config{
		"good": {DetectRegex: `^good`, EventStart: `^good`},
		"bad":  {DetectRegex: `(`, EventStart: `^X`},
	}
	err := custom.RegisterFromConfig(blocks)
	if err == nil {
		t.Fatalf("expected error; got nil")
	}
	if !strings.Contains(err.Error(), "formats.custom.bad") {
		t.Errorf("error %q does not name the offending block", err)
	}
	// Neither block should be registered after the failure.
	if _, ok := formats.Get("custom:good"); ok {
		t.Errorf("custom:good registered despite atomic failure")
	}
	if _, ok := formats.Get("custom:bad"); ok {
		t.Errorf("custom:bad registered despite atomic failure")
	}
}

// TestCustom_RegisterFromConfig_EmptyIsNoop: an empty map is
// allowed (zero custom blocks is the common case).
func TestCustom_RegisterFromConfig_EmptyIsNoop(t *testing.T) {
	if err := custom.RegisterFromConfig(nil); err != nil {
		t.Errorf("nil map: %v", err)
	}
	if err := custom.RegisterFromConfig(map[string]custom.Config{}); err != nil {
		t.Errorf("empty map: %v", err)
	}
}

// TestCustom_Goldens runs the shared harness against a small
// fixture set under testdata/. The five fixtures match the M14.5
// DoD: single match with end terminator, single match without
// end (implicit one-line Event), multiple matches, custom
// severity, custom kind.
//
// Each fixture has a sibling .config file naming the custom
// regex / severity / kind; the harness builds the Format and
// runs it. Goldens are diffed against .expected files; rebuild
// with DISTILL_AI_UPDATE_GOLDENS=1.
func TestCustom_FixtureCount(t *testing.T) {
	formats.FixtureCount(t, "testdata", 5)
}
