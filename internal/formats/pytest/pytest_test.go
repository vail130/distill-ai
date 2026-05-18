package pytest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
	_ "github.com/vail130/distill-ai/internal/formats/pytest"
)

// TestPytest_DetectClearMarker — the canonical session header
// anchors detection at the highest confidence.
func TestPytest_DetectClearMarker(t *testing.T) {
	f, ok := formats.Get("pytest")
	if !ok {
		t.Fatal("pytest not registered")
	}
	sample := []byte("============================= test session starts ==============================\n")
	if got := f.Detect(sample); got != event.Confidence(1.0) {
		t.Errorf("Detect(session starts) = %v, want 1.0", got)
	}
}

// TestPytest_DetectFailuresBanner — the `=== FAILURES ===` banner
// also anchors at 1.0. Catches truncated samples that start in the
// middle of a run.
func TestPytest_DetectFailuresBanner(t *testing.T) {
	f, _ := formats.Get("pytest")
	sample := []byte("=================================== FAILURES ===================================\n")
	if got := f.Detect(sample); got != event.Confidence(1.0) {
		t.Errorf("Detect(FAILURES banner) = %v, want 1.0", got)
	}
}

// TestPytest_DetectFuzzy — a `>` assertion line plus a conftest.py
// reference scores at the fuzzy confidence.
func TestPytest_DetectFuzzy(t *testing.T) {
	f, _ := formats.Get("pytest")
	sample := []byte(`>       assert response.status_code == 302
E       AssertionError
fixture defined in conftest.py:13
`)
	if got := f.Detect(sample); got != event.Confidence(0.8) {
		t.Errorf("Detect(> assert + conftest.py) = %v, want 0.8", got)
	}
}

// TestPytest_DetectFuzzyAssertionAloneNotEnough — a `>` line alone,
// without a config-file mention, doesn't raise the score. The
// assertion marker is too generic (every quoted diff uses `>` for
// added lines) to claim a sample without corroboration.
func TestPytest_DetectFuzzyAssertionAloneNotEnough(t *testing.T) {
	f, _ := formats.Get("pytest")
	sample := []byte("> diff line 1\n> diff line 2\n")
	if got := f.Detect(sample); got != 0 {
		t.Errorf("Detect(> alone) = %v, want 0", got)
	}
}

// TestPytest_DetectFuzzyConfigAloneNotEnough — mentioning
// conftest.py without an assertion marker doesn't raise the score
// either. Both signals must be present together for fuzzy match.
func TestPytest_DetectFuzzyConfigAloneNotEnough(t *testing.T) {
	f, _ := formats.Get("pytest")
	sample := []byte("we ship a conftest.py for shared fixtures\n")
	if got := f.Detect(sample); got != 0 {
		t.Errorf("Detect(conftest only) = %v, want 0", got)
	}
}

// TestPytest_DetectNegative — unrelated formats score 0.
func TestPytest_DetectNegative(t *testing.T) {
	f, _ := formats.Get("pytest")
	for _, sample := range []string{
		// Go test output
		"--- FAIL: TestLogin (0.02s)\n",
		// jest failure block
		"  ● Auth › login redirects\n",
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

// TestPytest_RegisteredAtInit — the side-effect import registers
// the format under the name "pytest".
func TestPytest_RegisteredAtInit(t *testing.T) {
	f, ok := formats.Get("pytest")
	if !ok {
		t.Fatal("formats.Get(\"pytest\") returned !ok")
	}
	if f.Name() != "pytest" {
		t.Errorf("Name() = %q, want \"pytest\"", f.Name())
	}
}

// TestPytest_ParseNonFailureEmitsNothing — input that contains no
// `=== FAILURES ===` banner produces zero Events even when the
// detector would have raised confidence on a session-start
// banner. The scanner drops everything in stateSession until the
// FAILURES section opens.
func TestPytest_ParseNonFailureEmitsNothing(t *testing.T) {
	f, _ := formats.Get("pytest")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := strings.NewReader("============================= test session starts ==============================\n" +
		"collected 0 items\n" +
		"========================== no tests ran in 0.01s ==========================\n")
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
