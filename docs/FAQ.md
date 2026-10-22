# FAQ

Short answers to the questions people ask first about Concord: servers, data, recovery, offline delivery, bots and security.

The full user manual starts at [Getting started](manual/getting-started.md).

### Is there a server?

No server holds accounts or messages. Peers connect to each other directly.
A [rendezvous](RENDEZVOUS.md) node is optional: it helps two peers behind home
routers find and reach each other, and relays ciphertext when a direct
connection fails. Anyone can run one, and a group only needs one.

### What can a rendezvous see?

Which identities connect through it, their IP addresses, and when. It cannot
read messages, attachments or calls. [DESIGN.md §6](DESIGN.md#6-the-rendezvous-node)
goes through it service by service.

### Where is my data?

On your devices: `~/.config/concord` on Linux, `~/Library/Application Support/concord`
on macOS, `%AppData%\concord` on Windows, or wherever `CONCORD_HOME` points; see
[Where your data is](manual/troubleshooting.md#where-your-data-is). Message bodies and your identity
seed are encrypted at rest under your passphrase.

### What if I lose my device?

If the account is linked on another device, link the new one from it and your
guilds and history come with it. Otherwise restore from the 24-word recovery
phrase shown when the identity was created (**Settings → Account → Recovery
phrase** shows it again). The phrase restores your identity only: the new device
starts with no guilds and no history, so ask someone in each guild for a fresh
invite, then restore a backup file (**Settings → Account → Backup & restore**) if
you made one. Without the phrase and without another linked device, the
identity cannot be recovered; there is no one to ask.

### Do messages arrive while I am offline?

If a rendezvous is set, every message, in a guild or a direct message, is also
deposited, encrypted, in the rendezvous mailbox of each member device that is
offline, and collected when that device comes back. The mailbox keeps them in
memory for up to 14 days. Anything it does not hold is synced from whichever
members are online when you reconnect.

### Do I get notifications on Android when the app is closed?

With **Stay connected** on (the default, under **Settings → Notifications &
sounds**), a foreground service keeps the node running and messages arrive as
they are sent. Push wakes through a third-party
service are optional and off unless a build is configured for them; see
[PUSH.md](PUSH.md).

### How big can a guild be?

Concord is built for friend groups and communities. Calls are a full mesh,
where every participant sends to every other, which suits a handful of people
per call.

### What cryptography does it use?

Ed25519 identities, MLS (RFC 9420) for every guild and direct message, and
DTLS-SRTP for calls. The vendored MLS library and its local changes are listed
in [THIRD-PARTY-NOTICES.md](../THIRD-PARTY-NOTICES.md); what is and is not
protected is in [DESIGN.md §13](DESIGN.md#13-threat-model-what-concord-defends-against).

### Can I bring an existing community's history?

Yes. A guild owner can import a chat-history export into a signed, encrypted
archive that every member can scroll back through. It is in guild settings,
under **Data → Import a chat archive**.

### Can I run bots?

Yes. A bot is a peer with its own identity that you invite like anyone else.
The headless build (`make cli`) has a `--bot` mode that speaks
newline-delimited JSON on stdin and stdout; see [BOTS.md](BOTS.md).

### Why does Windows warn about the download?

The binaries are not code-signed. [WINDOWS.md](../WINDOWS.md) explains the
warning and the alternatives. Every release is listed in `SHA256SUMS` with an
Ed25519 signature, and the app verifies that signature before updating itself.

### How do I report a security problem?

Privately; see [SECURITY.md](../SECURITY.md).
