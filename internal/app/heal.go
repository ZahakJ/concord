package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zahak/concord/internal/identity"
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

// authorizedCommitterOnline returns a connected peer authorized to commit
// membership changes for the guild (owner preferred, else a manage-members
// holder) — a valid target to request a re-add from.
func (s *Service) authorizedCommitterOnline(guildID string) (peer.ID, bool) {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var ownerFpr string
	var st GuildState
	if ok {
		ownerFpr = identity.FingerprintOf(g.OwnerID)
		st = s.govState[guildID]
	}
	s.mu.RUnlock()
	if !ok {
		return "", false
	}
	var fallback peer.ID
	var haveFallback bool
	for _, p := range s.host.Peers() {
		fpr := presenceFor(p).Fingerprint
		if fpr == ownerFpr {
			return p, true // the owner is always an authorized committer
		}
		if st.Can(ownerFpr, fpr, PermManageMembers) {
			fallback, haveFallback = p, true
		}
	}
	return fallback, haveFallback
}

// healOutOfSync attempts one re-add for a stranded guild. No-op if the guild
// isn't stranded or no authorized committer is reachable yet (a later attempt
// retries). Safe to call concurrently/repeatedly.
func (s *Service) healOutOfSync(guildID string) {
	if !s.OutOfSync(guildID) {
		return
	}
	pid, ok := s.authorizedCommitterOnline(guildID)
	if !ok {
		return // nobody who can re-add us is reachable yet
	}

	kp, err := s.mls.KeyPackage(s.ctx)
	if err != nil {
		return
	}
	req, _ := json.Marshal(inviteRequest{
		GuildID: guildID, KeyPackage: kp, Credential: s.PublicKey(), Profile: s.SelfProfile(),
	})
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()
	respBytes, err := s.host.RequestInvite(ctx, peer.AddrInfo{ID: pid}, req)
	if err != nil {
		return
	}
	var resp inviteResponse
	if json.Unmarshal(respBytes, &resp) != nil || resp.Error != "" || len(resp.Welcome) == 0 {
		return
	}
	// JoinGroup overwrites our local group entry with the fresh membership at the
	// current epoch — the ratchet is repaired.
	if _, err := s.mls.Join(s.ctx, resp.Welcome); err != nil {
		return
	}
	s.setOutOfSync(guildID, false)
	for fpr, p := range resp.Profiles {
		s.learnProfile(fpr, p)
	}
	go s.syncGuildFromPeer(guildID, pid) // pull any channel history we missed
	s.announceProfile(guildID)
	s.emitGuildUpdate()
}

// healStrandedGuilds attempts a re-add for every currently-stranded guild.
func (s *Service) healStrandedGuilds() {
	s.mu.RLock()
	ids := make([]string, 0, len(s.outOfSync))
	for id := range s.outOfSync {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	for _, id := range ids {
		s.healOutOfSync(id)
	}
}

// runHealLoop periodically retries recovery for stranded guilds, so a member
// that was stranded while every committer was offline heals automatically once
// one comes back — no user action.
func (s *Service) runHealLoop() {
	t := time.NewTicker(healRetryInterval)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			s.healStrandedGuilds()
		}
	}
}
