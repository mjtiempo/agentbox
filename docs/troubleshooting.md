# Troubleshooting

One-line errors carry one remedy; run with `AGENTBOX_DEBUG=1` to see the raw
engine command and its stderr.

## The engine is unreachable

```text
agentbox: cannot reach the container engine (docker)
  → is the Docker daemon running? try: docker info
```

Check `docker info` (or `podman info`), then `agentbox doctor --no-check` for
the detected mode. `AGENTBOX_ENGINE=docker|podman` overrides auto-detection;
`--engine` does the same per invocation.

## Ownership problems

| Symptom | Cause | Fix |
| --- | --- | --- |
| workspace files owned by root or a subuid | rootless Docker (no `keep-id`) | run with `--user 0` (container root maps to you under rootless) or accept the caveat; rootful Docker and Podman are guaranteed |
| `--user` fails up front with "fresh per-project state volume would be unwritable" | a new named volume is root-owned and `--user` skips the entrypoint drop | pass `--ephemeral`, or `--state-dir` pointing at a directory owned by that uid |
| permission denied writing `/state` after a previous root-owned run | entries left by an older run | `agentbox state rm <project>` and start fresh; agentbox deliberately never `chown -R`s state |
| `--state-dir` directory changed ownership | the entrypoint chowns `/state` to the agent UID/GID | expected and documented; it is normally already yours |

`agentbox doctor` reports rootful vs rootless and the security posture.

## SELinux (Fedora/RHEL)

When the kernel reports SELinux enforcing, agentbox adds `:z` to workspace and
state mounts automatically. If mounts still fail, the engine-side alternative
is `--security-opt label=disable`; agentbox does not pass it by default because
it weakens labeling.

## TTY problems

- Interactive TUIs need a TTY; `-it` is added only when stdin is a terminal.
- In CI, use headless arguments (`agentbox -- --print "..."`) or `--no-tty`.
- Ctrl-C with a TTY is forwarded by the engine to the agent's terminal; without
  a TTY, agentbox forwards SIGINT and a second SIGINT force-removes the
  container and exits 130.

## The TUI takes several seconds on the first run in a project

pi's extension loader (jiti) transpiles configured packages on first load and
caches the result. agentbox points that cache at the persistent state volume
(`TMPDIR=/state/home/.cache/tmp`, `JITI_RESPECT_TMPDIR_ENV=1`), so the first
run in a project pays the transpile (a few seconds, depending on how many
packages `settings.json` lists) and every later run is fast (~0.7 s, matching
native pi with a warm cache). A brand-new project starts cold again; raise
`--cpus` if the cold transpile feels slow.

agentbox says so up front, so the first run does not look hung:

```text
agentbox: first run in this project: preparing the harness cache (later runs start faster)
```

The line appears once per project (suppressed by `--quiet`).

## opencode: "Failed to open library /tmp/.bun-....so" or a slow TUI

Two opencode-specific issues and their fixes:

- **Bun native library error.** opencode v2 (Bun) extracts a render library and
  `dlopen`s it from `$TMPDIR`. The container's `/tmp` is a `noexec` tmpfs, so
  the agentbox adapter points `TMPDIR` at the exec-capable state volume
  (`/state/home/.cache/tmp`). `/tmp` deliberately stays `noexec`; no flag is
  needed.
- **~5 s wait on every start after the first.** opencode spawns a background
  `serve --service` process; in an ephemeral container it is pointless and
  costs about five seconds per warm start. The adapter passes
  `--standalone` (private server) instead. If you override the adapter's args
  and remove it, expect that delay back.

## Plugins and skills disappear between sessions

User-level harness content persists in `~/.agentbox/agents/<harness>/`:

- pi: `extensions/`, `skills/`, `themes/`
- claude: `plugins/`, `skills/`, `themes/`
- opencode: `plugins/`, `skills/`, `agents/`, `commands/`

Install from inside the container or drop files into the host folder; anything
else you place there is mounted too. To adopt your existing harness config:

```bash
agentbox config link      # symlink ~/.agentbox/agents/<h> to ~/.pi/agent etc.
agentbox config copy      # or copy it (sessions/auth/history excluded)
agentbox config reset     # remove the link or copy
```

Sessions and auth stay per-project. `--no-user-config` disables the folder;
`AGENTBOX_USER_DIR` moves the root.
With `--user`, the config chain must already be owned by that uid (the
entrypoint skips its chown), so prefer a pre-owned `--state-dir` or
`--no-user-config` in that mode.

## State volumes

```bash
agentbox state ls              # name, project, path, size (where exposed)
agentbox state rm <project>    # confirmation required; --force in scripts
agentbox state gc --force      # remove volumes whose workspace no longer exists
```

`state gc` lists candidates first and only removes after confirmation — a
script without `--force` exits 2 and removes nothing.

## Images and sizes

| Image | Size (dev build) | FR-10 target |
| --- | --- | --- |
| `agentbox-base` | ~299 MB | <= 400 MB |
| `agentbox-pi` | ~369 MB | <= 400 MB |
| `agentbox-opencode` (v2) | ~515 MB | over target: upstream ships a ~210 MB native binary |
| `agentbox-claude` | ~556 MB | over target: upstream ships a ~257 MB native binary |

The over-target images are a recorded exception, not an accident: the
harness's own binary dominates the size and cannot be removed without breaking
the harness. `scripts/release-check.sh` enforces the documented per-image
budgets and records actual digests in the release manifest (NFR-8).

If an image is missing, agentbox pulls it once with a `pulling image …` line;
local images built with `make image` are used silently. `--build` builds the
adapter image from the vendored Dockerfile when the binary runs from a source
checkout (it needs `images/<harness>/Dockerfile` next to the repo root).

## macOS, WSL2, Windows

- **macOS (Docker Desktop):** supported with ownership caveats. Workspace
  writes are mapped by virtiofs; UID/GID logic is effectively a no-op, so
  files may not appear host-owned. `--ro` and TTY behavior are verified; the
  engine-mode matrix above applies.
- **WSL2:** works on ext4-backed workspaces (`\\wsl$\...`/`/home/...`). On
  `/mnt/c` (DrvFs) there is no POSIX ownership, so FR-9 ownership and `--ro`
  semantics are best-effort; use a workspace under the Linux filesystem.
- **Windows-native:** best-effort/untested. Use WSL2.

## Configuration problems

```text
agentbox: "env" cannot be set by project config (agentbox.toml)
  → boundary-relevant keys (image, net, env, env-file, state-dir,
    credentials, user, name, keep) and agent = "image:<ref>" are CLI/user-config only
```

Project config may set only `agent` (built-in names), `cpus`, `memory`,
`pids-limit`, `ro`, `ephemeral`. `quiet` is rejected too, so a repository
cannot hide the provenance line or the FR-2 notices. Every applied config file
is named on stderr even under `--quiet`.

## Debugging checklist

```bash
agentbox --dry-run            # exact engine argv, secrets redacted
AGENTBOX_DEBUG=1 agentbox     # raw command + engine stderr
agentbox doctor --no-check    # engine, image, posture
agentbox agents               # image tags and credential env names
```
