# Integrating distill-ai with opencode

`distill-ai` does not ship an opencode plugin or per-agent hook
(see [ADR-0003](./decisions/0003-position-vs-rtk-and-snip.md) for
the why). The integration is **documentation + a skill**: a
project rules entry plus the bundled
[`skills/distill-ai/`](../skills/distill-ai/) skill that opencode
auto-loads when output volume warrants it.

This recipe takes about three minutes. The full library of options
each command supports is in `man distill-ai-run` after
[installation](../README.md#install).

## 1. Install the binary

```bash
brew install vail130/distill-ai/distill-ai

# Or:
go install github.com/vail130/distill-ai/cmd/distill-ai@latest
```

Verify with `distill-ai --version`.

## 2. Install the bundled skill

The repo at [`skills/distill-ai/`](../skills/distill-ai/) is a
self-contained, agent-agnostic skill. opencode auto-discovers
skills under `~/.config/opencode/skills/`; the Makefile target
links the bundled skill there:

```bash
make install-skill
```

The default destination is `~/.config/opencode/skills/distill-ai`.
Override with `SKILL_DEST=/path/to/skill make install-skill`. Run
`make uninstall-skill` to remove the link. The target is
idempotent and refuses to overwrite a non-symlink at the
destination — safe to re-run.

After installation, opencode finds the skill at session start.
When a user pipes a noisy test run or build through the agent's
Bash tool, the skill's heuristics kick in and suggest piping
through `distill-ai` for the rest of the conversation.

## 3. Add the pipe pattern to AGENTS.md

The skill is the discovery layer; AGENTS.md is the rule that makes
the pattern explicit. Open the `AGENTS.md` file at the root of
your project (create it if it doesn't exist), and add:

```markdown
## Distilling noisy command output

When running tests, builds, or tailing logs through the Bash tool,
pipe through `distill-ai` to keep the context window lean. It
autodetects the format:

  pytest 2>&1 | distill-ai
  go test ./... 2>&1 | distill-ai
  npx jest 2>&1 | distill-ai
  kubectl logs <pod> | distill-ai --dedupe

For a strict token cap:
  pytest 2>&1 | distill-ai --budget=2000

To see what was kept vs. dropped:
  pytest 2>&1 | distill-ai explain

See `skills/distill-ai/SKILL.md` (if installed) or
`man distill-ai-run` for the full surface.
```

opencode reads AGENTS.md at session start and applies the pattern
for the rest of the session.

## 4. Worked example

An opencode session driving a Go test run:

```
> Run the unit tests and tell me what's failing.

I'll run the tests with distill-ai piped in to keep the output
compact:

  go test ./... 2>&1 | distill-ai

events from gotest

[1] ERROR thing_test.go:42: expected 200, got 500
  at thing_test.go:42
  body:
      === RUN   TestThing
          thing_test.go:42: expected 200, got 500
      --- FAIL: TestThing (0.01s)
      FAIL    github.com/example/project/thing       0.123s
  metadata.test_id=TestThing
  metadata.package=github.com/example/project/thing

---
distilled 8 lines → 10 lines (90 tokens, heuristic)
dropped: 0 events, 0 truncated, 0 deduped, 0 vendor frames

One failure: TestThing in thing_test.go expected 200 but got 500.
Want me to open the test file?
```

The skill suggested the pipe; the agent ran it; the per-failure
structured Event let the agent answer the user's question
concisely.

## 5. Exit-code interpretation

opencode's Bash tool surfaces the rightmost pipe exit code, so the
mapping is identical to Claude Code's:

| Code | Meaning                                                                       |
|------|-------------------------------------------------------------------------------|
| `0`  | distill-ai ran and emitted at least one event.                                |
| `1`  | distill-ai ran but emitted zero events (clean input — no failures).           |
| `2`  | distill-ai itself failed (bad flags, IO error, autodetect refused under --strict). |
| `3`  | distill-ai ran but had to drop or truncate events to fit `--budget`.          |

To preserve the underlying command's exit code, use bash's `set
-o pipefail` or `${PIPESTATUS[@]}`.

## 6. Common pitfalls

- **Skipping `make install-skill`.** Without the bundled skill
  installed, opencode has no signal to suggest the pipe pattern on
  its own — AGENTS.md is the rule, the skill is the heuristic.
  Both pull their weight.
- **Forgetting `2>&1`.** Most test runners write failures to
  stderr. Without redirection only the trailing summary reaches
  distill-ai. The AGENTS.md snippet above includes `2>&1` on every
  example for this reason.
- **Re-using the skill in another repo.** The skill is
  agent-agnostic and assumes only `distill-ai` is on `PATH`. Once
  linked via `make install-skill`, it works in every opencode
  session, not just sessions inside this repo. To remove it, run
  `make uninstall-skill` from this repo, or `rm
  ~/.config/opencode/skills/distill-ai` directly.

## See also

- [`man distill-ai-run`](../man/man1/distill-ai-run.1) for every
  flag and subcommand.
- [Integration with Claude Code](./integration-claude-code.md)
  for the parallel recipe.
- [Integration with CI](./integration-ci.md) for GitHub Actions
  / GitLab CI recipes.
