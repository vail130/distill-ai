# distill-ai

**Turn noisy command output into structured Events for LLM consumption.**
Unix filter. No proxy, no network, no state. JSON output is a versioned
public API.

v1.0 ships five format parsers — `gotest`, `gotestsum`, `pytest`,
`jest`, `generic` — plus CI envelope strippers that
peel wrapper noise before format detection:

```bash
go test ./... 2>&1 | distill-ai        # gotest: --- FAIL blocks, panics, race reports
gotestsum -- ./... 2>&1 | distill-ai   # gotestsum: === Failed summaries, DONE rollups
pytest 2>&1 | distill-ai               # pytest: =/= FAILURES =/= blocks, tracebacks
npx jest 2>&1 | distill-ai             # jest:   ● failure markers, snapshot diffs
tail -f app.log | distill-ai           # generic: ERROR / FATAL / Traceback fallback
gh run view --log | distill-ai         # auto-strips the GitHub Actions envelope
glab ci trace | distill-ai             # auto-strips the GitLab CI envelope
```

Pipe it to an AI coding agent, paste it into chat, or consume the JSON
from your own tooling.

## Why

A 50,000-line test or build log typically contains a few hundred lines
of signal — the actual failures with their assertion messages,
`file:line` locations, and the context that explains them. The other
49,000+ lines are passing-test chatter, vendor stack frames,
deprecation warnings, and progress dots — and a coding agent pays for
every one of those lines in input tokens, spends seconds parsing the
noise before it can reason about the real failure, and frequently
latches onto the wrong error because the real one is buried.

`distill-ai` sits in the pipe between the command and the agent. It
detects the format, parses it, and emits a compact stream of typed
Events — severity, kind, file:line, frames, body, context — while
collapsing vendor frames and deduplicating identical events.

<!-- distill-ai-stats:gha-gotest-fail -->

Concrete: a `go test` run inside GitHub Actions, before vs. after:

| metric          | before | after | reduction |
| --------------- | -----: | ----: | --------: |
| lines           |     11 |    13 |       n/a |
| estimated tokens |   251 |   105 |     **58%** |

(Numbers from `test/integration/testdata/fixtures/gha-gotest-fail.input`;
regenerate with `make readme-stats`. Production logs typically run
~50–90% reduction; this fixture is small to keep the integration
suite fast.)

<!-- /distill-ai-stats:gha-gotest-fail -->

The token saving compounds: every command an agent runs is 50–90%
cheaper, and the smaller window leaves more room for code context.

## Install

```bash
# Homebrew (macOS, Linux)
brew install vail130/distill-ai/distill-ai

# Go (any platform with a Go toolchain)
go install github.com/vail130/distill-ai/cmd/distill-ai@latest

# Direct download (linux/macos/windows × amd64/arm64)
# https://github.com/vail130/distill-ai/releases
```

The Homebrew formula and the `.deb` / `.rpm` packages bundle the man
pages and shell completions. After a Homebrew install, `man distill-ai`
and `distill-ai completions zsh` work out of the box.

For `go install` users, the man pages live at `man/man1/` in the
source tree; copy them to `/usr/local/share/man/man1/` if `man
distill-ai` should resolve.

## Usage

The common case is a Unix filter — read stdin, autodetect the format,
write distilled output to stdout:

```bash
pytest 2>&1 | distill-ai
```

Worked examples for each shipped format, drawn from
`test/integration/testdata/fixtures/`:

### gotest

```bash
go test ./... 2>&1 | distill-ai
```

Input (excerpt; see `testdata/fixtures/gotest-fail.input` for the full
8-line fixture):

```
=== RUN   TestThing
    thing_test.go:42: expected 200, got 500
--- FAIL: TestThing (0.01s)
FAIL	github.com/example/project/thing	0.123s
```

Output:

```
events from gotest

[1] ERROR thing_test.go:42: expected 200, got 500
  at thing_test.go:42
  ...

---
distilled 8 lines → 10 lines (90 tokens, heuristic)
```

`gotest` emits four Event kinds: `test_failure`, `panic`,
`build_failure`, `race_condition`. Subtest paths, package names, and
goroutine-dump frames land in `metadata` and `frames`. See
[`docs/formats/gotest.md`](./docs/formats/gotest.md).

### pytest

```bash
pytest 2>&1 | distill-ai
```

A `=== FAILURES ===` block becomes one Event per failure with the
assertion line as `Title`, the traceback as `Body`, the test ID
(e.g., `tests/test_auth.py::test_login_redirect`) in `metadata`, and
the bottom user-code frame as `Location`. The four `--tb` shapes
(`long`, `short`, `line`, `native`) and the `=== ERRORS ===` /
`=== warnings summary ===` blocks are all handled.

Honours `--keep-warnings` and `--severity=warn` for warning passthrough.

See [`docs/formats/pytest.md`](./docs/formats/pytest.md).

### jest

```bash
npx jest 2>&1 | distill-ai
```

The `●` failure marker becomes one Event per failure (`test_failure`).
Snapshot mismatches get their own `snapshot_mismatch` kind so
downstream consumers can render the diff specially.
`● Test suite failed to run` blocks (file-load syntax errors, missing
modules) become `suite_error` Events.

See [`docs/formats/jest.md`](./docs/formats/jest.md).

### generic (fallback)

```bash
tail -f app.log | distill-ai          # autodetects as generic
kubectl logs my-pod | distill-ai
```

When no specific format matches, `generic` anchors on severity markers
(`ERROR`, `FATAL`, `WARN`, `panic:`, `Traceback`) and emits one Event
per anchor with surrounding context. Python tracebacks, Go panics, and
JVM exception blocks are accumulated as block Events with frames
extracted. ANSI escapes are stripped from `Title`; `Body` keeps them
so users see what was emitted.

See [`docs/formats/generic.md`](./docs/formats/generic.md).

### Envelopes (CI strippers)

```bash
gh run view --log | distill-ai                # auto-strip
glab ci trace | distill-ai                    # auto-strip
distill-ai --strip-envelope=github-actions    # force a specific stripper
distill-ai --strip-envelope=none              # opt out
```

The envelope strippers run **before** format detection. A wrapped
`go test` log still detects as `gotest` because the cleaned bytes
the detector sees are exactly what `go test` emitted. Wrapper-level
signals — a step exiting non-zero, a `##[error]` directive — surface
as Events with the dedicated `envelope_*` kinds alongside the
parser's own events. See [`docs/envelope.md`](./docs/envelope.md).

### Other useful flags

```bash
# Fit output to a token budget (drops lowest-severity Events first)
pytest 2>&1 | distill-ai --budget=2000

# JSON output for tooling
pytest 2>&1 | distill-ai --output=json | jq .

# Markdown output for pasting into chat
pytest 2>&1 | distill-ai --output=markdown

# Verbose: see which format the detector picked, on stderr
pytest 2>&1 | distill-ai -v

# Dry-run: see what's kept vs dropped, no distilled output
distill-ai explain pytest-output.log

# Identify a file's format without running the full pipeline
distill-ai detect pytest-output.log

# List every format the binary knows about
distill-ai list-formats

# Shell completion (bash | zsh | fish | powershell)
source <(distill-ai completions zsh)
```

### Exit codes

| Code | Meaning                                                              |
| ---- | -------------------------------------------------------------------- |
| `0`  | Success: at least one event was emitted.                             |
| `1`  | Success but no events found (input was clean).                       |
| `2`  | Error: bad flags, IO error, or autodetect failed under `--strict`.   |
| `3`  | Partial: ran successfully but dropped or truncated to fit `--budget`. |

## Supported formats

- `generic` — regex-driven fallback. Severity markers, Python
  tracebacks, Go panics, JVM stack dumps. Documented at
  [`docs/formats/generic.md`](./docs/formats/generic.md).
- `gotest` — `go test` output, including the `-json` reporter,
  panic blocks, build failures, and race-detector reports.
  Documented at [`docs/formats/gotest.md`](./docs/formats/gotest.md).
- `gotestsum` — gotestsum-style Go test summaries, including
  `=== Failed` / `=== FAIL:` blocks, `DONE ...` rollups, and
  package-level test-binary flag errors. Documented at
  [`docs/formats/gotestsum.md`](./docs/formats/gotestsum.md).
- `pytest` — `=== FAILURES ===` / `=== ERRORS ===` blocks, all
  four `--tb` shapes, parametrised tests, collection-phase failures,
  warning summaries. Documented at
  [`docs/formats/pytest.md`](./docs/formats/pytest.md).
- `jest` — `●` failure blocks, file-backed and inline snapshot
  mismatches, `● Test suite failed to run`, default and CI
  reporters. Documented at
  [`docs/formats/jest.md`](./docs/formats/jest.md).

Envelope strippers (run before detection):

- `github-actions` — peels per-line timestamps, `##[group]` /
  `##[endgroup]` markers, and the workflow-command directives.
  Translates `##[error]Process completed with exit code N` into
  `envelope_step_failure`.
- `gitlab-ci` — peels `section_start:` / `section_end:` markers
  and the trailing carriage returns the GitLab runner emits.
  Translates `ERROR: Job failed: exit code N` into
  `envelope_step_failure`.

Use `distill-ai list-formats` to see what's wired into your binary,
and `distill-ai detect FILE` to ask the autodetector which envelope
and inner format it picks for a given input.

## What distill-ai is, and is not

distill-ai is a **format-aware structured event extractor** for
command output. It is **not** a general CLI proxy or command
wrapper.

|                                  | distill-ai                                | proxy/wrapper tools                     |
| -------------------------------- | ----------------------------------------- | --------------------------------------- |
| **Position**                     | Unix filter — sits in a pipe              | Wraps the command (`tool git status`)   |
| **Forks the underlying command** | Never                                     | Always                                  |
| **Works on `tail -f`, log files, library use** | Yes                         | No (their model is fork-exec-capture)   |
| **Output**                       | Versioned structured Events (JSON schema) | Compressed text                         |
| **State / network / telemetry**  | None. Ever.                               | Varies                                  |
| **Format model**                 | Per-format parser emitting typed Events   | Per-command filter emitting text        |

If your problem is "my agent runs 50 different shell tools and
they're all noisy", a proxy/wrapper tool like [rtk] or [snip] is
probably what you want. If your problem is "I need an agent — or my
own tooling — to programmatically reason about test failures, stack
traces, and logs as typed events with locations and severities",
distill-ai is what you want. The two are complementary.

[rtk]: https://github.com/rtk-ai/rtk
[snip]: https://github.com/edouard-claude/snip

## Integration with coding agents

distill-ai does not ship per-agent hooks or `init` subcommands; it
deliberately stays out of the command-execution path (see
[ADR-0003](./docs/decisions/0003-position-vs-rtk-and-snip.md)). The
agent integration pattern is **documentation**: instruct the agent
via its project rules file to pipe through `distill-ai` when running
noisy commands.

Add the following block to `AGENTS.md`, `CLAUDE.md`, `.cursorrules`,
`GEMINI.md`, or your agent's equivalent project-rules file:

```markdown
When running tests, build commands, or tailing logs, pipe through
distill-ai to keep the context window lean:

  pytest 2>&1 | distill-ai
  npm test 2>&1 | distill-ai
  go test ./... 2>&1 | distill-ai
  kubectl logs <pod> | distill-ai --dedupe

For a strict token cap (e.g., fit a CI run in 2000 tokens):
  pytest 2>&1 | distill-ai --budget=2000

To inspect what distill-ai dropped before trusting it:
  pytest 2>&1 | distill-ai explain
```

The agent reads its rules file at session start and applies the
pattern to every applicable command for the rest of the session.

For opencode, the project ships
[`skills/distill-ai/SKILL.md`](./skills/distill-ai/SKILL.md) — a
self-contained, agent-agnostic skill that loads automatically when
output volume warrants it. To make the skill available in opencode
sessions outside this repo:

```bash
make install-skill
```

Per-agent integration recipes for Claude Code, opencode, and CI
systems land in `docs/integration-*.md` (M16.4); see TODO.md for
status.

## Embedding in Go

`distill-ai` ships a stable Go library at
[`pkg/distill`](./pkg/distill) for code that wants to embed the
distillation pipeline without shelling out to the binary. The full
surface is one function and four types:

```go
import "github.com/vail130/distill-ai/pkg/distill"

events, summary, err := distill.Distill(ctx, os.Stdin,
    distill.Options{Writer: os.Stdout},
)
if err != nil {
    log.Fatal(err)
}
for range events { /* or consume Events for routing */ }
summary.Wait()
os.Exit(distill.ExitCodeFromSummary(summary))
```

The Event channel publishes structured Events as the pipeline emits
them; the Writer receives the encoded output in parallel. Setup
errors (nil Writer, unknown format, unknown tokenizer) surface
synchronously via `error`; mid-stream parser problems degrade to
best-effort Events. See [docs/library-api.md](./docs/library-api.md)
for the full reference: consumption patterns, Summary timing,
exit-code mapping, the `os/exec` migration guide, and version-
pinning advice.

## Design principles

distill-ai's shape is deliberate. The seven principles in
[ARCHITECTURE.md § Design principles](./ARCHITECTURE.md#design-principles)
shape every decision:

1. **Unix-pipe-native.** stdin → stdout is the default path.
2. **Zero-config common case.** Autodetection picks the format.
3. **Deterministic output.** Same input → same output, byte for byte.
4. **Streaming-first.** Never require buffering the full input.
5. **Format plugins, not hardcoded formats.** Adding a format = one Go
   file. The format registers itself.
6. **Honest about what it dropped.** A footer summarises every
   collapse, dedupe, and drop.
7. **No network. Ever.** No telemetry, no updates, no remote lookups.
   The tool is usable in air-gapped CI and regulated environments
   without additional review.

See [ADR-0003](./docs/decisions/0003-position-vs-rtk-and-snip.md) for
the explicit position decision against the proxy/wrapper model.

## Status

v1.0 release prep in progress; release tag landing in M17. The v1.0
contract is recorded in
[ADR-0002](./docs/decisions/0002-v1.0-scope-and-post-v1.0-roadmap.md);
see [TODO.md](./TODO.md) for the milestone status, and
[ARCHITECTURE.md](./ARCHITECTURE.md) for the design.

**What v1.0 is:** four runtime-failure formats (gotest, pytest, jest,
generic), two CI envelope strippers (github-actions, gitlab-ci), a
streaming pipeline with dedupe / vendor-collapse / budget enforcement,
versioned JSON / text / markdown output, a configuration file, a
stable Go library API at `pkg/distill`, and man pages.

**What v1.0 is not — and won't grow before tagging:**

- **No static-analysis formats** (`golangci-lint`, `cargo`, `rustc`).
  Deferred to v1.1. See
  [TODO.md § M23, M24](./TODO.md#v11--static-analysis--linting-post-launch).
- **No MCP server.** Deferred to v1.2.
- **No source-code distillation.** Deferred to v1.3
  ([M18–M21](./TODO.md#v13--code-distillation)).
- **No Markdown / HTML doc formats.** Deferred to v1.4.
- **No additional log / test formats** (`k8s`, generic JSON logs,
  `npm` / `yarn`, `rspec`, `mocha`). Deferred to v1.5.
- **No filter-engine parity features** (`discover` subcommand,
  `--ultra-compact`, drop-side log, custom TOML formats).
  Deferred to v1.6
  ([M26–M30](./TODO.md#v16--filter-engine-parity-post-launch)).

The deferrals are deliberate: every v1.0 format and stripper is
solving a runtime-failure problem an agent or human actually has
*today* (failed test, panicking binary, broken CI run). Static
analysis, code-level distillation, and source-language parsers
are valuable but architecturally distinct, and shipping them
inside v1.0 would push the release surface beyond what one
contributor can stabilise.

## Documentation

Man pages live under [`man/man1/`](./man/man1/) and are bundled into
the `.deb` / `.rpm` artefacts goreleaser produces. After installing
via one of those packages — or copying the pages to
`/usr/local/share/man/man1/` manually after a `go install` — `man
distill-ai` and the per-subcommand pages (`man distill-ai-run`,
`man distill-ai-detect`, etc.) work.

The pages are generated from the cobra command tree by `go run
./cmd/distill-ai/gen-man` (also available as `make man`); they are
checked into the repo so distributions install them without
re-running the generator on the host.

The per-feature reference set lives under [`docs/`](./docs/):
[SCHEMA.md](./docs/formats/SCHEMA.md) (the JSON output contract),
[envelope.md](./docs/envelope.md) (CI envelope strippers),
[explain.md](./docs/explain.md) (dry-run mode),
[config.md](./docs/config.md) (configuration files),
[library-api.md](./docs/library-api.md) (`pkg/distill`),
[decisions/](./docs/decisions/) (ADRs).

## Inspiration and prior art

distill-ai is shaped by — and explicitly differentiates from — two
existing tools in the adjacent niche:

- **[rtk]** (Rust Token Killer, 49k+ stars). The dominant
  general-purpose solution for reducing agent token consumption. CLI
  proxy / wrapper model. 100+ hand-tuned filters. Auto-rewrite hooks
  for 13+ AI coding agents.
- **[snip]** (Go, ~240 stars). An rtk alternative whose
  differentiation is declarative YAML filters: 126 of them, composed
  from 19 pipeline actions.

Both are excellent at the breadth play — the agent-shell-loop case
where 50 different commands need filters. distill-ai is intentionally
narrower: depth-first format-aware parsing that produces typed Events
with locations and severities, suitable for programmatic reasoning by
agents or downstream tools. See
[ADR-0003](./docs/decisions/0003-position-vs-rtk-and-snip.md) for the
full comparison and the position decision.

## License

See [LICENSE](./LICENSE).
