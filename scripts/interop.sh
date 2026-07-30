#!/usr/bin/env bash
#
# LEGACY PEER INTEROP TEST
#
# Runs a real previous-release binary (default: the v0.41.0 linux/amd64 release
# in dist-release/) as one peer and a fresh build of HEAD as the other, both
# bootstrapping off a local rendezvous node, and checks that they can still find
# each other, join each other's guilds, exchange messages and attachments, sync
# history, and see each other in the member list.
#
# The point is to catch the regressions that only show up against a peer that
# has NOT updated: new wire fields an old decoder chokes on, new message kinds,
# new inline token formats, new guild-meta types, presence gating that hides an
# old peer. Unit tests cannot see any of this, because both sides of a unit test
# are always the same build.
#
# The old release binary is used AS SHIPPED — not rebuilt from the tag — so the
# embedded frontend is the old frontend too, and a token the old UI cannot parse
# shows up as a real rendering failure rather than being silently fixed by a
# newer bundle. It is a plain `-tags web` build (see main_web.go at v0.41.0), so
# it exposes the same loopback /rpc + /events surface HEAD does and can be driven
# directly over HTTP; no worktree build is needed.
#
# Usage:
#   ./scripts/interop.sh                       # default old binary
#   ./scripts/interop.sh path/to/old-concord   # a different legacy peer
#   KEEP=1 ./scripts/interop.sh                # leave logs + data dirs behind
#
#   # Control run: make the NEW peer another copy of the OLD binary. A lane that
#   # fails here too is a pre-existing limitation, not something this release
#   # broke — worth one command before filing anything as a regression.
#   NEW_BIN=dist-release/concord-linux-amd64-v0.41.0 ./scripts/interop.sh
#
# Exits 0 when every lane passes, nonzero with a one-line reason otherwise.
# Never reads or writes the user's real ~/.config/concord, and never uses their
# configured rendezvous: every peer gets a throwaway CONCORD_HOME and is pinned
# to the local rendezvous node started here.
set -uo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OLD_BIN="${1:-$REPO/dist-release/concord-linux-amd64-v0.41.0}"

# Ports live in a band nothing else in this repo uses, so a stray dev peer on
# 8787/8801 cannot be mistaken for one of ours.
RV_PORT=8961
OLD_WEB_PORT=8962
NEW_WEB_PORT=8963

WORK="$(mktemp -d /tmp/concord-interop.XXXXXXXX)"
RV_BIN="$WORK/rendezvous"
PIDS=()

die() { echo "interop: FAIL: $*" >&2; exit 1; }
note() { echo "interop: $*"; }

cleanup() {
  local st=$?
  for pid in ${PIDS+"${PIDS[@]}"}; do
    kill "$pid" 2>/dev/null
  done
  # Give them a beat to release ports and flush logs, then insist.
  sleep 0.5
  for pid in ${PIDS+"${PIDS[@]}"}; do
    kill -9 "$pid" 2>/dev/null
  done
  wait 2>/dev/null
  if [[ "${KEEP:-}" == "1" ]]; then
    echo "interop: kept $WORK"
  else
    rm -rf "$WORK"
  fi
  return $st
}
trap cleanup EXIT INT TERM

[[ -x "$OLD_BIN" ]] || die "old binary not found or not executable: $OLD_BIN"

# --- guard: never touch the user's real data ------------------------------------
# CONCORD_HOME is set per peer below, but an unset/empty value would silently
# fall back to ~/.config/concord. Make that impossible by construction.
export CONCORD_NO_OPEN=1
export CONCORD_DISABLE_MDNS=1
unset CONCORD_BOOTSTRAP

# --- build ---------------------------------------------------------------------
if [[ ! -d "$REPO/frontend/dist" ]]; then
  note "building frontend (dist missing)..."
  ( cd "$REPO/frontend" && npm run build ) >"$WORK/frontend-build.log" 2>&1 \
    || die "frontend build failed, see $WORK/frontend-build.log"
fi

if [[ -n "${NEW_BIN:-}" ]]; then
  [[ -x "$NEW_BIN" ]] || die "NEW_BIN not found or not executable: $NEW_BIN"
  note "CONTROL RUN: the 'new' peer is $(basename "$NEW_BIN"), not HEAD"
  NEW_BIN="$(cd "$(dirname "$NEW_BIN")" && pwd)/$(basename "$NEW_BIN")"
else
  NEW_BIN="$WORK/concord-new"
  note "building HEAD ($(git -C "$REPO" describe --tags --always 2>/dev/null))..."
  go build -C "$REPO" -tags web -o "$NEW_BIN" . >"$WORK/build-new.log" 2>&1 \
    || die "go build of HEAD failed, see $WORK/build-new.log"
fi
go build -C "$REPO" -o "$RV_BIN" ./cmd/rendezvous >"$WORK/build-rv.log" 2>&1 \
  || die "go build of ./cmd/rendezvous failed, see $WORK/build-rv.log"

# --- rendezvous ----------------------------------------------------------------
# Both peers meet through this the way real users meet through the deployed
# node: DHT provider records + relay + mailbox. A random identity per run is
# fine, we hand the printed multiaddr to both peers.
note "starting local rendezvous on :$RV_PORT..."
PORT="$RV_PORT" "$RV_BIN" >"$WORK/rendezvous.log" 2>&1 &
PIDS+=($!)

BOOTSTRAP=""
for _ in $(seq 1 100); do
  BOOTSTRAP="$(grep -om1 "/ip4/127\.0\.0\.1/tcp/$RV_PORT/p2p/[A-Za-z0-9]*" "$WORK/rendezvous.log" 2>/dev/null)"
  [[ -n "$BOOTSTRAP" ]] && break
  sleep 0.2
done
[[ -n "$BOOTSTRAP" ]] || die "rendezvous never printed a loopback multiaddr, see $WORK/rendezvous.log"
note "bootstrap: $BOOTSTRAP"
export CONCORD_BOOTSTRAP="$BOOTSTRAP"

# --- peers ---------------------------------------------------------------------
# start_peer <label> <binary> <web-port>
start_peer() {
  local label="$1" bin="$2" port="$3"
  mkdir -p "$WORK/$label"
  CONCORD_HOME="$WORK/$label" CONCORD_WEB_ADDR="127.0.0.1:$port" \
    "$bin" >>"$WORK/$label.log" 2>&1 &
  PIDS+=($!)
  eval "${label}_PID=$!"
  for _ in $(seq 1 150); do
    if curl -sf -m 2 -X POST "http://127.0.0.1:$port/rpc" \
         -H 'Content-Type: application/json' \
         -d '{"method":"HasIdentity","args":[]}' >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.2
  done
  die "$label never served /rpc on :$port, see $WORK/$label.log"
}

stop_peer() {
  local label="$1"
  local pid
  eval "pid=\${${label}_PID:-}"
  [[ -n "$pid" ]] || return 0
  kill "$pid" 2>/dev/null
  for _ in $(seq 1 50); do
    kill -0 "$pid" 2>/dev/null || return 0
    sleep 0.2
  done
  kill -9 "$pid" 2>/dev/null
}

note "starting OLD peer ($(basename "$OLD_BIN")) on :$OLD_WEB_PORT..."
start_peer old "$OLD_BIN" "$OLD_WEB_PORT"
note "starting NEW peer (HEAD) on :$NEW_WEB_PORT..."
start_peer new "$NEW_BIN" "$NEW_WEB_PORT"

# --- lanes ---------------------------------------------------------------------
export INTEROP_OLD="http://127.0.0.1:$OLD_WEB_PORT"
export INTEROP_NEW="http://127.0.0.1:$NEW_WEB_PORT"
export INTEROP_WORK="$WORK"

drive() {
  node "$REPO/scripts/interop.mjs" "$@"
  local rc=$?
  [[ $rc -eq 0 ]] || die "phase $1 failed (see output above; logs in $WORK)"
}

# Phase 1: everything that needs both peers online.
drive online

# Phase 2: history sync. OLD goes away, NEW keeps posting, OLD comes back and
# must catch up — the lane a legacy peer hits every time it opens the app after
# a few days away.
note "stopping OLD peer for the history-sync lane..."
stop_peer old
drive while-old-down
note "restarting OLD peer..."
start_peer old "$OLD_BIN" "$OLD_WEB_PORT"
drive old-came-back

# Phase 3: final report over collected evidence.
drive report

note "PASS: every lane survived a v0.41.0 peer"
