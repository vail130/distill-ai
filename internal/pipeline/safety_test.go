package pipeline_test

import (
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/pipeline"
)

// infiniteLineReader is an io.Reader that produces the same line
// forever. Used by bounded-memory tests so we can pipe arbitrarily
// large input without buffering it in the test fixture.
type infiniteLineReader struct {
	line []byte
	pos  int
}

func newInfiniteLineReader(line string) *infiniteLineReader {
	return &infiniteLineReader{line: []byte(line + "\n")}
}

func (r *infiniteLineReader) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		c := copy(p[n:], r.line[r.pos:])
		n += c
		r.pos += c
		if r.pos == len(r.line) {
			r.pos = 0
		}
	}
	return n, nil
}

// limitedReader is io.LimitedReader without the alloc overhead of
// reusing the stdlib one in a tight loop.
type limitedReader struct {
	R io.Reader
	N int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.N <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > l.N {
		p = p[:l.N]
	}
	n, err := l.R.Read(p)
	l.N -= int64(n)
	return n, err
}

// TestPipeline_NoGoroutineLeak_Stressed runs the pipeline 100 times
// under the success path; the goroutine count must return to baseline
// within a small slack window. This is the M2.3 hardened version of
// the cheap leak check shipped in M2.1.
func TestPipeline_NoGoroutineLeak_Stressed(t *testing.T) {
	baseline := snapshotGoroutines()
	const iterations = 100
	for i := 0; i < iterations; i++ {
		sink := &collectSink{}
		p := &pipeline.Pipeline{
			Source: &pipeline.FormatSource{
				Format: lineFormat{},
				Reader: strings.NewReader("a\nb\nc\nd\ne\n"),
			},
			Stages: []pipeline.Stage{
				pipeline.PassthroughStage{},
				pipeline.PassthroughStage{},
			},
			Sink: sink,
		}
		if err := p.Run(context.Background()); err != nil {
			t.Fatalf("iter %d: Run: %v", i, err)
		}
	}
	after := snapshotGoroutinesAfterSettle()
	if diff := after - baseline; diff > 4 {
		t.Errorf("goroutine leak across %d runs: baseline=%d after=%d diff=%d",
			iterations, baseline, after, diff)
	}
}

// TestPipeline_NoGoroutineLeak_OnCancel runs the pipeline 100 times
// where each run is cancelled mid-stream. Cancellation must not leak
// the source / stage / sink goroutines; if the relay loop or any
// stage forgets to honour ctx.Done(), goroutines accumulate.
func TestPipeline_NoGoroutineLeak_OnCancel(t *testing.T) {
	baseline := snapshotGoroutines()
	const iterations = 100
	for i := 0; i < iterations; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		sink := &collectSink{delay: 100 * time.Microsecond}
		p := &pipeline.Pipeline{
			Source: &pipeline.FormatSource{
				Format: lineFormat{},
				Reader: strings.NewReader(strings.Repeat("line\n", 50)),
			},
			Stages: []pipeline.Stage{pipeline.PassthroughStage{}},
			Sink:   sink,
		}
		go func() {
			time.Sleep(200 * time.Microsecond)
			cancel()
		}()
		err := p.Run(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("iter %d: unexpected Run error: %v", i, err)
		}
		cancel()
	}
	after := snapshotGoroutinesAfterSettle()
	if diff := after - baseline; diff > 4 {
		t.Errorf("goroutine leak across %d cancelled runs: baseline=%d after=%d diff=%d",
			iterations, baseline, after, diff)
	}
}

// TestPipeline_NoGoroutineLeak_OnSinkError exercises the error
// propagation path: when the Sink returns an error, the source / stages
// must observe the cancelled context and exit promptly. A forgotten
// ctx check would leak the source goroutine.
func TestPipeline_NoGoroutineLeak_OnSinkError(t *testing.T) {
	baseline := snapshotGoroutines()
	sentinel := errors.New("forced sink error")
	const iterations = 50
	for i := 0; i < iterations; i++ {
		// Large input ensures the source is still emitting when
		// the sink errors out.
		input := &limitedReader{R: newInfiniteLineReader("line"), N: 1 << 16}
		p := &pipeline.Pipeline{
			Source: &pipeline.FormatSource{
				Format: lineFormat{},
				Reader: input,
			},
			Stages: []pipeline.Stage{pipeline.PassthroughStage{}},
			Sink:   &erroringSink{err: sentinel},
		}
		err := p.Run(context.Background())
		if !errors.Is(err, sentinel) {
			t.Fatalf("iter %d: Run = %v, want sentinel", i, err)
		}
	}
	after := snapshotGoroutinesAfterSettle()
	if diff := after - baseline; diff > 4 {
		t.Errorf("goroutine leak across %d failing runs: baseline=%d after=%d diff=%d",
			iterations, baseline, after, diff)
	}
}

// TestPipeline_BoundedMemory_PeakSampling pipes a large input through
// a discarding sink while a sampler goroutine polls live heap usage.
// The peak measurement must stay under a generous absolute ceiling
// regardless of input size, proving the pipeline doesn't buffer the
// stream.
//
// The pipeline's working set is bounded by:
//
//	BufferSize × (len(Stages) + 1) × sizeof(Event) + parser scratch
//
// For BufferSize=16 and one Stage, that's ~32 in-flight Events plus
// bufio.Scanner's MaxScanTokenSize (default 64 KB) — well under 1 MB
// even with chunky Event bodies. We test against a 16 MB ceiling so
// we'd catch a real buffering regression (which would scale to
// hundreds of MB at this input size) but not flake on allocator
// behaviour.
//
// HeapAlloc rather than HeapInuse: HeapInuse includes allocator-held
// pages that won't shrink even after GC, which would make the test
// flaky. HeapAlloc tracks live-after-last-GC.
func TestPipeline_BoundedMemory_PeakSampling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping bounded-memory test in short mode")
	}
	const (
		inputBytes  = 8 << 20  // 8 MB of input
		peakCeiling = 16 << 20 // 16 MB live-heap ceiling
		sampleEvery = time.Millisecond
	)
	reader := &limitedReader{
		R: newInfiniteLineReader("synthetic-line-content-padding-to-make-events-non-trivial"),
		N: inputBytes,
	}
	p := &pipeline.Pipeline{
		Source: &pipeline.FormatSource{Format: lineFormat{}, Reader: reader},
		Stages: []pipeline.Stage{pipeline.PassthroughStage{}},
		Sink:   discardSink{},
	}
	stop := make(chan struct{})
	peakCh := make(chan uint64, 1)
	go func() {
		var peak uint64
		t := time.NewTicker(sampleEvery)
		defer t.Stop()
		var ms runtime.MemStats
		for {
			select {
			case <-stop:
				peakCh <- peak
				return
			case <-t.C:
				runtime.ReadMemStats(&ms)
				if ms.HeapAlloc > peak {
					peak = ms.HeapAlloc
				}
			}
		}
	}()
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	close(stop)
	peak := <-peakCh
	if peak > peakCeiling {
		t.Errorf("peak live-heap during %d-byte run was %d bytes; ceiling %d bytes — pipeline appears to buffer the stream",
			inputBytes, peak, peakCeiling)
	}
	t.Logf("bounded-memory: %d-byte input → %d bytes peak live-heap (ceiling %d)", inputBytes, peak, peakCeiling)
}

// BenchmarkPipeline_LargeInput runs the pipeline against a fixed-size
// large input. Useful as a baseline for future stages that might
// regress throughput or alloc behaviour. Not run by default; invoke
// with 'go test -bench=BenchmarkPipeline ./internal/pipeline/'.
func BenchmarkPipeline_LargeInput(b *testing.B) {
	const inputBytes = 1 << 20 // 1 MB per iteration
	for i := 0; i < b.N; i++ {
		reader := &limitedReader{
			R: newInfiniteLineReader("benchmark-line-content"),
			N: inputBytes,
		}
		p := &pipeline.Pipeline{
			Source: &pipeline.FormatSource{Format: lineFormat{}, Reader: reader},
			Stages: []pipeline.Stage{pipeline.PassthroughStage{}},
			Sink:   discardSink{},
		}
		if err := p.Run(context.Background()); err != nil {
			b.Fatalf("Run: %v", err)
		}
	}
	b.SetBytes(inputBytes)
}

// discardSink is a zero-alloc Sink that drains the channel.
type discardSink struct{}

func (discardSink) Sink(_ context.Context, in <-chan event.Event) error {
	count := 0
	for range in {
		count++
	}
	// Reference count to defeat dead-code elimination.
	_ = count
	return nil
}

func snapshotGoroutines() int {
	runtime.GC()
	runtime.Gosched()
	return runtime.NumGoroutine()
}

func snapshotGoroutinesAfterSettle() int {
	// Let any deferred goroutines finish before sampling.
	for i := 0; i < 4; i++ {
		runtime.GC()
		time.Sleep(50 * time.Millisecond)
	}
	return runtime.NumGoroutine()
}
