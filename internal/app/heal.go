package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
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
		} else if st.Can(ownerFpr, fpr, PermManageMembers) {
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
	for _, pid := range s.healCandidates(guildID) {
		if s.healViaCommitter(guildID, pid) {
			s.noteHealed(guildID)
			return
		}
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
func (s *Service) healCandidates(guildID string) []peer.ID {
	all := s.authorizedCommittersOnline(guildID)
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
	kp, err := s.mls.KeyPackage(s.ctx)
	if err != nil {
		return false
	}
	// Use this install's MLS leaf credential, not the bare account key: on a
	// linked device the two differ (the leaf is a device cert), and the
	// responder's credentialBoundToPeer check rejects the bare key, so every heal
	// attempt fails and the guild stays stranded. Mirrors JoinViaInvite.
	req, _ := json.Marshal(inviteRequest{
		GuildID: guildID, KeyPackage: kp, Credential: s.myCredential, Profile: s.selfWireProfile(),
	})
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()
	respBytes, err := s.host.RequestInvite(ctx, peer.AddrInfo{ID: pid}, req)
	if err != nil {
		return false
	}
	var resp inviteResponse
	if json.Unmarshal(respBytes, &resp) != nil {
		return false
	}
	if resp.Error != "" {
		// Two of the refusals are answers, not failures. A committer telling us
		// we were removed or banned is the guild's governance log speaking, and
		// asking the next candidate would only collect the same sentence from
		// somebody else. It is also, for a member kicked while offline, the
		// first they hear of it.
		s.noteEvictionRefusal(guildID, resp.Error)
		return false
	}
	if len(resp.Welcome) == 0 {
		return false
	}
	// JoinGroup overwrites our local group entry with the fresh membership at the
	// current epoch — the ratchet is repaired.
	if _, err := s.mls.Join(s.ctx, resp.Welcome); err != nil {
		return false
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
	for fpr, p := range resp.Profiles {
		s.learnProfile(fpr, p)
	}
	s.mu.RLock()
	var groupID []byte
	if g, ok := s.guilds[guildID]; ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if groupID != nil {
		// The join reset the group to the committer's current epoch; whatever
		// stranded us can never decrypt now, but its content comes back with the
		// history pull below. Drop the stash so it can't re-flag the guild.
		s.pendingCTMu.Lock()
		delete(s.pendingCT, string(groupID))
		s.pendingCTMu.Unlock()
	}
	go s.syncGuildFromPeer(guildID, pid) // pull any channel history we missed
	// Forced: the re-add re-keyed the group and replaced our leaf, so what the
	// other members last heard from us is not something we can reason about.
	s.announceProfileForce(guildID)
	s.emitGuildUpdate()
	return true
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
