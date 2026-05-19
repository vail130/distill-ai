# Known issues

Tracked drift between the interface specifications (ARCHITECTURE.md,
SCHEMA.md, godoc, scoped milestones in TODO.md) and the implementation
or scoped plan. None of these block the current milestone; each one
has a recommended landing point so the fix isn't lost.

Format: one issue per heading, with **Observed**, **Why it matters**,
**Owning milestone**, and **Recommendation**. Tick the issue off by
deleting it once the recommendation lands.

## 1. `Source` interface mid-stream error contract is broken

**Observed.**
[`Source.Source(ctx)`](./internal/pipeline/pipeline.go) returns
`(<-chan Event, error)`. The contract documented on
[`FormatSource.Source`](./internal/pipeline/pipeline.go) says "Errors
from `Format.Parse` propagate directly; the caller is responsible for
draining whatever events the parser emitted before the error."
[`Format.Parse`](./internal/formats/format.go) godoc says "Callers
must drain the channel before inspecting the error."

But `Pipeline.Run` only checks the error **before** starting the
relay goroutine. Once the channel is open, any error that arrives
from `Format.Parse` later in the run is silently dropped — there is
no second error return path on the channel.

**Why it matters.** Every parser shipped today is happy with
"emit-then-close on EOF / ctx-cancel; never error mid-stream"
because pytest / jest / generic all parse text-shaped input where
errors degrade to a best-effort Event rather than a hard stop.
M10's gotest is different: the `-json` mode (M10.4) consumes a
structured JSON-per-line protocol where a mid-stream JSON parse
error or a malformed build-failure block is a genuine reason to
surface an error that's currently invisible.

**Owning milestone.** M10.4 (gotest `-json` mode handling).

**Recommendation.** Two options:

1. **Narrow the contract.** Update `Format.Parse`'s godoc to "no
   streaming errors; close the channel and return early on
   unrecoverable failure. Convert non-fatal problems to a
   best-effort Event with `Severity=SeverityError` and continue."
   Matches what every existing parser does; aligns the spec with
   reality. Zero code change. M10.4's `-json` parse errors would
   emit a best-effort `Event{Kind:"json_parse_error"}` and continue
   to the next line.
2. **Widen the contract.** Add a side-channel error return — either
   a `<-chan error` alongside the event channel, or fold the error
   into a sentinel Event with a reserved `Kind`. Code change in
   `Pipeline.Run` and every Source implementation.

The first option is the lower-cost answer and probably the right one
for v1. Defer the decision to M10.4 implementation time; pre-decide
here that the default is option 1 unless M10.4 surfaces a concrete
need for option 2.

## 2. Docker-compose service-label prefix breaks format detection inside CI

**Observed.** When a GitLab CI (or any) job runs `docker compose up`
or `docker compose run`, the docker daemon prefixes every container
stdout line with `<service>-<replica>  | `:

```
testrunner-1  | === RUN   TestThing
testrunner-1  | --- FAIL: TestThing (0.01s)
testrunner-1  | FAIL    go.example.com/m/internal/somepkg           0.007s
```

No envelope stripper recognises or peels this prefix, so the format
detector sees indented lines that don't match `^=== RUN` or `^--- FAIL`
and falls back to `generic`. Even after stripping the gitlab-ci
envelope, the docker-compose framing remains and the inner format
fails to detect.

Reproduction: a real production GitLab CI integration-test job that
runs `docker compose` with a single test-runner service. After
gitlab-ci envelope stripping the cleaned bytes still carry the
docker-compose prefix, the format detector picks `generic`, and the
actual `=== FAIL:` and `FAIL\t<pkg>` markers in the log produce
zero Events.

**Why it matters.** A non-trivial fraction of real-world CI test
runs use `docker compose`. Detection failure means the binary's
output is the runner's terminal "Job failed" line and nothing about
the actual test failures inside the container.

**Owning milestone.** v1.5 (the "more log / test formats" theme per
[ADR-0002](./docs/decisions/0002-v1.0-scope-and-post-v1.0-roadmap.md)).
A `docker-compose` envelope stripper that peels the
`<service>-<replica>  | ` prefix is the natural shape: it composes
with the existing CI strippers (cleaned bytes pass through gitlab-ci,
then docker-compose, then the format detector). Implementation cost
is small — the prefix is a tab-aligned column with a `|` separator —
but it warrants its own milestone for fixture coverage and the
multi-replica corner cases (`testrunner-1`, `testrunner-2`,
…).

**Recommendation.** Open M-something under v1.5 for the
`docker-compose` envelope. Until that ships, document the
workaround in `docs/integration-ci.md` (M16.4): pipe the log
through `sed -E 's/^[A-Za-z0-9_.-]+ +\| //'` to peel the prefix
before `distill-ai`.

## 3. Format-detection sample window too small for envelope-wrapped logs

**Observed.** `internal/detect/detect.go:29` pins
`SampleSize = 4096` bytes. The detector reads the first 4 KB of
cleaned input and asks every registered Format to score the sample.

For raw test output (the fixtures we ship today, all under 1 KB)
this is plenty. For real CI logs the first 4 KB is consumed by
runner setup chatter — image pull, secret resolution, git clone,
docker compose up — well before the test runner emits anything.
Even after envelope stripping the actual `--- FAIL:` / `=== FAIL:` /
panic markers fall outside the sample window.

Reproduction: the same real-world log as issue #2. After gitlab-ci
stripping, the first 4 KB still does not contain a single gotest
marker; the gotest detector returns 0.0 and `generic` wins by
default.

**Why it matters.** The combination of issues #2 and #3 is what
makes a real CI log of this shape produce zero useful Events. Even
fixing docker-compose envelope stripping (#2) doesn't help if the
sample window can't reach the test output. And making the sample
large enough to always reach it (say 128 KB) bloats short-input
parsing unnecessarily.

**Owning milestone.** M3.x revisit, slated for post-v1.0 once
issues #2 and #4 are in motion (so the trade-off can be measured
against real fixtures).

**Recommendation.** Three options, in increasing order of cost /
quality:

1. **Bump `SampleSize` to 16 KB or 32 KB.** Cheap; one constant
   change; covers most envelope-wrapped logs. Trade-off: slightly
   more memory per Detect call (capped — the detector still buffers
   the sample, then prepends it to the stream).
2. **Multi-window peek.** Read 4 KB, score, and if every Format
   below the threshold, read another 32 KB and rescore. Bounded
   work, better worst-case detection. More plumbing.
3. **Re-sample after envelope stripping.** The pipeline already
   wraps the input through the stripper before detection;
   teach the detector to consume cleaned bytes until N
   non-envelope lines have flowed, then score. Most accurate;
   most code change. The pre-condition (envelope strip running
   first) is already satisfied.

Decide at implementation time, but option 1 is the lowest-cost
fix that unblocks this class of input. Add a property test that a
real-shape fixture detects correctly after the bump.

## 4. No real-world CI fixture covers the envelope-plus-docker-compose shape

**Observed.** Every fixture under
`test/integration/testdata/fixtures/` is hand-crafted and small
(< 30 lines). The longest is `gha-gotest-fail.input` at 11 lines.
None of them combine a CI envelope, docker-compose framing, and a
long preamble before the test output — the combination that
issues #1, #2, and #3 each individually surface.

**Why it matters.** The four bugs above all hid behind small,
well-formed fixtures. Without a realistic fixture, future
regressions (envelope handling, detector behaviour, format
parsers) will reproduce the same blind spot.

**Owning milestone.** M16.4 (integration recipes) is the natural
home: that milestone already calls for `docs/integration-ci.md`
with worked GitHub Actions / GitLab CI examples, and a real fixture
makes those examples honest. Alternative: M11.5 / M10.5 follow-up
to extend the per-format `testdata/` set.

**Recommendation.** Source a fixture from an open-source Go
project's public CI logs. The private log that surfaced these
bugs cannot be checked in — internal package names, runner
hostnames, vault references, docker-compose service shapes leak
architectural information that would require error-prone
sanitisation.

Suggested public candidates:

- `gitlab.com/gitlab-org/cli` (the `glab` repo itself) — a public
  Go project on gitlab.com; logs there exhibit the exact glab
  preamble shape without any organisation's internal naming.
- `gitlab.com/gitlab-org/gitlab-runner` — another public Go
  project, runs Go tests in its own CI with `--timestamps`.
- Any failing run of `kubernetes/kubernetes` or
  `prometheus/prometheus` in their GitHub Actions pipelines for
  the github-actions-side equivalent (already partially covered
  by `gha-gotest-fail.input`).

The fixture only needs the SHAPE — envelope + docker-compose
prefix + long preamble + real `--- FAIL:` markers somewhere past
the 4 KB mark — not the specific content. A 200-line synthesised
log built from those public projects' patterns would serve.
