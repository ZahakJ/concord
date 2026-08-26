# Concord Privacy Policy

*Last updated: 26 August 2026. This is the policy for the Concord application on
every platform it ships for, including the Android app.*

Concord is a peer-to-peer, end-to-end-encrypted messenger. This document states
plainly what data exists, where it lives, who can read it, and — just as
importantly — what Concord does *not* protect. The [README](README.md) covers
the same ground in architectural depth (see especially
[§13 Threat model](docs/DESIGN.md#13-threat-model-what-concord-defends-against));
this page is the short, honest version.

## What the developer collects: nothing

There is no exception to hedge and no "except as described below". Concord has
no company behind it, no backend of its own, and no account you can be a row in.

- **No analytics, no crash reporting, no advertising, no attribution.** Not
  disabled by default — absent. The Android app's entire third-party dependency
  list is [`apps/mobile/package.json`](apps/mobile/package.json): Capacitor
  itself, four of its plugins (app lifecycle, haptics, keyboard, push) and a
  biometric one. The web UI's is
  [`frontend/package.json`](frontend/package.json): an emoji set and two QR-code
  libraries. Nothing in either reports anything anywhere, and no advertising or
  attribution identifier is read.
- **The Firebase library ships but does not start.** Push notifications are
  written and unprovisioned: the push plugin brings Firebase Cloud Messaging
  with it, but there is no Firebase project behind it and no configuration file
  in the build, so its initialisation is switched off outright and nothing
  registers, reports or wakes. If push is ever configured, the wakes it carries
  are contentless by design (see below) and
  [docs/PUSH.md](docs/PUSH.md) documents exactly what a node would learn.
- **No accounts.** There is nothing to sign up for, so there is no email
  address, no phone number and no name held anywhere but on your own device and
  on the devices of the people you talk to.
- **No default server.** Concord ships pointing at nothing. Any rendezvous node
  you use is one you or a friend configured, and it is untrusted by
  construction (see below). The project operates no infrastructure that your
  copy of the app connects to automatically.
- **No device or advertising identifiers** are read or transmitted. Your
  identity is a keypair you generated.

Everything in the rest of this document is about data that stays on your device,
data that goes to people you are talking to, and a short, named list of
connections that go somewhere else — each of which is listed with what it
reveals and, where one exists, the switch that turns it off.

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

## The outbound calls that are not to your peers

Nearly every connection the app makes is to your peers or to whatever rendezvous
node you configure — but *nearly* is not *only*. Naming the exceptions is the
point of this section. This list is exhaustive.

### Two that can happen without you asking

- **`api.github.com`, once at launch, on desktop and web only.** The app asks
  the project's own repo (`ZahakJ/concord`) for its latest release tag so it can
  tell you an update exists. It is unauthenticated, sends no identifier beyond
  what any HTTP request carries — your IP and a `concord-updater` user agent —
  and any error is a silent no-op. Builds without a release version stamp (i.e.
  built from source) skip the request entirely.

  **The mobile app never makes this request automatically** (the launch check is
  skipped whenever the app is running under Capacitor). A *sideloaded* Android
  build offers a manual "Check for updates" button, because someone who
  downloaded an APK from a GitHub release has no other route to a fix; nothing
  happens until they press it. A build **installed from Google Play does not
  offer it at all** — the store owns updates there, and the app asks the running
  install where it came from to decide. See
  [`internal/bridge/update.go`](internal/bridge/update.go) and
  [`frontend/src/lib/installsource.js`](frontend/src/lib/installsource.js).
- **`stun.l.google.com`, at call time only.** Starting a call needs to learn
  your own external address. If the rendezvous you bootstrap through serves ICE
  configuration, that is used and Google is never contacted; only when it does
  not is there a fallback to this hardcoded public STUN server. It learns your
  IP and that you are starting a call — no media, no identity, no group, and
  nothing at all if you never place a call. See
  [`internal/app/ice.go`](internal/app/ice.go).

Neither can be switched off from the UI today. The README covers the same two
in architectural terms ([§1](docs/DESIGN.md#1-the-problem-and-the-thesis),
[§9.2](docs/DESIGN.md#92-ip-privacy-the-state-of-it)).

### Two searches that are yours to switch on or off

These are the only two features where something you type is sent to a search
service. Both are switches in *Settings → Privacy & safety*, and both are
enforced in the backend rather than in the interface: switched off, the request
is not made, not merely not displayed.

- **Game title search → Valve (`store.steampowered.com`), OFF by default.**
  The collection editor can suggest real game titles from Steam's public
  storefront search, and it asks again as you type — so a half-typed title, your
  IP, and the fact that you were online reach Valve. The query goes out from the
  Go backend rather than the webview, but the backend runs on your machine, so
  it is your IP either way. Switched off, the editor still works: type a title
  and it is added as you wrote it, and nothing leaves the device. An install
  that already had a game collection when this switch was introduced keeps it
  on, because it had been using the feature; every other install starts closed.
  See [`internal/app/games.go`](internal/app/games.go) and
  [`internal/app/offdevice.go`](internal/app/offdevice.go).
- **GIF search → your own rendezvous, ON by default.** The GIF picker's search
  tab sends what you type to the rendezvous node *you* configured. That node
  asks a public GIF service on your behalf — Giphy in the node this project
  ships, though the operator chooses, and the node names its provider in every
  reply rather than the app assuming one — and sends the pictures back through
  itself. The service therefore sees the node's IP and never yours, and your
  browser never connects to it: a search result carries an opaque handle rather
  than an address, and every thumbnail arrives as inline data from the Go side.
  **What it costs is that the operator of your rendezvous can see your search
  terms.** It is on by default because it reaches exactly one machine you had
  already chosen to route your traffic through, and off is one switch away if
  that trade is not one you want with whoever runs your node. A guild's own GIF
  pack is unaffected either way — it never leaves your machine. See
  [`internal/app/gifsearch.go`](internal/app/gifsearch.go) and
  [`cmd/rendezvous/gifsearch.go`](cmd/rendezvous/gifsearch.go).

### Three more that load a picture, all off by default

Game box art (`cdn.*.steamstatic.com`), link previews, and YouTube embeds all
have the same shape: rendering someone else's message would otherwise make your
browser fetch something from a third party, telling them your IP and the moment
you were online with no click at all. So all three are **off by default**, with
a switch each — *Privacy & safety → Game box art*, *→ Link previews*, and the
per-embed click-to-load. See
[§12 of DESIGN.md](docs/DESIGN.md#12-assets-why-nothing-is-fetched-at-runtime).

## How long anything is kept

There is no retention schedule to publish, because there is nobody holding a
copy to schedule. Your history is kept on your device until you delete it, and
on the devices of the people you sent it to until *they* delete it. Three things
shorten that on purpose:

- **Disappearing messages.** An author can put a timer on a message. The timer
  travels inside the encrypted content, so every device erases the body at the
  same wall-clock instant without coordinating.
- **A guild retention policy.** A guild can set how long messages are kept, per
  guild or per channel, and each member's app prunes its own copy when it comes
  around to it. This is local enforcement and nothing else: two members with
  different uptimes forget at slightly different moments, and a member running a
  modified client need not forget at all. The interface says so beside the
  switch. See [`internal/app/retention.go`](internal/app/retention.go).
- **The offline mailbox.** A deposit waiting at a rendezvous is an encrypted
  blob addressed to an opaque tag; it is drained and deleted when you next
  connect, and nodes expire undrained deposits on their own schedule.

## Deleting your data

**In the app: *Settings → Privacy & safety → Delete everything on this device*.**
It asks you to type your profile name, and then erases your identity keystore,
the encrypted database holding every message stored here, the MLS group state,
and the plaintext peer cache. The app is left exactly as it was before you first
opened it. Nothing is retained, anywhere, for any period, because there is
nowhere for it to be retained.

Smaller erasures, for when the whole device is not what you meant:

- **One message**, for everyone: delete it, and the deletion travels to the
  group like any other message.
- **Every deleted message's retained text** on this device: *Privacy & safety →
  Empty trash*.
- **One linked device**: *Settings → Devices → Unlink*, which tells that device
  to erase itself.

What deletion cannot do, and no feature of any peer-to-peer messenger can:
**reach the copies other people already have.** A message you sent is on the
recipient's device, sealed to them. There is no server holding the authoritative
copy that could be told to forget, which is the same fact that makes the rest of
this document true.

Because there is no account, there is also no "delete my account" request to
send anyone, and no data-subject request the developer could answer if you made
one — we hold nothing about you to disclose, correct or erase.

## Children

Concord is a general-audience communication app. It has no age gate, because it
has no accounts and collects no personal information from anyone, of any age, to
gate. It is not directed to children, and it is not designed, marketed or
featured for them.

It does carry user-generated content — anything a member of a guild you join
chooses to send — so it is rated accordingly, and the app provides blocking, a
report flow, and message requests that hold a stranger's first DM unopened. If
you are responsible for a young person's device, treat Concord as you would any
other open communication tool.

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

## Changes to this policy

This document lives in the repository next to the code it describes, so its
history is the change log: every revision is a commit, with a message saying
what changed and why. The date at the top is the last substantive revision.

A change that alters what leaves your device — a new outbound connection, a
default flipped from off to on — will land in the same release as the change
itself and be named in that release's notes, not slipped in quietly.

A published copy of the same policy, for anyone who would rather not read a
repository, lives at <https://zahakj.github.io/concord/privacy.html>
([`docs/privacy.html`](docs/privacy.html)). This file is the authoritative one;
the page carries the same text without the per-claim links into the source.

## Questions and contact

There is no support address, because there is no company to staff one. Both
routes are the repository:

- **Anything about this policy, or the app in general:**
  <https://github.com/ZahakJ/concord> — open an issue.
- **A security or privacy vulnerability:** use GitHub's private vulnerability
  reporting (the *Security* tab on the repository, then *Report a
  vulnerability*), which opens a private thread. [SECURITY.md](SECURITY.md) has
  the scope, the response times, and what is already a documented known gap.
  Please do not post a vulnerability as a public issue.

The implementation is the authoritative source, and is meant to be read as one:
storage encryption in `internal/store/`, identity keystore in
`internal/identity/`, MLS integration in `internal/crypto/mls/`, mailbox in
`internal/mailbox/`, the two search switches in `internal/app/offdevice.go`, the
device erase in `internal/bridge/wipe.go`, and the rendezvous node in
`cmd/rendezvous/`.
