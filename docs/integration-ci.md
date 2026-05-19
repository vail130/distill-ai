# Integrating distill-ai with CI systems

`distill-ai` is a Unix filter — it works anywhere stdin and stdout
do. In CI the value is twofold: (1) the JSON output is a versioned
public API, so build dashboards can consume structured failure
events without re-parsing every runner's format; (2) the
[envelope strippers](./envelope.md) peel the CI runner's framing
so the inner format (gotest, pytest, jest) detects correctly even
inside a `gh run view --log` or `glab ci trace` capture.

This page is the recipe shop. Each CI system gets a one-screen
example you can paste into your workflow.

The recipes below assume `distill-ai` is on `$PATH` in the CI
runner image. The [install](../README.md#install) section of the
README covers Homebrew, `go install`, and direct download.

## GitHub Actions

Two patterns are useful: distill the test step inline, and distill
the captured log after the fact for posting to a PR.

### Inline: pipe the test command

```yaml
name: Test
on: [push]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.26' }
      - name: Install distill-ai
        run: go install github.com/vail130/distill-ai/cmd/distill-ai@latest
      - name: Run tests
        run: |
          set -o pipefail
          go test ./... 2>&1 | distill-ai --output=json > distilled.json
        continue-on-error: false
      - name: Upload distilled output
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: distilled-failures
          path: distilled.json
```

`set -o pipefail` is the key line: without it, a `go test` failure
is masked by `distill-ai`'s `1` exit (no events) when the build
fails cleanly. With pipefail, the workflow step fails as soon as
`go test` does, and the artifact upload runs in the `if: always()`
branch.

### After-the-fact: distill `gh run view --log`

```yaml
- name: Post distilled failure to PR
  if: failure() && github.event_name == 'pull_request'
  run: |
    gh run view ${{ github.run_id }} --log \
      | distill-ai --strip-envelope=github-actions --output=markdown \
      > /tmp/distilled.md
    gh pr comment ${{ github.event.pull_request.number }} \
      --body-file /tmp/distilled.md
  env:
    GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

`--strip-envelope=github-actions` peels the per-line timestamp,
the `##[group]` / `##[endgroup]` markers, and the
`##[error]` / `##[warning]` workflow commands — so the inner
format (gotest / pytest / jest) detects from the cleaned bytes
the same way a raw `go test` log would. `--output=markdown`
renders fenced code blocks suitable for direct paste into a GitHub
PR comment.

## GitLab CI

The shape is the same; only the YAML keys differ.

### Inline: pipe the test command

```yaml
test:
  image: golang:1.26
  script:
    - go install github.com/vail130/distill-ai/cmd/distill-ai@latest
    - set -o pipefail
    - go test ./... 2>&1 | distill-ai --output=json > distilled.json
  artifacts:
    when: always
    paths:
      - distilled.json
    expire_in: 7 days
```

### After-the-fact: distill `glab ci trace`

```yaml
post-failure-summary:
  stage: notify
  when: on_failure
  script:
    - glab ci trace --job=test | distill-ai --strip-envelope=gitlab-ci --output=markdown > /tmp/distilled.md
    - cat /tmp/distilled.md
```

`--strip-envelope=gitlab-ci` peels the `section_start:` /
`section_end:` markers, the trailing carriage returns the GitLab
runner emits, and the `glab` per-line preamble (an RFC3339-Z
timestamp + step number + stream code, see
[`docs/envelope.md`](./envelope.md) § gitlab-ci for the details).
The terminal "Job failed: exit code N" line is consumed and
surfaced as an `envelope_step_failure` Event alongside the inner
parser's events.

## Generic shell-script CI (Jenkins, Buildkite, CircleCI, …)

The lowest-common-denominator pattern: pipe the build's combined
stdout/stderr through distill-ai, store the JSON as a build
artifact, optionally post the markdown rendering to a chat
channel.

```bash
#!/bin/sh
set -o pipefail
# Run the build; capture both streams.
make test 2>&1 | distill-ai --output=json --budget=8000 > distilled.json
exit_code=$?
case $exit_code in
  0) echo "Tests passed but events were emitted (warnings, maybe);"
     echo "see distilled.json." ;;
  1) echo "Clean build — no events." ;;
  2) echo "distill-ai itself failed; raw output:" ; cat distilled.json ;;
  3) echo "Build had events; some were dropped to fit --budget." ;;
esac
exit $exit_code
```

`--budget=8000` keeps the JSON artifact bounded so a job that
generates thousands of failures doesn't fill the artifact store.
Drop the flag entirely if you want every event.

## Troubleshooting

### The format detects as `generic` even though the test runner is recognised

Three likely causes:

1. **The envelope isn't being stripped.** Pass
   `--strip-envelope=github-actions` (or `=gitlab-ci`) explicitly
   instead of relying on autodetect. The auto path may pick the
   wrong stripper when the log's preamble is shorter than the
   format-detection sample window.
2. **The test runner's first 16 KB of output is preamble.**
   distill-ai samples the first 16 KB to pick a format. The
   `docker-compose` envelope stripper drops pre-attach compose
   preamble, but other wrappers can still push markers out of the
   sample. Pass an explicit format argument (`distill-ai pytest`)
   to skip detection.
3. **The test runner is wrapped in a `docker compose` log.**
   distill-ai's `docker-compose` envelope stripper peels the
   per-line `<service>  | ` (or `<service>-<replica>  | `) prefix
   automatically. When the docker-compose log itself is nested
   inside a CI job (a GitLab CI step that runs `docker compose up`
   under `gitlab-runner`), `envelope.Wrap` chains both strippers
   and `chosen.Name()` becomes `gitlab-ci+docker-compose`. No
   pre-filter required:

   ```bash
   docker compose logs test 2>&1 | distill-ai
   ```

   See [docs/envelope.md § docker-compose](./envelope.md#docker-compose)
   for the supported prefix shapes (uncoloured form only; on a
   runner that emits the coloured shape by default, disable colour
   in the docker compose invocation itself before piping into
   distill-ai).

### `distill-ai` exits 0 in CI even when tests fail

Without `set -o pipefail` (or its equivalent in your CI shell),
the pipe's exit code is the rightmost command — `distill-ai` —
which returns 0 when it has events to report. The build appears
to pass even though the test command failed.

Add `set -o pipefail` before the pipeline, or capture the test
command's exit explicitly with `${PIPESTATUS[0]}` (bash) /
`status=$?` after the pipe completes (POSIX sh requires the
intermediate capture to be set up manually).

### The `--budget` cap drops events I needed

`--budget=N` is a hard cap on output tokens. When `distill-ai`
must drop events, it drops the lowest-severity ones first and
exits 3 to signal partial output. Two options:

- Raise the budget. The full-fidelity output's token count is
  reported in the summary; size the budget above it.
- Run `distill-ai explain` against the same input to see exactly
  which events the budget would drop, then decide whether the
  budget is the right shape.

### CI can't `brew install` or `go install`

Pre-build the binary in one workflow and cache the artifact, or
download the platform-specific archive from
[GitHub Releases](https://github.com/vail130/distill-ai/releases)
and verify the checksum. No external network is required at run
time once the binary is on disk — distill-ai never phones home.

## See also

- [`docs/envelope.md`](./envelope.md) for the full envelope-stripper
  reference: detection, transformation, signal events.
- [`man distill-ai-run`](../man/man1/distill-ai-run.1) for every
  flag.
- [Integration with Claude Code](./integration-claude-code.md)
  and [Integration with opencode](./integration-opencode.md) for
  the agent-side equivalents.
