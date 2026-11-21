# An owner's device that was removed while it was away

This page is the design of the fix for the limit 0.61 shipped with: a device of a guild's owner that was offline when a member removed it could not rejoin on its own. The code is `internal/app/ownerchain.go` (responder) and the commit loop in `syncGuildFromPeerOnce` (requester); the tests are in `internal/app/ownerchain_test.go`.

## The gap

A member with Manage members can remove any leaf, the owner's devices included; governance never removes the owner's account, so the owner is entitled to come back. In 0.61 the owner's device comes back by reading the removal itself: `screenCommit` verifies the commit against its own tree at its own epoch, sees that another account removed its leaf, latches `taken_out:<guild>` (`noteTakenOut`), raises the out-of-sync flag, and the heal asks a committer to let it back in. The committer's door (`handleInviteRequest`) serves an unseated account only when it is the guild's effective owner.

All of that needs the commit. A device that was offline missed it on the control topic, and a catch-up cannot fetch it, because `handleSyncRequest` serves members only and every member that applied the removal no longer seats the owner. The device stayed at the old epoch for good, and no flag was raised.

## The change

A responder that does not seat the requester answers with the commit chain, and nothing else, when all of these hold:

1. The request says `ownerChain: true`. Only 0.62 and later send it, and only for a guild the sender's own state says it owns. A build that cannot read a chain it is not seated in is never handed one; it is declined, as before.
2. The requester's account is the guild's effective owner by the responder's governance state. The account comes from `presence`: the peer ID is authenticated by the Noise handshake, and a linked device maps to its account only through a device certificate that names that same key (`credentialBoundToPeer`, at hello or join). A device this responder's account has unlinked is never mapped.
3. The responder is not a copy that `CannotRepairHere`, and stands strictly ahead of the requester's epoch.
4. The throttle allows it (below).

The answer is `{commits, myEpoch}`, or `{epochGap, myEpoch}` when the log cannot bridge the requester. There is never a payload: the responder's tree does not seat the requester, so nothing encrypted could be read by it, and no history goes to a removed device.

The requester changes in one way only: it sends `ownerChain`. Everything it does with the answer is the existing catch-up loop.

## What is verified before acting

Nothing in the answer is taken on the responder's word. For each commit, in order, the requester:

- checks the author against its own governance state (`commitAuthorized`): only a member with Manage members, or the owner, may commit membership changes;
- verifies the commit against its own tree at its own epoch (`screenCommit` → `VerifyCommit`): the sender's leaf signature and the membership tag, which binds the commit to this group and this epoch;
- applies it if it does not remove this device; if it does, and the author is another account, latches taken-out and raises the flag (0.61 code); if the author is the owner's own account (an unlink), settles as removed.

The first commit that fails any check ends the loop, and nothing after it is read. So a forged, bit-flipped, truncated, reordered, replayed or foreign commit moves nothing; a chain cut short moves the owner along the honest prefix, which is a set of commits the group really made; and a removal replayed after the owner is back is for an epoch it has left and does not verify. `myEpoch` and `epochGap` are claims, used exactly as for a seated requester: at most they raise the flag, and the flag only makes the device ask the door, which decides on its own state.

## Bounds

- Size: at most 64 commits and 512 KiB of commit bytes per answer (always the first commit), inside the 1 MiB sync frame. The requester asks again in the same catch-up when a page moved its epoch.
- Rate: per guild and peer, the same page is served once per 30 seconds. A request from the epoch where the last page ended is the requester paging forward and is served at once, so the serves in a window are bounded by the chain's length over the page size. A gap answer is not repeated inside the window. The table holds 256 entries and refuses while it is full of fresh ones.
- Cost per serve: one indexed read of the commit log (bounded by `commitLogKeep`) and no encryption.

## Threat assumptions

- The serving peer is untrusted. It can decline, delay, or send any bytes; the worst it achieves is what a seated member's catch-up answer achieves today: nothing, or a flag that makes the owner ask the door.
- The owner's account is trusted for its own guild. What the chain reveals (the credentials in the Add commits since the requester's epoch) is less than the heal door already gives the same account: a Welcome carries the whole current tree. Members who joined and left during the absence are the one thing the chain shows that the Welcome would not.
- A member with Manage members can mint a removal of the owner's leaf and hand it to the owner alone. The owner then asks back in and is re-added: one extra commit, as in 0.61, where the same commit could arrive by gossip.
- A stolen owner device that the owner unlinked is refused by the owner's own devices (they hold the revocation and never map the key). A member that never learned the revocation would serve it the chain; the same member would already admit it at the heal door in 0.61, which is a wider opening than this and is not changed here.
- The owner's governance state may be stale after a long absence. If the remover was given Manage members by someone other than the owner while the owner was away, `commitAuthorized` refuses the removal on the owner's side, and the device stays where 0.61 left it. This is the one case left open; the governance ops that would settle it are in the encrypted payload, which an unseated device cannot read.
