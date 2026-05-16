# Architecture

This document captures the design decisions for `distill-ai`. Read this
before making non-trivial changes.

## Goal

Turn noisy command output (test runs, logs, stack traces) into a compact,
structured event stream suitable for LLM consumption. Sit in a Unix pipe.
Be invisible when there's nothing to distill, indispensable when there is.

## Design principles

1. **Unix-pipe-native.** stdin → stdout is the default path. Everything
   else is an affordance.
2. **Zero-config common case.** `cmd | distill-ai` should Just Work via
   format autodetection.
3. **Deterministic output.** Same input → same output. Critical for
   caching, golden tests, and agent reproducibility.
4. **Streaming-first.** Never require buffering the full input. Important
   for `tail -f`, CI streams, and large files.
5. **Format plugins, not hardcoded formats.** Adding pytest support
   shouldn't require touching jest code. New formats register themselves.
6. **Honest about what it dropped.** Always emit a footer summarising what
   was collapsed. The consumer (human or agent) must know what's missing.
7. **No network. Ever.** Reads stdin, writes stdout. No telemetry, no
   updates, no remote lookups.

## CLI surface

### Invocation

```
distill-ai [FORMAT] [OPTIONS] [FILE...]
```

- `FORMAT` is optional; omitted → autodetect.
- `FILE` is optional; omitted → read stdin.
- Output to stdout. Logs/diagnostics to stderr.

### Subcommands

```
distill-ai list-formats          # show registered formats
distill-ai detect FILE           # show detected format and confidence
distill-ai explain FILE          # dry-run; annotate keep/drop decisions
distill-ai completions [shell]   # bash/zsh/fish completion
distill-ai version
```

Subcommands are only for things that aren't the main pipeline.
**Resist adding more.**

### Flags

Total ~15. Resist adding more without strong justification.

**Format selection**

- `FORMAT` (positional) — explicit format
- `--auto` (default) — autodetect
- `--list-formats`

**Filtering**

- `--keep-vendor` — don't collapse vendor / library stack frames
- `--keep-warnings` — include warnings (default: errors only when errors exist)
- `--severity=error|warn|info` — minimum severity to keep
- `--max-events=N` — cap total events (default: 50)
- `--context=N` — lines of context around each event (default: 3)

**Deduplication**

- `--dedupe` (default: on for streaming, off for batch)
- `--no-dedupe`
- `--dedupe-window=N` — only dedupe within last N events (streams)

**Output**

- `--output=text|json|markdown` (default: text)
- `--budget=N` — target output token count; prunes further to fit
- `--no-footer` — suppress the "collapsed X, dropped Y" summary

**Behaviour**

- `--explain` — dry-run; annotate decisions
- `--strict` — exit 2 if format detection is uncertain
- `--passthrough` — if no events found, emit input unchanged
- `--tokenizer=heuristic|tiktoken` (default: heuristic)

**Standard**

- `-h` / `--help`
- `-v` / `--verbose` (to stderr)
- `--version`

### Exit codes

- `0` — success, events found and emitted
- `1` — success, but no events found (input was clean)
- `2` — error: bad flags, IO error, detection failed in `--strict`
- `3` — partial: ran successfully but dropped content to fit `--budget`

Agents and CI use these.

## Output formats

### `text` (default)

Compact human + LLM readable:

```
3 events from pytest

[1] FAIL tests/api/test_auth.py::test_login_redirect
  AssertionError: expected 302, got 200
  at test_auth.py:47
  context:
    45:     response = client.post("/login", data=creds)
    46:     assert response.status_code == 302
    47:>    assert response.headers["location"] == "/dashboard"

[2] FAIL tests/api/test_auth.py::test_logout_clears_session
  KeyError: 'session_id'
  at auth/views.py:112 (called from test_auth.py:89)
  ... 8 vendor frames collapsed

---
distilled 8,432 lines → 24 lines (340 tokens)
dropped: 8,388 lines (passing tests, warnings, vendor frames)
```

### `json`

Stable schema, machine-readable:

```json
{
  "format": "pytest",
  "events": [
    {
      "severity": "error",
      "kind": "test_failure",
      "title": "AssertionError: expected 302, got 200",
      "location": {"file": "test_auth.py", "line": 47},
      "context": ["...", "...", "..."],
      "frames_collapsed": 0,
      "count": 1
    }
  ],
  "summary": {
    "input_lines": 8432,
    "output_lines": 24,
    "events_found": 3,
    "events_emitted": 3,
    "estimated_tokens": 340
  }
}
```

In streaming mode, JSON output switches to `ndjson` (one event per line).

### `markdown`

For direct paste into chat. Same content as `text` with markdown headings
and fenced code blocks.

## Config file

Optional `.distill-ai.toml` in repo root or `~/.config/distill-ai/config.toml`:

```toml
default_budget = 2000
default_output = "text"

[formats.pytest]
keep_warnings = false
context_lines = 3

[formats.k8s]
dedupe = true
dedupe_window = 1000

[[formats.custom.myapp]]
detect_regex = '^\[myapp\]'
event_start = '^\[myapp\] ERROR'
event_end = '^\[myapp\] (INFO|DEBUG|ERROR)'
```

Per-project config matters for monorepos and lets agents inherit the
right defaults without flag-passing gymnastics.

## Internal architecture

### Package layout

```
cmd/distill-ai/
    main.go               # CLI wiring
    flags.go
    run.go                # orchestration

internal/
    pipeline/
        pipeline.go       # stream → detector → parser → filter → emitter
        budget.go         # token budgeting

    detect/
        detect.go         # format autodetection
        signatures.go     # heuristics

    formats/
        format.go         # Format interface
        registry.go       # plugin registry
        pytest/
        jest/
        gotest/
        k8s/
        json/             # structured JSON logs
        generic/          # regex fallback

    event/
        event.go          # Event type
        dedupe.go
        collapse.go       # stack frame collapsing

    output/
        text.go
        json.go
        markdown.go

    tokens/
        estimate.go       # token-count estimators

pkg/
    distill/              # exported API for library use
```

The `pkg/distill` package is the stable public library API. Until M14
lands the streaming `Distill(ctx, r, opts) (<-chan Event, error)` entry
point, this package exposes type aliases only (`Event`, `Severity`,
`Format`, `ParseOpts`, etc.), letting downstream code import the
public path so M14 doesn't have to rearrange imports. Internal
packages (`internal/event`, `internal/formats`, etc.) are not part of
the API contract and may change without a version bump.

### Core types

```go
// Event is the unit of distillation. The JSON shape is a public API;
// see docs/formats/SCHEMA.md.
type Event struct {
    Severity        Severity          // SeverityError, SeverityWarn, SeverityInfo
    Kind            string            // "test_failure", "panic", ...
    Title           string            // one-line summary
    Location        *Location         // file:line, nil when unknown
    Body            []string          // relevant lines verbatim
    Context         []string          // surrounding lines
    Frames          []StackFrame      // structured stack, optional
    FramesCollapsed int               // vendor frames omitted during collapse
    Count           int               // dedupe count (1 for unique events)
    Truncated       bool              // body forced-truncated by --budget
    Metadata        map[string]string // format-specific extras
    Raw             string            // original bytes; internal-only, json:"-"
}

// Format is the plugin interface. Adding a format = implementing this.
type Format interface {
    Name() string
    Detect(sample []byte) Confidence
    Parse(ctx context.Context, r io.Reader, opts ParseOpts) (<-chan Event, error)
}

// Confidence is a detector's self-reported certainty in [0.0, 1.0].
// Formats below ConfidenceMinDetect (0.6) are rejected.
type Confidence float64
```

The `<-chan Event` return is deliberate: streaming is first-class.
Parsers emit events as they find them; the pipeline doesn't wait for EOF.

### Pipeline

```
stdin ──▶ TeeReader (sample for detect) ──▶ Format.Parse() ──▶ chan Event
                                                                    │
                                                                    ▼
                                                              Dedupe filter
                                                                    │
                                                                    ▼
                                                              Frame collapse
                                                                    │
                                                                    ▼
                                                              Budget enforcer
                                                                    │
                                                                    ▼
                                                              Output encoder ──▶ stdout
```

Each stage is a goroutine reading from a channel and writing to the next.
Backpressure handled naturally. Cancellation via `context.Context`.

Implementation in `internal/pipeline/`:

- **`Source`** produces Events. `FormatSource` wraps a `Format.Parse`.
- **`Stage`** transforms an Event stream. The shipped stages are
  `CollapseStage` (M5, drops vendor-frame runs and counts them in
  `FramesCollapsed`), `DedupeStage` (M5, bounded-LRU collapse of
  identical Events into a single `Count > 1` entry), and
  `BudgetStage` (M6, caps the estimated token cost of the stream
  and records per-run stats on a shared `BudgetCounters`).
  `PassthroughStage` is the no-op identity, used in tests.
- **`Sink`** consumes the tail of the stream. Encoders (M7) are Sinks.
- **`Pipeline`** wires one Source, an ordered list of Stages, and one
  Sink. `Pipeline.Run(ctx)` is the entry point. The exported
  `Pipeline.BudgetCounters` field, populated by `Build` when
  `Options.Budget > 0`, is the Sink's window into what `BudgetStage`
  observed during the run.
- **`Build(src, sink, Options{})`** is the supported constructor; it
  returns a Pipeline with `[CollapseStage, DedupeStage]` pre-wired in
  the documented order (collapse before dedupe so dedupe signatures
  key on the post-collapse frame layout). When `Options.Budget > 0`,
  `BudgetStage` is appended to the chain and `Pipeline.BudgetCounters`
  is populated; otherwise the field is nil. Build returns
  `(*Pipeline, error)` because the `Tokenizer` option can fail to
  resolve, and a misconfigured run must surface that before any
  goroutine starts. Field-level Pipeline construction is reserved
  for tests substituting custom Stages.
- A single `BufferSize` (default 16) sizes the relay channel from the
  Source and propagates down the chain via `cap(in)` so every
  inter-stage channel is equally bounded.

Vendor-frame classification is centralised in `internal/event`; the
pattern catalogue lives in [docs/formats/vendor-frames.md](./docs/formats/vendor-frames.md)
so format authors add patterns in one place instead of per-format
tables.

### Autodetection

1. Read first 4 KiB of input (`detect.SampleSize`); the sample is
   buffered so no bytes are lost from the original stream.
2. Run `Detect(sample)` on every registered format in parallel. The
   generic format is excluded from the candidate set up front so it
   cannot win a tie on confidence alone; it is reserved for the
   fallback path.
3. Pick the highest-confidence format ≥ `event.ConfidenceMinDetect`
   (0.6). When two specific formats score identically the
   alphabetically-earlier `Name()` wins, so detection is
   deterministic across runs (Go map iteration is randomised).
4. If the winner is below 0.6:
   - `--strict`: return `detect.ErrNoFormat`, mapped to exit code 2 by
     the CLI.
   - Default: fall back to the format registered under the name
     `"generic"`, marking `Result.FellBackToGeneric = true`.
5. The detector returns the chosen format plus an `io.Reader`
   (`Result.Stream`) that yields the sampled bytes followed by the
   rest of the original input. The pipeline hands that reader to
   `Format.Parse` without losing the sample.

Signatures are cheap regex matches on known markers:
- pytest: `=== FAILURES ===`
- go test: `--- FAIL:`
- jest: `●` markers
- k8s/structured: JSON-per-line with `level`/`severity` fields

### Streaming behaviour

- `--dedupe` uses a bounded LRU keyed by event signature
  (`hash(title + location)`). Signature is FNV-64a over a sentinel byte
  plus the Title and (when set) `File:Line`; see `event.Signature`.
- The dedupe stage emits each Event downstream exactly once — at LRU
  eviction or when the upstream channel closes. No two-emit pattern;
  encoders see one Event per signature with the final `Count`. The
  cost is per-event latency: an Event is delayed in the LRU until
  either `--dedupe-window=N` more distinct events arrive or the input
  closes. `--dedupe-window=0` disables dedupe (every Event passes
  through with `Count=1`).
- Output emitters write incrementally as events arrive.
- JSON emitter uses `ndjson` (one event per line) when input is unbounded;
  switches to canonical JSON when input is bounded (file mode).

### Budget enforcement

When `--budget=N` is set, `BudgetStage` (in `internal/pipeline/budget.go`)
caps the estimated cost of the Event stream at N tokens.

1. Estimate tokens per event via `tokens.Estimator`.
2. Buffer the full input — severity-priority ordering can't be decided
   streaming, so `BudgetStage` is the one stage that deliberately
   breaks the streaming-first invariant. `--budget` is only meaningful
   for bounded input.
3. Sort buffered events by descending severity (`error` → `warn` →
   `info`); break ties by arrival order. Determinism is preserved.
4. Emit greedily until the remaining budget would be exceeded. The
   stage holds back `Reserve` tokens (default
   `DefaultBudgetReserve = 30`) so the Sink (M7) always has room for
   a summary line.
5. If a single high-priority event exceeds the remaining budget but
   its Title + Location + one body line + the sentinel
   `BudgetTruncationSentinel` fits, the event is emitted with
   `Body=[first-line, "... [truncated by --budget]"]` and
   `truncated: true`. Otherwise it is dropped.
6. With `Budget == 0` the stage degrades to a streaming pass-through;
   counters still track what passed through so the Sink can render a
   footer.
7. Drops and truncations are tracked on a `BudgetCounters` value
   shared with the Sink. The Sink (and M14 library callers) read
   counters after `Pipeline.Run` returns. Exit code 3 wiring lands
   in M6.3.

### Token estimation

Cross-model tokenization has no universal answer. GPT-4, Claude, Llama,
Gemini all tokenize differently. Real tokenizers need ~1-2MB vocab files
and are 10-100× slower than heuristics. The default must be fast, small,
and dependency-free.

Asymmetric cost: **underestimating is worse than overestimating.** A 20%
underestimate can overflow a model's context window. A 20% overestimate
just wastes some headroom. The default heuristic biases toward
overestimation.

```go
type Estimator interface {
    Estimate(s string) int
}

// Default: word+symbol heuristic with the +10% safety margin from
// tokens.DefaultSafetyMargin. ±15% accurate, zero deps, instant startup.
// Implementation: tokens.Heuristic with WordTokenRatio=1.3.
func Default() Estimator

// Opt-in: real BPE tokenizer (cl100k_base, OpenAI/Claude).
// Exact for GPT, ~95% for Claude, ~85% for Llama/Gemini.
// Adds ~2MB to binary, 50-100ms startup. The vocab is embedded
// via tiktoken-go-loader's offline loader — zero network on first
// init, enforced by TestTiktoken_NoNetwork at build time.
func Tiktoken() (Estimator, error)
```

Flag: `--tokenizer=heuristic|tiktoken` (default: heuristic).

We deliberately do **not**:
- Auto-detect the target model (no reliable signal from stdin).
- Support every tokenizer (diminishing returns, infinite maintenance).
- Expose token counts as a primary feature (budget is the user concept).

Tiktoken ships compiled into the binary (~5MB total). Lazy-loading isn't
practical in Go; the simplicity of one binary wins over the size savings
of splitting.

## Format plugin contract

A new format adds one file:

```go
// internal/formats/rails/rails.go
package rails

import (
    "context"
    "io"

    "github.com/vail130/distill-ai/internal/event"
    "github.com/vail130/distill-ai/internal/formats"
)

func init() {
    formats.Register(&Format{})
}

type Format struct{}

func (f *Format) Name() string { return "rails" }

func (f *Format) Detect(sample []byte) event.Confidence {
    // regex / heuristic checks
}

func (f *Format) Parse(ctx context.Context, r io.Reader, opts formats.ParseOpts) (<-chan event.Event, error) {
    // scanner loop, emit events; close channel on EOF or ctx cancel
}
```

`formats.Register` panics on duplicate names or nil / empty-name
Formats; both are programmer errors caught at init time rather than at
runtime. Get and All are the read APIs: `Get(name)` for CLI lookup,
`All()` for the detector's parallel fan-out and `list-formats`. All
returns formats sorted alphabetically by Name so output ordering is
reproducible across runs.

Registry picks it up via `init()`. No central list to edit.

## Out of scope (v1)

- **Interactive TUI.** `lnav` exists.
- **Log shipping / multi-source following.** Stay a filter.
- **Syntax highlighting.** Output targets LLMs primarily; humans can pipe
  through `bat` if they want colour.
- **Persistent cache.** Stateless. Could be a sibling tool later.
- **Regex rules engine for end users.** Custom formats via TOML config
  cover 90% of need; the rest is a Go plugin.
- **Network anything.** Hard rule.

## v1 scope

**Formats shipped:** `pytest`, `jest`, `gotest`, `generic`.

These cover ~70% of agent-debugging use cases. `k8s` and structured-`json`
logs land in v1.1.

**Token estimator:** heuristic default + tiktoken opt-in.

**Output formats:** text, json, markdown.

**MCP server:** deferred to v1.2. Useful but adds scope.

**Source-code distillation:** deferred to v1.3. The `Event` / `Format`
machinery generalises naturally to source files (one Event per
exported symbol, signature, type definition) but the language parsers
and binary-size implications are large enough to defer past v1.0. See
[TODO.md § v1.3](./TODO.md#v13--code-distillation) for milestones
M17–M21, and
[ADR-0001](./docs/decisions/0001-reject-cgo-tree-sitter-prefer-wasm.md)
for the parser-toolchain decision.

## Dependencies

Lean by design.

- **CLI:** `spf13/cobra` (subcommands, completions, mature).
- **Config:** `BurntSushi/toml` (lighter than viper; we don't need viper's
  multi-source merging).
- **Token estimation:** `pkoukk/tiktoken-go` (the BPE encoder) plus
  `pkoukk/tiktoken-go-loader` (offline vocab loader so first init
  doesn't hit the network). Used only when `--tokenizer=tiktoken` is
  selected; the default heuristic estimator has zero dependencies.
  Transitive: `dlclark/regexp2`. All pure Go, MIT, no CGo.
- **JSON:** stdlib `encoding/json`.
- **Testing:** stdlib + golden files under `testdata/`. Every format
  ships 5-10 fixture inputs with expected outputs.

Avoid: heavy logging libraries, ORM-style stream processors, anything
that buffers, anything that pulls in CGo.

## Testing strategy

- **Golden files per format.** `testdata/pytest/case-01.input` →
  `testdata/pytest/case-01.expected`. Diff-on-fail.
- **Streaming tests.** Feed input byte-by-byte through a slow reader,
  verify events emit as expected without waiting for EOF.
- **Budget tests.** Verify `--budget=N` never exceeds N tokens of actual
  output (using the same estimator).
- **Determinism tests.** Same input twice → byte-identical output.
- **Cross-format detection.** Mixed input (e.g., go test output that
  embeds JSON logs) detects the dominant format correctly.

No mocks. Real parsers on real fixture data.
