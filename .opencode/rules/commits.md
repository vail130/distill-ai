# Commit conventions

- **Imperative subject, prefixed with the relevant component:**
  `pytest: collapse vendor frames in tracebacks`
- **Body explains *why* the change was needed**, not *what* the diff
  shows. Future readers can run `git show` for the *what*.
- **Each commit independently builds and passes tests.** This is what
  makes `git bisect` useful. A commit that fails CI in the middle of a
  stack is a regression even if the tip is green.
- **One logical change per commit.** Don't bundle a format addition
  with a refactor. Don't bundle "fix typo" with a feature.
- **If your change closes an issue, end the body with `Closes #N`.**

## Subject prefixes used in this repo

| Prefix       | Use for                                                     |
|--------------|-------------------------------------------------------------|
| `build:`     | `go.mod`, Makefile, `go.sum`, build tooling                 |
| `ci:`        | `.github/workflows/`, lint config, goreleaser               |
| `docs:`      | Anything under `docs/`, top-level `*.md`, godoc-only edits  |
| `event:`     | `internal/event/`                                           |
| `formats:`   | `internal/formats/` (interface + registry; not format impls)|
| `pytest:`    | `internal/formats/pytest/`                                  |
| `jest:`      | `internal/formats/jest/`                                    |
| `gotest:`    | `internal/formats/gotest/`                                  |
| `generic:`   | `internal/formats/generic/`                                 |
| `pipeline:`  | `internal/pipeline/`                                        |
| `detect:`    | `internal/detect/`                                          |
| `tokens:`    | `internal/tokens/`                                          |
| `output:`    | `internal/output/`                                          |
| `cmd:`       | `cmd/distill-ai/`                                           |
| `distill:`   | `pkg/distill/`                                              |
| `repo:`      | `.gitignore`, `.editorconfig`, `.gitattributes`, similar    |
| `todo:`      | `TODO.md`-only edits                                        |

If a commit truly touches multiple components, that is usually a sign
it should be two commits.

## Amending vs. follow-up commits

- **Fixing an unlanded commit (not yet merged to main): amend, never
  a follow-up commit.** Stacking "fix CI" commits on an open PR is
  forbidden; rewrite the original and force-push.
- **Don't `--amend` once a commit has merged to main.** That's a force
  push to a protected branch.
- **If a CI hook auto-modifies files after a successful commit**, amend
  to include those changes; verify with `git log -1 --format='%an %ae'`
  that HEAD is the commit you just made.

## Working with PRs

- After ANY force-push to a branch with an open PR, update the PR
  description so it still matches the commit log.
- The PR description includes every commit's full message in a
  collapsible `<details>` block, per the [PR template](../../.github/PULL_REQUEST_TEMPLATE.md).
- Single-commit PR: title = commit subject.
