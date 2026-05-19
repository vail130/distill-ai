package envelope_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/envelope"
	"github.com/vail130/distill-ai/internal/event"
)

// stubStripper is a minimal Stripper used by registry and Wrap tests.
// Strip returns r unchanged plus an immediately-closed signals
// channel so the tests don't have to manage goroutine lifecycles.
type stubStripper struct {
	name  string
	score event.Confidence
}

func (s stubStripper) Name() string                     { return s.name }
func (s stubStripper) Detect(_ []byte) event.Confidence { return s.score }
func (s stubStripper) Strip(_ context.Context, r io.Reader) (io.Reader, <-chan event.Event, error) {
	ch := make(chan event.Event)
	close(ch)
	return r, ch, nil
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	envelope.Register(stubStripper{name: "alpha"})
	got, ok := envelope.Get("alpha")
	if !ok {
		t.Fatal(`Get("alpha") not found after Register`)
	}
	if got.Name() != "alpha" {
		t.Errorf("Get returned stripper with Name=%q, want %q", got.Name(), "alpha")
	}
}

func TestRegistry_GetMissingReturnsFalse(t *testing.T) {
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	if _, ok := envelope.Get("nope"); ok {
		t.Error("Get on empty registry returned ok=true")
	}
}

func TestRegistry_DuplicateRegisterPanics(t *testing.T) {
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	envelope.Register(stubStripper{name: "dup"})
	defer func() {
		if r := recover(); r == nil {
			t.Error("second Register with same name did not panic")
		}
	}()
	envelope.Register(stubStripper{name: "dup"})
}

func TestRegistry_NilRegisterPanics(t *testing.T) {
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	defer func() {
		if r := recover(); r == nil {
			t.Error("Register(nil) did not panic")
		}
	}()
	envelope.Register(nil)
}

func TestRegistry_EmptyNameRegisterPanics(t *testing.T) {
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	defer func() {
		if r := recover(); r == nil {
			t.Error("Register of stripper with empty Name() did not panic")
		}
	}()
	envelope.Register(stubStripper{name: ""})
}

// TestRegistry_ReservedNoneNamePanics protects ChoiceNone from being
// shadowed by a real stripper. Without the guard, --strip-envelope=none
// would be ambiguous (Noop or the impostor?).
func TestRegistry_ReservedNoneNamePanics(t *testing.T) {
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	defer func() {
		if r := recover(); r == nil {
			t.Error("Register with reserved name \"none\" did not panic")
		}
	}()
	envelope.Register(stubStripper{name: envelope.ChoiceNone})
}

func TestRegistry_AllIsSorted(t *testing.T) {
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	for _, n := range []string{"gamma", "alpha", "beta"} {
		envelope.Register(stubStripper{name: n})
	}
	all := envelope.All()
	if len(all) != 3 {
		t.Fatalf("All() returned %d strippers; want 3", len(all))
	}
	want := []string{"alpha", "beta", "gamma"}
	for i, w := range want {
		if all[i].Name() != w {
			t.Errorf("All()[%d].Name() = %q, want %q", i, all[i].Name(), w)
		}
	}
}

func TestRegistry_AllIsSnapshot(t *testing.T) {
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	envelope.Register(stubStripper{name: "alpha"})
	snapshot := envelope.All()
	snapshot[0] = stubStripper{name: "MUTATED"}
	got, _ := envelope.Get("alpha")
	if got.Name() != "alpha" {
		t.Errorf("mutating All() slice affected registry: Get(alpha).Name() = %q", got.Name())
	}
}

// TestRegistry_ConcurrentAccess validates concurrent Get/All against
// the race detector and against deadlock.
func TestRegistry_ConcurrentAccess(t *testing.T) {
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	for _, n := range []string{"a", "b", "c", "d"} {
		envelope.Register(stubStripper{name: n})
	}
	const goroutines = 32
	const iterations = 200
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				envelope.All()
				_, _ = envelope.Get("a")
				_, _ = envelope.Get("nope")
			}
		}()
	}
	wg.Wait()
}

func TestNoop_NameAndDetect(t *testing.T) {
	var n envelope.Noop
	if got := n.Name(); got != envelope.ChoiceNone {
		t.Errorf("Noop.Name() = %q, want %q", got, envelope.ChoiceNone)
	}
	if got := n.Detect([]byte("anything")); got != 0 {
		t.Errorf("Noop.Detect = %v, want 0 (Noop must never participate in auto-detection)", got)
	}
}

func TestNoop_StripPassesThroughBytes(t *testing.T) {
	const payload = "hello, world\n"
	cleaned, signals, err := envelope.Noop{}.Strip(context.Background(), strings.NewReader(payload))
	if err != nil {
		t.Fatalf("Noop.Strip: unexpected error %v", err)
	}
	got, err := io.ReadAll(cleaned)
	if err != nil {
		t.Fatalf("read cleaned: %v", err)
	}
	if string(got) != payload {
		t.Errorf("cleaned output = %q, want %q", got, payload)
	}
	// Signals must be closed before we drain.
	drainSignalsExpectClosed(t, signals)
}

func TestNoop_SignalsChannelClosesImmediately(t *testing.T) {
	_, signals, err := envelope.Noop{}.Strip(context.Background(), strings.NewReader(""))
	if err != nil {
		t.Fatalf("Noop.Strip: %v", err)
	}
	select {
	case _, ok := <-signals:
		if ok {
			t.Error("Noop.Strip signals channel produced a value; want immediate close")
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Noop.Strip signals channel did not close within 50ms")
	}
}

func TestWrap_NilReaderErrors(t *testing.T) {
	_, _, _, err := envelope.Wrap(context.Background(), nil, envelope.Options{})
	if err == nil {
		t.Fatal("Wrap with nil Reader returned nil error")
	}
}

func TestWrap_NoneForcesNoop(t *testing.T) {
	// Register a stripper that would claim the sample at 1.0 — but
	// Wrap must still pick Noop because Choice=ChoiceNone.
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	envelope.Register(stubStripper{name: "greedy", score: 1.0})
	const payload = "wrapped or not, this should pass through"
	cleaned, signals, chosen, err := envelope.Wrap(
		context.Background(),
		strings.NewReader(payload),
		envelope.Options{Choice: envelope.ChoiceNone},
	)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if chosen == nil || chosen.Name() != envelope.ChoiceNone {
		t.Fatalf("chosen.Name() = %q, want %q (Noop)", chosen.Name(), envelope.ChoiceNone)
	}
	got, _ := io.ReadAll(cleaned)
	if string(got) != payload {
		t.Errorf("cleaned bytes = %q, want %q", got, payload)
	}
	drainSignalsExpectClosed(t, signals)
}

func TestWrap_EmptyChoiceTreatedAsAuto(t *testing.T) {
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	envelope.Register(stubStripper{name: "claims-it", score: 1.0})
	_, _, chosen, err := envelope.Wrap(
		context.Background(),
		strings.NewReader("input"),
		envelope.Options{}, // empty Choice → auto.
	)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if chosen == nil || chosen.Name() != "claims-it" {
		t.Fatalf("chosen.Name() = %v, want \"claims-it\" (empty Choice should auto-detect)", nameOrNil(chosen))
	}
}

func TestWrap_AutoChoosesHighestConfidence(t *testing.T) {
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	// Use a non-matching stub (score 0) for the second candidate so
	// only the high-confidence one is picked. With chaining,
	// registering two matching stubs would apply both — the
	// dedicated TestWrap_AutoChainsMultipleStrippers below covers
	// that path. This test isolates the highest-of-many rule.
	envelope.Register(stubStripper{name: "low", score: 0})
	envelope.Register(stubStripper{name: "high", score: 0.95})
	_, _, chosen, err := envelope.Wrap(
		context.Background(),
		strings.NewReader("sample"),
		envelope.Options{Choice: envelope.ChoiceAuto},
	)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if chosen == nil || chosen.Name() != "high" {
		t.Fatalf("chosen.Name() = %v, want \"high\"", nameOrNil(chosen))
	}
}

// TestWrap_AutoTieBreaksAlphabetically asserts the deterministic
// tie-break rule documented in Wrap's godoc. Both stubs match at the
// same score; chaining applies them in alphabetical order so the
// chain Name() begins with the alphabetically-first stripper.
func TestWrap_AutoTieBreaksAlphabetically(t *testing.T) {
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	envelope.Register(stubStripper{name: "zulu", score: 0.8})
	envelope.Register(stubStripper{name: "alpha", score: 0.8})
	_, _, chosen, err := envelope.Wrap(
		context.Background(),
		strings.NewReader("sample"),
		envelope.Options{Choice: envelope.ChoiceAuto},
	)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	// Both stubs always match (Detect returns score regardless of
	// input), so chaining applies them in alphabetical-tie order.
	// Assert the first-applied stripper, which is what the
	// tie-break rule promises.
	want := "alpha+zulu"
	if chosen == nil || chosen.Name() != want {
		t.Fatalf("chosen.Name() = %v, want %q (alphabetical tie-break on first chain step)", nameOrNil(chosen), want)
	}
}

func TestWrap_AutoBelowThresholdFallsBackToNoop(t *testing.T) {
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	envelope.Register(stubStripper{name: "weak", score: 0.4}) // < 0.6
	_, _, chosen, err := envelope.Wrap(
		context.Background(),
		strings.NewReader("sample"),
		envelope.Options{Choice: envelope.ChoiceAuto},
	)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if chosen == nil || chosen.Name() != envelope.ChoiceNone {
		t.Fatalf("chosen.Name() = %v, want %q (Noop)", nameOrNil(chosen), envelope.ChoiceNone)
	}
}

func TestWrap_AutoNoStrippersRegisteredFallsBackToNoop(t *testing.T) {
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	_, _, chosen, err := envelope.Wrap(
		context.Background(),
		strings.NewReader("sample"),
		envelope.Options{Choice: envelope.ChoiceAuto},
	)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if chosen == nil || chosen.Name() != envelope.ChoiceNone {
		t.Fatalf("chosen.Name() = %v, want %q (Noop)", nameOrNil(chosen), envelope.ChoiceNone)
	}
}

// TestWrap_AutoPreservesAllBytesOnFallback proves Wrap never drops
// bytes: when no stripper wins and Noop takes over, the sample
// buffer plus the trailing reader produce the exact original input.
func TestWrap_AutoPreservesAllBytesOnFallback(t *testing.T) {
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	// No registered strippers → auto falls through to Noop. The
	// payload deliberately exceeds SampleSize so we exercise both
	// the buffered sample bytes and the trailing reader's bytes.
	payload := strings.Repeat("abc\n", (envelope.SampleSize/4)+10)
	cleaned, _, chosen, err := envelope.Wrap(
		context.Background(),
		strings.NewReader(payload),
		envelope.Options{Choice: envelope.ChoiceAuto},
	)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if chosen == nil || chosen.Name() != envelope.ChoiceNone {
		t.Fatalf("expected fallback to Noop, got %v", nameOrNil(chosen))
	}
	got, err := io.ReadAll(cleaned)
	if err != nil {
		t.Fatalf("read cleaned: %v", err)
	}
	if string(got) != payload {
		t.Errorf("cleaned bytes lost data: got %d bytes, want %d", len(got), len(payload))
	}
}

func TestWrap_ExplicitNameSelectsStripper(t *testing.T) {
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	// Register two strippers; explicit Choice picks the named one
	// regardless of confidence.
	envelope.Register(stubStripper{name: "first", score: 0.99})
	envelope.Register(stubStripper{name: "second", score: 0.0})
	_, _, chosen, err := envelope.Wrap(
		context.Background(),
		strings.NewReader("sample"),
		envelope.Options{Choice: "second"},
	)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if chosen == nil || chosen.Name() != "second" {
		t.Fatalf("chosen.Name() = %v, want \"second\"", nameOrNil(chosen))
	}
}

func TestWrap_UnknownChoiceReturnsErrUnknownStripper(t *testing.T) {
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	envelope.Register(stubStripper{name: "real", score: 1.0})
	_, _, _, err := envelope.Wrap(
		context.Background(),
		strings.NewReader("sample"),
		envelope.Options{Choice: "imaginary"},
	)
	if err == nil {
		t.Fatal("Wrap with unknown Choice returned nil error")
	}
	if !errors.Is(err, envelope.ErrUnknownStripper) {
		t.Errorf("error %v does not wrap ErrUnknownStripper", err)
	}
}

// TestWrap_OptionsStrippersOverridesRegistry isolates Wrap from the
// global registry for tests that need a deterministic candidate set.
func TestWrap_OptionsStrippersOverridesRegistry(t *testing.T) {
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	// Register a misleading stripper globally; Options.Strippers
	// overrides it.
	envelope.Register(stubStripper{name: "global", score: 1.0})
	_, _, chosen, err := envelope.Wrap(
		context.Background(),
		strings.NewReader("sample"),
		envelope.Options{
			Choice:    envelope.ChoiceAuto,
			Strippers: []envelope.Stripper{stubStripper{name: "local", score: 0.9}},
		},
	)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if chosen == nil || chosen.Name() != "local" {
		t.Fatalf("chosen.Name() = %v, want \"local\" (Options.Strippers should override global)", nameOrNil(chosen))
	}
}

// TestWrap_AutoChainsMultipleStrippers exercises the v1.0 chaining
// path that closes KNOWN_ISSUES.md issue #2: a sample that two
// strippers each claim above the detection floor must trigger both
// in order, with the second running against the first's cleaned
// output. The synthetic stripper here uses a "consume once" Detect
// so the second iteration only picks it if the first has finished.
func TestWrap_AutoChainsMultipleStrippers(t *testing.T) {
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	// outer always claims; inner is a chainStub that matches the
	// outer's cleaned-output marker. After outer runs the cleaned
	// stream begins with "INNER:" so inner.Detect returns 1.0; on
	// the first sample (raw "OUTER:..." input) inner would not
	// match.
	envelope.Register(matchPrefixStripper{name: "outer", prefix: "OUTER:", rewriteTo: "INNER:"})
	envelope.Register(matchPrefixStripper{name: "inner", prefix: "INNER:", rewriteTo: ""})
	cleaned, signals, chosen, err := envelope.Wrap(
		context.Background(),
		strings.NewReader("OUTER:body line\n"),
		envelope.Options{Choice: envelope.ChoiceAuto},
	)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if chosen == nil || chosen.Name() != "outer+inner" {
		t.Fatalf("chosen.Name() = %v, want \"outer+inner\"", nameOrNil(chosen))
	}
	got, err := io.ReadAll(cleaned)
	if err != nil {
		t.Fatalf("read cleaned: %v", err)
	}
	if string(got) != "body line\n" {
		t.Errorf("cleaned = %q, want %q (both strippers applied)", got, "body line\n")
	}
	drainSignalsExpectClosed(t, signals)
	// envelope.Chain exposes the constituent strippers.
	links := envelope.Chain(chosen)
	if len(links) != 2 || links[0].Name() != "outer" || links[1].Name() != "inner" {
		t.Errorf("Chain(chosen) = %v, want [outer inner]", chainNames(links))
	}
}

// TestWrap_AutoChainStopsWhenNoCandidateClaims verifies the chain
// terminates as soon as no remaining stripper scores ≥
// ConfidenceMinDetect. Without that guard a single-match input
// would still incur a full MaxChainDepth-iteration penalty.
func TestWrap_AutoChainStopsWhenNoCandidateClaims(t *testing.T) {
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	envelope.Register(matchPrefixStripper{name: "outer", prefix: "OUTER:", rewriteTo: ""})
	envelope.Register(matchPrefixStripper{name: "inner", prefix: "WONT-MATCH:", rewriteTo: ""})
	_, _, chosen, err := envelope.Wrap(
		context.Background(),
		strings.NewReader("OUTER:body\n"),
		envelope.Options{Choice: envelope.ChoiceAuto},
	)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if chosen == nil || chosen.Name() != "outer" {
		t.Fatalf("chosen.Name() = %v, want \"outer\" (only one stripper claims)", nameOrNil(chosen))
	}
}

// TestWrap_AutoChainBoundedByMaxDepth pins the safety cap so an
// always-matching pair (or a future bug that lets the same stripper
// re-claim its own output) can't loop forever. With three
// always-matching strippers and MaxChainDepth=4, exactly three
// strippers should apply — MaxChainDepth bounds the loop but the
// `used` set prevents re-picking.
func TestWrap_AutoChainBoundedByMaxDepth(t *testing.T) {
	envelope.ResetForTest()
	t.Cleanup(envelope.ResetForTest)
	envelope.Register(stubStripper{name: "a", score: 1.0})
	envelope.Register(stubStripper{name: "b", score: 1.0})
	envelope.Register(stubStripper{name: "c", score: 1.0})
	_, _, chosen, err := envelope.Wrap(
		context.Background(),
		strings.NewReader("sample"),
		envelope.Options{Choice: envelope.ChoiceAuto},
	)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	links := envelope.Chain(chosen)
	if len(links) != 3 {
		t.Errorf("Chain length = %d, want 3 (all distinct stubs applied once)", len(links))
	}
}

// TestKindConstantsAreStable pins the string values so a careless
// rename surfaces as a build failure rather than silent schema drift.
func TestKindConstantsAreStable(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{envelope.KindEnvelopeError, "envelope_error"},
		{envelope.KindEnvelopeWarning, "envelope_warning"},
		{envelope.KindEnvelopeStepFailure, "envelope_step_failure"},
		{envelope.ChoiceAuto, "auto"},
		{envelope.ChoiceNone, "none"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("envelope constant = %q, want %q (schema drift)", c.got, c.want)
		}
	}
}

func drainSignalsExpectClosed(t *testing.T, ch <-chan event.Event) {
	t.Helper()
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-time.After(50 * time.Millisecond):
			t.Fatal("signals channel did not close within 50ms")
		}
	}
}

func nameOrNil(s envelope.Stripper) string {
	if s == nil {
		return "<nil>"
	}
	return s.Name()
}

func chainNames(links []envelope.Stripper) []string {
	out := make([]string, 0, len(links))
	for _, s := range links {
		out = append(out, s.Name())
	}
	return out
}

// matchPrefixStripper is a stripper that only claims input whose
// first line begins with a configured prefix; on Strip it rewrites
// that prefix to rewriteTo (empty string drops the prefix entirely).
// Used by chaining tests to model real-world strippers where each
// layer only matches *after* the previous layer has peeled.
type matchPrefixStripper struct {
	name      string
	prefix    string
	rewriteTo string
}

func (m matchPrefixStripper) Name() string { return m.name }

func (m matchPrefixStripper) Detect(sample []byte) event.Confidence {
	if strings.HasPrefix(string(sample), m.prefix) {
		return 1.0
	}
	return 0
}

func (m matchPrefixStripper) Strip(_ context.Context, r io.Reader) (io.Reader, <-chan event.Event, error) {
	ch := make(chan event.Event)
	close(ch)
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, err
	}
	rewritten := strings.Replace(string(raw), m.prefix, m.rewriteTo, 1)
	return strings.NewReader(rewritten), ch, nil
}
