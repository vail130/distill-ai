# How distill-ai compares to other tools

`distill-ai` overlaps superficially with several existing tools. This
page explains where each one differs and when to reach for which.

The short version: nothing else is purpose-built to compress
command output for LLM consumption in a Unix pipe. Adjacent tools either
target humans, target a single format, or solve a different problem
(diffing, viewing, shipping).

## Summary table

| Tool                | Purpose                          | Format scope     | LLM-aware | In a pipe |
|---------------------|----------------------------------|------------------|-----------|-----------|
| `distill-ai`        | Compress for LLM context         | Multi-format     | Yes       | Yes       |
| `grep` / `ripgrep`  | Find matching lines              | Generic regex    | No        | Yes       |
| `lnav`              | Interactive log viewer           | Multi-format     | No        | No (TUI)  |
| `delta`             | Pretty-print git diffs           | Diff only        | No        | Yes       |
| `difftastic`        | Syntax-aware diff                | Diff only        | No        | Yes       |
| `bat`               | Syntax-highlighted `cat`         | Generic          | No        | Yes       |
| `jq`                | JSON query / transform           | JSON only        | No        | Yes       |
| `pytest-clarity`    | Better pytest assertion output   | pytest only      | No        | No (in-process) |
| `jest-stare`        | HTML report for Jest             | Jest only        | No        | No (file output) |
| `stern`             | Multi-pod kubectl log tail       | k8s only         | No        | Yes       |
| `humanlog`          | Pretty-print structured logs     | JSON logs        | No        | Yes       |

## grep / ripgrep

**What it does:** Returns lines matching a regex.

**Why it's not a substitute:** A test failure or stack trace is one
*event* spanning many lines. `grep "ERROR"` either misses the event
boundary entirely (just the matching line, no traceback) or floods you
with noise (matches `ERROR` in unrelated log lines too).

**When to use grep instead:** When you already know exactly which line
contains the answer and just want a needle.

## lnav

**What it does:** Interactive curses-based log viewer with multi-format
parsing.

**Why it's not a substitute:** `lnav` is excellent — but it's a TUI for
humans. You can't pipe its output to an LLM. It targets the exploration
problem, not the compression problem.

**When to use lnav instead:** Interactive exploration of large log
archives. Filtering, querying, jumping between events. If you're the one
debugging in a terminal, `lnav` is better than `distill-ai`.

## delta / difftastic

**What they do:** Render git diffs more readably. Syntax-aware diffing
(difftastic) understands code structure.

**Why they're not substitutes:** Diff output isn't log output. Different
problem space entirely. We've considered (and rejected) a `--diff` mode
in distill-ai for "what changed between this log and the last one";
that's distinct from source-code diffing.

**When to use them instead:** Code review. Pretty git diffs.

## bat

**What it does:** `cat` with syntax highlighting and line numbers.

**Why it's not a substitute:** `bat` shows you *more* (colour,
decoration), not less. Useful for humans reading code; counterproductive
for LLM input (ANSI codes burn tokens).

**When to use bat instead:** Reading code or logs interactively.

## jq

**What it does:** Query and transform JSON.

**Why it's not a substitute:** `jq` requires you to know the schema and
write a query. For a structured log stream you understand, `jq` is
faster and more precise than distill-ai. For arbitrary mixed output
(tests, stack traces, build chatter, occasional JSON), distill-ai's
format-aware extraction is what you want.

**When to use jq instead:** Targeted extraction from JSON you already
understand. Often complementary: `kubectl logs ... | jq 'select(.level=="error")' | distill-ai json`.

## pytest-clarity, jest-stare, format-specific prettifiers

**What they do:** Improve the human-readable output of a single test
framework.

**Why they're not substitutes:** They run in-process or post-process per
framework. You'd need a different one for every tool. `distill-ai` is
the unified tool that handles many formats and produces consistent
output across them — important for an agent that runs `pytest` in one
repo and `go test` in another.

**When to use them instead:** When you only ever use one framework and
the output is consumed by humans.

## stern, humanlog, structured-log prettifiers

**What they do:** Pretty-print structured logs (JSON, logfmt) for human
reading. `stern` aggregates multi-pod k8s logs.

**Why they're not substitutes:** They format for human eyes, not LLM
consumption. They expand information density (colours, alignment) rather
than compressing it. `humanlog`'s output of a single log line is often
*larger* than the JSON it consumed.

**When to use them instead:** Live tailing logs in a terminal you're
watching personally.

## What about "just letting the agent read the raw output"?

This is the actual default state and the thing distill-ai displaces.

**Why it's bad:**

1. **Cost.** A 50k-line log costs ~$0.50–$2 in input tokens per read.
   Across an iterative debugging session with repeated reads, this is
   real money.
2. **Latency.** The agent spends seconds parsing noise before reasoning.
3. **Quality.** Agents often latch onto the first error-like line they
   see, which may be a misleading warning, not the actual failure.
4. **Context window pressure.** Big logs crowd out the rest of the
   session — the agent forgets earlier conversation to make room.

distill-ai trades a tiny up-front CPU cost (parsing) for large savings
across all four dimensions.

## What about "I'll write a one-liner for each tool"?

You can. Many people do, badly and inconsistently. The argument for a
shared tool is the same as the argument for any shared tool: the awk
pipeline you'd write for pytest doesn't help your colleague debugging
Jest, your future self three months from now, or the agent invoking
commands across a polyglot repo.

`distill-ai` is the version of "your team's collection of log-mungling
one-liners" that's tested, format-aware, and consistent.
