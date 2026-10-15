# FAQ

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

On your devices: `~/.config/concord` on Linux, the platform's equivalent
elsewhere, or wherever `CONCORD_HOME` points. Message bodies and your identity
seed are encrypted at rest under your passphrase.

### What if I lose my device?

Restore from the 24-word recovery phrase shown when the identity was created
(Settings → Recovery phrase shows it again). History comes back from the other
members of each guild, and from your other linked devices. Without the phrase
and without another linked device, the identity cannot be recovered; there is
no one to ask.

### Do messages arrive while I am offline?

Direct messages are deposited, encrypted, in a rendezvous mailbox and collected
when you come back. Guild history is synced from whichever members are online
when you reconnect.

### Do I get notifications on Android when the app is closed?

With **Stay connected** on (the default), a foreground service keeps the node
running and messages arrive as they are sent. Push wakes through a third-party
service are optional and off unless a build is configured for them; see
[PUSH.md](PUSH.md).

### How big can a guild be?

Text is built for friend groups and communities, not millions. Membership
changes are batched into single MLS commits; a hundred people joining at once
has been measured converging in under a minute on one machine. Voice and video are a full mesh, so every participant sends
to every other: comfortable for a handful of people in a call, not for a
stadium.

### Is the cryptography audited?

No. Concord uses MLS (RFC 9420) through a vendored library with its local
changes recorded in [THIRD-PARTY-NOTICES.md](../THIRD-PARTY-NOTICES.md). The
known gaps are listed in [DESIGN.md §13](DESIGN.md#13-threat-model-what-concord-defends-against)
and [SECURITY.md](../SECURITY.md).

### Can I bring an existing community's history?

Yes. A guild owner can import a chat-history export into a signed, encrypted
archive that every member can scroll back through. It is in guild settings,
under Data.

### Can I run bots?

Yes. A bot is a peer with its own identity that you invite like anyone else.
The headless build (`make cli`) has a `--bot` mode that speaks
newline-delimited JSON on stdin and stdout; see [BOTS.md](BOTS.md).

### Why does Windows warn about the download?

The binaries are not code-signed. [WINDOWS.md](../WINDOWS.md) explains the
warning and the alternatives. Every release is listed in `SHA256SUMS` with an
Ed25519 signature, and the app verifies that signature before updating itself.

### Is there an iOS app?

Not yet.

### How do I report a security problem?

Privately; see [SECURITY.md](../SECURITY.md).
