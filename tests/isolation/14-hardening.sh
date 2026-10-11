#!/usr/bin/env bash
# AC 17: docker inspect shows the hardening defaults.
source "$(dirname "$0")/lib.sh"
require_engine

WS="$(mkws)"
trap 'rm -rf "$WS"; cleanup_own_containers' EXIT

AGENTBOX_TEST_NAME=iso-hardening "$AB" "$WS" --agent shell -- -c 'sleep 20' >/dev/null 2>&1 &
bg=$!
sleep 2

inspect_field() {
  "$ENGINE" inspect --format "$1" iso-hardening 2>/dev/null
}

if [ -z "$(inspect_field '{{.Id}}')" ]; then
  echo "  ${RED}FAIL${RESET}: container iso-hardening not found for inspection"
  FAIL=$((FAIL + 1))
  kill_bg "$bg"
  finish
fi

check "AC17 CapDrop" '["ALL"]' "$(inspect_field '{{json .HostConfig.CapDrop}}')"
check "AC17 CapAdd" '["CAP_CHOWN","CAP_SETGID","CAP_SETUID"]' "$(inspect_field '{{json .HostConfig.CapAdd}}')"
check "AC17 no-new-privileges" '["no-new-privileges"]' "$(inspect_field '{{json .HostConfig.SecurityOpt}}')"
check "AC17 pids limit 512" "512" "$(inspect_field '{{.HostConfig.PidsLimit}}')"
check "AC17 memory 4g" "4294967296" "$(inspect_field '{{.HostConfig.Memory}}')"
check "AC17 cpus 2" "2000000000" "$(inspect_field '{{.HostConfig.NanoCpus}}')"
check "AC17 init true" "true" "$(inspect_field '{{.HostConfig.Init}}')"
check "AC17 tmpfs /tmp size 256m" '{"/tmp":"size=256m"}' "$(inspect_field '{{json .HostConfig.Tmpfs}}')"
check "AC17 network bridge" "bridge" "$(inspect_field '{{.HostConfig.NetworkMode}}')"
check "AC17 not privileged" "false" "$(inspect_field '{{.HostConfig.Privileged}}')"
check "AC17 no docker socket" "" "$(inspect_field '{{range .Mounts}}{{.Source}}{{"\n"}}{{end}}' | grep docker.sock || true)"
check "AC17 user is root at start (entrypoint drops)" "" "$(inspect_field '{{.Config.User}}')"

kill_bg "$bg"
check "AC17 no containers remain" "0" "$(count_own_containers)"

finish
