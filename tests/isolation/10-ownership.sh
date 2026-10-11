#!/usr/bin/env bash
# AC 5, 6: workspace files are host-owned; non-root writes outside fail.
source "$(dirname "$0")/lib.sh"
require_engine

WS="$(mkws)"
trap 'rm -rf "$WS"; cleanup_own_containers' EXIT

# --- AC 5: workspace writes are host-owned ---------------------------------
"$AB" "$WS" --agent shell -- -c 'echo x > /workspace/from-agent' >/dev/null 2>&1
check "AC5 file exists on host" "yes" "$([ -f "$WS/from-agent" ] && echo yes || echo no)"
owner="$(stat -c '%u:%g' "$WS/from-agent" 2>/dev/null || stat -f '%u:%g' "$WS/from-agent")"
check "AC5 file owned by host uid:gid" "$(id -u):$(id -g)" "$owner"

# --- AC 6: non-root cannot write /etc --------------------------------------
out="$("$AB" "$WS" --agent shell -- -c 'echo x > /etc/agentbox-test' 2>&1)"
check_contains "AC6 /etc write denied" "Permission denied" "$out"
check "AC6 nothing created in /etc" "absent" "$([ -e /etc/agentbox-test ] && echo present || echo absent)"

# --- entrypoint identity (FR-9): HOME and uid inside ------------------------
out="$("$AB" "$WS" --agent shell -- -c 'echo "$(id -u):$(id -g):$HOME"' 2>/dev/null)"
check "FR-9 container identity" "$(id -u):$(id -g):/state/home" "$out"

finish
