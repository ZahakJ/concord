# Maintainers

Concord has two maintainers. Both hold commit access, both receive private
vulnerability reports, and either can cut a release.

| GitHub | Role |
|---|---|
| [@ZahakJ](https://github.com/ZahakJ) | Wrote the tree. Project lead: the tie-break on design, protocol and releases. |
| [@zyads](https://github.com/zyads) | Maintainer. Review, triage, and a second pair of eyes on protocol and crypto changes. |

Both maintainers can merge. [`.github/CODEOWNERS`](.github/CODEOWNERS) requests
review from both on every pull request.

## Areas

Both maintainers review anywhere in the tree, so no review request waits on the
"right" person. This table is here so an outside contributor can tell where a
change lands.

| Area | Paths |
|---|---|
| Core service and governance | `internal/app`, `internal/domain` |
| Transport bridge and HTTP API | `internal/bridge`, `internal/httpapi` |
| Crypto, identity, keystore, device linking | `internal/crypto`, `internal/identity`, `internal/link` |
| Networking, discovery, relays | `internal/net`, `internal/mailbox` |
| Storage | `internal/store` |
| Frontend | `frontend/` |
| Rendezvous node and deployment | `cmd/rendezvous`, `infra/`, `fly.rendezvous.toml` |
| Mobile shells and the gomobile core | `mobile/`, `apps/mobile/` |
| Release tooling | `scripts/publish-release.sh`, `.github/workflows/` |

## What a maintainer does

- Reviews and merges pull requests, including the boring ones.
- Answers issues, and closes the ones that are not going to happen, with a
  reason.
- Reads private vulnerability reports and drives them to a fix or a documented
  limitation. See [SECURITY.md](SECURITY.md).
- Cuts releases. They are built on a maintainer's own machine with
  `scripts/publish-release.sh`, not on a CI runner.
- Keeps the docs true. A design document that has drifted from the code is worse
  than no design document, because people trust it.

## How decisions get made

Two people cannot outvote each other, so the rules are about which changes need
both of us rather than about counting.

- Ordinary changes: one maintainer reviews, one approval merges. A maintainer
  may merge their own change without waiting when it is small and obviously
  correct (a typo, a dependency bump, a test). Behaviour changes wait for the
  other pair of eyes.
- Changes that cannot be un-shipped need both maintainers to approve: the wire
  protocol, the storage schema, governance rules, key handling, and the threat
  model. Peers running the previous release are already speaking the old rules,
  and there is no server that can migrate everyone at once.
- Disagreement gets talked out in the pull request, in writing, where the next
  person can read it later. If we still disagree, the project lead decides and
  says why in the thread. A tie-break is not a win, and the reasoning is the
  part that matters.
- Releases go out in batches, when there is something worth downloading, rather
  than one per merge. Either maintainer can cut one; neither cuts one the other
  has not seen.

## Reviewing outside contributions

We want them, and we would rather be blunt than slow:

- First response within a week, even if it is only "this needs a closer look, it
  will be a few days".
- If a change will not be merged, say so in the first reply. Three rounds of
  review followed by a no wastes the contributor's evening and ours.
- Ask for the smaller version. Splitting a large pull request is nearly always
  faster than reviewing it whole.
- A patch that is right but arrives without tests gets the tests asked for, not
  written for it. The person who found the bug knows how to reach it.

## Becoming a maintainer

We are not trying to grow the number. Two people who each understand the whole
system is the shape that suits a project of this size, and a maintainer who only
knows a corner cannot review the changes that matter most. The door is still
open.

What we look for:

- A run of merged, non-trivial pull requests. Not a count, just enough that we
  can predict what your patches look like before we open them.
- Review work on changes you did not write. Being useful in someone else's pull
  request is most of the job.
- Judgement in the areas that bite: protocol compatibility across versions, key
  handling, and what happens when two peers disagree.

How it happens: an existing maintainer proposes it, both existing maintainers
agree, and you agree. There is no application form and no vote. If you want to
head that way, say so, and we will point you at the work that gets you there
rather than let you guess.

A maintainer who goes quiet for six months and does not answer a direct ping
moves to the list below and hands back commit access. That is bookkeeping, not a
judgement, and it reverses on request.

## Emeritus

None yet.
