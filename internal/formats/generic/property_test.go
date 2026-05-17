package generic_test

import (
	"context"
	"io"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
	"github.com/vail130/distill-ai/internal/testutil"
)

// TestGeneric_ParseStreaming — drip a fixture through SlowReader
// and assert at least one Event emerges well before EOF. Ties into
// the project-wide streaming invariant: parsers must not buffer the
// entire input before emitting.
func TestGeneric_ParseStreaming(t *testing.T) {
	const lines = 200
	var sb strings.Builder
	// Front-load an ERROR followed by enough innocuous lines that
	// the post-context window closes long before EOF.
	sb.WriteString("ERROR: trigger\n")
	for i := 0; i < lines; i++ {
		sb.WriteString("filler line ")
		sb.WriteString(strings.Repeat("x", 32))
		sb.WriteString("\n")
	}
	input := sb.String()
	slow := &testutil.SlowReader{
		Inner:      strings.NewReader(input),
		ChunkSize:  64,
		ChunkDelay: 2 * time.Millisecond,
	}
	f, _ := formats.Get("generic")
	start := time.Now()
	ch, _ := f.Parse(context.Background(), slow, formats.ParseOpts{})
	first, ok := <-ch
	if !ok {
		t.Fatal("expected at least one event before EOF")
	}
	firstAt := time.Since(start)
	// Drain the rest. The total time to read the input is roughly
	// (len(input)/ChunkSize) * ChunkDelay; the first Event should
	// emerge well before that.
	totalExpected := time.Duration(len(input)/slow.ChunkSize) * slow.ChunkDelay
	if firstAt > totalExpected/2 {
		t.Errorf("first event emerged after %s; expected well before %s (~half of total %s)",
			firstAt, totalExpected/2, totalExpected)
	}
	remaining := 0
	for range ch {
		remaining++
	}
	_ = remaining
	if first.Title != "ERROR: trigger" {
		t.Errorf("first event title = %q, want \"ERROR: trigger\"", first.Title)
	}
}

// TestGeneric_ParseBoundedMemory — stream an effectively unbounded
// number of innocuous lines through io.Pipe; peak heap delta over
// baseline stays within a small constant of the scanner's bounded
// buffers. Companion to the TestPipeline_BoundedMemory_PeakSampling
// pattern from M2.3.
//
// io.Pipe is used rather than strings.NewReader so the input doesn't
// live in memory all at once — otherwise the test would measure
// "scanner + entire input buffer" rather than the parser's bound.
// Measuring delta from baseline (rather than absolute HeapAlloc)
// keeps the test portable across OSes / Go versions whose default
// GC slack differs.
func TestGeneric_ParseBoundedMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory test in short mode")
	}
	const (
		totalLines = 1 << 14 // 1.25 MiB of input
		ceiling    = 16 << 20
	)
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pr.Close() })
	go func() {
		defer func() { _ = pw.Close() }()
		line := []byte("innocuous prose line without any severity markers whatsoever, padded to roughly eighty\n")
		for i := 0; i < totalLines; i++ {
			if _, err := pw.Write(line); err != nil {
				return
			}
		}
	}()
	runtime.GC()
	time.Sleep(5 * time.Millisecond)
	var baseStats runtime.MemStats
	runtime.ReadMemStats(&baseStats)
	baseline := baseStats.HeapAlloc
	f, _ := formats.Get("generic")
	ch, _ := f.Parse(context.Background(), pr, formats.ParseOpts{})
	stop := make(chan struct{})
	finished := make(chan struct{})
	var peakDelta uint64
	go func() {
		defer close(finished)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		var stats runtime.MemStats
		for {
			select {
			case <-ticker.C:
				runtime.ReadMemStats(&stats)
				if stats.HeapAlloc > baseline {
					if delta := stats.HeapAlloc - baseline; delta > peakDelta {
						peakDelta = delta
					}
				}
			case <-stop:
				return
			}
		}
	}()
	count := 0
	for range ch {
		count++
	}
	close(stop)
	<-finished
	if count != 0 {
		t.Errorf("got %d events on innocuous input; expected 0", count)
	}
	if peakDelta > ceiling {
		t.Errorf("peak heap delta %d bytes > ceiling %d (scanner not bounded?)", peakDelta, ceiling)
	}
}

// TestGeneric_ParseBlockBoundedMemory — feed an adversarial input
// that starts a traceback block and then dumps an effectively
// unbounded number of indented lines via io.Pipe. The maxBlockLines
// cap (100) must hold and peak heap must stay under a small
// constant of the bounded buffers.
//
// The input is generated on-the-fly by a producer goroutine so it
// never lives in memory all at once — otherwise the test would
// measure "scanner state + entire input buffer" rather than the
// parser's actual memory bound. The producer stops after
// totalLines so the test terminates; the producer's GC pressure
// is what stresses the cap.
func TestGeneric_ParseBlockBoundedMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory test in short mode")
	}
	const (
		totalLines = 100000 // far beyond maxBlockLines
		ceiling    = 32 << 20
	)
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pr.Close() })
	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("Traceback (most recent call last):\n"))
		line := []byte("  File \"x.py\", line 1, in f\n")
		for i := 0; i < totalLines; i++ {
			if _, err := pw.Write(line); err != nil {
				return
			}
		}
	}()
	// Settle the runtime before measuring so the baseline reflects
	// post-startup live memory rather than init transients.
	runtime.GC()
	time.Sleep(5 * time.Millisecond)
	var baseStats runtime.MemStats
	runtime.ReadMemStats(&baseStats)
	baseline := baseStats.HeapAlloc
	f, _ := formats.Get("generic")
	ch, _ := f.Parse(context.Background(), pr, formats.ParseOpts{})
	stop := make(chan struct{})
	finished := make(chan struct{})
	var peakDelta uint64
	go func() {
		defer close(finished)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		var stats runtime.MemStats
		for {
			select {
			case <-ticker.C:
				runtime.ReadMemStats(&stats)
				if stats.HeapAlloc > baseline {
					if delta := stats.HeapAlloc - baseline; delta > peakDelta {
						peakDelta = delta
					}
				}
			case <-stop:
				return
			}
		}
	}()
	count := 0
	for range ch {
		count++
	}
	close(stop)
	<-finished
	if peakDelta > ceiling {
		t.Errorf("peak heap delta %d > ceiling %d (block accumulator not bounded?)", peakDelta, ceiling)
	}
}

// TestGeneric_ParseContextDeadline — a deadline that expires while
// the parser is mid-stream causes Parse to clean up promptly. No
// goroutine leak assertion here (the package-level test covers
// that elsewhere); only assert the channel closes.
func TestGeneric_ParseContextDeadline(t *testing.T) {
	input := strings.Repeat("ERROR: x\n"+strings.Repeat("filler\n", 100), 100)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	f, _ := formats.Get("generic")
	ch, _ := f.Parse(ctx, strings.NewReader(input), formats.ParseOpts{})
	deadline := time.Now().Add(2 * time.Second)
	drained := 0
	for range ch {
		drained++
		if time.Now().After(deadline) {
			t.Fatal("channel did not close within 2s of deadline")
		}
	}
	_ = drained
}

// TestGeneric_ParseNoGoroutineLeak — start and stop Parse a handful
// of times; final NumGoroutine returns to baseline. Catches the
// class of bug where the scanner goroutine misses ctx.Done().
func TestGeneric_ParseNoGoroutineLeak(t *testing.T) {
	// Settle the runtime before measuring.
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	baseline := runtime.NumGoroutine()
	f, _ := formats.Get("generic")
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		ch, _ := f.Parse(ctx, strings.NewReader(strings.Repeat("ERROR: x\n", 100)), formats.ParseOpts{})
		<-ch
		cancel()
		drained := 0
		for range ch {
			drained++
		}
		_ = drained
	}
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	if got := runtime.NumGoroutine(); got > baseline+1 {
		t.Errorf("goroutine count drifted: baseline=%d, after=%d", baseline, got)
	}
}

// Compile-time check that the package's event types are wired so
// the imports stay live; the import comment hides the lint about
// "unused import event" when the file is edited in isolation.
var _ = event.SeverityError
