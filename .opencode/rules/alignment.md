# Documentation and test alignment (hard rule)

Documentation and tests are first-class deliverables, not afterthoughts.
**Every commit that changes code must also update the corresponding docs
and tests in the same commit.** PRs that ship code without aligned
docs/tests are blocked at review, not deferred to follow-up commits.

## Doc alignment

Each kind of change has a fixed set of docs that must move with it:

| Change                                          | Doc(s) that must update in the same commit              |
|-------------------------------------------------|---------------------------------------------------------|
| New / renamed / removed exported symbol         | Godoc on the symbol; ARCHITECTURE.md if it appears there |
| New / renamed / removed CLI flag or subcommand  | README.md usage section; `--help` text in `cmd/`; the `cli-surface` manifest in `.opencode/skills/distill-output/SKILL.md` |
| New / changed JSON output field or kind value   | `docs/formats/SCHEMA.md`; bump `schema_version` if breaking |
| New format added                                | README format list; ARCHITECTURE format list; `docs/formats/<name>.md`; `.opencode/skills/distill-output/SKILL.md` recipes |
| Design principle bent or scope changed          | ARCHITECTURE.md design principles / out-of-scope sections |
| Public package API change in `pkg/distill/`     | godoc; `pkg/distill/example_test.go`                     |
| Performance budget changed (binary size, latency, throughput) | `.opencode/rules/performance.md`; commit-message justification |
| New dogfood-relevant binary behaviour (build output path, env vars consumed, etc.) | `.opencode/skills/distill-output/SKILL.md` recipes section |

If a change touches code that's described elsewhere and the description
doesn't change, that's also a doc bug — the doc has drifted.

## Test alignment

- **Every new exported function or method has at least one test.** No
  exceptions for "trivial" code; trivial code still has trivial tests.
- **Every change to existing exported behaviour ships a test that fails
  before the change and passes after.** State this in the commit body
  ("Test added: TestFoo covers the new branch" or "Existing TestBar now
  covers the regression").
- **Every format ships ≥5 golden fixtures.** Clean run, single failure,
  multi-failure, mixed warnings+errors, edge case. Detailed in
  [CONTRIBUTING.md § Adding a format](../../CONTRIBUTING.md#adding-a-format).
- **Determinism is a property test on every format** (same input twice
  → byte-identical output). Not optional.
- **Streaming behaviour is a property test on every format** (events
  emit incrementally, not buffered until EOF) once streaming lands in M2.

## Enforcement

- **Hard gates (CI must pass):** `go test ./...`, `go vet`,
  `golangci-lint run`. `revive`'s `exported` rule fails the build on
  undocumented exported symbols.
- **Skill drift guard.** `TestSkill_DocumentsCurrentCLISurface` in
  `test/integration/integration_test.go` parses the `cli-surface`
  manifest in `.opencode/skills/distill-output/SKILL.md` and asserts
  every subcommand and top-level flag listed there is recognised by
  the compiled binary — and conversely, every subcommand the binary
  prints in `--help` is in the manifest (or its `cli-surface-future`
  block). A new verb or flag without a matching manifest update
  fails the integration suite, which gates merges.
- **PR template checklist** explicitly requires docs + tests boxes
  ticked. Reviewers reject PRs with code changes and no corresponding
  doc / test diffs.
- **Milestone exit review:** before closing a milestone, grep
  ARCHITECTURE.md / README.md for symbol names that no longer exist,
  diff `--help` against README's usage section, and verify SCHEMA.md
  matches `--output=json` output of every format. Caught drift is fixed
  before the milestone is ticked complete.

## If you genuinely can't align in one commit

Don't open the PR. Either:

1. Defer the code change until you can write the docs / tests, or
2. Split the work into a docs-first commit ("define interface and
   document it") followed by an implementation commit ("implement the
   documented interface, with tests"). Both land in the same PR.

"I'll do the docs after" or "tests next sprint" is the failure mode
this rule exists to prevent.
