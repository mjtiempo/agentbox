#!/usr/bin/env bash
# AC 1–4, 12: workspace boundary, home/ssh/socket invisibility, exit codes.
source "$(dirname "$0")/lib.sh"
require_engine

WS="$(mkws)"
trap 'rm -rf "$WS"; cleanup_own_containers' EXIT
echo "fixture" >"$WS/file.txt"

# --- AC 1: pwd is /workspace -----------------------------------------------
out="$("$AB" --agent shell -- -c 'pwd' 2>/dev/null)"
check "AC1 pwd is /workspace" "/workspace" "$out"

# --- AC 2: no host home contents -------------------------------------------
out="$("$AB" --agent shell -- -c 'ls -A /home' 2>/dev/null)"
check_not_contains "AC2 host home not visible" "$(id -un)" "$out"

# --- AC 3: no ssh keys anywhere visible ------------------------------------
out="$("$AB" --agent shell -- -c 'test -e "$HOME/.ssh/id_rsa" && echo present || echo absent; test -d /home/*/.ssh 2>/dev/null && echo home-ssh-present || echo home-ssh-absent' 2>/dev/null)"
check_contains "AC3 no id_rsa in HOME" "absent" "$out"
check_contains "AC3 no /home/*/.ssh" "home-ssh-absent" "$out"
check_not_contains "AC3 no private key material" "PRIVATE KEY" "$out"

# --- AC 4: no docker socket -------------------------------------------------
out="$("$AB" --agent shell -- -c 'test -e /var/run/docker.sock && echo present || echo absent' 2>/dev/null)"
check "AC4 docker socket absent" "absent" "$out"

# --- AC 12: exit code passthrough ------------------------------------------
"$AB" --agent shell -- -c 'exit 7' >/dev/null 2>&1
check "AC12 exit 7 propagates" "7" "$?"

# --- lifecycle: no containers remain after a normal run --------------------
check "no containers remain after normal run" "0" "$(count_own_containers)"

# --- AC 21 dry-run determinism (stdout only) -------------------------------
a="$(AGENTBOX_TEST_NAME=iso-golden "$AB" --dry-run 2>/dev/null)"
b="$(AGENTBOX_TEST_NAME=iso-golden "$AB" --dry-run 2>/dev/null)"
check "AC21 dry-run stable across runs" "$a" "$b"
check_contains "AC21 dry-run pins the container name" "iso-golden" "$a"
check_contains "AC21 dry-run pins the run uuid" "agentbox-test-run" "$a"
check_contains "AC21 dry-run pins the owner label" "1@agentbox-test" "$a"
check_not_contains "AC21 non-TTY dry-run omits -it" "-it" "$a"

# --- TTY detection regression: -it must be passed under a real pty ----------
# DefaultHost once left IsTTY false, so TUI harnesses exited silently.
if command -v python3 >/dev/null 2>&1; then
  dry_pty="$(AB="$AB" python3 - 2>/dev/null <<'PY'
import os, pty, select, signal, sys, time
ab = os.environ["AB"]
pid, fd = pty.fork()
if pid == 0:
    os.environ["AGENTBOX_TEST_NAME"] = "iso-tty"
    # Set TERM explicitly so the passthrough assertion does not depend on the
    # CI environment (a terminal without TERM legitimately emits nothing).
    os.environ["TERM"] = "xterm-256color"
    os.execvp(ab, [ab, "--dry-run", "--quiet"])
out = b""
deadline = time.time() + 5
while time.time() < deadline:
    r, _, _ = select.select([fd], [], [], 0.3)
    if r:
        try:
            data = os.read(fd, 65536)
        except OSError:
            break
        if not data:
            break
        out += data
    if os.waitpid(pid, os.WNOHANG)[0] != 0:
        break
try:
    os.kill(pid, signal.SIGKILL)
except ProcessLookupError:
    pass
sys.stdout.write(out.decode("utf-8", "replace"))
PY
)"
  check_contains "TTY run passes -it" "-it" "$dry_pty"
  check_contains "TTY run passes TERM" "TERM" "$dry_pty"
else
  echo "  ${YELLOW}skip${RESET}: python3 not available for the pty TTY check"
fi

finish
