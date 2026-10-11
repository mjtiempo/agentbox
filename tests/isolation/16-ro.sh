#!/usr/bin/env bash
# AC 7: --ro denies workspace writes and shows :ro in --dry-run.
source "$(dirname "$0")/lib.sh"
require_engine

WS="$(mkws)"
trap 'rm -rf "$WS"; cleanup_own_containers' EXIT

dry="$(AGENTBOX_TEST_NAME=iso-ro "$AB" "$WS" --ro --dry-run 2>/dev/null)"
check_contains "AC7 dry-run shows :ro" ":/workspace:ro" "$dry"

out="$("$AB" "$WS" --ro --agent shell -- -c 'echo x > /workspace/denied' 2>&1)"
check_contains "AC7 write denied" "Read-only file system" "$out"
check "AC7 nothing written to host" "absent" "$([ -e "$WS/denied" ] && echo present || echo absent)"

# Read still works, and the entrypoint surfaces the marker.
out="$("$AB" "$WS" --ro --agent shell -- -c 'echo "$AGENTBOX_READONLY"' 2>/dev/null)"
check "AC7 AGENTBOX_READONLY marker" "1" "$out"

finish
