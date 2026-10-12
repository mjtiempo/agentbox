# PRD: agentbox — Containerized Agent Coding Environment

- **Status:** Draft v0.2 (post-review revision; see §13.3)
- **Owner:** Mark (maintainer)
- **Type:** New tool / developer productivity
- **Target platforms:** Linux (primary), macOS (secondary, Docker Desktop), WSL2
- **Runtime dependency:** Docker 24+ (Podman planned); no host-side language runtime required

---

## 1. Summary

`agentbox` is a small CLI that runs an AI coding agent inside an ephemeral container whose **only** host-visible, writable surface is the project workspace the user names (or the current directory).

```bash
cd ~/src/my-app && agentbox            # mounts ~/src/my-app as /workspace, starts pi
agentbox ~/src/other --agent claude    # mounts that dir, starts Claude Code
agentbox -- --print "explain this repo"
```

The agent gets a working directory, a state volume for its own sessions/config, a TTY, and network egress to reach model APIs. It does **not** get the host home directory, the Docker socket, SSH keys, cloud credential files, other projects, or the ability to write outside its workspace. The container is deleted on exit; only the project's own state volume and the workspace survive.

Positioning: `agentbox` is the boring, 30-second path to "let the agent touch this one folder and nothing else" — not an orchestration platform, not a sandbox VM, not a devcontainer replacement.

---

## 2. Problem & Motivation

Coding agents run shell commands, edit files, and install packages with the full permissions of the user account that launched them. On a typical developer machine that means read access to `~/.ssh`, `~/.aws`, browser profiles, every other repository, and write access to the entire home directory. Prompt injection via repo content, dependency README, or command output turns that into a realistic exfiltration or destruction path.

Today the options are:

1. **Trust the agent, review diffs.** No isolation. One `rm -rf ~` or `curl secret | nc` away from a bad day.
2. **Hand-rolled `docker run`.** Requires remembering mount flags, UID mapping, env allowlists, volume layout, TTY flags, and cleanup. Everyone gets it subtly wrong; nobody repeats it consistently.
3. **Full sandbox platforms** (Docker Sandboxes, OpenShell, micro-VMs, Gondolin). Strong, but heavyweight to install/configure and overkill for "run pi on this repo".
4. **Devcontainers.** Solve environment reproducibility, not agent containment; typically mount the host workspace *and* more.

Gap: a **one-command, opinionated, correctly-defaulted container boundary** that works with the agent harness you already have and makes "workspace-only access" the default rather than an exercise in flag plumbing.

---

## 3. Goals

### 3.1 Goals (v1.0)

- **G1 — One command.** `agentbox` with no arguments runs `pi` interactively in a container with `$PWD` mounted, in under 5 seconds after first image pull.
- **G2 — Workspace-only host access.** The container sees exactly one host path by default: the workspace, at `/workspace`. No home dir, no socket, no other project.
- **G3 — Ephemeral container, persistent agent state.** Container is removed on exit; sessions, settings, and model caches survive in a dedicated per-project state volume (`agentbox-state-<uid>-<hash8>`, FR-5).
- **G4 — Harness choice.** Default harness is `pi`; built-in adapters for `claude` (Claude Code), `opencode`, and a raw `shell`. Arbitrary images via `--image`/`--agent image:<ref>`.
- **G5 — Correct UID/GID.** Files the agent creates in the workspace are owned by the host user, never root (including via `image:<ref>`, which runs under the engine `--user`; see FR-9).
- **G6 — Honest, documented boundary.** `docs/isolation.md` states precisely what is and is not contained (filesystem, network, credentials, kernel).
- **G7 — Zero host dependencies.** Single static binary (Go) + a container engine. No Node/Python on the host.

### 3.2 Non-Goals (explicitly out of scope)

- Not a VM or a kernel-level escape defense; container-to-host kernel exploits are out of scope (documented).
- Not a multi-agent orchestrator, scheduler, or fleet manager.
- Not a devcontainer/dev environment builder; project-specific toolchains come via `--image` (M3 may add a compose/devcontainer escape hatch).
- Not a remote/cloud sandbox service.
- Not a secret manager. Credential *scoping* is in scope; vault/KMS integration is not.
- Not a replacement for CI sandboxing.
- No host execution path *for agentbox itself*. `agentbox` never executes agent code on the host and never mounts the container-engine socket into the container. Artifacts the agent writes into the workspace can still be executed later by host tooling (git hooks, package lifecycle scripts, editor tasks); that boundary is documented in §5.2.

---

## 4. Personas & Key Use Cases

**P1 — Individual developer (primary).** Has pi/Claude Code/opencode installed, works across many repos, occasionally runs agents on unfamiliar or untrusted code. Wants: `cd repo && agentbox`.

**P2 — Security-conscious team lead.** Wants a repeatable, auditable way for the team to run agents with a known boundary and documented defaults. Wants `--dry-run` and a checked-in config file.

**P3 — Agent tinkerer.** Wants to try a new harness or a custom image without touching host config. Wants `--agent image:...`, `--ephemeral`, `--net none`.

#### Use cases

- UC1: Run pi on a client repo with unknown history and zero risk to `~/.ssh`.
- UC2: Run two agents on two repos simultaneously; state and sessions must not bleed between them.
- UC3: Run an offline agent with `--net none`, guaranteeing no egress (the model must run inside the container; a host-local model needs explicit networking — see FR-7).
- UC4: Hand a repo to an agent and read-only the workspace (`--ro`) during exploration.
- UC5: Inspect exactly what the container would run before running it (`--dry-run`).
- UC6: Try Claude Code on a repo when the host is configured for pi, without reconfiguring the host.

---

## 5. Threat Model & Isolation Contract

### 5.1 What the boundary guarantees (P0)

For a process running inside the `agentbox` container:

| Resource | Default access |
| --- | --- |
| Workspace directory (host) | Read-write at `/workspace` (read-only with `--ro`) |
| Host home directory, other repos, `~/.ssh`, `~/.aws`, browser data | **Not visible.** No path exists in the container |
| Docker/container-engine socket (`/var/run/docker.sock`) | **Not mounted**, host socket not visible |
| Host processes, host PIDs, host network services on `localhost` | Not visible; container has its own PID/net namespace |
| Agent config/session data | The project's own state volume only (`agentbox-state-<uid>-<hash8>`); other projects' volumes are never mounted; host `~/.pi/agent` etc. **never** mounted by default |
| Environment variables | Only an explicit allowlist (see FR-6); host env is **not** inherited wholesale |
| Network | Full egress by default (needed for model APIs); `--net none` for offline work. Bridge mode isolates the host's loopback but not the host itself: services bound to non-loopback addresses (including the Docker gateway, e.g. `172.17.0.1`) are reachable |
| Container root filesystem | Writable, ephemeral, destroyed with the container |
| `/tmp` | tmpfs, destroyed with the container |

### 5.2 What the boundary does not protect against

- **Network egress.** With default networking, anything readable in the container can be sent out. `--net none` is the mitigation; egress allowlisting is M3.
- **Prompt injection.** Repo content, dependencies, and tool output can steer the model. Isolation limits *blast radius*, not the decision to run a command.
- **Credentials you hand in.** A passed API key is usable by anything in the container.
- **Kernel/container-runtime escapes.** A Docker/runC/kernel 0-day is out of scope. Host Docker daemon remains a root-equivalent trust anchor.
- **Resource exhaustion** beyond the configured `--cpus/--memory/--pids-limit` defaults. Disk usage is not quotaed: a runaway agent can fill the host filesystem through the workspace (and to a lesser degree the state volume).
- **Workspace damage.** The workspace is read-write by default. `--ro` exists for that reason; git is the recovery mechanism.
- **Host-side execution of workspace artifacts.** The container can write files the *host* later executes: `.git/hooks/*`, dangerous `.git/config` keys (`core.fsmonitor`, `filter.*`), package lifecycle scripts, `.envrc`, editor tasks, Makefiles, build backends. `agentbox` itself has no host execution path, but the workspace is a persistent bridge back to the host. Treat agent-written workspace files as untrusted before running host tooling; FR-2 prints a notice when common vectors are present.

The contract is: *a generated command can only damage or read the workspace, the agent state volume, and its own ephemeral container, or leak data over the network.* This sentence is the product.

---

## 6. Functional Requirements

Priorities: **P0** = required for v0.1 (M0–M1), **P1** = required for v1.0 (M2), **P2** = later (M3+).

### FR-1 — CLI surface

```text
agentbox [options] [workspace] [-- <agent args...>]
agentbox agents
agentbox auth <harness>
agentbox doctor
agentbox state ls | rm <project> | gc
agentbox version
```

| Flag | Default | Description | Pri |
| --- | --- | --- | --- |
| `[workspace]` | `$PWD` | Directory mounted as `/workspace`. Must exist and be a directory. | P0 |
| `--agent, -A <name>` | `pi` | Harness adapter: `pi`, `shell`, `claude`, `opencode` (P0); `image:<ref>` (P1) | P0 |
| `--image <ref>` | adapter default | Override container image | P0 |
| `--dry-run` | off | Print the exact engine command and exit | P0 |
| `--quiet, -q` | off | Suppress the startup banner and notices (the config-provenance line and `--force` echo are always shown) | P0 |
| `--ro` | off | Mount workspace read-only | P0 |
| `--force` | off | Override the workspace guardrail (FR-2); echoed in the banner and `--dry-run` | P0 |
| `--ephemeral` | off | No state volume; sessions/config discarded on exit | P0 |
| `--state-dir <path>` | — | Bind-mount host dir as `/state` instead of named volume | P1 |
| `--env, -e <KEY>` | — | Pass host env var `KEY` into the container (repeatable) | P0 |
| `--env-file <path>` | — | Pass env vars from a file (repeatable) | P1 |
| `--credentials <host-file>` | — | Mount a host credential file read-only at the adapter's expected target (repeatable; FR-6) | P1 |
| `--net <bridge\|none\|host>` | `bridge` | Network mode | P0 |
| `--cpus <n>` | `2` | CPU limit | P0 |
| `--memory <size>` | `4g` | Memory limit | P0 |
| `--pids-limit <n>` | `512` | PID limit | P0 |
| `--user <uid:gid>` | — (agent drops to host uid:gid) | Override the agent UID/GID; skips the entrypoint privilege drop (FR-9) | P1 |
| `--engine <docker\|podman>` | auto-detect | Container engine | P2 |
| `--name <name>` | generated | Container name (useful with `--keep`) | P2 |
| `--keep` | off | Do not remove the container on exit | P2 |
| `--workdir-path <path>` | `/workspace` | Mount point inside the container | P2 |
| `--config <path>` | auto | Config file (see FR-12) | P1 |
| `--build` | off | Build the adapter image locally from the vendored Dockerfile instead of pulling | P2 |
| `--no-check` | off | Skip `doctor`'s update check (NFR-4) | P1 |
| `--no-tty` | off | Force non-TTY mode (omit `-it`) | P1 |
| `--version`, `--help` | — | Standard | P0 |
| `--` | — | Everything after is forwarded verbatim to the agent binary | P0 |

Rules:

- Flags may appear before or after the positional workspace (interspersed parsing); everything after `--` is forwarded verbatim to the agent binary. Unknown flags before `--` are an **error** with a hint to use `--`.
- Positional arguments: at most one (the workspace). Not "is it a dir or a prompt" magic.
- `--env KEY` with `KEY` unset on the host is skipped with a one-line notice, never passed empty. `--env-file` paths resolve relative to the invocation CWD.
- `--ephemeral` with `--state-dir` is an error. `--agent image:<ref>` together with a conflicting `--image` is an error.
- Exit code = agent process exit code (128+signal when the agent dies from a signal). agentbox's own failures exit 1 (engine/IO) or 2 (usage); engine-level 125–127 failures are mapped to 1 after the engine error is printed, with 126/127 adding an image/entrypoint-not-found hint (relevant to `image:<ref>`).
- Signal handling depends on TTY mode. With a TTY (`-it`), Ctrl-C is an input byte forwarded by the engine to the agent's terminal and agentbox never sees it; the agent's own line discipline decides how to react, and cleanup runs after the agent exits. Without a TTY, agentbox forwards SIGINT/SIGTERM to the agent through the engine and waits for the agent to exit; a second SIGINT while the agent is still running force-removes the container and exits 130 (`--keep` leaves the container in place). SIGTERM/SIGHUP behave like the second SIGINT after a short grace period, also honoring `--keep`.
- `agentbox` with no TTY input available (`stdin` not a TTY) still runs; no interactive prompt required for defaults. Interactive TUIs require `-it`, which is passed when stdin is a TTY.

### FR-2 — Workspace mounting

- P0: Default workspace is `$PWD`, resolved to a canonical absolute path.
- P0: Mounted RW at `/workspace`; the container `WORKDIR`/cwd is `/workspace`.
- P0: Paths are canonicalized (symlinks resolved) before mounting; a workspace whose canonical path is not an existing directory is rejected with an actionable error. The mount is always the canonical path, so a symlink cannot widen it.
- P0: Guardrail — refuse to run when the workspace is `/`, `$HOME`, an ancestor of `$HOME`, or a known-sensitive path (`~/.ssh`, `~/.aws`, `~/.config`, `~/.gnupg`, `~/.docker`, `/etc`, `/var/run`, `/var/lib/docker`), unless `--force`. Rationale: the product promise is "only this folder"; mounting any of these voids it. The error explains the reason and the override, and the override is echoed in the startup banner and `--dry-run`.
- P0: `--ro` mounts `:ro` and sets an env marker (`AGENTBOX_READONLY=1`) so the entrypoint can surface a warning in the agent's context banner. (No enforcement inside the container beyond the mount flag.)
- P0: SELinux-enforcing hosts (Fedora/RHEL): agentbox detects the engine's SELinux state and applies a relabel option (`:z`) to workspace/state mounts, or documents the `--security-opt label=disable` override, so the mount is usable without manual flags.
- P0: Notice — once the workspace is known, if it contains common credential files (`.env`, `.netrc`, `credentials*.json`) or host-execution vectors (non-sample `.git/hooks/*`, `package.json` lifecycle scripts, `.envrc`), print a one-line notice that the agent can read these and that agent writes to them execute on the host later (§5.2). Notice only; never blocks; suppressible with `--quiet` from the CLI or user config only — project config cannot set `quiet` (FR-12). The scan is bounded to the workspace root plus `.git/hooks/`, never recursive, to stay inside NFR-1.
- P2: `--workdir-path` for tools that hardcode paths; default stays `/workspace` for predictability.

### FR-3 — Container lifecycle

- P0: `docker run --rm --init -it` semantics by default (TTY flags conditional on host TTY).
- P0: Container name generated as `agentbox-<project>-<rand>`; never reused implicitly; the random suffix, run uuid, and owner label are overridable via `AGENTBOX_TEST_NAME` for golden tests. Every container carries `--label agentbox=1 --label agentbox.run=<uuid> --label agentbox.project=<id> --label agentbox.uid=<host uid> --label agentbox.owner=<pid>@<boot-id>`; `run` identifies exactly one invocation and `owner` lets a later startup distinguish a live run from a stale one. Containers started with `--keep` also carry `--label agentbox.keep=1`, which exempts them from the startup sweep.
- P0: On normal exit, SIGINT/SIGTERM, or nonzero agent exit: container is stopped and removed (unless `--keep`). Verified by acceptance test (no `agentbox-*` containers remain).
- P0: In TTY mode the container is removed after the agent exits (unless `--keep`); in non-TTY mode agentbox forwards SIGINT and waits (FR-1). agentbox never kills a live agent to satisfy cleanup.
- P0: Cleanup does not rely on `--rm` alone. agentbox force-removes only its own container, selected by `agentbox.run=<uuid>` — never the shared `agentbox=1` label — if the agent does not exit within the grace period (`--keep` disables this force-remove). At startup, agentbox reaps containers labeled `agentbox.uid=<self>` whose `agentbox.owner` pid+boot-id is no longer alive (`agentbox.keep=1` containers are skipped); this covers SIGKILL and terminal close without ever touching a concurrent run (UC2) or another user's containers.
- P2: `--keep` for debugging: the container is not auto-removed on exit (the startup sweep and the signal force-remove both leave it in place). `--name` only names the container; it does not imply `--keep` — pair them (`--name box1 --keep`) to keep a named container for inspection.
- P0: If the engine is unreachable, fail with a one-line remedy (`is the docker daemon running?`), not a raw daemon stack trace.

### FR-4 — Agent harness adapters

An adapter is a built-in descriptor: image, entrypoint/command, config-dir env var and container path, credential env allowlist, optional credential file mount target, default args.

| Adapter | Image (default tag) | Config → container path | Notes |
| --- | --- | --- | --- |
| `pi` (default) | `agentbox-pi:<ver>` | `PI_CODING_AGENT_DIR=/state/pi` | npm-pinned `@earendil-works/pi-coding-agent`; sessions live under the same dir, so the per-project volume (FR-5) isolates them |
| `claude` | `agentbox-claude:<ver>` | `CLAUDE_CONFIG_DIR=/state/claude` | npm-pinned `@anthropic-ai/claude-code` |
| `opencode` | `agentbox-opencode:<ver>` | `XDG_CONFIG_HOME=/state/opencode/config`, `XDG_DATA_HOME=/state/opencode/data` | verify exact vars at implementation time |
| `shell` | `agentbox-base:<ver>` | n/a | Raw `bash`; useful as an isolation test and for non-agent use |
| `image:<ref>` | user-provided | user-provided via `--env` | Runs the image's own entrypoint under engine `--user <host uid:gid>`; requires `--ephemeral` or a `--state-dir` writable by that uid (FR-9) |

Requirements:

- P0: `pi`, `shell`, `claude`, `opencode` adapters; `--agent` validation with a list of valid names in the error.
- P0: Adapter args compose as: `image entrypoint [adapter default args] [-- user args...]`. User args always come last and can override (`pi --model ...`).
- P0: Adapter state paths point inside `/state` only. Because each project mounts its own `/state` volume, no adapter can read another project's sessions, settings, or credentials.
- P1: `image:<ref>` adapter; it runs the image's own entrypoint under engine `--user` and has no state-preparation entrypoint (FR-9).
- P1: `agentbox agents` lists adapters with image, pinned version, and required credential env names.
- P1: Documented harness prerequisites: host `.gitconfig` is not mounted, so git commits inside the container need `GIT_AUTHOR_*`/`GIT_COMMITTER_*` via `--env` (or an in-container `git config` persisted in state); workspace `.git` files that point outside the workspace (linked worktrees, submodules with absolute paths) are documented as unsupported.
- P2: User-defined adapters from `$XDG_CONFIG_HOME/agentbox/harnesses/*.{toml,json}` merged over built-ins; a user adapter shadows a built-in of the same name, and unknown keys are a hard error.
- P2: `--build` builds the adapter image locally from a vendored Dockerfile instead of pulling.

### FR-5 — State & persistence

- P0: One named volume per project: `agentbox-state-<uid>-<hash8>` (hash8 = first 8 hex of `sha256(canonical workspace path)`), mounted at `/state` and created on first use. There is no shared volume: an agent in project A has no path to project B's sessions, settings, or credentials.
- P0: Layout inside the volume: `/state/pi`, `/state/claude`, `/state/opencode`, each owned by the host UID/GID (FR-9). Renaming or moving a workspace starts a fresh volume and orphans the old one (`state gc`).
- P0: State volume is the **only** writable persistence outside the workspace.
- P0: `--ephemeral` skips the volume; `/state` is created in the container filesystem and discarded with the container, and harnesses run with their no-session equivalent where available (pi: `--no-session`).
- P0: Concurrent runs for the same project share the deterministic state volume; agentbox prints a warning that harness session/auth files may race (use `--ephemeral` or distinct `--state-dir` paths for parallel runs).
- P1: `--state-dir <path>` bind-mounts a host directory at `/state` instead of the named volume, for inspection/backup. It bypasses per-project volumes, so isolation between projects then depends on using distinct directories; documented. The entrypoint chowns `/state` to the host UID/GID, which changes ownership of the host directory — stated in the flag docs.
- P1: `agentbox state ls` lists project path, volume name, and size where the engine exposes it; `agentbox state rm <project>` requires confirmation; `agentbox state gc` removes volumes whose workspace path no longer exists and requires explicit confirmation (`--force` in scripts) so temporarily unmounted workspaces are not reaped by accident.
- P1: Documented mapping of what lives in state per harness (sessions, auth, caches, model catalogs) so users know what `state rm` costs.
- P2: `agentbox state export|import <project>`.

### FR-6 — Credentials

- P0: **No implicit credential file mounts.** Host `~/.pi/agent`, `~/.claude`, `~/.config/opencode`, `~/.ssh`, `~/.aws`, `~/.git-credentials`, `.netrc` are never mounted unless the user explicitly points at a path.
- P0: Env-var allowlist per adapter, passed only when set on the host, e.g. `ANTHROPIC_API_KEY`, `ANTHROPIC_OAUTH_TOKEN`, `OPENAI_API_KEY`, `OPENAI_BASE_URL`, `OPENROUTER_API_KEY`, `GEMINI_API_KEY`, `GOOGLE_API_KEY`, plus `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` and their lowercase variants, and `SSL_CERT_FILE`/`NODE_EXTRA_CA_CERTS` for corporate proxies. Values are passed as bare `-e KEY` (value read from agentbox's own environment), never `-e KEY=value`, so secrets do not appear in engine argv or host process listings. Path-valued entries print a one-line warning that a host-specific path may not exist in the adapter image; custom CA files need a custom image or `--credentials`.
- P0: Host env is **not** inherited wholesale. Verification test: `agentbox --agent shell` inside the container must not show arbitrary host vars (e.g. `AWS_SECRET_ACCESS_KEY`).
- P0: Secrets must never end up in image layers or committed config; the release check scans the built image filesystem for known key patterns and asserts no secret appears in `docker history` ENV/ARG/label output (AC 16).
- P0: `--env KEY` for anything outside the allowlist (repeatable). P1: `--env-file <path>`.
- P1: Explicit OAuth credential file support: `--credentials <host-file>` mounted read-only at the adapter's expected target. Documented for pi (`auth.json`) and Claude Code (`~/.claude/.credentials.json`) shapes. This is opt-in and clearly labeled "grants the container this credential". `--credentials` is boundary-relevant and can never be set by project config (FR-12).
- P1: `agentbox auth <harness>` prints which mechanism is detected (env key, credential file, or none) without printing secret values.
- P2: Interactive login *inside* the container persisted to state (`/login` flows work when the state volume persists; per-project by FR-5), with docs on the risk of credential-at-rest in a Docker volume.

### FR-7 — Network

- P0: Default `--net bridge`: full egress. Documented as "the agent can reach the internet, and the host gateway/LAN" — bridge mode isolates the host's loopback only, not services bound to non-loopback addresses (`172.17.0.1`).
- P0: `--net none` removes all networking — no egress and no host access — for offline work; the model must run inside the container. A host-local model requires `--net host` (warns; breaks the network boundary) or bridge networking with the model bound to a reachable interface (`host.docker.internal`). Verified by a test that `getent hosts example.com` / `curl` fails.
- P0: Warn on `--net host` (documented as breaking the network boundary; allowed for local-model/socket use cases).
- P2: `--net restricted --allow <host:port,...>` implemented with an internal Docker network plus a tiny egress-proxy sidecar with an allowlist. Model endpoints (`api.anthropic.com`, provider hosts) are the default entry set.

### FR-8 — Hardening defaults

- P0: `--cap-drop ALL --cap-add CHOWN --cap-add SETUID --cap-add SETGID`, `--security-opt no-new-privileges`, `--init`, tmpfs `/tmp` (`size=256m`). The three added capabilities exist only for the first-run state preparation and privilege drop in FR-9; the kernel clears them when the entrypoint execs the non-root agent.
- P0: No `--privileged`, no device passthrough, and no capabilities beyond the three above.
- P0: Default `--cpus 2`, `--memory 4g`, `--pids-limit 512` (configurable).
- P1: Workspace and state mount propagation left at default; nothing else mounted (no `/var/run/docker.sock`, no `/dev` passthrough beyond Docker defaults).
- P2: Optionally run with a user namespace remap on engines that support it (rootless Docker/Podman); `agentbox doctor` reports engine security posture.

### FR-9 — UID/GID correctness

- P0: The agent always runs as the host UID/GID. For built-in adapters agentbox does **not** pass `--user` by default: the container starts as root so the entrypoint can prepare writable state on first run, then drops to the host identity (`image:<ref>` is the exception; see below).
- P0: Entrypoint: create and chown `/state` and `$HOME`, export `HOME=/state/home` (path overridable per adapter), then `exec setpriv --reuid "$AGENTBOX_UID" --regid "$AGENTBOX_GID" --clear-groups -- <cmd>`. `setpriv` (util-linux) is present in every adapter image.
- P0: Capability rationale: `chown` needs `CAP_CHOWN`; `setresuid`/`setresgid`/`setgroups` need `CAP_SETUID`/`CAP_SETGID`. `--cap-drop ALL` alone makes the privilege drop fail with `EPERM`, so those three capabilities are re-added (FR-8). They are dropped by the kernel when the entrypoint execs the non-root agent, and `--security-opt no-new-privileges` stays in force.
- P0: Files created in `/workspace` are owned by the host user. Verified by test.
- P1: `--user <uid:gid>` skips the privilege drop: agentbox passes `--user` to the engine and the entrypoint skips chown. If the fresh per-project volume would be unwritable by that UID, agentbox fails up front with a remedy (`--state-dir` with a pre-owned directory, or `--ephemeral`) instead of a late permission error. `--user 0` is debug-only and prints a warning.
- P1: `--agent image:<ref>` has no agentbox entrypoint, so agentbox passes `--user <host uid:gid>` directly and requires `--ephemeral` or a writable `--state-dir`; this keeps workspace writes host-owned (G5) without the privilege-drop machinery.
- P1: If the entrypoint sees a non-root effective UID (explicit `--user` or an engine userns mapping), it skips chown and execs directly.
- P2: Engine-mode matrix documented and reported by `doctor`, with per-mode ownership expectations: root Docker (default; full UID/GID contract), Podman (needs `--userns=keep-id` for host-owned workspace files), Docker Desktop (virtiofs maps ownership; UID logic is a no-op). Rootless Docker is best-effort and explicitly caveated: it has no `keep-id` equivalent (`--userns` accepts only `host`/`private`), so non-root container UIDs map to subuids and workspace files are not host-owned — either run the agent as container root (which maps to the host user under rootless) or accept the ownership caveat; `doctor` reports which mode is active. Workspace file ownership is asserted per engine mode in tests.

### FR-10 — Images

- P0: Adapter images are minimal: `node:24-bookworm-slim` pinned by digest + `bash ca-certificates git ripgrep util-linux` + the harness npm package installed with an exact version and `--ignore-scripts`. Target size ≤ 400 MB per image.
- P0: Image tag includes the agentbox version and harness version: `agentbox-pi:0.1.0-pi1.1.0`.
- P0: Pull by default once images are published (P1/M2). Until then, M0–M1 use a locally built image via the documented `make image` dev path; `--build` (P2/M3) builds locally from the vendored Dockerfile. First run prints a one-line "pulling image…" notice; subsequent runs are silent.
- P1: Published multi-arch images (`linux/amd64`, `linux/arm64`) at `ghcr.io/<org>/agentbox-<harness>`.
- P1: `agentbox doctor` reports engine version, image availability/digest, and whether a newer agentbox is available.
- P2: `--image-digest` recorded and printed with `--dry-run` for reproducible runs; optional pin in config.

### FR-11 — UX & observability

- P0: `--dry-run` prints the exact `docker run` argv, one shell-quoted flag per line (copy-pasteable, safe for paths with spaces), secrets redacted as `<set>`/`<unset>`. This doubles as the debugging tool and the audit artifact.
- P0: Errors are one line + one remedy, no raw Docker dumps. `AGENTBOX_DEBUG=1` prints the raw command and engine stderr.
- P0: Startup banner is quiet: a single line on stderr (e.g. `agentbox: pi @ /workspace ← /home/me/src/app (rw, net: bridge, state: 1a2b3c4d)`) before handing the TTY to the agent. The config-provenance line and the `--force` echo are always printed, even with `--quiet`, whenever a config file was applied or `--force` was used; the rest of the banner is suppressible with `--quiet`.
- P0: `agentbox version`. P1: `agentbox doctor` (engine check, image check, isolation self-test).
- P0: After exit, print nothing on success; on nonzero agent exit, print the exit code only (agent owns its own error output).

### FR-12 — Configuration file (P1)

- P1: Discovery: `--config <path>`, else `$XDG_CONFIG_HOME/agentbox/config.toml` (default `~/.config/agentbox/config.toml`; user), else `agentbox.toml` in the workspace (project). Precedence: CLI > project config > user config. The project config path is canonicalized and must resolve inside the workspace; symlinks escaping the workspace are rejected, and parse errors never echo file contents.
- P1: User config keys align 1:1 with flags; CLI flags always win.
- P1: Project config may set boundary-neutral keys only: `agent` (built-in adapter names only — never `image:<ref>`), `cpus`, `memory`, `pids-limit`, `ro`, `ephemeral`.
- P1: Any key outside that set is a hard error (never silently ignored). Boundary-relevant keys are never allowed in project config: `image`, `net`, `env`, `env-file`, `state-dir`, `credentials`, `user`, `name`, `keep`, plus `agent = "image:<ref>"`. `quiet` is rejected too, so a repo cannot suppress the config-provenance line or the FR-2 notices. An untrusted repo must not be able to add host env vars, swap the image, or redirect state.
- P1: The config-provenance line names every config file applied, even under `--quiet`, so a project config is never invisible.
- P2: Per-project profiles (`[profiles.offline]`, `[profiles.ci]`).

---

## 7. Non-Functional Requirements

| ID | Requirement |
| --- | --- |
| NFR-1 | Startup overhead added by agentbox (excluding image pull and agent boot) ≤ 300 ms. |
| NFR-2 | Single static binary (Go 1.22+, `CGO_ENABLED=0`), no runtime deps, ≤ 15 MB; `agentbox --help` works on a machine with no Docker (engine check deferred). |
| NFR-3 | Zero-write guarantee for host paths outside workspace + state: verified by a before/after manifest of the workspace, the state volume, `$HOME`'s top level, and the sensitive paths named by the FR-2 guardrail. Engine-owned writes (e.g. `~/.docker`, `/var/lib/docker`, Docker Desktop's `~/Library/Containers`) are excluded as engine behavior and documented; deeper unlisted paths under `$HOME` are out of the manifest's scope and stated as such. |
| NFR-4 | No telemetry. No network calls except the engine CLI/API, image pull, `doctor`'s optional update check (labeled, suppressible with `--no-check`), and the agent's own traffic. `AGENTBOX_DEBUG=1` makes engine calls visible. |
| NFR-5 | Cross-platform: Linux + macOS + WSL2 (ext4-backed workspaces; `/mnt/c` DrvFs has no POSIX ownership, so FR-9 ownership and `--ro` semantics are best-effort there). Windows-native is best-effort/untested; document as such. |
| NFR-6 | Test suite runs in CI with Docker; isolation acceptance tests are part of the required check set. |
| NFR-7 | Docs: README (5-minute quickstart), `docs/isolation.md` (contract + limits), `docs/harnesses.md` (adapter contract + adding one), `docs/credentials.md`, `docs/troubleshooting.md`. |
| NFR-8 | Reproducible release: pinned base image digest and harness versions recorded in the release manifest; image tags immutable once published. |

---

## 8. UX Specification

### 8.1 Primary flows

```bash
# Zero-config: current dir, pi, persistent state
$ agentbox
agentbox: pi @ /workspace ← /home/me/src/app (rw, net: bridge, state: 1a2b3c4d)
# ... pi TUI takes over ...
# on exit: container gone, state and workspace intact

# Explicit workspace + different harness
$ agentbox ~/src/other --agent claude

# Headless, forwarded args
$ agentbox -- --print "list the failing tests and fix them"

# Read-only exploration
$ agentbox ~/src/untrusted --ro

# Truly offline
$ agentbox --net none --agent shell

# Audit before running
$ agentbox --dry-run
docker run --rm --init -it \
  --name agentbox-app-8f3c2a \
  --label agentbox=1 --label agentbox.run=2c9e... --label agentbox.project=app-1a2b3c4d \
  --label agentbox.uid=1000 --label agentbox.owner=4242@boot-a1b2 \
  --cap-drop ALL --cap-add CHOWN --cap-add SETUID --cap-add SETGID \
  --security-opt no-new-privileges \
  --cpus 2 --memory 4g --pids-limit 512 \
  --tmpfs /tmp:size=256m \
  -v /home/me/src/app:/workspace:rw \
  -v agentbox-state-1000-1a2b3c4d:/state \
  -e AGENTBOX_UID=1000 -e AGENTBOX_GID=1000 -e HOME=/state/home \
  -e PI_CODING_AGENT_DIR=/state/pi \
  -e ANTHROPIC_API_KEY=<set> \
  agentbox-pi:0.1.0-pi1.1.0
```

### 8.2 Error messages (examples, verbatim targets)

```text
agentbox: workspace "/tmp/nope" does not exist
  → check the path, or run agentbox with no argument to use the current directory

agentbox: refusing to mount /home/me — this is your home directory, not a project
  → cd into the project, or pass --force if you really mean it

agentbox: "env" cannot be set by project config (agentbox.toml)
  → boundary-relevant keys (image, net, env, env-file, state-dir, credentials, user, name, keep) and agent = "image:<ref>" are CLI/user-config only

agentbox: note: workspace contains .env and executable .git/hooks — the agent can read them, and agent writes there run on the host later
  → see docs/isolation.md; suppress with --quiet

agentbox: cannot reach the container engine (docker)
  → is the Docker daemon running? try: docker info

agentbox: unknown flag "--modell"
  → agent arguments go after "--": agentbox -- --modell foo

agentbox: ANTHROPIC_API_KEY and ANTHROPIC_OAUTH_TOKEN are unset for harness "claude"
  → export one, pass --env, or run: agentbox auth claude
```

---

## 9. Acceptance Criteria

Each item maps to a test in the repo's test suite (`tests/isolation/`, `tests/cli/`).

1. *(workspace boundary)* Given `agentbox ~/src/app --agent shell`, when the container runs `pwd`, then output is `/workspace`.
2. When the container runs `ls /home`, then no host home contents are visible (container-only paths only).
3. When the container runs `cat "$HOME/.ssh/id_rsa"` and `cat /home/*/.ssh/id_rsa`, then the files do not exist.
4. When the container runs `ls /var/run/docker.sock`, then it does not exist.
5. Given `--agent shell` and a workspace write (`echo x > /workspace/f`), then `~/src/app/f` exists on the host owned by the host UID (asserted per engine mode: root Docker and Podman `--userns=keep-id`; rootless Docker is best-effort per FR-9).
6. Given a container write attempt to `/etc/agentbox-test` (non-root UID), then it fails with permission denied.
7. Given `--ro`, then `/workspace` writes fail; `--dry-run` output shows `:ro`.
8. Given no agentbox flags, when run from an ancestor of `$HOME`, then it exits nonzero with the guardrail message.

9. *(lifecycle & state)* After a normal, interrupted (`SIGINT`), or killed (`SIGKILL`) run (without `--keep`), no `agentbox-*` container remains; the SIGKILL case is satisfied by the next-startup sweep, which removes only containers owned by this uid whose owner process is dead and which are not labeled `agentbox.keep=1`. Concurrent live sessions (UC2) survive a sweep.
10. Given two workspaces A and B, distinct `agentbox-state-<uid>-<hash8>` volumes are used, sessions created in A are not listed when running B, and A's container has no path to B's state (no shared `/state` mount).
11. Given `--ephemeral`, then no state volume is created and no session persists.
12. Exit code from the agent (`exit 7`) is returned by `agentbox`.

13. *(credentials & environment)* With `AWS_SECRET_ACCESS_KEY` set on the host and not allowlisted, `agentbox --agent shell -- env` inside the container does not show it.
14. With `ANTHROPIC_API_KEY` set on the host, it is present inside the container for the `pi`/`claude` adapters.
15. `--dry-run` redacts allowlisted secret values as `<set>`.
16. No secret value from the test build appears in the image: `docker history` shows no secret ENV/ARG/label, and a filesystem scan of the built image for known key patterns is clean.

17. *(hardening)* `docker inspect` of the running container shows `CapDrop: [ALL]`, `CapAdd: [CHOWN, SETUID, SETGID]`, `no-new-privileges`, and the configured cpu/memory/pids limits.
18. `--net none` run cannot resolve or connect to `example.com`.

19. *(UX)* `agentbox --help` exits 0 with all documented flags present.
20. `agentbox --bogus` exits nonzero and suggests `--`.
21. `agentbox --dry-run` output is stable under a golden-file test with a fixed `AGENTBOX_TEST_NAME` (pins the container name, run uuid, and owner label) and no TTY (so `-it` is omitted).
22. *(config trust)* Given a project `agentbox.toml` containing a boundary-relevant key (`env`, `image`, `net`, `state-dir`, `credentials`, `user`, `name`, `keep`), `quiet`, or `agent = "image:<ref>"`, `agentbox` exits nonzero with an explanatory message and does not apply the key.

---

## 10. Milestones

| Milestone | Content | Exit criteria |
| --- | --- | --- |
| **M0 — Walking skeleton** | Go CLI parsing, `pi` + `shell` adapters, Dockerfile.pi, local image build (`make image`), workspace mount, `-it`, `--rm`, `--dry-run`, exit-code propagation | UC1 works end-to-end; AC 1–4, 12, 19–21 pass |
| **M0.5 — Correctness** | UID/GID drop, per-project state volume, env allowlist, guardrails, `--ephemeral` | AC 5–6, 9–11, 13–14 pass |
| **M1 — v0.1 release** | `claude`/`opencode` adapters, `--ro`, resource/net flags, hardening flags, docs (isolation contract), CI test suite | AC 1–21 pass; README quickstart; tagged release |
| **M2 — v1.0** | Published multi-arch images, `image:<ref>` adapter, config file, `agentbox auth`/`doctor`/`state`, credential-file mounts, `--state-dir`, macOS verification | Docs complete; AC 22 pass; NFR-1/2/8 met; second-engine smoke (Podman) |
| **M3 — Hardening+** | Egress allowlist (`--net restricted`), user-defined adapters, `doctor` security posture, `--build`, optional same-path mount, optional blocking guard for host-execution vectors (FR-2), Windows/WSL polish | Threat-model doc updated; egress test in CI |

---

## 11. Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
| --- | --- | --- | --- |
| Users assume isolation is stronger than it is (network egress, kernel) | High (false security) | High | Explicit isolation contract doc; banner shows net mode; `--net none` prominent in docs; no marketing claim beyond the contract |
| Credential leakage via env into `docker inspect`, shell history, or image layers | High | Medium | Allowlist only, redacted `--dry-run`, no creds in build, `--env-file` guidance, docs on engine-visible env |
| Root-owned files in workspace breaking host workflows | Medium | High if unhandled | Root entrypoint with minimal caps (`CHOWN`, `SETUID`, `SETGID`) then `setpriv` drop; per-engine-mode tests |
| Docker Desktop / rootless engines behave differently (chown, userns, UID mapping) | Medium | High | Entrypoint detects and skips privilege logic when already non-root; Podman uses `--userns=keep-id`; rootless Docker has no `keep-id` (`--userns` accepts only `host`/`private`) and is explicitly best-effort with a documented ownership caveat; `doctor` reports posture; engine-mode matrix in CI |
| Image pull latency / registry dependence on first run | Low | Medium | Small images, cached pulls, `--build` fallback, clear "pulling…" notice |
| State volume grows indefinitely (sessions) | Low | High over time | Per-project volumes; `state ls` shows size; `state rm`/`state gc`; docs; optional retention setting in M3 |
| Harness TUI misbehaves without a "real" TTY (Windows terminals, CI) | Medium | Medium | TTY flags conditional on stdin; document CI usage as `-- --print`; `--no-tty` override in M2 |
| Project config widens the boundary silently | High | Medium | All keys outside the safe set rejected in project config; `agent` restricted to built-in names; `quiet` rejected; provenance line always printed |
| Startup cleanup kills a concurrent live session | Medium (lost work) | Medium | Per-run `agentbox.run` label; startup sweep only touches same-uid containers whose owner pid+boot-id is dead; never targets the shared `agentbox=1` label |
| Runaway agent fills the host disk through the workspace | Medium | Medium | Bind mounts cannot be quotaed; docs warn; `--ro` for exploration runs; monitor workspace growth |
| Agent-written workspace artifacts executed later by host tooling (git hooks, lifecycle scripts) | High | Medium | §5.2 contract; FR-2 startup notice; docs tell users to treat agent-written files as untrusted; optional blocking guard in M3 |
| Scope creep into devcontainers/orchestration | Medium (delays v1) | High | Non-goals section enforced at review; toolchain needs answered by `--image` only |

---

## 12. Open Questions

1. **Mount path:** Is `/workspace` always right, or should default be the same absolute path as host (`/home/me/src/app`)? Same-path keeps editor/LSP output consistent; `/workspace` keeps paths stable across machines and matches the pi docs convention. Proposal: `/workspace` default, same-path as M3 option unless user research says otherwise.
2. **OAuth credential UX:** `--credentials` mounted file vs. one-time login inside a persistent container (`agentbox login`). Claude Code and pi subscription auth favor the latter; API keys favor the former. Proposal: `--credentials` (read-only, explicit) first; in-container `/login` persists only in the per-project volume (M2), never via implicit host mounts.
3. **Default persistence vs. privacy:** Persistent state means sessions and possibly credentials at rest in a Docker volume. Is state-on-by-default acceptable, or should `--ephemeral` be the default with `--persist` opt-in? Proposal: persist sessions per project (usability, and FR-5 now isolates them per project); never persist credentials implicitly — only an explicit in-container `/login` writes them.
4. **macOS/Windows:** Is Docker Desktop UID/GID behavior good enough to call macOS supported, or "best effort"? Affects NFR-5 wording and CI matrix.
5. **Multiple workspaces per run:** mounting two dirs (`--workspace a --workspace b`) is occasionally useful and expands the attack surface. Keep v1 single-workspace?
6. **Engine abstraction depth:** Podman compatibility layer (translate a few flags) or a proper driver interface? Affects M2 scope. Proposal: a shallow `engine` interface that builds argv and execs the engine CLI (see decision log); a native API driver stays possible later.
7. **Image build provenance:** verify npm tarball integrity / pin via lockfile-hash in the image build, or trust the registry digest? Supply-chain posture decision. Proposal: lockfile + `npm ci` with `--ignore-scripts`, base image pinned by digest, release manifest records harness version and image digest (NFR-8).
8. **Workspace same-filesystem concerns:** e.g. mounted git repos with hooks, `.env` files with real secrets — should agentbox warn when it detects `.env`, `.git/hooks`, or credential-looking files inside the workspace? (Leakage *into* the agent is intended, but users may not realize.) Decided for v1: notice-only, never blocks, and covers both credential files and host-execution vectors (FR-2); a blocking guard is an M3 option.

---

## 13. Appendix

### 13.1 Glossary

- **Agent harness:** the CLI program that drives an LLM and executes tools (pi, Claude Code, opencode).
- **Workspace:** the single host directory mounted into the container.
- **State volume:** per-project Docker named volume (`agentbox-state-<uid>-<hash8>`) holding harness sessions/settings.
- **Isolation contract:** the precise statement in §5 of what the container can and cannot touch.

### 13.2 Reference points

- pi isolation guidance: `pi/docs/containerization.md`, `pi/docs/security.md` (upstream project docs).
- Docker: `--userns`, `--cap-drop ALL`, `no-new-privileges`, `setpriv` privilege drop pattern (util-linux).
- Comparison set: Docker Sandboxes, NVIDIA OpenShell, Gondolin micro-VM, devcontainers, `toolbox`/`distrobox` (different goal: environment, not containment).
- Harness versions used to size the default images (host survey): pi 1.1.0, Claude Code 2.1.x, opencode.

### 13.3 Decision log

| Date | Decision | Rationale |
| --- | --- | --- |
| — | Workspace mounted at `/workspace` by default | Stable, documented convention; matches upstream pi guidance |
| — | Container ephemeral, state volume persistent | Keeps "container as process" mental model while preserving sessions |
| — | No host config mounts by default | The product's core promise; explicit opt-in for credentials |
| — | Docker first, Podman second | Docker is the lowest-common-denominator engine on developer machines |
| — | Per-project state volume (`agentbox-state-<uid>-<hash8>`) instead of one shared volume | A shared `/state` was readable across projects, contradicting the isolation contract |
| — | Root entrypoint with `CHOWN`/`SETUID`/`SETGID` re-added, then `setpriv` drop; no `--user` by default | `chown`/`setuid` need capabilities, and passing `--user` prevents first-run volume preparation |
| — | Boundary-relevant keys (`image`, `net`, `env`, `env-file`, `state-dir`, `credentials`, `user`, `name`, `keep`) and `agent = "image:<ref>"` rejected in project config | Untrusted repo content must not widen the boundary |
| — | Engine access by exec'ing the engine CLI (`docker`/`podman`) | Correct TTY/signal passthrough and a truthful `--dry-run`; a native API driver can come later |
| — | Project config restricted to built-in adapter names; `image:<ref>` and `quiet` rejected | An untrusted repo must not swap the image or hide the provenance line and notices |
| — | Per-run `agentbox.run` label plus owner pid/boot-id; cleanup never targets the shared label | Concurrent runs (UC2) and shared daemons must not lose containers or work to cleanup |
| — | `image:<ref>` runs under engine `--user` and requires `--ephemeral` or a writable `--state-dir` | Preserves G5 ownership without agentbox's privilege-drop entrypoint |
| — | Rootless Docker is best-effort (no `keep-id`); Podman uses `--userns=keep-id` | Docker's `--userns` accepts only `host`/`private`, so host-owned workspace files cannot be guaranteed there |
| — | Priorities re-based: P0 = v0.1 (M0–M1), P1 = v1.0 (M2), P2 = later (M3+) | Removes the P-tag/milestone contradictions |
| — | Language: Go (1.22+); stdlib plus a small hand-rolled parser, TOML for config, no Docker SDK in v1 | agentbox is process/argv/lifecycle glue over the engine CLI, where Go minimizes code per feature; `CGO_ENABLED=0` cross-compiles with no toolchain and keeps NFR-2's 15 MB budget comfortable; the official Docker SDK path stays open. Rust reconsidered only if scope moves toward a long-running supervisor |

---

*Reviewers: please challenge §3.2 (non-goals) and §5.2 (what is not protected) first — the value of this product depends on those two being honest and stable.*
