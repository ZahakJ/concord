# Protocol and data model

The libp2p protocols Concord speaks, what travels on each, how peers catch up on history, and how a peer stores what it has seen. [DESIGN.md](../DESIGN.md) has the reasoning; this page is the map.

## Two lanes: gossip and streams

Peers exchange data in two ways.

- **Gossipsub topics** carry live group traffic: every member of a guild subscribes to the guild's topics and publishes MLS ciphertext to them. A message reaches everyone online at that moment, and nobody else.
- **Stream protocols** carry anything point-to-point: a join handshake, a history catch-up, an attachment download, a call setup. Each is a libp2p stream protocol under `/concord/`.

A peer that missed gossip recovers it through the `sync` stream, or from the mailbox on the rendezvous if no member was online to ask.

## Gossipsub topics

Topic names are derived in `internal/domain/model.go`. Each is `concord/c/` followed by the first 16 bytes, in hex, of a SHA-256 over the MLS group ID and a suffix, so a topic name reveals neither the guild nor its group ID to the pubsub network, while every member derives the same value.

| Topic | Derived from | Carries |
|---|---|---|
| Channel | group ID, `0x00`, channel ID | Chat messages and message actions for one channel |
| Control | group ID, `/control` | MLS commits: membership changes and epoch advances |
| Meta | group ID, `/meta` | Guild metadata updates and governance ops |
| Typing | group ID, `/typing/`, channel ID | Ephemeral typing signals, never stored |
| Voice | group ID, `/voice/`, channel ID | Join and leave announcements for a voice room |

Channel and meta topics carry MLS ciphertext. Commits travel in the clear on the control topic; MLS authenticates and orders them on apply. A typing signal is a single byte, with the sender's peer ID as the whole message. Voice announcements are small unencrypted JSON records naming the channel and the action (`join` or `leave`), so their content is readable by anyone subscribed to the topic, though the topic name itself is only derivable from the group ID.

## Stream protocols

Unless noted, a stream carries one request frame and one response frame, each a 4-byte big-endian length followed by the bytes (`writeFrame` and `readFrame` in `internal/net/invite.go`). The `internal/net` layer treats frames as opaque; the JSON shape and the crypto are defined in `internal/app`.

| Protocol ID | Defined in | Carries |
|---|---|---|
| `/concord/invite/1.0.0` | `internal/net/invite.go` | The guild join handshake: the joiner sends its MLS KeyPackage, the inviter answers with an MLS Welcome and guild metadata |
| `/concord/dm-invite/1.0.0` | `internal/net/dm.go` | Pushes a DM invitation to a peer. Starting a DM creates a new MLS group and this stream hands the recipient the invite code, which its client redeems over `invite` |
| `/concord/sync/2.0.0` | `internal/net/sync.go` | History catch-up (below). v2 added commit backfill and state snapshots and is not wire-compatible with v1 |
| `/concord/hello/1.0.0` | `internal/net/hello.go` | Identity introduction: a device presents the account-signed certificate for the key its PeerID is built on, so the other side can map the device to an account |
| `/concord/link/1.0.0` | `internal/net/link.go`, `internal/link/link.go` | Device linking: the new device proves knowledge of the linking secret from the QR code or link code and receives the account material |
| `/concord/signal/1.0.0` | `internal/net/signal.go` | WebRTC signaling (SDP offers and answers, ICE candidates) for voice and video. One frame, no response. Media itself flows between the webviews over WebRTC and never touches Go |
| `/concord/attach/1.0.0` | `internal/net/attach.go` | Attachment blobs. A message carries only a content-addressed reference; the blob is secretbox ciphertext whose key rides inside the MLS-encrypted message |
| `/concord/chronicle/1.0.0` | `internal/net/chronicle.go` | Chunks of a guild's history archive (the chronicle), up to 1 MiB each. Served to anyone who asks, because a chunk is ciphertext whose key travels only inside the MLS-encrypted manifest |
| `/concord/release/1.0.0` | `internal/net/release.go` | Peer-to-peer updates: a peer on a newer release serves its binary in offset-addressed 1 MiB chunks. The receiver installs nothing until the release signature verifies |
| `/concord/mailbox/1.0.0` | `internal/mailbox/protocol.go` | Client to rendezvous. Ops: `register`, `deposit`, `drain`, `ack`, `register-push`, `unregister-push` |
| `/concord/guest/1.0.0` | `internal/net/guest.go` | Rendezvous to meeting host. One long-lived stream per browser guest, relaying the guest's WebSocket. Newline-delimited JSON both ways |
| `/concord/booking/1.0.0` | `internal/net/booking.go` | Rendezvous to host. One booking request from a public booking page and one reply, as newline-delimited JSON |
| `/concord/gifsearch/1.0.0` | `internal/net/gifsearch.go` | Client to rendezvous. GIF search, with results and media bytes both proxied through the node so the client never contacts the GIF provider |

A change to any of these needs a plan for an older peer meeting a newer one. The convention in the tree is to add optional JSON fields that an older peer ignores, and to bump the protocol version only when that cannot work, as `sync` did for v2. `scripts/interop.sh` runs an older release against your build (see [Testing](testing.md)).

## MLS groups

Every conversation is one MLS group, and the group ID is what the topic names are derived from.

- A **guild** is one group. Channels share the guild's group and differ only by topic.
- A **DM** is its own group with two members, created through the same invite handshake as a guild. Group DMs and disposable meeting rooms are also groups; the `Kind` field on the guild record (`dm`, `group`, `meeting`, or empty for a guild) tells the UI how to present it.
- Every account also holds a private **Notes** group shared only among its own devices. Read state travels on it, which is how unread counts converge across devices.

The life of a message through MLS is in [§8 of DESIGN.md](../DESIGN.md#8-the-life-of-a-message). How commits are authorized with no server to arbitrate, and why two members cannot commit at once, is in [§7.4](../DESIGN.md#74-authorising-membership-changes-with-no-server-to-arbitrate) and [§7.5](../DESIGN.md#75-what-this-design-costs-no-merge).

## Records

### Messages

A chat message is a JSON-encoded `domain.Message` (`internal/domain/model.go`): ID, channel ID, sender account key, display name, `Kind`, `ReplyTo`, content, send time, and flags for edited, deleted and pinned. The author signs it (`Sig`) before it is MLS-encrypted and published to the channel topic. A sealed copy is also deposited in the mailbox of each member who is offline.

Message actions are messages too. A `Kind` of `edit`, `delete`, `reaction` or `pin` acts on the message named in `ReplyTo`, and `system` marks a notice. An edit carries `RowSig`, the author's signature over the row as it will read after the edit.

Author signatures matter because of history sync. Live gossip authenticates the sender through MLS, but a sync answer is authenticated only as coming from whichever member responded. Signed records (messages, stories, pack records, archive manifests) are checked against their author's key on arrival, so the responder is a courier rather than a witness. `internal/app/recordsig.go` states which records are covered and which are not.

### Guild metadata

The meta topic carries a `guildMeta` envelope (`internal/app/guild.go`) whose `type` names the change: `channel_added`, `channel_updated`, `channel_renamed`, `channel_removed`, `category_added`, `category_renamed`, `category_removed`, `emoji_added`, `emoji_removed`, `guild_renamed`, `profile`, `nickname`, `event_upserted`, `story`, `chronicle`, `gov_op`, and others. `receiveGuildMeta` is the full list.

### Governance

Roles, bans, mutes, ownership and similar decisions are a per-guild log of signed `govOp` records (`internal/app/govstate.go`). Each op names its signer and is signed over every field; replaying the log in canonical order yields the guild's current roles and permissions, and an op that is badly signed or whose signer lacked the authority is skipped on replay. Adding a new op type is safe on the wire, because an older build folds an unknown type to nothing and relays it unchanged. Adding a new field to `govOp` is not, because an older peer re-marshals the struct and would drop the field and break the signature.

## History sync

When a peer connects, and again on a reconcile beat, it asks a connected member of each guild what it missed over `/concord/sync/2.0.0`. The request (`syncRequest` in `internal/app/sync.go`) carries:

- the requester's current MLS **epoch**;
- a per-channel time cursor (`Since`), less a one-minute overlap to absorb sender clock skew;
- **marks**: per channel, the responder's own change number the requester last reached (`internal/store/syncmarks.go`), so the question is "what changed on your side since we last talked" rather than "what is newer than my newest row";
- **digests** (`Have`): truncated content hashes of what the requester already holds, so the responder can leave out anything unchanged (`internal/app/syncdigest.go`).

The response carries the MLS commits that bridge the requester from its epoch to the responder's, then a payload encrypted at the responder's epoch: the guild snapshot, the member profile roster, and per-channel message rows including edits, deletes, pins and reactions as current state. A `More` flag says a byte budget cut the payload short and the requester should ask again immediately. If the responder's commit log cannot bridge the gap, it sets `EpochGap` and sends no payload.

Every new field is optional, and every filter is an omission: an older responder ignores marks and digests and serves a full snapshot, and saving messages is idempotent by ID, so overlapping answers from several members are harmless.

The reconcile beat runs on every third tick of the 20-second heal loop (`internal/app/heal.go`), so about once a minute in the foreground, and on every slower tick when a phone is backgrounded. Reconcile is the backstop for a dropped gossip message; live traffic does not wait for it. [§11.2 of DESIGN.md](../DESIGN.md#112-catching-up-with-nobody-holding-anything-for-you) covers catching up when no member is online.

## The store

Each peer keeps one SQLite database, `concord.db`, in its data directory. The schema is declared in `migrate()` in `internal/store/store.go`.

- **Content tables:** `guilds`, `channels`, `categories`, `messages`, `reactions`, `attachments`, `events`, `stories`, `custom_emoji`, `guild_gifs`, `chronicle_manifests`, `chronicle_chunks`.
- **Protocol state:** `mls_commits` (the per-group commit log that sync replays), `guild_ops` (the governance log), `pending_members`, `left_guilds`, `sync_counter`, `sync_marks` and `sync_floor` (the sync bookkeeping in `internal/store/syncmarks.go`).
- **Local state:** `settings`, `read_state`, `contacts`, `profiles`, `nicknames`, `blocked`, `saved_messages`, `scheduled_sends`, `story_seen`, `story_tombstones`, `event_announced`.

Message bodies are stored as `content_enc` and `nonce`: NaCl secretbox under a data key derived from the unlocked identity. Structural columns (IDs, channel, sender, timestamps, flags) are in the clear so the database can index and order them. Search decrypts rows as it scans, newest first, with a cap on how many it decrypts per call.

Schema changes are additive. Tables are created with `CREATE TABLE IF NOT EXISTS`, and new columns are added by an `ALTER TABLE … ADD COLUMN` list whose "duplicate column" errors are ignored, so a database from any earlier version opens in place. There is no schema version number and no down-migration; a change that cannot be expressed as an added table or column needs its own migration code. [§11.1 of DESIGN.md](../DESIGN.md#111-on-disk) describes what is on disk and what an attacker holding the file can read.
