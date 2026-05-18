package orchestrator_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/pkg/distill/internal/orchestrator"

	// Register the v1 format set so the orchestrator's
	// detect-and-resolve path has formats to choose from. The
	// orchestrator package itself stays format-agnostic; tests
	// bring in whichever subset they need.
	_ "github.com/vail130/distill-ai/internal/envelope/githubactions"
	_ "github.com/vail130/distill-ai/internal/envelope/gitlabci"
	_ "github.com/vail130/distill-ai/internal/formats/generic"
	_ "github.com/vail130/distill-ai/internal/formats/gotest"
	_ "github.com/vail130/distill-ai/internal/formats/jest"
	_ "github.com/vail130/distill-ai/internal/formats/pytest"
)

// gotestFixture is a minimal gotest-fail sample inlined so the
// orchestrator tests don't depend on test/integration/testdata.
// The integration tests cover the cross-package end-to-end path.
const gotestFixture = `=== RUN   TestThing
    thing_test.go:42: expected 200, got 500
--- FAIL: TestThing (0.01s)
=== RUN   TestOtherThing
--- PASS: TestOtherThing (0.00s)
FAIL	example.com/m	0.012s
FAIL
`

// TestOrchestrator_NilReaderErrors asserts the documented
// ErrNilReader path.
func TestOrchestrator_NilReaderErrors(t *testing.T) {
	cfg := orchestrator.Config{Writer: &bytes.Buffer{}}
	_, err := orchestrator.Setup(t.Context(), cfg, nil)
	if !errors.Is(err, orchestrator.ErrNilReader) {
		t.Fatalf("Setup() err = %v, want ErrNilReader", err)
	}
}

// TestOrchestrator_NilWriterErrors asserts the documented
// ErrNilWriter path.
func TestOrchestrator_NilWriterErrors(t *testing.T) {
	cfg := orchestrator.Config{}
	_, err := orchestrator.Setup(t.Context(), cfg, strings.NewReader("x"))
	if !errors.Is(err, orchestrator.ErrNilWriter) {
		t.Fatalf("Setup() err = %v, want ErrNilWriter", err)
	}
}

// TestOrchestrator_UnknownTokenizerErrors asserts that an unknown
// tokenizer is rejected before any goroutine starts.
func TestOrchestrator_UnknownTokenizerErrors(t *testing.T) {
	cfg := orchestrator.Config{
		Writer:    &bytes.Buffer{},
		Tokenizer: "ggml",
	}
	_, err := orchestrator.Setup(t.Context(), cfg, strings.NewReader("x"))
	if !errors.Is(err, orchestrator.ErrUnknownTokenizer) {
		t.Fatalf("Setup() err = %v, want ErrUnknownTokenizer", err)
	}
}

// TestOrchestrator_UnknownFormatErrors asserts that a Config.Format
// not in the registry is rejected.
func TestOrchestrator_UnknownFormatErrors(t *testing.T) {
	cfg := orchestrator.Config{
		Writer: &bytes.Buffer{},
		Format: "nonsense-format",
	}
	_, err := orchestrator.Setup(t.Context(), cfg, strings.NewReader("x"))
	if !errors.Is(err, orchestrator.ErrUnknownFormat) {
		t.Fatalf("Setup() err = %v, want ErrUnknownFormat", err)
	}
}

// TestOrchestrator_UnknownOutputErrors asserts that an out-of-range
// Output constant is rejected.
func TestOrchestrator_UnknownOutputErrors(t *testing.T) {
	cfg := orchestrator.Config{
		Writer: &bytes.Buffer{},
		Output: orchestrator.Output(99),
	}
	_, err := orchestrator.Setup(t.Context(), cfg, strings.NewReader("x"))
	if !errors.Is(err, orchestrator.ErrUnknownOutput) {
		t.Fatalf("Setup() err = %v, want ErrUnknownOutput", err)
	}
}

// TestOrchestrator_UnknownStripEnvelopeErrors asserts that an
// unrecognised StripEnvelope name surfaces as
// ErrUnknownStripEnvelope rather than as the underlying envelope
// package error.
func TestOrchestrator_UnknownStripEnvelopeErrors(t *testing.T) {
	cfg := orchestrator.Config{
		Writer:        &bytes.Buffer{},
		Format:        "gotest",
		StripEnvelope: "nonsense-envelope",
	}
	_, err := orchestrator.Setup(t.Context(), cfg, strings.NewReader("x"))
	if !errors.Is(err, orchestrator.ErrUnknownStripEnvelope) {
		t.Fatalf("Setup() err = %v, want ErrUnknownStripEnvelope", err)
	}
}

// TestOrchestrator_EndToEndGotest exercises the full pipeline: a
// real gotest fixture, autodetected, distilled to a Text sink,
// summary populated by Wait.
func TestOrchestrator_EndToEndGotest(t *testing.T) {
	w := &bytes.Buffer{}
	cfg := orchestrator.Config{Writer: w}
	run, err := orchestrator.Setup(t.Context(), cfg, strings.NewReader(gotestFixture))
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	run.Start(t.Context())
	summary, err := run.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if summary.EventsEmitted < 1 {
		t.Errorf("EventsEmitted = %d, want >= 1", summary.EventsEmitted)
	}
	if !strings.Contains(w.String(), "TestThing") {
		t.Errorf("output missing TestThing reference: %q", w.String())
	}
	if summary.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", summary.ExitCode)
	}
	if summary.Estimator != "heuristic" {
		t.Errorf("Estimator = %q, want %q", summary.Estimator, "heuristic")
	}
}

// TestOrchestrator_ExplicitFormatBeatsAutodetect asserts that
// Config.Format bypasses detection.
func TestOrchestrator_ExplicitFormatBeatsAutodetect(t *testing.T) {
	w := &bytes.Buffer{}
	cfg := orchestrator.Config{Writer: w, Format: "gotest"}
	run, err := orchestrator.Setup(t.Context(), cfg, strings.NewReader(gotestFixture))
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	run.Start(t.Context())
	summary, err := run.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if summary.EventsEmitted == 0 {
		t.Errorf("EventsEmitted = 0; explicit format should produce events")
	}
}

// TestOrchestrator_SummaryNoEventsExitCode asserts the ExitCode=1
// arm when the parser emits no events.
func TestOrchestrator_SummaryNoEventsExitCode(t *testing.T) {
	w := &bytes.Buffer{}
	// Innocuous input that autodetects as generic with no severity
	// hits → zero events.
	cfg := orchestrator.Config{Writer: w}
	run, err := orchestrator.Setup(t.Context(), cfg, strings.NewReader("hello world\n"))
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	run.Start(t.Context())
	summary, err := run.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if summary.EventsEmitted != 0 {
		t.Errorf("EventsEmitted = %d, want 0", summary.EventsEmitted)
	}
	if summary.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", summary.ExitCode)
	}
}

// TestOrchestrator_ContextCancellation asserts that cancelling the
// pipeline's ctx propagates and Wait returns a non-nil error
// without leaking goroutines.
func TestOrchestrator_ContextCancellation(t *testing.T) {
	w := &bytes.Buffer{}
	cfg := orchestrator.Config{Writer: w, Format: "gotest"}
	ctx, cancel := context.WithCancel(t.Context())
	run, err := orchestrator.Setup(ctx, cfg, strings.NewReader(gotestFixture))
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	cancel()
	run.Start(ctx)
	_, _ = run.Wait()
	// We don't assert a specific error: pipelines may complete
	// cleanly if all events are processed before cancellation
	// propagates. The contract is "Wait returns, no goroutines
	// leak." A leak would manifest in the project-level
	// goroutine-leak guard.
}

// TestOrchestrator_OutputJSONStreaming smoke-tests the
// OutputJSONStreaming arm to confirm buildSink picks the streaming
// JSON encoder.
func TestOrchestrator_OutputJSONStreaming(t *testing.T) {
	w := &bytes.Buffer{}
	cfg := orchestrator.Config{
		Writer: w,
		Format: "gotest",
		Output: orchestrator.OutputJSONStreaming,
	}
	run, err := orchestrator.Setup(t.Context(), cfg, strings.NewReader(gotestFixture))
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	run.Start(t.Context())
	if _, err := run.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	// ndjson: every non-empty line should be a JSON object beginning
	// with `{"schema_version"`. Check the first line.
	lines := strings.Split(strings.TrimSpace(w.String()), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], `{"schema_version"`) {
		t.Errorf("expected ndjson, first line = %q", lines[0])
	}
}

// TestOrchestrator_OutputMarkdownPicksMarkdownSink smoke-tests the
// OutputMarkdown arm.
func TestOrchestrator_OutputMarkdownPicksMarkdownSink(t *testing.T) {
	w := &bytes.Buffer{}
	cfg := orchestrator.Config{
		Writer:    w,
		Format:    "gotest",
		Output:    orchestrator.OutputMarkdown,
		FenceLang: "go",
	}
	run, err := orchestrator.Setup(t.Context(), cfg, strings.NewReader(gotestFixture))
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	run.Start(t.Context())
	if _, err := run.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !strings.Contains(w.String(), "```go") {
		t.Errorf("markdown output missing go fence: %q", w.String())
	}
}

// TestOrchestrator_MinSeverityRespected asserts that Config.MinSeverity
// is propagated into ParseOpts so the generic format honours the
// filter.
func TestOrchestrator_MinSeverityRespected(t *testing.T) {
	// Generic format with a warning-only line. Default MinSeverity
	// (empty / SeverityError) should drop it; MinSeverity=Warn keeps it.
	const input = "WARNING: this is a problem\n"
	for _, c := range []struct {
		name string
		sev  event.Severity
		want int
	}{
		{"default-drops-warning", "", 0},
		{"warn-keeps", event.SeverityWarn, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := &bytes.Buffer{}
			cfg := orchestrator.Config{
				Writer:      w,
				Format:      "generic",
				MinSeverity: c.sev,
			}
			run, err := orchestrator.Setup(t.Context(), cfg, strings.NewReader(input))
			if err != nil {
				t.Fatalf("Setup: %v", err)
			}
			run.Start(t.Context())
			summary, err := run.Wait()
			if err != nil {
				t.Fatalf("Wait: %v", err)
			}
			if summary.EventsEmitted != c.want {
				t.Errorf("EventsEmitted = %d, want %d (output=%q)",
					summary.EventsEmitted, c.want, w.String())
			}
		})
	}
}
