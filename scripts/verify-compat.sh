#!/usr/bin/env bash
# Cross-version guest check. The gateway and the member app deploy SEPARATELY,
# so both mixed pairs have to be tried, not reasoned about:
#
#   A: OLD gateway (v0.41.0) + NEW app
#   B: NEW gateway         + OLD app (v0.41.0)
#
# The guest page is go:embed-ed into the rendezvous binary, so "old gateway"
# necessarily means "old page" and the two can never skew — which is precisely
# why direction A is the one that can bite.
#
# Needs the v0.41.0 binaries built beforehand:
#   OLD_RENDEZVOUS=<path> OLD_APP=<path> scripts/verify-compat.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
: "${OLD_RENDEZVOUS:?set OLD_RENDEZVOUS to a v0.41.0 rendezvous binary}"
: "${OLD_APP:?set OLD_APP to a v0.41.0 concord web binary}"

port_free() { ! (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }
pick() { for p in $(seq "$1" "$2"); do port_free "$p" && { echo "$p"; return; }; done; echo "" ; }

TMP="$(mktemp -d /tmp/concord-compat.XXXXXX)"
PIDS=()
cleanup() { for pid in "${PIDS[@]:-}"; do kill "$pid" 2>/dev/null || true; done; wait 2>/dev/null || true; echo "artifacts in $TMP" >&2; }
trap cleanup EXIT

echo "== build current binaries"
npm --prefix frontend run build >"$TMP/npm.log" 2>&1 || { echo "FAIL frontend build" >&2; exit 1; }
go build -o "$TMP/new-concord-web" . || exit 1
go build -o "$TMP/new-rendezvous" ./cmd/rendezvous || exit 1

run_case() { # name rendezvous_bin app_bin
  local NAME="$1" RDV="$2" APP="$3"
  local P2P GW WEB
  P2P="$(pick 8981 8984)"; GW="$(pick 8985 8987)"; WEB="$(pick 8988 8990)"
  [ -n "$P2P" ] && [ -n "$GW" ] && [ -n "$WEB" ] || { echo "FAIL: no ports for $NAME" >&2; return 1; }
  echo ""
  echo "=========== CASE $NAME  (rdv=$(basename "$RDV") app=$(basename "$APP")) ==========="
  PORT="$P2P" CONCORD_GUEST_PORT="$GW" "$RDV" >"$TMP/$NAME-rdv.log" 2>&1 &
  PIDS+=($!)
  local PEER=""
  for _ in $(seq 1 60); do PEER="$(sed -n 's/^PeerID: //p' "$TMP/$NAME-rdv.log" | head -1)"; [ -n "$PEER" ] && break; sleep 0.25; done
  [ -n "$PEER" ] || { echo "FAIL: $NAME rendezvous no PeerID" >&2; return 1; }
  CONCORD_HOME="$TMP/$NAME-home" CONCORD_WEB_ADDR="127.0.0.1:$WEB" \
    CONCORD_NO_OPEN=1 CONCORD_DISABLE_MDNS=1 \
    CONCORD_BOOTSTRAP="/ip4/127.0.0.1/tcp/$P2P/p2p/$PEER" \
    CONCORD_GUEST_BASE="http://127.0.0.1:$GW" \
    "$APP" >"$TMP/$NAME-app.log" 2>&1 &
  PIDS+=($!)
  for _ in $(seq 1 60); do curl -sf "http://127.0.0.1:$WEB/" >/dev/null 2>&1 && break; sleep 0.25; done
  curl -sf "http://127.0.0.1:$WEB/" >/dev/null || { echo "FAIL: $NAME app never came up" >&2; return 1; }

  CASE="$NAME" MEMBER_URL="http://127.0.0.1:$WEB" GATEWAY_URL="http://127.0.0.1:$GW" \
    OUT_DIR="$TMP" node "$ROOT/scripts/verify-compat.mjs" || true
}

run_case "A-oldgw-newapp" "$OLD_RENDEZVOUS" "$TMP/new-concord-web"
run_case "B-newgw-oldapp" "$TMP/new-rendezvous" "$OLD_APP"
echo ""
echo "artifacts: $TMP"
