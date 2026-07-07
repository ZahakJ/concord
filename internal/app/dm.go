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
		// Auto-accept is ONLY for a genuine 2-person DM opened by the peer who
		// pushed the invite. Without this check, any peer could push an arbitrary
		// invite code and silently force us into a full guild or a group we're not
		// the intended counterparty of (unsolicited membership + profile/mailbox-
		// key disclosure). Anything that isn't a 2-person DM with the sender is
		// undone immediately.
		if !s.isLegitDMWith(g.ID, senderFpr) {
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
