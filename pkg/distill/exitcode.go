package distill

// ExitCode constants mirror the CLI's exit-code contract. A library
// caller can replicate the CLI's exit-code semantics by calling
// [ExitCodeFromSummary] after Distill completes and forwarding the
// returned value to os.Exit.
const (
	// ExitOK is the success exit code: the pipeline ran cleanly and
	// emitted at least one Event.
	ExitOK = 0

	// ExitNoEvents signals that the pipeline ran cleanly but
	// produced no Events. Distinct from ExitError so a wrapping
	// script can tell "broken input" from "clean input that
	// happened to have no failures."
	ExitNoEvents = 1

	// ExitError covers everything that isn't a normal pipeline
	// outcome: a setup failure, a missing format under --strict,
	// or an unexpected runtime error. Distill itself returns a
	// non-nil error for these cases; ExitError is the code the
	// caller maps that error to.
	ExitError = 2

	// ExitPartial flags that the BudgetStage forced drops or
	// truncations under Options.Budget. The pipeline completed
	// successfully but some content didn't fit; a CI consumer
	// might want to surface this distinctly from a clean run.
	ExitPartial = 3
)

// ExitCodeFromSummary maps a *Summary onto the CLI's exit-code
// contract. The mapping mirrors cmd/distill-ai/exitcode.go:
//
//   - ExitPartial (3) wins when the Summary reports forced drops or
//     truncations, even if zero events were emitted — a tight
//     budget that drops everything is meaningfully different from
//     clean input with no failures.
//   - ExitNoEvents (1) when the pipeline ran cleanly but no Events
//     were emitted.
//   - ExitOK (0) when the pipeline ran cleanly and emitted at
//     least one Event.
//
// ExitError (2) is reserved for the error return from Distill
// itself; ExitCodeFromSummary does not own that case. Callers
// translate the error half of Distill's return separately:
//
//	events, summary, err := distill.Distill(ctx, r, opts)
//	if err != nil {
//	    return distill.ExitError
//	}
//	for ev := range events { ... }
//	summary.Wait()
//	return distill.ExitCodeFromSummary(summary)
//
// Safe on a nil receiver: returns ExitNoEvents. Calling with a
// Summary whose fields haven't been populated yet (i.e., before
// Summary.Wait returned) is a race; the result is well-defined
// but reflects in-flight state.
func ExitCodeFromSummary(s *Summary) int {
	if s == nil {
		return ExitNoEvents
	}
	if s.ForcedDrops() {
		return ExitPartial
	}
	if s.EventsEmitted == 0 {
		return ExitNoEvents
	}
	return ExitOK
}
