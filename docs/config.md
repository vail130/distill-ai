# Configuration file

`distill-ai` optionally reads two TOML configuration files at start:

- A **project config**, `.distill-ai.toml`, found by walking from the
  current working directory toward the filesystem root (stopping at
  the nearest git root). Used to ship per-project defaults inside a
  monorepo.
- A **user config**, `$XDG_CONFIG_HOME/distill-ai/config.toml` (or
  `~/.config/distill-ai/config.toml` when `XDG_CONFIG_HOME` is
  unset). Used for personal defaults that apply everywhere.

When both are present the project config wins. CLI flags override
both. See [Precedence](#precedence) for the full chain.

## Status

| Capability | Milestone | Status |
|------------|-----------|--------|
| Decode a TOML file into `Config` | M14.1 | shipped |
| Discover project + user configs | M14.2 | scoped |
| Merge precedence chain | M14.3 | scoped |
| Wire into CLI flag defaults | M14.4 | scoped |
| Custom-format registration | M14.5 | scoped |
| `--max-events`, `--passthrough` plumbing | M14.6 | scoped |

This page describes the **shipped surface**. Sections that document a
future milestone are tagged as such.

## Schema

The full schema, with default values and the M14 sub-milestone that
wires each key into the CLI:

| TOML key | Type | Default | Description | Wired by |
|----------|------|---------|-------------|----------|
| `schema_version` | int | `1` | Must equal the binary's expected version. Set explicitly for forward compatibility. | M14.1 |
| `default_budget` | int | `0` (no cap) | Seeds `--budget`. | M14.4 |
| `default_output` | string | `"text"` | Seeds `--output`. One of `text`, `json`, `json-streaming`, `markdown`. | M14.4 |
| `default_tokenizer` | string | `"heuristic"` | Seeds `--tokenizer`. One of `heuristic`, `tiktoken`. | M14.4 |
| `default_strip_envelope` | string | `"auto"` | Seeds `--strip-envelope`. One of `auto`, `none`, or a registered envelope name. | M14.4 |
| `max_events` | int | `0` (no cap) | Seeds `--max-events`. | M14.6 |
| `keep_warnings` | bool | `false` | Seeds `--keep-warnings`. | M14.4 |
| `keep_vendor` | bool | `false` | Seeds `--keep-vendor`. | M14.4 |
| `dedupe_window` | int | `0` (off) | Seeds `--dedupe-window`. | M14.4 |
| `context_lines` | int | `0` (format default) | Seeds `--context`. | M14.4 |
| `passthrough` | bool | `false` | Seeds `--passthrough`. | M14.6 |

### Per-format overrides

A `[formats.<name>]` block overrides selected top-level keys for one
format. The `<name>` is the lowercase identifier `formats.All()`
reports (`pytest`, `gotest`, `jest`, `generic`).

| TOML key | Type | Description |
|----------|------|-------------|
| `keep_warnings` | bool | Overrides the top-level `keep_warnings`. |
| `keep_vendor` | bool | Overrides the top-level `keep_vendor`. |
| `dedupe_window` | int | Overrides the top-level `dedupe_window`. Explicit `0` disables dedupe for this format only. |
| `context_lines` | int | Overrides the top-level `context_lines`. |
| `min_severity` | string | Overrides the parser's default minimum severity. One of `error`, `warn`, `info`. |

Per-format values are pointers internally so an explicit zero
overrides the top-level value, while an absent key falls through.

### Custom-format blocks (scoped: M14.5)

A `[[formats.custom.<name>]]` array table registers a regex-driven
Format at process start. The registered format's `Name()` returns
`custom:<name>` so it cannot collide with a built-in format.

| TOML key | Type | Required | Description |
|----------|------|----------|-------------|
| `detect_regex` | string | yes | Match a sample line to claim the format with Confidence 1.0. |
| `event_start` | string | yes | Match the first line of an Event. |
| `event_end` | string | no | Match the last line of an Event. If empty, each `event_start` match becomes a one-line Event. |
| `severity` | string | no | Defaults to `error`. |
| `kind` | string | no | Defaults to `match`. |

M14.5 compiles these regexes at startup; compilation failures fail
the binary before any input is read so misconfigured regexes are
surfaced immediately.

## Example

```toml
schema_version = 1
default_budget = 2000
default_output = "text"

[formats.pytest]
keep_warnings = false
context_lines = 3

[formats.gotest]
dedupe_window = 1000

[[formats.custom.myapp]]
detect_regex = '^\[myapp\]'
event_start = '^\[myapp\] ERROR'
event_end = '^\[myapp\] (INFO|DEBUG|ERROR)'
```

## Validation

`Load` rejects:

- Unknown top-level or nested keys (the user's typo of
  `keep_warning` instead of `keep_warnings` produces an error
  listing the offending dotted key, not silent fallback).
- A declared `schema_version` other than the binary's expected
  version (`1`). Unset is treated as `1` for backward compatibility
  with configs written before the key was added.
- A custom-format block missing `detect_regex` or `event_start`.
  The error names both the block and the missing field.

Validation that depends on the registered format set (an unknown
`[formats.foo]` block, an unsupported `default_output` value) is
deferred to the consuming CLI command at run time. A config that
references a format the binary doesn't ship is valid as long as the
user never opts into that format.

## Precedence

> Scoped: M14.3.

The resolution chain for any flag whose default is config-driven:

1. **CLI flag** if explicitly passed.
2. **Project `.distill-ai.toml`** if a key is set.
3. **User `~/.config/distill-ai/config.toml`** if a key is set.
4. **Built-in default**.

For per-format keys, the chain is: per-format override → top-level
key → built-in default. The CLI flag still wins over all of the
above.

## Debugging

> Scoped: M14.4 (`--print-config`).

`distill-ai run --print-config` (and `explain --print-config`) prints
the merged effective configuration as TOML and exits without running
the pipeline. Use it to verify "which config is winning?" without
reverse-engineering the precedence chain.

## Discovery

> Scoped: M14.2.

`Discover(cwd, home)` returns absolute paths to the project and user
config files, or empty strings when a file does not exist:

- **Project:** walks from `cwd` toward the root, stopping at the
  first directory that contains `.distill-ai.toml` *or* `.git/`.
  Bounded by an absolute depth cap (32 directories) to protect
  against symlink loops.
- **User:** `$XDG_CONFIG_HOME/distill-ai/config.toml` if the env
  var is set, else `<home>/.config/distill-ai/config.toml`.

`LoadAll(cwd, home)` is the convenience wrapper: Discover, Load each
non-empty path, then [Merge](#precedence).
