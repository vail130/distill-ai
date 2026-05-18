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
[`SampleSize`](../internal/envelope/envelope.go) bytes (4 KiB —
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
| `""` or `"auto"`      | Read SampleSize bytes, score every registered Stripper, pick the highest ≥ `ConfidenceMinDetect`. Fall back to Noop if nothing wins. |
| `"none"`              | Force the Noop stripper; do not run detection.                                                  |
| `<stripper name>`     | Look up the named stripper; use it unconditionally. Unknown names return `ErrUnknownStripper`.  |

`Wrap` never drops bytes: the SampleSize-byte buffer is prepended
to the trailing Reader before the chosen Stripper sees the stream,
the same `io.MultiReader` shape
[`detect.Detect`](../internal/detect/detect.go) uses.

The CLI's `--strip-envelope` flag (M13.2) maps directly onto
`Options.Choice`.

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
| `github-actions`  | planned  | M13.3    |
| `gitlab-ci`       | planned  | M13.4    |

M13.1 ships the package skeleton, the interface, the registry, the
Noop stripper, and the `Wrap` entry point. Concrete strippers
follow in M13.3 / M13.4; CLI flag plumbing in M13.2; fixtures and
the integration recipe in M13.5.

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
