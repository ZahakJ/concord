package app

import (
	"strconv"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// arrival.go records WHEN A GUILD ARRIVED ON THIS DEVICE, and uses it as the
// floor under "unread".
//
// Every path that brings a guild here — joining an invite, creating one,
// opening a DM, a linked device adopting the account's guilds — is followed by
// history: anything up to a few hundred rows per channel, synced from whoever
// has them, each one persisted and emitted exactly like a message that was
// just written. Nothing in the read model could tell those apart. A freshly
// joined guild has no read marks at all, so every historical mention was born
// unread in the inbox and every channel's badge counted its whole backlog; a
// phone that had been off for a week took its catch-up as a week of news. That
// is the bug the owner saw on the phone: history arriving as new.
//
// The rule every messenger follows is the obvious one — nothing that was said
// before you arrived is news to you — and the device is the only party that
// knows when that was. It is a LOCAL setting, written once per guild the
// moment the guild is first tracked here, and never written for a guild that
// was already on disk when the process started (that is the upgrade path, and
// a floor set at the upgrade would quietly retire everything the owner had not
// read yet). Guilds with no stamp get a floor of zero, which is the behaviour
// they always had.
//
// Deliberately NOT a read mark. Read marks travel to the account's other
// devices (readstate.go); a phone that has just linked must not tell the
// desktop that everything is read because the phone arrived. The floor stays
// on the device that arrived.
//
// A DIRECT MESSAGE is the exception, and the floor it gets is the
// conversation's creation. A DM is addressed to its members from the moment
// it exists: the person who opens one usually writes into it at once, and
// the invite reaches the other side on the next hello — minutes or a day
// later, when that phone comes online. Stamping the DM at the moment it was
// tracked would put its first message BEFORE the floor, and the one badge
// that must never be wrong — "someone wrote to you" — would stay dark. Read
// marks, which travel, are the right authority for a DM.
const guildArrivedPrefix = "guild_arrived:"

// noteGuildArrival stamps the guild if it has no stamp. Idempotent.
func (s *Service) noteGuildArrival(g *domain.Guild) {
	if g == nil || g.ID == "" {
		return
	}
	key := guildArrivedPrefix + g.ID
	if cur, err := s.store.GetSetting(key); err == nil && cur != "" {
		return
	}
	at := time.Now()
	if g.Kind == "dm" && !g.Created.IsZero() && g.Created.Before(at) {
		at = g.Created
	}
	_ = s.store.SetSetting(key, strconv.FormatInt(at.UnixNano(), 10))
}

// arrivalMemo answers guildArrivedNano once per guild for the span of one
// call — the inbox and the badge count both ask per row, and a page of fifty
// rows from three guilds is three reads, not fifty.
type arrivalMemo map[string]int64

func (s *Service) arrivedNanoMemo(m arrivalMemo, guildID string) int64 {
	if v, ok := m[guildID]; ok {
		return v
	}
	v := s.guildArrivedNano(guildID)
	m[guildID] = v
	return v
}

// guildArrivedNano reports the floor for a guild: the moment it arrived here,
// in UnixNano, or 0 for a guild that predates the stamp.
func (s *Service) guildArrivedNano(guildID string) int64 {
	if guildID == "" {
		return 0
	}
	raw, err := s.store.GetSetting(guildArrivedPrefix + guildID)
	if err != nil || raw == "" {
		return 0
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// channelArrivedNano is the same floor looked up by channel.
func (s *Service) channelArrivedNano(m arrivalMemo, channelID string) int64 {
	s.mu.RLock()
	guildID := s.channelToGuild[channelID]
	s.mu.RUnlock()
	return s.arrivedNanoMemo(m, guildID)
}
