# Changelog

All notable changes to `distill-ai` are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

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
  otherwise. `Parse` returns an immediately-closed channel for now
  — the M9.2 commit fills in the severity-anchored scanner. The
  side-effect import in `cmd/distill-ai/register.go` wires the
  package into the production binary so the detector's fallback
  path resolves end-to-end. Closes the "no generic fallback
  registered yet" gap that the M3 detector's help text and the
  integration suite called out.

### Changed

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

_None yet._

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
