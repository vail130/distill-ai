# Output stability

## JSON output is a public API

The schema emitted by `--output=json` is documented in
[`docs/formats/SCHEMA.md`](../../docs/formats/SCHEMA.md) and treated as
a public API. Breaking changes require:

1. **A version bump in the schema** (`"schema_version": 2`).
2. **A deprecation period for the old schema.** New `schema_version`
   defaults off until the next major version of the binary.
3. **Migration notes in the changelog.** Reference the bumped fields
   and the rollout plan.

Additive changes (new optional fields, new `kind` values, new severity
constants) **do not** bump the version. Consumers must ignore unknown
fields. Document the addition in CHANGELOG.md under `[Unreleased]`.

## Text and markdown output

The `text` and `markdown` outputs are human-targeted and can evolve
more freely than the JSON. **But** golden tests will catch unintended
changes. Update the goldens deliberately, not reflexively.

If a golden diff is unexpected, that's a regression — fix the code,
don't rubber-stamp the new output.

## Drift guard

`TestEvent_JSONSchemaMatchesDoc` (in `internal/event/`) is the
build-time enforcement: it reads `docs/formats/SCHEMA.md` and verifies
every JSON tag in the `Event`, `Location`, and `StackFrame` structs is
documented. If the struct and the doc drift, the build fails.

Per-format `docs/formats/<name>.md` files describe what each format
extracts, what it drops, and example I/O. Those are reference docs;
they aren't enforced at build time, but they're checked at milestone
exit (see [alignment.md § Enforcement](./alignment.md#enforcement)).
