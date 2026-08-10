# Concord rendezvous / relay node

An untrusted infrastructure node that lets Concord peers find and reach each
other across the internet. It serves the Kademlia DHT (for discovery) and a
Circuit Relay v2 service (for NAT'd peers), and **never sees plaintext or
media** — everything it carries is already end-to-end encrypted. What it can and
cannot observe is set out in [§6 of DESIGN.md](../../docs/DESIGN.md#6-the-rendezvous-node).

**Running one is documented in [docs/RENDEZVOUS.md](../../docs/RENDEZVOUS.md)** —
locally, in Docker, on a VPS, or on fly.io, plus the options, the trade-offs of
hosting one, and what to check when it is not working. This directory holds only
the build inputs.

## What is here

| File | |
|---|---|
| `Dockerfile` | Builds the node alone (`go build ./cmd/rendezvous`) into a distroless image. Build context is the repository root so the vendored MLS `replace` resolves; `.dockerignore` whitelists the four trees the build reads. |

The deployment descriptor for fly.io is `fly.rendezvous.toml` in the repository
root, and the node's source is `cmd/rendezvous/`.

## Building the image

```sh
docker build -f infra/rendezvous/Dockerfile -t concord-rendezvous .
```

from the repository root, not from this directory.
