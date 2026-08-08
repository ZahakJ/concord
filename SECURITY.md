# Security policy

Concord is a peer-to-peer, end-to-end-encrypted chat app. Its security claims,
and the specific things it does **not** protect, are written out in the README
under [§13 Threat model](README.md#13-threat-model--what-concord-defends-against).
Please read that section before reporting: several properties people expect are
listed there as known, deliberate gaps, and a report that restates one of them
tells us nothing new.

## Supported versions

| Version | Supported |
|---|---|
| Latest release | Yes |
| Anything older | No |

Concord has one maintained line. Fixes go into the next release; there are no
backports. Because peers speak to each other directly, an old client is not just
a risk to its own user — check that you are on the latest release before
reporting, and say which version you tested.

## How to report

**Use GitHub's private vulnerability reporting**: the *Security* tab on this
repository → *Report a vulnerability*. That opens a private thread visible only
to maintainers, and it is the only channel we monitor for this.

> Maintainers: this must be switched on once, under
> *Settings → Advanced Security → Private vulnerability reporting*. Until it is,
> the button below is not there and reporters have nowhere private to go.

Please do **not** open a public issue, and do not post a proof of concept to a
discussion or a pull request, until a fix has shipped.

A good report says what you attacked, what you got, and how to reproduce it —
ideally against a local two-peer setup (`make peers`, see CONTRIBUTING.md), so
we can run it without touching anyone's real conversations.

## Response time

This is a small project maintained in spare time. Expect:

- **Acknowledgement within 7 days.** If you have heard nothing after that,
  assume it was missed and ping the thread.
- **An assessment within 30 days** — either a fix in progress, or an explanation
  of why we consider it out of scope or already-known.

Fixes ship in an ordinary release. We will credit you in the release notes
unless you ask us not to.

## Scope

There are two very different things in this repository, and a report about one
is not a report about the other.

**The client** (everything the user runs: identity and keystore, MLS group
state, the store, the libp2p layer, the frontend). This is where the real trust
sits. In scope:

- Anything that recovers plaintext, keys, or the identity seed without the
  passphrase, beyond what §13 already admits is on disk in the clear.
- Any way to send or alter a message attributed to someone else, or to join or
  stay in a group without being admitted.
- Privilege escalation through the governance log — carrying a role you were
  never granted, or ranking above someone who granted it.
- Remote crashes, memory corruption, or resource exhaustion reachable from an
  unauthenticated peer.
- Anything a *guest* (no account, link only) can reach beyond the single meeting
  they were invited to.
- XSS or code execution in the frontend, including via message content,
  attachments, or profile fields.

**The rendezvous node** (`cmd/rendezvous`, `infra/`) is untrusted infrastructure
by design. It relays ciphertext, serves the DHT, and gateways guests; it holds
no group keys and cannot read a message. So "the rendezvous can see X" is only a
finding if X is something §6.3 says it cannot see. In scope for the node:

- Reading, injecting, or modifying member traffic — anything that breaks the
  claim that it is a dumb pipe.
- Escaping the guest gateway into other guilds, meetings, or the host process.
- SSRF, or using the GIF proxy or TURN relay to reach networks it should not.

**Out of scope**

- The metadata exposure inherent to peer-to-peer: your IP is visible to peers
  you connect to and to the rendezvous. Concord authenticates identities, it
  does not hide them (§13).
- Everything else already listed under "Does not defend against" in §13 —
  unpruned `contacts` rows, plaintext `peers.json`, unrevoked relay tags,
  history-sync authenticity, a removed member watching membership churn. These
  are open work, tracked in the README, not new findings. A concrete *escalation*
  beyond what is described there is in scope.
- A malicious member of a group you invited them to. Encryption keeps outsiders
  out; it was never meant to stop an insider.
- Anyone with your unlocked device, or your passphrase.
- Vulnerabilities in third-party dependencies with no exploitable path through
  Concord — report those upstream.
- Denial of service that requires being an admitted member of the group you are
  disrupting.
- Findings from automated scanners with no demonstrated impact.
