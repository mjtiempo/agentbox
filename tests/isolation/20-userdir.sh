#!/usr/bin/env bash
# Persistent user folder: ~/.agentbox/agents/<harness> survives across
# sessions and projects while sessions stay per-project.
source "$(dirname "$0")/lib.sh"
require_engine

WS_A="$(mkws)"
WS_B="$(mkws)"
USERROOT="$(mktemp -d "${TMPDIR:-/tmp}/agentbox-user-XXXXXX")"
SRC="$(mktemp -d "${TMPDIR:-/tmp}/agentbox-src-XXXXXX")"
trap 'rm -rf "$WS_A" "$WS_B" "$USERROOT" "$SRC"; cleanup_own_containers; "$ENGINE" rm -f iso-userdir >/dev/null 2>&1 || true' EXIT

export AGENTBOX_USER_DIR="$USERROOT"

# --- dry-run shows the mounts, --no-user-config hides them ------------------
dry="$("$AB" "$WS_A" --dry-run 2>/dev/null)"
check_contains "dry-run mounts skills from the user folder" \
  "$USERROOT/agents/pi/skills:/state/pi/skills" "$dry"
check_contains "dry-run mounts extensions from the user folder" \
  "$USERROOT/agents/pi/extensions:/state/pi/extensions" "$dry"
check_contains "pi points TMPDIR at the persistent state cache" \
  "TMPDIR=/state/home/.cache/tmp" "$dry"
dry_oc="$("$AB" "$WS_A" --agent opencode --dry-run 2>/dev/null)"
check_contains "opencode points TMPDIR at the state cache (Bun needs exec)" \
  "TMPDIR=/state/home/.cache/tmp" "$dry_oc"
check_contains "opencode uses a private server (no background service)" \
  "--standalone" "$dry_oc"
check_contains "opencode keeps its state dirs on the state volume" \
  "XDG_STATE_HOME=/state/opencode/state" "$dry_oc"
oc_cred="$(mktemp "${TMPDIR:-/tmp}/agentbox-occred-XXXXXX.json")"
echo '{"list":[]}' >"$oc_cred"
dry_oc_cred="$("$AB" "$WS_A" --agent opencode --credentials "$oc_cred" --dry-run 2>/dev/null)"
check_contains "opencode credential mount target" \
  "$oc_cred:/state/opencode/credentials.json:ro" "$dry_oc_cred"
check_contains "opencode imports the mounted credential once per revision" \
  "AGENTBOX_CRED_IMPORT=auth import /state/opencode/credentials.json --standalone" "$dry_oc_cred"
rm -f "$oc_cred"
dry_off="$("$AB" "$WS_A" --no-user-config --dry-run 2>/dev/null)"
check_not_contains "--no-user-config disables the user folder" "/agents/pi/skills" "$dry_off"

# --- seed a skill in the persistent folder ----------------------------------
mkdir -p "$USERROOT/agents/pi/skills/probe"
echo "persistent-skill" >"$USERROOT/agents/pi/skills/probe/SKILL.md"

# --- live proof: run the pi TUI under a pty so the container stays alive ----
start_pi() { # start_pi <workspace>
  AB="$AB" python3 - "$1" >/dev/null 2>&1 <<'PY' &
import os, pty, sys, time
ab = os.environ["AB"]
ws = sys.argv[1]
pid, fd = pty.fork()
if pid == 0:
    os.execvp(ab, [ab, ws, "--agent", "pi", "--name", "iso-userdir"])
time.sleep(45)
PY
  echo $!
}

wait_running() {
  local i state
  for i in $(seq 1 40); do
    state="$("$ENGINE" inspect --format '{{.State.Running}}' iso-userdir 2>/dev/null || true)"
    [ "$state" = "true" ] && return 0
    sleep 0.5
  done
  return 1
}

pty_pid="$(start_pi "$WS_A")"
if wait_running; then
  got="$("$ENGINE" exec iso-userdir cat /state/pi/skills/probe/SKILL.md 2>/dev/null)"
  check "user-folder skill is visible in the container" "persistent-skill" "$got"

  # docker exec defaults to root, which has no CAP_DAC_OVERRIDE here; write as
  # the agent identity, like the harness itself does.
  "$ENGINE" exec -u "$(id -u):$(id -g)" iso-userdir sh -c 'echo persisted > /state/pi/skills/probe/out.txt' >/dev/null 2>&1
  check "container write lands in the host user folder" "persisted" \
    "$(cat "$USERROOT/agents/pi/skills/probe/out.txt" 2>/dev/null)"

  check "standard user dirs were pre-created" "yes" \
    "$([ -d "$USERROOT/agents/pi/extensions" ] && echo yes || echo no)"

  # sessions must stay in the per-project volume, not the user folder.
  "$ENGINE" exec iso-userdir sh -c 'test -d /state/pi/sessions' >/dev/null 2>&1
  check "sessions dir exists inside the container" "0" "$?"
  check "user folder has no sessions dir" "absent" \
    "$([ -e "$USERROOT/agents/pi/sessions" ] && echo present || echo absent)"
else
  check "pi container started for inspection" "running" "not-running"
fi
kill "$pty_pid" 2>/dev/null || true
"$ENGINE" rm -f iso-userdir >/dev/null 2>&1 || true

# --- second project: same user folder, different state volume ---------------
pty_pid="$(start_pi "$WS_B")"
if wait_running; then
  got="$("$ENGINE" exec iso-userdir cat /state/pi/skills/probe/SKILL.md 2>/dev/null)"
  check "user-folder skill persists into a second project" "persistent-skill" "$got"
  vol_a="$(volume_for "$WS_A")"
  vol_b="$(volume_for "$WS_B")"
  check "second project uses a different state volume" "different" \
    "$([ "$vol_a" != "$vol_b" ] && echo different || echo same)"
else
  check "pi container started for the second project" "running" "not-running"
fi
kill "$pty_pid" 2>/dev/null || true
"$ENGINE" rm -f iso-userdir >/dev/null 2>&1 || true

# --- agentbox config link|copy|reset -----------------------------------------
mkdir -p "$SRC/skills/linked" "$SRC/sessions"
echo "linked-skill" >"$SRC/skills/linked/SKILL.md"
echo "session-data" >"$SRC/sessions/s.jsonl"

# copy: skills are copied, sessions/auth are excluded
"$AB" config copy pi --from "$SRC" >/dev/null 2>&1
check "config copy copies skills" "linked-skill" \
  "$(cat "$USERROOT/agents/pi/skills/linked/SKILL.md" 2>/dev/null)"
check "config copy skips sessions" "absent" \
  "$([ -e "$USERROOT/agents/pi/sessions" ] && echo present || echo absent)"

"$AB" config reset pi --force >/dev/null 2>&1
check "config reset removes the copy" "absent" \
  "$([ -e "$USERROOT/agents/pi" ] && echo present || echo absent)"

# link: whole host config becomes a symlink; reset removes only the link
"$AB" config link pi --from "$SRC" >/dev/null 2>&1
check "config link creates a symlink" "$SRC" "$(readlink "$USERROOT/agents/pi" 2>/dev/null)"

dry_linked="$("$AB" "$WS_A" --dry-run 2>/dev/null)"
check_contains "linked config mounts skills" \
  "$USERROOT/agents/pi/skills:/state/pi/skills" "$dry_linked"
check_not_contains "linked config never mounts sessions" ":/state/pi/sessions" "$dry_linked"
check_not_contains "linked config never mounts auth" ":/state/pi/auth.json" "$dry_linked"

"$AB" config reset pi --force >/dev/null 2>&1
check "config reset removes the link" "absent" \
  "$([ -e "$USERROOT/agents/pi" ] && echo present || echo absent)"
check "config reset never touches the linked source" "yes" \
  "$([ -f "$SRC/skills/linked/SKILL.md" ] && echo yes || echo no)"

# --- first-run cache notice --------------------------------------------------
notice_run() { # notice_run <workspace>: run pi briefly under a pty, capture output
  AB="$AB" python3 - "$1" 2>/dev/null <<'PY'
import os, pty, select, signal, sys, time
ab = os.environ["AB"]
ws = sys.argv[1]
pid, fd = pty.fork()
if pid == 0:
    os.execvp(ab, [ab, ws, "--agent", "pi", "--name", "iso-notice"])
out = b""
end = time.time() + 5
while time.time() < end:
    r, _, _ = select.select([fd], [], [], 0.3)
    if r:
        try:
            data = os.read(fd, 65536)
        except OSError:
            break
        if not data:
            break
        out += data
try:
    os.kill(pid, signal.SIGKILL)
except ProcessLookupError:
    pass
sys.stdout.write(out.decode("utf-8", "replace"))
PY
  "$ENGINE" rm -f iso-notice >/dev/null 2>&1 || true
}
NOTICE_WS="$(mkws)"
trap 'rm -rf "$WS_A" "$WS_B" "$USERROOT" "$SRC" "$NOTICE_WS"; cleanup_own_containers; "$ENGINE" rm -f iso-userdir iso-notice >/dev/null 2>&1 || true' EXIT

# --- agentbox auth copy: adopted credentials are used automatically ---------
AUTH_HOME="$(mktemp -d "${TMPDIR:-/tmp}/agentbox-authhome-XXXXXX")"
AUTH_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/agentbox-authroot-XXXXXX")"
mkdir -p "$AUTH_HOME/.pi/agent"
echo '{"token":"fixture-not-a-real-key"}' >"$AUTH_HOME/.pi/agent/auth.json"
HOME="$AUTH_HOME" AGENTBOX_USER_DIR="$AUTH_ROOT" "$AB" auth copy pi >/dev/null 2>&1
check "auth copy stores the credential with 0600" "600" \
  "$(stat -c '%a' "$AUTH_ROOT/credentials/pi.json" 2>/dev/null || stat -f '%Lp' "$AUTH_ROOT/credentials/pi.json" 2>/dev/null)"
dry_auth="$(HOME="$AUTH_HOME" AGENTBOX_USER_DIR="$AUTH_ROOT" "$AB" "$WS_A" --agent pi --dry-run 2>/dev/null)"
check_contains "adopted credential is mounted automatically" \
  "$AUTH_ROOT/credentials/pi.json:/state/pi/auth.json:ro" "$dry_auth"
detect="$(HOME="$AUTH_HOME" AGENTBOX_USER_DIR="$AUTH_ROOT" "$AB" auth pi 2>&1)"
check_contains "auth reports the adopted credential" "agentbox credential store" "$detect"
HOME="$AUTH_HOME" AGENTBOX_USER_DIR="$AUTH_ROOT" "$AB" auth reset pi --force >/dev/null 2>&1
check "auth reset removes the stored credential" "absent" \
  "$([ -e "$AUTH_ROOT/credentials/pi.json" ] && echo present || echo absent)"
rm -rf "$AUTH_HOME" "$AUTH_ROOT"

# --- quickstart: host config + credentials in one step -----------------------
QS_HOME="$(mktemp -d "${TMPDIR:-/tmp}/agentbox-qshome-XXXXXX")"
QS_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/agentbox-qsroot-XXXXXX")"
mkdir -p "$QS_HOME/.pi/agent/skills/probe"
echo "quickstart-skill" >"$QS_HOME/.pi/agent/skills/probe/SKILL.md"
echo '{"token":"fixture-not-a-real-key"}' >"$QS_HOME/.pi/agent/auth.json"
qs_out="$(HOME="$QS_HOME" AGENTBOX_USER_DIR="$QS_ROOT" "$AB" quickstart pi 2>&1)"
check_contains "quickstart copies the host config" "config: copied" "$qs_out"
check_contains "quickstart adopts host credentials" "auth: adopted credentials from" "$qs_out"
check "quickstart stored the credential with 0600" "600" \
  "$(stat -c '%a' "$QS_ROOT/credentials/pi.json" 2>/dev/null || stat -f '%Lp' "$QS_ROOT/credentials/pi.json" 2>/dev/null)"
check "quickstart copied the skill into the user folder" "quickstart-skill" \
  "$(cat "$QS_ROOT/agents/pi/skills/probe/SKILL.md" 2>/dev/null)"
rm -rf "$QS_HOME" "$QS_ROOT"

out_first="$(notice_run "$NOTICE_WS")"
check_contains "first run announces cache preparation" "preparing the harness cache" "$out_first"
out_second="$(notice_run "$NOTICE_WS")"
check_not_contains "second run has no cache notice" "preparing the harness cache" "$out_second"

finish
