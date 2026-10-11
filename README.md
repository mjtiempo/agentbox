# agentbox

Run an AI coding agent inside an ephemeral container whose **only**
host-visible, writable surface is the project directory you name.

## Quickstart

```bash
# one-time setup: adopt your host harness config and credentials
agentbox quickstart            # pi (default); or: agentbox quickstart claude|opencode

# then, in any project
cd ~/src/my-app
agentbox                       # mounts this dir at /workspace, starts pi
```

`quickstart` copies your harness's host config into
`~/.agentbox/agents/<harness>/` and adopts its credentials. If no credentials
are found it offers to run the harness's own login flow (`opencode auth login`,
or `/login` inside pi/claude) and stores the result — so every project is ready
without flags:

```text
auth: no provider/auth setup found for pi
Authenticate now? [Y/n]
```

## Everyday commands

```bash
agentbox -- --print "explain this repo"   # forward args to the harness
agentbox ~/src/other --agent claude       # different workspace + harness
agentbox --dry-run                        # print the exact engine argv, then exit
agentbox --ro                             # read-only workspace for exploration
agentbox --net none --agent shell         # fully offline shell
agentbox --ephemeral                      # no state volume; sessions discarded
agentbox -e ANTHROPIC_API_KEY             # pass a host env var
agentbox --credentials ~/.pi/agent/auth.json   # mount a credential file read-only
```

## Command reference

```text
agentbox [options] [workspace] [-- <agent args...>]
agentbox quickstart [harness]
agentbox agents
agentbox auth <harness>
agentbox auth copy|reset <harness>
agentbox config link|copy|reset [harness] [--from <path>] [--force]
agentbox doctor [--no-check]
agentbox state ls | rm <project> | gc
agentbox version
```

| Command | What it does |
| --- | --- |
| `quickstart [harness]` | Host config + credentials in one step; offers the harness login flow when no credentials exist |
| `agents` | Lists adapters with image tags, harness versions, and credential env names |
| `auth <harness>` | Reports the detected credential mechanism (never values) |
| `auth copy <harness>` | Adopts host credentials once; later runs use them automatically |
| `auth reset <harness>` | Forgets the adopted credential (confirmation required) |
| `config link <harness>` | Symlinks `~/.agentbox/agents/<h>` to the host config dir |
| `config copy <harness>` | Copies it instead (sessions/auth/history excluded) |
| `config reset <harness>` | Removes the link or the copy |
| `doctor` | Engine, image, security posture, optional labeled update check |
| `state ls\|rm\|gc` | Per-project state volumes (removal needs confirmation) |
| `version` | Prints the version |

Run options (full list in `agentbox --help`):

| Area | Flags |
| --- | --- |
| Workspace | `[workspace]` (default `$PWD`), `--ro`, `--force`, `--workdir-path` |
| Harness | `--agent, -A <name>`, `--image <ref>`, `--build` |
| Lifecycle | `--dry-run`, `--quiet, -q`, `--keep`, `--name <name>`, `--no-tty` |
| State | `--ephemeral`, `--state-dir <path>` |
| Environment | `--env, -e <KEY>`, `--env-file <path>`, `--credentials <file>`, `--no-user-config` |
| Network | `--net bridge\|none\|host` |
| Resources | `--cpus <n>`, `--memory <size>`, `--pids-limit <n>` |
| Identity | `--user <uid:gid>`, `--engine docker\|podman` |
| Config file | `--config <path>` |

`--` forwards everything after it verbatim to the agent binary
(`agentbox -- --print "…"`).

## How isolation works

| Surface | Default |
| --- | --- |
| Workspace | the named directory, mounted at `/workspace` (rw; `--ro` for read-only) |
| Host home, other repos, `~/.ssh`, `~/.aws`, browser data | not visible |
| Docker socket | not mounted |
| Environment | explicit allowlist only (FR-6); host env is not inherited |
| Persistence | one volume per project: `agentbox-state-<uid>-<hash8>` at `/state`; user-level harness content (plugins, skills) in `~/.agentbox/agents/<harness>/` |
| Container root filesystem, `/tmp` | writable, ephemeral, destroyed on exit |
| Network | full egress by default; `--net none` for offline work |

The full contract — including what it does **not** protect against — is in
[docs/isolation.md](docs/isolation.md). Read that before trusting it with
anything.

## Install

Requirements:

- Linux (primary), macOS with Docker Desktop, or WSL2
- Docker 24+ (Podman works with `--engine podman`; see the engine matrix in
  [docs/troubleshooting.md](docs/troubleshooting.md))
- No host language runtime: agentbox is a single static Go binary

Build from source:

```bash
make build          # -> ./agentbox
make image          # builds agentbox-base and agentbox-pi locally
make test           # unit + golden tests, no Docker needed
make test-isolation # real-container acceptance suite (Docker required)
```

Put it on your PATH:

```bash
ln -s "$PWD/agentbox" ~/.local/bin/agentbox
```

## Documentation

- [docs/isolation.md](docs/isolation.md) — the exact isolation contract and limits
- [docs/harnesses.md](docs/harnesses.md) — adapters, adding one, prerequisites
- [docs/credentials.md](docs/credentials.md) — quickstart, `auth copy`, `--credentials`, and what is never stored
- [docs/troubleshooting.md](docs/troubleshooting.md) — engine modes, ownership, SELinux, macOS/WSL
- [docs/acceptance.md](docs/acceptance.md) — every acceptance criterion and the test that proves it
- [PRD.md](PRD.md) / [AGENT_PLAN.md](AGENT_PLAN.md) — requirements and build plan

## Non-goals

Not a VM or kernel-escape defense, not an orchestrator, not a devcontainer
builder, not a secret manager, not a cloud sandbox. `agentbox` never executes
agent code on the host and never mounts the engine socket.
