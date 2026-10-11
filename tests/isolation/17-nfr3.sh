#!/usr/bin/env bash
# NFR-3: zero-write guarantee for host paths outside workspace + state.
#
# Manifest scope (documented): $HOME top level (engine-owned ~/.docker and the
# agentbox user folder ~/.agentbox excluded), /etc top level, /var/run top
# level (engine-owned docker.sock excluded), and the FR-2 sensitive paths.
# /var/lib/docker and the named volume directory are engine-owned and excluded
# by design. ~/.agentbox/agents/<harness> is an intentional, user-controlled
# writable surface (persistent harness plugins/skills); disable it with
# --no-user-config to keep the original zero-write profile.
source "$(dirname "$0")/lib.sh"
require_engine

WS="$(mkws)"
trap 'rm -rf "$WS"; cleanup_own_containers' EXIT

snapshot() {
  {
    for p in "$HOME" /etc /var/run; do
      [ -d "$p" ] || continue
      find "$p" -maxdepth 1 -mindepth 1 \
        ! -name '.docker' ! -name '.agentbox' ! -name 'docker.sock' \
        -printf '%p|%s|%T@|%U|%G\n' 2>/dev/null
    done
    for p in "$HOME/.ssh" "$HOME/.aws" "$HOME/.config" "$HOME/.gnupg"; do
      [ -d "$p" ] || continue
      find "$p" -maxdepth 1 -mindepth 1 -printf '%p|%s|%T@|%U|%G\n' 2>/dev/null
    done
  } | sort
}

before="$(snapshot)"
"$AB" "$WS" --agent shell -- -c 'echo nfr3 > /workspace/nfr3-probe' >/dev/null 2>&1
after="$(snapshot)"

diff_out="$(diff <(printf '%s\n' "$before") <(printf '%s\n' "$after") || true)"
if [ -z "$diff_out" ]; then
  check "NFR-3 no writes outside workspace+state" "clean" "clean"
else
  echo "  ${RED}FAIL${RESET}: NFR-3 manifest changed outside workspace+state:"
  printf '%s\n' "$diff_out" | sed 's/^/    /'
  check "NFR-3 no writes outside workspace+state" "clean" "dirty"
fi

# The workspace itself is allowed to change; state volume is allowed too.
check "NFR-3 workspace probe written" "yes" "$([ -f "$WS/nfr3-probe" ] && echo yes || echo no)"

finish
