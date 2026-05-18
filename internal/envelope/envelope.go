// Package envelope strips CI- and orchestrator-level wrappers from
// distill-ai's input stream before format autodetection runs.
//
// A GitHub Actions log, a GitLab CI trace, or a Docker buildkit log
// decorates the underlying command output with timestamps, group
// markers, severity directives, and section boundaries. Those bytes
// confuse the format detector — a wrapped `go test` failure no longer
// looks like a `go test` failure to gotest.Detect — and they cost
// tokens once they reach the encoder. The envelope package solves
// both: a Stripper sits in front of detection, returns a cleaned
// Reader that yields the underlying bytes, and surfaces wrapper-level
// signals (a `##[error]` line outside any group, a step exiting
// non-zero) as their own Events with the dedicated `envelope_*` Kinds.
//
// Strippers are deliberately decorators, not Formats. A wrapped gotest
// stream still parses as gotest with `Confidence=1.0`; the GitHub
// Actions envelope is metadata that happens to be present, not the
// format of interest. Keeping the abstraction separate avoids forcing
// every Format implementation to learn every CI wrapper.
//
// Design references:
//   - ARCHITECTURE.md § Autodetection (where Wrap sits in the chain).
//   - docs/envelope.md (the user-facing overview).
//   - docs/formats/SCHEMA.md § Envelope kinds (the public Kind list).
//
// M13.1 lands the package skeleton, the Stripper interface, the
// thread-safe registry, the Noop stripper, and the Wrap entry point.
// Concrete strippers (GitHub Actions, GitLab CI) and the CLI flag /
// pipeline wiring follow in M13.2–M13.5.
package envelope

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/vail130/distill-ai/internal/event"
)

// SampleSize is the number of bytes Wrap reads from the input before
// asking each registered Stripper to score it. Matches detect.SampleSize
// so envelope detection and format detection see the same window — a
// stripper that requires more than 4 KiB to identify itself is almost
// certainly looking at the wrong signal.
const SampleSize = 4096

// SignalBufferSize is the default capacity for the signals channel a
// Stripper returns. Matches pipeline.DefaultBufferSize so envelope
// signals participate in the same backpressure regime as parser
// Events.
const SignalBufferSize = 16

// Kind values for envelope-level signal Events. They are additive to
// SCHEMA.md and do not bump schema_version per the output-stability
// rule. Strippers MUST use these constants rather than string literals
// so a future renaming is a single-point change.
const (
	// KindEnvelopeError is set on signal Events that surface a
	// wrapper-level error: GitHub Actions' `##[error]`, GitLab CI's
	// `section_end` with a non-zero exit, or equivalents. Severity
	// is always SeverityError.
	KindEnvelopeError = "envelope_error"

	// KindEnvelopeWarning is set on signal Events for wrapper-level
	// warnings: `##[warning]` and equivalents. Severity is always
	// SeverityWarn.
	KindEnvelopeWarning = "envelope_warning"

	// KindEnvelopeStepFailure is set on signal Events that mark a
	// named job step or section ending with a non-zero exit code.
	// The step name is the Event Title; Metadata carries "step" and
	// "exit_code" entries so downstream consumers can route on
	// either. Severity is always SeverityError.
	KindEnvelopeStepFailure = "envelope_step_failure"
)

// Choice values for Options.Choice, the user-facing setting from the
// `--strip-envelope` flag M13.2 wires in.
const (
	// ChoiceAuto runs detection and picks the highest-confidence
	// stripper at or above ConfidenceMinDetect (the same threshold
	// the format detector uses). The default.
	ChoiceAuto = "auto"

	// ChoiceNone forces the Noop stripper, skipping detection even
	// if a registered stripper would have claimed the sample.
	// Useful for callers who know their input is bare command
	// output.
	ChoiceNone = "none"
)

// ConfidenceMinDetect is the confidence floor a Stripper must reach
// for Wrap to pick it. Mirrors event.ConfidenceMinDetect for symmetry
// with format detection: a stripper that scores below 0.6 falls back
// to Noop the same way a format that scores below 0.6 falls back to
// generic.
const ConfidenceMinDetect event.Confidence = event.ConfidenceMinDetect

// ErrUnknownStripper is returned by Wrap when Options.Choice names a
// stripper that has not been registered. Wrap-friendly so callers can
// errors.Is past additional context.
var ErrUnknownStripper = errors.New("envelope: unknown stripper")

// Stripper decorates an input stream by removing wrapper-level
// metadata and emitting wrapper-level signals as Events.
//
// Implementations are decorators, not parsers: the cleaned Reader
// returned by Strip is handed to the format detector and then to
// Format.Parse, which sees the underlying command output unmodified
// by the wrapper. Implementations must be safe for concurrent use:
// the registry exposes Stripper values to multiple goroutines, and a
// future feature may run multiple Wrap calls in parallel across
// different inputs.
//
// A reference implementation is the Noop stripper in this package;
// real implementations land in internal/envelope/githubactions and
// internal/envelope/gitlabci (M13.3, M13.4).
type Stripper interface {
	// Name returns the stable, lowercase identifier used on the
	// CLI (e.g. "github-actions", "gitlab-ci"). Must be unique
	// across all registered strippers — duplicate Register calls
	// panic at init time. Returned value must be constant for the
	// lifetime of the Stripper value.
	Name() string

	// Detect inspects the opening sample of input (typically the
	// first SampleSize bytes) and returns a self-reported
	// confidence in [0.0, 1.0] that the input is wrapped in this
	// envelope.
	//
	// Implementations may not modify the sample slice and may not
	// retain it beyond the call. Implementations must be cheap:
	// detection runs against every registered stripper on every
	// input, so anything beyond a regex match or a few field probes
	// is suspect.
	//
	// Sample may be shorter than SampleSize on small inputs;
	// implementations must handle empty and truncated samples
	// without panicking.
	Detect(sample []byte) event.Confidence

	// Strip consumes r and returns a cleaned Reader yielding the
	// input bytes with envelope metadata removed, plus a channel
	// of envelope-level signal Events the stripper synthesises.
	//
	// Lifecycle and contract:
	//
	//   - The cleaned Reader must produce output incrementally as
	//     r is consumed. No full-input buffering. Strip is the
	//     first stage in the pipeline; if it buffers, every
	//     downstream streaming guarantee breaks.
	//   - The signals channel is closed exactly once when r reaches
	//     EOF, when ctx is cancelled, or when the stripper finishes
	//     its work for any other reason. After close, callers may
	//     continue to drain in-flight signals from the channel.
	//   - Strip must not block indefinitely. If ctx is cancelled,
	//     the cleaned Reader must return io.EOF (or an error
	//     wrapping ctx.Err()) on the next Read, and the signals
	//     channel must close promptly.
	//   - Implementations may not retain r after Strip returns
	//     beyond what's needed to drive the cleaned Reader.
	//
	// The returned err is what Strip encountered before it started
	// producing output; nil on the happy path. Mid-stream errors do
	// not surface here; they degrade to a best-effort signal Event
	// with Severity=SeverityError per the same rule the format
	// parsers follow (see KNOWN_ISSUES.md § 2).
	Strip(ctx context.Context, r io.Reader) (cleaned io.Reader, signals <-chan event.Event, err error)
}

// Options tunes a Wrap call. The CLI maps `--strip-envelope=<value>`
// into the Choice field; library callers populate it directly.
type Options struct {
	// Choice is the user-facing envelope selection: ChoiceAuto
	// (default; run detection), ChoiceNone (force the Noop
	// stripper), or the Name() of a specific registered stripper.
	// Unknown names cause Wrap to return ErrUnknownStripper without
	// running detection.
	//
	// An empty Choice is treated as ChoiceAuto so the zero value of
	// Options is a sensible default.
	Choice string

	// Strippers overrides the registered Stripper set. When nil,
	// Wrap uses All(). Tests pass an explicit slice to isolate from
	// the global registry; production code leaves this nil.
	Strippers []Stripper
}

// Wrap inspects the first SampleSize bytes of r and returns a cleaned
// Reader plus a channel of envelope signal Events. The returned
// `chosen` is the Stripper that produced the cleaned Reader — Noop
// when Options.Choice is ChoiceNone, when no Stripper scored above
// ConfidenceMinDetect, or when no strippers are registered.
//
// Behaviour:
//
//   - Choice == ChoiceNone or "": when Choice is ChoiceNone the
//     Stripper is unconditionally Noop. The empty default is
//     equivalent to ChoiceAuto.
//   - Choice == ChoiceAuto: read up to SampleSize bytes, ask every
//     stripper from Options.Strippers (default: All()) to score the
//     sample, pick the highest-confidence stripper ≥
//     ConfidenceMinDetect; fall back to Noop otherwise. Detection
//     does not honour ctx because every Stripper.Detect must be
//     cheap; if a future implementation is not, the slowness is a
//     bug.
//   - Choice == <name>: look up the named stripper and use it
//     unconditionally. Unknown names return ErrUnknownStripper.
//
// The returned `cleaned` Reader yields the original input bytes
// processed by the chosen Stripper. For Noop and for ChoiceAuto with
// no match, the original input bytes pass through unchanged via an
// io.MultiReader of the sample buffer and the rest of r — Wrap never
// drops bytes.
//
// The returned `signals` channel is the chosen Stripper's signal
// channel. For Noop and Strippers that never emit signals, the
// channel closes immediately so a consuming goroutine doesn't leak.
//
// Wrap does not own r; the caller is responsible for closing r if r
// is an io.Closer. The cleaned Reader honours ctx via the chosen
// Stripper's implementation.
func Wrap(ctx context.Context, r io.Reader, opts Options) (cleaned io.Reader, signals <-chan event.Event, chosen Stripper, err error) {
	if r == nil {
		return nil, nil, nil, errors.New("envelope: nil Reader")
	}
	choice := opts.Choice
	if choice == "" {
		choice = ChoiceAuto
	}
	if choice == ChoiceNone {
		noop := Noop{}
		c, sigs, sErr := noop.Strip(ctx, r)
		if sErr != nil {
			return nil, nil, nil, fmt.Errorf("envelope: noop strip: %w", sErr)
		}
		return c, sigs, noop, nil
	}
	if choice != ChoiceAuto {
		candidates := opts.Strippers
		if candidates == nil {
			candidates = All()
		}
		for _, s := range candidates {
			if s.Name() == choice {
				c, sigs, sErr := s.Strip(ctx, r)
				if sErr != nil {
					return nil, nil, nil, fmt.Errorf("envelope: strip %q: %w", s.Name(), sErr)
				}
				return c, sigs, s, nil
			}
		}
		return nil, nil, nil, fmt.Errorf("%w: %q", ErrUnknownStripper, choice)
	}
	// Auto: sample, score, pick.
	sample, rest, sErr := readSample(r)
	if sErr != nil {
		return nil, nil, nil, fmt.Errorf("envelope: read sample: %w", sErr)
	}
	stream := prependSample(sample, rest)
	candidates := opts.Strippers
	if candidates == nil {
		candidates = All()
	}
	winner, score := pickStripper(candidates, sample)
	if winner == nil || score < ConfidenceMinDetect {
		noop := Noop{}
		c, sigs, nErr := noop.Strip(ctx, stream)
		if nErr != nil {
			return nil, nil, nil, fmt.Errorf("envelope: noop strip: %w", nErr)
		}
		return c, sigs, noop, nil
	}
	c, sigs, wErr := winner.Strip(ctx, stream)
	if wErr != nil {
		return nil, nil, nil, fmt.Errorf("envelope: strip %q: %w", winner.Name(), wErr)
	}
	return c, sigs, winner, nil
}

// Noop is the explicit "no envelope" Stripper. Its Strip returns the
// input Reader unchanged and an immediately-closed signals channel.
// Used by Wrap when Options.Choice is ChoiceNone, when no registered
// Stripper claims the sample, or when no strippers are registered at
// all.
//
// Name returns "none", which is also the public Choice value users
// pass on the CLI to force this stripper. Detect always returns 0.0
// so Noop never participates in auto-detection.
type Noop struct{}

// Name returns "none".
func (Noop) Name() string { return ChoiceNone }

// Detect always returns 0.0. Noop is the fallback target, not a
// detection candidate.
func (Noop) Detect(_ []byte) event.Confidence { return 0 }

// Strip returns r unchanged and an immediately-closed signals
// channel. ctx is honoured by the caller's read of r, not by Noop
// itself — Noop has no internal goroutine.
func (Noop) Strip(_ context.Context, r io.Reader) (io.Reader, <-chan event.Event, error) {
	ch := make(chan event.Event)
	close(ch)
	return r, ch, nil
}

// pickStripper scores every candidate stripper against the sample and
// returns the winner plus its score. Ties are broken alphabetically by
// Name so the choice is deterministic across runs.
func pickStripper(strippers []Stripper, sample []byte) (Stripper, event.Confidence) {
	if len(strippers) == 0 {
		return nil, 0
	}
	var (
		winner    Stripper
		bestScore event.Confidence
	)
	for _, s := range strippers {
		score := s.Detect(sample)
		switch {
		case winner == nil:
			winner, bestScore = s, score
		case score > bestScore:
			winner, bestScore = s, score
		case score == bestScore && s.Name() < winner.Name():
			winner = s
		}
	}
	return winner, bestScore
}

// readSample reads up to SampleSize bytes from r and returns them
// along with the remainder of r. Short input is handled the same way
// detect.readSample handles it: return what we read and an
// emptyReader so prependSample produces a consistent shape.
func readSample(r io.Reader) ([]byte, io.Reader, error) {
	buf := make([]byte, SampleSize)
	n, err := io.ReadFull(r, buf)
	switch {
	case err == nil:
		return buf[:n], r, nil
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return buf[:n], emptyReader{}, nil
	default:
		return nil, nil, err
	}
}

type emptyReader struct{}

func (emptyReader) Read(_ []byte) (int, error) { return 0, io.EOF }

// prependSample returns a Reader that yields sample's bytes first,
// then rest's bytes. Mirrors detect.prependSample so the two
// detectors compose without surprises.
func prependSample(sample []byte, rest io.Reader) io.Reader {
	if len(sample) == 0 {
		return rest
	}
	return io.MultiReader(bytes.NewReader(sample), rest)
}
