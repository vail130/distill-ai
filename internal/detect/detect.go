// Package detect picks which formats.Format should parse a given
// input stream. It reads a small sample of the input, asks every
// registered Format to score the sample, and returns the best match.
//
// See ARCHITECTURE.md § Autodetection for the design, including the
// sample-size constant, the confidence threshold, and the tie-breaking
// rule between specific formats and the generic fallback.
package detect

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
)

// SampleSize is the number of bytes the detector reads from the
// opening of an input before deciding which Format should parse it.
// 4 KiB is enough for every format we currently support to score
// confidently — every known marker (pytest's "=== FAILURES ===", go
// test's "--- FAIL:", jest's "●", structured-JSON's `{"level":...`)
// appears within the first kilobyte of any realistic input.
const SampleSize = 4096

// GenericFormatName is the name reserved for the catch-all fallback
// Format. When no specific Format scores above ConfidenceMinDetect,
// Detect returns this Format if it is registered, otherwise an error.
// The generic Format itself lands in M9.
const GenericFormatName = "generic"

// ErrNoFormat is returned when detection fails: no Format scored above
// the threshold and no generic fallback is registered, or --strict was
// set and no specific Format matched. Wrap-friendly so callers can
// errors.Is past additional context.
var ErrNoFormat = errors.New("detect: no format matched")

// Result is the outcome of a Detect call. It carries enough metadata
// for the pipeline to immediately parse, for the detect subcommand to
// show what happened, and for tests to assert on the choice and the
// runner-up.
type Result struct {
	// Format is the chosen Format. Non-nil on success.
	Format formats.Format

	// Confidence is the chosen Format's self-reported confidence for
	// this sample.
	Confidence event.Confidence

	// Sample is the bytes the detector inspected. Useful for the
	// detect subcommand and for tests; never modify the slice.
	Sample []byte

	// Stream is the prepended reader: bytes equal to Sample followed
	// by whatever else r had to offer. The pipeline hands this to
	// Format.Parse without losing the bytes the detector consumed.
	Stream io.Reader

	// Runner is the second-place Format, or nil if there was no
	// second candidate or only the generic format was considered.
	// Together with RunnerConfidence this is what the detect
	// subcommand shows so users can debug ambiguous input.
	Runner formats.Format

	// RunnerConfidence is the second-place Format's confidence.
	RunnerConfidence event.Confidence

	// FellBackToGeneric is true if Format is the generic fallback
	// rather than a winner of the scoring round. Strict mode treats
	// this as an error.
	FellBackToGeneric bool
}

// Opts tunes a Detect call.
type Opts struct {
	// Formats overrides the registered Format set. When nil, Detect
	// uses formats.All(). Tests pass an explicit list to isolate from
	// the global registry; production code leaves this nil.
	Formats []formats.Format

	// Strict makes detection fail when no specific Format scores above
	// the threshold. Without Strict the detector falls back to the
	// generic Format if it is registered. With Strict the detector
	// returns an error wrapping ErrNoFormat instead.
	Strict bool
}

// scored is an internal Format → confidence pairing used during the
// fan-out / fan-in dance.
type scored struct {
	format formats.Format
	score  event.Confidence
}

// Detect inspects the first SampleSize bytes of r and returns the
// Format that should parse the stream. The returned Result.Stream
// contains all of the original bytes including the sample, so the
// caller passes Result.Stream straight to Format.Parse.
//
// Behaviour:
//
//   - Reads up to SampleSize bytes from r. Fewer is fine: empty and
//     truncated inputs are handled.
//   - Asks every Format from opts.Formats (default: formats.All) to
//     score the sample. Scoring runs concurrently; each Format's
//     Detect must be cheap and side-effect-free.
//   - Picks the highest-confidence Format ≥ ConfidenceMinDetect.
//   - When two Formats tie on confidence, the generic Format loses to
//     any specific Format, then alphabetical name order breaks
//     remaining ties so the choice is deterministic across runs.
//   - When no Format scores above the threshold:
//   - Strict: return ErrNoFormat.
//   - Not strict: return the generic Format if registered, else
//     ErrNoFormat.
//
// Detect never modifies the sample bytes and never reads past
// SampleSize bytes for scoring purposes; the rest of r is left for
// Format.Parse to consume via Result.Stream.
//
// Detect honours ctx during the scoring fan-out: a cancelled context
// returns ctx.Err() promptly without waiting for slow Format.Detect
// implementations to finish.
func Detect(ctx context.Context, r io.Reader, opts Opts) (*Result, error) {
	if r == nil {
		return nil, errors.New("detect: nil Reader")
	}
	sample, rest, err := readSample(r)
	if err != nil {
		return nil, fmt.Errorf("detect: read sample: %w", err)
	}
	candidates := opts.Formats
	if candidates == nil {
		candidates = formats.All()
	}
	// generic is never scored as a normal candidate; it's the
	// fallback target. Pull it out of the candidate list so
	// well-meaning Detect implementations on a generic format can't
	// accidentally win a tie against a specific format on confidence
	// alone.
	specific := make([]formats.Format, 0, len(candidates))
	var generic formats.Format
	for _, f := range candidates {
		if f.Name() == GenericFormatName {
			generic = f
			continue
		}
		specific = append(specific, f)
	}
	scores, err := scoreAll(ctx, specific, sample)
	if err != nil {
		return nil, err
	}
	winner, runner := pickWinner(scores)
	stream := prependSample(sample, rest)
	if winner.format != nil && winner.score >= event.ConfidenceMinDetect {
		return &Result{
			Format:           winner.format,
			Confidence:       winner.score,
			Sample:           sample,
			Stream:           stream,
			Runner:           runner.format,
			RunnerConfidence: runner.score,
		}, nil
	}
	// Below threshold: fall back to generic (unless strict).
	if opts.Strict {
		return nil, fmt.Errorf("%w: highest confidence %.2f below threshold %.2f", ErrNoFormat, winner.score, event.ConfidenceMinDetect)
	}
	if generic == nil {
		return nil, fmt.Errorf("%w: no specific format matched and %q is not registered", ErrNoFormat, GenericFormatName)
	}
	return &Result{
		Format:            generic,
		Confidence:        0,
		Sample:            sample,
		Stream:            stream,
		Runner:            winner.format,
		RunnerConfidence:  winner.score,
		FellBackToGeneric: true,
	}, nil
}

// readSample reads up to SampleSize bytes from r and returns them
// along with the remainder of r. io.EOF inside the sample is fine; it
// just means the input was shorter than SampleSize.
func readSample(r io.Reader) ([]byte, io.Reader, error) {
	buf := make([]byte, SampleSize)
	n, err := io.ReadFull(r, buf)
	switch {
	case err == nil:
		return buf[:n], r, nil
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		// Short input; r is exhausted. Return what we read and an
		// empty trailing reader so prependSample produces the same
		// shape as the happy path.
		return buf[:n], emptyReader{}, nil
	default:
		return nil, nil, err
	}
}

// emptyReader is io.EOF on every Read. Used when readSample exhausts r.
type emptyReader struct{}

func (emptyReader) Read(_ []byte) (int, error) { return 0, io.EOF }

// scoreAll asks every Format to score the sample, in parallel. Each
// Format's Detect must be cheap; bounded concurrency is overkill at
// the format counts we'll ever reasonably register.
func scoreAll(ctx context.Context, fs []formats.Format, sample []byte) ([]scored, error) {
	if len(fs) == 0 {
		return nil, nil
	}
	results := make([]scored, len(fs))
	var wg sync.WaitGroup
	for i, f := range fs {
		i, f := i, f
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Allow ctx cancellation to short-circuit before we even
			// call Detect.
			if ctx.Err() != nil {
				return
			}
			results[i] = scored{format: f, score: f.Detect(sample)}
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		// Filter out zero-format entries (those whose ctx check
		// fired before Detect ran).
		out := results[:0]
		for _, s := range results {
			if s.format != nil {
				out = append(out, s)
			}
		}
		return out, nil
	case <-ctx.Done():
		// Best-effort: return ctx.Err. The goroutines will finish
		// in the background and write to a slice nobody reads. They
		// don't leak because they're not waiting on anything.
		return nil, ctx.Err()
	}
}

// pickWinner returns the winner and runner-up from a scored slice.
// Sorting rules:
//
//   - Higher score first.
//   - On equal score, alphabetical Name order for deterministic
//     output across runs (Go map iteration is randomised; without an
//     explicit order, ties would resolve nondeterministically).
//
// The generic format is already excluded from the slice by Detect
// before pickWinner sees it, so the second rule isn't strictly the
// "generic loses ties" rule — that's enforced earlier — but the same
// alphabetical fallback applies to any two specific formats that
// score identically.
func pickWinner(scores []scored) (winner, runner scored) {
	if len(scores) == 0 {
		return scored{}, scored{}
	}
	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].score != scores[j].score {
			return scores[i].score > scores[j].score
		}
		return scores[i].format.Name() < scores[j].format.Name()
	})
	winner = scores[0]
	if len(scores) > 1 {
		runner = scores[1]
	}
	return winner, runner
}

// prependSample returns a reader that yields sample's bytes first,
// then rest's bytes. Pointer-friendly; no copies of sample beyond what
// bytes.NewReader does internally.
func prependSample(sample []byte, rest io.Reader) io.Reader {
	if len(sample) == 0 {
		return rest
	}
	return io.MultiReader(bytes.NewReader(sample), rest)
}
