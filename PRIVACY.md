# Concord Privacy Policy

*Last updated: 19 November 2026. This is the policy for the Concord application on
every platform it ships for, including the Android app.*

Concord is a peer-to-peer, end-to-end-encrypted messenger. This document states
plainly what data exists, where it lives, who can read it, and — just as
importantly — what Concord does *not* protect. [DESIGN.md](docs/DESIGN.md) covers the same
ground in architectural depth, especially
[§13 Threat model](docs/DESIGN.md#13-threat-model-what-concord-defends-against).

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

## What the people you talk to learn

- **Delivery.** In a direct message or a group DM, each of your devices tells
  the sender's devices when it has received a message. This cannot be switched
  off, and it says nothing about whether you read the message. While you appear
  invisible these notices wait, and any still waiting when the app closes are
  dropped.
- **Read receipts and typing indicators** are off by default and work both
  ways: with one off, your device neither sends it nor shows other people's.
  Read receipts exist only in direct messages and group DMs.
- These notices travel inside the encrypted conversation, so no rendezvous or
  relay learns anything new from them. See
  [`internal/app/receipts.go`](internal/app/receipts.go).

## Local-first storage

Your history lives only on your own devices — there is no server copy.
Locally it is an encrypted SQLite database:

- Message bodies are sealed with NaCl secretbox under a key derived from your
  identity seed; a stolen database file yields no readable messages. The
  structure around them is not sealed: who sent what and when, reactions,
  profiles, and guild and channel names are stored in the clear so the app can
  query and order them (see *What is not protected*, below).
- Your identity seed is itself encrypted at rest with Argon2id + secretbox,
  unlocked by your passphrase.
- Concord sends the seed off your device in one case: when you link another
  device. It goes over an encrypted connection to the device that used your
  link code, and only after you have said yes on this device to that device
  by name and six-digit code. A code works once and for two minutes. (Your
  recovery phrase is the same seed in words; it leaves only if you copy it
  out yourself.)
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
- **Its two plain-HTTPS doors serve web pages to people with no Concord
  install.** A browser guest joining a meeting gets a page that encrypts the
  guest's chat, name and call setup to the host's app with a secret from the
  link that the node never receives, so the node relays only ciphertext and
  sees the guest's IP, timing, and frame sizes rounded to 256 bytes. Because
  the node serves that page, an operator who served a modified copy could read
  that guest's view of the meeting. A visitor booking a slot on a
  `/book/<token>` link sends their IP, the slot, their name and their note
  through the node in the clear, and sees the host's name, blurb and free
  times; only the meeting link the host returns is encrypted to the visitor's
  page, and that holds only against a node that reads without altering the
  exchange. It stores none of it.
- **The optional TURN relay for calls** exists so call participants can hide
  their IP addresses from *each other*: meetings use it whenever the node runs
  one, and any call does with *Hide my IP on calls* switched on. Media through it
  stays DTLS-SRTP encrypted end to end.
- You can self-host the whole thing.

## The outbound calls that are not to your peers

Nearly every connection the app makes is to your peers or to whatever rendezvous
node you configure — but *nearly* is not *only*. Naming the exceptions is the
point of this section. This list is exhaustive.

### One that can happen without you asking

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

It cannot be switched off from the UI today.

**A call contacts no third party unless you ask for it.** To connect two people
who are both behind home routers, each side has to learn its own external
address when a call starts. The app used to ask a public STUN server
(`stun.l.google.com`) for it, and the rendezvous this project ships listed the
same server, so every call start told that server the caller's IP. Neither
happens now. If your rendezvous runs its own STUN responder
(`CONCORD_STUN_PORT`), it answers the question and nobody else is asked. If it
does not, nobody is asked: the call connects directly when one of you can be
reached as you are, goes through your rendezvous's relay when it has one, and
otherwise does not connect. **Settings → Privacy & safety → No third-party call
servers** is on by default; switching it off brings the public server back for
that device, and only when your rendezvous has no responder of its own. It then
learns your IP and that you are starting a call — no media, no identity, no
group. A browser guest on a meeting link never asks it. See
[`frontend/src/lib/callice.js`](frontend/src/lib/callice.js),
[`cmd/rendezvous/turn.go`](cmd/rendezvous/turn.go) and
[`cmd/rendezvous/stun.go`](cmd/rendezvous/stun.go); DESIGN.md covers it in
architectural terms ([§9.2](docs/DESIGN.md#92-ip-privacy-the-state-of-it)).

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

### Pictures from other sites, and link cards

Game box art (`cdn.*.steamstatic.com`) and YouTube videos have the same shape:
rendering someone else's message would otherwise make your device fetch
something from a third party, telling them your IP and the moment you were
online with no click at all. Box art is **off by default** (*Privacy & safety →
Game box art*). A YouTube video loads, from `youtube-nocookie.com`, only when
you tap play. See
[§12 of DESIGN.md](docs/DESIGN.md#12-assets-why-nothing-is-fetched-at-runtime).

Link cards are made by the **sender**, never by the people who read them.
*Privacy & safety → Preview cards for links you send* is **off until you
choose**: the first link you send goes out without a card, and the app asks
once, unless you have already set the switch. The choice is kept on each
device. With it on, your device looks up the first link in a message once,
when you send it: YouTube's or Vimeo's oEmbed service and the server
of the video's picture for their videos, otherwise the page and the host of its
picture. That site learns your IP
address and the moment you sent the link, as if you had opened it. The title,
the site's name and a small copy of the picture, redrawn as a new JPEG of at
most 480 by 270 pixels, go inside your encrypted, signed message; a
disappearing message gets a card without a picture. Readers contact nobody:
the picture reaches them from other members like any attachment, and a reader
on Concord 0.60 sees the card as a titled link, its domain and a captioned
picture. Nothing is fetched while you type. The old receive-side *Link
previews* setting, which fetched every link that scrolled into view, is gone.
See [`internal/app/linkcard.go`](internal/app/linkcard.go) and
[`frontend/src/lib/linkcard.js`](frontend/src/lib/linkcard.js).

## What a picture or file you send carries

Before a picture leaves your device, Concord removes what is stored beside the
picture data: location, camera make and model, dates, embedded thumbnails, XMP
and IPTC data, comments and maker notes. It keeps the colour profile and, when
the picture is not upright, a tag saying which way up it goes. This applies to
PNG, JPEG, GIF and WebP pictures sent by any route, and to avatars, banners,
guild icons, forum banners, stories, custom emoji and guild GIFs. A picture in
another format, HEIC and AVIF among them, is redrawn as a JPEG when the app can
display it, which leaves its details behind; when it cannot, it is sent as a
file and cleaned as below. See [`internal/imgmeta/`](internal/imgmeta/imgmeta.go) and
[`internal/app/imageclean.go`](internal/app/imageclean.go).

MP4 and MOV videos, HEIC, HEIF and AVIF pictures sent as files, and M4A audio
lose their location, camera make and model, dates and other stored details
before they are sent. A video also loses its tracks that are neither picture
nor sound, such as the per-frame location some phones and action cameras
record, and its timecode. Music keeps its title, artist and album; a voice
recording loses its title too, since phones often name recordings after the
place. Only the file's index is rewritten: the picture and sound themselves are
not read or changed, so a location a camera writes into the video stream itself
stays. A video with subtitles or other text tracks, which can hold location, is
held until you choose *Remove and send* or *Send as it is*. A file Concord
cannot clean safely is held until you choose *Send as it is*, which sends it
unchanged: one of the formats above that it cannot rewrite with certainty, a
video in another container (MKV, WebM, AVI, MPEG transport or program stream,
WMV, FLV), or a TIFF, camera raw, JPEG XL or JPEG 2000 picture. See
[`internal/bmffmeta/`](internal/bmffmeta/bmffmeta.go) and
[`internal/app/attach.go`](internal/app/attach.go).

Documents, audio files other than M4A and WebM (MP3, Ogg, WAV, FLAC and the
like) and any file Concord does not recognise are sent as they are, with
whatever they hold; the composer says so under such a file before you send it.
An attached WebM or other Matroska file is held like a video in a container
Concord does not read, even when it holds only sound. A voice message recorded
in Concord is sent as it is when the recording is Ogg or WebM audio, with no
line under it; any other recording is cleaned or held as an attached file would be. A chat export imported into a guild has its PNG, JPEG, GIF and WebP pictures
cleaned in the same way as a sent picture, and its other files, HEIC and AVIF
pictures and videos among them, go to the guild as they are. Pictures and videos
sent before Concord 0.61 keep their details on every device that holds them.

On Android, the attach sheet opens the system photo picker. Concord asks for no
access to your photo library and receives only the photos and videos you pick.

## How long anything is kept

There is no retention schedule to publish, because there is nobody holding a
copy to schedule. Your history is kept on your device until you delete it, and
on the devices of the people you sent it to until *they* delete it. Three things
shorten that on purpose:

- **Disappearing messages.** You can set a timer on a conversation, and your
  own devices share it. Every message you send there, pictures and files
  included, carries the timer inside its encrypted content, so every device
  erases the body at the same wall-clock instant without coordinating.
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
and the plaintext peer cache. Three things outside those survive it: the
rendezvous address you configured (`netconfig.json`), the record of a verified
release download (`release.json`), and what the interface keeps in its own
browser storage — display preferences, plus the text of any unsent draft or
failed send. Nothing about you is retained anywhere else, because there is
nowhere else for it to be retained. An erase that is interrupted finishes the
next time Concord starts.

Smaller erasures, for when the whole device is not what you meant:

- **One message, for everyone**: delete it, and the deletion travels to the
  group like any other message. In a direct message or a group DM every device
  then erases the text. In a guild only the devices of members with *Manage
  messages* keep it, still sealed, so that a moderator can reveal it; every
  other device erases it. The message's earlier versions, reactions, receipts,
  bookmark and pin go with it, and the database overwrites the space it freed.
  Its notification is withdrawn wherever it is still shown. A device running
  Concord 0.60.x keeps the sealed text of a deleted guild message. A device that
  ran 0.60.5 also keeps, after updating, the text of guild messages deleted while
  it ran 0.60.5, until its trash is emptied. See [`internal/app/guild.go`](internal/app/guild.go)
  (`erasesDeletedBodies`) and [`internal/store/store.go`](internal/store/store.go)
  (`markDeleted`).
- **A deleted message's picture or file**: a device that erases the message's
  text (a delete for everyone, a message that disappears, *Empty trash*) also
  removes its encrypted copy and stops passing it to others. A moderator's
  device that keeps a deleted guild message for reveal keeps its pictures until
  its trash is emptied, and does not pass them on. A device passes a picture or
  file only to members of a guild or direct message where a message that is not
  deleted names it, or whose GIF pack or archive holds it. Two kinds of copy are
  left: a device on Concord 0.60.x keeps, and passes to anyone who asks for it by
  its id, the copy of a deleted message's attachment until it updates; and a copy
  whose message text 0.60.5 already erased stays in that device's cache until
  the cache needs the room, without being passed on. Neither can be opened
  without the key, which was in the erased text. See
  [`internal/app/blobrelease.go`](internal/app/blobrelease.go) and
  [`internal/store/blobrefs.go`](internal/store/blobrefs.go).
- **One message, for you only**: *Delete for me* hides it on your own devices,
  and nobody else is told. The text and any attachment stay on your devices,
  sealed, so that *Undo* can bring them back.
- **Every deleted message's retained text** on this device: *Privacy & safety →
  Empty trash*.
- **One linked device**: *Settings → Your devices*, then *Unlink* beside it. It
  is removed at once from the guilds and direct messages where you can remove
  members; elsewhere it stays listed, and can still receive
  new messages there, until someone who can remove it there does.
  It erases its own copy once it is unlocked and reaches the device that
  unlinked it. A device that stays switched off keeps what it had.

What deletion cannot do, and no feature of any peer-to-peer messenger can:
**make sure the copies other people already have are gone.** A message you sent
is on the recipient's device, sealed to them. A deletion asks that device to
erase the text, and an up-to-date Concord does; a modified app, a screenshot, a
file already saved or a backup made earlier keeps it. An edit does not take back
the earlier text either: each device that received a message keeps its earlier
versions, sealed, until the message is deleted. There is no server holding the authoritative
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
chooses to send — so it is rated accordingly, and the app provides blocking,
message requests that hold a stranger's first DM unopened, and a *Report
message* dialog. That dialog sends nothing to anyone, because there is no
moderation team to receive it: it hides the message, blocks its sender, or
saves an evidence file on your device for you to hand to someone who can act.
Removing a member or a message for everyone is up to that guild's own admins. If
you are responsible for a young person's device, treat Concord as you would any
other open communication tool.

## What is not protected

- **Metadata is not hidden.** This is inherent to peer-to-peer: the DHT and
  relay can observe *that* your peer ID connects and roughly when (never
  content), and peers you connect to directly learn your IP address, as with
  any P2P system (BitTorrent, most VoIP). Calls can opt into relay-only mode
  to hide IPs from other participants, which works only when your rendezvous
  runs a TURN relay. Onion-routed discovery is future work.
- **Concord is not an anonymity tool.** It authenticates identities; it does
  not hide them. If you need anonymity, use a network layer built for it.
- **Group members are trusted.** Encryption protects the group from outsiders;
  it cannot stop a member you invited from copying or screenshotting what you
  sent them. Messages that arrive through offline history sync are checked
  against their author's own signature; one with no signature (written before
  signatures existed, or served by an older build) is kept but shown as
  unverified. The guild snapshot, member profiles and events in a sync batch
  are still taken on the serving member's word.
- **Your passphrase is the at-rest anchor.** If you configure convenience
  auto-unlock (e.g. storing the passphrase on disk so the app signs in at
  boot), at-rest encryption no longer protects your identity or history from
  someone who compromises that machine. That trade-off is yours to make;
  Concord's default is to ask for the passphrase.
- **Not everything on your disk is encrypted.** Inside the database, only
  message bodies and their earlier versions, attachments and story captions are
  sealed; senders, timestamps, reactions, delivery and read receipts, the
  messages you deleted for yourself, your pinned and archived chats and their
  timers, which messages carry a photo, a video or a file, profiles, guild and
  channel names, the governance log and settings are in the clear. Unsent drafts and failed sends wait in the interface's own
  browser storage, unencrypted. And two things sit beside the database in the
  clear, which a stolen or shared machine gives up even though your message
  bodies stay sealed:
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
