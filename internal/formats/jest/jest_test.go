package jest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
	_ "github.com/vail130/distill-ai/internal/formats/jest"
)

// TestJest_DetectBulletMarker — the `●` bullet jest's default
// reporter prints at the head of every failure block anchors
// detection at the highest confidence.
func TestJest_DetectBulletMarker(t *testing.T) {
	f, ok := formats.Get("jest")
	if !ok {
		t.Fatal("jest not registered")
	}
	sample := []byte("  ● Auth › login redirects to dashboard\n")
	if got := f.Detect(sample); got != event.Confidence(1.0) {
		t.Errorf("Detect(● bullet) = %v, want 1.0", got)
	}
}

// TestJest_DetectFailWithPath — the per-file FAIL header followed
// by a path-shaped token anchors at the highest confidence.
func TestJest_DetectFailWithPath(t *testing.T) {
	f, _ := formats.Get("jest")
	sample := []byte("FAIL src/auth.test.js\n")
	if got := f.Detect(sample); got != event.Confidence(1.0) {
		t.Errorf("Detect(FAIL src/auth.test.js) = %v, want 1.0", got)
	}
}

// TestJest_DetectPassWithPath — PASS headers anchor at 1.0 too,
// because they unambiguously identify jest output even before any
// failure has been rendered (a clean run still passes detection).
func TestJest_DetectPassWithPath(t *testing.T) {
	f, _ := formats.Get("jest")
	sample := []byte("PASS src/utils.spec.ts\n")
	if got := f.Detect(sample); got != event.Confidence(1.0) {
		t.Errorf("Detect(PASS src/utils.spec.ts) = %v, want 1.0", got)
	}
}

// TestJest_DetectFailRequiresPathToken — a bare `FAIL` line without
// a path-shaped token must not raise the score. Prevents unrelated
// tools (mocha's terse `FAIL`, generic CI prose, `FAIL: rebooting
// <host>`) from claiming jest output.
func TestJest_DetectFailRequiresPathToken(t *testing.T) {
	f, _ := formats.Get("jest")
	for _, sample := range []string{
		"FAIL: rebooting node\n",
		"FAIL\n",
		"FAIL rebooting\n",       // single token, no path separator
		"FAIL some-thing-else\n", // no separator, no test-file suffix
		"FAIL internal/qsrestgrpc\n",
		"some FAIL state\n", // not at start of line
	} {
		got := f.Detect([]byte(sample))
		if got == event.Confidence(1.0) {
			t.Errorf("Detect(%q) = %v; want < 1.0 (no path token)", sample, got)
		}
	}
}

// TestJest_DetectFuzzy — a `Tests:` summary line corroborated by a
// jest mention or a .test./.spec. filename scores at the fuzzy
// confidence. Catches truncated tails of a long run.
func TestJest_DetectFuzzy(t *testing.T) {
	f, _ := formats.Get("jest")
	sample := []byte(`Tests:       1 failed, 2 passed, 3 total
Snapshots:   0 total
Time:        1.234 s
Ran all test suites matching auth.test.js
`)
	if got := f.Detect(sample); got != event.Confidence(0.8) {
		t.Errorf("Detect(Tests: summary + .test.js) = %v, want 0.8", got)
	}
}

// TestJest_DetectFuzzyWithJestWord — the corroborator can be a
// literal `jest` mention rather than a test-file suffix.
func TestJest_DetectFuzzyWithJestWord(t *testing.T) {
	f, _ := formats.Get("jest")
	sample := []byte(`Tests:       3 failed, 5 passed
Ran via jest --watch
`)
	if got := f.Detect(sample); got != event.Confidence(0.8) {
		t.Errorf("Detect(Tests: + jest word) = %v, want 0.8", got)
	}
}

// TestJest_DetectFuzzySummaryAloneNotEnough — a `Tests:` summary
// without a corroborating signal doesn't raise the score. Prevents
// unrelated tools that happen to print "Tests: 5 passed" from
// claiming jest output.
func TestJest_DetectFuzzySummaryAloneNotEnough(t *testing.T) {
	f, _ := formats.Get("jest")
	sample := []byte("Tests:       1 failed, 2 passed\nSome other tool\n")
	if got := f.Detect(sample); got != 0 {
		t.Errorf("Detect(Tests: alone) = %v, want 0", got)
	}
}

// TestJest_DetectFuzzyJestWordAloneNotEnough — the corroborator
// alone, without a Tests: summary, doesn't claim the format either.
// Mentioning "jest" in prose is too common.
func TestJest_DetectFuzzyJestWordAloneNotEnough(t *testing.T) {
	f, _ := formats.Get("jest")
	sample := []byte("we use jest to test our frontend code\n")
	if got := f.Detect(sample); got != 0 {
		t.Errorf("Detect(jest word alone) = %v, want 0", got)
	}
}

// TestJest_DetectNegative — unrelated formats (gotest, pytest,
// generic ERROR, JSON logs) score 0.
func TestJest_DetectNegative(t *testing.T) {
	f, _ := formats.Get("jest")
	for _, sample := range []string{
		// gotest
		"--- FAIL: TestLogin (0.02s)\n",
		// pytest
		"============================= test session starts ==============================\n",
		// Python traceback
		"Traceback (most recent call last):\n  File \"foo.py\"\n",
		// generic ERROR
		"ERROR: thing broke\n",
		// JSON logs
		`{"level":"error","msg":"boom"}` + "\n",
		"Hello, world.\n",
		"",
	} {
		if got := f.Detect([]byte(sample)); got != 0 {
			t.Errorf("Detect(%q) = %v, want 0", sample, got)
		}
	}
}

// TestJest_RegisteredAtInit — the side-effect import registers the
// format under the name "jest".
func TestJest_RegisteredAtInit(t *testing.T) {
	f, ok := formats.Get("jest")
	if !ok {
		t.Fatal("formats.Get(\"jest\") returned !ok")
	}
	if f.Name() != "jest" {
		t.Errorf("Name() = %q, want \"jest\"", f.Name())
	}
}

// TestJest_ParseNoFailuresEmitsNothing — input that contains no
// `●` headers and no FAIL/PASS markers produces zero Events. The
// scanner only anchors on those markers; everything else is
// dropped.
func TestJest_ParseNoFailuresEmitsNothing(t *testing.T) {
	f, _ := formats.Get("jest")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := strings.NewReader("PASS src/utils.spec.ts\n" +
		"Test Suites: 1 passed, 1 total\n" +
		"Tests:       3 passed, 3 total\n")
	ch, err := f.Parse(ctx, input, formats.ParseOpts{})
	if err != nil {
		t.Fatalf("Parse err = %v, want nil", err)
	}
	count := 0
	for range ch {
		count++
	}
	if count != 0 {
		t.Errorf("Parse emitted %d events; want 0", count)
	}
}
