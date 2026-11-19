# Privacy and encryption

What Concord encrypts, who can see what, what a rendezvous node learns, and what stays unprotected, in plain language. The formal policy is [PRIVACY.md](../../PRIVACY.md); the engineering detail is [DESIGN.md](../DESIGN.md), especially [§13 Threat model](../DESIGN.md#13-threat-model-what-concord-defends-against).

## The short version

- There is no account database and no company server. Your identity is a keypair made on your device.
- Your messages go directly between the people in the conversation, end-to-end encrypted. No one else can read them, including the operator of any rendezvous node you use.
- Your history is stored on your own devices, with message bodies encrypted under your passphrase.
- Who you talk to, and when, is **not** hidden. Peers you connect to directly see your IP address.

## How messages are encrypted

Every guild is one encrypted group, using MLS (Messaging Layer Security, [RFC 9420](https://www.rfc-editor.org/rfc/rfc9420)). Every direct message, including group DMs and your own Notes, is its own MLS group too. Only the current members hold the keys.

- **Messages, edits, deletions, reactions, pins** and the history batches a member sends you when you catch up after being offline are all encrypted to the group.
- **Forward secrecy.** Old keys are deleted as the group moves on, so a key stolen today cannot decrypt traffic recorded last month.
- **Removing someone re-keys the group.** A member who is kicked or banned cannot read anything sent after their removal.
- **Signatures.** Each message also carries its author's own signature. When a member hands you history you missed, a message whose signature fails is refused, and an old message with no signature is shown as unverified rather than attributed.
- **Voice and video** run browser-to-browser over WebRTC, encrypted with DTLS-SRTP. No Concord code sits in the media path. See [Voice and video](voice-and-video.md).
- **Every network hop** is additionally encrypted (Noise or DTLS), so someone on your Wi-Fi or at your ISP sees only ciphertext.

## What other people see

| Who | What they learn |
|---|---|
| Members of a guild you are in | Everything you post there, your profile (name, avatar, bio, status), whether you are online, your typing indicator if you and they both have typing indicators on, and your presence in voice rooms. |
| Anyone you connect to directly | Your IP address. Calls connect directly too, unless they are relayed (see below). |
| A stranger who sends you a message request | Nothing until you accept: not your profile, not your presence, not even that you are online. See [Message requests](moderation.md#message-requests). |
| The people in a direct message or group DM with you | When one of your devices has received each of their messages (the delivered tick). While you appear invisible these are held, and go out when you show again. When you have read them, only if you and they both have read receipts on. |
| Your own linked devices | Your read state, the chats you pinned or archived, the messages you deleted for yourself, and your choices for typing indicators, read receipts and disappearing messages. None of this goes to anyone else. |

Typing indicators and read receipts are off by default. Each is switched on in **Settings → Privacy & safety** and works both ways: with it off you neither send nor see it. The choice applies to all your devices. Both ways depends on the other person's app: someone running a modified app could see your typing or your receipts while not showing their own. Read receipts are sent only in direct messages and group DMs, inside the encrypted conversation. Delivered ticks have no switch there, and they say only that a message reached one of your devices, not that you looked at it. While you appear invisible they are held, and go out when you show again; any still held when the app closes are not sent. **Rich presence**, which shows what your music player is playing, is a separate switch on the same page; it works only where the platform exposes a music player (MPRIS, on Linux).

### Safety numbers

Every account has a **safety number**, a fingerprint of its public key. Open someone's profile and choose **Show** beside **Safety number**. In person, choose **Show my code / Scan theirs**: each of you shows a QR code and scans the other's, and a match marks them verified. Otherwise compare the number with them over a channel you trust, such as a phone call, and choose **It matches, verify** if it does. Verification is a private note on your own devices; nobody else sees it. It protects against the one attack encryption cannot catch by itself: someone handing you their key while pretending to be your friend.

Concord also posts a notice in the conversation when a contact adds a device, for example "Ann added a new device (iPhone, 11-14). If this wasn't Ann, verify.", so an account quietly gaining a new reader does not go unnoticed. See [Multiple devices and sync](devices.md).

## What a rendezvous node sees

A rendezvous is the small helper node that lets peers find each other, relays connections that cannot be made directly, holds an offline mailbox and can optionally run a call relay. A plain build of Concord points at none; you or a friend configure one. A copy someone built with a rendezvous built in starts from that one (it names it in **Settings → About** and marks it in **Settings → Connection**, where you can replace or clear it); that node sees your peer id, your network address and when you are online. See [Running a rendezvous](../RENDEZVOUS.md).

It is never a member of any group and holds no message keys, so it cannot read messages, attachments, history or calls. It can observe:

- the IP address and public key of every peer that connects to it;
- who talks to whom, and when, for connections it relays, and how much data passes;
- opaque mailbox tags, plus the size and time of each deposit (a mailbox tag is a salted hash of the recipient's key, not their identity);
- your **GIF search terms**, if GIF search is on (it is on by default; switch it off under **Settings → Privacy & safety → GIF search**);
- a **browser guest's** own messages in a meeting, and a **booking visitor's** IP plus the name and note they submit. These are the two plain-HTTPS doors, used by people with no Concord install.

It cannot learn who is in a guild, or that a guild exists, and it cannot forge or alter a message.

## Encryption on your device

Your history is a SQLite database on each of your devices. There is no server copy.

- **Encrypted:** message bodies, the earlier versions of edited messages, attachments and story captions. Your identity key is encrypted with your passphrase (Argon2id).
- **Not encrypted:** who sent each message and when, reactions, profiles and avatars, guild and channel names, the guild's governance log, and settings. So is `peers.json`, a list of up to 64 peers you have connected to recently (entries are forgotten after a month without contact), and the `mls/` folder holding each group's key state.
- **Unsent drafts** and failed sends wait in the interface's own browser storage, unencrypted, until they are sent or discarded.

Someone who takes your device without your passphrase cannot read your messages, but can learn the shape of your social life: who, when, and in which guilds.

Search runs on your device only. It decrypts and scans your own history, and the query never leaves the machine.

## What stays unprotected

- **Metadata.** Peer-to-peer means peers and relays see connections and timing. Concord is not an anonymity tool.
- **Your IP on calls.** Call media connects directly unless your rendezvous runs a TURN relay. Turn on **Settings → Privacy & safety → Hide my IP on calls** to force relaying; if the rendezvous has no relay, the setting says so and calls still connect directly. Meetings with browser guests always relay when a relay exists.
- **Members you invited.** Encryption cannot stop a member copying or screenshotting what you sent.
- **Deleted guild messages.** When a message is deleted for everyone in a guild, the devices of members with **Manage messages** keep its text, sealed, so that they can reveal it; every other device erases it. A moderator's device holds it until **Settings → Privacy & safety → Empty trash** is used there. In a direct message or a group DM, every device erases it. Space freed by an erase is overwritten, not just marked free. The first time Concord 0.60.5 or later starts, it erases the kept text of every message deleted earlier, on moderators' devices too.
- **Copies others hold.** Nothing you do can erase a message from someone else's device.

## Connections that are not to your peers

[PRIVACY.md](../../PRIVACY.md#the-outbound-calls-that-are-not-to-your-peers) lists every one. In short:

| Connection | When | Switch |
|---|---|---|
| `api.github.com` update check | Once at launch, desktop and web release builds only | None |
| `stun.l.google.com` | Never by default. When a call starts, if you switched the setting off and your rendezvous has no STUN responder of its own | **No third-party call servers**, on by default |
| Your rendezvous, for GIF search | As you type in the GIF picker | **GIF search**, on by default |
| Steam store search | As you type a game title | **Game title search**, off by default |
| Steam image CDN | When a profile lists games | **Game box art**, off by default |
| The linked site and its picture's host, or YouTube's or Vimeo's oEmbed service and picture server | Once, when **you send** a message with a link. Readers contact nobody | **Preview cards for links you send**, off until you choose there or answer the question asked at your first link |
| YouTube | When you tap play on a YouTube card or placeholder | Per tap |

All the switches are in **Settings → Privacy & safety**.

## Deleting your data

**Settings → Privacy & safety → Delete everything on this device** erases your identity, the database, the group keys and the peer list from this device after you type your profile name to confirm. It reaches nothing outside this device. If another of your devices is still linked, re-linking brings your guilds and history back; your 24-word recovery phrase alone restores the account key and, when another of your devices or someone you share a guild with is online, your profile, but no guilds or history. See [Backups and recovery](backups.md).
