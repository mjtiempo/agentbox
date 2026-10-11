#!/usr/bin/env bash
# Shared helpers for the isolation acceptance suite (PRD §9).
# Every script sources this file, calls require_engine, runs checks, finish.
set -u

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
AB="${AB:-$ROOT/agentbox}"
PASS=0
FAIL=0
ENGINE="${AGENTBOX_ENGINE:-docker}"

if [ -t 1 ]; then
  RED=$'\033[31m'; GREEN=$'\033[32m'; YELLOW=$'\033[33m'; RESET=$'\033[0m'
else
  RED=""; GREEN=""; YELLOW=""; RESET=""
fi

require_engine() {
  if ! command -v "$ENGINE" >/dev/null 2>&1; then
    if [ "${AGENTBOX_REQUIRE_ENGINE:-0}" = "1" ]; then
      echo "${RED}FATAL${RESET}: engine '$ENGINE' not found and AGENTBOX_REQUIRE_ENGINE=1"
      exit 1
    fi
    echo "${YELLOW}SKIP${RESET}: engine '$ENGINE' not installed"
    exit 0
  fi
  if ! "$ENGINE" info >/dev/null 2>&1; then
    if [ "${AGENTBOX_REQUIRE_ENGINE:-0}" = "1" ]; then
      echo "${RED}FATAL${RESET}: engine '$ENGINE' unreachable and AGENTBOX_REQUIRE_ENGINE=1"
      exit 1
    fi
    echo "${YELLOW}SKIP${RESET}: engine '$ENGINE' unreachable (is the daemon running?)"
    exit 0
  fi
  if [ ! -x "$AB" ]; then
    echo "${RED}FATAL${RESET}: agentbox binary not found at $AB (run make build)"
    exit 1
  fi
}

check() { # check <name> <expected> <actual>
  local name="$1" want="$2" got="$3"
  if [ "$want" = "$got" ]; then
    echo "  ${GREEN}ok${RESET}: $name"
    PASS=$((PASS + 1))
  else
    echo "  ${RED}FAIL${RESET}: $name: want [$want] got [$got]"
    FAIL=$((FAIL + 1))
  fi
}

check_contains() { # check_contains <name> <needle> <haystack>
  local name="$1" needle="$2" haystack="$3"
  if printf '%s' "$haystack" | grep -qF -- "$needle"; then
    echo "  ${GREEN}ok${RESET}: $name"
    PASS=$((PASS + 1))
  else
    echo "  ${RED}FAIL${RESET}: $name: missing [$needle] in [$haystack]"
    FAIL=$((FAIL + 1))
  fi
}

check_not_contains() { # check_not_contains <name> <needle> <haystack>
  local name="$1" needle="$2" haystack="$3"
  if printf '%s' "$haystack" | grep -qF -- "$needle"; then
    echo "  ${RED}FAIL${RESET}: $name: found [$needle] in [$haystack]"
    FAIL=$((FAIL + 1))
  else
    echo "  ${GREEN}ok${RESET}: $name"
    PASS=$((PASS + 1))
  fi
}

finish() {
  if [ "$FAIL" -gt 0 ]; then
    echo "${RED}FAILED${RESET} ($FAIL failure(s), $PASS pass(es))"
    exit 1
  fi
  echo "${GREEN}PASSED${RESET} ($PASS check(s))"
  exit 0
}

# mkws creates a canonical temp workspace.
mkws() {
  local d
  d="$(mktemp -d "${TMPDIR:-/tmp}/agentbox-ws-XXXXXX")"
  (cd "$d" && pwd -P)
}

# volume_for prints the FR-5 volume name for a canonical workspace path.
volume_for() {
  local ws="$1" uid
  uid="$(id -u)"
  printf 'agentbox-state-%s-%s' "$uid" "$(printf '%s' "$ws" | sha256sum | cut -c1-8)"
}

# count_own_containers prints the number of agentbox containers for this uid.
count_own_containers() {
  "$ENGINE" ps -a --filter "label=agentbox.uid=$(id -u)" --format '{{.ID}}' | grep -c . || true
}

# cleanup_own_containers force-removes leftover test containers.
cleanup_own_containers() {
  local ids
  ids="$("$ENGINE" ps -aq --filter "label=agentbox.uid=$(id -u)" 2>/dev/null || true)"
  if [ -n "$ids" ]; then
    # shellcheck disable=SC2086
    "$ENGINE" rm -f $ids >/dev/null 2>&1 || true
  fi
}

# kill_bg terminates a background agentbox and waits for it.
kill_bg() {
  local pid="$1" sig="${2:-TERM}"
  kill -"$sig" "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
}
