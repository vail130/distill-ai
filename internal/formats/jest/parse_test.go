package jest_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
	_ "github.com/vail130/distill-ai/internal/formats/jest"
	"github.com/vail130/distill-ai/internal/testutil"
)

// drain reads every Event from ch with a deadline so a hung test
// fails loudly instead of timing out the whole suite.
func drain(t *testing.T, ch <-chan event.Event) []event.Event {
	t.Helper()
	var out []event.Event
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-deadline:
			t.Fatal("drain timeout: scanner did not close channel")
		}
	}
}

// parseString feeds s to the registered jest format and returns
// the Events emitted, with ctx-cancellation handling so tests can
// assert on the slice.
func parseString(t *testing.T, s string) []event.Event {
	t.Helper()
	f, ok := formats.Get("jest")
	if !ok {
		t.Fatal("jest not registered")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := f.Parse(ctx, strings.NewReader(s), formats.ParseOpts{})
	if err != nil {
		t.Fatalf("Parse err = %v", err)
	}
	return drain(t, ch)
}

// TestJest_ParseSingleFailure — the canonical default-reporter
// failure block emits exactly one Event with the expected fields.
func TestJest_ParseSingleFailure(t *testing.T) {
	input := `FAIL src/auth.test.js
  Auth
    ✗ login redirects to dashboard (12 ms)

  ● Auth › login redirects to dashboard

    expect(received).toBe(expected) // Object.is equality

    Expected: 302
    Received: 200

      14 |     const res = await login('user', 'pass');
    > 15 |     expect(res.status).toBe(302);
         |                        ^
      16 |   });

      at Object.<anonymous> (src/auth.test.js:15:24)

Test Suites: 1 failed, 1 total
Tests:       1 failed, 1 total
`
	events := parseString(t, input)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	ev := events[0]
	if ev.Severity != event.SeverityError {
		t.Errorf("Severity = %v, want error", ev.Severity)
	}
	if ev.Kind != "test_failure" {
		t.Errorf("Kind = %q, want test_failure", ev.Kind)
	}
	// Title prefers the expect() call when present.
	if !strings.Contains(ev.Title, "expect(received).toBe(expected)") {
		t.Errorf("Title = %q, want substring expect(received).toBe(expected)", ev.Title)
	}
	if ev.Location == nil {
		t.Fatal("Location = nil, want non-nil")
	}
	if ev.Location.File != "src/auth.test.js" || ev.Location.Line != 15 {
		t.Errorf("Location = %+v, want File=src/auth.test.js Line=15", *ev.Location)
	}
	if ev.Location.Column == nil || *ev.Location.Column != 24 {
		t.Errorf("Location.Column = %v, want 24", ev.Location.Column)
	}
	if ev.Metadata["test_id"] != "Auth > login redirects to dashboard" {
		t.Errorf("test_id = %q, want %q (Unicode chevron must normalise)",
			ev.Metadata["test_id"], "Auth > login redirects to dashboard")
	}
	if ev.Metadata["suite_file"] != "src/auth.test.js" {
		t.Errorf("suite_file = %q, want src/auth.test.js", ev.Metadata["suite_file"])
	}
}

// TestJest_ParseMultiFailureAcrossFiles — three failures spread
// across two files; each Event carries the right suite_file metadata.
func TestJest_ParseMultiFailureAcrossFiles(t *testing.T) {
	input := `FAIL src/auth.test.js
  ● Auth › first failure
    AssertionError: first thing failed
      at src/auth.test.js:10:5

  ● Auth › second failure
    AssertionError: second thing failed
      at src/auth.test.js:20:5

FAIL src/utils.test.js
  ● Utils › third failure
    AssertionError: third thing failed
      at src/utils.test.js:5:5

Test Suites: 2 failed, 2 total
Tests:       3 failed, 3 total
`
	events := parseString(t, input)
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	wantSuites := []string{"src/auth.test.js", "src/auth.test.js", "src/utils.test.js"}
	for i, ev := range events {
		if ev.Metadata["suite_file"] != wantSuites[i] {
			t.Errorf("events[%d].suite_file = %q, want %q",
				i, ev.Metadata["suite_file"], wantSuites[i])
		}
		if ev.Kind != "test_failure" {
			t.Errorf("events[%d].Kind = %q, want test_failure", i, ev.Kind)
		}
	}
}

// TestJest_ParseSkipsPassing — a PASS file header opens no Events;
// only the FAIL block contributes.
func TestJest_ParseSkipsPassing(t *testing.T) {
	input := `PASS src/utils.spec.ts
  Utils
    ✓ should add (2 ms)

FAIL src/auth.test.js
  ● Auth › login fails
    Error: nope
      at src/auth.test.js:5:5

Test Suites: 1 failed, 1 passed, 2 total
Tests:       1 failed, 1 passed, 2 total
`
	events := parseString(t, input)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Metadata["suite_file"] != "src/auth.test.js" {
		t.Errorf("suite_file = %q, want src/auth.test.js", events[0].Metadata["suite_file"])
	}
}

// TestJest_ParseDropsConsoleLog — `console.log` output between
// tests must not leak into the failure Event's Body or appear as
// its own Event.
func TestJest_ParseDropsConsoleLog(t *testing.T) {
	input := `FAIL src/api.test.js
  console.log
    DEBUG: warming up cache
      at src/api.js:14:9

  console.error
    [error] downstream timeout
      at src/api.js:42:11

  ● Api › fetches user

    Error: timeout
      at src/api.test.js:8:5

Test Suites: 1 failed, 1 total
`
	events := parseString(t, input)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	for _, line := range events[0].Body {
		if strings.Contains(line, "warming up cache") {
			t.Errorf("Body leaked console.log line: %q", line)
		}
		if strings.Contains(line, "downstream timeout") {
			t.Errorf("Body leaked console.error line: %q", line)
		}
	}
	if events[0].Title != "Error: timeout" {
		t.Errorf("Title = %q, want Error: timeout", events[0].Title)
	}
}

// TestJest_ParseStripsANSIFromTitle — ANSI colour escapes in the
// assertion line must be stripped from Title but retained in Body.
func TestJest_ParseStripsANSIFromTitle(t *testing.T) {
	input := "FAIL src/auth.test.js\n" +
		"  \x1b[31m●\x1b[0m Auth \u203a login fails\n" +
		"\n" +
		"    \x1b[31mexpect(received).toBe(expected)\x1b[0m\n" +
		"\n" +
		"      at src/auth.test.js:1:1\n" +
		"\n" +
		"Test Suites: 1 failed, 1 total\n"
	events := parseString(t, input)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if strings.Contains(events[0].Title, "\x1b[") {
		t.Errorf("Title retains ANSI escapes: %q", events[0].Title)
	}
	if !strings.Contains(events[0].Title, "expect(received).toBe(expected)") {
		t.Errorf("Title = %q, want expect(received).toBe(expected)", events[0].Title)
	}
	// Body must retain ANSI so the user sees what jest emitted.
	foundANSI := false
	for _, line := range events[0].Body {
		if strings.Contains(line, "\x1b[31m") {
			foundANSI = true
			break
		}
	}
	if !foundANSI {
		t.Error("Body has no ANSI escapes; the scanner should preserve them in Body")
	}
}

// TestJest_ParseStreaming — events emerge as their blocks close,
// not at EOF. Use testutil.SlowReader to feed bytes at a measurable
// interval and require the first Event to arrive before the source
// could have finished.
func TestJest_ParseStreaming(t *testing.T) {
	input := `FAIL src/auth.test.js
  ● Auth › first failure
    Error: first
      at src/auth.test.js:1:1

  ● Auth › second failure
    Error: second
      at src/auth.test.js:2:1

  ● Auth › third failure
    Error: third
      at src/auth.test.js:3:1

Test Suites: 1 failed, 1 total
`
	const chunkDelay = 5 * time.Millisecond
	slow := &testutil.SlowReader{
		Inner:      strings.NewReader(input),
		ChunkSize:  4,
		ChunkDelay: chunkDelay,
	}
	f, _ := formats.Get("jest")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := f.Parse(ctx, slow, formats.ParseOpts{})
	if err != nil {
		t.Fatalf("Parse err = %v", err)
	}
	// Total source-feed time is roughly (chunks * delay). The first
	// Event is forwarded when the second `●` line is consumed —
	// that's roughly halfway through the input. Asserting it
	// arrives before EOF (with a small margin) confirms streaming
	// rather than buffer-to-EOF.
	totalFeed := time.Duration(len(input)/4+1) * chunkDelay
	deadline := time.After(totalFeed - chunkDelay*8)
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before any event arrived")
		}
		if ev.Kind != "test_failure" {
			t.Errorf("first event kind = %q, want test_failure", ev.Kind)
		}
	case <-deadline:
		t.Fatalf("no event after %v; scanner buffered to EOF", totalFeed/2)
	}
	// Drain the rest so the goroutine exits cleanly.
	drained := 0
	for range ch {
		drained++
	}
	_ = drained
}

// TestJest_ParseDeterministic — same input twice produces
// byte-identical Event sequences. Anchors the project's
// determinism invariant for this format.
func TestJest_ParseDeterministic(t *testing.T) {
	input := `FAIL src/auth.test.js
  ● Auth › fails
    Error: nope
      at src/auth.test.js:5:5

  ● Auth › fails again
    AssertionError: also nope
      at src/auth.test.js:10:5

Test Suites: 1 failed, 1 total
`
	a := parseString(t, input)
	b := parseString(t, input)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("two parses differ:\nfirst:  %+v\nsecond: %+v", a, b)
	}
}

// TestJest_ParseContextCancellation — cancelling ctx before any
// read closes the channel cleanly without leaking the scanner
// goroutine. Mirrors the pytest pattern: cancel up front, drain
// the channel, assert close within a short deadline.
func TestJest_ParseContextCancellation(t *testing.T) {
	input := `FAIL src/auth.test.js
  ● Auth › fails
    Error: nope
      at src/auth.test.js:5:5

Test Suites: 1 failed, 1 total
`
	f, _ := formats.Get("jest")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch, err := f.Parse(ctx, strings.NewReader(input), formats.ParseOpts{})
	if err != nil {
		t.Fatalf("Parse err = %v", err)
	}
	done := make(chan struct{})
	go func() {
		drained := 0
		for range ch {
			drained++
		}
		_ = drained
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("channel did not close after ctx cancel")
	}
}
