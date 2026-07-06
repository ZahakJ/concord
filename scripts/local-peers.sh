#!/usr/bin/env bash
#
# Spin up N isolated Concord peers (browser-served) for local multi-peer
# testing. Each peer gets its own data dir and port, so they behave like
# separate people on the same machine. They discover each other over mDNS.
#
# Usage:
#   ./scripts/local-peers.sh [N]      # default 2
#   BASE_PORT=9000 ./scripts/local-peers.sh 3
#
# Open the printed URLs in separate browser windows/profiles. Ctrl-C stops all.
set -euo pipefail
cd "$(dirname "$0")/.."

N="${1:-2}"
BASE_PORT="${BASE_PORT:-8801}"
mkdir -p .dev

echo "Building frontend + web server..."
( cd frontend && npm install >/dev/null 2>&1 && npm run build >/dev/null 2>&1 )
go build -o .dev/concord-web .

pids=()
cleanup() {
  echo
  echo "Stopping peers..."
  for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done
}
trap cleanup EXIT INT TERM

echo
echo "Starting $N peer(s):"
for i in $(seq 1 "$N"); do
  port=$((BASE_PORT + i - 1))
  home=".dev/peer$i"
  mkdir -p "$home"
  CONCORD_HOME="$home" CONCORD_WEB_ADDR="127.0.0.1:$port" \
    .dev/concord-web > ".dev/peer$i.log" 2>&1 &
  pids+=($!)
  printf "  peer %d  →  http://127.0.0.1:%d   (data: %s)\n" "$i" "$port" "$home"
done

cat <<EOF

Open each URL in a separate browser window (or private windows).
On first unlock, pick any passphrase — each peer has its own identity.
Try: create a guild in peer 1 → Invite → paste the code into peer 2's "Join".

Logs:  .dev/peer*.log      Stop all:  Ctrl-C
EOF

wait
