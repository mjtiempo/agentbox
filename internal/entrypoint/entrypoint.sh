#!/bin/sh
# agentbox entrypoint (FR-9 frozen contract).
#
# 1. If EUID != 0: prepare HOME, then exec "$@"  (explicit --user / engine userns)
#    — no chown, no privilege use; some engines (Podman --userns=keep-id) start
#    the container as the host uid, so this path must still set up HOME.
# 2. chown /state to the agent identity, then create ${AGENTBOX_HOME}
# 3. export HOME="${AGENTBOX_HOME:-/state/home}"
# 4. exec setpriv --reuid ... --regid ... --clear-groups -- "$@"
#
# Capability rationale (FR-8): --cap-drop ALL removes CAP_DAC_OVERRIDE, so
# container root can only write where it owns or has permission. A
# bind-mounted --state-dir is owned by the host user, so HOME creation falls
# back to running mkdir as the agent identity rather than widening caps.
# The chown is deliberately non-recursive: the dropped agent owns everything
# it creates, and a recursive heal would re-walk large state volumes on every
# start.
#
# Requires util-linux (setpriv) and the capabilities CHOWN/SETUID/SETGID,
# which FR-8 re-adds after --cap-drop ALL.
set -eu

if [ "$(id -u)" != "0" ]; then
	# Non-root start (explicit --user, or an engine userns mapping such as
	# Podman's --userns=keep-id): skip chown, but still make sure HOME exists
	# and points somewhere writable before handing over to the harness.
	nonroot_home="${AGENTBOX_HOME:-/state/home}"
	if ! mkdir -p "$nonroot_home" 2>/dev/null; then
		nonroot_home=/tmp/agentbox-home
		mkdir -p "$nonroot_home" 2>/dev/null || true
	fi
	export HOME="$nonroot_home"
	exec "$@"
fi

: "${AGENTBOX_UID:?agentbox: AGENTBOX_UID is required when the container starts as root}"
: "${AGENTBOX_GID:?agentbox: AGENTBOX_GID is required when the container starts as root}"

AGENTBOX_HOME_DIR="${AGENTBOX_HOME:-/state/home}"

# /state first: fixing ownership before creating anything inside it lets the
# HOME fallback below work without CAP_DAC_OVERRIDE.
if [ -d /state ]; then
	chown "$AGENTBOX_UID:$AGENTBOX_GID" /state
fi

# Docker creates mount destinations (and their parents) as root inside the
# state volume: the user-folder mounts under the harness config dir, and the
# read-only credential file mount. Hand every directory in those chains under
# /state to the agent identity so it can create siblings (sessions, logs)
# next to them. Non-recursive per directory; mount points are already owned by
# the host user. The credential file itself is read-only, hence the `|| true`.
chown_chain() {
	dir="$1"
	while [ -n "$dir" ] && [ "$dir" != "/" ] && [ "$dir" != "/state" ]; do
		if [ -d "$dir" ]; then
			chown "$AGENTBOX_UID:$AGENTBOX_GID" "$dir" 2>/dev/null || true
		fi
		dir="$(dirname "$dir")"
	done
}

if [ -n "${AGENTBOX_CONFIG_DIR:-}" ]; then
	chown_chain "$AGENTBOX_CONFIG_DIR"
fi

if [ -n "${AGENTBOX_CRED_FILE:-}" ]; then
	chown_chain "$(dirname "$AGENTBOX_CRED_FILE")"
fi

# Create HOME as root where possible (named volume, ephemeral /state); when
# root lacks write permission (bind-mounted --state-dir owned by the host
# user, especially with restrictive modes), create it as the agent identity
# instead — it is then already owned by the agent and needs no chown.
if mkdir -p "$AGENTBOX_HOME_DIR" 2>/dev/null; then
	chown "$AGENTBOX_UID:$AGENTBOX_GID" "$AGENTBOX_HOME_DIR"
elif ! setpriv --reuid "$AGENTBOX_UID" --regid "$AGENTBOX_GID" --clear-groups -- \
	mkdir -p "$AGENTBOX_HOME_DIR" 2>/dev/null; then
	# Some rootless engine setups leave / unwritable for the agent identity and
	# provide no writable state; fall back to the tmpfs so the harness can
	# still start (state is ephemeral for this run).
	AGENTBOX_HOME_DIR=/tmp/agentbox-home
	mkdir -p "$AGENTBOX_HOME_DIR" 2>/dev/null || \
		setpriv --reuid "$AGENTBOX_UID" --regid "$AGENTBOX_GID" --clear-groups -- \
			mkdir -p "$AGENTBOX_HOME_DIR"
fi

export HOME="$AGENTBOX_HOME_DIR"

# Harnesses that keep credentials in their own store (opencode's per-project
# database) import the mounted --credentials file once per credential
# revision. The runner passes the harness's import argv (subcommand, target,
# trailer args) in AGENTBOX_CRED_IMPORT; a hash marker in the state volume
# makes subsequent starts free and re-imports when the file changes.
if [ -n "${AGENTBOX_CRED_FILE:-}" ] && [ -f "$AGENTBOX_CRED_FILE" ] \
	&& [ -n "${AGENTBOX_CRED_IMPORT:-}" ]; then
	marker="/state/.agentbox-cred-import-$(basename "$1")"
	# The mounted file is owned by the host user and /state belongs to the
	# agent; root has no CAP_DAC_OVERRIDE here, so hash, import, and marker all
	# run as the agent identity.
	as_agent() {
		setpriv --reuid "$AGENTBOX_UID" --regid "$AGENTBOX_GID" --clear-groups -- "$@"
	}
	sum="$(as_agent sha256sum "$AGENTBOX_CRED_FILE" 2>/dev/null | cut -d' ' -f1)"
	imported="$(as_agent cat "$marker" 2>/dev/null || true)"
	if [ -z "$sum" ] || [ "$imported" != "$sum" ]; then
		# Word splitting is intentional: the argv is fixed and space-free.
		# shellcheck disable=SC2086
		if as_agent "$1" $AGENTBOX_CRED_IMPORT >/dev/null 2>&1; then
			if [ -n "$sum" ]; then
				as_agent sh -c 'printf "%s" "$1" > "$2"' _ "$sum" "$marker" 2>/dev/null || true
			fi
		elif [ "${AGENTBOX_QUIET:-}" != "1" ]; then
			echo "agentbox: could not import the mounted credential file; run the harness import command manually" >&2
		fi
	fi
fi

if [ "${AGENTBOX_READONLY:-}" = "1" ]; then
	echo "agentbox: workspace is mounted read-only (--ro)" >&2
fi

# First-run warm-up: the harness builds a cache in AGENTBOX_CACHE_DIR (e.g.
# pi's extension transpile cache), so the first run in a project is slower.
# Creating the directory here makes the notice fire exactly once per project
# even when the harness ends up not using the cache. --quiet suppresses the
# line via AGENTBOX_QUIET.
if [ -n "${AGENTBOX_CACHE_DIR:-}" ] && [ ! -d "$AGENTBOX_CACHE_DIR" ]; then
	if [ "${AGENTBOX_QUIET:-}" != "1" ]; then
		echo "agentbox: first run in this project: preparing the harness cache (later runs start faster)" >&2
	fi
	setpriv --reuid "$AGENTBOX_UID" --regid "$AGENTBOX_GID" --clear-groups -- \
		mkdir -p "$AGENTBOX_CACHE_DIR" 2>/dev/null || true
fi

exec setpriv --reuid "$AGENTBOX_UID" --regid "$AGENTBOX_GID" --clear-groups -- "$@"
