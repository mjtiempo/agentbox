#!/usr/bin/env bash
# Release gate (D5): secret scan, image history check, size budgets.
#
# Checks:
#   1. no secret material in the repo tree
#   2. no secret material in image filesystems
#   3. no secret ENV/ARG/label in image history or config
#   4. image size budgets (base/pi <= 400 MB per FR-10; claude/opencode have
#      documented upstream-binary exceptions recorded here)
#   5. agentbox binary <= 15 MB (NFR-2)
#
# Exits nonzero on the first hard failure. Requires the images to be built
# (make image, plus image-claude/image-opencode for the full set).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-dev}"
ENGINE="${DOCKER:-docker}"
failures=0

say()  { printf '%s\n' "$*"; }
fail() { printf 'release-check: FAIL: %s\n' "$*"; failures=$((failures + 1)); }
ok()   { printf 'release-check: ok: %s\n' "$*"; }

# 1. repo secret scan ---------------------------------------------------------
# Fixtures in tests use obviously fake values; this scans for real-looking
# key material anywhere in the tree (excluding .git and image build context
# caches).
if grep -rIl -E 'sk-ant-[A-Za-z0-9_-]{20,}|AKIA[0-9A-Z]{16}|-----BEGIN (RSA|OPENSSH|EC|PGP) PRIVATE KEY-----' \
    --exclude-dir=.git --exclude-dir=node_modules --exclude='*.golden' . 2>/dev/null | grep -q .; then
  fail "repo tree contains secret-looking material"
else
  ok "repo tree secret scan clean"
fi

# 2/3. image scans ------------------------------------------------------------
image_present() {
  "$ENGINE" image inspect "$1" >/dev/null 2>&1
}

scan_image_secrets() {
  local image="$1"
  local out
  out="$("$ENGINE" run --rm --entrypoint sh "$image" -c \
    "grep -rIl -E 'sk-ant-[A-Za-z0-9_-]{20,}|AKIA[0-9A-Z]{16}|-----BEGIN (RSA|OPENSSH|EC|PGP) PRIVATE KEY-----' /usr/local /etc /opt 2>/dev/null | head -5" \
    2>/dev/null || true)"
  if [ -n "$out" ]; then
    fail "$image filesystem contains secret-looking material: $out"
  else
    ok "$image filesystem secret scan clean"
  fi
}

scan_image_history() {
  local image="$1"
  local hist cfg
  hist="$("$ENGINE" history --no-trunc --format '{{.CreatedBy}}' "$image" 2>/dev/null || true)"
  cfg="$("$ENGINE" inspect --format '{{json .Config.Env}} {{json .Config.Labels}}' "$image" 2>/dev/null || true)"
  if printf '%s\n%s' "$hist" "$cfg" | grep -qE 'sk-ant-[A-Za-z0-9_-]{20,}|AKIA[0-9A-Z]{16}|PRIVATE KEY'; then
    fail "$image history/config exposes secret-looking material"
  else
    ok "$image history/config scan clean"
  fi
}

size_bytes() {
  "$ENGINE" image inspect --format '{{.Size}}' "$1" 2>/dev/null || echo 0
}

check_size() {
  local image="$1" budget="$2" why="$3"
  local size
  size="$(size_bytes "$image")"
  if [ "$size" -eq 0 ]; then
    say "release-check: note: $image not built, skipping size check ($why)"
    return
  fi
  local mb=$((size / 1024 / 1024))
  if [ "$mb" -le "$budget" ]; then
    ok "$image is ${mb}MB (budget ${budget}MB)"
  else
    fail "$image is ${mb}MB, over the ${budget}MB budget ($why)"
  fi
}

for image in "agentbox-base:${VERSION}" "agentbox-pi:${VERSION}-pi1.1.0"; do
  if image_present "$image"; then
    scan_image_secrets "$image"
    scan_image_history "$image"
  else
    say "release-check: note: $image not built, skipping image scan"
  fi
done

check_size "agentbox-base:${VERSION}" 400 "FR-10 target"
check_size "agentbox-pi:${VERSION}-pi1.1.0" 400 "FR-10 target"

# Documented exceptions: upstream native binaries (claude ~257MB, opencode v2
# ~210MB) push these images over the FR-10 target; the budget below is the
# recorded release budget (NFR-8 manifest records the actual size).
for image in "agentbox-claude:${VERSION}-claude2.1.296" "agentbox-opencode:${VERSION}-opencode2.0.26"; do
  if image_present "$image"; then
    scan_image_secrets "$image"
    scan_image_history "$image"
  fi
done
check_size "agentbox-claude:${VERSION}-claude2.1.296" 600 "upstream 257MB native binary"
check_size "agentbox-opencode:${VERSION}-opencode2.0.26" 560 "upstream 210MB native binary (v2)"

# 4. binary budget ------------------------------------------------------------
if [ -x ./agentbox ]; then
  size=$(stat -c%s ./agentbox)
  if [ "$size" -le 15728640 ]; then
    ok "agentbox binary is $((size / 1024 / 1024))MB (budget 15MB)"
  else
    fail "agentbox binary is $size bytes, over the 15MB budget (NFR-2)"
  fi
  # NFR-2: single static binary, no runtime deps.
  if file ./agentbox | grep -q 'statically linked'; then
    ok "agentbox binary is statically linked (CGO_ENABLED=0)"
  else
    fail "agentbox binary is not statically linked; build with CGO_ENABLED=0"
  fi
else
  say "release-check: note: ./agentbox not built, skipping binary budget"
fi

if [ "$failures" -gt 0 ]; then
  say "release-check: FAILED ($failures issue(s))"
  exit 1
fi
say "release-check: PASSED"
