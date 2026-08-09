# Concord: how it works

Concord is a peer-to-peer group chat, voice and video application: a Go core
with a Svelte front end, shipped as one binary. There is no company, no account
database, and no server that stores your messages. Every guild ("server") is a
cryptographic group, every message is end-to-end encrypted, and the one piece of
infrastructure it can use is untrusted by construction.

This document is the design paper. It explains what peer-to-peer means for each
job a server normally does, defines every term the first time it appears, and
describes each mechanism precisely enough that you could argue with it. It is
written to be read straight through without opening the source; a [map of the
code](#14-map-of-the-code) is at the end for when you want to. For the short,
plain-language account of what data exists and who can read it, see
[PRIVACY.md](../PRIVACY.md).

---

## Table of contents

1. [The problem, and the thesis](#1-the-problem-and-the-thesis)
2. [Client–server, peer-to-peer, and three hard problems](#2-clientserver-peer-to-peer-and-three-hard-problems)
3. [Identity: a keypair is an account](#3-identity-a-keypair-is-an-account)
4. [Discovery: what a distributed hash table is, and how Concord finds people](#4-discovery-what-a-distributed-hash-table-is-and-how-concord-finds-people)
5. [Reachability: NAT, hole punching, and relays](#5-reachability-nat-hole-punching-and-relays)
6. [The rendezvous node](#6-the-rendezvous-node)
7. [Group encryption: MLS, epochs, and membership with no arbiter](#7-group-encryption-mls-epochs-and-membership-with-no-arbiter)
8. [The life of a message](#8-the-life-of-a-message)
9. [Voice and video](#9-voice-and-video)
10. [Guests: joining with no account](#10-guests-joining-with-no-account)
11. [Storage, history, and catching up](#11-storage-history-and-catching-up)
12. [Assets: why nothing is fetched at runtime](#12-assets-why-nothing-is-fetched-at-runtime)
13. [Threat model: what Concord defends against](#13-threat-model-what-concord-defends-against)
14. [Map of the code](#14-map-of-the-code)

---

## 1. The problem, and the thesis

Centralized chat platforms have a structural property that no privacy policy can
fix: the operator sits between every message you send and every friend who reads
it. The operator authenticates you, stores your history, relays your voice, and,
with the best intentions in the world, *can* read, mine, moderate, lose, leak, or
be compelled to hand over all of it. Transport encryption (TLS) does not change
this: the server terminates TLS, so the plaintext exists on the operator's
machines by design. Availability, privacy, history and membership are promises
the operator makes to you, not guarantees you hold.

Concord's thesis, in one sentence:

> If the participants do the work a server would otherwise do, then nobody has
> to be trusted. The price is that a room can only be as big as its members'
> own bandwidth.

Everything below is either a mechanism that discharges some server's job onto
the participants, or a statement of what that costs.

### Self-contained, not minimal

Concord ships stories, a soundboard, a meme editor, polls, events, calendars,
more than twenty themes and a game collection for your profile. It is not a
minimal application. It is a self-contained one: one binary, no external
services, no runtime fetches by default, no account database. Features are built
to hold that line rather than trimmed to avoid testing it. The soundboard
synthesizes its sounds from oscillators instead of shipping audio files. Emoji
and typefaces are compiled in rather than requested from a CDN. Story
backgrounds are drawn presets, not stock images pulled down on demand.

Where a feature genuinely cannot be built without reaching a third party, the
line holds by making the reach opt-in rather than by dropping the feature or
quietly taking it: link previews, YouTube embeds and game box art all ship off,
and §12 explains each. That "by default" is load-bearing, and the moment it
stops being true of something, this document is wrong rather than aspirational.

### What Concord does not have

- **No account database.** Your identity is a keypair on your device. There is
  nothing to sign up for, no email, no phone number, no username table, and
  nothing to breach.
- **No message store.** History lives on the devices of the people in the
  conversation and nowhere else.
- **No trusted server.** There is one optional piece of infrastructure (§6). It
  is cryptographically incapable of reading what it carries, and the system is
  designed to survive its loss.
- **No runtime fetches.** No fonts, no emoji images, no analytics, no crash
  reporting, no update ping to anything except the release repository you can
  see. A request to a font host on every launch would reveal your IP and the
  moment you opened the app (§12).
- **No second thing to install.** Whatever ships has to work on a fresh machine
  with nothing else present, a rule that got written down only after an
  optional, off-by-default helper which shelled out to a separately installed
  program was built and removed twice.

### The trade

Concord targets friend groups and communities, not million-user servers. Text
scales fine; a full mesh of live audio connections does not scale to stadiums,
and does not need to. That single trade is what buys everything else, and it is
stated up front rather than discovered later.

---

## 2. Client–server, peer-to-peer, and three hard problems

### The client–server model

In a centralized system nobody talks to anybody. Everybody talks to the
middleman:

```
        alice ──────┐                  ┌────── carol
                    ▼                  ▼
               ┌─────────────────────────────┐
               │       COMPANY SERVER        │
               │  • authenticates everyone   │
               │  • stores every message     │
               │  • relays every voice call  │
               │  • sees EVERYTHING          │
               └─────────────────────────────┘
                    ▲                  ▲
        bob ────────┘                  └────── dave
```

This is not a lazy design. It is a *convenient* one, because the server answers
four questions for free: where is bob (he has a session with me), can I reach
him (he dialled me, so yes), is this really bob (I authenticated him), and who
should get this message (I keep the member list). Remove the server and all four
questions come back, unanswered.

### The peer-to-peer model

In a peer-to-peer system the participants *are* the system. Each person runs a
node; nodes connect directly to each other:

```
        alice ◄════════════════► carol
          ▲  ╲                 ╱  ▲
          ║   ╲               ╱   ║        every line is a direct,
          ║    ╲             ╱    ║        Noise-encrypted connection
          ║     ╲           ╱     ║        between two peers
          ▼      ╲         ╱      ▼
         bob ◄════╲═══════╱═══► dave
```

### The three hard problems

| Problem | Why it is hard without a server | Answered in |
|---|---|---|
| **Discovery**: how does Alice learn Bob's current address? | Home connections have no stable, publicly listed address, and there is no directory to look him up in. | §4 |
| **Reachability**: having his address, how does she get a packet to him? | Home routers drop unsolicited inbound traffic by default. Both ends are behind one. | §5 |
| **Trust**: with nobody vouching, how does she know it is Bob? | Anyone can claim any name. Anyone can offer any key. | §3, §7 |

A fourth follows from the third: with no server-side gatekeeper, encryption is
not a feature you switch on but the only mechanism that decides who can read
what. Which means Alice must be able to encrypt to *a group whose membership
changes*, correctly, with nobody to arbitrate. That is §7.

Discovery and reachability are frequently confused, so here is the blunt form:
discovery is finding out where someone is; reachability is being able to send
them a packet once you know. They are solved by different mechanisms, they fail
independently, and each has its own set of fallbacks. §4 and §5 are separate
chapters for that reason.

### Why the difficulty is worth it

Because solving these problems among the participants means nobody else has to
be trusted. Privacy stops being a policy the operator promises and becomes a
property of the mathematics. Client–server is mailing every letter through one
post office that photocopies each page "for safety". Peer-to-peer is handing
your friend a sealed, tamper-evident envelope that only they can open, with no
post office in the picture.

---

## 3. Identity: a keypair is an account

### A keypair as an account

On first run Concord generates 32 random bytes, a **seed**, and derives an
**Ed25519 keypair** from it. Ed25519 is a digital-signature scheme: the private
key can sign a message, and anyone holding the public key can verify that
signature and cannot forge one.

```
   32 random bytes (the seed)
          │
          ▼  Ed25519
   ┌──────────────┐         ┌────────────────────────────────┐
   │ private key  │────────►│ public key  (shareable)        │
   │ never leaves │  derive │   ├─► libp2p PeerID  ← address │
   │ the device   │         │   └─► fingerprint   ← identity │
   └──────────────┘         └────────────────────────────────┘
          │
          ▼  sign anything → unforgeable proof it came from this account
```

That keypair *is* the account. There is no row in a database that says the
account exists; the account exists because you hold the key. Nobody can suspend
it, rename it, or issue a second one. Nobody can help you if you lose it either,
which is why the seed is also expressible as a 24-word BIP39 recovery phrase.

### Your address is derived from your identity

Concord's networking is built on **libp2p**, a toolkit that gives every node a
**PeerID**. For Ed25519 keys the public key is short enough that libp2p embeds
it in the PeerID verbatim rather than hashing it, so a PeerID is a public key in
disguise.

This single fact removes an entire category of infrastructure. When you dial a
peer, the transport handshake (**Noise**, the same construction WireGuard uses)
proves the far end holds the private key for the PeerID you dialled. If it does
not, the handshake fails. There is no certificate, no certificate authority, and
nothing to revoke: *the address is the identity check.* An attacker who
intercepts your connection cannot impersonate the peer you asked for; the best
they can do is stop you reaching it.

### Fingerprints, and what verification proves

A PeerID is 52 characters of base58, fine for a machine and useless for reading
aloud. So Concord also derives a **fingerprint**:

```
   fingerprint = base32( SHA-256(public key)[0:20] )   grouped in fours
               = "2XXL XRW4 TT5D 6QK7 …"                (32 characters, 160 bits)
```

This is what Signal calls a *safety number*. Its purpose is narrow, and the
narrowness matters:

- **What it defends against.** The one attack the mathematics cannot see is a
  substitution at first contact: an attacker who hands you *their* key while
  claiming to be your friend. Every subsequent guarantee then holds perfectly,
  for the wrong person. Comparing fingerprints over a channel the attacker does
  not control (in person, over the phone, in an existing verified conversation)
  detects that substitution, and detects nothing else.
- **What "verify" does in the app.** It sets one local boolean beside that
  fingerprint in your own encrypted database. It is a record of *your* claim to
  have compared them out of band. It is not a cryptographic proof, not a
  signature, and not visible to anyone else. Nothing verifies it for you, which
  is the point: the human step is the security.
- **What it gates.** Adding someone to a guild directly requires that you have
  verified them, and a guild invitation pushed at you is only auto-accepted from
  a verified contact.

Because a fingerprint is derived from the **account** key, it does not change
when you add a device, which is convenient and slightly dangerous, so Concord
posts a local notice into the conversation when a contact's device set grows
(*devices*, below). That notice is the half of safety numbers that catches an
account quietly growing a second reader.

### Devices versus accounts

An **account** is the seed and the keypair derived from it: stable across every
machine you use, and the thing fingerprints are computed from. A **device** is
one installation. Each installation generates its own separate 32-byte device
seed, which never leaves it.

Linking a second device does not copy an identity so much as *certify* one:

```
   Account key  (Alice)                       Device key  (Alice's phone)
        │                                              │
        │  signs ──────► DeviceCert {                  │
        │                  account pubkey,  ◄──────────┘
        │                  device pubkey,
        │                  device name,
        │                  issued-at
        │                }
        ▼
   Every peer can check: this device speaks for this account.
```

- The **certificate is signed by the account key** over a length-prefixed,
  domain-separated encoding of its fields, so it cannot be replayed into another
  context.
- A linked device gets **its own network address** (its PeerID comes from its
  device key, so two devices of one account do not collide) and **its own place
  in every group's key tree** (§7). It signs its own messages with its own key.
- Every authorization decision normalizes a credential to an account first: a
  device certificate resolves to its `account` field only if the signature
  verifies, otherwise the raw bytes are used. A forged certificate therefore
  cannot masquerade as somebody else's account; it becomes an unknown identity
  instead.
- Consequences that fall out of this and are enforced: banning an account
  removes *every* device leaf it holds, because a ban that removed only the
  first would leave the banned account still reading and posting from the
  second; and a second device joining a guild does not produce a bogus "joined
  the server" notice.

**The linking handshake.** The already-unlocked device shows a QR code carrying a
single-use 32-byte secret and its own address, valid for two minutes. The new
device dials it over a Noise-encrypted stream, and both sides prove knowledge of
the secret with an HMAC over role-separated labels (compared in constant time),
so neither a network attacker nor an eavesdropper on the QR can insert
themselves. The issuer then hands over the account seed, a device certificate it
signs for the joiner's device key, its bootstrap configuration, the list of
fingerprints it has verified, and one invite code per guild so the new device can
join every existing group. The joiner checks that the certificate verifies,
that it certifies *its own* device key, and that the seed it was given derives
the account named in the certificate, before writing anything to disk.

### The keystore

The seed is the only true secret, so it gets the strongest treatment on disk.
It is sealed with **NaCl secretbox** (XSalsa20-Poly1305) under a key stretched
from your passphrase with **Argon2id**: 64 MiB of memory, 3 passes, 4 lanes, a
fresh 16-byte salt per save. Argon2id is memory-hard, so guessing your
passphrase costs an attacker 64 MiB of RAM per guess, which is what makes
offline brute-force expensive rather than free. The parameters are recorded in
the file so they can be raised later without breaking existing keystores. The
plaintext seed never touches disk. A wrong passphrase is indistinguishable from
a corrupted file, by design.

Three further keys are derived from the seed by HKDF-SHA256 with distinct
labels: `concord-store-v1` for the local database, `concord-mls-sig-v1` for the
group signing key, `concord-mbx-enc-v1` for the offline mailbox. Distinct labels
mean that even a total break of one context cannot be levered into another, and
because they are *derived* rather than stored, a restarted device recomputes
them and carries on with nothing to back up.

---

## 4. Discovery: what a distributed hash table is, and how Concord finds people

Alice wants to send Bob a message. Before anything else can happen she must
learn a current address for him. This section is about that and nothing else.

### 4.1 Start with a hash table

A **hash table** maps keys to values. You hash the key to an index, and the
value lives there. One machine holds the whole table; lookups are one step.

A **distributed hash table (DHT)** spreads that same table across many machines
with no coordinator. The design problem is a single question: *given a key,
which machine holds it?* It has to be answered without asking anyone, because
asking requires a directory, which is what we are building.

Kademlia, the DHT design libp2p uses, answers it like this:

1. **Give every node an identifier from the same space as the keys.** Node IDs
   and keys are both fixed-length bit strings.
2. **Define a distance between two identifiers.** Kademlia's distance is
   *bitwise XOR, read as an unsigned integer.*
3. **Store each key at the nodes whose IDs are closest to it.** No election, no
   allocation table. "Who holds key K" is a pure function of the IDs in the
   network.

### 4.2 Why XOR

XOR distance looks strange until you notice what it measures: the length of the
shared prefix.

```
   key    K = 1011 0110 1001 …
   node   P = 1011 1100 0011 …
            ─────────────────── XOR
                0000 1010 1010 …
                └──┘
             4 leading zeros ⇒ 4 bits of shared prefix ⇒ a small distance

   node   Q = 0011 0110 1001 …
            ─────────────────── XOR with K
                1000 0000 0000 …
                └┘
             0 leading zeros ⇒ differs at the first bit ⇒ an enormous distance
```

XOR is symmetric (d(A,B) = d(B,A)), so if A is a useful contact for B, B is a
useful contact for A, and every lookup that passes through a node teaches it
something it can reuse. It is also *unidirectional*: for any key and any
distance there is one identifier at that distance and no other, so lookups from
different starting points converge on the same neighbourhood rather than
wandering. And because distance is dominated by prefix length, the identifier
space behaves like a binary tree with the keys at the leaves.

### 4.3 Why the routing takes O(log n) hops

Each node keeps its contacts in **buckets** by shared-prefix length: bucket *i*
holds up to *k* peers whose IDs share the first *i* bits with its own and differ
at the next. In Concord's DHT *k* = 20. So a node knows a great many peers
"near" itself and only a handful in each successively more distant half of the
space, a routing table of about log₂(n) buckets rather than n entries.

A lookup for key K asks the closest contacts it knows (α = 10 in parallel) a
single question: *who do you know that is closer to K than you are?* Each answer
comes from a node with a longer shared prefix with K than the last, so each
round fixes at least one more bit and halves the remaining search space:

```
   you ──ask──► a node sharing ≥1 bit with K
                     ──ask──► a node sharing ≥2 bits
                                  ──ask──► ≥3 bits
                                              ⋮
                                       ──► the ~20 nodes closest to K
                                           (they hold the record)

   halving each round ⇒ ~log₂(n) rounds.  A million nodes ⇒ about 20 hops.
```

That is the whole reason a DHT is usable: the *storage* is spread over everyone,
but the *search* touches a logarithmic number of nodes. A lookup also does not
stop at the first answer. It keeps querying until the closest handful of peers
have been reached, so one absent or lying node does not end the search.

### 4.4 Content-addressed keys, and provider records

Nobody hands out DHT keys. They are computed, which is what makes independent
nodes agree without talking:

- **The key is a hash of the thing being looked up.** Concord's namespace is the
  literal string `concord/dht/v1`; the DHT key is its SHA-256 hash. Every
  Concord node in the world computes byte-identical keys with zero coordination.
  This is what *content-addressed* means: the name is derived from the content,
  not assigned by an authority.
- **The value is not the data.** Concord stores no data in the DHT. It stores a
  **provider record**: a small note saying *"peer P can serve key K, and here
  are P's current addresses."* Publishing one is called **providing**; a record
  points at a peer rather than containing anything.

So `Provide(K)` means: walk the routing table to the ~20 nodes closest to K and
ask each to remember "I provide K". `FindProviders(K)` means: walk to the same
neighbourhood and collect the notes you find there.

Provider records are **soft state**. They carry a validity (about 24 hours in
this DHT) and are forgotten when it lapses, so the publisher republishes, every
3 hours in Concord's case. Nothing has to be deleted when a node dies; its
records age out on their own. A DHT is therefore a *self-cleaning* directory,
which matters a great deal when the things being indexed are laptops.

### 4.5 What "advertising under a rendezvous key" means

Put the two halves together and the mechanism is almost anticlimactic. Every
Concord node, on startup:

1. computes the key from `concord/dht/v1`,
2. calls `Provide` on it, republishing every 3 hours, and
3. calls `FindProviders` on the *same* key every 15 seconds, dialling anything
   new that comes back.

The DHT is not being used as storage. It is being used as a **mailing list**:
the key means "I am a Concord node", and the value is where to find whoever said
so. That is a *rendezvous point*, an agreed-upon location where parties who have
never met can each leave a note for the other.

Two consequences:

- **The key is guessable.** It is a hash of a published string. Anyone can
  compute it and enumerate the addresses of everyone advertising under it. Your
  messages stay sealed; the fact that you run Concord, and from where, does not.
  This is why the public-DHT rung below is opt-in.
- **A DHT is only distributed if it has many servers.** A node that cannot
  accept inbound connections cannot store records for others, so Concord peers
  participate as DHT *clients* (they also pin their reachability to "private" so
  relay reservations happen immediately, §5). In a deployment with one
  rendezvous, the phone book is therefore one shelf, held by that node. The
  Kademlia machinery is what makes that degrade gracefully into a distributed
  table the moment there is more than one server: a second rendezvous, a friend
  on a public IP, or the public network in rung 4.

### 4.6 The ladder Concord climbs

Concord does not have "a discovery mechanism". It has four, tried in this order,
and only two of them involve a server of any kind:

```
  ┌────────────────────────────────────────────────────────────────────────┐
  │ 1 · mDNS on the local network            no server, no internet       │
  │     "any Concord peers here?" shouted at the LAN                      │
  ├────────────────────────────────────────────────────────────────────────┤
  │ 2 · Remembered peers from past sessions  no server                    │
  │     yesterday's friends at yesterday's addresses                      │
  ├────────────────────────────────────────────────────────────────────────┤
  │ 3 · The DHT via your own rendezvous      one untrusted node           │
  │     everyone advertises under the same computed key                   │
  ├────────────────────────────────────────────────────────────────────────┤
  │ 4 · The public IPFS DHT  (opt-in)        no Concord server at all     │
  │     thousands of strangers' nodes hold the record instead             │
  └────────────────────────────────────────────────────────────────────────┘
```

**Rung 1, mDNS.** *Multicast DNS* is DNS shouted at a local network instead of
asked of a server: a UDP packet to a multicast group that every machine on the
segment receives. Concord broadcasts under its own service tag so it only finds
other Concord instances, and dials anything that answers.

```
        Your Wi-Fi
   ┌────────────────────────┐
   │  Alice ◄────► Bob      │   one multicast packet → a direct connection.
   │  (no internet at all)  │   No servers. Works on a plane.
   └────────────────────────┘
```

Multicast is blocked on plenty of corporate and locked-down networks, and
Android's sandbox denies the socket that the implementation needs, so its
failure is logged and ignored rather than fatal.

**Rung 2, remembered peers.** Every peer you have shared a guild with is written
to a small local cache with its addresses and a last-seen timestamp. This is the
rung that needs *nothing*: if the rendezvous is gone, yesterday's friends are
still dialable at yesterday's addresses. Specifics, because "we remember peers"
is the kind of claim that hides a lot:

- at most 64 peers, at most 8 addresses each;
- forgotten after 30 days without a successful connection;
- retired after 5 consecutive *outages*, counted at most once per outage and
  never once per dial attempt, so the friend who happens to be asleep on the
  night the rendezvous dies is not deleted by a retry loop;
- addresses are only recorded for peers you share a guild with, and only
  addresses at a host that traffic arrived from (a peer can *claim* any address
  in the handshake; a claim is not evidence);
- dialled on the first pass of every launch, since a restart should reach
  yesterday's friends before any DHT lookup completes, and after that only while
  the rendezvous is unreachable.

Known gap: this cache is plaintext on disk. See §13.

**Rung 3, the DHT.** As described above. Your rendezvous is the bootstrap node
(the way into the network: a DHT can only refresh a routing table that already
has somebody in it) and, in a small deployment, the DHT server that stores the
records. Reaching a bootstrap node is retried with exponential backoff for the
life of the process, because the first attempt routinely fails through nobody's
fault (the app launches before the OS finishes bringing up the network, a laptop
resumes from sleep, a VPN flaps) and a failure there is total rather than
partial: a node that never reached a bootstrap peer has no way in at all.

**Rung 4, the public IPFS DHT (opt-in).** The same Kademlia network that IPFS
uses, with thousands of public servers. Turning this on means two people who
have never met, with no server of their own, can still find each other. Its
price is the metadata described in §4.5, now paid to strangers: your peer ID and
addresses become visible on a public network, and the key you advertise under is
guessable, so an observer can enumerate Concord nodes. Messages stay sealed; the
fact of running Concord does not. That is the user's trade to make, so it is off
unless they make it.

### 4.7 Invite codes: the out-of-band rung

There is a fifth path, and it is the one most people use first. An **invite
code** is a compact base64 blob you paste into a chat, carrying the guild's
opaque handle and name, the inviter's peer ID, the inviter's current addresses,
and the rendezvous list the inviter is using. It is discovery by human courier:
the joiner's app configures itself entirely from the code, dials the addresses
it names, and asks to join.

Two details in the implementation. Relay addresses are only included once a
relay reservation is held, because libp2p only lists a circuit address when it
will work, and a code is a permanent artifact: a confidently wrong address
pasted into a chat costs every future joiner a dial timeout. And a rendezvous
the inviter was *configured* with is advertised in its DNS form rather than the
IP snapshot libp2p renders, because a DNS name survives the host being
redeployed onto a different address.

---

## 5. Reachability: NAT, hole punching, and relays

Alice now has an address for Bob. That does not mean she can reach him.

### 5.1 What NAT is, and why it breaks inbound connections

Your home router owns a single public IP address. Your devices have private
addresses (`192.168.x.x`, `10.x.x.x`) that mean nothing on the internet. The
router performs **Network Address Translation**: on the way out it rewrites the
packet's source address and port to its own, and records the substitution in a
table so that replies can be rewritten back.

```
   Bob's laptop            Bob's router                       the internet
   192.168.1.7:51000  ──►  203.0.113.9:40001  ──────────────►  server:443
                           └── table entry created by
                               THIS OUTBOUND PACKET

   server:443 ──────────►  203.0.113.9:40001  ──►  192.168.1.7:51000   ✓ matched
   stranger   ──────────►  203.0.113.9:40001  ──►  ???                 ✗ dropped
```

The critical detail is that the table entry is created by an outbound packet. An
inbound packet that matches no entry names no internal destination the router
could compute, so it is discarded. This is why you can browse the web from
behind a NAT and cannot be dialled, and why the entire client–server internet
works while peer-to-peer needs the rest of this section.

NATs differ in how strictly they match, and the differences decide what works:

| Behaviour | What it means | Punchable? |
|---|---|---|
| Full cone | one external port per internal socket; anyone may use it | yes, easily |
| Address-restricted | only the host you sent to may reply | yes |
| Port-restricted | only the exact host:port you sent to may reply | yes, with care |
| Symmetric | a fresh external port per destination | no |

Symmetric NAT is the killer. If the router assigns a new external port for every
destination, then the port a third party observed you using is not the port you
will use when you contact Bob, so telling Bob that port tells him nothing.

### 5.2 Hole punching (DCUtR)

If a NAT entry is created by an outbound packet, then two NAT'd peers can create
entries *for each other* by both sending at once. That is hole punching.

```
   The problem:                       The mechanism:

   Alice ─►│NAT│  ✗  │NAT│◄─ Bob      1. Both learn the address the outside
           router    router              world observes for the other.
                                       2. Both send to it AT THE SAME TIME.
   Neither router has a table          3. Each router, having just seen an
   entry, so each drops the               OUTBOUND packet to that address,
   other's first packet.                  now has a table entry that permits
                                          the matching INBOUND one.

   Result:  Alice ◄══════════════════► Bob     direct, full speed, no third party
```

Two things must be arranged, and neither peer can do them alone: each must learn
its own externally observed address (which requires someone outside to tell it),
and the two sends must be simultaneous within roughly a round-trip time.

libp2p's answer is **DCUtR**, *Direct Connection Upgrade through Relay*. The two
peers first establish a **relayed** connection (§5.3), then use that channel to
exchange their observed addresses and to measure the round-trip time so both can
start dialling at the same instant. The relay is scaffolding: it exists to carry
the handshake that makes it unnecessary.

**Success rate.** This is a property of the internet's NAT population, not of
Concord. libp2p's own measurement campaigns report roughly 70% success over TCP
and roughly 80% over QUIC. Most pairs go direct; a minority (symmetric NATs,
some carrier-grade NAT deployments, restrictive corporate firewalls) never do,
and for those the relay is not a stepping stone but the connection.

**The escape hatch that removes every intermediary.** Concord lets you pin the
port it listens on. Pin it, forward it on your router, and you are directly
dialable with no punching, no relay and no rendezvous in the path: the one
setting that buys a fully unmediated connection. It is a setting rather than a
default because it needs the port to be *stable*, and an ephemeral port changes
every launch, so no forwarding rule can follow it. Concord checks the port is
free before binding (libp2p will not reliably tell you: its TCP transport sets
`SO_REUSEPORT`, so a second instance silently shares the port and inbound
connections land at whichever identity the kernel prefers), verifies that *both*
TCP and UDP came up on it, falls back to an ephemeral port rather than refusing
to start, and tells the UI when it had to, because a half-working forward that
nothing complains about is worse than no forward at all.

### 5.3 Circuit relay, and why it cannot read what it carries

When punching fails, packets have to go through somebody. libp2p's **Circuit
Relay v2** provides that, and its structure is the reason a relay is not a
server.

```
   Alice ─►│strict│      │strict│◄─ Bob        no direct path exists
             NAT             NAT

   ┌── Noise A↔R ──┐   ┌── Noise R↔B ──┐       the relay's own two hops
   Alice ────────► │ R │ ◄──────── Bob         (it is an endpoint of these)
                   └───┘
   ╞════════════ Noise A↔B ═════════════╡      the peers' own session,
                                                spliced THROUGH the relay
   ╞═════════ MLS group ciphertext ══════╡     inside that, for members only
```

Mechanically: Alice holds a **reservation** with the relay, so it will accept
circuits addressed to her. Bob asks the relay to connect him to Alice; the relay
splices his stream to hers and forwards bytes. Alice and Bob then run their *own*
Noise handshake end-to-end over that spliced pipe, and that is the whole
argument:

- The relay is **not an endpoint of the A↔B session.** It forwards the outer
  ciphertext of a handshake it is not party to. It cannot read it and cannot
  inject into it, because the handshake authenticates both PeerIDs (§3) and any
  tampering fails the authenticator.
- Inside that session sits a **second, independent** layer: MLS group ciphertext
  (§7), openable only by group members. The relay is not a member.
- So a relay learns Alice's IP, Bob's IP, that they are connected, when, and how
  many bytes. Nothing else is available to it, not as a matter of policy but of
  construction.

### 5.4 Why a relay is a fallback and not the norm

- **Latency.** Two hops through a third machine instead of one, plus whatever
  detour its geography imposes.
- **Throughput.** Bounded by the relay's link and by explicit resource limits,
  but not by a per-circuit meter. On the rendezvous: 512 reservations, 64
  concurrent circuits, 8 reservations per peer and 16 per IP. On a peer relaying
  for friends: 32 reservations, 8 circuits, 4 per peer and 4 per IP, because it
  is somebody's laptop rather than a server. There is no per-circuit byte or
  duration cap, and that absence is load-bearing rather than generous: a relay
  advertising *any* limit makes every connection through it a "limited"
  connection on both ends, and go-libp2p quarantines those, with gossipsub
  refusing to attach a limited peer to a mesh. Measured on the relay-only test
  topology with the previous 1 h / 512 MB cap: the two devices connected,
  presence fired, both rendered each other ONLINE, and then not one message
  crossed in either direction. A relayed connection Concord cannot publish over
  is worse than none, so the meter went. What bounds abuse is the counts above,
  plus the member-only ACL on a peer relay, which is the real answer to
  "strangers spending my machine" (§5.5).
- **It is somebody else's bandwidth**, and asking for it should be the exception.
- **It is designed to be replaced.** Every relayed connection is immediately a
  DCUtR candidate; when punching succeeds the circuit is dropped.

### 5.5 A relay is not necessarily *the* rendezvous

This is the part most descriptions of Concord get wrong, including this
document's previous edition. Any sufficiently reachable node can relay, and
Concord uses that:

- **Rendezvous nodes are offered first.** They are dedicated, always public, and
  named in invite codes.
- **Then friends.** A Concord node that finds a routable address on one of its
  own network interfaces starts a relay service. Its access list admits only
  peers it has tagged as guild members, so being publicly reachable does not
  turn a user's machine into free transit for strangers.
- **The candidate list is live, not fixed.** Peers connect and disconnect, the
  user re-points the app at a different rendezvous, addresses are observed later.
  Fixing the relay set at startup would pin a NAT'd peer to a rendezvous for the
  life of the process, so when it died that peer could not be reached at all.
- **Only vouched-for peers are offered as our relay.** Whoever relays for you
  sees every friend who dials you through the circuit: their peer IDs, their
  addresses, when they connect. The circuit address also ends up in your invite
  codes and your DHT record, so it is handed to people who never chose it.
  Offering any connected stranger would let anyone who simply advertises the
  Concord rendezvous key volunteer as your relay and collect the lot.

Known gap: relay privileges, once granted, are never withdrawn. See §13.

---

## 6. The rendezvous node

This is the chapter to read if you only read one. "Serverless" invites the fair
question *what is that thing you deploy to fly.io, then?*, so here is the
complete answer: what it runs, what it can see, what it cannot, and what stops
working the day it dies.

### 6.1 What it is

One Go binary, a couple of thousand lines including everything below, running as
a single process on the smallest machine a host will rent. It holds a stable
identity derived from one environment variable (`CONCORD_RELAY_SEED`) so its
address is predictable and can be embedded in invite codes. It stores none of
your data. There is no database, no user table, no message store, and, apart
from an optional push-token file, nothing on its disk that concerns you.

```
   ┌─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┐
   │                      RENDEZVOUS NODE                              │
   │                                                                   │
   │  1  DHT server        stores provider records (§4)                │
   │  2  Circuit relay     splices two streams (§5.3)                  │
   │  3  Offline mailbox   holds sealed envelopes for absent peers     │
   │  4  Guest gateway     plain-HTTPS door into a meeting (§10)       │
   │  5  Booking gateway   plain-HTTPS door to a host's free slots     │
   │  6  GIF proxy         fetches results and image bytes (§6.6)      │
   │  7  Push bridge       optional, contentless wake notifications    │
   │  8  TURN relay        optional, off by default (§9)               │
   │                                                                   │
   └─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┘
```

### 6.2 Service by service

**1 · DHT server.** Runs the Kademlia DHT in *server* mode, meaning it stores
provider records for others rather than only querying (§4.4). This is what makes
"advertise under the Concord key and find whoever else did" work between two
peers on different networks.

**2 · Circuit relay.** Accepts reservations from peers and splices circuits
between them, as §5.3 describes, with the limits in §5.4.

**3 · Offline mailbox.** With no server holding messages, a direct message to
somebody who is offline has nowhere to go. The mailbox is the minimum viable
answer: a bounded, in-memory, opaque drop box.

```
   sender                                   rendezvous                recipient
   ──────                                   ──────────                ─────────
   tag = SHA-256(recipient account key ‖ "concord-mbx-v1")[0:16]
   seal MLS ciphertext in a NaCl box to the recipient's X25519 mailbox key,
   using a FRESH EPHEMERAL sender key
        │
        └── deposit(tag, sealed) ─────────►  map[tag] → []envelope
                                                    │
                                                    │  ◄─ drain(tag)  on reconnect
                                                    └─  deleted only on ack
```

Each of its properties is a refusal:

- The address is a **16-byte opaque tag**, the truncated hash of the recipient's
  account key with a fixed salt. A sender can compute it (they already hold that
  key as a group credential). The node cannot invert it.
- The envelope is sealed with an **ephemeral sender key**, so the node cannot
  learn who deposited it even in principle. It learns only which peer's
  connection carried the deposit, which it needs for fair accounting.
- It is **memory only**: 64 KiB per envelope, 200 envelopes per box, 64 MiB
  total, 14-day expiry. Nothing is written to disk, which is also why losing the
  node loses nothing that matters. An undelivered envelope is recovered by
  ordinary history sync from any member who has the message (§11).
- **Deposits are only accepted to tags that have registered**, which requires
  proving control of the account key via the connection's own authentication.
  You cannot spam a mailbox that does not exist.
- Overflow within one box does not evict oldest-first. It evicts the oldest
  envelope belonging to the *heaviest depositor* in that box, so a flooder
  displaces only its own mail and cannot flush a victim's genuine pending
  messages.
- Deletion happens on **acknowledgement**, not on delivery, so a recipient that
  crashes mid-processing gets its mail again.

**4 · Guest gateway.** One of the node's two plain-HTTPS surfaces, the two places
where somebody with no Concord install reaches a Concord user at all. It serves
a single self-contained page and pipes that page's WebSocket into a libp2p
stream to the meeting host. Fully described in §10, including the one thing it
*can* read.

**5 · Booking gateway.** The other one, sharing the same HTTPS listener. It
serves the booking page for a `/book/<token>` link and relays each of that
page's requests over one short libp2p stream to the host named in the link.
The node holds no calendar: it does not know which tokens are real, which slots
are free, or what was booked. The host computes and keeps all of it, and an
offline host means the page says so rather than answering from a cache that
does not exist. What this node *does* own is abuse control for the open-web
side. A visitor has no identity, so the only handle to rate-limit a scripted
booker by is their IP, and that is visible only here: browsing slots gets a
loose budget, booking gets a hard trickle, because a booking mints a meeting
room on the host's disk.

**6 · GIF proxy.** See §6.6; the design reasoning is the interesting part.

**7 · Push bridge (optional).** On a mobile device a process that is not running
cannot notice a deposit, so, only if the operator configures platform
credentials, the node sends a **contentless wake** on deposit: for Apple, a
generic "New encrypted message" alert (iOS discards silent pushes from
force-quit apps); for Android, a data-only `{"wake":"1"}`. Tokens are keyed by
the opaque mailbox tag, never by identity, and are rate-limited to one wake per
mailbox per 30 seconds. This is the only component that stops being commodity
infrastructure, because it holds Apple/Google credentials. With no credentials
configured it does not exist, and the mailbox works as before.

**8 · TURN relay (optional, off).** A media relay for hiding participants' IP
addresses from each other. §9 explains what it does, why it is switched off on
the reference deployment, and why a *dead* relay is worse than no relay.

### 6.3 What it can therefore observe

| It sees | Because |
|---|---|
| **IP addresses** of every peer that connects to it | it is the other end of a TCP/QUIC connection |
| **Peer IDs**, i.e. public keys | the transport handshake authenticates both ends |
| **Who talks to whom, and when**, for relayed pairs | it splices the two ends of the circuit |
| **Sizes and timing** of relayed traffic | it counts the bytes it forwards |
| **That a peer runs Concord**, and its addresses | that is what a provider record says |
| **Opaque mailbox tags**, envelope sizes, deposit times | it is the drop box |
| **Guest search terms and the results fetched** (if the GIF proxy is on) | it makes the upstream request on your behalf |
| **A guest's own messages in a meeting** (if a guest joins via the gateway) | the guest's leg terminates at the gateway before continuing to the host |
| **A booking visitor's IP, and every request they make**: the link token, the slot picked, the name and note typed | the booking page's leg is plain HTTPS terminating here, and the node re-marshals the request before relaying it |

That list is the metadata cost of peer-to-peer. It is not a bug that can be
patched out; onion-routed discovery is the only real answer and is future work.

### 6.4 What it provably cannot

| It cannot | Because |
|---|---|
| Read any message, edit, reaction, attachment or history batch | it is not a member of any MLS group and holds no group key |
| Read the peer-to-peer session it relays | it is not an endpoint of that Noise handshake (§5.3) |
| Read any voice or video | media is DTLS-SRTP between browsers and never transits it (§9) |
| Read a mailbox envelope | it is a NaCl box to a key derived from a seed it never sees |
| Learn who is in a guild, or that a guild exists | membership lives in MLS ratchet trees on members' devices; topic names are hashes |
| Impersonate anybody | a PeerID *is* a public key; the handshake would fail (§3) |
| Forge, alter or reorder a message | every message is authenticated under the group key |
| Retain your history | it stores no messages, and the mailbox is bounded, opaque and in memory |

The exceptions in that table's direction are the two plain-HTTPS legs: the
guest leg (§10), which is why guests are meeting-scoped, single-channel and
labelled as guests, and the booking leg, which is why a booking carries a name
and a note and nothing else.

### 6.5 The day it dies

Be specific, because "it degrades gracefully" is what everybody says.

**Keeps working, with no server anywhere in the path:**

- **Existing friendships.** Remembered peers are dialled on the first pass of
  every launch and re-dialled whenever the rendezvous is unreachable (§4.6). If
  a friend is still at yesterday's address, you connect.
- **Everyone on your LAN**, via mDNS.
- **Sending and receiving messages** between connected peers. Delivery is
  gossipsub between peers; the rendezvous was never in that path.
- **History catch-up.** Sync is a direct peer-to-peer request (§11).
- **All group cryptography, membership changes and governance.** These are
  peer-to-peer by construction; there was never an arbiter to lose.
- **Voice and video** between peers who can already reach each other. Media was
  never relayed; signalling rides a peer-to-peer stream.
- **Inbound reachability**, if a friend with a routable address is online to
  relay for you (§5.5), or if you pinned and forwarded a port.

**Stops working:**

- **Meeting anyone new by DHT lookup.** Your only DHT server is gone, so
  `FindProviders` has nobody to ask. Workarounds, in order of convenience: paste
  a fresh invite code out of band (it carries live addresses), turn on the public
  DHT (rung 4), or point the app at another rendezvous.
- **Being reachable at all, if you are behind a strict NAT and no friend can
  relay.** You can still reach out; nobody can reach in.
- **The offline mailbox.** A direct message to somebody offline waits until you
  are both online at once. Nothing is lost; it simply is not stored anywhere.
- **Guest links, completely.** The gateway *is* the door, and it is on that host.
- **GIF search**, and **push wakes** on mobile.

**What re-homing costs.** Paste a new rendezvous address on the login screen.
The peer cache carries your friends across; new invite codes carry the new node.
Old invite codes embed the dead node's PeerID and are worthless, which is the
argument for keeping `CONCORD_RELAY_SEED` stable forever: it is the node's
identity, and every invite code ever issued names it.

### 6.6 The GIF proxy, the one place the node does work on your behalf

Everything else the rendezvous carries is ciphertext. GIF search is different in
kind, and the reasoning is the interesting part.

A GIF picker that calls the provider's API directly from the client sends every
keystroke of every search, and every member's IP address, to that provider.
Discord solves this by proxying through Discord, who then see the searches
instead. Concord proxies through the rendezvous you already run and already
connect to constantly: the same trade, made with an operator you chose.

The property that makes it worth having is that the node fetches **the bytes, not
just the results**:

```
   member                      rendezvous                     upstream GIF API
   ──────                      ──────────                     ────────────────
   search("cat") ──────────►  request + operator's API key ──────►
                              ◄─────────────────────────── results (with URLs)
   ◄── results with NO URLs,
       only HMAC-signed handles
   fetch(handle) ──────────►  verify HMAC, re-check allowlist ───►
                              ◄──────────────────────────── image bytes
   ◄── image bytes ─────────
       rendered as a data: URL

   The member's browser never opens a connection to the provider. Not once.
```

Without that second round trip the feature would be a privacy claim with nothing
behind it: a URL handed to the browser is a request the browser makes, and the
provider sees the IP anyway.

The node fetches arbitrary remote media on peers' behalf, so it is written as
hostile-input plumbing:

- **Peers can never supply a URL.** A result carries an opaque handle: the
  upstream address plus an HMAC under a 32-byte secret generated fresh at
  process start and never persisted. A forged handle and an address that is not
  allowed produce the same innocuous answer. Handles die on restart, and the UI
  says "search again" rather than failing mysteriously.
- **Every fetch is re-checked against a host allowlist**, including on each
  redirect, with a hop limit.
- **Bodies are size-capped** by a limiting reader rather than by trusting a
  declared content length, and the content type must map to a known image
  format.
- **Requests are token-bucketed per peer**, with searches drawing on a much
  tighter bucket than media fetches.
- **The outbound request carries headers the node chose**: one constant
  user-agent, no cookies, no referrer, nothing forwarded from the peer. The
  provider sees one IP for the whole guild and nothing that distinguishes
  members.
- **Upstream error text is never echoed back**, because it can contain the API
  key.
- The client **re-validates everything it gets back** (subtype, size,
  dimensions), on the stated grounds that the node is not trusted to be honest.

With no API key configured the node answers "unavailable" and the picker says so.
That is a supported state, not a failure: it is what lets the client tell an
unconfigured node from a broken one.

Sending a found GIF seals it into an ordinary encrypted attachment, so recipients
need no new code and do not fetch it from the provider either. "Save to this
server's GIFs" moves a good result into the guild's own pack, after which it
needs no proxy at all.

**Who learns what.** The rendezvous operator sees your search terms and which
results you fetched. The upstream provider sees the rendezvous's IP and the
search terms, with no way to attribute either to a person. Other guild members
see an ordinary encrypted image. A network observer sees Noise-encrypted libp2p
traffic to a node you already talk to constantly.

### 6.7 Why "untrusted infrastructure" rather than "server"

A server is something whose correct behaviour you depend on. This is not that.
The distinction is testable: assume the node is fully malicious and enumerate the
damage. It can refuse service, return nothing, drop bytes, count bytes,
correlate IP addresses and timing, and read a guest's leg of a meeting. It cannot
read a member's message, forge one, join a group, learn a roster, or keep your
history. And the system is built so that its absence is a degradation rather
than an outage (§6.5).

That is what "untrusted" means here: not "we promise not to look", but "looking
would not help".

---

## 7. Group encryption: MLS, epochs, and membership with no arbiter

### 7.1 Why pairwise encryption is not enough

Encrypting a two-person conversation is a solved problem. A group is not, and
the two reasons are separate:

- **Cost.** Sealing each message separately for every recipient is O(n) work and
  O(n) bandwidth per message, and establishing sessions after a membership change
  is O(n²).
- **Agreement.** Far more importantly, pairwise sessions have no shared notion
  of "the group". Two members can hold different beliefs about who is in it,
  forever, with no mechanism that notices. In a system with no server to be
  authoritative, that is fatal: encryption is the *only* thing deciding who can
  read, so "who is a member" must be a cryptographic fact and not an opinion.

**MLS** (*Messaging Layer Security*, RFC 9420) is the IETF standard built for
this, and now shipping in Apple and Google's messengers. In Concord every guild
is one MLS group:

```
   Guild "gamers"  ══  one MLS group  (one shared group secret per epoch)
     ├── #general  ══  gossipsub topic   messages, sealed to the group
     ├── #memes    ══  gossipsub topic
     ├── meta      ══  gossipsub topic   channels, roles, profiles, packs (sealed)
     └── control   ══  gossipsub topic   membership changes ("commits")
```

Topic names are hashes, not labels: `concord/c/` followed by the first 16 bytes
of a SHA-256 over the group's identifier and a domain-separated channel
identifier. The pub/sub network cannot tell which guild or channel a message
belongs to, or even that two topics belong to the same guild.

### 7.2 The ratchet tree, an epoch, and a commit

MLS arranges members as the leaves of a binary tree. Every node in the tree
holds a key pair. A member knows the private keys along its own path to the root
and no others. The root secret is the group secret.

```
                       ● root ← this epoch's group secret
                      ╱   ╲
                     ●     ●        every node holds a key pair
                    ╱ ╲   ╱ ╲
     leaves →      A   B C   D      one leaf per member DEVICE

   Alice (leaf A) knows the private keys on A → ● → root. Nothing else.

   To re-key, Alice replaces every key on her path and encrypts each new
   secret to the sibling subtree she is NOT in: 2 ciphertexts here,
   log₂(n) in general. Everyone in the tree can then compute the new root.
   Nobody outside it can.
```

That structure is what makes group rekeying cheap: O(log n) ciphertext for a
full group rekey, rather than O(n).

Two terms follow directly:

- An **epoch** is a version number for the group. Each epoch has its own key
  schedule derived from that epoch's root secret. Epoch *e*'s secrets are
  derived from epoch *e−1*'s plus fresh entropy, forward only, through a one-way
  key-derivation function.
- A **commit** is the message that moves the group from epoch *e* to epoch
  *e+1*. It carries the tree updates, is signed by the committing member's leaf
  key, and must be applied by every other member gaplessly and in order. That is
  what makes comparing two members' epoch numbers a precise statement of which
  commits one of them is missing, and it is the basis of history sync's commit
  backfill (§11).

In Concord, commits are *only* member additions and removals. There are no
standalone proposals, no periodic self-updates, no empty commits. A third
message type, the **Welcome**, is handed privately to a joiner: encrypted to the
*key package* they published (their leaf's public keys plus their credential), it
carries enough group state for them to begin at the current epoch.

### 7.3 Forward secrecy and post-compromise security, concretely

These two phrases are used loosely everywhere. Here is what they mean here,
including their limits.

**Forward secrecy: compromise now does not retroactively open the past.** When
the group advances from epoch *e* to *e+1*, epoch *e*'s secrets are deleted, and
they cannot be recomputed from *e+1*'s because derivation runs one way. An
attacker who takes your device today, with today's keys, cannot open traffic
sealed in previous epochs.

> The limit, stated plainly: forward secrecy protects *captured network
> traffic*. It does not protect *your local database*, which holds your
> decrypted history sealed under a key derived from your seed. Someone who takes
> your device and your passphrase reads your history. Forward secrecy means the
> ciphertext an eavesdropper recorded last month stays useless, not that your
> messages evaporate.

**Post-compromise security: the group heals.** An attacker holding epoch *e*'s
secrets loses them the moment a commit they cannot read advances the group,
because the new root secret mixes in entropy encrypted to the leaves, and they
are either not at a leaf, or their leaf has been removed. Removal is therefore
not a policy flag; it is a rekey that makes the removed member's keys
arithmetically useless.

> The limit: MLS delivers post-compromise security when a member performs an
> update. Concord's only commits are adds and removes, so a guild with a stable
> roster does not rekey on its own. Post-compromise security is realised at the
> next membership change, not on a timer. Discord and most chat applications
> have neither property at all, but "we rekey on membership change" is a smaller
> claim than "we rekey continuously", and it is the true one.

### 7.4 Authorising membership changes with no server to arbitrate

Two questions that look like one, and separating them is the whole design.

**Who *is* a member?** Answered cryptographically, not administratively. You are
a member if and only if you hold a leaf in the ratchet tree. There is nothing to
consult and nothing to disagree about.

**Who *may change* membership?** Answered by a **signed, replayable governance
log** that every peer folds into identical state.

```
   Each governance operation carries:
       ├─ the author's ACCOUNT public key
       ├─ a sequence number and a wall-clock timestamp
       ├─ the operation itself   (role_upsert | role_delete | role_assign |
       │                          ban | unban | mute | unmute | slow_mode |
       │                          transfer_owner | set_heir | claim_heir)
       └─ an Ed25519 signature over all of the above

   Operations travel MLS-ENCRYPTED on the guild's meta topic, and are re-served
   in history sync. Every peer sorts them by (sequence, timestamp, hash), a
   deterministic total order independent of arrival order, and folds them into
   roles, role assignments, bans, mutes, per-channel slow mode, and who the
   guild's owner and designated heir currently are. Invalid or unauthorised
   operations are skipped, not fatal.
```

The trust anchor is the **current owner**, and *current* is doing real work.
The founding key, the account stamped into the guild when it was created, is
only the seed the replay starts from. Ownership then moves along a chain of
`transfer_owner` ops, each signed by the then-reigning owner, and the anchor is
the head of that chain: a founder who handed the guild over is an ordinary
member from that op onward, and a stale op they sign afterwards is dead on
arrival. Because the chain is folded in the same canonical order everywhere,
every peer computes the same head without asking anyone. No MLS state is
touched by a handover at all; the crown is a fact about the log, not about the
ratchet tree.

`set_heir` and `claim_heir` are the same mechanism aimed at the owner who
*vanishes* rather than hands over. Only the reigning owner may name an heir (or
revoke the designation with an empty target), and only that heir's own
signature converts the designation into ownership. The claim is valid whenever
the heir uses it, and is not gated on the owner having gone quiet, since
wall-clock liveness is not a fact two sides of a partition can agree on, and
gating on it could crown two owners at once. So an heir holds a permanent,
revocable break-glass, and the UI says as much when it is granted. Any ownership
change voids the standing designation: it was the old owner's trust decision,
and the new owner names their own.

From there, permissions are a bitmask (manage members, manage messages, manage
channels, manage guild, manage roles, mute members, act as a sync host), and the
escalation-prevention invariants are enforced during replay on every peer:

- a role you create cannot carry permissions you do not hold;
- a role you create or edit must rank strictly below your own highest rank;
- only the current owner may grant a role to the current owner;
- the current owner cannot be banned or muted;
- only the current owner transfers ownership or names an heir, and a banned
  fingerprint can neither inherit nor be transferred to;
- `slow_mode` rides manage-channels, and its six-hour ceiling is clamped inside
  the replay rather than in the UI that issues it, so a hand-crafted op folds to
  the same state on every peer.

**Then the two halves are joined.** When a commit arrives on the control topic,
every receiver reads the committer's identity out of the commit's *signed public
framing*, no group secrets required, maps the leaf index to an account, and
refuses to apply the commit unless that account is the current owner or holds
manage-members. A patched client can publish whatever commit it likes; it is
simply dropped by everyone. The gate runs identically on the live control topic
and on commits backfilled through history sync. Two related refusals fall out of
the same rule: a member who cannot commit is refused when they try to *mint* an
invite code, and refused again when a joiner asks them to serve one, because a
code that cannot be honoured would fork the joiner onto a private epoch, which
is a worse failure than an error message.

Bans are enforced at the gate rather than merely recorded: a join request is
matched against the banlist by the *account* fingerprint of the presented
credential, which is what makes a ban survive rejoining under a new device.

### 7.5 What this design costs: no merge

There is no conflict resolution for concurrent commits, and pretending otherwise
would be the hand-waving this document is trying to avoid.

Two authorized members who commit at the same epoch fork the group. One branch
loses. Its members do not reconcile; they **recover by being re-welcomed**. A
heal loop notices the group is stranded, finds an authorized committer that is
online, sends a fresh key package, and joins at the current epoch, overwriting
its local group state. Local mitigations keep this rare (a mutex serializes
add-and-publish on one node; the first commit seen at an epoch wins the local
log slot) but concurrency across two machines is unhandled, and the recovery
path is re-admission rather than merge.

### 7.6 Engineering notes

The cipher suite is MLS suite `0x0001`
(`MLS_128_DHKEMX25519_AES128GCM_SHA256_Ed25519`), chosen because its signature
algorithm is Ed25519, the same algorithm as a Concord account. That alignment
buys a real property: the MLS signing key is **derived from your seed** by
HKDF-SHA256 with the label `concord-mls-sig-v1`, so a restarted device
recomputes it and keeps signing with nothing to back up. (On a *linked* device
the MLS credential is the device certificate and the signing key is the device
key itself, so the same property holds per device.) Supplying the signing key
rather than letting the library generate a random one took a single isolated
patch to the vendored MLS library; it adds no cryptographic logic, and the
library's RFC 9420 interoperability vectors still pass.

One exposure to name. Commits are MLS *public* messages, signed rather than
encrypted, because a receiver must be able to identify the committer before
applying anything. The control topic's name is a hash of the group identifier,
so only members can derive it. But a **removed** member still knows that
identifier, and can therefore keep subscribing and watch the roster change after
their removal. Message content stays sealed; membership churn does not.

---

## 8. The life of a message

Everything above, in one narrative.

Alice types "hello" in `#general` of a guild she shares with Bob and Carol. Bob
is online. Carol is asleep with her laptop shut.

```
  ALICE                                                            BOB
  ─────                                                            ───
  ① a Message record is built: id, sender = Alice's account key,
     content, timestamp, kind
       │
       ▼
  ② sealed to the GUILD's MLS group at the current epoch.
     Only members' keys open it. Alice's leaf signature authenticates it.
       │
       ▼
  ③ published to gossipsub topic  concord/c/<hash(groupID, channelID)>
     (a name that reveals neither guild nor channel)
       │
       ├──── ④ stored locally: body sealed again at rest under the
       │         store key derived from Alice's seed
       │
       ├══════ peer-to-peer, inside a Noise session ═══════════════►  ⑤ arrives
       │       (direct if punched, through a circuit if not;                │
       │        either way the relay is not an endpoint of it)               ▼
       │                                                     ⑥ MLS.Decrypt →
       │                                                        "hello", plus
       │                                                        authenticated
       │                                                        proof of author
       │                                                              │
       │                                                              ▼
       │                                                     ⑦ stored, rendered
       │
       └──── ⑧ for CAROL, who is offline: the same ciphertext is sealed
                again in a NaCl box to her mailbox key, using a fresh
                ephemeral sender key, and deposited at the rendezvous under
                her opaque 16-byte tag. It sits in memory for up to 14 days.
                     │
                     ▼
                Carol opens her laptop → registers → drains her mailbox →
                acknowledges (which is what deletes it) → and, separately,
                asks a connected member for anything else she missed (§11).
```

Two things to underline:

- **Two independent encryption layers, for two different adversaries.** MLS
  protects the message from anyone who is not a group member, including every
  relay and the rendezvous. Noise protects each hop from anyone watching the
  wire. The rendezvous is neither a group member nor an endpoint of the peers'
  Noise session, so it holds bytes it cannot interpret in either layer.
- **Edits, deletes, reactions and pins ride the same channel.** They are
  ordinary messages with a `kind` and a target message identifier, equally
  encrypted and equally authenticated. Each re-emits the updated state so every
  peer converges, and because storage is idempotent by message identifier,
  replays are harmless. Authorization is enforced where it can be checked: only
  the original author may edit or delete, matched against the stored sender key.

---

## 9. Voice and video

### 9.1 Media is peer-to-peer and never touches the backend

Text is small and delay-tolerant. Audio and video are large and real-time, and
the browser already has a well-tested stack for that. So Concord splits the job:
the browser does the media, Go does the matchmaking.

```
   Alice's browser  ══ WebRTC: Opus audio + video, DTLS-SRTP ══►  Bob's browser
        ▲                                                              ▲
        │  SDP / ICE "signalling": how to connect                      │
        └──── relayed as opaque blobs over a libp2p stream ────────────┘
              presence rides gossipsub · Go never touches a media frame
```

There is no media code in the Go tree at all: no WebRTC stack, no track
handling, no mixer, no forwarding unit. The claim "media never touches the
backend" is therefore not a policy but an absence. There is nothing there that
could.

**What signalling is.** Two browsers cannot start a call by wishing at each
other. Each must tell the other what codecs it supports and what network
addresses to try: a *session description* (SDP) and a set of *candidate*
addresses (ICE). That exchange is signalling. It is small, and it needs any
authenticated channel at all rather than a special one. Concord carries it over
a dedicated libp2p stream between the two peers, one fire-and-forget framed
payload per message, and carries voice-room presence on a gossipsub topic per
channel with a 3-second heartbeat. So the thing a commercial platform runs a
signalling server for is here just another peer-to-peer message.

**Topology is a full mesh**: every participant opens one connection to every
other. Glare (both sides offering at once) is resolved deterministically by
comparing peer IDs, so exactly one side is "polite". A session nonce rides every
signalling message so a page refresh is distinguishable from a heartbeat, which
is needed because a relayed connection's allocation can outlive the tab and keep
reporting itself healthy for minutes. A mesh is why voice targets small rooms:
it is the bandwidth ceiling of pure peer-to-peer.

**Quality is tuned, not left to the default.** Browsers negotiate Opus timidly,
around 32 kbit/s, mono, tuned for a bad phone line. Concord rewrites the Opus
parameters in every offer and answer: fullband 48 kHz, in-band forward error
correction on, discontinuous transmission off, and a configurable bitrate (64
kbit/s by default, clamped to 8–320 kbit/s). Because those parameters describe
what you will *receive*, the sender's own encoder ceiling is raised separately,
and voice is marked high-priority network traffic. A screen share's audio rides
its own media line in **stereo** at a higher rate, because music through a mono
speech codec is the thing everybody notices.

**Screen sharing and camera are independent sources**, each with its own senders
per peer, so either can be added or removed without disturbing the other. Stream
kind (screen or camera) is announced out of band so the UI can lay out focus and
theatre tiles correctly. On platforms that capture no audio with the screen,
Concord can capture the chosen audio input instead with echo cancellation, noise
suppression and automatic gain all off: the Linux "Monitor of *output*" case.
Devices swap with a track replacement rather than a renegotiation, so switching
microphone mid-call leaves no gap.

### 9.2 IP privacy: the state of it

WebRTC connects peers directly. That is the point, and it has a consequence:
each participant learns the others' IP addresses. Among friends this is
unremarkable, and equally true of BitTorrent and most VoIP. For a **meeting link
you paste somewhere public** it is a deanonymization vector: a stranger who
clicks it sees your home IP, and you see theirs.

The standard fix is a **TURN relay**. When a client sets its ICE transport
policy to relay-only it offers *only* relayed candidates, so its real address
never leaves the machine. If both ends relay, media flows client → relay →
client and neither participant sees the other's address. The relay does, since
it has to in order to forward, but the relay is already untrusted infrastructure
that cannot read the media, which stays DTLS-SRTP encrypted end to end. The
trade is moving one piece of metadata from "any stranger with a link" to "the
one node you already bootstrap through". Credentials are time-windowed HMACs
derived from a shared secret, so the secret itself never ships to a client.

Concord implements this. It is off on the reference deployment, and the reason is
measured rather than theoretical:

> A TURN relay cannot work on fly.io. A TURN server hands each client an
> ephemeral *relayed transport address* on the relay host and advertises it. When
> two clients both relay, packets must travel to those advertised addresses,
> which means the relay sends to its own public address and needs the platform's
> edge to hairpin them back. Measured against the live deployment: packets
> between two allocations on the same machine are dropped, because fly's edge
> does not hairpin UDP back to the advertised public IP. The relay
> authenticates, allocates, reports success, and carries nothing.

This failed in the worst possible way before it was understood, and the shape of
that failure is why the code now behaves as it does. With no relay address
configured, the old default advertised `0.0.0.0`; allocation *succeeded*; clients
were handed `0.0.0.0:port` as their relayed address, a place nothing can send
anything. Because allocation succeeded, the credential endpoint reported a relay
as available, so the app forced relay-only transport for guest meetings and for
"hide my IP". Those calls then had one candidate and it was useless. Chat kept
working, because chat is libp2p rather than WebRTC, while audio never arrived and
shared screens stayed black.

So the relay now **refuses to start** unless it can produce a routable relay
address, resolving the deployment's own public hostname when it has not been told
its IP explicitly. The trade is stated in the code, and here:

> A dead relay is worse than no relay. No relay means calls connect directly:
> you lose IP privacy, which is a real loss, but a working call beats a private
> one that cannot carry sound.

When no relay is available, the app says so ("No relay available, this call won't
hide your IP"), greys out the "hide my IP" switch, and connects directly. A guest
page with no relay prints the same warning in plain words. Nothing silently
pretends.

**If IP-private calls matter to you**, host the relay on a machine that owns its
public IP, any small VPS, and point the TURN settings at it. That is the whole
fix: privacy or connectivity, decided by whether your host will let a relay be a
relay.

One more note about outbound contact during a call. When no rendezvous supplies
ICE configuration, the fallback is a public Google STUN server, contacted at call
time to learn your own external address. It fetches no assets and reveals nothing
but your IP and the fact that you are starting a call, but it is a third party,
it is hardcoded, and this document would be dishonest not to name it.

**A future optional SFU**, a *selective forwarding unit*, which forwards frames
without decoding them, could scale rooms beyond what a mesh allows while
forwarding still-encrypted frames, so it would not become trusted. It does not
exist yet.

---

## 10. Guests: joining with no account

Sometimes the person you want in a call should not have to install anything: a
client, a contractor, somebody who will be in one meeting. Guests are that path,
and they are the one place Concord relaxes its trust model, so the relaxation is
bounded on every axis.

### 10.1 How it works

```
   HOST (a full Concord peer)                                 GUEST (a browser)
   ──────────────────────────                                 ─────────────────
   creates a "meeting" guild, mints a link:
     https://<gateway>/guest#h=<host peer id>&t=<24-byte token>
                    ▲                └──────────┬──────────┘
                    │                     in the URL FRAGMENT, which a
                    │                     browser never sends to the server
                    │
                    │            guest opens the link ──────────────┐
                    │                                               ▼
              ┌─────┴──────────────────────────────┐      one self-contained
              │        RENDEZVOUS GATEWAY          │      page, hard CSP,
              │  serves the page; upgrades to a    │◄──── nothing external
              │  WebSocket; pipes it byte-for-byte │      TLS
              │  into a libp2p stream to the HOST  │
              └─────┬──────────────────────────────┘
                    │  Noise
   validates token ◄┘
   runs the whole session; is the guest's
   crypto endpoint into the MLS group
```

The gateway is a **dumb pipe**: it dials the host (reachable because the host
holds a relay reservation with that very node), forwards newline-delimited
frames in both directions, and keeps no state. It does not validate the token,
because it cannot: it has never seen one. It does not read the meeting.

The token is a **24-byte bearer secret** from a cryptographic random source, not
a signed structure. There is nothing to verify because there is nothing to
verify against: the host looks it up in its own table. That table is persisted
in the host's encrypted database, so a link mailed out on Monday still works on
Wednesday after the host has restarted, and re-minting a link for the same
meeting reuses the same token with a new lifetime, so changing a duration never
invalidates a link somebody already has. Expiry is checked twice at connect
time: the token's own expiry, and the meeting's. Lifetimes are a short menu (an
hour, a day, a week, a month) with a hard ceiling, because a host picks "office
hours all week" rather than solving a duration puzzle, and a fixed set is also
what stops a peer talking your client into keeping a room forever.

### 10.2 What a guest can and cannot reach

**Can:** send and read chat in one channel of one meeting; see the last 30
messages of it; join the call with audio; turn on camera; share their screen.
They appear in the roster explicitly labelled as a guest, under a self-asserted
display name that is sanitized of characters that could fake formatting or a
mention.

**Cannot:** reach any other channel (there is no frame that changes channel);
reach any other guild; read history beyond those 30 messages; hold an identity,
a key package, or a leaf in the MLS group; be verified; appear in a member's
contact list; or persist beyond the meeting.

The bounds are explicit: at most 5 guests per meeting, 24 bytes of display name,
2000 bytes per message, a chat rate limit of 5 messages refilling one per second,
a separate and much larger bucket for signalling, a 20-second window to say
hello, a 10-minute idle timeout outside a call and 3 hours inside one.

A meeting can be **locked**, in which case an arriving guest *knocks* instead of
joining. A knocking guest is registered (and counts against the cap) but
completely inert: no chat, no history, no roster, no signalling, no call, until
admitted, re-checked at the point of delivery as well as at the gate. The lock
is a **lease** rather than a flag, re-announced every few seconds while it is on,
so a host that crashes or reloads unlocks itself instead of leaving a door
nobody alive can open.

### 10.3 The cost

**The guest's own leg is readable by the gateway.** A guest has no identity, so
there is no key to encrypt to and no handshake to bind: their traffic is TLS to
the gateway and then Noise to the host, never plaintext on the wire, but the
gateway is an endpoint of the first hop. Member-to-member traffic in that same
meeting stays end-to-end encrypted and unreadable to it; the guest's own
messages, in that hop, are not.

That is the price of "no install". It is not fixable without giving guests
identities, at which point they are members, and it is why guests are
meeting-scoped, single-channel, capped, and labelled.

Guest chat *is* MLS-encrypted onto the guild's topic: the host is the guest's
crypto endpoint and seals on their behalf, with the message marked as a guest
message and carrying the guest's self-asserted name as decoration. A member
reading it should understand it as the UI presents it: *somebody who called
themselves this, relayed by the host.*

Guest media is ordinary WebRTC, direct between browser and member app, DTLS-SRTP
encrypted, never through the rendezvous. Guest meetings are the case that most
wants the TURN relay of §9.2, and with no relay available, the guest page says so
in plain words rather than quietly exposing everyone.

---

## 11. Storage, history, and catching up

### 11.1 On disk

Your history lives only on your own devices. Locally it is a **SQLite database**
(pure Go, no C toolchain), and what is protected is specific rather than
hand-waved:

- **Message bodies are sealed** with NaCl secretbox under a key derived from your
  seed by HKDF with the label `concord-store-v1`, with a fresh nonce per write. A
  stolen database file yields no readable messages.
- **Attachments are sealed under their own per-blob key**, which is not in the
  database at all; it travels inside the (sealed) message body. See §11.3.
- **What stays in the clear** is worth reading twice, because "encrypted at
  rest" is often heard as more than it is: sender account keys, display names,
  timestamps, message kinds, reply and pin state, reactions with the fingerprint
  that made them, guild and channel names, categories, profiles including
  avatars and banners, custom emoji images, nicknames, read state, the signed
  governance log, and settings. Also outside the database: the MLS group state
  directory. So a stolen laptop without the passphrase yields the *shape* of your
  social life, who and where and when and in which rooms, while message content
  stays sealed. §13 states this as a gap rather than leaving you to infer it.
- **Deletion overwrites.** A delete replaces the body with a sealed empty string
  and leaves a tombstone row, so the content is gone rather than merely flagged.

**Search is local-only and has no index.** It walks your own messages newest
first, decrypts each body, and matches in memory, stopping as soon as it has
enough results. That is slower than a server-side index and it is the entire
point: the query never leaves your machine, and a server-based application
structurally cannot offer you that. Unread counts are computed with SQL alone,
decrypting nothing. **Export** dumps a channel to Markdown, because your data is
yours.

### 11.2 Catching up with nobody holding anything for you

With no server buffering messages, a peer that was offline has to ask. The
protocol is a direct peer-to-peer request:

```
   Bob was offline ─────────────────────────►  Bob reconnects
      (missed messages, a rename, two                  │
       new roles, a membership change)                 ▼
                          "for guild G: I am at epoch 7, and here is my
                           latest timestamp per channel"
                                                       │
                                       ◄───────────────┴─────────────────
                                       ① MLS commits after epoch 7, in the
                                          clear (they are signed public
                                          messages, and are re-authorised
                                          on arrival as if they had come
                                          live)
                                       ② one payload, MLS-ENCRYPTED to the
                                          group at the responder's epoch:
                                          messages, profiles, categories,
                                          emoji, GIF-pack records,
                                          governance operations
                                                       │
                                                       ▼
                          applied idempotently by message id; the UI fills
                          in the gap
```

Details that matter:

- **The cursor is per channel**, taken as the latest of *sent* and *updated*, so
  a reaction or a pin on an old message brings it along too. It is backdated by
  five minutes to absorb clock skew between two machines that trust no clock.
- **The responder answers only members**, checked against the connection's
  authenticated identity. Membership is not a courtesy here: add commits carry
  joiners' key packages in the clear, so serving them to a non-member would leak
  the roster.
- **Everything is bounded**: 200 messages per channel per response, a 700 KiB
  payload budget under a 1 MiB transport frame, two rounds per guild per peer, a
  20-second timeout. Truncation is safe because whatever is saved advances the
  cursor and the next round continues from there.
- **Epoch gaps are reported, not papered over.** If the responder's commit log
  cannot bridge the requester's epoch to its own gaplessly, it says so, and the
  requester marks the guild out of sync and heals by re-admission (§7.5).
- **Sync is not only on reconnect.** It runs on peer connect, on a periodic
  anti-entropy loop, whenever applying a live commit fails, and when the OS
  resumes the app. Peers holding the sync-host permission are asked first.

**The trust boundary, stated plainly.** Synced content is *attested by the
serving member*, from their local copies, and is not re-verified against each
original sender. This is the same trust a centralized app places in its server,
narrowed to people who are already in your guild. Two real gates sit on top of
it: MLS commits are re-authorised through the §7.4 governance gate as the epoch
advances, so a member cannot smuggle in an unauthorised membership change; and
*destructive* reconciliation, meaning tombstoning, overwriting, replacing
reactions, or overwriting an already-known profile, is restricted to the guild
owner or a member holding the sync-host permission. An ordinary member may only
fill gaps. The residual gap is that an ordinary member could forge a *new*
message attributed to somebody else; closing it wants per-message author
signatures.

### 11.3 Attachments

Images and files never travel inline. Each is sealed with a fresh random key,
content-addressed by the SHA-256 of its *ciphertext*, and referenced from the
message body by a token carrying the blob identifier and the key. The key lives
only inside the (MLS-encrypted, then at-rest-encrypted) message, so the reference
is a **capability**: holding it is what lets you decrypt, and the ciphertext is
useless without it.

Fetching walks connected members of the relevant guild one at a time rather than
fanning out, verifies the hash before storing, and moves on if it does not match.
Serving is ungated on purpose: any peer that asks gets the ciphertext, because it
is meaningless without the key and the 256-bit identifier is unguessable. The
payoff is that every member who has viewed an image becomes a source, which is
what keeps pictures loadable after the sender goes offline. Caps: 5 MiB per
inline image, 25 MiB per generic file, a 1 GiB local cache evicted
least-recently-used.

---

## 12. Assets: why nothing is fetched at runtime

Concord bundles a lot: 3,720 Twemoji SVGs, 143 animated emoji, 101 meme
templates, and eight typefaces in 22 subsetted WOFF2 files. That is roughly 25 MB
of the binary. (The meme templates are the one optional set: they live in
`frontend/public/memes/`, and the editor falls back to bring-your-own images when
that directory is absent.)

The principle: no webfont, icon or image is ever fetched at runtime. A request to
a font host or an image CDN on every launch tells that host your IP address and
the moment you opened the application. Repeat it daily and it is an activity log
kept by a third party you never chose, describing an app whose entire purpose is
that no third party knows anything. There is no way to have this feature and the
CDN. So the assets ship in the binary, every stylesheet reference is same-origin,
and the favicon is an inline data URI rather than one more request.

The same reasoning shapes three features that *could* have been remote:

- **Guild GIF packs and custom emoji.** A guild curates its own collection.
  Records travel on the guild's meta topic (MLS-encrypted); the images ride the
  ordinary encrypted-attachment path (§11.3) and post as ordinary image
  messages. Nothing leaves the machine, and searching a pack is a local string
  match.
- **Link previews and YouTube embeds** are off by default, and when off the
  placeholder says why in plain words ("previews are off, this contacts
  Google"). Turning them on is one click; so is a second click to load an embed.
  Non-YouTube previews are fetched by the local backend behind an SSRF guard,
  not by the browser.
- **Game box art.** A profile can list the games you play, and Steam's public
  storefront has the covers. Those are images on Valve's CDN, fetched by the app
  itself, so rendering one hands Valve your IP and the time — and a profile card
  would do it with no click at all, which is the link-preview problem wearing a
  different hat. So covers are **off by default** and collections render a
  gradient generated from the title hash instead; the fallback was designed to
  stand on its own rather than to look broken, which is what makes the default
  affordable. The backend also allowlists the cover URLs it will store
  (`validGameCover`), because an arbitrary-host URL in a profile would be a
  deanonymizing beacon aimed at everyone who opened your card. The allowlist
  bounds *which* third party; the switch is what bounds *whether*.

### An asymmetry in how packs are received

Worth documenting because it is a real hole, self-documented in the source, and
the kind of thing that belongs in a design paper rather than being discovered
later.

```
   GOSSIP path (guild meta topic)          HISTORY-SYNC path
   ──────────────────────────────          ─────────────────
   MLS authenticates the announcer         MLS authenticates the RESPONDER
            │                                       │
   check: does the announcer hold          check: … impossible.
   MANAGE GUILD?                           Catch-up is served by whichever
            │                              member answered, NOT by the admin
        yes ▼ apply                        who created the record.
        no  ✗ drop                                  │
                                                    ▼ applied after format
                                                      validation only
```

The gossip path verifies that the member announcing a GIF-pack entry or a custom
emoji holds the manage-guild permission. The history-sync path cannot, because
requiring the *responder* to be an admin would stop an ordinary member handing
over a pack that is legitimately theirs to relay. The consequence is real: a
member without manage-guild can inject a pack record, or replace an existing
emoji's image, by serving a doctored snapshot.

What bounds the damage: records are validated as a local addition would be (name
and tag patterns, a 500-entry pack ceiling for new identifiers, image data URIs
restricted to PNG/JPEG/GIF/WebP under 256 KiB, with SVG excluded so there is no
stored scripting), the record's own guild claim is discarded in favour of the
topic it arrived on, and deletions cannot be injected at all because the sync
payload carries only additions.

Closing it properly wants pack and emoji records to carry the creating admin's
signature, the way governance operations already do (§7.4). That is a larger
change than moving one line, and one that should cover both at once. Until then
it is stated here rather than buried.

---

## 13. Threat model: what Concord defends against

Security claims are meaningless without a stated adversary.

### Defends against

| Adversary | Why they fail |
|---|---|
| Network eavesdropper (Wi-Fi, ISP) | Every hop is Noise- or DTLS-encrypted; payloads are independently MLS- or SRTP-encrypted. They see ciphertext twice over. |
| A malicious or compromised rendezvous / relay | Never a group member, never an endpoint of the peers' own session. Its worst behaviour is refusal (§6.7). |
| "Hack the server" | There is no central store to breach, subpoena, or leak. |
| Message forgery or tampering | Every message is MLS-authenticated under the group key and signed by the sender's leaf. |
| Impersonation on the network | A PeerID *is* a public key; the transport handshake fails against anyone who does not hold the private half. |
| Impersonation at first contact | Out-of-band fingerprint ("safety number") comparison, plus a local notice when a contact's device set grows. |
| Retroactive decryption of recorded traffic after a key leak | Forward secrecy: past epochs' keys are deleted and cannot be recomputed. |
| A removed member reading new messages | Removal is a rekey, not a flag; their keys become useless (§7.3). |
| A member escalating their own privileges | Governance operations are signed and replayed identically on every peer, under invariants that cap what a role may carry and where it may rank. |
| A stranger learning anything by messaging you | An unaccepted message request discloses nothing: not your profile, not your mailbox key, not that you are online. |
| The GIF provider profiling your guild | No member's browser ever contacts it; it sees one IP and no way to tell members apart (§6.6). |
| A font or asset host logging your launches | Nothing is fetched at runtime (§12). |

### Does not defend against, and the known gaps

| Not protected | The detail |
|---|---|
| **Metadata privacy** | Inherent to peer-to-peer. The rendezvous sees IPs, timing and sizes; peers you connect to learn your IP, as in any P2P system. Onion-routed discovery is future work. See §6.3 for the complete list. |
| **Anonymity** | Concord authenticates identities; it does not hide them. If you need anonymity, use a network layer built for it. |
| **A malicious member you invited** | Encryption protects the group's secrets from outsiders. It cannot stop an insider screenshotting, and it is not meant to. |
| **A stolen device, in full** | Message bodies, attachments and the identity seed are encrypted at rest. Sender keys, timestamps, reactions, profiles, avatars, guild and channel names, the governance log, settings, and the MLS group state directory including leaf private keys, are on disk in the clear. Someone with the disk learns the shape of your social life and can decrypt captured group traffic at the current epoch; message *content* in your database still needs your passphrase. |
| **`peers.json` is a plaintext contact list** | Remembering peers so friendships survive the rendezvous means writing peer IDs, addresses and last-seen times beside the encrypted store, readable without the passphrase. Each entry converts directly into the fingerprint the app shows for verification, so a stolen laptop or a config directory swept into a cloud backup yields the social graph, who and where and when, while every message stays sealed. Individually these addresses crossed the wire anyway; the *set* is what the encrypted store is otherwise careful never to write in the clear. It wants sealing with the same key as everything else, which means loading it after unlock rather than before, which is why it is not done yet. |
| **Relay privileges are granted but never revoked** | Removing or banning someone drops them from the guild, but nothing withdraws the tag that grants them relay access, so until the process restarts they keep it and keep being re-dialled. |
| **History-sync authenticity** | Content served during catch-up is attested by the serving member, not re-verified per original sender. Commits are re-authorised and destructive reconciliation is restricted, but an ordinary member could forge a new message attributed to someone else. Wants per-message author signatures (§11.2). |
| **GIF-pack and custom-emoji records injected via sync** | The gossip path checks manage-guild; the sync path cannot. Bounded by format validation and pack ceilings, not by authority. Wants signed records (§12). |
| **A removed member watching the roster** | Commits are signed but not encrypted, and a former member still knows the group identifier, so they can keep observing membership churn. Content stays sealed (§7.6). |
| **The `contacts` table grows and is never pruned** | One row per peer ID ever seen. Only unverified rows are ever deleted, and this is the same mechanism that lets strangers reach you at all. |
| **Concurrent membership commits do not merge** | One branch loses and its members recover by re-admission (§7.5). |
| **A call reveals your IP unless a relay carries it** | And on some hosts a relay cannot work at all (§9.2). Guest meetings are the case that most needs one. |
| **Push notifications require trusting the node** | The push bridge holds Apple/Google credentials; wakes are contentless and keyed by opaque tag, but that node stops being commodity infrastructure. Self-hosting without push is fully supported. |
| **Your passphrase is the at-rest anchor** | Configure convenience auto-unlock and at-rest encryption no longer protects you from someone who compromises that machine. |
| **Public-scale spam and abuse** | This is a friend-group tool, not a public platform. |

### Concord versus a centralized platform

| Property | Discord | Concord |
|---|---|---|
| Who can read your messages | The operator (plaintext to them) | Only group members (MLS end-to-end) |
| Central point to hack, subpoena, or ban | Yes | None (P2P; one optional untrusted node) |
| Forward secrecy / post-compromise security | No | Yes (MLS ratchet; PCS on membership change) |
| Where history lives | Their datacenter | Your devices |
| Private full-text search | Server-side; they see your queries | Local-only |
| Data export | Limited | One-click Markdown |
| Identity | A row in their database | A keypair you own |
| Self-hostable infrastructure | No | Yes, and untrusted when you do |
| Runtime asset fetches | Many | None |
| Running cost | Their servers | Roughly free |
| What it costs you | n/a | Small rooms; metadata is not hidden |

---

## 14. Map of the code

About 25,000 lines of product Go plus roughly 10,000 of tests, in strict layers
where each depends only on those below it, and a Svelte front end of about
42,000 lines. Every dependency is pure Go, with no C toolchain, which is what
makes "one file, download and run" possible on every platform.

```
  ┌────────────────────────────────────────────────────────────────┐
  │ UI          Svelte app: guilds, chat, voice, settings          │  frontend/
  ├────────────────────────────────────────────────────────────────┤
  │ App / API   Service orchestrates everything; a transport-      │  internal/app
  │             agnostic bridge exposes it identically to the      │  internal/bridge
  │             browser build (HTTP + SSE) and the native window   │
  ├──────────────┬──────────────────┬──────────────────────────────┤
  │ Domain       │ Storage          │ Media                        │  internal/domain
  │ pure model:  │ encrypted SQLite │ WebRTC in the browser;       │  internal/store
  │ guilds,      │ history, search  │ Go relays signalling only    │
  │ channels,    │                  │                              │
  │ messages,    │                  │                              │
  │ topic hashes │                  │                              │
  ├──────────────┴──────────────────┴──────────────────────────────┤
  │ Network     libp2p host · mDNS + DHT discovery · relay ·       │  internal/net
  │             gossipsub · invite/sync/signal/link/attach streams │
  ├────────────────────────────────────────────────────────────────┤
  │ Crypto / ID Ed25519 identity · device certificates ·           │  internal/identity
  │             MLS group encryption · at-rest sealing             │  internal/crypto/mls
  └────────────────────────────────────────────────────────────────┘
```

```
  internal/identity/    Ed25519 identity, device certs, Argon2id keystore, BIP39
  internal/crypto/mls/  MLS group-encryption engine (swappable Engine interface)
  internal/domain/      pure model: guilds, channels, messages, topic derivation
  internal/net/         libp2p host, discovery, relay, gossipsub, and the
                        invite / sync / signal / link / attach / guest /
                        gifsearch / release stream protocols
  internal/store/       encrypted SQLite: history, reactions, settings, search
  internal/mailbox/     the offline store-and-forward mailbox + push bridge
  internal/link/        device-linking offers and HMAC proofs
  internal/app/         the Service: orchestrates everything; the shared API
  internal/bridge/      transport-agnostic bridge; signed self- and peer-update
  main_web.go           browser-served front end (HTTP + SSE)
  main_wails.go         native desktop window (Wails)
  frontend/             Svelte UI, and every bundled asset
  cmd/rendezvous/       the untrusted node: DHT, relay, mailbox, guest gateway,
                        GIF proxy, optional TURN
  mobile/               gomobile core for the Android/iOS shells in apps/
  third_party/mls-go/   vendored MLS library + the deterministic-key patch
```

**Where the tests are pointed.** Identity and keystore behaviour (the seed never
appears in the keystore file; a wrong passphrase fails closed); MLS group flows
(ratchet, removal, outsider exclusion); at-rest encryption and wrong-key
failure; message-action authorization; governance replay and escalation
invariants; the guest framing contract (§10); and in-process multi-peer
integration tests under Go's race detector.

How to build and run all of this is in [RELEASING.md](RELEASING.md) for
maintainers and [CONTRIBUTING.md](../CONTRIBUTING.md) for the development loop.
