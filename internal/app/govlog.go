package app

import (
	"fmt"
	"sort"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// govlog.go surfaces something that has always been there and has never been
// visible: the guild's governance log.
//
// Every ban, mute, role change, slow-mode setting and ownership handover is
// already an Ed25519-signed operation, already folded in the same deterministic
// order on every member's machine, already re-served in history sync. Nothing
// here records, transmits or stores anything — this file is a renderer over
// state each peer independently holds and independently verifies.
//
// That distinction is the whole point of the screen. Elsewhere an audit trail is
// a table its operator writes and could rewrite; here it is a signature chain,
// and the reader checks it on their own hardware. So the view type carries the
// verdict, not just the sentence: whether the signature verifies against the
// signer's account key, and whether the op was authorized at its own point in
// the order. An op can be perfectly signed and still have changed nothing — a
// moderator who lost the role between signing and folding, an op replayed out of
// rank — and a log that printed that as a ban would be exactly the kind of
// comfortable fiction this feature exists to refuse.
//
// Read access is not permission-gated, deliberately. There is no server holding
// the log back: every member already has these bytes on disk and could read
// them with a text editor. Gating the SCREEN would hide the record from the
// people it is about while hiding nothing from anyone determined, which is
// theatre. The panel says so in one line.

// GovLogEntry is one operation, resolved for display. Names are resolved at read
// time from the current roster, so a member who has since been renamed reads
// under the name they answer to now — the fingerprint is carried alongside for
// anyone who needs the durable identity.
type GovLogEntry struct {
	Hash string `json:"hash"`
	Seq  uint64 `json:"seq"`
	Type string `json:"type"`

	Signer     string `json:"signer"`
	SignerName string `json:"signerName,omitempty"`
	Target     string `json:"target,omitempty"`
	TargetName string `json:"targetName,omitempty"`

	RoleID   string `json:"roleId,omitempty"`
	RoleName string `json:"roleName,omitempty"`
	Color    string `json:"color,omitempty"`
	Perms    uint32 `json:"perms,omitempty"`
	Position int    `json:"position,omitempty"`
	Add      bool   `json:"add,omitempty"`
	Until    int64  `json:"until,omitempty"`
	// Created marks the role_upsert that first brought a role into existence, so
	// the sentence can say "created" once and "changed" thereafter. Only the log
	// knows this — the folded state cannot tell a role's birth from its last
	// edit, and every upsert reads identically without it.
	Created bool `json:"created,omitempty"`

	ChannelID   string `json:"channelId,omitempty"`
	ChannelName string `json:"channelName,omitempty"`
	Seconds     int64  `json:"seconds,omitempty"`

	// At is the author's own wall clock, in unix milliseconds. It is the honest
	// field for "when did this happen": the store's `created` column records
	// when THIS device first saw the op, which for anything that arrived in a
	// history sync is a different day entirely.
	At int64 `json:"at"`

	Verified bool `json:"verified"`
	Applied  bool `json:"applied"`
}

// GovernanceLog returns a page of a guild's governance log, newest first.
//
// Paging is by position in the canonical order rather than by timestamp,
// because the canonical order is the thing every peer agrees on and the
// timestamps are author clocks that can disagree. `offset` is how many entries
// from the newest end to skip.
func (s *Service) GovernanceLog(guildID string, offset, limit int) ([]GovLogEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	s.mu.RLock()
	g, ok := s.guilds[guildID]
	if !ok {
		s.mu.RUnlock()
		return nil, fmt.Errorf("app: unknown guild %s", guildID)
	}
	ops := append([]govOp(nil), s.govOps[guildID]...)
	owner := append([]byte(nil), g.OwnerID...)
	s.mu.RUnlock()

	// The verdicts come from a real replay of the whole log, not from a
	// per-op guess: whether a ban was authorized depends on what the ops before
	// it did to the banner's roles, so there is no shortcut that is also true.
	_, verdicts := replayGuildOpsRecording(owner, ops, true)

	// Names for roles that no longer exist have to come from the log itself —
	// a role_delete leaves nothing in the folded state to look up, and "removed
	// the role" with a blank where the name goes is the least useful sentence
	// the screen could print. Walking the log forwards and remembering the last
	// name each role wore gives every row the name it had at the time.
	ordered := canonicalOps(ops)
	roleNames := make(map[string]string, 8)
	firstUpsert := make(map[string]string, 8) // roleID -> hash of the op that created it
	for _, o := range ordered {
		if o.Type != "role_upsert" || o.RoleID == "" {
			continue
		}
		if o.Name != "" {
			roleNames[o.RoleID] = o.Name
		}
		if _, seen := firstUpsert[o.RoleID]; !seen {
			firstUpsert[o.RoleID] = o.hash()
		}
	}

	channelNames := map[string]string{}
	for _, c := range s.channelsOf(guildID) {
		channelNames[c.ID] = c.Name
	}

	// Newest first, which is the reverse of the fold order.
	out := make([]GovLogEntry, 0, limit)
	for i := len(ordered) - 1; i >= 0; i-- {
		if offset > 0 {
			offset--
			continue
		}
		if len(out) >= limit {
			break
		}
		o := ordered[i]
		v := verdicts[o.hash()]
		e := GovLogEntry{
			Hash:        o.hash(),
			Seq:         o.Seq,
			Type:        o.Type,
			Signer:      o.signerFpr(),
			Target:      o.Target,
			RoleID:      o.RoleID,
			RoleName:    roleNames[o.RoleID],
			Color:       o.Color,
			Perms:       o.Perms,
			Position:    o.Position,
			Add:         o.Add,
			Until:       o.Until,
			ChannelID:   o.ChannelID,
			ChannelName: channelNames[o.ChannelID],
			Seconds:     o.Seconds,
			At:          time.Unix(0, o.Time).UnixMilli(),
			Verified:    v.Verified,
			Applied:     v.Applied,
		}
		if o.Type == "role_upsert" {
			if o.Name != "" {
				e.RoleName = o.Name
			}
			e.Created = firstUpsert[o.RoleID] == e.Hash
		}
		e.SignerName = s.govActorName(guildID, e.Signer)
		if e.Target != "" {
			e.TargetName = s.govActorName(guildID, e.Target)
		}
		out = append(out, e)
	}
	return out, nil
}

// GovernanceLogSize is how many operations the guild's log holds, so a pager can
// say whether there is more without fetching it.
func (s *Service) GovernanceLogSize(guildID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.govOps[guildID])
}

// govActorName resolves a fingerprint the way the member list does — a
// per-guild nickname shadows the profile name — and falls back to nothing
// rather than to a guess. A banned member is gone from the roster and from the
// MLS group, so the fingerprint is often all there is, and the panel prints a
// short form of it instead of inventing a name.
func (s *Service) govActorName(guildID, fpr string) string {
	if fpr == "" {
		return ""
	}
	if fpr == s.id.Fingerprint() {
		if n := s.SelfProfile().Name; n != "" {
			return n
		}
	}
	if nick := s.NickOf(guildID, fpr); nick != "" {
		return nick
	}
	return s.ProfileName(fpr)
}

// channelsOf lists a guild's channels for name resolution, sorted so the result
// is stable. Kept here rather than reaching into s.channels at the call site so
// the lock discipline stays in one place.
func (s *Service) channelsOf(guildID string) []domain.Channel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.guilds[guildID]
	if !ok {
		return nil
	}
	out := append([]domain.Channel(nil), g.Channels...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
