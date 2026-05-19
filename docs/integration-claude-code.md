# Integrating distill-ai with Claude Code

`distill-ai` does not ship a Claude Code plugin or per-agent hook
(see [ADR-0003](./decisions/0003-position-vs-rtk-and-snip.md) for
the why). The integration pattern is **documentation**: tell Claude
Code to pipe noisy commands through `distill-ai` via its
`CLAUDE.md` project rules file, and the agent does the right thing
for the rest of the session.

This recipe takes about three minutes to set up. The full library
of options each command supports is in `man distill-ai-run` after
[installation](../README.md#install).

## 1. Install the binary

```bash
# Homebrew (macOS / Linux)
brew install vail130/distill-ai/distill-ai

# Or Go (any platform with a Go toolchain)
go install github.com/vail130/distill-ai/cmd/distill-ai@latest
```

Verify with `distill-ai --version`.

## 2. Add the pipe pattern to CLAUDE.md

Open the `CLAUDE.md` file at the root of your project (create it if
it doesn't exist), and add this block:

```markdown
## Distilling noisy command output

When running tests, builds, or tailing logs through the Bash tool,
pipe the command through `distill-ai` to keep the context window
lean. It detects the format automatically:

  pytest 2>&1 | distill-ai
  go test ./... 2>&1 | distill-ai
  npx jest 2>&1 | distill-ai
  kubectl logs <pod> | distill-ai --dedupe

For a strict token cap (e.g., fit a CI run in 2000 tokens):
  pytest 2>&1 | distill-ai --budget=2000

To see what was kept vs. dropped before trusting the output:
  pytest 2>&1 | distill-ai explain
```

Claude reads `CLAUDE.md` at session start and applies the pattern
to every applicable command for the rest of the session.

## 3. Worked example

A failing pytest run inside Claude's Bash tool, before:

```
$ pytest tests/test_auth.py
============================= test session starts ==============================
platform darwin -- Python 3.11.4, pytest-7.4.0, pluggy-1.2.0
rootdir: /Users/example/project
collected 3 items

tests/test_auth.py .F.                                                  [100%]

=================================== FAILURES ===================================
_______________________________ test_login_redirect _______________________________

    def test_login_redirect():
        creds = {"u": "alice", "p": "secret"}
        response = client.post("/login", data=creds)
        assert response.status_code == 302
>       assert response.headers["location"] == "/dashboard"
E       AssertionError: expected '/dashboard', got '/login?next=/'

tests/test_auth.py:47: AssertionError
=========================== short test summary info ============================
FAILED tests/test_auth.py::test_login_redirect - AssertionError: expected '/dashboard', got '/login?next=/'
========================= 1 failed, 2 passed in 0.42s ==========================
```

After (`pytest tests/test_auth.py 2>&1 | distill-ai`):

```
events from pytest

[1] ERROR AssertionError: expected '/dashboard', got '/login?next=/'
  at tests/test_auth.py:47
  ...

---
distilled 21 lines → 15 lines (179 tokens, heuristic)
dropped: 0 events, 0 truncated, 0 deduped, 0 vendor frames
```

Claude sees the failure with its location and assertion message
directly, without the surrounding session-header noise.

## 4. Exit-code interpretation

Claude's Bash tool surfaces the exit code of the **last** command
in a pipeline (the pipe's right-hand side), so `pytest 2>&1 |
distill-ai` returns the exit code of `distill-ai`, not `pytest`.
The mapping is:

| Code | Meaning                                                                       | What Claude should do                          |
|------|-------------------------------------------------------------------------------|------------------------------------------------|
| `0`  | distill-ai ran and emitted at least one event.                                | The agent should read the events.              |
| `1`  | distill-ai ran but emitted zero events (clean input — no failures).           | The agent should treat this as a green build.  |
| `2`  | distill-ai itself failed (bad flags, IO error, autodetect refused under --strict). | The agent should investigate the error from stderr. |
| `3`  | distill-ai ran but had to drop or truncate events to fit `--budget`.          | The events emitted are still useful; the agent should note that some were dropped. |

To preserve the underlying command's exit code in bash, use
`set -o pipefail` or capture `${PIPESTATUS[@]}` before the pipe
completes.

## 5. Common pitfalls

- **Forgetting `2>&1`.** Most test runners write failures to
  stderr, not stdout. Without redirection only the trailing
  summary reaches distill-ai. The CLAUDE.md snippet above includes
  `2>&1` on every test-runner example for this reason.
- **Piping `--output=json` when the agent expects text.** The
  default output mode is text — easy for Claude to read directly.
  Reserve `--output=json` for tooling that parses the schema. If
  Claude is asked to extract specific fields, `distill-ai
  --output=json | jq ...` is the idiom.
- **Forgetting the format autodetect.** distill-ai picks the
  format from the first 16 KB of input. A test runner whose output
  starts with thousands of lines of unrelated chatter (rare but
  possible in long-running CI jobs) may fall back to the generic
  parser. Pass an explicit `FORMAT` argument (`distill-ai pytest`)
  when in doubt.
- **Pipe-buffering surprises.** Some terminals fully buffer stdout
  when piped. `pytest -u` (or `python -u`) and `go test -v` both
  flush more eagerly; distill-ai itself streams, so the buffering
  artifact is always upstream.

## See also

- [`man distill-ai-run`](../man/man1/distill-ai-run.1) for every
  flag and subcommand the binary supports.
- [Integration with opencode](./integration-opencode.md) for the
  parallel recipe targeting that agent.
- [Integration with CI](./integration-ci.md) for GitHub Actions
  / GitLab CI recipes the agent reads alongside its dev loop.
