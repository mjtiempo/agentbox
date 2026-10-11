# Isolation contract

This document states precisely what an agent running under `agentbox` can and
cannot touch. It mirrors PRD §5 and makes no claim stronger than that section.

## What the boundary guarantees

For a process running inside the `agentbox` container:

| Resource | Default access |
| --- | --- |
| Workspace directory (host) | read-write at `/workspace` (read-only with `--ro`) |
| Host home directory, other repos, `~/.ssh`, `~/.aws`, browser data | **not visible**; no path exists in the container |
| Docker/container-engine socket (`/var/run/docker.sock`) | **not mounted** |
| Host processes, host PIDs, host `localhost` services | not visible; the container has its own PID/network namespace |
| Agent config/session data | only this project's state volume (`agentbox-state-<uid>-<hash8>`); other projects' volumes are never mounted; host `~/.pi/agent` etc. are **never** mounted implicitly |
| Persistent user folder | `~/.agentbox/agents/<harness>/` is mounted at the harness config path for user-level content (plugins, skills, themes); sessions and auth stay in the per-project volume. Disable with `--no-user-config` |
| Environment variables | only the adapter's explicit allowlist plus `--env`/`--env-file` (FR-6); the host environment is **not** inherited wholesale |
| Network | full egress by default (model APIs); `--net none` for offline work. Bridge mode isolates the host loopback but not services bound to non-loopback addresses, including the Docker gateway `172.17.0.1` |
| Container root filesystem | writable, ephemeral, destroyed with the container |
| `/tmp` | tmpfs (`size=256m`), destroyed with the container |

Harnesses that need to execute helper libraries from a temp directory (Bun's
`dlopen` in opencode, pi's transpile cache) get a `TMPDIR` on the state volume;
`/tmp` itself stays a `noexec` tmpfs.

Hardening defaults applied to every run (FR-8): `--cap-drop ALL`,
`--cap-add CHOWN --cap-add SETUID --cap-add SETGID`,
`--security-opt no-new-privileges`, `--init`, `--tmpfs /tmp:size=256m`,
`--cpus 2`, `--memory 4g`, `--pids-limit 512`. The three capabilities exist
only so the entrypoint can prepare state and drop privileges; the kernel
clears them when it execs the non-root agent. No `--privileged`, no device
passthrough.

## What the boundary does not protect against

- **Network egress.** With default networking, anything readable in the
  container can be sent out. `--net none` is the mitigation; egress
  allowlisting is not implemented (M3).
- **Prompt injection.** Repo content, dependencies, and tool output can steer
  the model. Isolation limits blast radius, not the decision to run a command.
- **Credentials you hand in.** A passed API key is usable by anything in the
  container.
- **Kernel/container-runtime escapes.** A Docker/runC/kernel vulnerability is
  out of scope. The host container daemon remains a root-equivalent trust
  anchor.
- **Resource exhaustion** beyond the configured limits. Disk usage is not
  quotaed: a runaway agent can fill the host filesystem through the workspace.
- **Workspace damage.** The workspace is read-write by default; `--ro` exists
  for that reason and git is the recovery mechanism.
- **Host-side execution of workspace artifacts.** The container can write files
  the host later executes: `.git/hooks/*`, dangerous `.git/config` keys,
  package lifecycle scripts, `.envrc`, editor tasks, Makefiles. `agentbox`
  itself has no host execution path, but the workspace is a persistent bridge
  back to the host. Treat agent-written workspace files as untrusted before
  running host tooling. FR-2 prints a one-line notice when common vectors are
  present.

The contract in one sentence: *a generated command can only damage or read the
workspace, the agent state volume, and its own ephemeral container, or leak
data over the network.*

## Persistent user folder

agentbox keeps user-level harness content in `~/.agentbox/agents/<harness>/`
and mounts it at the harness config path, so plugins and skills survive across
sessions and projects:

| Harness | Host folder | Mounted at | Shared | Stays per-project |
| --- | --- | --- | --- | --- |
| pi | `~/.agentbox/agents/pi/` | `/state/pi` | extensions, skills, themes, any entry you add | sessions, auth.json, history |
| claude | `~/.agentbox/agents/claude/` | `/state/claude` | plugins, skills, themes | project transcripts, credentials, history |
| opencode | `~/.agentbox/agents/opencode/` | `/state/opencode/config` | plugins, skills, agents, commands | data (sessions, auth, storage) |

- The folder is created on first run (mode 0700) with the harness's standard
  user-level directories; any other entry you place there is mounted too.
- Writes made inside the container to those paths land on the host.
- It is the only host-writable surface besides the workspace and the
  per-project state volume. `--no-user-config` disables it;
  `AGENTBOX_USER_DIR` relocates the root.
- Sessions are deliberately **not** shared: project A still cannot read
  project B's sessions (FR-5).

## Zero-write guarantee (NFR-3)

`tests/isolation/17-nfr3.sh` takes a before/after manifest of `$HOME`'s top
level, `/etc`, `/var/run`, and the FR-2 sensitive paths, runs an agent, and
fails if anything outside the workspace and state volume changed.

Excluded as engine behavior (documented, not asserted): `~/.docker`,
`/var/lib/docker`, `/var/run/docker.sock`, Docker Desktop's
`~/Library/Containers`, and the named-volume storage under the engine's data
root. `~/.agentbox` is also excluded because it is the intentional,
user-controlled persistent user folder described above; run with
`--no-user-config` to keep the original zero-write profile. Deeper unlisted
paths under `$HOME` are outside the manifest's scope.

## Engine modes and ownership (FR-9 P2)

| Engine mode | Workspace file ownership | Notes |
| --- | --- | --- |
| Root Docker (default) | host UID/GID, guaranteed | entrypoint drops root -> `AGENTBOX_UID`/`GID` via `setpriv` |
| Podman | host UID/GID with `--userns=keep-id` (agentbox adds it automatically) | rootless Podman maps the host user into the container |
| Docker Desktop (macOS/Windows) | mapped by virtiofs; UID logic is effectively a no-op | see docs/troubleshooting.md |
| Rootless Docker | **best effort** | Docker's `--userns` accepts only `host`/`private`; there is no `keep-id`, so non-root container UIDs map to subuids and files may not be host-owned. Run as container root (maps to the host user) or accept the caveat; `agentbox doctor` reports the mode |
| `--user <uid:gid>` | whatever that UID is | skips the privilege drop; a fresh named volume is refused up front with a remedy |
| `--agent image:<ref>` | host UID/GID via engine `--user` | no agentbox entrypoint; requires `--ephemeral` or a writable `--state-dir` |

## Known limits

- `--state-dir` chowns the host directory to the agent UID/GID (it is usually
  already yours); the entrypoint chown is deliberately non-recursive.
- If a prior root-owned run leaves unwritable entries in a state volume,
  remediate with `agentbox state rm <project>`, not by expecting a per-start
  `chown -R`.
- Disk is not quotaed; `--ro` and `state gc` are the available controls.
