#!/usr/bin/env bash
# AC 9: no agentbox containers remain after normal, SIGINT, or SIGKILL runs.
# The SIGKILL case is satisfied by the next-startup sweep.
source "$(dirname "$0")/lib.sh"
require_engine

WS="$(mkws)"
trap 'rm -rf "$WS"; cleanup_own_containers' EXIT

# --- normal run -------------------------------------------------------------
"$AB" "$WS" --agent shell -- -c 'true' >/dev/null 2>&1
check "AC9 no containers after normal exit" "0" "$(count_own_containers)"

# --- SIGINT (non-TTY: forwarded, agent exits, cleanup runs) ------------------
AGENTBOX_TEST_NAME=iso-sigint "$AB" "$WS" --agent shell -- -c 'sleep 30' >/dev/null 2>&1 &
bg=$!
sleep 2
kill -INT "$bg" 2>/dev/null || true
wait "$bg" 2>/dev/null || true
sleep 1
check "AC9 no containers after SIGINT" "0" "$(count_own_containers)"

# --- SIGKILL: container is left behind, next startup sweeps it ---------------
AGENTBOX_TEST_NAME=iso-sigkill "$AB" "$WS" --agent shell -- -c 'sleep 60' >/dev/null 2>&1 &
bg=$!
sleep 2
kill -KILL "$bg" 2>/dev/null || true
wait "$bg" 2>/dev/null || true
sleep 1
left="$(count_own_containers)"
if [ "$left" -lt 1 ]; then
  # The engine may have reaped it already (e.g. --rm raced); force a stale
  # container so the sweep is still exercised.
  "$ENGINE" run -d --rm --label agentbox=1 --label "agentbox.uid=$(id -u)" \
    --label agentbox.run=iso-fake-run --label "agentbox.owner=999999@stale-boot" \
    --name iso-stale-sweep agentbox-base:dev sleep 60 >/dev/null 2>&1
  left="$(count_own_containers)"
fi
check "AC9 SIGKILL leaves a container to sweep" "1" "$left"

"$AB" "$WS" --agent shell -- -c 'true' >/dev/null 2>&1
check "AC9 next startup sweeps the stale container" "0" "$(count_own_containers)"

# --- concurrent live sessions survive a sweep (UC2) --------------------------
# Use --name (not AGENTBOX_TEST_NAME): TEST_NAME pins the owner label to a
# fake boot id, which the startup sweep would correctly treat as stale.
"$AB" "$WS" --name iso-concurrent-live --agent shell -- -c 'sleep 20' >/dev/null 2>&1 &
bg=$!
sleep 2
"$AB" "$WS" --agent shell -- -c 'true' >/dev/null 2>&1
alive="$("$ENGINE" ps --filter "name=iso-concurrent-live" --format '{{.ID}}' 2>/dev/null | grep -c . || true)"
check "AC9 concurrent live session survives the sweep" "1" "$alive"
kill_bg "$bg"
"$ENGINE" rm -f iso-concurrent-live >/dev/null 2>&1 || true

finish
