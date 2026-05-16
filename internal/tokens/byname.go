package tokens

import "fmt"

// Known Estimator names. The CLI in M8 maps the --tokenizer flag
// onto these. Keep the strings stable: they're a user-visible API.
const (
	NameHeuristic = "heuristic"
	NameTiktoken  = "tiktoken"
)

// ByName returns the named Estimator. Used by the M8 CLI to wire the
// --tokenizer flag through to the pipeline. Centralises the
// flag-string → Estimator mapping so the CLI doesn't have to know
// which Estimator factories exist.
//
// Returns an error for unknown names rather than silently falling
// back; misspellings should fail loudly so the user can correct them
// rather than discover at the end of a run that their --budget was
// enforced by the wrong estimator.
func ByName(name string) (Estimator, error) {
	switch name {
	case "", NameHeuristic:
		return Default(), nil
	case NameTiktoken:
		return Tiktoken()
	default:
		return nil, fmt.Errorf("tokens: unknown estimator %q (valid: %s, %s)", name, NameHeuristic, NameTiktoken)
	}
}
