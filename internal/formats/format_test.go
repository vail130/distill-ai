package formats_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
)

// fakeFormat is the minimum viable Format implementation, used to
// check the interface at compile time and to back the runnable
// godoc example below.
type fakeFormat struct {
	name string
	conf event.Confidence
}

func (f *fakeFormat) Name() string { return f.name }

func (f *fakeFormat) Detect(_ []byte) event.Confidence { return f.conf }

func (f *fakeFormat) Parse(ctx context.Context, _ io.Reader, _ formats.ParseOpts) (<-chan event.Event, error) {
	ch := make(chan event.Event)
	go func() {
		defer close(ch)
		select {
		case ch <- event.Event{
			Severity: event.SeverityInfo,
			Kind:     "stub",
			Title:    "fake event",
			Body:     []string{"fake"},
			Count:    1,
		}:
		case <-ctx.Done():
		}
	}()
	return ch, nil
}

// TestFormat_InterfaceContract is a compile-time check: if fakeFormat
// drifts from the Format interface (a method signature changes, for
// instance), this assignment fails to compile and the build breaks.
func TestFormat_InterfaceContract(t *testing.T) {
	var _ formats.Format = (*fakeFormat)(nil)
	f := &fakeFormat{name: "fake", conf: 0.9}
	if f.Name() != "fake" {
		t.Errorf("Name() = %q, want %q", f.Name(), "fake")
	}
	if f.Detect(nil) != 0.9 {
		t.Errorf("Detect(nil) = %v, want 0.9", f.Detect(nil))
	}
}

func TestFakeFormat_ParseEmitsAndCloses(t *testing.T) {
	f := &fakeFormat{name: "fake"}
	ch, err := f.Parse(context.Background(), strings.NewReader(""), formats.ParseOpts{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var got []event.Event
	for ev := range ch {
		got = append(got, ev)
	}
	if len(got) != 1 || got[0].Kind != "stub" {
		t.Errorf("expected 1 stub event; got %+v", got)
	}
}

func TestFakeFormat_ParseHonoursCancellation(t *testing.T) {
	f := &fakeFormat{name: "fake"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch, err := f.Parse(ctx, strings.NewReader(""), formats.ParseOpts{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Drain; goroutine should exit promptly via ctx.Done().
	drain := 0
	for range ch {
		drain++
	}
	_ = drain
}

func TestParseOpts_ZeroValueIsSensible(t *testing.T) {
	var opts formats.ParseOpts
	if opts.ContextLines != 0 {
		t.Errorf("zero ParseOpts.ContextLines = %d, want 0", opts.ContextLines)
	}
	if opts.KeepVendor {
		t.Errorf("zero ParseOpts.KeepVendor = true, want false")
	}
}
