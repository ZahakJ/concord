# Concord rendezvous / relay node

An untrusted infrastructure node that lets Concord peers find and reach each
other across the internet. It serves the Kademlia DHT (for discovery) and a
Circuit Relay v2 service (for NAT'd peers), and **never sees plaintext or
media** — everything it carries is already end-to-end encrypted.

## Run locally

```sh
go run ./cmd/rendezvous            # random identity (PeerID changes each run)
# or with a stable identity:
CONCORD_RELAY_SEED=$(openssl rand -hex 32) go run ./cmd/rendezvous
```

It prints its `PeerID` and bootstrap multiaddrs. Point peers at one:

```sh
CONCORD_BOOTSTRAP="/ip4/1.2.3.4/tcp/4001/p2p/<PeerID>" go run .   # web app
# multiple, comma-separated, are allowed
```

Setting `CONCORD_BOOTSTRAP` enables the DHT; add `CONCORD_DISABLE_MDNS=1` to test
internet discovery without LAN mDNS shortcutting it.

## Deploy to fly.io

```sh
# from the repo root
fly launch --no-deploy -c fly.rendezvous.toml
fly secrets set CONCORD_RELAY_SEED=$(openssl rand -hex 32)   # STABLE identity
fly deploy -c fly.rendezvous.toml

# discover the node's public multiaddr:
fly logs         # look for the printed /ip4/<public-ip>/tcp/4001/p2p/<PeerID>
```

Give that multiaddr to peers as `CONCORD_BOOTSTRAP`. Keep `CONCORD_RELAY_SEED`
stable so the PeerID (and thus the bootstrap address) doesn't change across
deploys.

## Note on local testing

DHT discovery needs a healthy routing table; a 2–3 node localhost mesh is not
representative and often won't converge. Verify discovery against a real
deployment (above) or a larger set of nodes.
