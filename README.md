# Concord

A clean, security-first, **peer-to-peer** alternative to Discord. Run a local
instance, create a guild (server) with channels, and chat with **end-to-end
encrypted** group messages — no central server holds your messages or keys.

Built in Go. Voice/video (P2P mesh) is on the roadmap.

## Status

| Phase | What | State |
|-------|------|-------|
| 0 | Encrypted identity (Ed25519 account, Argon2id keystore) | ✅ done |
| 1 | libp2p transport + mDNS LAN discovery (Noise-encrypted) | ✅ done |
| 2 | Group text channels: MLS group E2EE, gossipsub, invites, encrypted history | ✅ done |
| 4 | Voice mesh: browser WebRTC media + libp2p signaling | ✅ done¹ |
| 3 | Internet: DHT discovery + relay + hole-punching + fly.io node | ✅ code + deploy² |
| 5 | Hardening & polish | ⏳ planned |
| — | GUI (browser-served + Wails desktop) | ✅ browser; Wails needs `webkit2gtk` |

¹ Voice signaling backbone (presence + SDP/ICE relay) is built and tested; the
WebRTC audio path is browser-side and needs a real mic to exercise live.
² Discovery/relay code + the fly.io rendezvous node are built; full internet
discovery needs a real deployment to verify (a localhost DHT won't converge).
See `infra/rendezvous/`.

## Security model

- **Identity = a local Ed25519 keypair.** No signup, no server account. Your
  libp2p PeerID derives from it; your public-key **fingerprint** ("safety
  number") is what you verify out-of-band with contacts.
- **Group messages are end-to-end encrypted with MLS** (RFC 9420, cipher suite
  CS1). Every guild is an MLS group with a continuously-ratcheted key giving
  **forward secrecy** and **post-compromise security**. Only ciphertext travels
  over the gossipsub network. Each member's MLS leaf is signed by a key derived
  from its Ed25519 account, binding group membership to the account.
- **Transport encryption:** every libp2p connection uses the Noise protocol.
- **Voice is end-to-end encrypted** via WebRTC DTLS-SRTP, browser-to-browser in
  a mesh. Go only relays the SDP/ICE signaling; audio never passes through it.
- **At rest:** message bodies are sealed with NaCl secretbox under a key derived
  from your (passphrase-protected) identity. The identity itself lives in an
  Argon2id + secretbox encrypted keystore.
- **Contact verification:** peers are recorded trust-on-first-use; you confirm a
  contact's fingerprint out-of-band and mark them `verified` to defeat a
  man-in-the-middle (`contacts` / `verify` in the console).
- **Display names are decorative, fingerprints are truth:** a peer's chosen name
  is self-asserted and shown for convenience, but the authenticated identity is
  always the Ed25519 fingerprint (shown next to the name), which cannot be
  spoofed. Online presence and typing indicators are conveniences over gossipsub.

## Architecture (layered)

```
7. UI            frontend/            (Wails/Svelte — planned)
6. App/API       internal/app         orchestrates everything; the API both CLI and GUI use
3. Domain        internal/domain      pure guild/channel/message model (no I/O)
4. Media         internal/media       Pion WebRTC mesh (planned)
5. Persistence   internal/store       encrypted SQLite (pure-Go, no CGO)
2. Network       internal/net         libp2p host, mDNS, gossipsub, invite streams
1. Crypto/ID     internal/identity    Ed25519 identity + encrypted keystore
                 internal/crypto/mls  MLS group-encryption engine (behind a swappable interface)
```

## Build & run (GUI)

The same Svelte UI runs two ways, both driving the same `internal/app` Service.
Unlock with a passphrase, then create/join guilds, chat, manage members, and
verify contacts.

**Browser-served (no system dependencies — works anywhere Go does):**

```sh
cd frontend && npm install && npm run build && cd ..
go run .            # serves http://127.0.0.1:8787
# then open that URL in your browser
```

**Local multi-peer testing** — spin up several isolated peers at once:

```sh
make peers          # 2 peers at :8801, :8802  (make peers N=3 for more)
make dev-clean      # remove the local test data afterwards
```

Open each printed URL in a separate browser window; they discover each other
over mDNS. `make help` lists all targets.

**Native desktop (Wails)** — needs the system WebView:

```sh
sudo pacman -S --needed webkit2gtk-4.1        # Arch; or webkit2gtk (4.0)
make gui        # builds bin/concord-gui, then run it
# or hot-reload dev (needs the wails CLI):  make gui-dev
```

`make gui` applies the right build tags (`wails desktop production webkit2_41`)
so it selects the native variant (main_wails.go) with webkit2gtk-4.1. The
default `go build` / `go run` (no tags) builds the browser variant instead.

## Build & run (headless CLI)

The desktop GUI needs a system WebView (`webkit2gtk`) that may not be installed;
until then the full stack runs headless via a terminal console.

```sh
go build -o concord ./cmd/concord

# Show your identity (creates it on first run):
CONCORD_PASSPHRASE=yourpass ./concord --status

# Run an interactive node (mDNS LAN discovery on):
CONCORD_PASSPHRASE=yourpass ./concord --serve
```

Run two nodes on one machine with separate data dirs via `CONCORD_HOME`:

```sh
# terminal 1
CONCORD_HOME=~/.concord-a CONCORD_PASSPHRASE=p ./concord --serve
> create MyServer          # prints a guildID and channelID
> invite <guildID>         # prints an invite code to share

# terminal 2
CONCORD_HOME=~/.concord-b CONCORD_PASSPHRASE=p ./concord --serve
> join <invite code>       # joins the guild
> send <channelID> hello   # both peers see the message
```

REPL commands: `create`, `guilds`, `members`, `kick`, `invite`, `join`, `send`,
`history`, `contacts`, `verify`, `whoami`, `help`, `quit`.

## Tests

```sh
go test ./...            # full suite, incl. a 3-node E2EE integration test
go test -short ./...     # skip the networked integration test
go test -race ./...      # race detector
```

## Vendored dependency

`third_party/mls-go` is a local copy of `github.com/thomas-vilte/mls-go` v1.5.0
(MIT), used via a `replace` directive in `go.mod`. It carries **one Concord
patch**, isolated in `client_concord_ed25519.go` plus a few lines in `client.go`:
a `WithEd25519SignatureKey` option that lets the caller supply a deterministic
Ed25519 signing key, mirroring the library's existing `WithX509Credential` seam.

Upstream generates a random signing key per client and never reloads it, so a
member couldn't sign after a restart. The patch adds no cryptographic logic and
the library's own test suite (including RFC 9420 interop vectors) passes against
the patched copy. It's intended to be contributed upstream.

## Play with friends over the internet

On the same Wi-Fi, peers find each other automatically (mDNS) — nothing to set
up. To play with friends on **different networks**, one person hosts a tiny
rendezvous node (it only relays already-encrypted traffic; it never sees your
messages). It's a one-time, ~3-command setup:

**1. One friend deploys the rendezvous** (needs a free [fly.io](https://fly.io) account):

```sh
fly launch --no-deploy -c infra/rendezvous/fly.toml   # pick an app name
fly secrets set CONCORD_RELAY_SEED=$(openssl rand -hex 32) \
                CONCORD_PUBLIC_HOST=<your-app-name>.fly.dev
fly deploy -c infra/rendezvous/fly.toml
fly logs        # copy the ">>> SHARE THIS ADDRESS <<<" line
```

**2. Everyone pastes that address** on the Concord login screen under
“Connect with friends”, then unlocks. That's it — you're all on the same
network now.

**3. Create a guild, hit Invite, share the code**, friends paste it into “Join”.

(A native mobile app — roadmap #14 — will make this even more turnkey.)

## Roadmap

Concord has the core of a secure Discord alternative. To reach parity, in rough
priority order:

**Platform**
- **Mobile apps (iOS + Android)** — full native phone UI/UX (not just a resized
  desktop layout): bottom-nav, swipe gestures, push notifications, background
  connectivity. Likely a shared Go core (gomobile) with a native/Flutter/React
  Native shell, or a Capacitor wrapper over the existing Svelte UI.
- Deploy the fly.io rendezvous node and verify cross-internet discovery (#13).

**Messaging**
- History sync on reconnect / offline delivery (#11).
- Direct messages (1:1) and group DMs.
- Message edits and threads. *(Replies, deletes, and reactions: done.)*
- Attachments: images/files (chunked, E2E-encrypted, over libp2p streams).
- Mentions + notifications (per-guild/channel mute).

**Voice / video**
- Voice panel: participant tiles with avatars and a **green speaking ring**
  (via WebRTC audio-level detection), per-user mute/deafen, connection quality.
- Screen sharing and webcam video in the mesh.
- Untrusted SFU (self-hostable) for large rooms, keeping E2E via SFrame.

**Guild / server management**
- Roles & permissions (admin/mod/member), channel categories, per-channel perms.
- Server settings UI: manage members, bans, invites (revocable/expiring),
  ownership transfer, guild rename/icon.
- Moderation: message delete by mods, audit log.

**Personalization**
- Profiles: avatars, custom status, "about me", accent colour.
- App themes (light/dark/custom), notification preferences, per-guild nicknames.

**Hardening**
- Metadata privacy (onion-routed discovery), key backup/recovery, multi-device.

## Notes / current limitations

- **Membership (MVP):** the guild owner is the sole committer — only the owner
  issues invites. This serializes MLS commits and avoids concurrent-commit
  conflict resolution for now.
- **Offline delivery:** messages are delivered to online peers; store-and-forward
  for offline members is future work.
- **MLS state across restarts:** fully working. Group ratchet state is persisted
  to disk, and the MLS signing key is derived deterministically from the identity
  (HKDF-separated from the libp2p key), so a restarted node recovers its groups
  and can both receive and send. This required a small patch to the MLS library
  (see *Vendored dependency* below).
- **Reach:** works on a LAN today (mDNS). Cross-internet connectivity
  (rendezvous/relay) arrives in Phase 3.
