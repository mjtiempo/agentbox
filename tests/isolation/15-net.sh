#!/usr/bin/env bash
# AC 18: --net none cannot resolve or reach the network.
source "$(dirname "$0")/lib.sh"
require_engine

WS="$(mkws)"
trap 'rm -rf "$WS"; cleanup_own_containers' EXIT

out="$("$AB" "$WS" --net none --agent shell -- -c 'getent hosts example.com || echo "no-dns"' 2>/dev/null)"
check_contains "AC18 --net none: no DNS" "no-dns" "$out"

out="$("$AB" "$WS" --net none --agent shell -- -c 'curl -s -m 3 https://example.com >/dev/null 2>&1 && echo connected || echo blocked' 2>/dev/null)"
check_contains "AC18 --net none: no egress" "blocked" "$out"

# --net host warns (FR-7).
out="$("$AB" "$WS" --net host --agent shell -- -c 'true' 2>&1)"
check_contains "FR-7 --net host warning" "host network" "$out"

finish
