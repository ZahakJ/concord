#!/usr/bin/env bash
# INDEPENDENT VERIFICATION of the guest system (lock/knock/admit door, guest
# theater UI, chosen link lifetimes). Same shape as scripts/smoke-guest.sh —
# real rendezvous, real member app, real Chromium on both sides — but it drives
# the LOCKED path and inspects the guest's WebSocket traffic directly, because
# the question that matters is what an unadmitted stranger RECEIVES, and the UI
# is not evidence of that.
#
# Its own port block (8981-8990) so it can run alongside the other harnesses.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

port_free() { ! (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }
PORTS=()
for p in $(seq 8981 8990); do
  port_free "$p" && PORTS+=("$p")
  [ "${#PORTS[@]}" -ge 3 ] && break
done
[ "${#PORTS[@]}" -ge 3 ] || { echo "FAIL: no free ports in 8981-8990" >&2; exit 1; }
RDV_P2P="${PORTS[0]}" GATEWAY="${PORTS[1]}" WEB="${PORTS[2]}"

TMP="$(mktemp -d /tmp/concord-guest-verify.XXXXXX)"
PIDS=()
STATUS=1
cleanup() {
  for pid in "${PIDS[@]:-}"; do kill "$pid" 2>/dev/null || true; done
  wait 2>/dev/null || true
  echo "artifacts in $TMP" >&2
}
trap cleanup EXIT

echo "== build (UI + web binary + rendezvous)"
npm --prefix frontend run build >"$TMP/npm-build.log" 2>&1 || { echo "FAIL: frontend build" >&2; exit 1; }
go build -o "$TMP/concord-web" . || { echo "FAIL: go build ." >&2; exit 1; }
go build -o "$TMP/rendezvous" ./cmd/rendezvous || { echo "FAIL: go build ./cmd/rendezvous" >&2; exit 1; }

echo "== start rendezvous (p2p :$RDV_P2P, guest gateway :$GATEWAY)"
PORT="$RDV_P2P" CONCORD_GUEST_PORT="$GATEWAY" "$TMP/rendezvous" >"$TMP/rdv.log" 2>&1 &
PIDS+=($!)
PEER=""
for _ in $(seq 1 50); do
  PEER="$(sed -n 's/^PeerID: //p' "$TMP/rdv.log" | head -1)"
  [ -n "$PEER" ] && break
  sleep 0.2
done
[ -n "$PEER" ] || { echo "FAIL: rendezvous never printed its PeerID" >&2; exit 1; }
BOOT="/ip4/127.0.0.1/tcp/$RDV_P2P/p2p/$PEER"

echo "== start member app (:$WEB)"
CONCORD_HOME="$TMP/member-home" \
  CONCORD_WEB_ADDR="127.0.0.1:$WEB" \
  CONCORD_NO_OPEN=1 CONCORD_DISABLE_MDNS=1 \
  CONCORD_BOOTSTRAP="$BOOT" \
  CONCORD_GUEST_BASE="http://127.0.0.1:$GATEWAY" \
  "$TMP/concord-web" >"$TMP/app.log" 2>&1 &
PIDS+=($!)
for _ in $(seq 1 50); do
  curl -sf "http://127.0.0.1:$WEB/" >/dev/null 2>&1 && break
  sleep 0.2
done
curl -sf "http://127.0.0.1:$WEB/" >/dev/null || { echo "FAIL: member app never came up" >&2; exit 1; }

MEMBER_URL="http://127.0.0.1:$WEB" \
  GATEWAY_URL="http://127.0.0.1:$GATEWAY" \
  APP_HOME="$TMP/member-home" \
  APP_BIN="$TMP/concord-web" \
  BOOTSTRAP="$BOOT" \
  WEB_PORT="$WEB" \
  OUT_DIR="$TMP" \
  node "$ROOT/scripts/verify-guest.mjs"

STATUS=0
echo "artifacts: $TMP"
