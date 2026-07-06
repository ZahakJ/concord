package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zahak/concord/internal/domain"
)

// History sync: when a peer (re)connects, we ask it for messages we missed
// while offline, per channel, newer than the latest message we already hold.
//
// The response batch is MLS-encrypted to the guild's group, so only current
// members can read it, and the responder's MLS signature authenticates who
// served it. Trust note: synced messages are *attested by the responding
// member* (their local copies), not re-verified against each original sender —
// the same trust Discord places in its server, but limited to guild members.
// Saving is idempotent by message ID, so overlapping syncs are harmless.

type syncRequest struct {
	GuildID   string `json:"guildId"`
	ChannelID string `json:"channelId"`
	SinceNano int64  `json:"since"`
}

type syncBatch struct {
	Messages []json.RawMessage `json:"messages"`
}

// handleSyncRequest serves a peer's catch-up request from local history.
func (s *Service) handleSyncRequest(ctx context.Context, _ peer.ID, request []byte) ([]byte, error) {
	var req syncRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, err
	}
	s.mu.RLock()
	g, ok := s.guilds[req.GuildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if !ok {
		return []byte{}, nil // not in that guild; nothing to serve
	}

	msgs, err := s.store.MessagesSince(req.ChannelID, req.SinceNano, 200)
	if err != nil || len(msgs) == 0 {
		return []byte{}, nil
	}
	var batch syncBatch
	for _, m := range msgs {
		if raw, err := json.Marshal(m); err == nil {
			batch.Messages = append(batch.Messages, raw)
		}
	}
	payload, err := json.Marshal(batch)
	if err != nil {
		return []byte{}, nil
	}
	// Encrypt to the group: only members can read the served history.
	return s.mls.Encrypt(ctx, groupID, payload)
}

// syncFromPeer pulls missed history for every guild channel from one peer.
// Best-effort: any failure just means we try again on the next connect.
func (s *Service) syncFromPeer(p peer.ID) {
	s.mu.RLock()
	type target struct {
		guildID, channelID string
		groupID            []byte
	}
	var targets []target
	for _, g := range s.guilds {
		for _, c := range g.Channels {
			targets = append(targets, target{g.ID, c.ID, g.GroupID})
		}
	}
	s.mu.RUnlock()

	for _, t := range targets {
		since, err := s.store.LatestTimestamp(t.channelID)
		if err != nil {
			continue
		}
		req, _ := json.Marshal(syncRequest{GuildID: t.guildID, ChannelID: t.channelID, SinceNano: since})
		ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
		resp, err := s.host.RequestSync(ctx, p, req)
		cancel()
		if err != nil || len(resp) == 0 {
			continue
		}
		s.applySyncBatch(t.channelID, t.groupID, resp)
	}
}

// applySyncBatch decrypts a served batch and stores any messages we're missing.
func (s *Service) applySyncBatch(channelID string, groupID, ciphertext []byte) {
	dec, err := s.mls.Decrypt(s.ctx, groupID, ciphertext)
	if err != nil {
		return
	}
	var batch syncBatch
	if json.Unmarshal(dec.Plaintext, &batch) != nil {
		return
	}
	for _, raw := range batch.Messages {
		var m domain.Message
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		// Only accept messages for the channel we asked about, and never
		// accept action kinds through sync (they were already applied).
		if m.ChannelID != channelID || m.Kind != "" {
			continue
		}
		if inserted, err := s.store.SaveMessage(m); err == nil && inserted {
			s.emitMessage(m)
		}
	}
}
