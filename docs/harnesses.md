# Harnesses (adapters)

An adapter is a built-in descriptor: image, command, state paths, credential
env allowlist, and an optional credential-file target (FR-4).

## Built-in adapters

| Adapter | Image (dev tag) | State paths | Credential env (allowlist) | Credential file target |
| --- | --- | --- | --- | --- |
| `pi` (default) | `agentbox-pi:<ver>-pi1.1.0` | `PI_CODING_AGENT_DIR=/state/pi` | provider keys + proxies + CA vars | `/state/pi/auth.json` |
| `claude` | `agentbox-claude:<ver>-claude2.1.296` | `CLAUDE_CONFIG_DIR=/state/claude` | anthropic vars + provider keys + proxies + CA vars | `/state/claude/.credentials.json` |
| `opencode` | `agentbox-opencode:<ver>-opencode2.0.26` (runs `opencode --standalone`, `TMPDIR` on the state volume; `--credentials` is imported once per project) | `XDG_CONFIG_HOME=/state/opencode/config`, `XDG_DATA_HOME=/state/opencode/data`, `XDG_STATE_HOME=/state/opencode/state`, `XDG_CACHE_HOME=/state/opencode/cache` | provider keys + proxies + CA vars | — |
| `shell` | `agentbox-base:<ver>` | n/a | none (use `--env`) | — |
| `image:<ref>` | your image | yours | none | — |

`agentbox agents` prints this table with the exact tags for the current build.

> **opencode v2:** the adapter tracks the v2 CLI (`@opencode/cli`, pinned
> 2.0.26). The older 1.x line (`opencode-ai`) is no longer the adapter default;
> it stays reachable by overriding the image (build your own with
> `--agent image:<ref>`, or pin an older `agentbox-opencode` tag). agentbox
> pins exactly one harness version per release (NFR-8).

Argument composition is always:

```text
image entrypoint [adapter default args] [ephemeral args] [-- user args...]
```

User args come last and can override harness defaults, e.g.
`agentbox -- --model gpt-5`.

## Persistent user folder (plugins, skills)

User-level content persists across projects in `~/.agentbox/agents/<harness>/`
and is mounted at the harness config path:

| Harness | Host folder | Shared (persistent) | Per-project (state volume) |
| --- | --- | --- | --- |
| pi | `~/.agentbox/agents/pi/` | `extensions/`, `skills/`, `themes/`, plus any entry you add | `sessions/`, `auth.json`, history |
| claude | `~/.agentbox/agents/claude/` | `plugins/`, `skills/`, `themes/`, plus any entry you add | session transcripts, credentials, history |
| opencode | `~/.agentbox/agents/opencode/` | `plugins/`, `skills/`, `agents/`, `commands/`, plus any entry you add | `$XDG_*` dirs on the state volume: `config/`, `data/` (sessions DB, credentials), `state/`, `cache/` |

- Install plugins/skills from inside the container and they land in the host
  folder; or drop files there directly.
- Sessions stay per-project, so switching projects never exposes another
  project's session history.
- `--no-user-config` disables the folder; `AGENTBOX_USER_DIR` relocates the
  root (default `~/.agentbox`).

### Quickstart

```bash
agentbox quickstart <harness>   # host config + credentials in one step
```

Copies the host config directory into the user folder and adopts host
credentials; if none are found it offers to run the harness's own login flow
and adopts the result. Details in [credentials.md](credentials.md).

### Adopting your host credentials

```bash
agentbox auth copy <harness>    # pi, claude, opencode
agentbox auth reset <harness>   # forget it again
```

Adopted credentials live in `~/.agentbox/credentials/<harness>.json` (0600) and
are mounted automatically on later runs — no `--credentials` needed. opencode's
export is imported into each project's database on first use.

### Adopting your existing harness config

```bash
agentbox config link [harness]   # symlink ~/.agentbox/agents/<h> -> the host config
agentbox config copy [harness]   # copy it instead (sessions/auth/history excluded)
agentbox config reset [harness]  # remove the link or the copy
```

`link`/`copy` discover the harness's host config automatically
(`~/.pi/agent`, `~/.claude`, `${XDG_CONFIG_HOME:-~/.config}/opencode`); use
`--from <path>` for a non-standard location. `copy` merges into the
pre-created standard dirs and never overwrites existing files without
`--force`; it skips session/auth/history entries by design. `reset` removes
only the agentbox link (never the linked target) and asks for confirmation
unless `--force` is passed.

## What lives in state (cost of `state rm`)

| Adapter | Contents of `/state` |
| --- | --- |
| `pi` | sessions, settings, model catalog/caches under `/state/pi` |
| `claude` | config, session transcripts, and any credential file written by an in-container login under `/state/claude` |
| `opencode` | config under `/state/opencode/config`, data/caches under `/state/opencode/data` |
| `shell` | whatever you write to `/state` |

`agentbox state rm <project>` deletes all of it for that project; there is no
per-file recovery. `agentbox state gc` removes volumes whose recorded
workspace path no longer exists (confirmation required; `--force` in scripts).

## Prerequisites and known gaps

- The host `.gitconfig` is **not** mounted. Git commits inside the container
  need identity via env: `--env GIT_AUTHOR_NAME --env GIT_AUTHOR_EMAIL --env
  GIT_COMMITTER_NAME --env GIT_COMMITTER_EMAIL`, or an in-container
  `git config --global` persisted in state.
- Workspace `.git` files that point outside the workspace are unsupported:
  linked worktrees (`git worktree`) and submodules with absolute paths will
  not resolve.
- A TTY is required for interactive TUIs. Without one, use a headless flag,
  e.g. `agentbox -- --print "..."`; `--no-tty` forces non-TTY mode.
- `--ephemeral` discards `/state` with the container; harnesses run their
  no-session equivalent where available (`pi --no-session`).
- `image:<ref>` runs the image's own entrypoint under engine `--user` and has
  no state preparation; pair it with `--ephemeral` or a writable `--state-dir`.

## Adding an adapter (development)

Adapters are code (`internal/adapter/builtins.go`), not config files. To add
one:

1. Add a descriptor with `Name`, `Image`, `Env` (container config paths under
   `/state`), `CredEnv` (host env allowlist), `CredFile` (container target or
   `""`), `DefaultArgs`, optional `EphemeralArgs`, and `UseEntrypoint: true`.
2. Add an image under `images/<name>/Dockerfile` based on
   `agentbox-base:<ver>`; install the harness with an exact version and
   `--ignore-scripts`, and document any vendor postinstall you run explicitly.
3. Wire the image target into the `Makefile` and the version constant into
   `internal/adapter/builtins.go` (and the release manifest).
4. Add unit tests for the descriptor and a dry-run assertion; run
   `make test && make test-isolation`.

Keep adapter state paths inside `/state` only: because each project mounts its
own volume, no adapter can read another project's sessions or settings.
