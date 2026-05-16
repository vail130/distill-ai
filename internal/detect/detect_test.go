package detect_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/detect"
	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
)

// fakeFormat is a Format whose Name and Detect score are
// test-controlled. Parse is unused (the M3 detector only calls
// Name and Detect).
type fakeFormat struct {
	name  string
	score event.Confidence
}

func (f *fakeFormat) Name() string                     { return f.name }
func (f *fakeFormat) Detect(_ []byte) event.Confidence { return f.score }
func (f *fakeFormat) Parse(_ context.Context, _ io.Reader, _ formats.ParseOpts) (<-chan event.Event, error) {
	ch := make(chan event.Event)
	close(ch)
	return ch, nil
}

func TestDetect_HighConfidenceWins(t *testing.T) {
	res, err := detect.Detect(context.Background(), strings.NewReader("ignored"), detect.Opts{
		Formats: []formats.Format{
			&fakeFormat{name: "low", score: 0.7},
			&fakeFormat{name: "high", score: 0.95},
		},
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if res.Format.Name() != "high" {
		t.Errorf("winner = %q, want high", res.Format.Name())
	}
	if res.Runner == nil || res.Runner.Name() != "low" {
		t.Errorf("runner = %v, want low", res.Runner)
	}
}

func TestDetect_GenericLosesTies(t *testing.T) {
	// A specific format and the generic format both report 0.8.
	// Per the design, the generic format is never scored as a
	// normal candidate — it is reserved for the fallback path.
	// So this test asserts the specific format wins even though
	// the generic format would have scored equally.
	res, err := detect.Detect(context.Background(), strings.NewReader("input"), detect.Opts{
		Formats: []formats.Format{
			&fakeFormat{name: detect.GenericFormatName, score: 0.8},
			&fakeFormat{name: "pytest", score: 0.8},
		},
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if res.Format.Name() != "pytest" {
		t.Errorf("winner = %q, want pytest; generic should never win a tie", res.Format.Name())
	}
}

func TestDetect_BelowThresholdFallsBackToGeneric(t *testing.T) {
	res, err := detect.Detect(context.Background(), strings.NewReader("input"), detect.Opts{
		Formats: []formats.Format{
			&fakeFormat{name: "pytest", score: 0.4},
			&fakeFormat{name: detect.GenericFormatName, score: 0},
		},
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !res.FellBackToGeneric {
		t.Errorf("expected FellBackToGeneric=true; got %+v", res)
	}
	if res.Format.Name() != detect.GenericFormatName {
		t.Errorf("Format = %q, want %q", res.Format.Name(), detect.GenericFormatName)
	}
	if res.Runner == nil || res.Runner.Name() != "pytest" {
		t.Errorf("Runner should be the would-be winner; got %v", res.Runner)
	}
}

func TestDetect_BelowThresholdNoGenericReturnsError(t *testing.T) {
	_, err := detect.Detect(context.Background(), strings.NewReader("input"), detect.Opts{
		Formats: []formats.Format{
			&fakeFormat{name: "pytest", score: 0.4},
		},
	})
	if !errors.Is(err, detect.ErrNoFormat) {
		t.Errorf("err = %v, want errors.Is(ErrNoFormat)", err)
	}
}

func TestDetect_StrictReturnsErrorOnLowConfidence(t *testing.T) {
	_, err := detect.Detect(context.Background(), strings.NewReader("input"), detect.Opts{
		Strict: true,
		Formats: []formats.Format{
			&fakeFormat{name: "pytest", score: 0.4},
			&fakeFormat{name: detect.GenericFormatName, score: 0},
		},
	})
	if !errors.Is(err, detect.ErrNoFormat) {
		t.Errorf("strict mode below threshold: err = %v, want errors.Is(ErrNoFormat)", err)
	}
}

func TestDetect_NonStrictFallsBack(t *testing.T) {
	res, err := detect.Detect(context.Background(), strings.NewReader("input"), detect.Opts{
		Formats: []formats.Format{
			&fakeFormat{name: "pytest", score: 0.4},
			&fakeFormat{name: detect.GenericFormatName, score: 0},
		},
	})
	if err != nil {
		t.Fatalf("non-strict: unexpected error %v", err)
	}
	if !res.FellBackToGeneric {
		t.Errorf("non-strict should fall back; got %+v", res)
	}
}

func TestDetect_StrictAboveThresholdSucceeds(t *testing.T) {
	res, err := detect.Detect(context.Background(), strings.NewReader("input"), detect.Opts{
		Strict: true,
		Formats: []formats.Format{
			&fakeFormat{name: "pytest", score: 0.9},
		},
	})
	if err != nil {
		t.Fatalf("strict + high confidence: %v", err)
	}
	if res.Format.Name() != "pytest" {
		t.Errorf("Format = %q, want pytest", res.Format.Name())
	}
}

func TestDetect_EmptyInput(t *testing.T) {
	res, err := detect.Detect(context.Background(), strings.NewReader(""), detect.Opts{
		Formats: []formats.Format{
			// A format that claims confidence even on empty input;
			// nothing about the design prevents that.
			&fakeFormat{name: "pytest", score: 0.9},
			&fakeFormat{name: detect.GenericFormatName, score: 0},
		},
	})
	if err != nil {
		t.Fatalf("Detect empty: %v", err)
	}
	if len(res.Sample) != 0 {
		t.Errorf("Sample on empty input = %v, want empty", res.Sample)
	}
	// Stream should be readable and immediately EOF.
	buf := make([]byte, 1)
	n, eofErr := res.Stream.Read(buf)
	if n != 0 || !errors.Is(eofErr, io.EOF) {
		t.Errorf("empty Stream Read: n=%d err=%v, want 0, EOF", n, eofErr)
	}
}

func TestDetect_BinaryInput(t *testing.T) {
	// Random-ish bytes including NUL. The detector and any sane
	// Detect implementation must not crash.
	data := []byte{0, 1, 2, 0xff, 0xfe, 'a', 0, 'b', 0xc0, 0xc1}
	res, err := detect.Detect(context.Background(), bytes.NewReader(data), detect.Opts{
		Formats: []formats.Format{
			&fakeFormat{name: "pytest", score: 0.1},
			&fakeFormat{name: detect.GenericFormatName, score: 0},
		},
	})
	if err != nil {
		t.Fatalf("binary input: %v", err)
	}
	if !res.FellBackToGeneric {
		t.Errorf("binary input below threshold should fall back; got %+v", res)
	}
}

func TestDetect_SingleByteInput(t *testing.T) {
	res, err := detect.Detect(context.Background(), strings.NewReader("X"), detect.Opts{
		Formats: []formats.Format{
			&fakeFormat{name: "pytest", score: 0.9},
		},
	})
	if err != nil {
		t.Fatalf("single byte: %v", err)
	}
	if len(res.Sample) != 1 || res.Sample[0] != 'X' {
		t.Errorf("Sample = %v, want [X]", res.Sample)
	}
}

func TestDetect_SampleNotConsumed(t *testing.T) {
	original := strings.Repeat("ab", 4096) // > SampleSize
	res, err := detect.Detect(context.Background(), strings.NewReader(original), detect.Opts{
		Formats: []formats.Format{
			&fakeFormat{name: "pytest", score: 0.9},
		},
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(res.Sample) != detect.SampleSize {
		t.Errorf("Sample length = %d, want %d", len(res.Sample), detect.SampleSize)
	}
	got, err := io.ReadAll(res.Stream)
	if err != nil {
		t.Fatalf("read Stream: %v", err)
	}
	if string(got) != original {
		t.Errorf("Stream content diverged from input:\n got len=%d\nwant len=%d", len(got), len(original))
	}
}

func TestDetect_NilReader(t *testing.T) {
	_, err := detect.Detect(context.Background(), nil, detect.Opts{})
	if err == nil {
		t.Error("nil Reader should produce an error")
	}
}

func TestDetect_NoFormatsRegistered(t *testing.T) {
	// Empty Formats list and no generic anywhere.
	_, err := detect.Detect(context.Background(), strings.NewReader("x"), detect.Opts{
		Formats: []formats.Format{},
	})
	if !errors.Is(err, detect.ErrNoFormat) {
		t.Errorf("err = %v, want errors.Is(ErrNoFormat)", err)
	}
}

func TestDetect_DeterministicTieBreaking(t *testing.T) {
	// Two formats with identical scores; the alphabetically earlier
	// one must win every time.
	for i := 0; i < 50; i++ {
		res, err := detect.Detect(context.Background(), strings.NewReader("input"), detect.Opts{
			Formats: []formats.Format{
				// Register out of alphabetical order to verify
				// the sort isn't accidentally relying on input
				// ordering.
				&fakeFormat{name: "zebra", score: 0.9},
				&fakeFormat{name: "alpha", score: 0.9},
				&fakeFormat{name: "mike", score: 0.9},
			},
		})
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if res.Format.Name() != "alpha" {
			t.Errorf("iter %d: winner = %q, want alpha (alphabetical tie-break)", i, res.Format.Name())
		}
	}
}

func TestDetect_UsesGlobalRegistryByDefault(t *testing.T) {
	formats.ResetForTest()
	t.Cleanup(formats.ResetForTest)
	formats.Register(&fakeFormat{name: "registered", score: 0.9})
	res, err := detect.Detect(context.Background(), strings.NewReader("input"), detect.Opts{})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if res.Format.Name() != "registered" {
		t.Errorf("Format = %q, want registered (from global registry)", res.Format.Name())
	}
}

func TestDetect_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := detect.Detect(ctx, strings.NewReader("input"), detect.Opts{
		Formats: []formats.Format{&fakeFormat{name: "x", score: 0.9}},
	})
	// Either ctx.Err() or success: the goroutines might finish
	// before the ctx check fires. Both behaviours are acceptable;
	// we just verify we don't hang or panic.
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want either nil or context.Canceled", err)
	}
}

func TestSampleSize_ReasonableConstant(t *testing.T) {
	if detect.SampleSize < 1024 {
		t.Errorf("SampleSize = %d, want ≥ 1024 (formats need room to find markers)", detect.SampleSize)
	}
}
