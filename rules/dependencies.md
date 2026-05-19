# Dependency policy

**Default answer to "should I add this dependency?" is no.**

Adding a dependency requires justification in the commit message. The
[dependency list in ARCHITECTURE.md](../ARCHITECTURE.md#dependencies)
is the allow-list. New entries need:

1. A concrete reason the stdlib won't do.
2. Confirmation that no existing dependency covers it.
3. Verification that it doesn't pull in CGo or transitively bloat the
   binary.

distill-ai ships as a single static binary; every dependency is binary
size we have to justify. Heavyweight libraries (anything > 1 MB or with
transitive CGo) are rejected even when they would technically work.

When in doubt, write the code rather than add the dep. Most "I need
a library for X" turns out to be 30 lines of stdlib code.

## After adding or removing a dependency

Always run `go mod tidy` and stage the resulting `go.mod` / `go.sum`
changes in the **same commit** as the import that introduced (or
dropped) the dep. A commit that adds an `import` line without the
corresponding `go.sum` update is a bisect hazard: CI will pass on
the tip of the branch but fail at the intermediate commit because
the module graph is out of sync.

This applies equally to indirect dependencies surfaced by `tidy` and
to entries that disappear when an import is removed.
