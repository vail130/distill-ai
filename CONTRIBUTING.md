# Contributing to distill-ai

Thanks for your interest in contributing. This guide covers the practical
mechanics of contributing code, docs, and bug reports. For the *why*
behind design decisions, read [ARCHITECTURE.md](./ARCHITECTURE.md). For
day-to-day code conventions (used by both humans and AI agents), read
[AGENTS.md](./AGENTS.md).

## Ways to contribute

- **Report bugs.** File an issue using the bug report template. Include
  the input that triggered the bug (or a minimal reproducer).
- **Request a new format.** Use the "format request" issue template.
  Include a sample log file (≥1 KB, ≤1 MB) and what you'd expect to be
  extracted.
- **Submit a format.** Adding support for a new tool's output is the
  highest-impact contribution. See [Adding a format](#adding-a-format).
- **Improve docs.** Especially per-format docs under `docs/formats/`
  and integration guides under `docs/integration/`.
- **Fix bugs / add tests.** Look for issues labelled `good first issue`
  or `help wanted`.

## Development setup

You need Go 1.26+.

```bash
git clone https://github.com/<owner>/distill-ai
cd distill-ai
go build ./...
go test ./...
```

Optional but recommended:

```bash
# Linter
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
golangci-lint run

# Install locally
go install ./cmd/distill-ai
```

## Running tests

```bash
# All tests
go test ./...

# Single format
go test ./internal/formats/pytest/

# Update golden files (after intentional output changes)
go test ./... -update

# With race detector
go test -race ./...

# Benchmarks
go test -bench=. ./...

# Integration suite only (forks the compiled binary)
make test-integration
```

Every format has golden-file tests under
`internal/formats/<name>/testdata/`. Run them before opening a PR.

### Integration tests

`test/integration/` holds tests that exec the real compiled binary
rather than calling functions in-process. They build `cmd/distill-ai`
with `-race` once per `go test` run and exercise it the way an agent
or human would: argv, stdin, exit codes, stdout/stderr separation.

Fixtures live under `test/integration/testdata/fixtures/` as raw
`.input` files. Expected output lives under `testdata/golden/` as
`*.contains.txt` files (one substring per non-empty line; all must
appear in the captured stream). Byte-exact goldens will arrive when
M8+ ships the distilled output encoders, whose output is stable
across machines.

Add an integration test when:

- A new subcommand or top-level flag lands (every flag in
  [ARCHITECTURE.md § Flags](./ARCHITECTURE.md#flags) needs at least
  one end-to-end exercise).
- A new format starts winning detection (replace the pre-format
  "falls through to no-match" assertion with a positive detection
  assertion).
- A regression is found that the unit tests missed.

## Adding a format

This is the most common contribution. The process:

1. Open an issue using the **format request** template first, with a
   sample log file. This lets maintainers confirm the format is in scope
   and isn't already being worked on.
2. Create `internal/formats/<name>/<name>.go`.
3. Implement the [`Format` interface][format-godoc] (`Name`, `Detect`,
   `Parse`). The godoc on each method is the canonical spec: detection
   must be cheap, parse must close the channel on EOF or context
   cancellation, etc. A runnable minimum implementation lives in
   [`internal/formats/example_test.go`][format-example].
4. Register in `init()`: `formats.Register(&Format{})`.
5. Add fixtures under `internal/formats/<name>/testdata/`. **Minimum five
   cases**: clean run, single failure, multiple failures, mixed
   warnings+errors, edge case (empty or truncated input).
6. Run `go test ./internal/formats/<name>/` and confirm goldens match.
7. Run `distill-ai detect <fixture>` and confirm your format wins over
   `generic`.
8. Add the format to the format list in `README.md` and
   `ARCHITECTURE.md`.
9. Add a doc under `docs/formats/<name>.md` describing what's extracted
   and what's dropped.
10. Add a recipe to
    `.opencode/skills/distill-output/SKILL.md` showing how to dogfood
    the new format on this repo's own command output. Skill recipes
    are part of the documentation surface; see the
    [alignment rule](./.opencode/rules/alignment.md) for the full
    list of docs that must move when the CLI surface or recipe-
    relevant behaviour changes.

Don't touch other formats' code. The plugin model exists for isolation.

## Submitting a pull request

> **Hard rule:** every commit that changes code must also update the
> corresponding docs and tests in the same commit. PRs without aligned
> docs/tests are rejected. See
> [.opencode/rules/alignment.md](./.opencode/rules/alignment.md)
> for the full rule and the table mapping each kind of change to the
> docs that must move with it.

1. Fork the repo and create a branch from `main`:
   `git checkout -b <component>/<short-description>` (e.g.,
   `pytest/collapse-vendor-frames`).
2. Make your changes. Keep commits small and focused — one logical
   change per commit.
3. Run `go test ./...` and `golangci-lint run`. Both must pass.
4. **Update docs and tests in the same commit as the code they
   describe.** README usage section + `--help` text for flag changes,
   `docs/formats/SCHEMA.md` for output changes, format docs for new
   formats, godoc for every exported symbol.
5. Push and open a PR. Fill in the PR template completely; the docs +
   tests boxes are not optional.
6. Be patient. Maintainers review on best-effort basis.

### Commit messages

Full rules in [`.opencode/rules/commits.md`](./.opencode/rules/commits.md).
Summary:

- Imperative subject line, prefixed with the component:
  `pytest: collapse vendor frames in tracebacks`
- Body explains *why* the change is needed, not *what* the diff shows.
- Each commit independently builds and passes tests. Don't bundle a
  refactor with a feature.
- If your change closes an issue, end the body with `Closes #123`.

### Code style

Full rules in [`.opencode/rules/code-style.md`](./.opencode/rules/code-style.md).
Summary:

- Go: no blank lines inside functions; struct/map literals one field per line.
- Comments: short, single-line where possible; explain *why* not *what*.
- Errors: wrap with `fmt.Errorf("context: %w", err)`.
- `context.Context` is the first parameter of any function doing I/O.
- No `panic` outside `init()` or genuine programmer errors.

### Adding dependencies

Full rules in [`.opencode/rules/dependencies.md`](./.opencode/rules/dependencies.md).
Summary: the default answer is **no**. If you believe one is needed:

1. Justify it in the commit message: what does it do that the stdlib
   can't?
2. Confirm no existing dependency covers it.
3. Verify it doesn't pull in CGo or transitively bloat the binary.

The current allow-list is in
[ARCHITECTURE.md](./ARCHITECTURE.md#dependencies).

## What we won't accept

These are listed so you don't waste effort building them:

- Anything that adds network behaviour (telemetry, auto-update, remote
  format definitions, etc.). Hard rule.
- Interactive TUI features. `lnav` already does this well.
- Persistent caching. Could be a sibling tool, won't live in this one.
- Auto-detection of the target LLM model.
- Heavyweight dependencies (anything pulling CGo or >1 MB).
- Features that bend the design principles in
  [ARCHITECTURE.md](./ARCHITECTURE.md#design-principles).

If your idea is in this list but you think the rationale is wrong, open
an issue to discuss before writing code.

## Reporting security issues

Do not file public issues for security vulnerabilities. See
[SECURITY.md](./SECURITY.md) for the disclosure process.

## Code of conduct

By participating in this project, you agree to abide by the
[Code of Conduct](./CODE_OF_CONDUCT.md).

## Questions

- Bug or feature: file an issue.
- General discussion: GitHub Discussions (if enabled) or open an issue
  labelled `question`.

[format-godoc]: https://pkg.go.dev/github.com/vail130/distill-ai/internal/formats#Format
[format-example]: ./internal/formats/example_test.go
