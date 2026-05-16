// Package tokens estimates the token cost of text. The budget enforcer
// (M6) uses these counts to decide which Events fit in the user's
// --budget=N.
//
// Two estimators ship:
//
//   - Heuristic (default): word + symbol-run counting with a safety
//     margin. Zero dependencies, instant startup, ±15% accurate for
//     typical log/code text. Biases toward overestimation because
//     underestimating is worse than overestimating for our use case
//     (a too-large output overflows the consumer's context window).
//   - Tiktoken (opt-in): real BPE tokenizer using OpenAI's
//     cl100k_base vocabulary. Exact for GPT-4 / GPT-3.5-turbo,
//     ~95% accurate for Claude, ~85% for Llama and Gemini.
//
// See ARCHITECTURE.md § Token estimation for the design rationale,
// including why we don't auto-detect the target model and why we
// don't support every tokenizer.
package tokens

import (
	"unicode"
)

// Estimator returns an approximate token count for the given string.
// Implementations must be safe for concurrent use and deterministic:
// the same input must always return the same count.
type Estimator interface {
	Estimate(s string) int
}

// WordTokenRatio is the empirical multiplier used by Heuristic to
// translate word count into token count. ~1.3 matches OpenAI's
// historical guidance ("100 tokens ≈ 75 words" → ~1.33×); the value
// is a constant so the design is reviewable without re-reading the
// implementation.
const WordTokenRatio = 1.3

// DefaultSafetyMargin is the fraction by which Heuristic.Estimate
// inflates its raw count, to bias toward overestimation per
// ARCHITECTURE.md § Token estimation. 0.10 = +10%.
const DefaultSafetyMargin = 0.10

// Heuristic is the default Estimator. It counts words and symbol
// runs, applies WordTokenRatio to words, and inflates the result by
// SafetyMargin to bias toward overestimation.
//
// "Word" is a maximal run of letter-or-digit runes.
// "Symbol run" is a maximal run of non-letter, non-digit, non-space
// runes. Whitespace separates runs and is never counted.
//
// Heuristic is intentionally cheap and approximate. For a typical
// 1 KB log line it runs in < 1 µs. The ±15% accuracy is good enough
// for budget enforcement; callers needing exact counts should use
// Tiktoken.
type Heuristic struct {
	// SafetyMargin is the fraction by which the raw count is
	// inflated. 0 disables the bias; values are clamped to >= 0.
	SafetyMargin float64
}

// Default returns a Heuristic pre-configured with DefaultSafetyMargin.
// This is the Estimator the pipeline uses when no --tokenizer flag is
// set.
func Default() Estimator {
	return &Heuristic{SafetyMargin: DefaultSafetyMargin}
}

// Estimate implements Estimator.
func (h *Heuristic) Estimate(s string) int {
	words, symbols := countRuns(s)
	raw := float64(words)*WordTokenRatio + float64(symbols)
	margin := h.SafetyMargin
	if margin < 0 {
		margin = 0
	}
	// math.Ceil would over-round on tiny inputs; round toward
	// nearest-up by adding 0.5 before int truncation, which keeps
	// the heuristic monotonic in input length.
	scaled := raw * (1 + margin)
	return int(scaled + 0.5)
}

// countRuns walks s once and returns the number of word runs and
// symbol runs. Whitespace is ignored.
func countRuns(s string) (words, symbols int) {
	const (
		stateNone = iota
		stateWord
		stateSymbol
	)
	state := stateNone
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			state = stateNone
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if state != stateWord {
				words++
				state = stateWord
			}
		default:
			if state != stateSymbol {
				symbols++
				state = stateSymbol
			}
		}
	}
	return words, symbols
}
