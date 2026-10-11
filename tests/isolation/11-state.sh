#!/usr/bin/env bash
# AC 10, 11: per-project state volumes; --ephemeral leaves no state.
source "$(dirname "$0")/lib.sh"
require_engine

WS_A="$(mkws)"
WS_B="$(mkws)"
WS_C="$(mkws)"
trap 'rm -rf "$WS_A" "$WS_B" "$WS_C"; cleanup_own_containers' EXIT

VOL_A="$(volume_for "$WS_A")"
VOL_B="$(volume_for "$WS_B")"

# --- AC 10: distinct volumes, no bleed --------------------------------------
"$AB" "$WS_A" --agent shell -- -c 'mkdir -p /state/probe && echo session-a > /state/probe/session' >/dev/null 2>&1
"$AB" "$WS_B" --agent shell -- -c 'test -e /state/probe && echo leaked || echo clean' >/dev/null 2>&1
out_b="$("$AB" "$WS_B" --agent shell -- -c 'test -e /state/probe && echo leaked || echo clean' 2>/dev/null)"
check "AC10 project B cannot see A's state" "clean" "$out_b"

out_a="$("$AB" "$WS_A" --agent shell -- -c 'cat /state/probe/session' 2>/dev/null)"
check "AC10 project A state persists" "session-a" "$out_a"

vols="$("$ENGINE" volume ls --filter "label=agentbox.uid=$(id -u)" --format '{{.Name}}')"
check_contains "AC10 volume A exists" "$VOL_A" "$vols"
check_contains "AC10 volume B exists" "$VOL_B" "$vols"
if [ "$VOL_A" = "$VOL_B" ]; then
  check "AC10 volumes differ" "differ" "same"
else
  check "AC10 volumes differ" "differ" "differ"
fi

# --- AC 10b: container has no shared mount to the other project -------------
out="$("$AB" "$WS_A" --agent shell -- -c 'mount | grep -c " on /state " || true' 2>/dev/null)"
check "AC10 exactly one /state mount" "1" "$out"

# --- AC 11: --ephemeral creates no volume ----------------------------------
"$AB" "$WS_C" --ephemeral --agent shell -- -c 'test -d /state && echo state-dir-present || echo no-state' >/dev/null 2>&1
vols_c="$("$ENGINE" volume ls --filter "label=agentbox.path=$WS_C" --format '{{.Name}}')"
check "AC11 ephemeral creates no volume" "" "$vols_c"
out="$("$AB" "$WS_C" --ephemeral --agent shell -- -c 'mount | grep -c " on /state " || true' 2>/dev/null)"
check "AC11 ephemeral /state is not a mount" "0" "$out"

# --- FR-5: concurrent-run warning -------------------------------------------
# Same workspace, no TEST_NAME: the project label is deterministic, so the
# second run sees the first one's live container.
"$AB" "$WS_A" --agent shell -- -c 'sleep 3' >/dev/null 2>&1 &
bg=$!
sleep 1
out="$("$AB" "$WS_A" --agent shell -- -c 'true' 2>&1)"
kill_bg "$bg"
check_contains "FR-5 concurrent run warning" "concurrent run" "$out"

finish
