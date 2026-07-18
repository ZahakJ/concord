# Concord Privacy

Concord is a peer-to-peer, end-to-end-encrypted messenger. This document states
plainly what data exists, where it lives, who can read it, and — just as
importantly — what Concord does *not* protect. The [README](README.md) covers
the same ground in architectural depth (see especially
[§13 Threat model](README.md#13-threat-model--what-concord-defends-against));
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
- **The TURN relay for calls** exists so meeting-link participants can hide
  their IP addresses from *each other*; media through it stays DTLS-SRTP
  encrypted end to end.
- You can self-host the whole thing.

## The optional AI assistant

Concord ships an assistant that can summarize a channel ("catch me up"), draft
a reply, and widen a search with related terms. It is **off by default** and
does nothing until you switch it on.

With it on, the default path is strictly on-device: it talks to an
[Ollama](https://ollama.com) server on `127.0.0.1`, and that restriction is
structural rather than a policy — a non-loopback endpoint is rejected both when
you configure it and again on every call, so there is no configuration, and no
hand-edit of the settings database, in which assistant traffic leaves your
machine. The model reads exactly the messages your own screen already shows,
decrypted with your own key. Machine-to-machine app traffic is excluded, and
nothing about the assistant touches the MLS or transport layers.

### The "shared brain" — a second, separate opt-in

Drafting a reply is the one job a small local model is genuinely bad at, so it
can optionally be routed to a **shared brain**: a Claude Code session running
as a local process on this machine, on your own Claude subscription, reached
through Aether's job queue. There is no API key and no metered spend, and the
job does not go to any third-party service you are not already signed in to.

**But Claude does see the message content in that request.** That is a real
difference from the Ollama path, where the bytes never leave the box, and it is
the one place where Concord's "nothing leaves this machine" promise is narrower
than everywhere else. So:

- It is **off by default**, and turning the assistant on does not turn it on.
- It requires the assistant's own consent toggle *and* a separate opt-in.
- It affects **only** the draft-reply feature. Catch-up summaries and search
  expansion always stay on the local model.
- Every assistant answer **names the engine that produced it** in the UI. A
  local answer never claims to have come from the brain.
- `CONCORD_BRAIN=off` in the environment pins the machine local-only and
  overrides the in-app toggle.

#### Exactly what is on by default, and what still asks

One brain-related thing *is* on by default, and it is worth being precise about
which:

| Surface | Default | What leaves this machine |
| --- | --- | --- |
| **Brain discovery** (is a brain present, is a session attached, how deep is the queue) | **ON** | Nothing. Concord asks the local Aether process and gets back two booleans and an integer. No message text, no channel or contact names, no store-derived metadata, and no network connection — Aether is another process on the same box. |
| **Drafted replies via the brain** | **OFF** — two explicit opt-ins | The decrypted conversation excerpt. Claude reads it. |
| **Catch-up summaries** | Local model only; never routed | Nothing. |
| **Search expansion** | Local model only; never routed | Nothing. |
| **App data-plane payloads** (`kind: "app"`) | Never routed to the brain | Nothing. |

Discovery is on because gating it bought no privacy — it moves no user content
— while costing you the ability to learn the feature exists. **Knowing a brain
is there is not consent to use it.** Discovery never enqueues a job, and both
gates still stand between a discovered brain and a single byte of conversation.
`CONCORD_BRAIN=off` short-circuits discovery too, so it remains a complete kill
switch rather than a routing-only one.

Nothing that carries message content is on by default, and nothing that was
consent-gated before is un-gated now. Concord is an end-to-end-encrypted chat
app; message bodies are the thing it exists to protect, and no default will ever
send one anywhere on your behalf.

#### When the brain isn't there

The brain is a Claude Code session, and sessions end — the machine reboots, the
session is closed, or it hits its usage limit mid-week. Every one of those looks
the same from Concord's side, and the behaviour is the same in all of them: the
draft is written by your local model and **labeled local**. Concord never
fabricates a brain answer, never presents a local answer as a brain one, never
silently drops the request, and never blocks waiting on a brain that isn't
coming. An accepted job that is merely still queued is shown as queued, with a
note that you can close it and use the local model instead.

If Aether isn't installed, isn't running, or has no session attached, the
feature degrades quietly to the local model — it never blocks and never fails.

Implementation: `internal/assist/` (local models, loopback enforcement),
`internal/brain/` (the queue client), `internal/app/assist.go` (routing).

## No telemetry

Concord contains no analytics, no crash reporting, no telemetry, and no
phone-home of any kind. There are no accounts, so there is nothing to sign up
for and no email or phone number to collect. The only network connections the
app makes are to your peers and to whatever rendezvous node you configure.

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
- **Verification matters.** First-contact impersonation is defeated by
  comparing key fingerprints ("safety numbers") out of band — actually do it.

## Questions

The implementation is the authoritative source: storage encryption in
`internal/store/`, identity keystore in `internal/identity/`, MLS integration
in `internal/crypto/mls/`, mailbox in `internal/mailbox/`, and the rendezvous
node in `cmd/rendezvous/`.
