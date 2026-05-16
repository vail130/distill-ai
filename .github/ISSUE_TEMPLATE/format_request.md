---
name: Format request
about: Request support for a new log / test / output format
title: "format: <tool name>"
labels: format-request
assignees: ''
---

> Before filing, check the [list of supported formats](../../README.md#usage)
> and the [v1.1 roadmap](../../TODO.md#v11-post-launch) — your format
> may already be in flight.

## Tool name

<!-- e.g., rspec, cargo test, pino, zap, kubectl logs, ... -->

## Why this matters

<!--
Concrete scenarios where this would save tokens. Don't just say "I use
this tool". Explain what gets piped to distill-ai today and what's
painful.
-->

## Sample input

<!--
**Required.** Attach (don't paste) a sample log file or test output that
demonstrates the format. Aim for ≥1 KB and ≤1 MB.

Include:
- A clean / passing run (so we know what's noise)
- A failing run
- Any mixed warning + error case

Redact secrets, hostnames, or anything sensitive.
-->

## Expected extraction

<!--
For the failing run above, what events would you expect distill-ai to
emit? Severity, kind, location, title. Rough sketch is fine.

Example:
  Event 1:
    severity: error
    kind: test_failure
    title: "AssertionError: ..."
    location: spec/models/user_spec.rb:42
-->

## Format characteristics

<!-- Check all that apply. -->

- [ ] Line-oriented (one event per N lines, predictable boundaries)
- [ ] Multi-line blocks with clear start / end markers
- [ ] JSON / structured per line
- [ ] Embeds stack traces with vendor frames worth collapsing
- [ ] Has explicit severity field
- [ ] Has source location (file:line) for events
- [ ] Mixes with other formats (e.g., a test runner whose tests emit JSON logs)

## Detection hints

<!--
What's a unique marker we can use for autodetection? A header line,
a specific symbol, a JSON field, etc. Be specific.

Example: "Every rspec failure block starts with a line matching
`^\s*\d+\)\s+`"
-->

## Are you offering to implement?

- [ ] Yes — I'll open a PR following [Adding a format](../../CONTRIBUTING.md#adding-a-format)
- [ ] No — requesting only
