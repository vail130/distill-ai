package gotestsum_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
	_ "github.com/vail130/distill-ai/internal/formats/gotestsum"
	"github.com/vail130/distill-ai/internal/testutil"
)

func parseAll(t *testing.T, input string) []event.Event {
	t.Helper()
	f, ok := formats.Get("gotestsum")
	if !ok {
		t.Fatal("gotestsum not registered")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, err := f.Parse(ctx, strings.NewReader(input), formats.ParseOpts{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var out []event.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

func TestGotestsum_DetectMarkers(t *testing.T) {
	f, _ := formats.Get("gotestsum")
	for _, sample := range []string{
		"=== Failed\n",
		"=== FAIL: example.com/app TestLogin (0.01s)\n",
		"DONE 3 tests, 1 failure in 0.12s\n",
		"FAIL example.com/app.TestLogin (0.01s)\n",
	} {
		if got := f.Detect([]byte(sample)); got != event.Confidence(1.0) {
			t.Errorf("Detect(%q) = %v, want 1.0", sample, got)
		}
	}
}

func TestGotestsum_DetectNegative(t *testing.T) {
	f, _ := formats.Get("gotestsum")
	for _, sample := range []string{"--- FAIL: TestLogin (0.01s)\n", "Traceback (most recent call last):\n", "plain prose\n"} {
		if got := f.Detect([]byte(sample)); got != 0 {
			t.Errorf("Detect(%q) = %v, want 0", sample, got)
		}
	}
}

func TestGotestsum_ParseSingleFailure(t *testing.T) {
	input := "FAIL example.com/app.TestLogin (0.01s)\n\n=== Failed\n=== FAIL: example.com/app TestLogin (0.01s)\n    auth_test.go:42: expected 200, got 500\n\nDONE 1 tests, 1 failure in 0.12s\n"
	got := parseAll(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1: %+v", len(got), got)
	}
	ev := got[0]
	if ev.Kind != "test_failure" {
		t.Errorf("kind = %q, want test_failure", ev.Kind)
	}
	if ev.Title != "expected 200, got 500" {
		t.Errorf("title = %q, want assertion message", ev.Title)
	}
	if ev.Metadata["package"] != "example.com/app" || ev.Metadata["test_id"] != "TestLogin" {
		t.Errorf("metadata = %+v, want package and test_id", ev.Metadata)
	}
	if ev.Location == nil || ev.Location.File != "auth_test.go" || ev.Location.Line != 42 {
		t.Errorf("location = %+v, want auth_test.go:42", ev.Location)
	}
}

func TestGotestsum_ParsePackageFlagError(t *testing.T) {
	got := parseAll(t, "=== Failed\n=== FAIL: example.com/app\nflag provided but not defined: -test.db.migrations\nDONE 1 tests, 1 failure in 0.01s\n")
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1: %+v", len(got), got)
	}
	if got[0].Kind != "build_failure" {
		t.Errorf("kind = %q, want build_failure", got[0].Kind)
	}
	if got[0].Metadata["package"] != "example.com/app" {
		t.Errorf("package = %q, want example.com/app", got[0].Metadata["package"])
	}
}

func TestGotestsum_ParseSummaryOnly(t *testing.T) {
	got := parseAll(t, "DONE 2 tests, 1 failure in 0.01s\n")
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1: %+v", len(got), got)
	}
	if got[0].Metadata["summary_only"] != "true" {
		t.Errorf("metadata.summary_only = %q, want true", got[0].Metadata["summary_only"])
	}
}

func TestGotestsum_ParseDeterministic(t *testing.T) {
	input := "=== Failed\n=== FAIL: example.com/app TestLogin (0.01s)\n    auth_test.go:42: expected 200, got 500\nDONE 1 tests, 1 failure in 0.12s\n"
	first := parseAll(t, input)
	second := parseAll(t, input)
	if !reflect.DeepEqual(first, second) {
		t.Errorf("parse not deterministic\nfirst=%+v\nsecond=%+v", first, second)
	}
}

func TestGotestsum_ParseStreaming(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("=== Failed\n")
	sb.WriteString("=== FAIL: example.com/app TestEarly (0.01s)\n")
	sb.WriteString("    early_test.go:1: failed early\n")
	sb.WriteString("=== FAIL: example.com/app TestLate (0.01s)\n")
	for i := 0; i < 200; i++ {
		sb.WriteString("    filler line\n")
	}
	sb.WriteString("DONE 2 tests, 2 failures in 0.20s\n")
	input := sb.String()
	slow := &testutil.SlowReader{
		Inner:      strings.NewReader(input),
		ChunkSize:  64,
		ChunkDelay: 2 * time.Millisecond,
	}
	f, _ := formats.Get("gotestsum")
	start := time.Now()
	ch, _ := f.Parse(context.Background(), slow, formats.ParseOpts{})
	first, ok := <-ch
	if !ok {
		t.Fatal("expected at least one event before EOF")
	}
	firstAt := time.Since(start)
	totalExpected := time.Duration(len(input)/slow.ChunkSize) * slow.ChunkDelay
	if firstAt > totalExpected/2 {
		t.Errorf("first event emerged after %s; expected before %s", firstAt, totalExpected/2)
	}
	if first.Metadata["test_id"] != "TestEarly" {
		t.Errorf("first event test_id = %q, want TestEarly", first.Metadata["test_id"])
	}
	drained := 0
	for range ch {
		drained++
	}
	_ = drained
}
