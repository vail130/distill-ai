# Security Policy

## Reporting a vulnerability

If you believe you've found a security vulnerability in `distill-ai`,
please report it privately. **Do not file a public issue.**

### How to report

Email the maintainers at **<security-contact@example.com>** (replace
with a real address before publishing the repo) with:

- A description of the issue
- Steps to reproduce, or a proof-of-concept
- The version of `distill-ai` affected (`distill-ai --version`)
- Your assessment of the impact

Alternatively, use GitHub's [private vulnerability reporting][gh-pvr]
via the **Security** tab on this repository.

[gh-pvr]: https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability

### What to expect

- **Acknowledgement** within 5 business days.
- **Initial assessment** within 10 business days.
- **Fix and disclosure timeline** agreed with you, typically ≤90 days
  from acknowledgement.
- **Credit** in the security advisory and changelog (unless you prefer
  to remain anonymous).

## Threat model

`distill-ai` is a Unix filter. It reads input, transforms it, writes
output. It does not:

- Make network connections (hard rule; any network behaviour is itself a
  bug).
- Write files outside paths the user explicitly passes.
- Execute subprocesses.
- Load remote code or configuration.

In-scope vulnerabilities include:

- **Input parsing crashes** that could be triggered by hostile input
  (e.g., malformed test output from an attacker-controlled CI run).
- **Resource exhaustion** (memory or CPU) on adversarial input.
- **Path traversal** in `--config` or file argument handling.
- **Information disclosure** through unexpected output (e.g., a parser
  bug that emits content marked for suppression).

Out-of-scope:

- The agent or LLM consumer mishandling distilled output. That's the
  consumer's responsibility.
- Bugs in dependencies; report those upstream, but feel free to also
  notify us so we can update.

## Supported versions

Only the latest `v1.x` release receives security fixes. Older majors are
out of support once a new major is released.

| Version | Supported          |
|---------|--------------------|
| 1.x     | :white_check_mark: |
| < 1.0   | :x:                |
