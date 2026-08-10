# Running a rendezvous

Two Concord peers can only talk if at least one of them can accept an incoming
connection. On the same Wi-Fi that is free. Across the internet, home routers
usually refuse, and something has to introduce the two sides.

That something is a *rendezvous*: a small always-on node that helps peers find
each other and forwards their traffic when neither can be dialled directly. It
is not a server in the sense Concord is arguing against — it holds no accounts,
stores no history, and cannot read a message. [§6 of
DESIGN.md](DESIGN.md#6-the-rendezvous-node) sets out exactly what it can and
cannot see.

**You may not need one.** Work down this list and stop at the first that fits.

| Your situation | What to do | Cost |
|---|---|---|
| Everyone on the same Wi-Fi | Nothing. Peers find each other over mDNS. | Desktop-to-desktop only; Android's sandbox blocks the socket, and many work and campus networks drop the packets. |
| You already use Tailscale or WireGuard | Nothing. See [below](#the-easy-answer-tailscale). | Everyone needs to be on the same tailnet. |
| One person can forward a port | That person pins a port and adds one router rule. Invite codes carry their address. | Their home IP is inside every invite code they hand out. |
| Nobody can forward a port | Somebody runs a rendezvous. The rest of this document. | One machine has to exist somewhere. It does not have to be yours. |

Inside the app, **Can people reach me?** (in the Ctrl+K palette, and beside every
invite code) tells you which row you are actually in.

## The easy answer: Tailscale

If you and your friends are on a [Tailscale](https://tailscale.com) tailnet — or
any WireGuard mesh — you need no rendezvous at all. Every machine gets a stable
address that every other machine can dial directly, which is precisely the thing
home routers refuse to provide.

Start Concord normally on each machine, then in **Settings → Connection** set the
listen address, or start it with the tailnet address bound:

```sh
CONCORD_WEB_ADDR=0.0.0.0:8787 ./concord-web
```

Then hand out invite codes as usual. Concord's own address detection picks up the
tailnet interface, so the code carries an address the other side can reach.

This is the best option for a private group: no public infrastructure, no
port-forwarding, and no IP address handed to strangers. It does not help you meet
someone who is *not* on your tailnet — for that, read on.

## Run one locally (to try it, or on a LAN)

```sh
make rendezvous          # builds bin/rendezvous
./bin/rendezvous
```

It prints its PeerID and its bootstrap addresses. Point a peer at one:

```sh
CONCORD_BOOTSTRAP="/ip4/192.168.1.50/tcp/4001/p2p/12D3KooW…" ./concord-web
```

or paste the same address into **Settings → Connection** in the running app,
which saves it and dials immediately.

By default the node's identity is random, so its PeerID — and therefore its
address — changes on every restart. Pin it:

```sh
CONCORD_RELAY_SEED=$(openssl rand -hex 32) ./bin/rendezvous
```

Keep that seed. It *is* the node's identity; losing it invalidates every
bootstrap address you have handed out.

> A two- or three-node mesh on one machine is not a representative test of DHT
> discovery: the routing table is too small to converge the way it does in the
> open. Local runs are good for checking the node starts and peers connect to it,
> not for judging whether discovery works.

## Run one with Docker

The image builds only the node — no frontend, no app.

```sh
docker build -f infra/rendezvous/Dockerfile -t concord-rendezvous .
docker run -d --name rendezvous --restart unless-stopped \
  -p 4001:4001/tcp -p 4001:4001/udp \
  -e CONCORD_RELAY_SEED=$(openssl rand -hex 32) \
  concord-rendezvous
docker logs rendezvous     # read the printed bootstrap address
```

Both TCP and UDP on the same port: libp2p uses TCP and QUIC, and losing UDP
costs you the faster path and some NAT traversal.

Put the seed in a file rather than the command line if the host is shared — it is
the node's private identity, and `docker inspect` shows environment variables.

## Run one on a VPS

Any small machine with a public IP works; the node is a single static binary with
no dependencies. The cheapest tier at any provider is enough — it forwards
ciphertext and answers DHT queries, and 256 MB of RAM is plenty for a group of
friends.

```sh
# on the server
git clone https://github.com/ZahakJ/concord && cd concord
make rendezvous
sudo cp bin/rendezvous /usr/local/bin/concord-rendezvous
```

Give it a stable identity and a service that restarts it:

```sh
sudo tee /etc/concord-rendezvous.env >/dev/null <<EOF
CONCORD_RELAY_SEED=$(openssl rand -hex 32)
PORT=4001
EOF
sudo chmod 600 /etc/concord-rendezvous.env

sudo tee /etc/systemd/system/concord-rendezvous.service >/dev/null <<'EOF'
[Unit]
Description=Concord rendezvous node
After=network-online.target

[Service]
EnvironmentFile=/etc/concord-rendezvous.env
ExecStart=/usr/local/bin/concord-rendezvous
Restart=always
RestartSec=5
DynamicUser=yes
# It needs nothing from the filesystem but its own binary.
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
NoNewPrivileges=yes

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl enable --now concord-rendezvous
sudo journalctl -u concord-rendezvous -n 30    # read the bootstrap address
```

Open 4001 on both TCP and UDP in the provider's firewall and in `ufw`:

```sh
sudo ufw allow 4001/tcp && sudo ufw allow 4001/udp
```

If the machine has a DNS name, set `CONCORD_PUBLIC_HOST` to it and the node
prints a `/dns/…` address instead of a raw IP. That address survives the machine
being rebuilt on a new IP, which a `/ip4/…` one does not — worth doing before you
hand the address to anyone.

## Run one on fly.io

`fly.rendezvous.toml` in the repository root is a working deployment on a free
account. Rename the app first — fly app names are globally unique.

```sh
fly launch --no-deploy -c fly.rendezvous.toml
fly secrets set CONCORD_RELAY_SEED=$(openssl rand -hex 32)
fly secrets set CONCORD_PUBLIC_HOST=<your-app>.fly.dev
fly deploy -c fly.rendezvous.toml
fly logs                      # read the printed /dns/… address
```

**Do not enable the TURN relay on fly.io.** The config ships with that block
commented out and a comment explaining why: fly's edge does not hairpin UDP back
to the advertised public IP, so a relay there authenticates, allocates, and then
carries nothing, while calls silently fail instead of falling back. Host the
relay on a machine that owns its public IP if you want it.

## Handing the address out

The node prints something like:

```
/dns/rdv.example.com/tcp/4001/p2p/12D3KooWCzonwxmETSLwgDY9JAe9WczUHssnYTrSpCqkp6UXZg1q
```

Friends paste that into **Settings → Connection**, or start with
`CONCORD_BOOTSTRAP=…` (comma-separated for several). After that it is automatic:
an invite code generated by someone bootstrapped through your node carries the
node's address with it, so the person joining is pointed at it without being told
to configure anything.

## Options

Everything is an environment variable; nothing is required except a port.

| Variable | Default | What it does |
|---|---|---|
| `PORT` | `4001` | libp2p listen port, TCP and UDP. |
| `CONCORD_RELAY_SEED` | random | 32-byte hex seed fixing the node's identity. Set it, keep it. |
| `CONCORD_PUBLIC_HOST` | — | DNS name to advertise, so the printed address survives an IP change. |
| `CONCORD_GUEST_PORT` | off | Port for the browser-guest gateway, so people can join a meeting with no install. Needs TLS in front. |
| `CONCORD_GIF_KEY` | off | Enables GIF search through this node. Without it the picker says the feature is unavailable. |
| `CONCORD_GIF_PROVIDER` | `giphy` | `giphy` or `tenor`. Tenor's public API was decommissioned in June 2026; it is kept for self-hosted mirrors. |
| `CONCORD_TURN_SECRET` | off | Enables the TURN relay for IP-private calls. Read the fly.io warning above first. |
| `CONCORD_TURN_PORT` | `3478` | TURN listen port when the relay is enabled. |

## What running one commits you to

- **It sees who talks to whom, and when.** It cannot read a message, a name, or a
  file — those are encrypted end to end and it holds no keys — but it necessarily
  learns which peers connect through it and at what times. Run one for people who
  are content for you to know that.
- **It uses very little.** Bandwidth is only what it relays, and it relays only
  for peers that cannot connect directly.
- **It is not a backup.** No history lives there. If everyone's devices are lost,
  the conversation is gone; the node has nothing to restore from.
- **It going away is survivable.** Peers remember addresses they have reached
  before and re-dial them directly, so an outage degrades discovery rather than
  stopping the app.

## If it is not working

| Symptom | Likely cause |
|---|---|
| Peers never see each other, node looks healthy | UDP is blocked, or only TCP is forwarded. Open both on the same port. |
| The address stopped working after a restart | No `CONCORD_RELAY_SEED`, so the identity — and the PeerID in the address — was regenerated. |
| The address stopped working after a rebuild | It was an `/ip4/…` address and the IP changed. Set `CONCORD_PUBLIC_HOST` and hand out the `/dns/…` form. |
| Works on the LAN, not across the internet | mDNS is doing the work locally. Set `CONCORD_DISABLE_MDNS=1` on a peer to test the real path. |
| Calls connect but nobody hears anything | A TURN relay that cannot hairpin — see the fly.io note. Disable it and calls fall back to direct. |
| Guest meeting links do nothing | `CONCORD_GUEST_PORT` is unset, or nothing terminates TLS in front of it. |
