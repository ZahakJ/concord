# Security policy

Concord is a peer-to-peer, end-to-end-encrypted chat app. What it protects, and
what it does not, is set out in the
[threat model](docs/DESIGN.md#13-threat-model-what-concord-defends-against). Read that before reporting.
Several properties people expect are listed there as known gaps, and a report
that restates one of them tells us nothing new.

## Supported versions

| Version | Supported |
|---|---|
| Latest release | Yes |
| Anything older | No |

There is one maintained line. Fixes go into the next release; there are no
backports. Because peers speak to each other directly, an old client is a risk
to the people it talks to as well as to its own user. Check that you are on the
latest release before reporting, and say which version you tested.

## How to report

Use GitHub's private vulnerability reporting: the *Security* tab on this
repository, then *Report a vulnerability*. That opens a private thread visible
to both maintainers, and it is the only channel we watch for this.

Both maintainers named in [MAINTAINERS.md](MAINTAINERS.md) receive these
reports. If one is unreachable, the other picks it up; you do not need to chase
a specific person.

Please do not open a public issue, and do not post a proof of concept in a
discussion or a pull request, until a fix has shipped.

A useful report says what you attacked, what you got, and how to reproduce it.
Reproducing against a local two-peer setup (`make peers`, see
[CONTRIBUTING.md](CONTRIBUTING.md#the-multi-peer-dev-loop)) is ideal, because
then we can run it without touching anyone's real conversations.

## Response time

Two people maintain Concord in their spare time. What that gets you:

- Acknowledgement within 7 days. If you have heard nothing by then, assume the
  notification was missed and ping the thread.
- An assessment within 30 days: either a fix in progress, or the reasoning for
  why we consider it out of scope or already known.
- The fix in the next ordinary release. There is no on-call rotation and no
  out-of-band patch channel, because there is nobody sitting by a pager.

If a disclosure deadline shorter than that matters to you, say so in your first
message and we will tell you straight away whether we can meet it. Agreeing a
date beats missing one. We credit reporters in the release notes unless you ask
us not to.

## Scope

Two very different things live in this repository, and a report about one is not
a report about the other.

### The client

Everything the user runs: identity and keystore, MLS group state, the store, the
libp2p layer, the frontend. This is where the trust sits. In scope:

- Recovering plaintext, keys, or the identity seed without the passphrase,
  beyond what the threat model already admits is on disk in the clear.
- Sending or altering a message attributed to someone else, or joining or
  staying in a group without being admitted.
- Privilege escalation through the governance log: carrying a role you were
  never granted, or ranking above the person who granted it.
- Remote crashes, memory corruption, or resource exhaustion reachable from an
  unauthenticated peer.
- Anything a guest (no account, invite link only) can reach beyond the single
  meeting they were invited to.
- XSS or code execution in the frontend, including through message content,
  attachments, or profile fields.

### The rendezvous node

`cmd/rendezvous` and `infra/` are untrusted infrastructure by design. The node
relays ciphertext, serves the DHT, and gateways guests. It holds no group keys
and cannot read a message, so "the rendezvous can see X" is a finding only when
X is something the design says it cannot see. In scope for the node:

- Reading, injecting, or modifying member traffic: anything that breaks the
  claim that it is a dumb pipe.
- Escaping the guest gateway into other guilds, meetings, or the host process.
- SSRF, or using the GIF proxy or TURN relay to reach networks it should not.

### Out of scope

- Metadata exposure inherent to peer-to-peer. Your IP is visible to the peers
  you connect to and to the rendezvous. Concord authenticates identities; it
  does not hide them.
- The gaps already described in the threat model, summarised under "Known
  limitations" below. An escalation beyond what is written there is in scope;
  restating the description is not.
- A malicious member of a group you invited them to. Encryption keeps outsiders
  out. It was never meant to stop an insider.
- Anyone holding your unlocked device, or your passphrase.
- Dependency vulnerabilities with no exploitable path through Concord. Report
  those upstream.
- Denial of service that requires being an admitted member of the group you are
  disrupting.
- Scanner output with no demonstrated impact.

## Known limitations

These are documented rather than fixed. We know about each one, and a report
that only describes it again will be closed as known. The
[threat model](docs/DESIGN.md#13-threat-model-what-concord-defends-against) carries the full detail. The short
list:

- Governance operations commit to a sequence number that their own signer
  chooses, and replay folds the log in that order. A member who has lost
  authority can therefore sign a new operation bearing an old sequence number,
  and replay would fold it as though they still held the power to issue it. The
  defence sits at ingest: an operation arriving live is refused if it claims a
  sequence number at or below one we already hold from that signer. Sync
  backfill is exempt, because a legitimate catch-up does replay a signer's older
  operations out of order. That leaves a bound worth stating. A peer that has
  not yet seen the newer operation has nothing to compare against, so it will
  accept the forgery until it syncs. Closing that requires operations to commit
  to the log head they were built on, which is a protocol change rather than a
  check. The comment in `internal/app/govern.go` carries the detail.
- MLS group state, including leaf private keys, is on disk in the clear. So are
  `peers.json`, sender keys, timestamps, and the governance log. Message bodies
  and the identity seed are encrypted at rest; the shape of your social life is
  not.
- History served during catch-up is attested by the member serving it, not
  re-verified per original sender. An ordinary member could forge a message
  attributed to someone else in a backfill.
- Relay privileges are granted but never revoked. Removing or banning someone
  drops them from the guild without withdrawing the tag that grants relay
  access, which they keep until the process restarts.
- GIF-pack and custom-emoji records arriving over sync are bounded by format
  validation and pack ceilings, not by authority.
- A removed member can still watch membership churn. Commits are signed but not
  encrypted, and a former member still knows the group identifier. Content stays
  sealed.

An exploit that chains one of these into something worse (reading content,
retaking a guild you were removed from, executing code) is a finding. Say which
limitation you started from.
