# Adding a flag

Before adding a flag, answer:

1. **Is there an existing flag that could be generalised instead?**
2. **Can it be inferred from context** (autodetect, sensible default)?
3. **Will an agent ever use it, or only a human?** Agent-visible flags
   are higher value; human-only flags are usually a sign the feature
   should be a separate subcommand or skipped.
4. **Does it interact safely with `--budget` and streaming?**

The current flag set is intentionally small (~15, see
[ARCHITECTURE.md](../../ARCHITECTURE.md) for the canonical list). Each
addition is a maintenance burden: documentation, help text, conflict
matrices with other flags, golden output tests that exercise the new
combination, and downstream agents that have to learn about it.

**Default: don't add it.**

If you genuinely need a new flag, the alignment rule applies: README's
usage section, the `--help` text in `cmd/distill-ai/`, and
ARCHITECTURE.md's flag list all update in the same commit that adds
the flag.
