package tokens_test

import (
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/tokens"
)

func TestHeuristic_EmptyString(t *testing.T) {
	h := tokens.Default()
	if got := h.Estimate(""); got != 0 {
		t.Errorf("Estimate(\"\") = %d, want 0", got)
	}
}

func TestHeuristic_WhitespaceOnly(t *testing.T) {
	h := tokens.Default()
	if got := h.Estimate("   \t\n  "); got != 0 {
		t.Errorf("Estimate(whitespace-only) = %d, want 0", got)
	}
}

func TestHeuristic_PureASCIIWords(t *testing.T) {
	// "the quick brown fox jumps over the lazy dog" — 9 words.
	// Raw count: 9 * 1.3 = 11.7. With +10% margin: 12.87 → 13.
	h := tokens.Default()
	got := h.Estimate("the quick brown fox jumps over the lazy dog")
	if got < 11 || got > 15 {
		t.Errorf("Estimate of 9-word sentence = %d, want ~13 (range 11-15)", got)
	}
}

func TestHeuristic_SymbolHeavyCode(t *testing.T) {
	// A Go snippet with brackets, semicolons, parens, operators.
	// Words: func, foo, x, int, return, x, plus, 1 (8 words).
	// Symbol runs: ( , ) ,  { , }, the `+` and `;` etc.
	// We don't need to know the exact answer; just that symbols
	// contribute non-trivially.
	h := &tokens.Heuristic{SafetyMargin: 0}
	code := "func foo(x int) int { return x + 1; }"
	plain := "func foo x int int return x 1"
	codeCount := h.Estimate(code)
	plainCount := h.Estimate(plain)
	if codeCount <= plainCount {
		t.Errorf("symbol-heavy code (%d tokens) should score higher than its word-only stripped form (%d tokens)",
			codeCount, plainCount)
	}
}

func TestHeuristic_SafetyMarginZero(t *testing.T) {
	// With SafetyMargin = 0, the result equals round(words*1.3 + symbols).
	// "hello world" = 2 words, 0 symbols, raw = 2.6 → 3.
	h := &tokens.Heuristic{SafetyMargin: 0}
	if got := h.Estimate("hello world"); got != 3 {
		t.Errorf("Estimate(\"hello world\") with margin 0 = %d, want 3", got)
	}
}

func TestHeuristic_SafetyMarginAddsHeadroom(t *testing.T) {
	zero := &tokens.Heuristic{SafetyMargin: 0}
	ten := &tokens.Heuristic{SafetyMargin: 0.10}
	// Use enough text that the margin produces a visible delta.
	s := strings.Repeat("alpha beta gamma delta epsilon ", 20)
	if ten.Estimate(s) <= zero.Estimate(s) {
		t.Errorf("non-zero SafetyMargin should produce a higher estimate; zero=%d ten=%d",
			zero.Estimate(s), ten.Estimate(s))
	}
}

func TestHeuristic_NegativeMarginClampsToZero(t *testing.T) {
	neg := &tokens.Heuristic{SafetyMargin: -0.5}
	zero := &tokens.Heuristic{SafetyMargin: 0}
	s := "the quick brown fox"
	if neg.Estimate(s) != zero.Estimate(s) {
		t.Errorf("negative SafetyMargin (%d) should clamp to zero (%d)",
			neg.Estimate(s), zero.Estimate(s))
	}
}

// TestHeuristic_OverestimatesByDefault is the property test for the
// asymmetric-cost principle: the default heuristic biases toward
// overestimation so a budget enforcer that says "we'll fit in N
// tokens" doesn't underfit. Using fixture strings with hand-counted
// approximate-true-token-counts.
func TestHeuristic_OverestimatesByDefault(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		trueCount int // hand-estimated from OpenAI tiktoken behaviour
	}{
		// One word, one token for short ASCII.
		{"one-word", "hello", 1},
		// Five short words, ~5 tokens.
		{"short-sentence", "the quick brown fox jumps", 5},
		// A Go-ish line. Real tiktoken cl100k_base count: ~13.
		{"go-snippet", "func foo(x int) int { return x + 1 }", 13},
	}
	h := tokens.Default()
	underestimates := 0
	for _, c := range cases {
		got := h.Estimate(c.text)
		if got < c.trueCount {
			underestimates++
			t.Logf("case %q: heuristic estimated %d, true ~%d", c.name, got, c.trueCount)
		}
	}
	// Allow at most one case to underestimate; the bias should
	// hold across the typical input.
	if underestimates > 1 {
		t.Errorf("heuristic underestimated %d/%d cases; the +10%% safety margin is not biased enough toward overestimation",
			underestimates, len(cases))
	}
}

func TestHeuristic_DeterministicAcrossCalls(t *testing.T) {
	h := tokens.Default()
	s := "deterministic input — same output every time, even with — em-dashes."
	first := h.Estimate(s)
	for i := 0; i < 100; i++ {
		if got := h.Estimate(s); got != first {
			t.Fatalf("non-deterministic: iter %d returned %d, want %d", i, got, first)
		}
	}
}

func TestHeuristic_HandlesUnicode(t *testing.T) {
	h := tokens.Default()
	// Unicode letters count as word characters; the heuristic
	// must not crash or skip them.
	got := h.Estimate("café résumé naïve")
	if got < 2 {
		t.Errorf("Unicode-letter words should still count; got %d", got)
	}
}

func TestWordTokenRatio_Constant(t *testing.T) {
	if tokens.WordTokenRatio < 1.0 || tokens.WordTokenRatio > 2.0 {
		t.Errorf("WordTokenRatio = %v, expected in [1.0, 2.0]", tokens.WordTokenRatio)
	}
}

func TestDefaultSafetyMargin_Constant(t *testing.T) {
	if tokens.DefaultSafetyMargin <= 0 || tokens.DefaultSafetyMargin > 0.5 {
		t.Errorf("DefaultSafetyMargin = %v, expected in (0, 0.5]", tokens.DefaultSafetyMargin)
	}
}
