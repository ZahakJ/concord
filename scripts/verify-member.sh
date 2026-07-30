#!/usr/bin/env bash
# Member-side REGRESSION sweep for the guest work: two real member apps in one
# guild, a normal (non-guest) call between them, audio both ways, screen share,
# and the meeting-creation UI that the new lifetime chips landed in. The guest
# changes touched voice.go, VoicePanel and state.svelte.js, all of which the
# ordinary member path runs through — so "guests work" is not evidence that
# members still do.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

port_free() { ! (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }
PORTS=()
for p in $(seq 8981 8990); do
  port_free "$p" && PORTS+=("$p")
  [ "${#PORTS[@]}" -ge 4 ] && break
done
[ "${#PORTS[@]}" -ge 4 ] || { echo "FAIL: need 4 free ports in 8981-8990" >&2; exit 1; }
RDV_P2P="${PORTS[0]}" GATEWAY="${PORTS[1]}" WEB_A="${PORTS[2]}" WEB_B="${PORTS[3]}"

TMP="$(mktemp -d /tmp/concord-member-verify.XXXXXX)"
PIDS=()
cleanup() { for pid in "${PIDS[@]:-}"; do kill "$pid" 2>/dev/null || true; done; wait 2>/dev/null || true; echo "artifacts in $TMP" >&2; }
trap cleanup EXIT

echo "== build"
npm --prefix frontend run build >"$TMP/npm-build.log" 2>&1 || { echo "FAIL: frontend build" >&2; exit 1; }
go build -o "$TMP/concord-web" . || exit 1
go build -o "$TMP/rendezvous" ./cmd/rendezvous || exit 1

PORT="$RDV_P2P" CONCORD_GUEST_PORT="$GATEWAY" "$TMP/rendezvous" >"$TMP/rdv.log" 2>&1 &
PIDS+=($!)
PEER=""
for _ in $(seq 1 50); do PEER="$(sed -n 's/^PeerID: //p' "$TMP/rdv.log" | head -1)"; [ -n "$PEER" ] && break; sleep 0.2; done
[ -n "$PEER" ] || { echo "FAIL: no rendezvous PeerID" >&2; exit 1; }
BOOT="/ip4/127.0.0.1/tcp/$RDV_P2P/p2p/$PEER"

start_app() { # name port
  CONCORD_HOME="$TMP/$1-home" CONCORD_WEB_ADDR="127.0.0.1:$2" \
    CONCORD_NO_OPEN=1 CONCORD_DISABLE_MDNS=1 CONCORD_BOOTSTRAP="$BOOT" \
    CONCORD_GUEST_BASE="http://127.0.0.1:$GATEWAY" \
    "$TMP/concord-web" >"$TMP/$1.log" 2>&1 &
  PIDS+=($!)
  for _ in $(seq 1 60); do curl -sf "http://127.0.0.1:$2/" >/dev/null 2>&1 && return 0; sleep 0.25; done
  echo "FAIL: app $1 never came up" >&2; exit 1
}
echo "== start two member apps (:$WEB_A, :$WEB_B)"
start_app alice "$WEB_A"
start_app bob "$WEB_B"

A_URL="http://127.0.0.1:$WEB_A" B_URL="http://127.0.0.1:$WEB_B" OUT_DIR="$TMP" \
  node "$ROOT/scripts/verify-member.mjs"
echo "artifacts: $TMP"
