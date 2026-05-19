# Envelope handling

CI logs, container orchestrators, and similar systems decorate
command output with their own metadata: per-line timestamps,
group / section markers, severity directives, step-failure
boundaries. Those decorations confuse the format detector — a
wrapped `go test` failure no longer looks like a `go test` failure
to `gotest.Detect` — and they waste tokens once they reach the
output encoder.

The **envelope** layer strips wrapper-level metadata from the
input stream *before* format autodetection runs and surfaces the
wrapper-level signals (a `##[error]` line, a step exiting non-zero)
as Events with dedicated `envelope_*` Kinds. Inner-format
detection runs against the cleaned bytes, so a GitHub Actions
workflow log wrapping `go test` still detects as `gotest` with
`Confidence=1.0`.

The envelope layer is a **decorator**, not a Format. Wrappers
identify themselves through their own markers (`##[group]`,
`section_start:`, an RFC3339-Z prefix on every line); the
underlying command output is whatever it is. Keeping the
abstraction separate avoids forcing every Format implementation to
learn every CI wrapper, and lets new wrappers (CircleCI, Buildkite,
Docker buildkit, systemd journal) land as drop-in packages without
touching the format-plugin contract.

## Where it sits in the pipeline

The flow is:

```
input  →  envelope.Wrap  →  cleaned io.Reader  →  detect.Detect  →  Format.Parse  →  Stages  →  Sink
                       ↘   signals <-chan Event  ───────────────────────────────────────────↗
```

`Wrap` peels off the wrapper. `detect.Detect` sees bytes that look
exactly like the bare command output. The signals channel feeds
envelope-level Events into the same downstream pipeline so they
participate in dedupe, budget, and encoder rendering without any
special-casing.

## Strippers

A `Stripper` is the envelope analogue of a `Format`:

```go
type Stripper interface {
    Name() string
    Detect(sample []byte) event.Confidence
    Strip(ctx context.Context, r io.Reader) (cleaned io.Reader, signals <-chan event.Event, err error)
}
```

`Detect` runs against the first
[`SampleSize`](../internal/envelope/envelope.go) bytes (16 KiB —
the same window the format detector uses, so the two layers see
the same shape of input). `Strip` must be streaming: the cleaned
Reader produces output incrementally, and the signals channel is
bounded so a slow consumer applies backpressure to the stripper
rather than blowing memory.

Like Formats, Strippers self-register via `init()`:

```go
package githubactions

import "github.com/vail130/distill-ai/internal/envelope"

func init() { envelope.Register(Stripper{}) }
```

`envelope.Get(name)`, `envelope.All()`, and `envelope.Register` are
the registry-side API. Their semantics match
[`formats.Register`](../internal/formats/registry.go) exactly:
duplicate names panic at init, nil values panic at init,
`Register` is safe for concurrent use, `All()` returns a sorted
snapshot.

The name `"none"` is reserved for the [Noop](#the-noop-stripper)
stripper and cannot be registered.

## Wrap

```go
cleaned, signals, chosen, err := envelope.Wrap(ctx, r, envelope.Options{
    Choice: envelope.ChoiceAuto, // "auto" | "none" | <stripper name>
})
```

Behaviour:

| `Choice`              | Behaviour                                                                                       |
|-----------------------|-------------------------------------------------------------------------------------------------|
| `""` or `"auto"`      | Detect and apply a Stripper, then re-detect on the cleaned bytes and apply the next match. Iterate up to `MaxChainDepth` times. Fall back to Noop if nothing wins on the first pass. |
| `"none"`              | Force the Noop stripper; do not run detection.                                                  |
| `<stripper name>`     | Look up the named stripper; use it unconditionally. Unknown names return `ErrUnknownStripper`. No chaining. |

`Wrap` never drops bytes: the SampleSize-byte buffer is prepended
to the trailing Reader before the chosen Stripper sees the stream,
the same `io.MultiReader` shape
[`detect.Detect`](../internal/detect/detect.go) uses.

### Chaining

Real-world inputs often nest envelopes: a GitLab CI job that runs
`docker compose up` wraps its container's stdout in **both** the
GitLab CI section/timestamp envelope **and** the docker-compose
service-name prefix (`testrunner-1  | === RUN TestThing`). Peeling
only the outer envelope still leaves the inner-format detector
looking at `testrunner-1  | ` line prefixes, which don't match
`gotest`'s `^=== RUN` anchor and force the fall-back to `generic`.

`Wrap` on `ChoiceAuto` solves this by chaining: after the
highest-confidence Stripper finishes, the next iteration re-samples
the cleaned bytes and picks again from the **remaining** Strippers
(the same Stripper can't apply twice in a single chain). Chaining
stops as soon as no remaining Stripper scores ≥
`ConfidenceMinDetect`, or after `MaxChainDepth` iterations as a
safety cap.

When more than one Stripper applies, `chosen.Name()` is the joined
chain (e.g. `"gitlab-ci+docker-compose"`); the
[`envelope.Chain`](../internal/envelope/envelope.go) helper returns
the raw slice for callers that need the breakdown.

Signals from every applied Stripper flow through a single fan-in
channel so downstream consumers see one unified stream regardless
of how many envelopes were peeled.

The CLI's `--strip-envelope` flag (M13.2) maps directly onto
`Options.Choice`.

## CLI

`--strip-envelope=<choice>` is registered on both the `run` and
`explain` subcommands, with default `auto`. Behaviour:

```bash
# Default: detect a registered stripper from the first 16 KiB of
# input; fall back to Noop if nothing claims it.
gh run view --log | distill-ai run

# Equivalent (auto is the default).
gh run view --log | distill-ai run --strip-envelope=auto

# Skip envelope handling entirely. Useful when stdin is bare
# command output and you want to short-circuit detection.
go test ./... 2>&1 | distill-ai run --strip-envelope=none

# Force a specific stripper, bypassing detection. Lets the user
# override an ambiguous sample, or pin behaviour in CI.
glab ci trace | distill-ai run --strip-envelope=gitlab-ci
```

Errors:

- Unknown stripper name → exit code 2, stderr names the unknown
  value.
- `--strip-envelope=auto` with no registered strippers (the
  state today, before M13.3 / M13.4) → Wrap silently falls back
  to Noop, exit code 0. The same fallback fires when the
  sample doesn't match any registered stripper.

The flag is also reflected in the
[`cli-surface`](../skills/distill-ai-dev/SKILL.md) manifest;
[`TestSkill_DocumentsCurrentCLISurface`](../test/integration/integration_test.go)
gates merges on the manifest staying in sync with the compiled
binary.

The chosen stripper is reported on stderr when `--verbose` /
`-v` is set, alongside the existing `format=`, `source=`, and
`sample_bytes=` line. Noop suppresses the envelope line so the
verbose output is unchanged for users who don't run inside a CI
wrapper.

## The Noop stripper

`envelope.Noop` is the explicit "no envelope" Stripper. It returns
the input Reader unchanged and an immediately-closed signals
channel. `Wrap` selects Noop when:

- `Options.Choice` is `ChoiceNone`, or
- `Options.Choice` is `ChoiceAuto` and no registered Stripper
  scores at or above `ConfidenceMinDetect`, or
- no Strippers are registered at all.

Noop's `Detect` always returns `0.0` so it never participates in
auto-detection; it is the fallback target, not a candidate. Its
name is `"none"`, which is also the public Choice value users pass
on the CLI to force it.

## Signal Events

Concrete strippers (landing in M13.3 / M13.4) translate
wrapper-level signals into Events with these Kinds:

| `Kind`                    | Severity        | Meaning                                                                |
|---------------------------|-----------------|------------------------------------------------------------------------|
| `envelope_error`          | `SeverityError` | Wrapper-level error directive (GitHub `##[error]`, etc.).              |
| `envelope_warning`        | `SeverityWarn`  | Wrapper-level warning directive (`##[warning]`, etc.).                 |
| `envelope_step_failure`   | `SeverityError` | A named step / section ended with a non-zero exit code.                |

Signal Events flow through the same pipeline as parser Events
once `pipeline.Build` wires the fan-in (M13.2). They participate
in dedupe, collapse, budget enforcement, and final encoding the
same way; encoders do not special-case the `envelope_*` Kinds.

For `envelope_step_failure`, the step name is the Event Title and
the metadata carries:

| Key          | Value                                                                                       |
|--------------|---------------------------------------------------------------------------------------------|
| `step`       | The step / section name as the wrapper reports it.                                          |
| `exit_code`  | Decimal string of the exit code (e.g., `"1"`).                                              |

The Kind constants live as exported values on the `envelope`
package (`envelope.KindEnvelopeError`,
`envelope.KindEnvelopeWarning`,
`envelope.KindEnvelopeStepFailure`). Stripper authors MUST use the
constants rather than string literals so a future rename is a
single-point change.

The Kind values are documented in
[`docs/formats/SCHEMA.md`](./formats/SCHEMA.md#envelope-kinds) as
additive entries; per the [output-stability
rule](../rules/output-stability.md) they do not bump
`schema_version`.

## Shipped strippers

| Name              | Status   | Lands in |
|-------------------|----------|----------|
| `none` (Noop)     | shipped  | M13.1    |
| `github-actions`  | shipped  | M13.3    |
| `gitlab-ci`       | shipped  | M13.4    |
| `docker-compose`  | shipped  | pre-v1.0 |

### github-actions

Strips the GitHub Actions workflow envelope: per-line RFC3339-Z
timestamps, `##[group]` / `##[endgroup]` markers, and workflow
commands. Surfaces `##[error]`, `##[warning]`, `##[notice]`, and the
canonical "step failed" line as signal Events.

**Detection.** Confidence `1.0` when the sample contains any of
`##[group]`, `##[error]`, `##[warning]`, `##[debug]`,
`##[notice]`, or `::set-output ` on a line (with or without a
leading timestamp). Confidence `0.8` when no workflow command
appears but at least three of the first ten non-blank lines carry
an RFC3339-Z timestamp prefix (e.g., `2024-01-15T10:23:45.1234567Z `).

**Stripped from cleaned output.**

| Input                                              | Cleaned output                |
|----------------------------------------------------|-------------------------------|
| `2024-01-15T10:23:45.1234567Z hello`               | `hello`                       |
| `##[group]Build` and `##[endgroup]`                | *(removed)*                   |
| `##[error]Boom`                                    | *(removed)*; emits signal     |
| `##[warning]Deprecated`                            | *(removed)*; emits signal     |
| `##[notice]Heads up`                               | *(removed)*; emits signal     |
| `##[debug]Verbose`                                 | *(removed)*; no signal        |
| `::set-output name=foo::bar`, `::add-mask::…` etc. | *(removed)*; no signal        |

**Synthesised signal Events.**

| Input line                                                | `Kind`                    | Severity | Title / Metadata                            |
|-----------------------------------------------------------|---------------------------|----------|---------------------------------------------|
| `##[error]Boom`                                           | `envelope_error`          | `error`  | Title `"Boom"`                              |
| `##[warning]Deprecated` or `##[notice]Heads up`           | `envelope_warning`        | `warn`   | Title `"Deprecated"` / `"Heads up"`         |
| `##[error]Process completed with exit code N.`            | `envelope_step_failure`   | `error`  | Title = surrounding group's name; `metadata.exit_code="N"`, `metadata.step` set when in scope |

**Notes.**

- ANSI escape sequences in input are passed through unchanged.
  Inner-format parsers handle ANSI on Titles where it matters.
- The group-name stack is bounded at depth 8; deeper nesting is
  silently ignored. Real workflows nest at most two or three
  levels, so the cap is purely defensive.
- The `##[error]Process completed with exit code N.` pattern is
  the canonical "step finished failing" marker the runner appends
  after every failing step. When the marker fires before the
  surrounding `##[endgroup]`, the signal's `metadata.step` and
  Title carry the group name. When it fires after (the common
  case for actions whose final command itself emits
  `##[endgroup]`), `metadata.step` is empty.

**Example.**

Input:

```
2024-01-15T10:23:45.1234567Z ##[group]Run go test
2024-01-15T10:23:46.1234567Z --- FAIL: TestLogin (0.02s)
2024-01-15T10:23:47.1234567Z     auth_test.go:42: expected 200, got 502
2024-01-15T10:23:48.1234567Z FAIL
2024-01-15T10:23:49.1234567Z ##[endgroup]
2024-01-15T10:23:50.1234567Z ##[error]Process completed with exit code 1.
```

Cleaned output passed to format detection:

```
--- FAIL: TestLogin (0.02s)
    auth_test.go:42: expected 200, got 502
FAIL
```

The cleaned bytes detect as `gotest` with `Confidence=1.0`; the
gotest parser sees exactly the bytes `go test` emitted.

One synthesised signal Event arrives in parallel:
`envelope_step_failure` with `Title=""` (the group was already
closed), `metadata.exit_code="1"`.

### gitlab-ci

Strips the GitLab CI job envelope: `section_start:` /
`section_end:` markers, trailing carriage returns, the runner's
terminal "Job failed" line, and — when present — the per-line
preamble that `glab ci trace` and `gitlab-runner --timestamps`
prepend to every line.

**Detection.** Confidence `1.0` when the sample contains any line
matching `section_start:NS:name` or `section_end:NS:name` (with or
without the canonical `\r` terminator), with or without the glab
preamble. Confidence `0.8` when the sample contains the "Running
with gitlab-runner " banner together with at least five distinct
ANSI CSI escape sequences in the first 1 KiB.

**Stripped from cleaned output.**

| Input                                                                       | Cleaned output                |
|-----------------------------------------------------------------------------|-------------------------------|
| `section_start:1700000000:build\r`                                          | *(removed)*                   |
| `section_end:1700000000:build\r`                                            | *(removed)*                   |
| `2026-05-19T00:02:58.540261Z 00O section_start:1700000000:build`            | *(removed)*                   |
| `2026-05-19T00:03:22.731006Z 00O+\x1b[0Ksection_start:1700000001:script`   | *(removed)*                   |
| `2026-05-19T00:15:07.553120Z 00O \x1b[31;1mERROR: Job failed: exit status 1` | *(removed)*; emits signal     |
| `line ending with \r\n`                                                     | `line ending with \n`         |
| `ERROR: Job failed: exit code N`                                            | *(removed)*; emits signal     |

The glab preamble itself is `<RFC3339-Z timestamp> <2-digit step
number><1-letter stream code><sep>` where `<sep>` is either a
single space (the canonical case) or a `+` followed by an ANSI CSI
"erase to end of line" sequence (`\x1b[0K`) that glab emits on
continuations of carriage-return-terminated runner writes. Any
ANSI CSI escapes that follow are also consumed by the same prefix
strip so the section / failure regexes apply to the line content
directly. Per-line ANSI escapes that appear later in the line body
are passed through unchanged — inner-format parsers handle them
where they matter (gotest, pytest, jest all strip ANSI from Title
fields).

**Synthesised signal Events.**

| Input line                                                                                | `Kind`                    | Severity | Title / Metadata                                                                                                            |
|-------------------------------------------------------------------------------------------|---------------------------|----------|-----------------------------------------------------------------------------------------------------------------------------|
| `ERROR: Job failed: exit code N` or `ERROR: Job failed: exit status N` (with or without preamble) | `envelope_step_failure` | `error`  | Title = surrounding section name (or empty); `metadata.exit_code="N"`, `metadata.step` set when in scope                    |

Both "exit code N" and "exit status N" phrasings are accepted —
different GitLab runner / glab versions print one or the other but
they convey the same signal.

**Notes.**

- When the log is consumed via `glab ci trace`, every line is
  wrapped in the glab preamble. distill-ai recognises and strips
  it so `glab ci trace | distill-ai` and the raw-runner-output
  case behave identically.
- The glab preamble strip is conservative: a line without the
  preamble matches the regex as a no-op so non-wrapped GitLab CI
  logs are unaffected.
- The section stack is bounded at depth 8 for symmetry with the
  github-actions stripper; in practice GitLab sections do not
  nest.
- The runner appends `ERROR: Job failed: exit code N` after a
  failing job — typically after the final `section_end`. The
  surrounding section is therefore usually closed when the marker
  fires; `metadata.step` is empty in that case. When the marker
  appears inside an open section (rare in practice but possible
  with custom runner configurations), the section name is
  attached.

**Example.**

Input:

```
Running with gitlab-runner 16.0.0 (abcdef12)
section_start:1700000000:run_go_test
--- FAIL: TestLogin (0.02s)
    auth_test.go:42: expected 200, got 502
FAIL
section_end:1700000000:run_go_test
ERROR: Job failed: exit code 1
```

(With `\r` terminators on every line in the real runner output.)

Cleaned output passed to format detection:

```
Running with gitlab-runner 16.0.0 (abcdef12)
--- FAIL: TestLogin (0.02s)
    auth_test.go:42: expected 200, got 502
FAIL
```

The cleaned bytes detect as `gotest` (the `Running with
gitlab-runner` line is harmless prose to gotest's parser).

One synthesised signal Event arrives in parallel:
`envelope_step_failure` with `Title=""` (the section was closed
before the marker fired), `metadata.exit_code="1"`.

### docker-compose

Strips the per-line `<service>  | ` (or `<service>-<replica>  | `)
prefix the docker daemon prepends to every container stdout line
when one or more services are attached. The stripper is built to
compose with the CI strippers: a GitLab CI job that runs
`docker compose up` arrives at `Wrap` with two envelopes around
the test runner's bytes, and the [chaining loop](#chaining) peels
both before format detection.

**Detection.** Confidence `1.0` when the first non-blank line of
the sample carries the prefix. Confidence `0.8` when at least four
distinct lines in the first 4 KiB carry the prefix, even if the
first line does not (catches runs whose preamble — image pull
progress, attach banner — precedes the container output).
Confidence `0.0` otherwise.

**Stripped from cleaned output.**

| Input                                                          | Cleaned output                |
|----------------------------------------------------------------|-------------------------------|
| `testrunner-1  \| === RUN TestThing`                           | `=== RUN TestThing`           |
| `api      \| ready`                                            | `ready`                       |
| `[+] Pulling testrunner ...` (no prefix)                       | passed through verbatim       |
| `Attaching to testrunner-1`                                    | passed through verbatim       |

**No signal Events.** docker compose's framing carries no
error/warning/step-failure semantics that aren't already present in
the inner stream — the test runner emits its own `FAIL:` markers;
docker compose just relays bytes. The stripper exists purely so
the inner-format detector sees the bare command output.

**Notes.**

- The stripper recognises the **uncoloured** prefix shape only.
  Recent docker compose versions emit a colourised prefix
  (`\x1b[36mtestrunner-1  |\x1b[0m === RUN ...`) when stdout is a
  TTY; CI runners and `docker compose --no-ansi` produce the
  uncoloured form this stripper handles. A coloured-form follow-up
  is deferred because it has to preserve any inner ANSI codes
  emitted by the test runner itself.
- The grammar for the service-name token follows the Compose spec:
  lowercase alphanumerics, underscores, and dashes, optionally
  ending in `-<replica-number>` for replicated services. The
  `--padding-spaces--| ` minimum is two spaces, which is the
  docker-compose default when only a single service is attached.

**Example.**

Input (the shape KNOWN_ISSUES.md issue #2 traced):

```
section_start:1700000000:run_tests
Running with gitlab-runner 16.0.0
[+] Pulling testrunner ...
Attaching to testrunner-1
testrunner-1  | === RUN   TestLogin
testrunner-1  | --- FAIL: TestLogin (0.02s)
testrunner-1  |     auth_test.go:42: expected 200, got 502
testrunner-1  | FAIL
section_end:1700000000:run_tests
ERROR: Job failed: exit code 1
```

`envelope.Wrap` on `ChoiceAuto` picks `gitlab-ci` first
(`section_start:` matches at 1.0), runs Strip, re-samples the
cleaned bytes, picks `docker-compose` next (every `testrunner-1`
line matches the prefix pattern), runs Strip again. The cleaned
output handed to the format detector is bare `go test` output:

```
Running with gitlab-runner 16.0.0
[+] Pulling testrunner ...
Attaching to testrunner-1
=== RUN   TestLogin
--- FAIL: TestLogin (0.02s)
    auth_test.go:42: expected 200, got 502
FAIL
```

The cleaned bytes detect as `gotest` (the preamble lines are
harmless prose to gotest's parser). `chosen.Name()` returns
`"gitlab-ci+docker-compose"`; `envelope.Chain(chosen)` returns the
two-element slice for callers that want to inspect each link.

One synthesised signal Event still arrives from the gitlab-ci
stripper: `envelope_step_failure` with `metadata.exit_code="1"`.

## Adding a new stripper

Future envelope sources (CircleCI, Buildkite, Docker buildkit,
systemd journal, …) follow the same pattern as the M13.3 / M13.4
implementations:

1. New package under `internal/envelope/<name>/`.
2. `func init() { envelope.Register(Stripper{}) }`.
3. `Detect` returns ≥ `ConfidenceMinDetect` on a definitive marker
   and < that on ambiguous input.
4. `Strip` returns a cleaned Reader (streaming, no full-input
   buffering) and a signals channel using the documented Kinds.
5. Fixtures under `internal/envelope/<name>/testdata/`.
6. One integration test that feeds a wrapped fixture to the binary
   via stdin and asserts the inner Format detects correctly.

No architectural change required — the envelope layer is
deliberately additive.
