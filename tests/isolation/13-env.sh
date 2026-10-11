#!/usr/bin/env bash
# AC 13, 14, 15: env allowlist, no wholesale inheritance, dry-run redaction.
source "$(dirname "$0")/lib.sh"
require_engine

WS="$(mkws)"
trap 'rm -rf "$WS"; cleanup_own_containers' EXIT

# --- AC 13: poisoned host env is not inherited ------------------------------
export AWS_SECRET_ACCESS_KEY="poison-do-not-pass"
export SOME_RANDOM_VAR="poison-random"
out="$("$AB" "$WS" --agent shell -- -c 'env' 2>/dev/null)"
check_not_contains "AC13 AWS_SECRET_ACCESS_KEY not inherited" "AWS_SECRET_ACCESS_KEY" "$out"
check_not_contains "AC13 random host var not inherited" "SOME_RANDOM_VAR" "$out"

# --- AC 14 (mechanism): bare -e passes host value for the pi adapter --------
# The allowlist selection is unit-tested (internal/adapter); here we prove the
# bare `-e KEY` transport actually delivers a host value into the container.
export ANTHROPIC_API_KEY="sk-ant-test-fixture"
out="$("$AB" "$WS" --agent image:agentbox-base:dev --ephemeral \
  --env ANTHROPIC_API_KEY -- bash -c 'echo "key=$ANTHROPIC_API_KEY"' 2>/dev/null)"
check "AC14 bare -e delivers host value" "key=sk-ant-test-fixture" "$out"

# The pi adapter's allowlist must emit ANTHROPIC_API_KEY (dry-run proof).
dry="$(AGENTBOX_TEST_NAME=iso-env "$AB" "$WS" --dry-run 2>/dev/null)"
check_contains "AC14 pi allowlist emits ANTHROPIC_API_KEY" "ANTHROPIC_API_KEY" "$dry"
check_not_contains "AC14 dry-run never contains the value" "sk-ant-test-fixture" "$dry"

# claude adapter allowlist too.
dry_claude="$(AGENTBOX_TEST_NAME=iso-env "$AB" "$WS" --agent claude --dry-run 2>/dev/null)"
check_contains "AC14 claude allowlist emits ANTHROPIC_API_KEY" "ANTHROPIC_API_KEY" "$dry_claude"

# --- AC 15: dry-run redacts secrets -----------------------------------------
check_contains "AC15 dry-run renders <set>" "ANTHROPIC_API_KEY=<set>" "$dry"

unset ANTHROPIC_API_KEY
dry_unset="$(AGENTBOX_TEST_NAME=iso-env "$AB" "$WS" --dry-run 2>/dev/null)"
check_not_contains "AC15 unset allowlist var is omitted" "ANTHROPIC_API_KEY" "$dry_unset"

# --- --env for an unset key is skipped with a notice -------------------------
out="$("$AB" "$WS" --agent shell --env DEFINITELY_UNSET_KEY -- -c 'env' 2>&1)"
check_contains "--env unset key notice" "DEFINITELY_UNSET_KEY is not set" "$out"

finish
