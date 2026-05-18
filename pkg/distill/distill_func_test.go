package distill_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vail130/distill-ai/pkg/distill"
)

// gotestFixture is a small gotest-fail sample used across the
// Distill tests. Kept inline so test failures don't depend on the
// integration testdata directory layout.
const gotestFixture = `=== RUN   TestThing
    thing_test.go:42: expected 200, got 500
--- FAIL: TestThing (0.01s)
=== RUN   TestOtherThing
--- PASS: TestOtherThing (0.00s)
FAIL	example.com/m	0.012s
FAIL
`

// drainEvents consumes every Event on ch in the background. Tests
// that care only about Distill's encoded output use this so the
// internal teeing sink doesn't backpressure the pipeline.
func drainEvents(ch <-chan distill.Event) {
	go func() {
		//nolint:revive // empty loop is intentional: discard events
		for range ch {
		}
	}()
}

// TestDistill_PipeToText is the canonical happy path: a real
// gotest fixture, default options, text encoder, sane Summary.
func TestDistill_PipeToText(t *testing.T) {
	w := &bytes.Buffer{}
	events, summary, err := distill.Distill(t.Context(),
		strings.NewReader(gotestFixture),
		distill.Options{Writer: w},
	)
	if err != nil {
		t.Fatalf("Distill: %v", err)
	}
	drainEvents(events)
	waitForChannelClose(t, events)
	summary.Wait()
	if summary.EventsEmitted == 0 {
		t.Errorf("Summary.EventsEmitted = 0; want >= 1")
	}
	if !strings.Contains(w.String(), "TestThing") {
		t.Errorf("output missing TestThing: %q", w.String())
	}
	if summary.ExitCode != 0 {
		t.Errorf("Summary.ExitCode = %d, want 0", summary.ExitCode)
	}
}

// TestDistill_PipeToJSON exercises the OutputJSON arm; the output
// should be a single batch JSON object parseable by encoding/json.
func TestDistill_PipeToJSON(t *testing.T) {
	w := &bytes.Buffer{}
	events, _, err := distill.Distill(t.Context(),
		strings.NewReader(gotestFixture),
		distill.Options{Writer: w, Output: distill.OutputJSON},
	)
	if err != nil {
		t.Fatalf("Distill: %v", err)
	}
	drainEvents(events)
	waitForChannelClose(t, events)
	var got map[string]any
	if err := json.Unmarshal(w.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %q", err, w.String())
	}
	if _, ok := got["schema_version"]; !ok {
		t.Errorf("output missing schema_version")
	}
	if _, ok := got["events"]; !ok {
		t.Errorf("output missing events array")
	}
}

// TestDistill_PipeToJSONStreaming asserts ndjson output: every line
// is its own JSON object beginning with `{"schema_version"`.
func TestDistill_PipeToJSONStreaming(t *testing.T) {
	w := &bytes.Buffer{}
	events, _, err := distill.Distill(t.Context(),
		strings.NewReader(gotestFixture),
		distill.Options{Writer: w, Output: distill.OutputJSONStreaming},
	)
	if err != nil {
		t.Fatalf("Distill: %v", err)
	}
	drainEvents(events)
	waitForChannelClose(t, events)
	lines := strings.Split(strings.TrimSpace(w.String()), "\n")
	if len(lines) < 1 {
		t.Fatalf("ndjson empty: %q", w.String())
	}
	for i, line := range lines {
		var got map[string]any
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Errorf("line %d not valid JSON: %v\n%q", i, err, line)
		}
	}
}

// TestDistill_ExplicitFormatBeatsAutodetect asserts that
// opts.Format bypasses detection.
func TestDistill_ExplicitFormatBeatsAutodetect(t *testing.T) {
	w := &bytes.Buffer{}
	events, summary, err := distill.Distill(t.Context(),
		strings.NewReader(gotestFixture),
		distill.Options{Writer: w, Format: "gotest"},
	)
	if err != nil {
		t.Fatalf("Distill: %v", err)
	}
	drainEvents(events)
	waitForChannelClose(t, events)
	summary.Wait()
	if summary.EventsEmitted == 0 {
		t.Errorf("explicit gotest format produced 0 events")
	}
}

// TestDistill_AutodetectFallsBackToGeneric feeds a low-confidence
// input and asserts it autodetects as generic and runs cleanly.
func TestDistill_AutodetectFallsBackToGeneric(t *testing.T) {
	w := &bytes.Buffer{}
	events, summary, err := distill.Distill(t.Context(),
		strings.NewReader("hello world\n"),
		distill.Options{Writer: w},
	)
	if err != nil {
		t.Fatalf("Distill: %v", err)
	}
	drainEvents(events)
	waitForChannelClose(t, events)
	summary.Wait()
	if summary.EventsEmitted != 0 {
		t.Errorf("generic on clean input emitted %d events; want 0",
			summary.EventsEmitted)
	}
	if summary.ExitCode != 1 {
		t.Errorf("zero-event Summary.ExitCode = %d, want 1", summary.ExitCode)
	}
}

// TestDistill_StrictReturnsErrorOnLowConfidence asserts the
// Strict mode setup-error path.
func TestDistill_StrictReturnsErrorOnLowConfidence(t *testing.T) {
	// Strict mode wants a low-confidence input that no specific
	// format claims. Plain prose with no severity hits qualifies.
	_, _, err := distill.Distill(t.Context(),
		strings.NewReader("hello world\n"),
		distill.Options{Writer: &bytes.Buffer{}, Strict: true},
	)
	if err == nil {
		t.Fatalf("Distill: nil err under --strict + low-confidence input")
	}
}

// TestDistill_NilWriterErrors asserts the ErrNilWriter contract.
func TestDistill_NilWriterErrors(t *testing.T) {
	_, _, err := distill.Distill(t.Context(),
		strings.NewReader("x"),
		distill.Options{},
	)
	if !errors.Is(err, distill.ErrNilWriter) {
		t.Fatalf("Distill err = %v, want ErrNilWriter", err)
	}
}

// TestDistill_UnknownTokenizerErrors asserts the
// ErrUnknownTokenizer contract.
func TestDistill_UnknownTokenizerErrors(t *testing.T) {
	_, _, err := distill.Distill(t.Context(),
		strings.NewReader("x"),
		distill.Options{Writer: &bytes.Buffer{}, Tokenizer: "ggml"},
	)
	if !errors.Is(err, distill.ErrUnknownTokenizer) {
		t.Fatalf("Distill err = %v, want ErrUnknownTokenizer", err)
	}
}

// TestDistill_UnknownFormatErrors asserts the ErrUnknownFormat
// contract.
func TestDistill_UnknownFormatErrors(t *testing.T) {
	_, _, err := distill.Distill(t.Context(),
		strings.NewReader("x"),
		distill.Options{Writer: &bytes.Buffer{}, Format: "ggml"},
	)
	if !errors.Is(err, distill.ErrUnknownFormat) {
		t.Fatalf("Distill err = %v, want ErrUnknownFormat", err)
	}
}

// TestDistill_UnknownOutputErrors asserts the ErrUnknownOutput
// contract for the public Options.Output field.
func TestDistill_UnknownOutputErrors(t *testing.T) {
	_, _, err := distill.Distill(t.Context(),
		strings.NewReader("x"),
		distill.Options{Writer: &bytes.Buffer{}, Output: distill.OutputFormat("custom")},
	)
	if !errors.Is(err, distill.ErrUnknownOutput) {
		t.Fatalf("Distill err = %v, want ErrUnknownOutput", err)
	}
}

// TestDistill_SummaryReflectsCounters asserts the Summary's
// counters reflect what the encoder reports. Compares against the
// JSON encoder's trailer for the same input.
func TestDistill_SummaryReflectsCounters(t *testing.T) {
	w := &bytes.Buffer{}
	events, summary, err := distill.Distill(t.Context(),
		strings.NewReader(gotestFixture),
		distill.Options{Writer: w, Output: distill.OutputJSON, Format: "gotest"},
	)
	if err != nil {
		t.Fatalf("Distill: %v", err)
	}
	drainEvents(events)
	waitForChannelClose(t, events)
	summary.Wait()
	var batch struct {
		Summary struct {
			EventsEmitted int `json:"events_emitted"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(w.Bytes(), &batch); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if summary.EventsEmitted != batch.Summary.EventsEmitted {
		t.Errorf("Summary.EventsEmitted = %d; JSON trailer = %d",
			summary.EventsEmitted, batch.Summary.EventsEmitted)
	}
}

// TestDistill_StreamingBeforeEOF asserts the streaming contract:
// the Event channel emits at least one Event before the pipeline
// finishes. Uses a slowReader that drips input so a non-streaming
// implementation would buffer the whole thing first.
func TestDistill_StreamingBeforeEOF(t *testing.T) {
	r := &slowReader{
		chunks: [][]byte{
			[]byte("=== RUN   TestA\n"),
			[]byte("    a_test.go:1: bang\n"),
			[]byte("--- FAIL: TestA (0.00s)\n"),
			[]byte("=== RUN   TestB\n"),
			[]byte("    b_test.go:2: boom\n"),
			[]byte("--- FAIL: TestB (0.00s)\n"),
			[]byte("FAIL\texample.com/m\t0.01s\nFAIL\n"),
		},
		delay: 20 * time.Millisecond,
	}
	w := &bytes.Buffer{}
	events, _, err := distill.Distill(t.Context(), r,
		distill.Options{Writer: w, Format: "gotest"},
	)
	if err != nil {
		t.Fatalf("Distill: %v", err)
	}
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatalf("channel closed before any Event arrived")
		}
		_ = ev
	case <-timeout.C:
		t.Fatalf("no Event arrived before timeout (streaming broken)")
	}
	drainEvents(events)
	waitForChannelClose(t, events)
}

// TestDistill_ContextCancellationClosesChannel asserts that
// cancelling the ctx propagates: the channel closes within a
// bounded interval and the Summary is non-nil.
func TestDistill_ContextCancellationClosesChannel(t *testing.T) {
	// A deliberately huge input that the pipeline can't drain
	// quickly. We cancel mid-stream and expect the channel to
	// close cleanly.
	r := strings.NewReader(strings.Repeat(gotestFixture, 500))
	w := &bytes.Buffer{}
	ctx, cancel := context.WithCancel(t.Context())
	events, summary, err := distill.Distill(ctx, r,
		distill.Options{Writer: w, Format: "gotest"},
	)
	if err != nil {
		t.Fatalf("Distill: %v", err)
	}
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	drainEvents(events)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-events:
			if !ok {
				if summary == nil {
					t.Fatalf("Summary is nil after channel close")
				}
				return
			}
		case <-deadline:
			t.Fatalf("channel did not close within 2s of cancellation")
		}
	}
}

// TestDistill_NoGoroutineLeak runs Distill many times with
// cancellation and asserts the goroutine count returns to baseline.
// Catches the class of bug where the teeingSink or its
// orchestrator goroutine forgets to exit on ctx cancellation.
func TestDistill_NoGoroutineLeak(t *testing.T) {
	// Let the runtime settle before snapshotting baseline.
	time.Sleep(20 * time.Millisecond)
	baseline := runtime.NumGoroutine()
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithCancel(t.Context())
		events, _, err := distill.Distill(ctx,
			strings.NewReader(gotestFixture),
			distill.Options{Writer: io.Discard, Format: "gotest"},
		)
		if err != nil {
			t.Fatalf("Distill iter %d: %v", i, err)
		}
		drainEvents(events)
		waitForChannelClose(t, events)
		cancel()
	}
	time.Sleep(50 * time.Millisecond)
	final := runtime.NumGoroutine()
	if final > baseline+2 {
		t.Errorf("goroutine count baseline=%d final=%d (leak suspected)",
			baseline, final)
	}
}

// TestDistill_DeterministicForFixedInput asserts the documented
// determinism invariant: same input twice → byte-equal output and
// matching Summary.
func TestDistill_DeterministicForFixedInput(t *testing.T) {
	type captured struct {
		output              string
		eventsEmitted       int
		eventsDroppedBudget int
		eventsTruncated     int
		exitCode            int
	}
	run := func() captured {
		w := &bytes.Buffer{}
		events, summary, err := distill.Distill(t.Context(),
			strings.NewReader(gotestFixture),
			distill.Options{Writer: w, Format: "gotest", Output: distill.OutputJSON},
		)
		if err != nil {
			t.Fatalf("Distill: %v", err)
		}
		drainEvents(events)
		waitForChannelClose(t, events)
		summary.Wait()
		return captured{
			output:              w.String(),
			eventsEmitted:       summary.EventsEmitted,
			eventsDroppedBudget: summary.EventsDroppedBudget,
			eventsTruncated:     summary.EventsTruncated,
			exitCode:            summary.ExitCode,
		}
	}
	c1 := run()
	c2 := run()
	if c1.output != c2.output {
		t.Errorf("output not deterministic:\nrun1:\n%s\nrun2:\n%s",
			c1.output, c2.output)
	}
	if c1 != c2 {
		t.Errorf("Summary not deterministic: %+v != %+v", c1, c2)
	}
}

// TestDistill_EventChannelDeliversEvents asserts the streaming
// contract: programmatic consumers receive every Event emitted by
// the pipeline, and the count matches Summary.EventsEmitted.
func TestDistill_EventChannelDeliversEvents(t *testing.T) {
	w := &bytes.Buffer{}
	events, summary, err := distill.Distill(t.Context(),
		strings.NewReader(gotestFixture),
		distill.Options{Writer: w, Format: "gotest"},
	)
	if err != nil {
		t.Fatalf("Distill: %v", err)
	}
	got := 0
	for ev := range events {
		_ = ev
		got++
	}
	summary.Wait()
	if got != summary.EventsEmitted {
		t.Errorf("channel saw %d events; Summary.EventsEmitted = %d",
			got, summary.EventsEmitted)
	}
}

// waitForChannelClose blocks until ch closes or the test deadline
// fires. Tests pair this with summary.Wait() to synchronise on
// both the Event channel and the Summary's done channel before
// reading Summary fields.
func waitForChannelClose(t *testing.T, ch <-chan distill.Event) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatalf("channel did not close within 5s")
		}
	}
}

// slowReader feeds the input in chunks with a delay between each.
// Used by TestDistill_StreamingBeforeEOF to prove the channel
// publishes Events incrementally rather than after EOF.
type slowReader struct {
	chunks [][]byte
	delay  time.Duration
	mu     sync.Mutex
	idx    int
	buf    []byte
}

func (s *slowReader) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.buf) == 0 {
		if s.idx >= len(s.chunks) {
			return 0, io.EOF
		}
		if s.idx > 0 {
			time.Sleep(s.delay)
		}
		s.buf = s.chunks[s.idx]
		s.idx++
	}
	n := copy(p, s.buf)
	s.buf = s.buf[n:]
	return n, nil
}
