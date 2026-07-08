package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zahak/concord/internal/domain"
	"github.com/zahak/concord/internal/identity"
)

// Direct messages are ordinary MLS groups tagged kind="dm", rendered without
// server chrome. The simplest is the self-DM ("Notes") — a dm group with a
// single member (you) — a private, end-to-end-encrypted scratchpad that syncs
// across your own devices once device-linking lands. Peer DMs (a 2-person dm
// group set up via the friend handshake) build on this same shape.

const notesGuildName = "Notes"

// NotesDM returns your personal self-DM, creating it on first use. It is a
// one-member MLS group, so nothing leaves your device until you have linked a
// second one.
func (s *Service) NotesDM() (domain.Guild, error) {
	s.mu.RLock()
	for _, g := range s.guilds {
		if g.Kind == "dm" && len(g.OwnerID) > 0 && string(g.OwnerID) == string(s.PublicKey()) && g.Name == notesGuildName {
			gc := *g
			s.mu.RUnlock()
			return gc, nil
		}
	}
	s.mu.RUnlock()

	gid, err := s.mls.CreateGroup(s.ctx)
	if err != nil {
		return domain.Guild{}, fmt.Errorf("app: create notes group: %w", err)
	}
	g := domain.NewGuild(notesGuildName, gid, s.PublicKey())
	g.Kind = "dm"
	g.Channels[0].Name = "notes"
	if err := s.store.SaveGuild(g); err != nil {
		return domain.Guild{}, err
	}
	s.trackGuild(&g)
	return g, nil
}

type dmInvite struct {
	Code string `json:"code"`
}

// NewDMInvite creates a fresh 2-person DM group owned by this peer and returns
// a shareable invite code for it — so you can start a direct conversation with
// someone you DON'T already share a guild with (they paste the code to join, no
// guild required). The first person to redeem it becomes the other party; more
// redeemers make it a group DM.
func (s *Service) NewDMInvite() (string, error) {
	gid, err := s.mls.CreateGroup(s.ctx)
	if err != nil {
		return "", fmt.Errorf("app: create dm group: %w", err)
	}
	g := domain.NewGuild("Direct message", gid, s.PublicKey())
	g.Kind = "dm"
	g.Channels[0].Name = "dm"
	if err := s.store.SaveGuild(g); err != nil {
		return "", err
	}
	s.trackGuild(&g)
	return s.InviteCode(g.ID)
}

// groupDMMax bounds a group DM so it stays a small conversation, not a guild.
const groupDMMax = 10

// CreateGroupDM opens a group direct message with the given fingerprints. Every
// invitee must be a VERIFIED contact — verification is the trust gate for group
// DMs (you pull people you've confirmed into a private group, Discord-style but
// stricter). The group is an ordinary kind="dm" MLS group owned by this peer;
// each reachable invitee is pushed the standard DM invite and auto-redeems it.
// Unreachable invitees simply don't join yet (they can be re-invited later).
func (s *Service) CreateGroupDM(fingerprints []string) (domain.Guild, error) {
	verified, err := s.store.VerifiedFingerprints()
	if err != nil {
		verified = map[string]bool{}
	}
	self := s.id.Fingerprint()
	seen := map[string]bool{}
	var targets []string
	for _, f := range fingerprints {
		if f == "" || f == self || seen[f] {
			continue
		}
		seen[f] = true
		if !verified[f] {
			return domain.Guild{}, fmt.Errorf("app: everyone in a group DM must be a verified contact — verify %s first", shortFpr(f))
		}
		targets = append(targets, f)
	}
	if len(targets) < 2 {
		return domain.Guild{}, fmt.Errorf("app: a group DM needs at least two other people")
	}
	if len(targets) > groupDMMax {
		return domain.Guild{}, fmt.Errorf("app: a group DM tops out at %d people", groupDMMax)
	}

	// If we already have a DM with exactly this set of people, reuse it instead
	// of spawning a duplicate conversation.
	if g := s.findDMByMembers(targets); g != nil {
		return *g, nil
	}

	gid, err := s.mls.CreateGroup(s.ctx)
	if err != nil {
		return domain.Guild{}, fmt.Errorf("app: create group dm: %w", err)
	}
	g := domain.NewGuild("Group message", gid, s.PublicKey())
	g.Kind = "dm"
	g.Channels[0].Name = "dm"
	if err := s.store.SaveGuild(g); err != nil {
		return domain.Guild{}, err
	}
	s.trackGuild(&g)

	code, err := s.InviteCode(g.ID)
	if err != nil {
		return domain.Guild{}, err
	}
	// Remember everyone we intend to add, then push to whoever's reachable now.
	// Unreachable invitees stay pending and are invited when they next connect
	// (see deliverPendingDMInvites), so the group eventually gathers everyone.
	s.dmInviteMu.Lock()
	s.pendingDMInvites[g.ID] = map[string]bool{}
	for _, f := range targets {
		s.pendingDMInvites[g.ID][f] = true
	}
	s.dmInviteMu.Unlock()

	for _, f := range targets {
		if pid, ok := s.peerForFingerprint(f); ok {
			s.pushDMInvite(pid, code)
		}
	}
	return g, nil
}

// retryPendingDMInvites re-pushes group-DM invites to any pending invitee who
// is reachable right now but hasn't joined yet — covering the case where the
// initial push failed while they stayed connected (so no reconnect fires the
// redelivery). Runs on the heal-loop tick.
func (s *Service) retryPendingDMInvites() {
	s.dmInviteMu.Lock()
	type todo struct{ gid, fpr string }
	var pending []todo
	for gid, set := range s.pendingDMInvites {
		for fpr := range set {
			pending = append(pending, todo{gid, fpr})
		}
	}
	s.dmInviteMu.Unlock()

	for _, t := range pending {
		if s.guildHasMember(t.gid, t.fpr) {
			s.clearPendingDMInvite(t.gid, t.fpr)
			continue
		}
		pid, ok := s.peerForFingerprint(t.fpr)
		if !ok {
			continue // still offline
		}
		if code, err := s.InviteCode(t.gid); err == nil {
			s.pushDMInvite(pid, code)
		}
	}
}

// clearPendingDMInvite drops a fingerprint from a group DM's pending set once
// they've joined (called from the add path). Safe for non-DM guilds (no-op).
func (s *Service) clearPendingDMInvite(guildID, fpr string) {
	s.dmInviteMu.Lock()
	defer s.dmInviteMu.Unlock()
	if set, ok := s.pendingDMInvites[guildID]; ok {
		delete(set, fpr)
		if len(set) == 0 {
			delete(s.pendingDMInvites, guildID)
		}
	}
}

// pushDMInvite fires the standard DM-invite push to a reachable peer; they dial
// back and are added via handleInviteRequest.
func (s *Service) pushDMInvite(pid peer.ID, code string) {
	req, _ := json.Marshal(dmInvite{Code: code})
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
		defer cancel()
		_, _ = s.host.RequestDMInvite(ctx, pid, req)
	}()
}

// deliverPendingDMInvites is called when a peer connects: if they're an
// outstanding group-DM invitee we couldn't reach earlier, invite them now.
// Entries for peers who have since joined are pruned.
func (s *Service) deliverPendingDMInvites(p peer.ID) {
	fpr := presenceFor(p).Fingerprint
	if fpr == "" {
		return
	}
	s.dmInviteMu.Lock()
	var invite []string // guild IDs to (re)push for this peer
	for gid, set := range s.pendingDMInvites {
		if !set[fpr] {
			continue
		}
		if s.guildHasMember(gid, fpr) {
			delete(set, fpr) // already in — nothing to do
		} else {
			invite = append(invite, gid)
		}
		if len(set) == 0 {
			delete(s.pendingDMInvites, gid)
		}
	}
	s.dmInviteMu.Unlock()

	for _, gid := range invite {
		code, err := s.InviteCode(gid)
		if err != nil {
			continue
		}
		s.pushDMInvite(p, code)
	}
}

// shortFpr abbreviates a fingerprint for user-facing messages.
func shortFpr(f string) string {
	if len(f) > 9 {
		return f[:9]
	}
	return f
}

// StartDM opens (creating if needed) a direct-message conversation with the
// peer identified by fingerprint. Clicking someone's profile → Message calls
// this. It creates a 2-person MLS group and pushes an invite to the recipient
// over the dm-invite stream; the recipient's client auto-redeems it and joins.
// Requires the recipient to be reachable (a connected member of a shared
// guild); offline delivery arrives with the mailbox.
func (s *Service) StartDM(fingerprint string) (domain.Guild, error) {
	if fingerprint == "" || fingerprint == s.id.Fingerprint() {
		return s.NotesDM()
	}
	if g := s.findPeerDM(fingerprint); g != nil {
		return *g, nil
	}

	pid, ok := s.peerForFingerprint(fingerprint)
	if !ok {
		return domain.Guild{}, fmt.Errorf("app: that person isn't reachable right now — they need to be online")
	}

	gid, err := s.mls.CreateGroup(s.ctx)
	if err != nil {
		return domain.Guild{}, fmt.Errorf("app: create dm group: %w", err)
	}
	g := domain.NewGuild("Direct message", gid, s.PublicKey())
	g.Kind = "dm"
	g.Channels[0].Name = "dm"
	if err := s.store.SaveGuild(g); err != nil {
		return domain.Guild{}, err
	}
	s.trackGuild(&g)

	// Reuse the guild invite code + handshake: push it to the recipient, who
	// redeems it (dials us back, we add them via handleInviteRequest).
	code, err := s.InviteCode(g.ID)
	if err != nil {
		return domain.Guild{}, err
	}
	req, _ := json.Marshal(dmInvite{Code: code})
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
		defer cancel()
		_, _ = s.host.RequestDMInvite(ctx, pid, req)
	}()
	return g, nil
}

// handleDMInvite is the recipient side: auto-redeem the pushed invite code so
// the DM appears without any manual step.
func (s *Service) handleDMInvite(_ context.Context, from peer.ID, request []byte) ([]byte, error) {
	var req dmInvite
	if err := json.Unmarshal(request, &req); err != nil {
		return []byte{}, nil
	}
	// The invite is authenticated to the peer that pushed it (libp2p PeerID).
	senderFpr := presenceFor(from).Fingerprint
	go func() {
		g, err := s.JoinViaInvite(req.Code)
		if err != nil {
			return
		}
		// Auto-accept guards against a peer pushing an arbitrary invite code to
		// force us into unsolicited membership (which would disclose our profile +
		// mailbox key). We accept two shapes:
		//   - a genuine 2-person DM whose other member is the sender (like getting
		//     a first text from someone — low harm), or
		//   - a GROUP DM whose inviter we already trust: someone we've verified, or
		//     someone we already share a 2-person DM with. That trust gate is what
		//     stops a stranger from silently pulling us into a group.
		// Anything else is undone immediately.
		if !s.isLegitDMWith(g.ID, senderFpr) && !s.isTrustedGroupDMInvite(g.ID, senderFpr) {
			_ = s.LeaveGuild(g.ID)
			return
		}
		s.emitGuildUpdate()
	}()
	return []byte("ok"), nil
}

// isLegitDMWith reports whether guildID is a 2-person DM whose other member is
// senderFpr — i.e. a real direct message opened by the peer that invited us.
func (s *Service) isLegitDMWith(guildID, senderFpr string) bool {
	if senderFpr == "" {
		return false
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	isDM := ok && g.Kind == "dm"
	s.mu.RUnlock()
	if !isDM {
		return false
	}
	creds, err := s.mls.Members(s.ctx, groupID)
	if err != nil || len(creds) != 2 {
		return false
	}
	self := s.id.Fingerprint()
	for _, c := range creds {
		if f := identity.FingerprintOf(c); f != self {
			return f == senderFpr
		}
	}
	return false
}

// isTrustedGroupDMInvite reports whether guildID is a group DM (a kind="dm"
// group with more than two members) that senderFpr is a member of AND whose
// inviter we already have a relationship with — we've verified them, or we
// already share some other group (a guild or an existing DM) with them. That is
// what "approved" means in practice, and it stops a stranger who merely pushed
// an invite code out of nowhere from pulling us into a group.
func (s *Service) isTrustedGroupDMInvite(guildID, senderFpr string) bool {
	if senderFpr == "" {
		return false
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	isDM := ok && g.Kind == "dm"
	s.mu.RUnlock()
	if !isDM {
		return false
	}
	creds, err := s.mls.Members(s.ctx, groupID)
	if err != nil || len(creds) <= 2 {
		return false // not a group DM
	}
	senderIsMember := false
	for _, c := range creds {
		if identity.FingerprintOf(c) == senderFpr {
			senderIsMember = true
			break
		}
	}
	if !senderIsMember {
		return false
	}
	if verified, err := s.store.VerifiedFingerprints(); err == nil && verified[senderFpr] {
		return true
	}
	return s.sharesOtherGroupWith(senderFpr, guildID)
}

// sharesOtherGroupWith reports whether we already share some group — a guild or
// an existing DM — with fingerprint, excluding exclID (the group currently being
// evaluated, which the sender is a member of too and would match trivially).
func (s *Service) sharesOtherGroupWith(fingerprint, exclID string) bool {
	s.mu.RLock()
	type ref struct {
		id  string
		gid []byte
	}
	var groups []ref
	for id, g := range s.guilds {
		if id != exclID {
			groups = append(groups, ref{id, g.GroupID})
		}
	}
	s.mu.RUnlock()
	for _, r := range groups {
		creds, err := s.mls.Members(s.ctx, r.gid)
		if err != nil {
			continue
		}
		for _, c := range creds {
			if identity.FingerprintOf(c) == fingerprint {
				return true
			}
		}
	}
	return false
}

// findDMByMembers returns an existing DM whose set of OTHER members (everyone
// but us) exactly equals want, or nil. Used to avoid creating a duplicate DM
// for a group of people we already have a conversation with.
func (s *Service) findDMByMembers(want []string) *domain.Guild {
	wantSet := make(map[string]bool, len(want))
	for _, f := range want {
		wantSet[f] = true
	}
	self := s.id.Fingerprint()
	s.mu.RLock()
	var candidates []*domain.Guild
	for _, g := range s.guilds {
		if g.Kind == "dm" {
			candidates = append(candidates, g)
		}
	}
	s.mu.RUnlock()
	for _, g := range candidates {
		creds, err := s.mls.Members(s.ctx, g.GroupID)
		if err != nil {
			continue
		}
		others := make(map[string]bool)
		for _, c := range creds {
			if f := identity.FingerprintOf(c); f != self {
				others[f] = true
			}
		}
		if len(others) != len(wantSet) {
			continue
		}
		match := true
		for f := range wantSet {
			if !others[f] {
				match = false
				break
			}
		}
		if match {
			gc := *g
			return &gc
		}
	}
	return nil
}

// findPeerDM returns an existing 2-person DM with the given fingerprint, or nil.
func (s *Service) findPeerDM(fingerprint string) *domain.Guild {
	s.mu.RLock()
	var candidates []*domain.Guild
	for _, g := range s.guilds {
		if g.Kind == "dm" {
			candidates = append(candidates, g)
		}
	}
	s.mu.RUnlock()
	for _, g := range candidates {
		creds, err := s.mls.Members(s.ctx, g.GroupID)
		if err != nil {
			continue
		}
		for _, c := range creds {
			if identity.FingerprintOf(c) == fingerprint {
				gc := *g
				return &gc
			}
		}
	}
	return nil
}

// peerForFingerprint resolves a connected peer's libp2p ID from its fingerprint.
func (s *Service) peerForFingerprint(fingerprint string) (peer.ID, bool) {
	for _, p := range s.host.Peers() {
		if presenceFor(p).Fingerprint == fingerprint {
			return p, true
		}
	}
	return "", false
}
