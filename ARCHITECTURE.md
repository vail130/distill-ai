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

### Autodetection

1. Read first 4KB of input via `TeeReader` (so it's not consumed).
2. Run `Detect(sample)` on every registered format in parallel.
3. Pick highest confidence ≥ 0.6. Ties broken by specificity (specific
   formats beat `generic`).
4. If max confidence < 0.6 and `--strict`, exit 2. Otherwise fall back to
   `generic`.
5. Resume reading from the buffered sample + remaining stream.

Signatures are cheap regex matches on known markers:
- pytest: `=== FAILURES ===`
- go test: `--- FAIL:`
- jest: `●` markers
- k8s/structured: JSON-per-line with `level`/`severity` fields

### Streaming behaviour

- `--dedupe` in stream mode uses a bounded LRU keyed by event signature
  (`hash(title + location)`).
- Output emitters write incrementally as events arrive.
- JSON emitter uses `ndjson` (one event per line) when input is unbounded;
  switches to canonical JSON when input is bounded (file mode).
- Periodic dedupe flush every N events or M seconds in stream mode.

### Budget enforcement

When `--budget=N` is set:

1. Estimate tokens per event via `tokens.Estimator`.
2. Greedily emit highest-severity events until budget would be exceeded.
3. If a single high-priority event exceeds budget, truncate its body and
   mark `truncated: true`.
4. Always emit summary footer (~30 tokens) so consumer knows what dropped.
5. Exit code 3 if budget forced drops.

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

// Default: word+symbol heuristic with +10% safety margin.
// ±15% accurate, zero deps, instant startup.
func Default() Estimator

// Opt-in: real BPE tokenizer (cl100k_base, OpenAI/Claude).
// Exact for GPT, ~95% for Claude, ~85% for Llama/Gemini.
// Adds ~2MB to binary, 50-100ms startup.
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

## Dependencies

Lean by design.

- **CLI:** `spf13/cobra` (subcommands, completions, mature).
- **Config:** `BurntSushi/toml` (lighter than viper; we don't need viper's
  multi-source merging).
- **Token estimation:** `pkoukk/tiktoken-go` for opt-in tiktoken support.
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
