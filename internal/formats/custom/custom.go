// Package custom implements regex-driven Formats registered at
// process start from `[[formats.custom.NAME]]` blocks in the user's
// configuration. Each block becomes a Format instance whose
// Name() returns "custom:NAME", participating in the registry
// alongside built-in formats but distinguished by the namespace
// prefix so it can never collide with a built-in.
//
// Unlike built-in formats which are value types registered via
// init(), custom formats are instances constructed at runtime
// after config.LoadAll resolves the merged configuration. The CLI
// calls RegisterFromConfig from cmd/distill-ai's root pre-run
// hook after loading the config; failures (a bad regex, a missing
// required field) fail the binary at startup before stdin is
// read, so the user sees the error rather than a silent fallback
// to autodetect.
//
// The detector's M3 tie-breaker rule applies: when both a custom
// Format and a built-in Format report Confidence 1.0 on the same
// sample, the detector's alphabetical Name sort breaks the tie.
// "custom:myapp" sorts at "c", so it loses ties against built-ins
// whose names start with "a" or "b" (none today) and wins against
// "generic", "gotest", "jest", and "pytest" only when those score
// strictly below 1.0. Users who want their custom format to take
// precedence in all cases pass the explicit positional FORMAT
// argument (`distill-ai run custom:myapp ...`).
package custom

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
)

// NamePrefix is the namespace every custom-format Name() carries.
// Reserved so a config writer can't collide with a built-in name
// (the built-in "pytest" stays unambiguous even if a user defines
// `[[formats.custom.pytest]]` — the custom one is "custom:pytest").
const NamePrefix = "custom:"

// defaultSeverity is the severity used when a [[formats.custom.NAME]]
// block leaves the `severity` field empty. The choice matches the
// generic format's default — every other format treats unset
// severity as SeverityError.
const defaultSeverity = event.SeverityError

// defaultKind is the Kind string used when a [[formats.custom.NAME]]
// block leaves the `kind` field empty. "match" is intentionally
// generic — the user picks a kind only when they want a more
// descriptive label.
const defaultKind = "match"

// ansiEscape strips ANSI SGR escape sequences from the Title of
// every emitted Event so the title is grep-able. Same regex
// the generic format uses; lifted here so the two packages stay
// independent.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// Format is a runtime-configured implementation of formats.Format.
// Each [[formats.custom.NAME]] block becomes one Format instance;
// instances are independent and may be registered in any order.
//
// Field values are derived from CustomFormatConfig at construction
// time. The constructor compiles the regex strings once and
// returns an error for compilation failures; the Parse loop then
// runs against the precompiled regexes with no per-line
// compilation cost.
type Format struct {
	name       string
	detectRe   *regexp.Regexp
	eventStart *regexp.Regexp
	eventEnd   *regexp.Regexp // optional; nil ⇒ one-line Events
	severity   event.Severity
	kind       string
	// trimmedName carries the user's name without the
	// "custom:" prefix, used for Metadata["custom_format"].
	trimmedName string
}

// Name returns "custom:NAME" where NAME is the original
// configuration key. The prefix guarantees no collision with a
// built-in format's name and lets downstream consumers route on
// the prefix to distinguish custom formats from first-party
// formats.
func (f *Format) Name() string { return f.name }

// Detect reports Confidence(1.0) when the sample contains at
// least one line matching the configured detect regex; otherwise
// Confidence(0.0). Custom formats deliberately have only two
// confidence values — the user's regex is the authority on
// matching, so there's no nuance for the detector to add.
func (f *Format) Detect(sample []byte) event.Confidence {
	if f.detectRe == nil {
		return 0
	}
	if f.detectRe.Match(sample) {
		return 1.0
	}
	return 0
}

// Parse scans r line-by-line. A line matching the configured
// event_start regex opens an Event; subsequent lines append to
// Body until either:
//
//   - A line matches event_end (when configured). The end line is
//     appended to Body and the Event is emitted.
//   - Another event_start arrives. The first Event is emitted
//     (closed implicitly), the new Event opens.
//   - The reader closes. The in-flight Event (if any) is
//     emitted before the channel closes.
//
// When event_end is nil, each event_start match becomes a
// one-line Event — the line is the entire Body.
//
// Per-Event shape:
//
//   - Severity: the configured severity (default error).
//   - Kind: the configured kind (default "match").
//   - Title: the matched start line, ANSI-stripped.
//   - Body: the verbatim block lines (no ANSI strip; the user
//     sees what arrived).
//   - Metadata["custom_format"]: the configured name without the
//     "custom:" prefix, so consumers can route on the user's name.
//
// Streaming: each Event is forwarded as soon as its terminator is
// seen. Backpressure propagates via the unbuffered channel — a
// slow downstream stage blocks the parser at send time.
func (f *Format) Parse(ctx context.Context, r io.Reader, _ formats.ParseOpts) (<-chan event.Event, error) {
	if f.eventStart == nil {
		return nil, errors.New("custom: event_start is required (Format constructed without compilation)")
	}
	out := make(chan event.Event)
	go f.run(ctx, r, out)
	return out, nil
}

// run is the body of the Parse goroutine. Single-goroutine
// ownership keeps the line-scan loop straightforward; ctx checks
// happen at every send so cancellation propagates within one
// line's worth of work.
func (f *Format) run(ctx context.Context, r io.Reader, out chan<- event.Event) {
	defer close(out)
	scanner := bufio.NewScanner(r)
	// Allow long lines (some app logs print 200KB JSON on a single
	// line). The default 64KB token limit is too small for log
	// shapes we see in the wild; 1MB matches the generic scanner.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var current *event.Event
	for scanner.Scan() {
		line := scanner.Text()
		// event_start takes precedence over event_end when a
		// line matches both: a new start always opens a fresh
		// Event, closing any in-flight one. Documented in the
		// package godoc.
		if f.eventStart.MatchString(line) {
			if current != nil {
				if !send(ctx, out, *current) {
					return
				}
			}
			current = f.newEvent(line)
			// With no event_end regex, each start line is
			// itself the entire Body — emit immediately.
			if f.eventEnd == nil {
				if !send(ctx, out, *current) {
					return
				}
				current = nil
			}
			continue
		}
		if current != nil && f.eventEnd != nil && f.eventEnd.MatchString(line) {
			// Append the terminator line to Body, emit, reset.
			current.Body = append(current.Body, line)
			if !send(ctx, out, *current) {
				return
			}
			current = nil
			continue
		}
		if current != nil {
			current.Body = append(current.Body, line)
		}
	}
	// Flush any in-flight Event.
	if current != nil {
		_ = send(ctx, out, *current)
	}
}

// newEvent constructs an Event for a freshly-matched start line.
// Body is initialised with the matched line so multi-line blocks
// preserve the start line in the Body.
func (f *Format) newEvent(line string) *event.Event {
	title := ansiEscape.ReplaceAllString(line, "")
	return &event.Event{
		Severity: f.severity,
		Kind:     f.kind,
		Title:    title,
		Body:     []string{line},
		Metadata: map[string]string{"custom_format": f.trimmedName},
	}
}

// send dispatches one Event downstream, respecting ctx
// cancellation. Returns false when the caller should stop.
func send(ctx context.Context, out chan<- event.Event, ev event.Event) bool {
	select {
	case <-ctx.Done():
		return false
	case out <- ev:
		return true
	}
}

// New constructs a Format from a configuration block. Returns an
// error naming the offending field when a regex fails to compile
// or a required field is missing.
//
// name is the human-chosen key from `[[formats.custom.NAME]]`; the
// returned Format's Name() prepends NamePrefix. detectRegex and
// eventStart are required (matching the M14.1 schema). eventEnd is
// optional; an empty string yields one-line Events. severity may
// be empty (defaults to error) or any value event.ParseSeverity
// accepts. kind may be empty (defaults to "match").
//
// The function does NOT register the Format with the global
// registry; the caller (RegisterFromConfig) does that after
// validating all blocks. Splitting construction from registration
// lets the caller fail atomically: if block #3 fails, blocks #1
// and #2 are not partially registered.
func New(name, detectRegex, eventStart, eventEnd, severity, kind string) (*Format, error) {
	if detectRegex == "" {
		return nil, fmt.Errorf("formats.custom.%s: detect_regex is required", name)
	}
	if eventStart == "" {
		return nil, fmt.Errorf("formats.custom.%s: event_start is required", name)
	}
	dre, err := regexp.Compile(detectRegex)
	if err != nil {
		return nil, fmt.Errorf("formats.custom.%s.detect_regex: %w", name, err)
	}
	sre, err := regexp.Compile(eventStart)
	if err != nil {
		return nil, fmt.Errorf("formats.custom.%s.event_start: %w", name, err)
	}
	var ere *regexp.Regexp
	if eventEnd != "" {
		ere, err = regexp.Compile(eventEnd)
		if err != nil {
			return nil, fmt.Errorf("formats.custom.%s.event_end: %w", name, err)
		}
	}
	sev := defaultSeverity
	if severity != "" {
		parsed, perr := event.ParseSeverity(severity)
		if perr != nil {
			return nil, fmt.Errorf("formats.custom.%s.severity: %w", name, perr)
		}
		sev = parsed
	}
	k := kind
	if k == "" {
		k = defaultKind
	}
	return &Format{
		name:        NamePrefix + name,
		detectRe:    dre,
		eventStart:  sre,
		eventEnd:    ere,
		severity:    sev,
		kind:        k,
		trimmedName: name,
	}, nil
}

// Config is the contract RegisterFromConfig reads. Mirrors the
// fields of internal/config.CustomFormatConfig but is re-declared
// here so this package doesn't import internal/config (a
// back-edge in the dependency graph). The CLI's wiring code
// translates between the two structs at the call site.
type Config struct {
	DetectRegex string
	EventStart  string
	EventEnd    string
	Severity    string
	Kind        string
}

// RegisterFromConfig compiles each block's regexes, constructs a
// Format per block, and registers each with the global formats
// registry. Returns an error on any compilation failure; on
// success, every block is registered and the function returns nil.
//
// The function validates ALL blocks first and only registers them
// once every block is verified. A single bad block fails the
// whole call without registering any of them — important because
// a partial registration would leave the binary in a state where
// some user-configured formats work and others don't, and the
// user would have no easy way to find which ones.
//
// Iteration order is deterministic (alphabetical by name) so the
// error from a malformed config is reproducible.
func RegisterFromConfig(blocks map[string]Config) error {
	if len(blocks) == 0 {
		return nil
	}
	names := make([]string, 0, len(blocks))
	for name := range blocks {
		names = append(names, name)
	}
	sort.Strings(names)
	compiled := make([]*Format, 0, len(blocks))
	for _, name := range names {
		c := blocks[name]
		f, err := New(name, c.DetectRegex, c.EventStart, c.EventEnd, c.Severity, c.Kind)
		if err != nil {
			return err
		}
		compiled = append(compiled, f)
	}
	for _, f := range compiled {
		formats.Register(f)
	}
	return nil
}
