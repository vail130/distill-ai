package envelope

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
)

// updatingGoldens mirrors internal/formats.updatingGoldens: golden
// fixtures regenerate when DISTILL_AI_UPDATE_GOLDENS=1 is set. Reuses
// the same env var so contributors learn one switch.
func updatingGoldens() bool { return os.Getenv("DISTILL_AI_UPDATE_GOLDENS") == "1" }

// GoldenCase is the JSON shape stored in *.expected files. Signals
// and ParserEvents are kept separate so the diff makes it obvious
// which side of the fan-in produced which Event. The harness
// drains signals fully before running the inner parser, so the
// order within each list is deterministic even though the real
// pipeline's fan-in is not.
type GoldenCase struct {
	// Stripper is the Name() of the envelope.Stripper that won
	// detection. "none" when no stripper matched.
	Stripper string `json:"stripper"`

	// Format is the formats.Format.Name() chosen by detect on the
	// cleaned bytes. Useful so reviewers can see the round-trip
	// at a glance.
	Format string `json:"format"`

	// Signals is the list of envelope-level Events produced by
	// the chosen Stripper, in source order.
	Signals []event.Event `json:"signals"`

	// ParserEvents is the list of Events the inner Format emitted
	// from the cleaned stream, in source order.
	ParserEvents []event.Event `json:"parser_events"`
}

// RunGoldens walks dir for *.input fixtures, runs each through
// envelope.Wrap + the inner format's Parse, and diffs the result
// against *.expected. Mirrors internal/formats.RunGoldens but
// understands the two-stream fan-in.
//
// The harness assumes the inner format is auto-detected. Fixtures
// whose inner format would be `generic` work too; the harness uses
// detect.Detect, which falls back to generic when no specific
// format claims the cleaned bytes.
//
// Live in a regular .go file (not _test.go) so test packages
// outside internal/envelope (e.g. integration tests) can call it.
func RunGoldens(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("RunGoldens: read %s: %v", dir, err)
	}
	cases := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".input") {
			cases = append(cases, strings.TrimSuffix(name, ".input"))
		}
	}
	sort.Strings(cases)
	if len(cases) == 0 {
		t.Fatalf("RunGoldens: no *.input fixtures found under %s", dir)
	}
	for _, name := range cases {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Helper()
			runOneGolden(t, dir, name)
		})
	}
}

func runOneGolden(t *testing.T, dir, name string) {
	t.Helper()
	inputPath := filepath.Join(dir, name+".input")
	expectedPath := filepath.Join(dir, name+".expected")
	input, err := os.ReadFile(inputPath) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("read %s: %v", inputPath, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := runFixture(ctx, input)
	if err != nil {
		t.Fatalf("runFixture: %v", err)
	}
	actual, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	actual = append(actual, '\n')
	if updatingGoldens() {
		if err := os.WriteFile(expectedPath, actual, 0o644); err != nil { //nolint:gosec // test path
			t.Fatalf("write %s: %v", expectedPath, err)
		}
		t.Logf("updated %s", expectedPath)
		return
	}
	expected, err := os.ReadFile(expectedPath) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("read %s (run with DISTILL_AI_UPDATE_GOLDENS=1 to create): %v", expectedPath, err)
	}
	if !bytes.Equal(actual, expected) {
		t.Errorf("%s diverged from golden\n--- expected\n%s\n--- got\n%s", name, expected, actual)
	}
}

// runFixture is the workhorse: envelope.Wrap, drain signals into a
// slice, hand the cleaned bytes to detect.Detect, parse with the
// chosen format, drain events into a slice. Returns the assembled
// GoldenCase.
//
// Signals are drained synchronously, fully, before the inner
// parser starts. This is acceptable for fixture testing — the
// production pipeline drives both concurrently via the fan-in
// Source in pipeline.Build — and it makes golden output stable
// regardless of host scheduling.
func runFixture(ctx context.Context, input []byte) (GoldenCase, error) {
	cleaned, sigCh, stripper, err := Wrap(ctx, bytes.NewReader(input), Options{Choice: ChoiceAuto})
	if err != nil {
		return GoldenCase{}, fmt.Errorf("Wrap: %w", err)
	}
	// Buffer the cleaned stream up-front so signal draining can
	// proceed without blocking the strip goroutine on a slow
	// downstream Reader. 64 KiB is enough for every fixture in
	// the test suite; longer fixtures would need a sliding buffer
	// but none of the v1 cases approach that limit.
	cleanedBuf, err := io.ReadAll(cleaned)
	if err != nil {
		return GoldenCase{}, fmt.Errorf("read cleaned: %w", err)
	}
	signals := drainSignals(sigCh)
	chosenFormat, parserEvents, err := parseInner(ctx, cleanedBuf)
	if err != nil {
		return GoldenCase{}, err
	}
	stripperName := ChoiceNone
	if stripper != nil {
		stripperName = stripper.Name()
	}
	return GoldenCase{
		Stripper:     stripperName,
		Format:       chosenFormat,
		Signals:      signals,
		ParserEvents: parserEvents,
	}, nil
}

func drainSignals(ch <-chan event.Event) []event.Event {
	out := []event.Event{}
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

// parseInner is a thin wrapper around detect.Detect + Format.Parse
// for the cleaned bytes. Defined inline here (rather than reaching
// into internal/detect from a goldens-only path) to keep
// internal/envelope free of test-only imports.
//
// The function is intentionally local: import cycle paranoia would
// prevent internal/envelope from depending on internal/detect or
// the format packages even for test-only code paths. We do the
// detection ourselves by calling formats.All() and picking the
// highest-scoring Format, mirroring detect.Detect's algorithm.
func parseInner(ctx context.Context, cleaned []byte) (string, []event.Event, error) {
	chosen := pickFormat(cleaned)
	if chosen == nil {
		return "", []event.Event{}, nil
	}
	ch, err := chosen.Parse(ctx, bytes.NewReader(cleaned), formats.ParseOpts{})
	if err != nil {
		return "", nil, fmt.Errorf("parse: %w", err)
	}
	out := []event.Event{}
	for ev := range ch {
		out = append(out, ev)
	}
	return chosen.Name(), out, nil
}

// pickFormat returns the registered Format with the highest
// Detect() score, or nil when no Format claims the input. Mirrors
// detect.Detect's "highest specific format wins, generic loses
// ties" rule without importing internal/detect (which would
// create a test-only cycle through internal/formats).
func pickFormat(sample []byte) formats.Format {
	all := formats.All()
	if len(all) == 0 {
		return nil
	}
	const genericName = "generic"
	var (
		bestSpecific      formats.Format
		bestSpecificScore event.Confidence
		generic           formats.Format
	)
	for _, f := range all {
		if f.Name() == genericName {
			generic = f
			continue
		}
		score := f.Detect(sample)
		switch {
		case bestSpecific == nil:
			bestSpecific, bestSpecificScore = f, score
		case score > bestSpecificScore:
			bestSpecific, bestSpecificScore = f, score
		case score == bestSpecificScore && f.Name() < bestSpecific.Name():
			bestSpecific = f
		}
	}
	if bestSpecific != nil && bestSpecificScore >= event.ConfidenceMinDetect {
		return bestSpecific
	}
	return generic
}

// FixtureCount mirrors formats.FixtureCount: asserts a directory
// contains exactly the expected number of *.input fixtures so
// future drift surfaces immediately.
func FixtureCount(t *testing.T, dir string, want int) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("FixtureCount: read %s: %v", dir, err)
	}
	got := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".input") {
			got++
		}
	}
	if got != want {
		t.Fatalf("FixtureCount: %s has %d *.input fixtures; want %d", dir, got, want)
	}
}
