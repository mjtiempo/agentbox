# AGENT_PLAN.md — agentbox build plan

> Derived from `PRD.md` v0.2. This is an execution plan for delegating the work to coding agents.
> Waves run sequentially; tasks inside a wave run in parallel **only** where file ownership is disjoint.
> Every gate is verified with a fresh-context review lane and real command evidence.

---

## 0. Operating rules for agents

1. **Read `PRD.md` first.** Requirements, priorities, and acceptance criteria live there; this plan only schedules them. Where the plan and the PRD disagree, the PRD wins.
2. **One writer per file.** A task owns the files it lists. If a task needs a change in a file owned by another task, it reports the need instead of editing.
3. **Contracts are frozen in Wave 0** (§2.2). Changing a contract requires the integrator; parallel lanes must not drift from it.
4. **Evidence over claims.** Every task ends with the exact commands run and their observed output (exit codes, test names). "It should work" is not evidence.
5. **Non-goals are scope.** No devcontainer features, no orchestrator, no host-side execution path for `agentbox` itself (PRD §3.2).
6. **Never weaken the isolation contract to make a test pass.** If an AC cannot be met, stop and report; do not relax flags, drops, or mounts.
7. **Secrets discipline.** Never write key material into the repo, images, golden files, or argv. Test fixtures use obviously fake values and assert redaction.

### Task card template

```text
Task:        <id> <name>
Goal:        <one paragraph>
Depends on:  <task ids>
Owns:        <file globs>
Deliverable: <concrete artifacts>
Evidence:    <commands + expected observations>
ACs:         <PRD acceptance criteria covered>
Out of scope:<explicit exclusions>
```

---

## 1. Target architecture

### 1.1 Repo layout (canonical; frozen in Wave 0)

```text
agentbox/
  cmd/agentbox/main.go            # process entry; wires cli -> run
  internal/cli/                   # flag parsing, dispatch, help, errors, exit codes
  internal/adapter/               # harness descriptors: pi, claude, opencode, shell, image:<ref>
  internal/engine/                # engine CLI wrapper: docker/podman, pure argv builder, exec
  internal/workspace/             # canonicalization, guardrail, notices scan, SELinux probe
  internal/state/                 # volume naming, /state mount spec, ls/rm/gc helpers
  internal/run/                   # orchestration: spec assembly, naming, signals, cleanup, reaping
  internal/entrypoint/            # entrypoint.sh (embedded via go:embed)
  images/base/Dockerfile          # node:24-bookworm-slim digest + bash ca-certificates git ripgrep util-linux
  images/pi/Dockerfile            # base + pinned pi npm package
  images/claude/Dockerfile        # P1
  images/opencode/Dockerfile      # P1
  tests/cli/                      # Go golden tests (argv, help, errors) — no Docker needed
  tests/isolation/                # bash scripts driving real containers; per-engine-mode assertions
  .github/workflows/ci.yml        # build + unit + isolation on Docker, podman smoke (M2)
  docs/                           # isolation.md, harnesses.md, credentials.md, troubleshooting.md
  Makefile                        # build, test, test-isolation, image, vet, release-check
  go.mod / go.sum                 # Go 1.22+, CGO_ENABLED=0, TOML lib only (P1)
  AGENT_PLAN.md  PRD.md  README.md
```

### 1.2 Frozen contracts (Wave 0 deliverable, changes need integrator)

```go
// internal/engine/spec.go — the single source of truth for what runs.
type Mount struct {
    Host, Container string
    ReadOnly        bool
    Relabel         string // "" | "z" (FR-2 SELinux relabel); rendered as a mount option
}

type EnvVar struct {
    Name  string // e.g. ANTHROPIC_API_KEY
    Value string // literal value; "" => bare "-e Name" (value read from agentbox's own env)
    Set   bool   // host-side presence; false => omit the flag entirely (never pass empty)
}
// Frozen rendering rule: Set=false omit; Set=true && Value=="" => "-e Name";
// Set=true && Value!="" => "-e Name=Value". --dry-run renders configured secrets as <set>/<unset>.

type RunSpec struct {
    Name      string            // container name (AGENTBOX_TEST_NAME overrides)
    Image     string            // ensure/pull happens in the engine layer before Run (FR-10/E3); not part of RunArgv
    Cmd       []string          // [adapter args] [user args]; entrypoint comes from the image or --entrypoint, never argv; CLI "--" is not forwarded
    Env       []EnvVar
    Workspace Mount
    State     *Mount            // nil when --ephemeral
    Net       string            // bridge|none|host
    Rm        bool              // always true (FR-3) until --keep (P2)
    Workdir   string            // "/workspace" (FR-2) until --workdir-path (P2)
    CapDrop   []string          // ["ALL"] (FR-8)
    CapAdd    []string          // ["CHOWN","SETUID","SETGID"] (FR-8/FR-9); each rendered as its own --cap-add
    NoNewPriv bool              // always true (FR-8)
    Init      bool              // always true (FR-3)
    Tmpfs     []string          // ["/tmp:size=256m"] (FR-8)
    CPUs      string            // "2"
    Memory    string            // "4g"
    PidsLimit int
    TTY       bool              // "-it" when host stdin is a TTY and !--no-tty
    User      string            // "" unless --user or image:<ref>
    Userns    string            // "" | "keep-id" (Podman ownership, FR-9 P2; D2); rendered --userns=<value>
    ReadOnly  bool              // workspace readonly (AGENTBOX_READONLY=1 marker too)
    Labels    map[string]string // agentbox=1, agentbox.run, agentbox.project, agentbox.uid, agentbox.owner
}

type Engine interface {
    Name() string                                   // "docker"|"podman"
    RunArgv(RunSpec) []string                       // pure; golden-testable; includes "run"
    Run(ctx context.Context, argv []string) (int, error) // exec + exit-code mapping
    // image ensure/pull (FR-10, E3) and probe/cleanup surface (inspect, remove, volume list) land in Wave 2/E3
}
```

```go
// internal/adapter/adapter.go
type Adapter struct {
    Name          string            // pi|claude|opencode|shell
    Image         string            // e.g. agentbox-pi:<agentboxVer>-pi<harnessVer>
    Env           []EnvVar          // always-set container vars, e.g. PI_CODING_AGENT_DIR=/state/pi
    CredEnv       []string          // host-env allowlist names; BuildEnv emits EnvVar{Set: hostHas(k)} (bare -e) per FR-6
    CredFile      string            // container target for --credentials ("" = none)
    Home          string            // container HOME; "" => /state/home; runner emits AGENTBOX_HOME (FR-9)
    DefaultArgs   []string
    EphemeralArgs []string          // appended when --ephemeral (pi: ["--no-session"])
    UseEntrypoint bool              // true: force --entrypoint /entrypoint.sh (built-ins, incl. --image override); false: image's own (image:<ref>)
}
```

```sh
# internal/entrypoint/entrypoint.sh contract (FR-9)
# 1. If EUID != 0: exec "$@"                       (explicit --user / engine userns)
# 2. mkdir -p "${AGENTBOX_HOME:-/state/home}"
# 3. chown "$AGENTBOX_UID:$AGENTBOX_GID" /state "${AGENTBOX_HOME:-/state/home}"  # non-recursive; never chown -R
# 4. export HOME="${AGENTBOX_HOME:-/state/home}"
# 5. exec setpriv --reuid "$AGENTBOX_UID" --regid "$AGENTBOX_GID" --clear-groups -- "$@"
# AGENTBOX_HOME comes from Adapter.Home (runner-emitted, default /state/home). Non-recursive is deliberate:
# the dropped agent owns everything it creates, and a recursive heal would re-walk large state volumes on
# every start. If a prior root-owned run leaves unwritable entries, remediate via state rm, not per-start chown -R.
```

Frozen format for `--dry-run` (golden-tested): one shell-quoted argv token per line, in this exact order —
`<engine> run` → lifecycle (`--rm --init`, plus `-it` when TTY) → `--name` → labels → identity (`--user`,
`--userns`) → `--net` → caps/hardening (`--cap-drop ALL`, one `--cap-add` per value, `--security-opt
no-new-privileges`) → limits (`--cpus`, `--memory`, `--pids-limit`) → `--tmpfs` → mounts (`-v`, options
joined `z,ro`/`z`/`ro` as applicable) → env (`-e`) → `--entrypoint` (only when `UseEntrypoint`) → image →
cmd. New flags land in their section only. Secrets render `<set>`/`<unset>`; `AGENTBOX_TEST_NAME` pins
name/run uuid/owner label; TTY detection governs `-it`.

---

## 2. Task waves

### Wave 0 — Foundation (serial, single agent)

- [ ] **F0 — repo skeleton & contracts**
  - Depends on: —
  - Owns: `go.mod`, `Makefile`, `.gitignore`, all empty package dirs + `spec.go`/`adapter.go` contract files, `cmd/agentbox/main.go` stub, `git init` (worktree isolation for later waves needs a repo).
  - Deliverable: `go build ./...` passes; `go vet ./...` clean; `make build` produces `agentbox`; contracts compile; `Makefile` targets defined (`test`, `test-isolation`, and a runnable `image` that invokes `docker build` for both Dockerfiles by path — F0 owns this file into Wave 1; later image-target gaps route through A5); dry-run format documented in `spec.go` comment; `--` boundary semantics documented there (parser consumes it, never forwards it as argv).
  - Evidence: build + vet output; `./agentbox --help` stub exits 0.
  - Exit: contracts reviewed by a fresh-context read-only agent against PRD §6 (FR-1…FR-12) before Wave 1 starts.

### Wave 1 — M0 walking skeleton (parallel lanes + serial integration)

- [ ] **A1 — CLI parser, help, errors**
  - Depends on: F0 · Owns: `internal/cli/**`, `tests/cli/parse_test.go`
  - Deliverable: hand-rolled interspersed parser (flags before/after positional), at most one positional workspace (omitted → current directory), `--` boundary pass-through (consumed by the parser, never forwarded as argv), unknown flag error with `agentbox -- --flag` hint, all FR-1 P0 flags, `--help` listing every documented flag, `--version` and the `version` subcommand, usage exit code 2, one-line error style + remedies (PRD §8.2).
  - Evidence: `go test ./internal/cli/...`; `./agentbox --bogus` exits 2 with hint; `--help` diff against FR-1 table.
  - ACs: 19, 20 (parser half)

- [ ] **A2 — engine package: pure argv builder, dry-run, naming**
  - Depends on: F0 · Owns: `internal/engine/**`, `internal/run/naming.go`, `tests/cli/dryrun_golden_test.go` + golden files
  - Deliverable: engine-agnostic argv builder per §1.2 ordering (docker/podman grammar shared); shell-quoting renderer; secret redaction; engine resolution (`AGENTBOX_ENGINE` override, else auto-detect `docker` then `podman` on PATH; the `--engine` flag lands in D2); engine reachability probe with one-line remedy (FR-3); exit-code mapping (125–127 → 1 + hint for 126/127); container naming `agentbox-<project>-<rand>` + `AGENTBOX_TEST_NAME` override; label set incl. `agentbox.owner=<pid>@<boot-id>`.
  - Evidence: golden test with fixed `AGENTBOX_TEST_NAME`, non-TTY input → stable output; unit test proves `-e KEY` bare form (no value in argv).
  - ACs: 15, 21, part of 9 (labels/owner), 12 (mapping only — real run in A5)

- [ ] **A3 — workspace package: canonicalize, guardrail, notices**
  - Depends on: F0 · Owns: `internal/workspace/**`
  - Deliverable: canonical abs path (symlinks resolved; mount uses canonical path only); reject non-existent/non-dir with remedy; guardrail list per FR-2 (`/`, `$HOME`, ancestors, `~/.ssh`, `~/.aws`, `~/.config`, `~/.gnupg`, `~/.docker`, `/etc`, `/var/run`, `/var/lib/docker`) with `--force` override; bounded notice scan (workspace root + `.git/hooks/`, non-recursive) for credential files and host-execution vectors; SELinux state detection → `:z` relabel option.
  - Evidence: unit tests per guardrail path; notice output for a fixture workspace.
  - ACs: 8 (package half), FR-2 notices

- [ ] **A4 — base + pi images and entrypoint**
  - Depends on: F0 · Owns: `images/**`, `internal/entrypoint/**` (F0's `image` target builds both Dockerfiles by path; if a target gap appears, report it to A5 — never edit `Makefile`)
  - Deliverable: `images/base/Dockerfile` (node:24-bookworm-slim pinned by digest + bash ca-certificates git ripgrep util-linux) and `images/pi/Dockerfile` (pinned `@earendil-works/pi-coding-agent`, `npm ci --ignore-scripts`); `entrypoint.sh` per §1.2 contract with `setpriv`; `make image` builds both; ≤ 400 MB.
  - Evidence: `docker images` sizes; container smoke: entrypoint drops to numeric UID, `id -u` = host uid, file created in `/workspace` host-owned.
  - ACs: 5, 6 (entrypoint half), 16 (build hygiene)

- [ ] **A5 — integration: `run` orchestration, M0 exit**
  - Depends on: A1–A4 · Owns: `internal/run/run.go` (+`signal.go`, `cleanup.go`), `cmd/agentbox/main.go`, `tests/isolation/00-boundary.sh`
  - Deliverable: wire CLI → workspace → adapter/engine → argv → exec; `--rm --init` + conditional `-it`; TTY/non-TTY signal handling per FR-1; force-remove only own `agentbox.run` container; exit propagation; silence on success, exit code only on nonzero agent exit (FR-11); dry-run path; `--agent pi` and `--agent shell`; `--ephemeral` accepted (volume wiring in C4).
  - Evidence: UC1 end-to-end (`cd fixture && agentbox --agent shell -- -c pwd`); AC 2–4 via `tests/isolation/00-boundary.sh` (no host home contents, no `~/.ssh`, no `/var/run/docker.sock`); exit-code test (`exit 7`); no `agentbox-*` containers remain after normal + SIGINT runs.
  - ACs: 1–4, 12, 21 (integration proof)

### Wave 2 — M0.5 correctness (parallel packages + serial integration)

- [ ] **C1 — state package**
  - Depends on: A5 · Owns: `internal/state/**`
  - Deliverable: `VolumeName(uid, canonical) = agentbox-state-<uid>-<hash8>` (`sha256` first 8 hex); `/state` mount spec; ephemeral variant; concurrent-run warning text; P1 stubs (`ls`, `rm`, `gc`) behind clear interface.
  - Evidence: unit tests (same path → same name across runs; different path → different name); AC 10/11 assertions stand up once C4 wires it.
  - ACs: 10, 11 (package half)

- [ ] **C2 — adapter package + env allowlist assembly**
  - Depends on: A5 · Owns: `internal/adapter/**`, `tests/cli/env_test.go`
  - Deliverable: `pi`, `shell` descriptors (`pi` sets `EphemeralArgs: ["--no-session"]`), plus `--agent` name validation whose error lists the valid built-ins; per-adapter `CredEnv` allowlist per FR-6 (keys, proxies, CA vars incl. lowercase); `BuildEnv(hostEnv)` — never inherits host wholesale, skips unset `--env KEY` with notice, path-valued vars warn; bare `-e KEY` form; `--credentials` target map (implementation P1).
  - Evidence: unit tests with a poisoned host env (`AWS_SECRET_ACCESS_KEY` must not appear); dry-run redaction re-checked.
  - ACs: 13, 14, 15 (allowlist half)

- [ ] **C3 — entrypoint hardening & caps finalization**
  - Depends on: A4 · Owns: `internal/entrypoint/entrypoint.sh`, `images/*/Dockerfile`
  - Deliverable: capability rationale honored — entrypoint only needs chown/setuid/setgid; skips chown when EUID≠0; `--ro` warning surface via `AGENTBOX_READONLY`; image build stays deterministic (`--ignore-scripts`, no secrets in layers/history).
  - Evidence: `docker history` shows no secret ENV/ARG/label; filesystem scan clean; already-non-root path exercised by explicit `--user`.
  - ACs: 5, 6, 16

- [ ] **C4 — integration: correctness wiring + isolation tests**
  - Depends on: C1–C3 · Owns: `internal/run/run.go`, `tests/isolation/**` (first drop)
  - Deliverable: mount state volume (or ephemeral, incl. adapter `EphemeralArgs` such as pi `--no-session`), hardening defaults from the Wave-0 fields (`--cap-drop ALL --cap-add CHOWN,SETUID,SETGID`, `no-new-privileges`, tmpfs `/tmp:size=256m`, `--init`), limits defaults, env assembly, uid/gid env, SELinux relabel, guardrail refusal + notices + `--force` echo, banner line; startup sweep of stale same-uid containers (owner pid+boot-id dead), SIGKILL-safe; isolation test scripts for AC 5, 6, 8, 9, 10, 11, 13, 14 (pi half), 17; NFR-3 zero-write manifest check (workspace, state volume, `$HOME` top level, guardrail paths; engine-owned exclusions documented).
  - Evidence: `make test-isolation` green on root Docker; AC 8 exit transcript from an ancestor of `$HOME`; two-workspace AC 10 proof; SIGKILL then next-startup sweep proof; NFR-3 manifest shows no unexpected writes.
  - ACs: 5, 6, 8, 9, 10, 11, 13, 14 (pi half — claude half in D1), 17, NFR-3

### Wave 3 — M1 release (parallel lanes + serial integration)

- [ ] **D1 — `claude` + `opencode` adapters, `agentbox agents`**
  - Depends on: C4 · Owns: `internal/adapter/**`, `images/claude/**`, `images/opencode/**`, `internal/cli/agents.go`
  - Deliverable: descriptors per FR-4 table (verify opencode env vars empirically against the image — see open decisions); images; `agents` listing image/version/credential env names; adapter args composition `[defaults] [-- user args]`.
  - Evidence: `agentbox agents` output; dry-run argv for both adapters; live smoke for each (cheap ping prompt or `--version`).
  - ACs: 14 (claude half), FR-4 (claude/opencode)

- [ ] **D2 — flag surface completion**
  - Depends on: C4 · Owns: `internal/cli/**` (excluding `internal/cli/agents.go`), `internal/engine/spec.go` + argv builder
  - Deliverable: `--ro`, `--net bridge|none|host` (+ host warning), `--engine docker|podman` (uses A2 engine resolution), `--cpus`, `--memory`, `--pids-limit`, `-q/--quiet`, `--no-tty`, `--env-file` (paths resolve relative to invocation CWD), `--credentials` parsing (mount wiring in E2a), banner contents per FR-11; engine-mode ownership: append `--userns=keep-id` for Podman runs (`RunSpec.Userns`, FR-9 P2) with rootful/rootless semantics verified empirically.
  - Evidence: goldens updated for each flag; `--ro` dry-run shows `:ro` and a live container write to `/workspace` is denied (AC 7 write half); `--net none` container fails DNS (AC 18); `--engine podman` dry-run uses the podman grammar and shows `--userns=keep-id`.
  - ACs: 7, 18, part of 19

- [ ] **D3 — CI + full acceptance suite**
  - Depends on: C4 (capability), D1/D2 (coverage) · Owns: `.github/workflows/ci.yml`, `tests/isolation/**`
  - Deliverable: CI job: `go vet`, unit tests, image build, isolation suite on Docker; podman smoke job (documented expectations); all AC 1–21 mapped to a named test or script; `make test` = unit, `make test-isolation` = containers.
  - Evidence: CI run URL/locally reproduced `make test && make test-isolation` output.
  - ACs: all 1–21 (suite), NFR-6

- [ ] **D4 — docs set**
  - Depends on: D2 (flags stable), D1 (adapters) · Owns: `docs/**`, `README.md`
  - Deliverable: README 5-minute quickstart; `docs/isolation.md` (contract §5 verbatim + limits); `docs/harnesses.md` (adapter contract + adding one + harness prerequisites: host `.gitconfig` not mounted → `GIT_AUTHOR_*`/`GIT_COMMITTER_*`, linked worktrees/absolute-path submodules unsupported, per-harness state-content map so `state rm` cost is clear); `docs/credentials.md`; `docs/troubleshooting.md`. No claim stronger than §5.
  - Evidence: fresh-context review agent checks each doc against PRD §5/§6 for overclaiming.
  - ACs: NFR-7, G6

- [ ] **D5 — M1 integration & release gate**
  - Depends on: D1–D4 · Owns: `internal/run/run.go`, `internal/cli/**` (final dispatch wiring, integration only), `Makefile` release targets, version injection (`ldflags`)
  - Deliverable: final wiring, banner/provenance hook, `make release-check` (secret scan + image history check + size ≤ 15 MB / ≤ 400 MB), tagged `v0.1.0` flow. Run full AC sweep and record evidence table.
  - Evidence: AC 1–21 evidence table in the PR body.
  - Exit: gate G3.

### Wave 4 — M2 v1.0 (parallel lanes + serial integration)

- [ ] **E1 — config file**
  - Depends on: D5 · Owns: `internal/config/**`, `internal/cli/**` (excluding `auth.go`, `state.go`, `doctor.go`)
  - Deliverable: TOML discovery + precedence CLI > project > user; project config restricted key set; hard error for boundary-relevant keys and `quiet` and `agent="image:<ref>"` (FR-12); provenance line always printed; parse errors never echo file contents; project path canonicalization + symlink-escape rejection.
  - Evidence: unit tests per rejected key; AC 22; provenance line visible under `--quiet`.
  - ACs: 22

- [ ] **E2a — run path: `image:<ref>`, `--user`, `--state-dir`, credential mounts**
  - Depends on: D5 · Owns: `internal/adapter/**`, `internal/run/run.go`, `tests/cli/imageref_test.go`, `tests/isolation/**` (run-path additions)
  - Deliverable: `image:<ref>` with engine `--user host-uid:gid`, requires `--ephemeral` or writable `--state-dir`, fails early with remedy (FR-9); conflict validation with one-line remedy: `--ephemeral` + `--state-dir`, `image:<ref>` + `--image`; `--user` skips privilege drop, fails up front when the fresh per-project volume is unwritable (remedy: pre-owned `--state-dir` or `--ephemeral`), warns for `--user 0`; `--state-dir` bind mount + chown caveat; `--credentials` mount wiring per FR-6 (parsing landed in D2). Consumes the C1 state interface; helper changes go to E2b (one writer per file).
  - Evidence: dry-run for each; `--user` fresh-volume failure transcript; `--credentials` dry-run shows the `:ro` target and no value; live ownership check for `image:<ref>` under root Docker.
  - ACs: FR-4 P1, FR-5 P1, FR-6 P1, FR-9 P1

- [ ] **E2b — subcommands: auth, state, doctor (incl. state helpers)**
  - Depends on: D5 · Owns: `internal/state/**`, `internal/auth/**`, `internal/doctor/**`, `internal/cli/auth.go`, `internal/cli/state.go`, `internal/cli/doctor.go`, `tests/cli/subcommand_test.go`
  - Deliverable: `agentbox auth` (detection only, no values); `agentbox state ls|rm|gc` (impl of the C1 ls/rm/gc interface); `agentbox doctor` (engine, image digest, update check with `--no-check`, security posture including rootless/rootful/user-ns mode per FR-9 P2); per-harness credential preflight notice when no mechanism is detected (env/credential file/state; §8.2 wording, never prints values). E5 wires dispatch.
  - Evidence: state ls/rm/gc against fixture volumes; auth output shows mechanism, never values; doctor transcript on root Docker.
  - ACs: FR-5 P1, FR-6 P1, FR-11 P1

- [ ] **E3 — image publishing**
  - Depends on: D5 · Owns: `.github/workflows/release.yml`, `images/**`
  - Deliverable: multi-arch (`linux/amd64`, `linux/arm64`) builds pushed to `ghcr.io/<org>/agentbox-<harness>`; immutable tags; pull path in engine (one-line "pulling…" first time, silent after); release manifest with base digest + harness versions (NFR-8).
  - Evidence: manifest file in release; pull smoke on both arches (or documented single-arch CI).
  - ACs: NFR-8, FR-10 P1

- [ ] **E4 — macOS verification**
  - Depends on: D5 · Owns: `docs/troubleshooting.md`, `docs/isolation.md` (ownership section)
  - Deliverable: Docker Desktop run-through on macOS: workspace ownership behavior (virtiofs), `--ro`, TTY; documented verdict per PRD open question 4; CI matrix note; NFR-5 platform wording (Linux primary, macOS verdict, WSL2 ext4 caveat vs `/mnt/c` DrvFs, Windows-native best-effort).
  - Evidence: transcript of checks with engine-mode matrix recorded.

- [ ] **E5 — M2 integration gate**
  - Depends on: E1, E2a, E2b, E3, E4 · Owns: `internal/run/run.go` (final integration only; E2a landed the run-path features), `internal/cli/**` (dispatch wiring), docs cross-refs (content owned by E4)
  - Deliverable: wire config provenance, subcommand dispatch, and cross-lane integration of the E2a paths; NFR-1 startup budget measured; NFR-2 size check; NFR-4 audit note (engine/pull/labeled update check only, `AGENTBOX_DEBUG=1` visibility); second-engine (Podman) smoke via `--engine podman`, asserting workspace ownership under rootless (`--userns=keep-id`, FR-9 P2, AC 5 per engine mode).
  - Evidence: NFR checklist + AC 22 + full AC 1–21 re-run.

### Wave 5 — M3 backlog (not scheduled; candidates)

`--net restricted` egress proxy sidecar (FR-7 P2) · user-defined adapters (FR-4 P2) · `--build` (FR-4/10 P2) · `--name`/`--keep` (FR-3 P2) · `--workdir-path` (FR-2 P2) · `--image-digest` in `--dry-run` (FR-10 P2) · config profiles (FR-12 P2) · in-container `/login` persistence (FR-6 P2) · same-path mount option · blocking guard for host-execution vectors · `state export|import` · WSL2/Windows polish · rootless Docker posture in `doctor` (FR-9 P2).

---

## 3. Gates (fresh-context review + evidence)

| Gate | After | Verifies | Evidence required |
| --- | --- | --- | --- |
| G0 | F0 | Contracts match PRD §6; repo builds | build/vet output; contract review notes |
| G1 | A5 | AC 1–4, 12, 19–21 | `make test`, UC1 transcript, `tests/isolation/00-boundary.sh` output (AC 2–4), container list empty |
| G2 | C4 | AC 5–6, 8–11, 13–14 (pi half), 17, NFR-3 | `make test-isolation` output; AC 8 exit transcript; NFR-3 manifest diff; dry-run golden diff |
| G3 | D5 | AC 1–21 + NFR-6 + docs (NFR-7) | CI green; AC evidence table; doc overclaim review |
| G4 | E5 | AC 22 + NFR 1/2/4/5/8 + Podman smoke | NFR measurements; engine-mode matrix docs; release manifest; AC 22 test |

Gate procedure: launch one read-only reviewer agent (fresh context) with the AC list + repo state; it must run the tests itself where possible. Findings become fix tasks executed by the parent/integrator, then re-verify. Never pass a gate on the author's summary alone.

---

## 4. Orchestration recipe

- One wave per parent turn: children run in a single `subagent` workflow call; integration task is a separate awaited run that owns shared files; then the gate review runs after it.
- Parallel writers get isolated worktrees when they touch the same repo concurrently; integration happens in the parent cwd. Because Wave 0 owns shared files and lanes own disjoint directories, worktrees are a safety net, not a requirement — but merge the integration lane last.
- Keep the prompt for each child to: this task card verbatim + `PRD.md` + "read the frozen contracts in `internal/engine/spec.go` and `internal/adapter/adapter.go`".
- Test-only lanes (isolation suite) can start against frozen contracts and be verified after the integration lane lands; mark them dependency-verified, not author-verified.

---

## 5. Conflict map (who may edit what)

| File / dir | Wave 1 | Wave 2 | Wave 3 | Wave 4 |
| --- | --- | --- | --- | --- |
| `go.mod`, `Makefile` | A5 (F0 laid them down in Wave 0; A4 reports image-target gaps to A5, never edits) | C4 | D5 | E5 |
| `internal/run/run.go` | A5 only | C4 only | D5 only | E2a, then E5 (final integration; E5 depends on E2a) |
| `internal/cli/**` | A1 | — | D2 (excl. `agents.go`), D1 (`agents.go`), D5 (dispatch wiring) | E1 (excl. `auth.go`/`state.go`/`doctor.go`), E2b (those three), E5 (dispatch wiring) |
| `internal/engine/spec.go`, argv builder | A2 | C4 (defaults) | D2 | E5 |
| `internal/adapter/**` | — | C2 | D1 | E2a |
| `internal/entrypoint/**`, `images/**` | A4 | C3 | D1 | E3 |
| `internal/workspace/**` | A3 | — | — | — |
| `internal/state/**` | — | C1 | — | E2b |
| `tests/cli/**` | A1/A2 | C2 | D2 | E1 (`config_test.go`), E2a (`imageref_test.go`), E2b (`subcommand_test.go`) |
| `tests/isolation/**` | A5 (`00-boundary.sh` only) | C4 | D3 | E2a (run-path additions), E5 (integration) |
| `.github/workflows/**` | — | — | D3 | E3 |
| `docs/**`, `README.md` | — | — | D4 | E4 (E5 cross-refs) |

If a task must touch a file outside its column, it stops and reports; the integrator folds the change into the owning task.

---

## 6. Conventions

- **Exit codes:** agent code passthrough (128+signal); agentbox failures 1 (engine/IO), 2 (usage); engine 125–127 → 1 with image/entrypoint hint for 126/127.
- **Errors:** one line + one remedy, no raw engine dumps; `AGENTBOX_DEBUG=1` prints raw command + engine stderr.
- **Tests:** `AGENTBOX_TEST_NAME` pins name/uuid/owner for determinism; golden changes require an explicit rationale in the task report; isolation tests skip with a clear message when no engine is present, and CI must fail if they were skipped unexpectedly.
- **Secrets:** fixtures only, assert `<set>`/`<unset>` redaction; `make release-check` scans images and history (AC 16).
- **Commits:** conventional prefixes (`feat|fix|test|docs|chore`); one task = one focused commit series; no mixed-wave commits.
- **Go:** stdlib-first; external deps only TOML (Wave 4) and nothing else without integrator approval (NFR-2).

---

## 7. Open decisions (owner input needed at the marked task)

| # | Decision | Needed by | Default if owner silent |
| --- | --- | --- | --- |
| 1 | Exact opencode config env vars (`XDG_CONFIG_HOME` vs `OPENCODE_CONFIG_DIR`) | D1 | Verify empirically in container; document what works |
| 2 | GHCR org/repo namespace for published images | E3 | `ghcr.io/<github-owner>/agentbox-<harness>` |
| 3 | macOS status wording: supported vs best-effort | E4 | "supported with ownership caveats" per Docker Desktop findings |
| 4 | Isolation tests: bash scripts vs Go-driven | C4/D3 | bash scripts via `make test-isolation`, invoked from CI |
| 5 | Persistence default (PRD Q3): state-on vs ephemeral-on | C4 | state-on per project (PRD proposal) |
| 6 | GitHub Actions runner engine sourcing (preinstalled Docker vs setup action) | D3 | preinstalled Docker, podman smoke as separate non-blocking job until G4 |

---

## 8. Milestone exit criteria (from PRD §10)

| Milestone | Wave | Exit |
| --- | --- | --- |
| M0 walking skeleton | Wave 1 | UC1 e2e; AC 1–4, 12, 19–21 |
| M0.5 correctness | Wave 2 | AC 5–6, 8–11, 13–14, 17; NFR-3 |
| M1 v0.1 | Wave 3 | AC 1–21; README quickstart; tagged release |
| M2 v1.0 | Wave 4 | AC 22; docs complete; NFR-1/2/8; Podman smoke |
| M3 hardening+ | Wave 5 | Threat-model update; egress test in CI |

Plan exits are supersets of PRD §10 where work lands early: M0.5 also proves AC 8 (guardrail e2e), AC 17 (hardening defaults) and NFR-3 (isolation suite); the PRD M0.5 exit list remains a subset.
