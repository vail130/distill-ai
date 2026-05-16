# TODO

Implementation roadmap for `distill-ai`. Tasks are grouped by milestone
and ordered roughly by dependency. Tick items as they land.

See [ARCHITECTURE.md](./ARCHITECTURE.md) for the design that drives this
list and [AGENTS.md](./AGENTS.md) for code/commit conventions.

---

## M0 — Project scaffolding

- [x] `go.mod` with module path `github.com/vail130/distill-ai`
- [x] Go version pin (1.26)
- [x] `cmd/distill-ai/main.go` minimal entry point
- [x] Top-level `Makefile` with `build`, `test`, `lint`, `install`, `tidy`, `bench`, `release-dry-run`
- [x] `.golangci.yml` linter config (v2 schema)
- [x] GitHub Actions: build + test + lint on push (linux/darwin/windows matrix)
- [x] Release workflow: cross-compile linux/darwin/windows × amd64/arm64 via goreleaser
- [x] `goreleaser` config for tagged releases
- [ ] Decide and document binary distribution: Homebrew tap, GitHub Releases, `go install` (deferred to M16)

---

## M1 — Core types & interfaces

- [ ] `internal/event/event.go`: `Event`, `Severity`, `Location`, `StackFrame` types
- [ ] `internal/event/event_test.go`: round-trip serialisation tests
- [ ] `internal/formats/format.go`: `Format` interface, `Confidence`, `ParseOpts`
- [ ] `internal/formats/registry.go`: thread-safe `Register` + `All` + `Get(name)`
- [ ] `internal/formats/registry_test.go`: registration, duplicate detection, lookup

---

## M2 — Pipeline plumbing

- [ ] `internal/pipeline/pipeline.go`: orchestrates detect → parse → dedupe → collapse → budget → emit
- [ ] Context cancellation propagation through every stage
- [ ] Backpressure handling via bounded channels
- [ ] `internal/pipeline/pipeline_test.go`: end-to-end with a fake format
- [ ] Determinism test: same input twice → byte-identical output
- [ ] Streaming test: bytes arrive over time, events emit incrementally

---

## M3 — Format autodetection

- [ ] `internal/detect/detect.go`: 4KB `TeeReader` sample, parallel `Detect()` fan-out
- [ ] Confidence tie-breaking: specificity > generic
- [ ] `--strict` mode: exit 2 when max confidence < 0.6
- [ ] `internal/detect/detect_test.go`: mixed-format inputs, edge cases (empty, binary, single byte)
- [ ] `distill-ai detect FILE` subcommand: print format + confidence + reasoning

---

## M4 — Token estimation

- [ ] `internal/tokens/estimate.go`: `Estimator` interface
- [ ] Heuristic estimator: word + symbol counting, +10% safety margin
- [ ] Heuristic estimator benchmarks (target: ≥100 MB/sec)
- [ ] Tiktoken estimator: lazy-init `cl100k_base` vocab
- [ ] `--tokenizer=heuristic|tiktoken` flag wiring
- [ ] Accuracy tests: golden corpus with known GPT-4 token counts, assert ±15% heuristic / exact tiktoken
- [ ] Document accuracy expectations in `--help` text

---

## M5 — Event processing

- [ ] `internal/event/dedupe.go`: bounded LRU keyed by `hash(title + location)`
- [ ] `--dedupe-window=N` flag wiring
- [ ] Stream-mode dedupe: emit collapsed `Count: N` periodically
- [ ] Batch-mode dedupe: post-process before emit
- [ ] `internal/event/collapse.go`: stack frame collapsing
- [ ] Vendor-frame detection: configurable patterns per language
  - Python: `site-packages/`, `dist-packages/`, stdlib paths
  - Node: `node_modules/`
  - Go: `GOROOT`, `vendor/`, `pkg/mod/`
  - Java/JVM: package prefixes (`java.`, `sun.`, `org.junit.`)
- [ ] `--keep-vendor` flag wiring
- [ ] Frame collapse tests per language

---

## M6 — Budget enforcement

- [ ] `internal/pipeline/budget.go`: greedy emit by severity until budget hit
- [ ] Single-event-exceeds-budget: truncate body, mark `truncated: true`
- [ ] Footer always emitted (~30 token reserve)
- [ ] Exit code 3 when budget forces drops
- [ ] Tests: assert output never exceeds `--budget=N` by more than estimator margin
- [ ] Tests: footer present even when all events dropped

---

## M7 — Output encoders

- [ ] `internal/output/text.go`: default compact format
- [ ] `internal/output/json.go`: stable schema; bounded → JSON, streaming → ndjson
- [ ] `internal/output/markdown.go`: headings + fenced blocks
- [ ] Footer rendering per format
- [ ] `--no-footer` flag wiring
- [ ] Schema versioning constant + tests
- [ ] Golden output tests for all three formats

---

## M8 — CLI surface

- [ ] `cmd/distill-ai/flags.go`: cobra flag definitions
- [ ] `cmd/distill-ai/run.go`: wires flags → pipeline opts
- [ ] Positional `FORMAT` + `FILE...` parsing
- [ ] Stdin/file input handling (multi-file = concatenated stream)
- [ ] Exit code mapping (0/1/2/3) per ARCHITECTURE
- [ ] `--help` text matches ARCHITECTURE flag list
- [ ] `--version` from build-time ldflags
- [ ] `-v` / `--verbose` writes pipeline diagnostics to stderr

### Subcommands

- [ ] `list-formats`: prints registered formats with version/source
- [ ] `detect FILE`: prints chosen format + confidence + runner-up
- [ ] `explain FILE`: dry-run; annotates kept/dropped/why
- [ ] `completions [bash|zsh|fish]`: generate shell completion
- [ ] `version`: build version + commit + date

---

## M9 — Generic format (fallback)

- [ ] `internal/formats/generic/generic.go`: regex-based error/warning detection
- [ ] Heuristics: lines matching `ERROR|FATAL|panic|Exception|Traceback`, severity keywords
- [ ] Context capture: N lines before/after match
- [ ] Confidence: always returns low value (loses to specific formats)
- [ ] Fixtures: 10+ cases covering mixed/unknown log shapes

---

## M10 — pytest format

- [ ] `internal/formats/pytest/pytest.go`
- [ ] `Detect`: `=== FAILURES ===`, `=== test session starts ===` markers
- [ ] Parse failure blocks: test ID, assertion, file:line, source context
- [ ] Parse error blocks (collection errors, fixture failures)
- [ ] Parse short test summary info section
- [ ] Skip passing tests entirely
- [ ] Handle `-v` and non-`-v` output shapes
- [ ] Handle `--tb=short` / `--tb=long` / `--tb=line`
- [ ] Fixtures: clean run, single fail, multi fail, errors, parametrised, xfail/xpass, warnings, collection error

---

## M11 — jest format

- [ ] `internal/formats/jest/jest.go`
- [ ] `Detect`: `●` markers, `FAIL` / `PASS` line prefixes
- [ ] Parse failure blocks: test path, description, diff, stack
- [ ] Snapshot diff handling (multi-line, structured)
- [ ] Handle `--verbose` and default output
- [ ] Coverage table suppression
- [ ] Fixtures: clean, single fail, snapshot mismatch, multiple suites, console.log noise

---

## M12 — go test format

- [ ] `internal/formats/gotest/gotest.go`
- [ ] `Detect`: `--- FAIL:`, `FAIL\t<pkg>` markers
- [ ] Parse `--- FAIL: TestName (Xs)` blocks
- [ ] Parse panic blocks (separate event kind)
- [ ] Parse build failures (separate event kind)
- [ ] Handle `-json` mode (already structured, but distill removes noise)
- [ ] Handle `-v` and non-`-v`
- [ ] Race detector output: extract race report as single event
- [ ] Fixtures: pass, single fail, panic, build error, race, subtests, table-driven

---

## M13 — Config file support

- [ ] `internal/config/config.go`: load `.distill-ai.toml` from CWD upward, then `~/.config/distill-ai/config.toml`
- [ ] Precedence: CLI flag > project config > user config > default
- [ ] Per-format config sections override format defaults
- [ ] Custom regex-based format registration via `[[formats.custom.NAME]]`
- [ ] Config validation with clear errors
- [ ] Tests: precedence, override, malformed config

---

## M14 — Library API

- [ ] `pkg/distill/distill.go`: exported `Distill(ctx, r, opts) (<-chan Event, error)`
- [ ] Stable public API; document in package godoc
- [ ] Examples in `pkg/distill/example_test.go`
- [ ] Mark internal packages as such; nothing leaks except `pkg/distill`

---

## M15 — Documentation

- [ ] `man/distill-ai.1` man page generated from cobra
- [ ] README usage examples expanded with real fixtures
- [ ] `docs/formats/` per-format docs: what's detected, what's dropped, example I/O
- [ ] `docs/integration-claude-code.md`: how to wire into Claude Code
- [ ] `docs/integration-opencode.md`: how to wire into opencode AGENTS.md
- [ ] `docs/integration-ci.md`: piping CI output through distill-ai for failure summaries
- [ ] CHANGELOG.md with semantic versioning

---

## M16 — v1.0 release prep

- [ ] All M0–M15 complete or explicitly deferred
- [ ] `go test ./...` clean, `golangci-lint run` clean
- [ ] Cross-compile verified on linux/darwin/windows × amd64/arm64
- [ ] Binary size budget: ≤6 MB stripped (with tiktoken)
- [ ] Cold-start latency budget: ≤20 ms (heuristic), ≤120 ms (tiktoken)
- [ ] Throughput budget: ≥50 MB/sec single core
- [ ] Tag `v1.0.0`, run `goreleaser`, publish

---

## v1.1 (post-launch)

- [ ] `k8s` format: kubectl logs, structured + unstructured
- [ ] `json` format: generic JSON-per-line logs (Zap, slog, Bunyan, Pino)
- [ ] `npm`/`yarn`/`pnpm` install/build output
- [ ] `cargo` test/build output
- [ ] `rspec` format
- [ ] `mocha` format

---

## v1.2 — MCP server

- [ ] `distill-ai mcp` subcommand: expose tool over MCP stdio transport
- [ ] Tool: `sift(command, format?) -> distilled_output`
- [ ] Tool: `sift_file(path, format?) -> distilled_output`
- [ ] Document setup for Claude Desktop, opencode, Continue, etc.
- [ ] Integration tests against a real MCP client

---

## Backlog (no milestone)

- [ ] Plugin loading from `~/.config/distill-ai/plugins/*.so` (Go plugins are
      fragile; evaluate WASM as alternative before committing)
- [ ] `--watch` mode: re-distill a file when it changes
- [ ] Coloured text output when stdout is a TTY (off by default; LLM consumers want plain)
- [ ] Profile-guided optimisation build
- [ ] Fuzz testing per format parser (`go test -fuzz`)
- [ ] Benchmarks in CI with regression detection
- [ ] `--diff` mode: distill two log files and show only the delta (useful for "what changed between this run and the last passing one?")
- [ ] Format-specific `--summary-only` modes (e.g., "just the failure count and titles")

---

## Explicitly out of scope

These are listed so they don't accidentally creep into the backlog:

- Interactive TUI
- Log shipping / aggregation
- Persistent caching (consider as separate sibling tool)
- Network features of any kind
- Auto-detection of target LLM model
- Built-in support for every conceivable log format (use generic + custom config)
