# Messaging gap audit

**Method.** Grepped `internal/{app,store,bridge}` and `frontend/src` (plus `apps/mobile`) for each feature on the checklist and read the hits; "has" means code exists, not that it is polished. Compared against Signal (S), WhatsApp (W), iMessage (i) and Telegram (T) from general knowledge. **Privacy stance for ranking:** anything that tells others about you is opt-in and mutual; no new metadata to servers (rendezvous and relays stay untrusted, content-blind); no phone number or account identity; state that is only about me syncs through the Notes self-group, never to peers. Gaps rank by user value against privacy cost; anything needing a server to see content or metadata is last or in "Deliberately not doing".

## Already has (do not re-propose)

| Feature | Evidence |
|---|---|
| Edit (for everyone, `edited` flag) | `internal/app/guild.go` `EditMessage`; `internal/store/store.go` `edited` column; `EditAttachment` in bridge |
| Delete for everyone, trash, reveal-deleted | `internal/app/guild.go` / `bridge.go` `DeleteMessage`, `EmptyTrash`, `RevealDeleted`; `Message.svelte` `canRevealDeleted` |
| Replies / threads | `reply_to` in `store.go`; `CreateThread`; `lib/threadlinks.js` |
| Reactions | `guild.go` `ToggleReaction` / `applyReaction` |
| Forward | `frontend/src/modals/ModalForward.svelte` |
| Search (filters from:/in:/has:/after:) | `store.go` `SearchMessages` (decrypt-and-scan, not FTS); `SearchPanel.svelte`; `SearchChronicle` |
| Pinned messages | `guild.go` `PinMessage`/`applyPin`/`mayPin` |
| Mute / notification levels, alert words | `lib/notifs.js` (levels, `migrateMutes`), `lib/alertwords.js`, `ChannelList.svelte` `toggleMute` |
| Mentions + mention inbox | `internal/app/inbox.go`; `Composer.svelte` @user/@role/#channel; `CatchUpCard.svelte` |
| Typing indicator, opt-in and mutual | `internal/app/typing.go` (`typing_indicators` pref, default OFF since 1c6dba4c, off = neither send nor see); `lib/typing.js` |
| Disappearing messages (on-device sweep), with a per-chat timer | `internal/app/ephemeral.go`, `store/ephemeral.go`, `ChatHeader.svelte` "Disappearing messages"; guild `retention.go`; `lib/ephemeral.svelte.js` `channelTTL`/`setChannelTTL` (a device-local timer per channel that stamps every message you send there) |
| Saved / bookmarked messages | `bridge.go` `BookmarkMessage`, `SavedMessages`; `lib/saved.svelte.js` |
| Scheduled send + local reminders | `bridge.go` `ScheduleSend`; `lib/scheduled.svelte.js` |
| Polls, events/RSVP, booking | `lib/polls.js`, `PollView.svelte`, `internal/app/events.go`, `booking.go` |
| Link cards, built by the sender (since d6836900) | `internal/app/linkcard.go` (SSRF-guarded fetch at send time, oEmbed for YouTube and Vimeo); `lib/linkcard.js`; `LinkPreview.svelte`; readers fetch nothing |
| Voice notes | `VoiceMessage.svelte`, `lib/voicemsg.js` |
| Voice/video calls, screen share | `internal/app/voice.go`, `VoicePanel.svelte` |
| GIF search (opt-in), guild GIFs, custom emoji, memes | `gifsearch.go`, `gifs.go`, `emoji.go`, `lib/meme.js` (no sticker packs) |
| Multi-device linking | `internal/app/device.go`, `devices.go`, `link.go`; `LinkedDevices` |
| Backup / export | `archive.go` `ExportArchive`/`ImportArchive` (sealed), `exportmd.go`, mnemonic restore, chat-import (`chronimport`) |
| Contact verification (fingerprint mark) | `service.go` `VerifyFingerprint`; `ProfilePopover.svelte` (shows a read-aloud fingerprint string and a Verify button; no QR scan) |
| Block, report-as-evidence-file | `internal/app/block.go`; `lib/report.js` |
| Read state (private, self-synced) | `internal/app/readstate.go` (Notes self-group, never leaves account) |
| Drafts (per-device) | `lib/drafts.svelte.js` |
| Message requests from strangers | `internal/app/request.go`, `MessageRequests` |
| Identity without phone number | keypair identity, `PRIVACY.md` |
| Screenshot/app-switcher blocking (Android) | `apps/mobile/.../MainActivity.java` `FLAG_SECURE` |
| Biometric app lock | `lib/biometric.js` |
| Forums, stories, announcements (broadcast-ish) | `forum.go`, `story.go`, `AnnouncementView.svelte` |
| Bots | `docs/BOTS.md` (`concord --bot`) |
| Folders (partial) | guilds + categories only; no user-defined chat folders |

## In progress (other branches)

| Feature | Note |
|---|---|
| Delivered ticks | per-message delivery state |
| Opt-in mutual read receipts | complements private self-synced read state |
| Delete for me | local-only removal |

## Gaps, ranked

| # | Feature | Who | Value | Privacy cost | Effort | Privacy-safe design note |
|---|---|---|---|---|---|---|
| 1 | Notification content privacy (hide sender/body on lock screen and in OS tray) | S W i T | H | none: before 9feb4290, `notify.js` put a 120-char snippet in the OS notification, which other apps and lock screens can read | S | **Done in 9feb4290**: a setting for what notifications show, default nothing (`lib/notifprivacy.js`) |
| 2 | Pinned chats + archive chat (DM/channel list) | S W i T | H | none: local view state | S | Store in Notes self-group like read state so it syncs across own devices only |
| 3 | Link previews generated by the sender | S W i | H | low: before d6836900, every recipient's client fetched the URL, leaking their IP to the site and a "read it now" timing signal | M | **Done in d6836900**, now as link cards: the sender fetches once, at send, and the title, site and picture travel in the encrypted message; recipients never fetch, and the receiver toggle is gone (`internal/app/linkcard.go`) |
| 4 | Edit history (view prior versions) | S(yes) i(yes) W(no) T(no) | M | low: reveals earlier text to people who already saw it | S | Keep prior versions inside the edit event; show "Edited, view history" to members only; sender can opt to omit |
| 5 | Chat folders / filters (Unread, DMs, custom) | T W i | M | none | M | Local view over existing chats; sync via Notes self-group |
| 6 | QR scan to verify (read-aloud compare exists) | S W i | M | none | S | Show a comparable fingerprint code + QR scan to mark verified; extend `VerifyFingerprint`; warn when a key changes |
| 7 | Drafts sync across own devices | T (W not verified) | M | none: self-group only | S | Encrypt drafts into Notes self-group, last-writer-wins; never to peers |
| 8 | Shared chat disappearing default + "view once" media | S W i(partial) | M | low: advisory only; recipient can still screenshot | M | A per-chat timer already exists, but it is device-local and stamps only your own sends (`channelTTL`); what is missing is a default the chat's members share, and view-once. View-once: sealed attachment flagged view-once, key deleted after first open; label as best-effort; reuse `ephemeral.go` |
| 9 | Sticker packs | S W i T | M | low: fetching packs from a third party leaks interest | M | Packs ship as encrypted attachments from a peer (like guild GIFs/custom emoji); no central store |
| 10 | Typing indicator made opt-in + mutual (not default on) | S(opt) W(no) | M | low: reveals when you are composing | S | **Done in 1c6dba4c**: default OFF, shown only when both sides enabled, synced across own devices; it used to be on by default |
| 11 | Message bookmarks surfaced as "Saved Messages" self-chat | T W | L | none | S | Already stored; just expose as a Notes DM view (`NotesDM` exists) |
| 12 | Per-conversation mute durations / schedules ("mute 8h") | S W i T | L | none | S | Local timestamp in the notification level model |
| 13 | Link-preview/URL tracking-parameter stripping on send | i (iOS 17 Link Tracking Protection, in Messages); S not known to | L | none | S | Strip utm/fbclid client-side before sending |
| 14 | Live location share | W i T (not S) | M | high: continuous location to the group | M | Only P2P to chosen members, expiring, explicit per-share consent; no relay logging |
| 15 | Quick Reply / `/` command bar parity for bots (inline bots) | T | L | low | M | Keep bots as peers; no server-side bot API |
| 16 | Hidden / secret chats lock (per-chat biometric) | W(chat lock); S has none (app-wide screen lock only) | M | none | M | Extend `biometric.js` to gate specific chats; local only |
| 17 | Contact-sync "who's on Concord" | S W T | M | high: uploads address book | L | Do not build; use invite links/QR (see below) |

## Recommended next 5

All five have since landed: 9feb4290, d6836900, 83921356, 52e0c04e and 1e2bee0b, in this order.

1. **Notification content privacy**: it was the only gap that leaked real content outside the app, and it is a small local setting.
2. **Sender-generated link previews**: closes the one place a recipient's client makes an outbound request because of someone else's message.
3. **Pinned chats + archive**: the highest-frequency daily-use list hygiene feature competitors all have, zero privacy cost.
4. **QR verify + key-change warning**: makes the E2EE promise checkable in one scan; the read-aloud string and `VerifyFingerprint` already exist.
5. **Edit history**: cheap, builds on the existing edit event, and matches the trust model (only members who already saw the text).

## Deliberately not doing

- **Phone-number identity / contact discovery**: requires uploading an address book to a server and ties identity to a carrier record; keypair + invite/QR stays.
- **Server-side search or server-held message index**: would put content or query metadata on a server; search stays local decrypt-and-scan (consider a local FTS index only).
- **Cloud message backup held by a Concord/third-party service**: sealed local archive export already exists; no hosted copy.
- **Always-on read receipts and always-on typing/online status**: telling others about you must be opt-in and mutual.
- **Server-side bots / bot API with message access**: bots stay ordinary peers with membership.
- **Centralized sticker/GIF stores fetched by every client**: leaks interest and IP; GIF search stays opt-in, packs travel peer to peer.
- **Public channel discovery / directory**: needs a server that sees metadata and invites abuse.
- **Link-preview fetching by recipients (as default)**: see gap 3; replace, don't keep.

## Corrections to the first version (1511cbef)

- **Gap 8.** A per-chat disappearing timer already exists (`lib/ephemeral.svelte.js` `channelTTL`/`setChannelTTL`). It is device-local and stamps every message you send in that channel. What is missing is a default shared by the chat's members, and view-once.
- **Gap 4.** Signal does keep edit history: an edited message shows its earlier versions.
- **Gap 14.** Signal has no live location sharing.
- **Gap 16.** Signal has no per-chat lock; WhatsApp does. Signal's lock is the whole app.
- **Gap 7, rechecked.** Telegram syncs drafts across devices. WhatsApp is not verified to, so it is no longer listed as having it. Concord keeps drafts per device (`lib/drafts.svelte.js`); nothing syncs them.
- **Gap 13, rechecked.** Signal is not known to strip tracking parameters. iMessage does (iOS 17 Link Tracking Protection removes identifying parameters from links in Messages and Mail). Concord strips nothing today.
- **Already has.** Two rows were overtaken by the commits that followed the audit: typing indicators are now off by default and mutual (1c6dba4c), and link previews are built by the sender (d6836900). Gaps 1, 2, 3, 4, 6 and 10 landed in 9feb4290, 83921356, d6836900, 1e2bee0b, 52e0c04e and 1c6dba4c.
