<!--
Thanks for opening a PR. Fill in the sections below and tick the
checklist. Items that don't apply can be marked N/A.

For style and conventions, see AGENTS.md and CONTRIBUTING.md.
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

## Checklist

- [ ] One logical change per commit; commit subjects are imperative and
      component-prefixed (e.g., `pytest: collapse vendor frames`).
- [ ] Commit bodies explain *why*, not *what*.
- [ ] `go test ./...` passes.
- [ ] `golangci-lint run` passes.
- [ ] New / changed behaviour has tests (golden fixtures for format
      changes; unit tests for everything else).
- [ ] No new dependencies — or, if added, justified in the commit message
      and consistent with the
      [dependency allow-list](../ARCHITECTURE.md#dependencies).
- [ ] Docs updated where relevant (README, ARCHITECTURE, AGENTS,
      per-format docs).
- [ ] Change fits the
      [design principles](../ARCHITECTURE.md#design-principles).
      If it doesn't, the description above explains why we should bend
      them.
- [ ] No network behaviour introduced.
- [ ] Output schema unchanged, or schema version bumped per
      [AGENTS.md output stability](../AGENTS.md#output-stability).

## For format additions only

- [ ] At least five fixtures under `internal/formats/<name>/testdata/`
      covering: clean run, single failure, multi failure, mixed
      warnings + errors, edge case.
- [ ] `distill-ai detect <fixture>` prefers this format over `generic`.
- [ ] Format listed in README and ARCHITECTURE.
- [ ] `docs/formats/<name>.md` added.
