package cli

// Help returns the full usage text. Every flag documented in PRD FR-1 appears
// here, verbatim, so `agentbox --help` is a complete reference (AC 19).
func Help() string {
	return `agentbox — run an AI coding agent in a container whose only host-visible,
writable path is the workspace you name.

usage: agentbox [options] [workspace] [-- <agent args...>]
       agentbox agents
       agentbox auth <harness>
       agentbox auth copy|reset <harness>
       agentbox doctor
       agentbox state ls | rm <project> | gc
       agentbox config link | copy | reset [harness] [--from <path>] [--force]
       agentbox quickstart [harness]
       agentbox version

options:
  [workspace]            Directory mounted as /workspace (default: $PWD)
  -A, --agent <name>     Harness adapter: pi, shell, claude, opencode, image:<ref> (default: pi)
      --image <ref>      Override the container image
      --dry-run          Print the exact engine command and exit
  -q, --quiet            Suppress the startup banner and notices
      --ro               Mount the workspace read-only
      --force            Override the workspace guardrail; echoed in the banner and --dry-run
      --ephemeral        No state volume; sessions/config are discarded on exit
      --state-dir <path> Bind-mount a host directory as /state instead of a named volume
  -e, --env <KEY>        Pass host env var KEY into the container (repeatable)
      --env-file <path>  Pass env vars from a file (repeatable; relative to the invocation CWD)
      --credentials <f>  Mount a host credential file read-only at the adapter target (repeatable)
      --net <mode>       Network mode: bridge | none | host (default: bridge)
      --cpus <n>         CPU limit (default: 2)
      --memory <size>    Memory limit (default: 4g)
      --pids-limit <n>   PID limit (default: 512)
      --user <uid:gid>   Run the agent as this UID:GID (skips the privilege drop)
      --engine <name>    Container engine: docker | podman (default: auto-detect)
      --name <name>      Container name (useful with --keep)
      --keep             Do not remove the container on exit
      --workdir-path <p> Mount point inside the container (default: /workspace)
      --config <path>    Config file (default: $XDG_CONFIG_HOME/agentbox/config.toml, else agentbox.toml)
      --build            Build the adapter image locally instead of pulling
      --no-check         Skip the doctor update check
      --no-tty           Force non-TTY mode (omit -it)
      --no-user-config   Do not mount the persistent user folder (~/.agentbox/agents/<harness>)
      --version          Print version and exit
  -h, --help             Print this help and exit
      --                 Everything after is forwarded verbatim to the agent binary
`
}
