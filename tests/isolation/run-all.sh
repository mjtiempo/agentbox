#!/usr/bin/env bash
# Runs every isolation acceptance script in order and reports a summary.
# Usage: bash tests/isolation/run-all.sh   (or: make test-isolation)
#
# Environment:
#   AGENTBOX_REQUIRE_ENGINE=1  fail instead of skipping when no engine is
#                              reachable (CI sets this)
#   AGENTBOX_ENGINE=docker|podman  engine under test (default docker)
set -u

cd "$(dirname "$0")"

failures=0
ran=0
skipped=0
for f in $(ls [0-9][0-9]-*.sh 2>/dev/null | sort); do
  echo ""
  echo "=== $f ==="
  out="$(bash "$f" 2>&1)"
  code=$?
  printf '%s\n' "$out"
  if [ $code -eq 0 ]; then
    if printf '%s' "$out" | grep -q "SKIP:"; then
      skipped=$((skipped + 1))
    else
      ran=$((ran + 1))
    fi
  else
    failures=$((failures + 1))
    echo "--- FAILED: $f"
  fi
done

echo ""
echo "=============================================="
echo "isolation suite: $ran passed, $skipped skipped, $failures failed"
if [ "$failures" -gt 0 ]; then
  exit 1
fi
if [ "${AGENTBOX_REQUIRE_ENGINE:-0}" = "1" ] && [ "$ran" -eq 0 ]; then
  echo "FATAL: AGENTBOX_REQUIRE_ENGINE=1 but every script skipped"
  exit 1
fi
exit 0
