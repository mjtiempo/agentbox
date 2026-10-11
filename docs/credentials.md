# Credentials

`agentbox` never mounts host credential files implicitly and never writes key
material into images, argv, or config files. This page describes the explicit
mechanisms and their risks.

## What is never done

Host `~/.pi/agent`, `~/.claude`, `~/.config/opencode`, `~/.ssh`, `~/.aws`,
`~/.git-credentials`, `.netrc`, and `~/.docker/config.json` are **not** mounted
unless you explicitly point at a path with `--credentials`.

## 1. Environment allowlist (default)

Each adapter declares an allowlist of host env names (FR-6): provider keys
(`ANTHROPIC_API_KEY`, `ANTHROPIC_OAUTH_TOKEN`, `OPENAI_API_KEY`,
`OPENAI_BASE_URL`, `OPENROUTER_API_KEY`, `GEMINI_API_KEY`, `GOOGLE_API_KEY`),
proxies (`HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY` and lowercase variants), and
corporate CA variables (`SSL_CERT_FILE`, `NODE_EXTRA_CA_CERTS`). Claude also
accepts `CLAUDE_CODE_OAUTH_TOKEN`.

- Only allowlisted names are considered; unset names are omitted entirely.
- Values are passed as a bare `-e KEY`, so the value is read from agentbox's
  own environment and never appears in the engine argv or a host process
  listing.
- `--dry-run` shows `-e KEY=<set>` / `<unset>` instead of the value.
- Path-valued CA variables print a one-line warning: a host path may not exist
  in the adapter image. Bake the CA into a custom image or use `--credentials`.

## 2. Explicit host env: `--env KEY` (and `--env-file`)

`--env KEY` adds a single host variable (repeatable). `--env-file <path>`
reads `KEY=value` lines (comments and blank lines ignored; `export` and
quotes handled). `--env-file` paths resolve relative to the invocation CWD.

- An unset `--env KEY` is skipped with a one-line notice, never passed empty.
- Values from env files are injected into the agentbox child environment and
  passed as bare `-e KEY`, so they do not appear in argv either.

## 3. One-command setup: `agentbox quickstart [harness]`

```bash
agentbox quickstart pi          # host config + credentials, then start
```

It copies the harness's host config directory into the agentbox user folder and
adopts its credentials (equivalent to `config copy` + `auth copy`). When no
host credential is found it asks:

```text
auth: no provider/auth setup found for pi
Authenticate now? [Y/n]
```

Answering yes runs the harness's own authentication flow in a scratch container
(`opencode auth login`, or the TUI with `/login` for pi/claude) and adopts the
credential it produces, so every project is set up from then on. `--force`
replaces existing copied config; a non-interactive terminal never prompts.

## 4. Adopt host credentials: `agentbox auth copy <harness>`

The same credential adoption on its own: copy the credential your harness
already has on the host into agentbox once, and every later run uses it
automatically.

```bash
agentbox auth copy pi          # ~/.pi/agent/auth.json
agentbox auth copy claude      # ~/.claude/.credentials.json
agentbox auth copy opencode    # runs `opencode auth export` on the host
agentbox auth <harness>        # reports the detected mechanism
agentbox auth reset <harness>  # forget it (confirmation required)
```

- Stored at `~/.agentbox/credentials/<harness>.json` (0600, `AGENTBOX_USER_DIR`
  relocates the root) and mounted read-only at the adapter's credential target.
- opencode keeps credentials in its per-project database, so agentbox imports
  the stored export automatically on the first run in each project (a hash
  marker skips the import afterwards and re-imports when the file changes).
- Nothing is adopted implicitly: `auth copy` is the explicit opt-in. Runs print
  a one-line notice when the stored credential is used.
- Read-only means token refresh cannot write back: for OAuth flows that
  refresh, log in inside the container instead (state volume, per project).

## 5. Credential files: `--credentials <host-file>`

`--credentials` mounts a host file **read-only** at the adapter's expected
container target (repeatable):

| Adapter | Container target |
| --- | --- |
| `pi` | `/state/pi/auth.json` |
| `claude` | `/state/claude/.credentials.json` |
| `opencode` | `/state/opencode/credentials.json` |

opencode v2 keeps credentials in its per-project state database rather than a
file, so agentbox imports the mounted file automatically: on the first run
with `--credentials`, the entrypoint runs `opencode auth import` as the agent
identity and records a hash of the file in the project's state volume. Later
runs skip the import; replacing the file (new export) triggers it again.

```bash
opencode auth export > ~/oc-auth.json          # on the host, once
agentbox --agent opencode --credentials ~/oc-auth.json
```

That run is authenticated and every later run in the same project is too
(credentials live in that project's state DB). Other projects need the flag
once; each project's credentials stay isolated (FR-5). Do **not** point
`--credentials` at `opencode.db`: it is SQLite (needs read-write and locking),
it is not the import format, and sharing it would expose other projects'
sessions. Provider env keys (`--env ANTHROPIC_API_KEY`, ...) also work without
any import.

This is opt-in and grants the container that credential. `--credentials` is
boundary-relevant and can never be set by project config (FR-12). The file
must exist; harnesses without a credential target (`shell`) reject the flag
with a remedy.

## 6. Detection: `agentbox auth <harness>`

Prints which mechanism is detected — environment key, adopted credential,
host credential file, or state volume — and never a value:

```text
harness: claude
mechanism: environment
source: ANTHROPIC_API_KEY
```

When a run starts a credential-accepting harness with no mechanism at all,
agentbox prints the same style of notice once (suppressed by `--quiet`).

## Risks and caveats

- **Engine-visible environment.** Bare `-e KEY` keeps values out of argv, but
  the engine daemon and `docker inspect` on the container still see the
  environment. On a shared host, treat that as visible to other privileged
  users.
- **Anything in the container can use the credential.** A prompt-injected
  command can read and exfiltrate it. Scope keys narrowly.
- **Credential files are read-only mounts.** The harness cannot rotate them;
  the host file is the single source of truth.
- **In-container logins** (`/login` flows) write credentials into the
  per-project state volume at rest. `agentbox state rm <project>` deletes
  them; the volume is not encrypted.
- **Never put secrets in config files.** Project config cannot set `env`,
  `credentials`, or `image` at all; user config is still a file on disk — use
  `--env-file` with restrictive permissions instead.
