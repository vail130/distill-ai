package jest_test

import (
	"strings"
	"testing"
)

// TestJest_ParseExtractsFrames — a default-reporter failure block
// populates Event.Frames with every `at` line. Order is source
// order; Vendor flags are not set by the parser (the M5
// CollapseStage owns that).
func TestJest_ParseExtractsFrames(t *testing.T) {
	input := `FAIL src/auth.test.js
  ● Auth › login fails

    Error: nope

      at Object.<anonymous> (src/auth.test.js:15:24)
      at runTest (node_modules/jest-runner/build/runTest.js:42:10)
      at processTicksAndRejections (internal/process/task_queues.js:95:5)

Test Suites: 1 failed, 1 total
`
	events := parseString(t, input)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	ev := events[0]
	if len(ev.Frames) != 3 {
		t.Fatalf("got %d frames, want 3: %+v", len(ev.Frames), ev.Frames)
	}
	wantFiles := []string{
		"src/auth.test.js",
		"node_modules/jest-runner/build/runTest.js",
		"internal/process/task_queues.js",
	}
	wantLines := []int{15, 42, 95}
	for i, f := range ev.Frames {
		if f.File != wantFiles[i] {
			t.Errorf("frames[%d].File = %q, want %q", i, f.File, wantFiles[i])
		}
		if f.Line != wantLines[i] {
			t.Errorf("frames[%d].Line = %d, want %d", i, f.Line, wantLines[i])
		}
		if f.Vendor {
			t.Errorf("frames[%d].Vendor = true; parser must leave it false (CollapseStage owns it)", i)
		}
	}
	if ev.Frames[0].Function != "Object.<anonymous>" {
		t.Errorf("frames[0].Function = %q, want Object.<anonymous>", ev.Frames[0].Function)
	}
	if ev.Frames[1].Function != "runTest" {
		t.Errorf("frames[1].Function = %q, want runTest", ev.Frames[1].Function)
	}
}

// TestJest_ParseFramesNilWhenAbsent — a block whose stack has
// been suppressed (jest's --noStackTrace) yields Frames=nil. The
// failure Event still emits with its Title and Body.
func TestJest_ParseFramesNilWhenAbsent(t *testing.T) {
	input := `FAIL src/auth.test.js
  ● Auth › login fails

    Error: nope without a stack

Test Suites: 1 failed, 1 total
`
	events := parseString(t, input)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Frames != nil {
		t.Errorf("Frames = %v, want nil", events[0].Frames)
	}
	if events[0].Title != "Error: nope without a stack" {
		t.Errorf("Title = %q, want Error: nope without a stack", events[0].Title)
	}
}

// TestJest_ParseFramesNoFunctionShape — a frame in the no-function
// form (common in async / bundled output) yields a StackFrame
// with Function empty and File/Line populated.
func TestJest_ParseFramesNoFunctionShape(t *testing.T) {
	input := `FAIL src/bundle.test.js
  ● Bundle › fails

    Error: bundled

      at src/bundle.test.js:99:10

Test Suites: 1 failed, 1 total
`
	events := parseString(t, input)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if len(events[0].Frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(events[0].Frames))
	}
	f := events[0].Frames[0]
	if f.Function != "" {
		t.Errorf("Function = %q, want empty (no-fn shape)", f.Function)
	}
	if f.File != "src/bundle.test.js" || f.Line != 99 {
		t.Errorf("frame = %+v, want File=src/bundle.test.js Line=99", f)
	}
}

// TestJest_ParseSuiteError — a `● Test suite failed to run` header
// promotes the Event Kind to suite_error and suppresses test_id.
func TestJest_ParseSuiteError(t *testing.T) {
	input := `FAIL src/import-broken.test.js
  ● Test suite failed to run

    Cannot find module 'missing-pkg'

      at Resolver.resolveModule (node_modules/jest-resolve/build/index.js:303:11)

Test Suites: 1 failed, 1 total
`
	events := parseString(t, input)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	ev := events[0]
	if ev.Kind != "suite_error" {
		t.Errorf("Kind = %q, want suite_error", ev.Kind)
	}
	if _, ok := ev.Metadata["test_id"]; ok {
		t.Errorf("Metadata has test_id %q; suite errors should not", ev.Metadata["test_id"])
	}
	if ev.Metadata["suite_file"] != "src/import-broken.test.js" {
		t.Errorf("suite_file = %q, want src/import-broken.test.js",
			ev.Metadata["suite_file"])
	}
	// Suite errors still carry frames so the user can find the
	// import that failed to resolve.
	if len(ev.Frames) != 1 {
		t.Errorf("Frames = %v, want one frame", ev.Frames)
	}
}

// TestJest_ParseVerboseSameAsTerse — the `--verbose` reporter
// inserts ✓/✗ indicator lines that the scanner drops. The Events
// emitted are identical to the non-verbose form modulo the test_id
// (which both forms carry).
func TestJest_ParseVerboseSameAsTerse(t *testing.T) {
	terse := `FAIL src/auth.test.js
  ● Auth › login fails

    Error: nope

      at src/auth.test.js:5:5

Test Suites: 1 failed, 1 total
`
	verbose := `FAIL src/auth.test.js
  Auth
    ✗ login fails (2 ms)
    ✓ logout succeeds (1 ms)
  ● Auth › login fails

    Error: nope

      at src/auth.test.js:5:5

Test Suites: 1 failed, 1 total
`
	a := parseString(t, terse)
	b := parseString(t, verbose)
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("got %d / %d events; want 1 each", len(a), len(b))
	}
	if a[0].Title != b[0].Title {
		t.Errorf("Title differs:\nterse:   %q\nverbose: %q", a[0].Title, b[0].Title)
	}
	if a[0].Kind != b[0].Kind {
		t.Errorf("Kind differs: %q vs %q", a[0].Kind, b[0].Kind)
	}
	if a[0].Metadata["test_id"] != b[0].Metadata["test_id"] {
		t.Errorf("test_id differs: %q vs %q",
			a[0].Metadata["test_id"], b[0].Metadata["test_id"])
	}
	// Body differs in length (verbose has extra ✓/✗ lines before
	// the bullet) so we don't deep-equal — but the body must
	// contain the same anchored content lines.
	for _, want := range []string{"Error: nope", "at src/auth.test.js:5:5"} {
		foundA, foundB := false, false
		for _, line := range a[0].Body {
			if strings.Contains(line, want) {
				foundA = true
			}
		}
		for _, line := range b[0].Body {
			if strings.Contains(line, want) {
				foundB = true
			}
		}
		if !foundA || !foundB {
			t.Errorf("expected %q in both Bodies; terse=%v verbose=%v",
				want, foundA, foundB)
		}
	}
}

// TestJest_ParseCIReporterModeNoANSI — the CI reporter mode emits
// the same content without ANSI escapes. The scanner's state
// machine keys off content markers (●, FAIL, Snapshot:) not
// column positions, so plain output produces identical Events.
func TestJest_ParseCIReporterModeNoANSI(t *testing.T) {
	colored := "FAIL src/auth.test.js\n" +
		"  \x1b[31m●\x1b[0m Auth \u203a login fails\n" +
		"\n" +
		"    \x1b[31mError: nope\x1b[0m\n" +
		"\n" +
		"      at src/auth.test.js:5:5\n" +
		"\n" +
		"Test Suites: 1 failed, 1 total\n"
	plain := "FAIL src/auth.test.js\n" +
		"  \u25cf Auth \u203a login fails\n" +
		"\n" +
		"    Error: nope\n" +
		"\n" +
		"      at src/auth.test.js:5:5\n" +
		"\n" +
		"Test Suites: 1 failed, 1 total\n"
	a := parseString(t, colored)
	b := parseString(t, plain)
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("got %d / %d events; want 1 each", len(a), len(b))
	}
	if a[0].Title != b[0].Title {
		t.Errorf("Title differs: coloured=%q plain=%q", a[0].Title, b[0].Title)
	}
	if a[0].Metadata["test_id"] != b[0].Metadata["test_id"] {
		t.Errorf("test_id differs: %q vs %q",
			a[0].Metadata["test_id"], b[0].Metadata["test_id"])
	}
}
