<!--
Thanks for opening a PR. Fill in the sections below and tick the
checklist. Items that don't apply can be marked N/A.

For style and conventions, see AGENTS.md and CONTRIBUTING.md.

HARD RULE: every commit that changes code must also update the
corresponding docs and tests in the same commit. See
https://github.com/vail130/distill-ai/blob/main/AGENTS.md#documentation-and-test-alignment-hard-rule
-->

## What this changes

<!-- Short description of the change, in plain language. -->

## Why

<!--
Why is this needed? Link the issue this closes if applicable.

Closes #
-->

## How

<!--
Brief technical summary of the approach. Skip if the diff makes it
obvious.
-->

## Docs touched

<!--
List every doc updated in this PR. If a doc that should have moved did
not, justify here. "N/A" is acceptable only when the code change has no
user-visible or interface-level surface.

Example:
- README.md: added `--budget` flag to usage examples
- docs/formats/SCHEMA.md: added new `truncated` field
- godoc on Event.Truncated
-->

## Tests added

<!--
List the tests that are new or modified for this change. State which
test would have failed before this PR.

Example:
- TestEventTruncationMarksFlag (new)
- TestBudgetEnforcerDropsLowSeverity (extended; previously didn't cover
  the truncation path)
-->

## Checklist

### Code & commits

- [ ] One logical change per commit; subjects are imperative and
      component-prefixed (e.g., `pytest: collapse vendor frames`).
- [ ] Commit bodies explain *why*, not *what*.
- [ ] Each commit independently builds and passes tests.
- [ ] `go test ./...` passes.
- [ ] `golangci-lint run` passes.
- [ ] No new dependencies — or, if added, justified in the commit message
      and consistent with the
      [dependency allow-list](../ARCHITECTURE.md#dependencies).

### Docs alignment (hard rule)

Tick every box that applies; explicitly mark N/A for the rest.

- [ ] **Godoc** added/updated for every new or changed exported symbol.
- [ ] **README.md** updated if a user-visible flag, subcommand, output,
      or workflow changed.
- [ ] **`--help` text** in `cmd/distill-ai/` matches README usage.
- [ ] **ARCHITECTURE.md** updated if a public interface, package layout,
      design principle, or scope item changed.
- [ ] **`docs/formats/SCHEMA.md`** updated if the JSON output schema
      changed; `schema_version` bumped for breaking changes.
- [ ] **`docs/formats/<name>.md`** added/updated if a format was added
      or its extraction behaviour changed.
- [ ] **CHANGELOG.md** updated under `[Unreleased]` for user-visible
      changes.

### Tests alignment (hard rule)

- [ ] Every new exported function/method has at least one test.
- [ ] Every changed exported behaviour has a test that fails before this
      PR and passes after; named in the "Tests added" section above.
- [ ] Determinism tests pass for any format / encoder changes.
- [ ] Streaming tests pass for any pipeline changes (once M2 lands).

### Design

- [ ] Change fits the
      [design principles](../ARCHITECTURE.md#design-principles). If it
      doesn't, the "Why" section above explains why we should bend them.
- [ ] No network behaviour introduced.
- [ ] Output schema unchanged, or schema version bumped per
      [AGENTS.md output stability](../AGENTS.md#output-stability).

## For format additions only

- [ ] At least five fixtures under `internal/formats/<name>/testdata/`
      covering: clean run, single failure, multi failure, mixed
      warnings + errors, edge case.
- [ ] `distill-ai detect <fixture>` prefers this format over `generic`.
- [ ] Format listed in README and ARCHITECTURE.
- [ ] `docs/formats/<name>.md` added with extraction behaviour, dropped
      content, and example I/O.
