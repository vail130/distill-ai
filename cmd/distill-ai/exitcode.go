package main

// Exit codes. The values match
// [ARCHITECTURE.md § Exit codes](../../ARCHITECTURE.md#exit-codes)
// and are the source of truth for what each numeric code means in
// distill-ai. Every subcommand maps its terminal state to one of
// these; the CLI translates exitCodeError into the same set on the
// way out of run().
//
// Agents and CI rely on these:
//
//   - ExitOK signals "ran the pipeline and emitted at least one
//     event". A shell-script wrapper can branch on it to decide
//     whether to forward output to the next stage.
//   - ExitNoEvents distinguishes "clean input" from "broken input".
//     A test runner that calls `distill-ai run gotest` can treat
//     ExitNoEvents as a green build.
//   - ExitError covers everything that isn't a normal pipeline
//     outcome: bad flags, IO failures, --strict autodetect refusal,
//     internal errors in any stage.
//   - ExitPartial flags that the budget enforcer (--budget) had to
//     drop or truncate events to fit. Output is still emitted; the
//     caller knows the result is incomplete.
//
// The precedence rule (when multiple terminal conditions hold) is:
//
//	ExitError  > ExitPartial > ExitNoEvents > ExitOK
//
// In particular, ExitPartial wins over ExitNoEvents — if the budget
// dropped every event so nothing emerged, the caller cares more that
// the budget was the cause than that the output is empty.
const (
	ExitOK       = 0
	ExitNoEvents = 1
	ExitError    = 2
	ExitPartial  = 3
)
