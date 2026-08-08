# Concord Privacy

Concord is a peer-to-peer, end-to-end-encrypted messenger. This document states
plainly what data exists, where it lives, who can read it, and — just as
importantly — what Concord does *not* protect. The [README](README.md) covers
the same ground in architectural depth (see especially
[§13 Threat model](docs/DESIGN.md#13-threat-model-what-concord-defends-against));
this page is the short, honest version.

## The model in one paragraph

There is no company, no account database, and no server that stores your
messages. Your identity is a keypair on your device; your history is an
encrypted SQLite database on your device; your messages travel directly between
group members, encrypted end-to-end with [MLS](https://www.rfc-editor.org/rfc/rfc9420)
(RFC 9420). Any helper infrastructure Concord can use is **untrusted by
construction**: it forwards ciphertext and is cryptographically incapable of
reading it.

## What is end-to-end encrypted

- **Messages** (including edits, deletes, reactions, pins, and offline
  history-sync batches) are MLS-encrypted to the group. Only current members
  hold the keys. MLS provides forward secrecy (past epochs' keys are deleted)
  and post-compromise security (removing a member re-keys the group).
- **Voice** is a browser-to-browser WebRTC mesh; media is DTLS-SRTP encrypted
  between participants.
- **Transport** hops are additionally Noise/DTLS-encrypted, so a network
  eavesdropper (Wi-Fi, ISP) sees only ciphertext either way.

## Local-first storage

Your history lives only on your own devices — there is no server copy.
Locally it is an encrypted SQLite database:

- Message bodies are sealed with NaCl secretbox under a key derived from your
  identity seed; a stolen database file yields no readable messages.
- Your identity seed is itself encrypted at rest with Argon2id + secretbox,
  unlocked by your passphrase.
- Full-text search runs entirely on your machine; queries never leave it.
- Export is one click to Markdown — your data is yours.

## The untrusted rendezvous node

The one optional piece of infrastructure is a small rendezvous host (DHT
phone-book + circuit relay + offline mailbox + optional TURN relay for calls).
It is deliberately designed to learn as little as possible:

- **It only ever sees ciphertext.** It is never a group member and never holds
  message keys.
- **The offline mailbox is addressed by an opaque tag** derived (salted hash)
  from the recipient's public key. Senders can compute it; the node cannot
  reverse it to an identity. Deposits are end-to-end-encrypted blobs, drained
  and deleted when the recipient reconnects.
- **Optional push wakes are contentless** — "you have mail", never message
  content. Push tokens are keyed by the opaque mailbox tag, not by identity.
  Push is the one feature that requires trusting a node with APNs/FCM
  credentials; self-hosting without push is fully supported.
- **Its two plain-HTTPS doors are the exception to "ciphertext only".** A
  browser guest joining a meeting, and a visitor booking a slot on a
  `/book/<token>` link, have no Concord install and no keys, so their leg
  terminates at the node before continuing to the host. It therefore sees a
  guest's own messages, and a booking visitor's IP plus whatever they submit —
  the slot, their name, their note. It stores none of it, and everything else
  passing through remains ciphertext.
- **The TURN relay for calls** exists so meeting-link participants can hide
  their IP addresses from *each other*; media through it stays DTLS-SRTP
  encrypted end to end.
- You can self-host the whole thing.

## No telemetry, and the two outbound calls that are not to your peers

Concord contains no analytics, no crash reporting, and no telemetry. There are
no accounts, so there is nothing to sign up for and no email or phone number to
collect. Nearly every connection the app makes is to your peers or to whatever
rendezvous node you configure — but *nearly* is not *only*, and there are
exactly two exceptions. Naming them is the point of this section.

- **`api.github.com`, once at launch.** The app asks the project's own repo
  (`ZahakJ/concord`) for its latest release tag so it can tell you an
  update exists. It is unauthenticated, sends no identifier beyond what any
  HTTP request carries — your IP and a `concord-updater` user agent — and any
  error is a silent no-op. Builds without a release version stamp (i.e. built
  from source) skip the request entirely, and so does the mobile app, which
  does not self-update. See `internal/bridge/update.go`.
- **`stun.l.google.com`, at call time only.** Starting a call needs to learn
  your own external address. If the rendezvous you bootstrap through serves ICE
  configuration, that is used and Google is never contacted; only when it does
  not is there a fallback to this hardcoded public STUN server. It learns your
  IP and that you are starting a call — no media, no identity, no group, and
  nothing at all if you never place a call. See `internal/app/ice.go`.

Neither can be switched off from the UI today. The README covers the same two
in architectural terms ([§1](docs/DESIGN.md#1-the-problem-and-the-thesis),
[§9.2](docs/DESIGN.md#92-ip-privacy-the-state-of-it)).

## Honest limitations

Stating what is *not* protected is part of the privacy policy:

- **Metadata is not hidden.** This is inherent to peer-to-peer: the DHT and
  relay can observe *that* your peer ID connects and roughly when (never
  content), and peers you connect to directly learn your IP address, as with
  any P2P system (BitTorrent, most VoIP). Calls can opt into relay-only mode
  to hide IPs from other participants. Onion-routed discovery is future work.
- **Concord is not an anonymity tool.** It authenticates identities; it does
  not hide them. If you need anonymity, use a network layer built for it.
- **Group members are trusted.** Encryption protects the group from outsiders;
  it cannot stop a member you invited from copying or screenshotting what you
  sent them. Offline history-sync batches are attested by the serving member,
  not re-verified per original sender.
- **Your passphrase is the at-rest anchor.** If you configure convenience
  auto-unlock (e.g. storing the passphrase on disk so the app signs in at
  boot), at-rest encryption no longer protects your identity or history from
  someone who compromises that machine. That trade-off is yours to make;
  Concord's default is to ask for the passphrase.
- **Not everything on your disk is encrypted.** Two things sit beside the
  database in the clear, and a stolen or shared machine gives them up even
  though your message bodies stay sealed:
  - `peers.json` — a plaintext record of everyone this install has connected
    to: peer ID (a public key), last known addresses, last-seen time. It is
    bounded (64 entries, forgotten after a month of no contact) and holds
    nothing the network did not already see, but read as a list it is a map of
    who you talk to. It is outside the encrypted store on purpose: reconnecting
    has to work before your identity is unlocked.
  - The `mls/` directory — each group's serialized MLS state and its leaf
    private keys, written as plain files by the MLS library. Your history is
    encrypted under your passphrase; your group keys are not, so at-rest
    protection does not extend to them.
- **Verification matters.** First-contact impersonation is defeated by
  comparing key fingerprints ("safety numbers") out of band — actually do it.

## Questions

The implementation is the authoritative source: storage encryption in
`internal/store/`, identity keystore in `internal/identity/`, MLS integration
in `internal/crypto/mls/`, mailbox in `internal/mailbox/`, and the rendezvous
node in `cmd/rendezvous/`.
