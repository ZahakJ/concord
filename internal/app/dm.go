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
func (s *Service) handleDMInvite(_ context.Context, _ peer.ID, request []byte) ([]byte, error) {
	var req dmInvite
	if err := json.Unmarshal(request, &req); err != nil {
		return []byte{}, nil
	}
	go func() {
		if _, err := s.JoinViaInvite(req.Code); err == nil {
			s.emitGuildUpdate()
		}
	}()
	return []byte("ok"), nil
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
