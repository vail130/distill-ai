# Changelog

All notable changes to `distill-ai` are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- M15.4: `pkg/distill/internal/orchestrator` private subpackage.
  Hosts the bridge between the public `pkg/distill.Options` and
  every internal package the run touches: `internal/detect`,
  `internal/envelope`, `internal/formats`, `internal/output`,
  `internal/pipeline`, `internal/tokens`. Go's `internal/`
  visibility rule keeps the orchestrator unreachable from outside
  the `pkg/distill` subtree, so the public surface
  (`pkg/distill/distill.go`, `options.go`, `errors.go`) stays
  clean while the bridge can freely import internal packages.
  The package exposes `Setup(ctx, Config, Reader) (*Run, error)`
  which validates the Config, resolves format (autodetect or
  explicit), wires envelope stripping, builds the pipeline, and
  returns a `*Run` whose `Start(ctx)` / `Wait()` split lets the
  caller stream Events to its consumer while the pipeline runs,
  then read the Summary once the channel closes. Five sentinel
  errors — `ErrNilReader`, `ErrNilWriter`, `ErrUnknownTokenizer`,
  `ErrUnknownFormat`, `ErrUnknownOutput`,
  `ErrUnknownStripEnvelope` — surface setup failures before any
  goroutine starts. `pkg/distill/register.go` brings the v1
  format set (generic, gotest, jest, pytest) and envelope
  strippers (github-actions, gitlab-ci) into the global registry
  via side-effect imports so library consumers get the same
  default behaviour as the CLI without enumerating each one.
  Twelve orchestrator unit tests cover every sentinel error
  arm, the end-to-end gotest path, explicit-format-beats-detect,
  zero-events exit-code mapping, context cancellation, ndjson
  output, markdown fence-lang, and the MinSeverity propagation
  into ParseOpts. Two integration tests in test/integration/
  guard the layering invariant: `TestPublicAPI_NoLeakedInternalImports`
  parses every .go file directly under pkg/distill/ and asserts
  every internal/* import is either (a) a documented type-alias
  target (internal/event, internal/formats) or (b) a known
  side-effect import for registry registration;
  `TestPublicAPI_OrchestratorIsPrivate` confirms the
  orchestrator directory sits under pkg/distill/internal/ so
  Go's visibility rule protects it.
- M15.1: `pkg/distill` public library API surface. New
  `Options` struct exposes every CLI flag's library equivalent
  (Format, Strict, Output, Budget, Tokenizer, DedupeWindow,
  KeepVendor, KeepWarnings, MinSeverity, MaxEvents, ContextLines,
  StripEnvelope, Writer, NoFooter, FenceLang). New
  `OutputFormat` typed string with constants `OutputText`,
  `OutputJSON`, `OutputJSONStreaming`, `OutputMarkdown` so
  callers choose the encoder without depending on
  `internal/output`. New `Summary` struct mirrors the JSON
  schema's summary object (input_lines, events_emitted,
  events_dropped_budget, etc.) plus a `ForcedDrops()` helper
  that's nil-safe. Five sentinel errors — `ErrNilWriter`,
  `ErrUnknownOutput`, `ErrUnknownTokenizer`, `ErrUnknownFormat`,
  `ErrUnknownStripEnvelope` — surface setup failures before any
  goroutine starts so callers see deterministic errors. The
  existing type aliases (`Event`, `Severity`, `Location`,
  `StackFrame`, `Confidence`, `Format`, `ParseOpts`,
  `SeverityError`/`SeverityWarn`/`SeverityInfo`,
  `ConfidenceMinDetect`) carry over from M1.4 unchanged. The
  `Distill` function lands in M15.2; M15.1 only ships the types
  the function consumes. Tests pin every default, every
  stringer, every ForcedDrops arm (true on drops, true on
  truncations, false on clean run, false on nil receiver), and a
  drift-guard against SCHEMA.md's summary table.
- M14.6: `--max-events` and `--passthrough` plumbed end-to-end;
  closes KNOWN_ISSUES § 1. `--max-events=N` adds a new
  MaxEventsStage to the pipeline that caps Events at N and
  drains the remainder so upstream Stages terminate cleanly;
  the stage runs after BudgetStage so the budget enforcer's
  severity-priority ranking operates on the full stream and the
  cap then trims to the top N. `--passthrough` tees the input
  through a `bytes.Buffer`; when the pipeline emits zero
  Events, the buffered raw input replaces the sink's "no events
  found" output on stdout and the binary exits 0 (not 1). The
  Sink's writer is also buffered in passthrough mode so the
  "no events" header doesn't bleed onto stdout before the
  passthrough decision; the buffer is flushed when events were
  emitted, dropped when not. Both flags pick up defaults from
  the config file: `max_events = N` and `passthrough = true` on
  the top-level Config; per-format overrides not yet exposed
  (the use cases are global). Tests: five MaxEventsStage unit
  tests covering cap-at-N, exact-N, zero/negative disabled,
  context cancellation, and the Build stage-order rule; three
  CLI integration tests for max-events (CLI, config-driven,
  works alongside --budget) and three for passthrough
  (zero-events copy, events suppress copy, config-driven). The
  KNOWN_ISSUES.md § 1 entry is deleted in this commit.
- M14.5: regex-driven custom formats. A `[[formats.custom.NAME]]`
  block in a `.distill-ai.toml` registers a Format that
  participates in autodetection and is invokable by the
  namespaced identifier `custom:NAME`. `internal/formats/custom`
  exports `New(name, detectRegex, eventStart, eventEnd, severity,
  kind)` which compiles each regex once and returns an error
  naming the offending field on failure, plus
  `RegisterFromConfig(blocks)` which the CLI's pre-run hook
  calls atomically after `config.LoadAll`: every block validates
  before any registers, so a single bad regex aborts the lot
  rather than leaving the binary in a half-registered state.
  Parse is a line-by-line scanner; `event_start` opens an Event,
  `event_end` closes it (with the terminator line included in
  Body). An empty `event_end` yields one-line Events. A new
  `event_start` always opens a fresh Event, closing any in-flight
  one — start wins over end on a line that matches both, which
  is the only behaviour that makes the
  "ERROR…INFO…ERROR…INFO" pattern produce two Events instead of
  one. Title is ANSI-stripped; Body keeps the original bytes so
  consumers see what arrived. `Metadata["custom_format"]`
  carries the user's NAME (without the `custom:` prefix) so
  downstream routing can match on it. Five canonical fixtures
  under `internal/formats/custom/testdata/` cover the
  start-and-end pair, the one-line variant, multiple matches,
  custom severity, and custom kind; each carries a
  `<name>.config` sibling because custom formats are runtime-
  configured. Two CLI integration tests prove the
  registered-via-config end-to-end path and the
  bad-regex-fails-at-startup case.
- M14.4: CLI wired to read `.distill-ai.toml` defaults. The root
  command's `PersistentPreRunE` calls `config.LoadAll(os.Getwd(),
  os.UserHomeDir())` and stashes the merged Config on the
  command's context; the `run` subcommand pulls it out and
  applies config values to any flag the user did not explicitly
  pass (verified via `pflag.Flag.Changed`). The precedence
  chain ships intact: CLI flag > project config > user config >
  built-in default. Two new flags: `--config <path>` overrides
  discovery (loads only the named file, skips both the project
  walk and the user config — useful for CI shipping a vetted
  config), and `--print-config` dumps the merged effective
  configuration as TOML and exits without reading stdin or
  running the pipeline. Per-format `[formats.<name>]` blocks
  apply only after the format is resolved (autodetect or
  explicit FORMAT), so the override targets the right parser
  even when the user did not commit to a format upfront. A
  malformed config fails the binary with ExitError before any
  pipeline work happens; the error message names the offending
  path. The SKILL.md `cli-surface` manifest gains `--config` and
  `--print-config` so the integration suite's drift guard tracks
  them.
- M14.2 / M14.3: config discovery and merge. `config.Discover(cwd,
  home)` walks from CWD toward the filesystem root looking for
  `.distill-ai.toml`, stopping at the first match, the first git
  root, the filesystem root, or a 32-directory depth cap.
  Honours `$XDG_CONFIG_HOME/distill-ai/config.toml` falling back
  to `~/.config/distill-ai/config.toml` for the user config.
  `config.LoadAll(cwd, home)` is the convenience wrapper:
  Discover + Load each present file + Merge in precedence order.
  `config.Merge(user, project)` produces a single Config with
  project overrides on top of user; nil-safe on both sides;
  per-format `[formats.NAME]` blocks merge field-by-field via the
  nullable-pointer rule, while `[[formats.custom.NAME]]` blocks
  replace whole. `Config.ApplyToOptions(opts, parseOpts,
  formatName)` writes the merged values onto `pipeline.Options`
  and `formats.ParseOpts`, honouring the
  per-format > top-level > caller-default chain and treating a
  caller's non-zero value as an "already-set explicit flag" that
  wins over config. The CLI integration in M14.4 reads
  ApplyToOptions's output as flag defaults.
- M14.1: `internal/config` package decodes the TOML configuration
  schema documented in `docs/config.md` (and sketched in
  ARCHITECTURE.md § Config file). `Config.Load(path)` and
  `Config.LoadBytes(data)` decode a single file into the in-memory
  shape; unknown keys (typos like `keep_warning` instead of
  `keep_warnings`) fail loudly with the offending dotted key
  named, and an explicit `schema_version` mismatch produces a
  clear "config schema version N not supported by this binary
  (version 1)" error. Per-format `[formats.<name>]` blocks decode
  into a `FormatConfig` whose nullable-pointer fields distinguish
  an explicit zero from an absent key — the precedence chain in
  M14.3 depends on that distinction. `[[formats.custom.NAME]]`
  array tables decode into `CustomFormats[NAME]` with
  `detect_regex` and `event_start` validated as required; regex
  compilation is deferred to M14.5. Adds `BurntSushi/toml` v1.6.0
  as the second project-config dependency (after the cobra /
  pflag pair landed in M8.1).
- M10: `gotest` format — the first specific Format ships
  end-to-end. The detector raises Confidence to 1.0 on
  `--- FAIL:` headers, `FAIL\t<pkg>` summaries (with a Go-package-
  shaped token guard so unrelated tools' `FAIL` lines don't claim
  it), and `=== RUN` headers; to 0.8 on bare goroutine dumps
  with a `.go:N` reference. The scanner is a bufio.Scanner-driven
  state machine emitting four Event kinds: `test_failure`
  (per `--- FAIL:` block, with assertion-derived Title /
  Location), `panic` (with structured StackFrames from the
  goroutine dump and the first non-runtime / non-testing frame
  selected for Location), `build_failure` (one Event per
  `path/to/file.go:line:col:` line), and `race_condition` (the
  `==================`-framed report with both contained
  goroutine stacks merged into Frames). Per-package buffering
  stamps `metadata.package` on every Event without breaking
  cross-package streaming. `panic` / `race_condition` Events
  attributed to a running test suppress the trailing
  `--- FAIL: TestName` to avoid duplicate diagnostics. `-json`
  reporter mode dispatches via the `{"Time":` prefix on the first
  non-blank line: per-test output actions accumulate into a body
  buffer, `fail` emits a `test_failure`, `pass`/`skip` discard,
  per-package `fail` is swallowed, and build-error output
  (Test == "") maps to `build_failure` directly. Eight canonical
  fixtures land under `internal/formats/gotest/testdata/`
  (clean, single-fail, multi-fail, subtests, panic, race,
  build-failure, json) with goldens generated by the shared
  `internal/formats.RunGoldens` harness; the count is pinned by
  `TestGotest_FixtureCount`. Integration suite gains
  `TestBinary_GotestEndToEndProducesOutput` exercising the full
  argv → cobra → detect → pipeline → sink chain. Closes the
  gotest leg of KNOWN_ISSUES.md § 6 and makes
  `make test 2>&1 | ./bin/distill-ai` the canonical dogfooding
  loop for this project.
- Initial project scaffolding.
- Architecture, contribution, and roadmap documentation.
- M8 CLI surface, end-to-end usable from a pipe:
  - `distill-ai run [FORMAT] [FILE...]` (also the default subcommand)
    runs the full distillation pipeline against stdin or one or more
    files.
  - `distill-ai detect FILE` autodetects a format and prints stable
    key:value diagnostics; `--strict` turns the no-match outcome
    into exit 2 for CI use.
  - `distill-ai list-formats` enumerates the registered formats.
  - `distill-ai explain [FORMAT] [FILE...]` is a dry-run mode that
    annotates every event with `kept`/`dropped:<reason>` and inline
    `<dedupe-evicted=K>` / `<vendor-collapsed=N>` / `<truncated>`
    markers. Powered by `pipeline.BuildExplain` +
    `pipeline.ExplainingBudgetStage` + `output.ExplainSink`.
  - `distill-ai completions [bash|zsh|fish|powershell]` generates
    a shell completion script.
  - `distill-ai version` prints build info one field per line.
  - Named exit-code constants (`ExitOK`, `ExitNoEvents`, `ExitError`,
    `ExitPartial`) in `cmd/distill-ai/exitcode.go` with the
    documented precedence `ExitError > ExitPartial > ExitNoEvents
    > ExitOK`.
  - JSON `summary.exit_code` is now derived from observed Sink
    state (events emitted + `BudgetCounters.ForcedDrops()`) so it
    is honest even though the encoder writes its trailer inside
    `Pipeline.Run`.
  - All v1 flags from ARCHITECTURE.md § Flags registered on the
    `run` and `explain` commands. `--auto`, `--keep-vendor`,
    `--dedupe`, `--no-dedupe`, `--dedupe-window`, `--output`,
    `--output-streaming`, `--budget`, `--no-footer`, `--strict`,
    `--tokenizer`, `--explain`, `--list-formats`, `-v` / `--verbose`
    are fully plumbed. `--max-events`, `--keep-warnings`,
    `--severity`, `--context`, `--passthrough` are registered with
    documented help text and "(plumbing lands in M8.2.x)" notices.
- `summary.events_truncated` field on the JSON output schema
  (additive; `schema_version` stays at `1` per the additive-change
  rule). Distinguishes events whose body was shortened by `--budget`
  from events that were dropped entirely. The text and markdown
  encoders render the same counter in their footers
  (`N truncated` / `**Events truncated:** N`). Wired from the
  existing `BudgetCounters.EventsTruncated` field, which had been
  populated since M6 but never surfaced. Drift-guarded by
  `TestJSONSink_SummarySchemaMatchesDoc` (new), parallel to the
  existing Event drift guard.
- M9.1: `generic` format skeleton registered under the reserved
  name. Implements `formats.Format`; `Detect` returns
  `confidenceFloor = 0.1` on any severity-anchored line and 0
  otherwise. `Parse` returned an immediately-closed channel; M9.2
  filled in the scanner. The side-effect import in
  `cmd/distill-ai/register.go` wires the package into the
  production binary so the detector's fallback path resolves
  end-to-end. Closes the "no generic fallback registered yet" gap
  that the M3 detector's help text and the integration suite
  called out.
- M9.2: severity-anchored event scanner for the `generic` format.
  `bufio.Scanner`-driven, with a bounded rolling window
  (`2*contextLines + 1` strings, regardless of input size). Emits
  one `Event` per line matching the package catalogue
  (`ERROR` / `FATAL` / `panic:` / `Exception:` / `Traceback ` /
  `WARN(ING)` / `Deprecation` / `Warning:` / `W\d{4}:`), with
  configurable pre/post context (default 3 lines each).
  Catalogue evaluation matches against an ANSI-stripped copy of
  the line so coloured anchors still anchor; `Event.Body` keeps
  the raw bytes. Best-effort `Location` extraction via a
  `path:line(:col)?` regex that requires a `/` or `\` in the path
  segment so `host:port` pairs don't false-positive. Streaming-
  friendly (first Event emerges well before EOF; verified by
  `TestGeneric_ParseStreaming`); bounded-memory under adversarial
  input (`TestGeneric_ParseBoundedMemory` pins a 16 MiB peak-heap
  ceiling for 1.25 MiB of innocuous lines); no goroutine leak on
  cancellation.
- M9.5: canonical fixture set for the `generic` format. Ten
  `.input` / `.expected` pairs under
  `internal/formats/generic/testdata/` covering clean input, single
  / multi errors, Python tracebacks, Go panics, JVM stack dumps,
  mixed warnings + errors, ANSI-coloured input, nested paths /
  host:port disambiguation, and a block-overflow case for the
  `maxBlockLines` cap. The harness lives at
  `internal/formats.RunGoldens` and is shared with future formats
  (gotest M10, pytest M11, jest M12). `DISTILL_AI_UPDATE_GOLDENS=1`
  rewrites expected files. Fixture count pinned by
  `TestGeneric_FixtureCount` so future drift is caught. Catalogue
  grows a `[A-Z][A-Za-z0-9_]*Error:` rule so suffix-form error
  types (`AssertionError:`, `ValueError:`) anchor even when not
  preceded by a word boundary. `lastNonBlank` skips the truncation
  sentinel when re-deriving traceback Titles. Closes
  `TestBinary_GenericEndToEndProducesOutput` in the integration
  suite — argv → cobra → run → pipeline → sink proven end-to-end.
  Closes KNOWN_ISSUES.md issue #2 (was: integration suite has no
  positive-distillation test for generic).
- M9.4: severity-filter plumbing for the `generic` format. Adds
  `MinSeverity` and `KeepWarnings` fields to `formats.ParseOpts`;
  the generic scanner honours both inside its anchor loop so
  filtered lines free their post-context window for the next
  surviving Event (filtered anchors still slide into the
  pre-context ring, so surviving Events see them as context).
  `--severity=error|warn|info` and `--keep-warnings` on the
  `run` and `explain` subcommands now thread end-to-end through
  `buildParseOpts` → `pipeline.FormatSource.Opts`; `--context=N`
  threads through the same path. Bad `--severity` values now
  produce an "invalid --severity" diagnostic with exit 2 instead
  of silently falling back to the default. Closes the M9.4 entry
  in KNOWN_ISSUES.md (`ParseOpts` was missing fields M8 already
  accepted on the CLI). End-to-end coverage by
  `TestRun_SeverityFiltersWarnings`, `TestRun_KeepWarningsEndToEnd`,
  `TestRun_SeverityFlagAcceptsWarn`, `TestRun_SeverityFlagInvalidValue`,
  `TestRun_ContextLinesHonoured`, `TestRun_ExplainHonoursSeverityFilter`.
- M9.3: traceback / panic block accumulation for the `generic`
  format. When the scanner anchors a `traceback` or `panic` Event,
  it switches into block mode: subsequent lines extend
  `Event.Body` until the kind's continuation rule fails,
  `maxBlockLines = 100` is hit (final Body line becomes
  `... [block truncated]`), or EOF arrives. Frame extractors then
  run over the captured Body to populate `Event.Frames`:
  Python `File "PATH", line N, in FUNC`, JVM
  `at pkg.cls.method(File.java:N)`, and Go
  `pkg.Func(args)` + tab-indented `path:line +0xOFFSET` tails.
  `traceback` Title is re-derived to the last non-blank Body line
  (the exception message); `panic` Title stays as the original
  `panic: <message>`. JVM `Exception in thread "main" ...` headers
  anchor as `Kind=traceback` (not `exception`) because they
  behave as a Python-style traceback with a multi-line stack.
  Bounded memory under adversarial input pinned by
  `TestGeneric_ParseBlockBoundedMemory` (100k-line traceback
  inside a 16 MiB ceiling). M9.4 wires `--severity` and
  `--keep-warnings`.

### Changed

- AI assets (rules and skills) relocated from opencode-specific
  `.opencode/` paths to agent-agnostic top-level directories: rules
  now live at `rules/`, skills at `skills/`. Opencode auto-loading is
  preserved via two mechanisms: `opencode.json` points its
  `instructions` glob at `rules/*.md`, and a symlink
  `.opencode/skills → ../skills` is checked into the tree so opencode
  still discovers skills at its expected location. Existing
  references in README, CONTRIBUTING, ADRs, PR template, and the
  alignment rule updated to the new paths. The skill formerly known
  as `distill-output` is renamed `distill-ai-dev` and is now the
  in-repo dogfooding / parser-debugging skill; a new sibling
  `distill-ai` skill is the self-contained, agent-agnostic
  consumer-usage skill that downstream tooling can reuse without
  carrying repo-internal context. `TestSkill_DocumentsCurrentCLISurface`
  updated to read the new path.
- `cmd/distill-ai/main.go` switched from a hand-rolled switch on
  `os.Args[1]` to a `cobra`-based root command. Production behaviour
  for `--help`, `--version`, and `detect` is preserved.
- `-v` is now `--verbose`, not `--version`. The long-form
  `--version` is unchanged.
- An unknown positional verb is no longer "unknown subcommand"; the
  root accepts `cobra.ArbitraryArgs` so `cmd | distill-ai pytest`
  works, and an unknown name flows to the input resolver (yielding
  "no such file or directory" when nothing matches). Unknown flags
  still error with cobra's standard "unknown flag" wording.
- Adds dependencies `github.com/spf13/cobra`, `github.com/spf13/pflag`,
  and `github.com/inconshreveable/mousetrap` (Windows-only). All
  pure Go, MIT/BSD, no CGo. Documented in ARCHITECTURE.md.

### Changed

- ADR-0002 records the v1.0 scope (unchanged: `pytest`, `jest`,
  `gotest`, `generic`) and the post-v1.0 roadmap: v1.1 is now a
  focused static-analysis & linting theme (M23 `golangci-lint` +
  `go vet`, M24 `cargo-json` covering rustc / cargo build /
  cargo test / clippy via `--message-format=json`); v1.2 (MCP) and
  v1.3 (code distillation) are unchanged; v1.4 adds Markdown
  outline (M25); the previous v1.1 grab-bag (k8s, JSON logs,
  npm/yarn/pnpm, rspec, mocha) moves to v1.5. M22 narrows to
  `tsc` / `gcc` / `clang` since Rust moved forward into M24. The
  three post-v1.0 milestones (M23, M24, M25) are scoped now so the
  working-agreement minimum of three open scoped milestones holds
  once M11–M13 land. ARCHITECTURE.md, TODO.md, and the
  `distill-ai-dev` skill (formerly `distill-output`) updated to
  point at the new ADR.

### Deprecated

_None yet._

### Removed

_None yet._

### Fixed

_None yet._

### Security

_None yet._

---

[Unreleased]: https://github.com/<owner>/distill-ai/compare/v0.0.0...HEAD
