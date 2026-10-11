#!/usr/bin/env bash
# FR-4 P1 / FR-9 P1 / FR-5 P1: image:<ref>, --user, --state-dir, --credentials.
source "$(dirname "$0")/lib.sh"
require_engine

WS="$(mkws)"
WS_USER="$(mkws)"
SD="$(mktemp -d "${TMPDIR:-/tmp}/agentbox-statedir-XXXXXX")"
CRED="$(mktemp "${TMPDIR:-/tmp}/agentbox-cred-XXXXXX.json")"
echo '{"token":"fixture-secret-value"}' >"$CRED"
trap 'rm -rf "$WS" "$WS_USER" "$SD" "$CRED"; cleanup_own_containers' EXIT

# --- image:<ref> runs the image's own entrypoint under --user ----------------
"$AB" "$WS" --agent image:agentbox-base:dev --ephemeral -- sh -c 'echo hi > /workspace/imageref-file' >/dev/null 2>&1
check "image:<ref> file exists on host" "yes" "$([ -f "$WS/imageref-file" ] && echo yes || echo no)"
if [ -f "$WS/imageref-file" ]; then
  owner="$(stat -c '%u:%g' "$WS/imageref-file" 2>/dev/null || stat -f '%u:%g' "$WS/imageref-file")"
  check "image:<ref> file is host-owned" "$(id -u):$(id -g)" "$owner"
fi

# --- image:<ref> requires --ephemeral or a writable --state-dir --------------
out="$("$AB" "$WS" --agent image:agentbox-base:dev -- sh -c true 2>&1)"
code=$?
check "image:<ref> without state fails up front" "2" "$code"
check_contains "image:<ref> remedy" "--ephemeral" "$out"
check_contains "image:<ref> remedy mentions --state-dir" "--state-dir" "$out"

# --- --user with a fresh named volume fails up front (FR-9 P1) ---------------
out="$("$AB" "$WS_USER" --user "$(id -u):$(id -g)" --agent shell -- -c true 2>&1)"
code=$?
check "--user fresh-volume failure" "2" "$code"
check_contains "--user fresh-volume remedy" "--ephemeral" "$out"

# --- --state-dir bind mount works --------------------------------------------
"$AB" "$WS" --state-dir "$SD" --agent shell -- -c 'echo state > /state/probe' >/dev/null 2>&1
check "--state-dir run succeeds" "0" "$?"
check "--state-dir file created" "yes" "$([ -f "$SD/probe" ] && echo yes || echo no)"
check "--state-dir content" "state" "$(cat "$SD/probe" 2>/dev/null)"

# --- --credentials dry-run shows the :ro target and never the value ----------
dry="$(AGENTBOX_TEST_NAME=iso-cred "$AB" "$WS" --agent pi --credentials "$CRED" --dry-run 2>/dev/null)"
check_contains "credentials mount is :ro at adapter target" "$CRED:/state/pi/auth.json:ro" "$dry"
check_not_contains "credential value never appears" "fixture-secret-value" "$dry"

finish
