# Acceptance criteria and evidence

Every acceptance criterion from PRD §9 maps to a named test or isolation
script. Commands below are the exact evidence commands; `make test` runs the
unit/golden half and `make test-isolation` runs the container half
(`AGENTBOX_REQUIRE_ENGINE=1` fails instead of skipping).

## Acceptance criteria

| AC | Criterion (abridged) | Test / script | Evidence command |
| --- | --- | --- | --- |
| 1 | `pwd` inside is `/workspace` | `tests/isolation/00-boundary.sh` | `bash tests/isolation/00-boundary.sh` |
| 2 | no host home contents visible | `tests/isolation/00-boundary.sh` | same |
| 3 | no SSH keys visible | `tests/isolation/00-boundary.sh` | same |
| 4 | no Docker socket | `tests/isolation/00-boundary.sh` | same |
| 5 | workspace writes host-owned | `tests/isolation/10-ownership.sh` | `bash tests/isolation/10-ownership.sh` |
| 6 | non-root write to `/etc` fails | `tests/isolation/10-ownership.sh` | same |
| 7 | `--ro` denies writes, dry-run shows `:ro` | `tests/isolation/16-ro.sh` | `bash tests/isolation/16-ro.sh` |
| 8 | guardrail refusal from ancestor of `$HOME` | `tests/isolation/18-guardrail.sh` | `bash tests/isolation/18-guardrail.sh` |
| 9 | no containers remain (normal/SIGINT/SIGKILL sweep) | `tests/isolation/12-lifecycle.sh` | `bash tests/isolation/12-lifecycle.sh` |
| 10 | per-project state volumes, no bleed | `tests/isolation/11-state.sh` | `bash tests/isolation/11-state.sh` |
| 11 | `--ephemeral` creates no volume | `tests/isolation/11-state.sh` | same |
| 12 | agent exit code passthrough (`exit 7`) | `tests/isolation/00-boundary.sh` | same |
| 13 | host env not inherited (`AWS_SECRET_ACCESS_KEY`) | `tests/isolation/13-env.sh` | `bash tests/isolation/13-env.sh` |
| 14 | `ANTHROPIC_API_KEY` reaches pi/claude containers | `internal/adapter` unit tests + `tests/isolation/13-env.sh` | `go test ./internal/adapter/` |
| 15 | dry-run redacts allowlisted secrets as `<set>` | `tests/cli/dryrun_golden_test.go`, `tests/isolation/13-env.sh` | `go test ./tests/cli/ -run Golden` |
| 16 | no secret in image history/filesystem | `scripts/release-check.sh` | `make release-check` |
| 17 | `docker inspect` hardening defaults | `tests/isolation/14-hardening.sh` | `bash tests/isolation/14-hardening.sh` |
| 18 | `--net none` cannot resolve/connect | `tests/isolation/15-net.sh` | `bash tests/isolation/15-net.sh` |
| 19 | `--help` exits 0 with every flag | `tests/cli/help_test.go` | `go test ./tests/cli/ -run Help` |
| 20 | unknown flag exits nonzero and suggests `--` | `tests/cli/help_test.go`, `tests/cli/parse_test.go` | same |
| 21 | dry-run golden stable under `AGENTBOX_TEST_NAME`, no TTY | `tests/cli/dryrun_golden_test.go` + `tests/isolation/00-boundary.sh` | `go test ./tests/cli/ -run Golden` |
| 22 | project config boundary keys rejected | `tests/cli/config_test.go`, `internal/config` | `go test ./tests/cli/ -run Config` |

FR-level coverage beyond the numbered ACs lives in the same packages:
`internal/run` (spec assembly, banner, conflicts), `internal/state`
(volume naming/labels), `internal/workspace` (canonicalization, guardrail,
notices, SELinux), `internal/auth`/`internal/doctor`
(subcommands), `internal/entrypoint` (FR-9 script contract),
`internal/config` (FR-12).

## Non-functional requirements

| NFR | Requirement | Evidence |
| --- | --- | --- |
| NFR-1 | startup overhead <= 300 ms | ~231 ms measured (CLI + engine bookkeeping + container start, excluding agent boot and image pull); see "Startup performance" below |
| NFR-2 | static binary, CGO_ENABLED=0, <= 15 MB, `--help` works without Docker | `file ./agentbox` -> statically linked; 10.77 MB; `--help` needs no engine |
| NFR-3 | zero writes outside workspace+state | `tests/isolation/17-nfr3.sh` (manifest of `$HOME` top level, `/etc`, `/var/run`, sensitive paths; `~/.docker` and the intentional `~/.agentbox` user folder excluded, the latter disabled with `--no-user-config`) |
| NFR-4 | no telemetry; only labeled update check | `doctor --no-check` skips it; `internal/doctor` tests assert the `agentbox/<ver>` user agent; no other HTTP calls in the tree |
| NFR-5 | Linux primary, macOS/WSL2 documented | `docs/troubleshooting.md` (engine matrix, macOS verdict, WSL2 DrvFs caveat, Windows best-effort) |
| NFR-6 | CI with Docker; isolation tests required | `.github/workflows/ci.yml` (unit + isolation + release-check jobs) |
| NFR-7 | README + four docs | `README.md`, `docs/isolation.md`, `docs/harnesses.md`, `docs/credentials.md`, `docs/troubleshooting.md` |
| NFR-8 | reproducible release manifest | `.github/workflows/release.yml` writes `release-manifest.json` (base digest, harness versions, image digests) and refuses to overwrite published tags |

## Image sizes (FR-10)

Two measurements are recorded because they differ slightly: `docker images`
shows the user-visible size (shared base layers counted), while
`docker image inspect .Size` (used by `release-check`) sums layer sizes.

| Image | `docker images` | `inspect .Size` | Budget | Note |
| --- | --- | --- | --- | --- |
| `agentbox-base:dev` | 299 MB | 285 MB | 400 MB | multi-stage build; stripped node, no yarn/corepack/headers |
| `agentbox-pi:dev-pi1.1.0` | 369 MB | 352 MB | 400 MB | FR-10 target met |
| `agentbox-opencode:dev-opencode2.0.26` | 515 MB | 492 MB | 560 MB (recorded) | upstream ~210 MB native binary (v2) |
| `agentbox-claude:dev-claude2.1.296` | 556 MB | 530 MB | 600 MB (recorded) | upstream ~257 MB native binary |

`make release-check` enforces these budgets, scans the images for secret
material, and asserts no secret appears in image history/config.

## Startup performance

Measured on a Linux development host (Docker 29.8.2), medians of 15 runs, `pi --version` as the process-boot proxy (the real 107 MB binary, not
a shim):

| Path | Median |
| --- | --- |
| native pi on the host | ~250 ms |
| bare `docker run` of the base image (`/bin/true`) | ~190 ms |
| `docker run` + entrypoint + `pi --version` (no agentbox) | ~377 ms |
| agentbox `--agent pi -- --version`, 20 linked-config mounts | ~418 ms |

Breakdown of the agentbox run:

- agent boot inside the container: ~187 ms (377 − 190)
- container creation (`docker run`): ~190 ms
- **agentbox CLI + engine bookkeeping: ~41 ms** — image inspect, one
  `docker volume create`, and one container listing that doubles as the
  engine-reachability check, the concurrent-run warning, and the stale sweep
- extra bind mounts from the linked user config: ~5–10 ms for 20 mounts

So agentbox adds **~41 ms over an identical bare `docker run`** of the same
harness. The rest of the gap to native pi is container cost, not agentbox
overhead. NFR-1's budget (≤ 300 ms excluding image pull and agent boot) is met
with ~231 ms.

Reductions applied after the first measurement (~505 ms → ~418 ms): the
separate `docker info` probe (35 ms) was replaced by the container listing,
the duplicate `docker ps` queries were merged into that one call, and the
post-run cleanup query is skipped after a clean `--rm` exit.

### TUI time-to-first-frame

`pi --version` measures process boot; the interactive TUI additionally loads
the user's extensions. With a linked host config containing seven configured
packages, the TUI took **7.5–9.2 s** to render in the container while native pi
took ~0.7–0.9 s (warm). Root cause: pi's extension loader (jiti) caches
transpiled packages under `$TMPDIR/jiti`, and the container's `/tmp` is a fresh
tmpfs per run, so every start re-transpiled every package (~5 s CPU, ~1 GB
RSS). Native pi keeps a warm `/tmp/jiti`.

Fix: the `pi` adapter sets `TMPDIR=/state/home/.cache/tmp` and
`JITI_RESPECT_TMPDIR_ENV=1`, so the jiti cache lives in the persistent
per-project state volume.

| pi TUI first frame | Before | After |
| --- | --- | --- |
| first run in a project (cold cache) | ~7.4 s | ~7.4 s |
| subsequent runs (warm cache) | 7.5–9.2 s | **~0.71–0.79 s** |

Warm container startup now matches warm native pi (~0.7–0.9 s). The cache is
per project (state volume), so a brand-new project pays the transpile once.
The cold run is somewhat slower than native pi's cold run (~4.9 s) because of
the container CPU quota; raising `--cpus` narrows that gap.

On the first run in a project the entrypoint prints
`agentbox: first run in this project: preparing the harness cache (later runs
start faster)` (once, suppressed by `--quiet`), so the slower start is not
mistaken for a hang. `tests/isolation/20-userdir.sh` asserts both the notice
on the first run and its absence on the second.

## Gate results (this worktree)

| Gate | Verifies | Result |
| --- | --- | --- |
| G0 | contracts match PRD §6; repo builds | `go build ./...`, `go vet ./...` clean |
| G1 | AC 1–4, 12, 19–21 | `make test` + `00-boundary.sh` green |
| G2 | AC 5–6, 8–11, 13–14 (pi), 17, 18, NFR-3 | isolation suite green (11 scripts) |
| G3 | AC 1–21 + docs | full suite + docs present |
| G4 | AC 22 + NFR 1/2/4/5/8 + Podman smoke | config tests green; NFR measurements above; Podman job is CI-only (podman is not installed on the dev host — see "Environment limits") |

## Environment limits of the local verification

- **Podman** is not installed on the verification host. The `podman-smoke` CI
  job is non-blocking and exercises `--engine podman` with
  `--userns=keep-id`; the podman grammar is additionally covered by unit and
  dry-run golden tests. The plan's open decision #6 defaults to a
  non-blocking job until G4.
- **macOS** hardware was not available; `docs/troubleshooting.md` records the
  Docker Desktop verdict per the plan's default (supported with ownership
  caveats) and the CI matrix covers Linux only.
- **Multi-arch image publishing** is defined in `.github/workflows/release.yml`
  and cannot execute locally; digests are recorded at release time by the
  manifest job.
- The isolation suite ran on **rootful Docker** (`29.8.2`, overlay2,
  cgroup v2) on Linux; SELinux was not enforcing on this host, so the `:z`
  relabel path is unit-tested rather than exercised live.
