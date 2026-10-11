#!/usr/bin/env bash
# AC 8: guardrail refusal from an ancestor of $HOME, with --force override.
source "$(dirname "$0")/lib.sh"
require_engine

WS="$(mkws)"
trap 'rm -rf "$WS"; cleanup_own_containers' EXIT

# Run from an ancestor of $HOME (no workspace argument => $PWD).
ancestor="$(dirname "$HOME")"
out="$(cd "$ancestor" && "$AB" 2>&1)"
code=$?
if [ "$code" -eq 0 ]; then
  check "AC8 ancestor of HOME refused" "nonzero" "0"
else
  check "AC8 ancestor of HOME refused" "nonzero" "nonzero"
fi
check_contains "AC8 guardrail message" "refusing to mount" "$out"
check_contains "AC8 guardrail remedy" "--force" "$out"

# $HOME itself is refused.
out="$(cd "$HOME" && "$AB" 2>&1)"
code=$?
if [ "$code" -eq 0 ]; then
  check "AC8 HOME refused" "nonzero" "0"
else
  check "AC8 HOME refused" "nonzero" "nonzero"
fi
check_contains "AC8 HOME message" "your home directory" "$out"

# / is refused.
out="$(cd / && "$AB" 2>&1)"
code=$?
if [ "$code" -eq 0 ]; then
  check "AC8 / refused" "nonzero" "0"
else
  check "AC8 / refused" "nonzero" "nonzero"
fi

# --force is echoed in the banner and dry-run.
out="$(cd "$HOME" && AGENTBOX_TEST_NAME=iso-force "$AB" --dry-run --force 2>&1)"
check_contains "AC8 --force echo" "--force: workspace guardrail overridden" "$out"

# Normal project still works.
out="$("$AB" "$WS" --agent shell -- -c 'true' 2>&1; echo "rc=$?")"
check_contains "AC8 normal project runs" "rc=0" "$out"

finish
