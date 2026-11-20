# Browser guests, end to end

Status: design for 0.61, revised after a security review (changes adopted are
listed at the end). Supersedes the guest-leg description in DESIGN.md §10.

## The flaw

A browser guest's WebSocket ends at the rendezvous, which pipes it into a
libp2p stream to the meeting host. The frames inside were plain JSON. While a
guest was in a meeting the operator could read every member message fanned out
to the guest, the last 30 messages replayed on arrival, the guest's own
messages, and the WebRTC SDP (which carries the DTLS fingerprints, so an
operator who also runs the TURN relay could swap them and sit in the middle of
the call). The link token travelled in the hello frame, so the operator could
replay it and walk in as a guest; links were multi-use and an instant meeting
had no revoke.

## Goal and threat model

The rendezvous operator learns nothing but ciphertext and metadata. Metadata:
the host's peer id; an opaque link id; the guest's IP; `/turn` fetches and the
TURN allocation; timing; frame count; record sizes in 256-byte buckets (so an
SDP exchange or a history burst is recognisable by shape, not content). The
TURN relay sees DTLS-SRTP packets and their timing.

The design holds against an operator who reads or records everything it
relays and who drops, reorders, replays or injects frames. It does not hold
against an operator who serves a modified guest page: the page is served
`no-store` by the rendezvous, so a modified copy can be served to one IP, once,
without trace, and it can read the fragment. Whoever terminates TLS in front of
the gateway (fly.io for the default deployment) has the same power. If that
happens, the exposure is everything the guest sees: members' messages in that
meeting while the guest is present, the call, and history if the host enabled
it. The user docs say this, and the meeting dialog says it where the link is
minted. (Follow-up, not 0.61: serve the page from a second origin.)

## Link

    https://<gateway>/guest#h=<host peer id>&k=<S>

`S` is 32 random bytes, base64url, from the CSPRNG; minting fails rather than
fall back to anything weaker. It lives only in the fragment and is never put on
the socket. HKDF-SHA256, empty salt:

- `linkId = HKDF(S, "concord guest v2 link id", 16)`, sent in clear for lookup.
- `K = HKDF(S, "concord guest v2 link key", 32)`.

The host stores `{S, guild, channel, expires, history, maxGuests, guests[],
banned[]}` under the `guest.links` setting. Note: settings are not encrypted at
rest (privacy.md), so `S` has the same at-rest exposure the old bearer token
had; a device thief could join as a guest until expiry. Unchanged by this
work; noted in privacy.md.

## Handshake and records

1. Guest → host, clear: `{"type":"hello","v":2,"link":linkId,"gn":<16 bytes>,
   "gpk":<ephemeral P-256 public key, raw uncompressed>}`.
2. Host → guest, clear, always (known link or not, live or not):
   `{"type":"chal","hn":<16 bytes>,"hpk":<ephemeral P-256 public key>}`.
   Nonces and ephemeral keys are fresh per connection and never stored. The
   operator cannot tell a live link from a dead one here.
3. `Z = ECDH(ephemeral keys)`. Keys, with `ikm = K ‖ Z`, `salt = gn ‖ hn` and
   `ctx = 0x00 ‖ "v2" ‖ 0x00 ‖ linkId ‖ 0x00 ‖ hostPeerId ‖ 0x00 ‖ gpk ‖ 0x00 ‖
   hpk` (keys base64url):
   `k_g2h = HKDF(ikm, salt, "concord guest v2 g2h" ‖ ctx, 32)`,
   `k_h2g = HKDF(ikm, salt, "concord guest v2 h2g" ‖ ctx, 32)`.
   `K` authenticates (only a holder of the link can complete step 5); `Z`
   gives forward secrecy: a link that leaks later (from an email, an event
   card, the host's disk) does not decrypt recorded sessions. For an unknown
   link the host uses a random `K`, so it behaves the same and fails at step 5.
4. Every later frame is `{"type":"e","c":base64url(AES-256-GCM(k_dir,
   nonce = 4 zero bytes ‖ 64-bit big-endian counter, plaintext))}`. Counters
   start at 0 per direction and the receiver demands exactly the next value.
   Plaintext is the JSON frame's UTF-8 bytes, right-padded with spaces to a
   multiple of 256. In the page, seal and open run through two promise queues
   so async completion cannot reorder counters.
5. The guest's first record is `{"type":"join","name":…,"gid":…}`, proving
   possession of `K`. If it fails to open, or the link is unknown, expired or
   revoked (revoked links are deleted, so they are unknown), the host closes
   the stream and sends nothing, so every failure looks the same on the wire.
   The page says "This link no longer works" when the socket closes before a
   welcome. No slot, knock or notice is spent. A recorded handshake replayed
   later fails here because `hn` and `hpk` are new.
6. After `chal`, both sides drop any frame that is not `e`. A clear frame
   before `chal` (only the gateway's "host unreachable") carries a code; the
   page shows its own text for known codes and a generic line otherwise,
   never the frame's prose.

Pre-join handshakes are capped (8 per link, 64 per node) and do not count
against the five-guest cap. Frame caps rise from 32 KiB to 64 KiB at host and
gateway for base64 and padding on large SDP.

WebCrypto exists only in secure contexts. The production gateway is HTTPS and
`http://localhost` qualifies. Elsewhere the page refuses locally and opens no
socket. A link in the old format (`#t=`) is refused locally too, and the token
is never sent.

## Who encrypts

The host's node is the guest's only endpoint. It is an MLS member, decrypts
member messages and seals what it forwards under `k_h2g`; it opens what the
guest sends and posts it into the group under its own key, labelled
"(guest)". Other members never talk to the guest: their messages reach the
host as MLS ciphertext. On every hop the rendezvous carries MLS ciphertext or
guest-session ciphertext. The host can read the guest's messages, as before.

## Limited use, expiry, revoke, removal

- Expiry: unchanged (the meeting's lifetime, checked at the door and polled).
- Limited use: a link admits at most `maxGuests` distinct guests (5 by
  default; 2 for booking rooms). The page keeps a random `gid` per link in
  `localStorage`, falling back to `sessionStorage`, then memory. A new `gid`
  is refused before the knock when the link is full and counted on admission.
  A known `gid` only avoids using up a slot; it never skips the knock.
- Removal: "Remove from call" on a guest bans that `gid` for that link, so the
  same browser cannot walk back in. (Someone holding the link can clear their
  storage and use a fresh slot; the slot limit and revoke cover that.)
- Revoke: `RevokeGuestLink(guild)` deletes the link and ends every session in
  that meeting at once, including knocking and pre-join ones, with a sealed
  `end`. The session's call is left, the host's mesh closes the
  RTCPeerConnection, and media stops. The link is re-checked after `join` and
  again at admission, under the same lock revoke takes, so a revoke during the
  handshake or the knock wins. The next Copy link mints a fresh `S`. Offered in
  the meeting dialog (reached also from the call's Meeting menu → Meeting
  details).
- Scheduled rooms: `RevokeEventGuestLink(guild, event)` works only on the node
  that hosts the room (`GuestHost`), because the room and `S` live there.
  It revokes, mints a fresh link and stamps it on the event, so members' cards
  show the new one. Booking rooms are revoked by cancelling the booking, which
  now also ends live sessions.
- Old links: dropped on load. Event rooms are re-stamped with a fresh link at
  load. Outstanding booking rooms are re-minted at load and the host gets a
  room notice in Notes that the visitor's link changed and must be re-sent;
  the host has the visitor's name and note there.

## History

Default: none. A guest sees only what is said after admission. The host can
turn on "Guests can read the last 30 messages" per link in the meeting dialog
(`SetGuestLinkHistory`). Event rooms still show the event's details: rendered
from the event record at welcome, not replayed. Guest presence notices stay
filtered.

## Booking

The booking link is published, so its token is public by design and gives the
operator nothing it could not get by visiting it. What must not leak is the
meeting URL the host hands back. The page sends an ephemeral P-256 public key
with `book`; the host replies with its ephemeral key and
`nonce(12, random) ‖ AES-256-GCM(HKDF(ECDH, salt = epk ‖ spk, "concord booking
v1 seal"), {meetingURL, ics})`. A `book` without a key is refused. This holds
against a passive operator only: the keys are not authenticated, so an
operator that rewrites the exchange (or serves a modified page) gets the
meeting URL; it already knows the visitor's name, so it could knock as the
visitor and take one of the room's two slots. The host should treat a booking
knock as unauthenticated, and the docs say so. (Follow-up: the host signs
`epk ‖ spk` with its libp2p key and the page checks it against `h`.) Still in
clear, and documented: the host's display name, blurb
and free/busy slots, the visitor's IP, name and note.

## Call media

The guest's RTCPeerConnection is with the host's app only. Media is
DTLS-SRTP between browser and app. With TURN (meetings ask for it), media
crosses the relay as DTLS-SRTP ciphertext. The DTLS fingerprints now travel in
sealed, authenticated SDP, so the relay cannot swap them. Without a relay the
page tells the guest the call is direct.

## Tests

- Known-answer vectors (HKDF outputs, nonce layout, padding) asserted in Go and
  in the page's JS.
- Capture: a tap between `serveGuest` and a Go guest client records every
  relayed byte while a member message, a history message (history on), a guest
  message, an SDP blob, the guest's name, the meeting name, the channel id and
  the `gid` each carry markers. No marker, no `S`, no `K`, no `waiting`/`end`/
  `info` text in the capture; every record's ciphertext length is a multiple
  of 256 plus 16.
- Wrong key, replayed handshake, counter skip, reflected host record, clear
  frame injected after `chal`: each closes or is ignored, both sides.
- Unknown, revoked, expired and live links all answer `chal`; with a wrong key
  all three produce the same frames (one `chal`) and then a close.
- Forward secrecy: a session recorded by the tap does not open with `S` alone.
- Limited use and a returning `gid`; kicked `gid` refused; history off by
  default and on when chosen; revoke during knock, between `chal` and `join`,
  and in the room; event revoke stamps a new URL; booking seal opens only with
  the visitor's key and fails after a key swap.
- e2e (`guest-e2e` spec): host mints a link; a guest in a second browser
  context joins through the real gateway; chat both ways; the guest joins the
  call and the host sees the peer connected; the host revokes; the guest page
  shows the end, its call ends and the host's peer connection closes; the old
  link is refused; a `#t=` link opens no socket.

## Review changes adopted

From the design review: clear frames after `chal` dropped and pre-`chal`
reasons mapped to page text (1); link re-validated after `join` and at
admission, revoke covers knocking and pre-join sessions (2); booking scoped
honestly as passive-only with every clear field documented (3; the booking
token is public, so a fragment secret buys nothing there); `gid` ban on
removal, known `gid` never skips the knock (4); residual-risk wording corrected
and shown at mint time (5); old-link handling specified (6); event revoke
limited to the hosting node (7); HKDF context binding, fresh nonces, fail-closed
RNG, byte-level padding, serialised WebCrypto queues (8); uniform `chal`, capped
pre-join handshakes (9); fuller doc and metadata list (10); tests (11).
Second round: silent close on every failed join (no liveness oracle);
ephemeral ECDH mixed into the session keys for forward secrecy; booking's
active-attack consequence stated; the at-rest comment on `guest.links` made to
agree with privacy.md. Optional suggestions taken: CSP `connect-src 'self'`, storage fallback, a
no-relay notice. Deferred: sealed keepalive, host-settable `maxGuests`.
