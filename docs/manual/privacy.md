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
| Members of a guild you are in | Everything you post there, your profile (name, avatar, bio, status), whether you are online, your typing indicator, and your presence in voice rooms. |
| Anyone you connect to directly | Your IP address. Calls connect directly too, unless they are relayed (see below). |
| A stranger who sends you a message request | Nothing until you accept: not your profile, not your presence, not even that you are online. See [Message requests](moderation.md#message-requests). |
| Your own linked devices | Your read state. It never goes to anyone else, so there are no read receipts to switch off. |

Typing indicators can be switched off in **Settings → Privacy & safety → Typing indicators**. They work both ways: switch them off and you stop seeing other people's too. **Rich presence**, which shows what your music player is playing, is a separate switch on the same page; it works only where the platform exposes a music player (MPRIS, on Linux).

### Safety numbers

Every account has a **safety number**, a fingerprint of its public key. Open someone's profile, choose **Show** beside **Safety number**, and compare it with them over a channel you trust: in person, or on a phone call. If it matches, choose **Verify identity**. Verification is a private note on your own device; nobody else sees it. It protects against the one attack encryption cannot catch by itself: someone handing you their key while pretending to be your friend.

Concord also posts a notice in the conversation when a contact's set of devices grows, so an account quietly gaining a new reader does not go unnoticed. See [Multiple devices and sync](devices.md).

## What a rendezvous node sees

A rendezvous is the small helper node that lets peers find each other, relays connections that cannot be made directly, holds an offline mailbox and can optionally run a call relay. Concord ships pointing at none; you or a friend configure one. See [Running a rendezvous](../RENDEZVOUS.md).

It is never a member of any group and holds no message keys, so it cannot read messages, attachments, history or calls. It can observe:

- the IP address and public key of every peer that connects to it;
- who talks to whom, and when, for connections it relays, and how much data passes;
- opaque mailbox tags, plus the size and time of each deposit (a mailbox tag is a salted hash of the recipient's key, not their identity);
- your **GIF search terms**, if GIF search is on (it is on by default; switch it off under **Settings → Privacy & safety → GIF search**);
- a **browser guest's** own messages in a meeting, and a **booking visitor's** IP plus the name and note they submit. These are the two plain-HTTPS doors, used by people with no Concord install.

It cannot learn who is in a guild, or that a guild exists, and it cannot forge or alter a message.

## Encryption on your device

Your history is a SQLite database on each of your devices. There is no server copy.

- **Encrypted:** message bodies, attachments and story captions. Your identity key is encrypted with your passphrase (Argon2id).
- **Not encrypted:** who sent each message and when, reactions, profiles and avatars, guild and channel names, the guild's governance log, and settings. So is `peers.json`, a list of up to 64 peers you have connected to recently (entries are forgotten after a month without contact), and the `mls/` folder holding each group's key state.
- **Unsent drafts** and failed sends wait in the interface's own browser storage, unencrypted, until they are sent or discarded.

Someone who takes your device without your passphrase cannot read your messages, but can learn the shape of your social life: who, when, and in which guilds.

Search runs on your device only. It decrypts and scans your own history, and the query never leaves the machine.

## What stays unprotected

- **Metadata.** Peer-to-peer means peers and relays see connections and timing. Concord is not an anonymity tool.
- **Your IP on calls.** Call media connects directly unless your rendezvous runs a TURN relay. Turn on **Settings → Privacy & safety → Hide my IP on calls** to force relaying; if the rendezvous has no relay, the setting says so and calls still connect directly. Meetings with browser guests always relay when a relay exists.
- **Members you invited.** Encryption cannot stop a member copying or screenshotting what you sent.
- **Deleted guild messages.** A message deleted in a guild stays, sealed, on every member's device so that moderators can reveal it. Each device holds it until it empties its trash with **Settings → Privacy & safety → Empty trash**. Deleted direct messages are overwritten for real.
- **Copies others hold.** Nothing you do can erase a message from someone else's device.

## Connections that are not to your peers

[PRIVACY.md](../../PRIVACY.md#the-outbound-calls-that-are-not-to-your-peers) lists every one. In short:

| Connection | When | Switch |
|---|---|---|
| `api.github.com` update check | Once at launch, desktop and web release builds only | None |
| `stun.l.google.com` | When a call starts, to learn your external address | None |
| Your rendezvous, for GIF search | As you type in the GIF picker | **GIF search**, on by default |
| Steam store search | As you type a game title | **Game title search**, off by default |
| Steam image CDN | When a profile lists games | **Game box art**, off by default |
| The linked site | When a message contains a link | **Link previews**, off by default |
| YouTube | When you click a YouTube embed (its thumbnail loads only with link previews on) | Per embed |

All the switches are in **Settings → Privacy & safety**.

## Deleting your data

**Settings → Privacy & safety → Delete everything on this device** erases your identity, the database, the group keys and the peer list from this device after you type your profile name to confirm. It reaches nothing outside this device. If another of your devices is still linked, re-linking brings your guilds and history back; your 24-word recovery phrase alone restores only the account key. See [Backups and recovery](backups.md).
