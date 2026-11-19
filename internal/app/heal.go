package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/crypto/mls"
	"github.com/ZahakJ/concord/internal/identity"
)

// heal.go recovers a member stranded at an old MLS epoch (the "out of sync"
// state) WITHOUT a manual leave/rejoin. When history-sync can't bridge the gap —
// because no reachable peer holds the missing commits — the stranded member asks
// an online authorized committer (owner or a manage-members holder) to re-add it:
// the committer Removes the stale leaf and re-Invites with a fresh key package,
// producing a Welcome that the stranded member Joins. mls JoinGroup overwrites
// the local group state at the current epoch, so this cleanly lifts us back into
// the ratchet; message history (stored per channel) is untouched.

const healRetryInterval = 20 * time.Second

// authorizedCommittersOnline returns every connected peer authorized to commit
// membership changes for the guild, owner devices first (the owner is always
// authorized; a manage-members holder needs the governance state to say so).
// All of them are valid targets to request a re-add from, and a heal should
// try each — the first pick being briefly unable to serve (mid-catch-up
// itself, a dying connection) used to cost a full retry beat.
func (s *Service) authorizedCommittersOnline(guildID string) []peer.ID {
	s.mu.RLock()
	_, ok := s.guilds[guildID]
	var ownerFpr string
	var st GuildState
	if ok {
		// The EFFECTIVE owner — after a transfer, heals must court the new
		// owner's devices first; the founder is just another member now.
		ownerFpr = s.effectiveOwnerLocked(guildID)
		st = s.govState[guildID]
	}
	s.mu.RUnlock()
	if !ok {
		return nil
	}
	var owners, mods []peer.ID
	for _, p := range s.host.Peers() {
		fpr := s.presence(p).Fingerprint
		if fpr == ownerFpr {
			owners = append(owners, p)
		} else if st.Can(ownerFpr, fpr, PermManageMembers) && !st.Removed[fpr] && !st.Banned[fpr] {
			mods = append(mods, p)
		}
	}
	return append(owners, mods...)
}

// recoverOutOfSync converges a guild whose ratchet ran ahead of us, cheapest
// remedy first:
//
//  1. History-sync from each connected member. A member that merely missed a
//     commit (a dropped gossip frame, a message that outran its commit) is
//     bridged by any peer's commit log in one round trip — no new commits, no
//     re-keying, nothing for OTHER members to keep up with. Every reachable
//     member is tried, not just the first that answers: a peer at our own
//     stale epoch answers happily and bridges nothing.
//  2. Only if no reachable member could bridge us (a real epoch gap, or a
//     forked group state): flag the guild — that is when the "Catching up…"
//     banner is honest — and ask an authorized committer for a re-add. The
//     re-add is the remedy of last resort because it costs two epoch-advancing
//     commits that every other member must gaplessly apply; handing it out for
//     transient races was itself a source of the drift it repaired.
//
// After each step the stashed undecryptable messages are retried, which is
// both the delivery of record for whatever stranded us and the test of
// whether recovery worked. One run per guild at a time; concurrent callers
// return immediately (the running pass covers them).
func (s *Service) recoverOutOfSync(guildID string) {
	s.pendingCTMu.Lock()
	if s.recovering == nil {
		s.recovering = map[string]bool{}
	}
	if s.recovering[guildID] {
		s.pendingCTMu.Unlock()
		return
	}
	s.recovering[guildID] = true
	s.pendingCTMu.Unlock()
	defer func() {
		s.pendingCTMu.Lock()
		delete(s.recovering, guildID)
		s.pendingCTMu.Unlock()
	}()

	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if !ok {
		return
	}
	// Nothing to heal in a guild that threw us out. The re-add IS the thing that
	// used to undo a kick, so this is not merely a wasted round trip.
	if s.EvictedFrom(guildID) != "" {
		return
	}
	// Evidence first: a fork verdict against a peer that has since gone offline
	// is not something this pass can act on, and left standing it would keep the
	// guild flagged for as long as the peer stayed away.
	s.pruneForkEvidence(guildID)

	healthy := func() bool {
		return s.pendingCiphertexts(groupID) == 0 && !s.OutOfSync(guildID)
	}
	if healthy() {
		return
	}
	synced := 0
	for _, p := range s.memberPeers(guildID) {
		if err := s.syncGuildFromPeer(guildID, p); err != nil {
			continue
		}
		synced++
		s.retryPendingCiphertexts(groupID)
		if healthy() {
			return
		}
	}
	// Nobody left to wait for. A guild whose only other members have been
	// removed has no holder for the updates we are missing and never will have
	// one, so "waiting for someone with the missing updates to come online" is
	// not a status, it is a promise the guild cannot keep — the owner of a guild
	// they had just emptied watched that bar for forty-five minutes. Whatever is
	// still in the stash was written on a branch this guild has left behind.
	//
	// Decided only AFTER the sync above, and never while the latch stands.
	// "Alone" is read from this device's own tree, and a tree restored from a
	// backup can predate the leaf of the very device that holds what it is
	// missing: a desktop restored to before its phone was linked holds a Notes
	// tree of one leaf. The sync is what meets that phone and latches; resolving
	// first threw the evidence away before it could be gathered, and resolving
	// after a latch would throw it away once gathered. The manual
	// ResolveStranding stays open in that state (CannotRepairHere is false for
	// a device alone in its tree), for when the phone is gone for good.
	if s.aloneInGuild(guildID) && !s.behindLatched(guildID) {
		s.resolveStranding(guildID)
		return
	}
	// Escalate to the re-add only on EVIDENCE that our group state is beyond
	// bridging — syncGuildFromPeer flags the guild when a peer reports an epoch
	// gap, or when a current member's payload is unreadable at an epoch we
	// supposedly share (a fork). A non-empty stash alone is not evidence: it
	// may be a message racing a commit that is still in flight to everyone
	// (retried when that commit lands), or junk from a sender who is himself
	// broken — re-adding US fixes neither, and each re-add is two commits the
	// whole guild must chew through.
	if s.OutOfSync(guildID) {
		s.healOutOfSync(guildID)
		return
	}
	// Unreadable traffic and nobody we could ask about it: assume we are the
	// stale side, say so (the banner), and let the heal loop keep trying as
	// peers come and go.
	if s.pendingCiphertexts(groupID) > 0 && synced == 0 {
		s.setOutOfSync(guildID, true)
	}
}

// aloneInGuild reports whether this device's leaf is the only one in a guild's
// MLS group. It reads the ROSTER, not who happens to be online: members who are
// merely away can still turn up with what we are missing, and the banner is
// right to wait for them.
//
// It counts LEAVES, not accounts. Every leaf of Notes belongs to the owner's
// account, so by accounts a Notes group was always "alone", and a restored
// desktop resolved its stranding instead of asking the phone that holds what
// it lost. The same holds in any guild: a second device of this account can be
// the one ahead. One exception: a leaf of this account's own device that this
// device holds a revocation for. Such a leaf can stay in a tree indefinitely
// where nobody could commit its removal, it will never serve anything, and
// counting it would keep the guild from ever resolving on its own.
func (s *Service) aloneInGuild(guildID string) bool {
	c := s.guildMemberSet(guildID)
	if c == nil || len(c.creds) == 0 {
		return false // an unknown roster is not evidence of an empty one
	}
	for _, cred := range c.creds {
		if bytes.Equal(cred, s.myCredential) || s.leafRevokedHere(cred) {
			continue
		}
		return false
	}
	return true
}

// leafRevokedHere reports whether a leaf credential is a device certificate of
// this account whose device this device has unlinked.
func (s *Service) leafRevokedHere(cred []byte) bool {
	cert, ok := identity.ParseDeviceCert(cred)
	if !ok || !cert.Verify() || !ed25519Equal(cert.AccountPub, s.id.PublicKey()) {
		return false
	}
	return s.deviceIsRevoked(cert.DevicePub)
}

// AloneInGuild is the exported read, for the view layer. A copy that is alone
// by its own tree but has seen a member ahead of it is not alone: its tree is
// what is stale, and the banner must not say that nobody else is here.
func (s *Service) AloneInGuild(guildID string) bool {
	return s.aloneInGuild(guildID) && !s.behindLatched(guildID)
}

// ResolveStranding ends a stranding that nothing can repair: it drops the
// undecryptable stash, withdraws the fork evidence that routed us here, and
// lowers the banner. The guild continues from the epoch this device is on.
//
// It is deliberately a decision and not a timeout. Everything else in this file
// exists to get the missing updates, and it usually works; this is the one exit
// for the case where they are provably unobtainable — the last other member left
// or was removed — plus the button that says so to a person staring at a bar
// that has been spinning since breakfast. What it does NOT do is weaken any of
// the evidence rules above: the fork verdict is still latched only against a
// peer strictly ahead of us, and is still the only thing that routes a re-add.
//
// It refuses with ErrBehindOnlyCommitter when CannotRepairHere holds: there the
// stranding is not unobtainable updates but a copy that fell off the guild, and
// continuing from it makes a private guild that looks healthy.
func (s *Service) ResolveStranding(guildID string) error {
	if s.CannotRepairHere(guildID) {
		return ErrBehindOnlyCommitter
	}
	s.resolveStranding(guildID)
	return nil
}

// ErrBehindOnlyCommitter refuses "Continue from here" on a device that is its
// guild's only possible committer while members stand ahead of it.
var ErrBehindOnlyCommitter = errors.New("app: this copy fell behind and cannot be repaired from here; start a new guild with the same people")

// CannotRepairHere reports that this device's copy of a guild is behind its
// members and that nobody can bring it back: a member was seen strictly ahead
// of it where it could not follow (the "seen behind" latch, which outlives that
// member going offline and a restart) and no other account or device may
// commit, so no re-add will ever come. Continuing from here would fork the
// guild for good: every member this device then admitted or kicked would be
// admitted to, or kicked from, a copy only it and its newcomers can see. The
// guild's view carries this so the banner can say it instead of offering to
// continue.
func (s *Service) CannotRepairHere(guildID string) bool {
	// The proven latch first: it is a map read, and it is down on almost every
	// guild almost always. Every send and governance op asks this
	// (mintBarred). An unproven claim (noteBehindClaim) never bars anything:
	// any member can make one up.
	if !s.behindProven(guildID) || s.aloneInGuild(guildID) {
		return false
	}
	return s.onlyPossibleCommitter(guildID)
}

// onlyPossibleCommitter reports whether this device is the only one that may
// commit membership changes in the guild: its account is the effective owner,
// no other device of that account is in the group (authorizedCommitter accepts
// any of them), and no other account still in the group holds manage-members or
// stands as heir ready to claim ownership. Anyone in those seats could re-admit
// us. A seat held by someone who has left the group counts for nothing.
func (s *Service) onlyPossibleCommitter(guildID string) bool {
	mine := s.id.Fingerprint()
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if !ok || len(groupID) == 0 {
		return false
	}
	creds, err := s.mls.Members(s.ctx, groupID)
	if err != nil || len(creds) == 0 {
		return false // an unreadable roster proves nothing
	}
	// Another device of this account could repair this copy. "Another
	// device" is ownAccountLeaf's verdict, not a matching fingerprint: a leaf
	// that only carries this account's credential is not one.
	others, err := s.otherOwnDevices(groupID)
	if err != nil {
		return false
	}
	if others > 0 {
		return false
	}
	inGroup := map[string]bool{}
	for _, c := range creds {
		inGroup[accountFingerprintOf(c)] = true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	owner := s.effectiveOwnerLocked(guildID)
	if owner != mine {
		return false
	}
	st := s.govState[guildID]
	seated := func(f string) bool {
		return f != "" && f != mine && inGroup[f] && !st.Removed[f] && !st.Banned[f]
	}
	if seated(st.Heir()) {
		return false
	}
	for f := range inGroup {
		if seated(f) && st.Can(owner, f, PermManageMembers) {
			return false
		}
	}
	return true
}

// behindSeenPrefix keys the persisted "seen behind" latch: the epoch a member
// stood at when this device, behind it, could not follow.
const behindSeenPrefix = "behind_seen:"

// noteBehind latches, on PROOF, that the guild stands at epoch, strictly ahead
// of this device, and this device could not be bridged there. The proof is a
// commit for this device's own current epoch, made by this device's own leaf,
// that verifies against this device's tree (signature and membership tag, mls
// VerifyCommit) and that it still could not apply (ownLostCommit): the guild
// moved on from where this copy stands, by a commit this copy cannot follow.
// Another committer's commit is not proof: that one it could apply. A peer's
// word for its epoch is not proof either; see noteBehindClaim.
//
// The proven latch is kept until the device is level again with a readable
// answer, a heal Join replaces its group state, ResolveStranding is accepted,
// or the device is evicted. Losing the ahead member, or restarting, does not
// clear it; that is what lets the guard in ResolveStranding hold once the
// members who are ahead close their apps. It is the only latch CannotRepairHere
// reads.
func (s *Service) noteBehind(guildID string, epoch uint64) {
	s.mu.Lock()
	if epoch <= s.behindSeen[guildID] {
		s.mu.Unlock()
		return
	}
	s.behindSeen[guildID] = epoch
	s.mu.Unlock()
	s.persistBehind(guildID)
	s.emitGuildUpdate()
}

// noteBehindClaim records a member's unproven word that it stands at epoch,
// ahead of this device: an EpochGap, or a payload this device could not read
// from a peer reporting a higher epoch, with no verifiable commit behind it.
// Both are a number any seated member can send. The claim keeps the recovery
// from resolving the stranding by itself, keeps the banner from calling the
// copy alone, and lets the heal ask the peer that made it; it bars nothing.
// It lives in memory only and is dropped at the next readable answer from any
// member (dropBehindClaim), so an unproven claim lasts until the next readable answer.
//
// An honest claim is how a Notes copy restored to before its phone was linked
// learns it is behind: the phone's log starts at its own join, so it can only
// answer with an EpochGap.
func (s *Service) noteBehindClaim(guildID string, epoch uint64) {
	s.mu.Lock()
	if epoch <= s.behindClaimed[guildID] {
		s.mu.Unlock()
		return
	}
	s.behindClaimed[guildID] = epoch
	s.mu.Unlock()
	s.emitGuildUpdate()
}

// dropBehindClaim forgets an unproven claim, after a readable answer.
func (s *Service) dropBehindClaim(guildID string) {
	s.mu.Lock()
	_, had := s.behindClaimed[guildID]
	delete(s.behindClaimed, guildID)
	s.mu.Unlock()
	if had {
		s.emitGuildUpdate()
	}
}

// noteAhead records that this device itself saw p standing strictly ahead of
// it in a guild, where it could not follow. It is set beside noteBehind, and
// unlike a fork verdict it is kept through the re-add cooldown: it routes
// nothing, it only says who may be asked (healCandidates).
func (s *Service) noteAhead(guildID string, p peer.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seenAhead[guildID] == nil {
		s.seenAhead[guildID] = map[peer.ID]bool{}
	}
	s.seenAhead[guildID][p] = true
}

// sawAhead reports whether noteAhead holds p for a guild.
func (s *Service) sawAhead(guildID string, p peer.ID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.seenAhead[guildID][p]
}

// behindLatched reports whether this device holds any evidence that a guild is
// ahead of it, proven or claimed.
func (s *Service) behindLatched(guildID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.behindSeen[guildID] > 0 || s.behindClaimed[guildID] > 0
}

// behindProven reports whether the proven latch stands (noteBehind).
func (s *Service) behindProven(guildID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.behindSeen[guildID] > 0
}

// clearBehindIfLevel drops the latch after a readable answer once this device
// itself stands at or past the latched epoch, whatever epoch the peer reports
// (an older build reports none). mintBarred is what makes that safe: a device
// the latch could trap mints no commits, so it can only have got there by
// applying the guild's own.
func (s *Service) clearBehindIfLevel(guildID string, groupID []byte) {
	s.mu.RLock()
	seen := s.behindSeen[guildID]
	s.mu.RUnlock()
	if seen == 0 {
		return
	}
	if cur, err := s.mls.Epoch(s.ctx, groupID); err != nil || cur < seen {
		return
	}
	s.clearBehind(guildID)
}

// clearBehind drops the latch and its persisted copy, any claim, and what this
// device saw of which peers stood ahead of it.
func (s *Service) clearBehind(guildID string) {
	s.mu.Lock()
	delete(s.seenAhead, guildID)
	delete(s.behindClaimed, guildID)
	if s.behindSeen[guildID] == 0 {
		s.mu.Unlock()
		return
	}
	delete(s.behindSeen, guildID)
	s.mu.Unlock()
	s.persistBehind(guildID)
	s.emitGuildUpdate()
}

// loadBehindSeen restores the latch at startup. Where the latch traps the
// device (CannotRepairHere) it raises the out-of-sync flag too: the group state
// on disk reads as a healthy guild, and the banner would otherwise stay down
// until a member who is ahead happened to answer. Elsewhere a re-add can still
// come, and the ordinary sync finds out soon enough whether one is needed.
func (s *Service) loadBehindSeen() {
	s.mu.RLock()
	ids := make([]string, 0, len(s.guilds))
	for id := range s.guilds {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	for _, id := range ids {
		v, err := s.store.GetSetting(behindSeenPrefix + id)
		if err != nil || v == "" {
			continue
		}
		epoch, err := strconv.ParseUint(v, 10, 64)
		if err != nil || epoch == 0 {
			continue
		}
		s.mu.Lock()
		s.behindSeen[id] = epoch
		s.mu.Unlock()
		if s.CannotRepairHere(id) {
			s.mu.Lock()
			if s.evicted[id] == "" {
				s.outOfSync[id] = true
			}
			s.mu.Unlock()
		}
	}
}

// mintBarred refuses any membership commit, any governance op (issueGovOp) and
// any message (sendAsWith) from a copy that CannotRepairHere. A message from
// such a copy is encrypted to the tree it was restored to, which can still
// seat a member removed since. The latch, not the out-of-sync flag,
// is the guard, because the flag has more than one way down. Without new
// commits of its own, this device's epoch can reach the latched one only by
// applying the real guild's commits, which is what lets a readable answer at
// that epoch clear the latch safely.
func (s *Service) mintBarred(guildID string) error {
	if s.CannotRepairHere(guildID) {
		return ErrBehindOnlyCommitter
	}
	return nil
}

// resolveStranding is ResolveStranding without the guard, for the case the
// recovery loop proves itself: nobody else is left in the guild.
func (s *Service) resolveStranding(guildID string) {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if !ok {
		return
	}
	if len(groupID) > 0 {
		s.pendingCTMu.Lock()
		delete(s.pendingCT, string(groupID))
		s.pendingCTMu.Unlock()
	}
	s.clearForkEvidence(guildID)
	s.setOutOfSync(guildID, false)
}

// memberPeers lists connected peers that are members of the guild, SyncHost
// holders first (same preference as syncGuildFromAnyPeer).
func (s *Service) memberPeers(guildID string) []peer.ID {
	var hosts, others []peer.ID
	for _, p := range s.host.Peers() {
		fpr := s.presence(p).Fingerprint
		if !s.guildHasMember(guildID, fpr) {
			continue
		}
		if s.memberHasPerm(guildID, fpr, PermSyncHost) {
			hosts = append(hosts, p)
		} else {
			others = append(others, p)
		}
	}
	return append(hosts, others...)
}

// healOutOfSync attempts one re-add for a stranded guild, trying every online
// authorized committer until one Welcome lands. No-op if the guild isn't
// stranded or no committer is reachable yet (a later attempt retries). Safe to
// call concurrently/repeatedly.
func (s *Service) healOutOfSync(guildID string) {
	if !s.OutOfSync(guildID) {
		return
	}
	candidates := s.healCandidates(guildID)
	unseated := 0
	for _, pid := range candidates {
		ok, refusal := s.healAsk(guildID, pid)
		if ok {
			s.noteHealed(guildID)
			return
		}
		if refusal == unseatedRefusal {
			unseated++
		}
	}
	// Every committer we could reach says its guild never seated us. One of them
	// might merely be behind, but all of them cannot be: the welcome we hold came
	// from someone who could not admit us (typically a member already thrown
	// out), and the "guild" we have been reading is their private copy. Say so
	// and stop asking, rather than sit on a catching-up bar that cannot end.
	// Not for the guild's owner: no honest committer refuses the owner a seat,
	// and the owner waits for one that does not.
	if unseated > 0 && unseated == len(candidates) && !s.ownsGuild(guildID) {
		s.noteEvicted(guildID, evictedUnseated)
	}
}

// forkEvidenceCooldown is how long a completed re-add suppresses the NEXT fork
// verdict for the same guild.
//
// A re-add is the most expensive thing this file can decide to do — two commits
// every other member has to apply gaplessly — and the evidence that routes us
// to one is, in the end, a member's own word about its epoch plus a payload we
// could not read. An ordinary member with no permissions at all can produce
// both. Without a floor, a member that keeps claiming to be far ahead and
// keeps sending unreadable traffic can make this device re-add itself on every
// beat, and the guild pays for it; the old any-peer-clears verdict happened to
// stop that, and a verdict that latches must not lose the property. Suspicion
// during the window still flags the guild and still runs the sync-everyone
// recovery, which is what fixes a real gap anyway. A genuine second fork inside
// the window is delayed, not dropped — and it takes two authorized committers
// to make one, so two inside two minutes is churn that has its own problem.
const forkEvidenceCooldown = 2 * time.Minute

// noteHealed records that an automatic re-add just completed for a guild. Only
// the automatic lane arms it — healViaCommitter is also called directly, and a
// single deliberate re-add is not the loop worth damping.
func (s *Service) noteHealed(guildID string) {
	s.mu.Lock()
	if s.lastHealed == nil {
		s.lastHealed = map[string]time.Time{}
	}
	s.lastHealed[guildID] = time.Now()
	s.mu.Unlock()
}

// healedRecently reports whether a re-add landed inside forkEvidenceCooldown.
func (s *Service) healedRecently(guildID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.lastHealed[guildID]
	return ok && time.Since(t) < forkEvidenceCooldown
}

// healCandidates orders the online authorized committers so that anyone we hold
// FORK evidence against comes first.
//
// Ordering is not a preference here, it is the difference between a repair and
// a treadmill. Only committers mint commits, so a fork always has one on each
// side; a re-add served by a committer on our OWN branch is a Remove and an Add
// that put us back exactly where we started, still unable to read the other
// half — and repeated on the heal beat that is the re-add storm this module
// exists to avoid, not a cure. The peer whose payload we could not read is, by
// construction, standing on the branch we need to be moved to.
//
// A device of this device's OWN account is a candidate only if this device saw
// it strictly ahead in this guild. What this device knows of its account's
// devices can be older than the account: restored from a backup, it no longer
// holds the revocation of a device unlinked since, so presence places that
// device under the account again, and its stale tree may still seat it. Asked
// for a heal, such a device would draw this one onto its own copy. A sibling
// that really holds what this device is missing stands ahead of it, and the
// sync that precedes every heal is what sees that.
func (s *Service) healCandidates(guildID string) []peer.ID {
	mine := s.id.Fingerprint()
	var all []peer.ID
	for _, p := range s.authorizedCommittersOnline(guildID) {
		if s.presence(p).Fingerprint == mine && !s.sawAhead(guildID, p) {
			continue
		}
		all = append(all, p)
	}
	forked := s.forkedWith(guildID)
	if len(forked) == 0 || len(all) < 2 {
		return all
	}
	set := make(map[peer.ID]bool, len(forked))
	for _, p := range forked {
		set[p] = true
	}
	var first, rest []peer.ID
	for _, p := range all {
		if set[p] {
			first = append(first, p)
		} else {
			rest = append(rest, p)
		}
	}
	return append(first, rest...)
}

// healViaCommitter asks one authorized committer to re-add us, reporting
// whether the ratchet was repaired.
func (s *Service) healViaCommitter(guildID string, pid peer.ID) bool {
	ok, _ := s.healAsk(guildID, pid)
	return ok
}

// healAsk is healViaCommitter that also hands back the committer's refusal,
// so a pass over every candidate can tell "nobody could serve us right now"
// from "nobody here ever seated us".
func (s *Service) healAsk(guildID string, pid peer.ID) (bool, string) {
	kp, err := s.mls.KeyPackage(s.ctx)
	if err != nil {
		return false, ""
	}
	// Use this install's MLS leaf credential, not the bare account key: on a
	// linked device the two differ (the leaf is a device cert), and the
	// responder's credentialBoundToPeer check rejects the bare key, so every heal
	// attempt fails and the guild stays stranded. Mirrors JoinViaInvite.
	req, _ := json.Marshal(inviteRequest{
		GuildID: guildID, KeyPackage: kp, Credential: s.myCredential, Profile: s.selfWireProfile(),
		Heal: true,
	})
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()
	ask := s.host.RequestInvite
	if f := s.requestInvite.Load(); f != nil {
		ask = *f
	}
	respBytes, err := ask(ctx, peer.AddrInfo{ID: pid}, req)
	if err != nil {
		return false, ""
	}
	var resp inviteResponse
	if json.Unmarshal(respBytes, &resp) != nil {
		return false, ""
	}
	if resp.Error != "" {
		// Two of the refusals are answers, not failures. A committer telling us
		// we were removed or banned is the guild's governance log speaking, and
		// asking the next candidate would only collect the same sentence from
		// somebody else. It is also, for a member kicked while offline, the
		// first they hear of it.
		s.noteEvictionRefusal(guildID, resp.Error)
		return false, strings.TrimSpace(resp.Error)
	}
	if len(resp.Welcome) == 0 {
		return false, ""
	}
	s.mu.RLock()
	var want []byte
	if g, ok := s.guilds[guildID]; ok {
		want = g.GroupID
	}
	s.mu.RUnlock()
	if len(want) == 0 {
		return false, ""
	}
	// Only a Welcome for THIS guild's group. A Welcome carries its own group
	// id, and one for another group has repaired nothing here: the guild is
	// still stranded, and nothing below may clear its flag or its latch. The
	// check is made inside the join, before anything is stored, so a Welcome
	// for another group creates or replaces no group state on this device.
	// For this group, the join overwrites our local entry with the fresh
	// membership at the current epoch — the ratchet is repaired.
	if _, err := s.mls.JoinAs(s.ctx, resp.Welcome, want); err != nil {
		if errors.Is(err, mls.ErrWrongGroup) {
			log.Printf("concord/app: guild %s: committer %s answered a heal with a welcome for another group; refused", guildID, pid)
		}
		return false, ""
	}
	// A welcome we could join disproves the terminal state: somebody with
	// manage-members just seated us.
	s.clearEvicted(guildID)
	// The welcome replaced our whole group state, roster included.
	s.forgetMemberSet(guildID)
	// The Join replaced our group state outright, so nothing we had concluded
	// about our old tree survives it: drop the fork evidence before clearing the
	// flag, or the verdict that routed us here would keep the flag up forever.
	s.clearForkEvidence(guildID)
	s.setOutOfSync(guildID, false)
	s.markCaughtUp(guildID)
	for fpr, p := range resp.Profiles {
		s.learnProfile(fpr, p)
	}
	// The join reset the group to the committer's current epoch; whatever
	// stranded us can never decrypt now, but its content comes back with the
	// history pull below. Drop the stash so it can't re-flag the guild.
	s.pendingCTMu.Lock()
	delete(s.pendingCT, string(want))
	s.pendingCTMu.Unlock()
	// The Welcome carried a whole tree we have not read: audit it, flag
	// only (seatcheck.go).
	s.auditSeats(guildID, want)
	go s.syncGuildFromPeer(guildID, pid) // pull any channel history we missed
	// Forced: the re-add re-keyed the group and replaced our leaf, so what the
	// other members last heard from us is not something we can reason about.
	s.announceProfileForce(guildID)
	s.emitGuildUpdate()
	return true, ""
}

// healStrandedGuilds runs recovery for every currently-stranded guild —
// sync-first, re-add only if syncing cannot bridge (see recoverOutOfSync).
func (s *Service) healStrandedGuilds() {
	s.mu.RLock()
	ids := make([]string, 0, len(s.outOfSync))
	for id := range s.outOfSync {
		ids = append(ids, id)
	}
	quiet := make(map[string][]byte, len(s.guilds))
	for id, g := range s.guilds {
		if !s.outOfSync[id] {
			quiet[id] = g.GroupID
		}
	}
	s.mu.RUnlock()
	// A guild sitting on messages it could not read is not healthy either,
	// whatever the flag says — and nothing else was going back for it. Recovery
	// is kicked when an unreadable message ARRIVES, so a member on the wrong
	// side of a split that then goes quiet keeps the words it already failed to
	// read and never asks about them again. Ask once a beat until the stash
	// drains (it expires on its own after pendingCipherTTL, so this cannot run
	// forever on junk).
	for id, groupID := range quiet {
		if s.pendingCiphertexts(groupID) > 0 {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		s.recoverOutOfSync(id)
	}
}

// runHealLoop periodically retries recovery for stranded guilds, so a member
// that was stranded while every committer was offline heals automatically once
// one comes back — no user action.
// reconcileEvery runs the guild anti-entropy on every Nth heal tick (~60s)
// rather than every tick. Reconcile is the backstop for a DROPPED gossip
// message — live traffic still arrives instantly over the mesh — and measured
// on the wire each reconcile costs the ASKED peer a 14–27 packet burst through
// its radio. At 20s that burst was two-thirds of what a backgrounded phone
// received from an idle desktop; peers cannot throttle each other, so the
// polite cadence has to be the default one.
const reconcileEvery = 3

// The interval is paced (bgPace): backgrounded on a phone this beats every
// backgroundBeat instead — anti-entropy this frequent through a phone radio is
// most of what "Concord eats the battery" was — and the bgWake case runs a tick
// immediately on return to foreground.
func (s *Service) runHealLoop() {
	tick := 0
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.bgWakeCh():
			// Foregrounded: catch up now rather than in up to a full beat.
		case <-time.After(s.bgPace(healRetryInterval)):
		}
		tick++
		s.healStrandedGuilds()
		s.retryPendingDMInvites()
		s.retryOfferedInvites()
		s.noteDeviceLeaves()
		// Backgrounded the tick itself is one slow beat, so reconcile rides
		// every tick; foreground it runs on the polite cadence. The pending
		// invite re-push rides the same cadence: it is anti-entropy too (an
		// invite the other side never received), not something that has to
		// happen three times a minute.
		if s.backgrounded() || tick%reconcileEvery == 0 {
			s.reconcilePendingMembers()
			s.reconcileGuilds()
			s.pruneCommitLogs()
		}
		s.sweepMailbox()
	}
}

// reconcileGuilds is periodic anti-entropy: every heal tick, pull each guild's
// state from a connected member and fold it in. Live gossip (channels, roles,
// messages, profiles) is best-effort — a peer can miss an update and, before
// this, would stay diverged until it reconnected. Now every peer re-syncs on a
// timer, so whoever knows the most propagates it and views converge within one
// tick even if the live message was dropped. The sync is incremental (per-
// channel `since` cursor + epoch), so a steady-state tick transfers almost
// nothing. Each guild runs in its own goroutine so one slow/timing-out peer
// doesn't stall the others.
//
// Note: the merge in applySyncPayload is additive (it adopts channels/ops a peer
// has), so ADDITIONS converge; deletions still need their own propagation.
func (s *Service) reconcileGuilds() {
	evicted := s.evictedGuildIDs()
	s.mu.RLock()
	ids := make([]string, 0, len(s.guilds))
	for id := range s.guilds {
		if evicted[id] {
			continue // no longer ours to keep in step with
		}
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	for _, id := range ids {
		go s.syncGuildFromAnyPeer(id)
	}
}
