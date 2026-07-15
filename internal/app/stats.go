package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
)

// stats.go gathers read-only diagnostics for the Stats panel: per-guild storage
// + sync health, and a whole-device network/storage view. Everything here is a
// cheap snapshot safe to poll while the panel is open; nothing writes.

// GuildStatsView is one guild/DM's local footprint and sync health.
type GuildStatsView struct {
	Channels     int    `json:"channels"`
	Messages     int64  `json:"messages"`
	MessageBytes int64  `json:"messageBytes"` // stored (encrypted) message payload
	OldestUnix   int64  `json:"oldestUnix"`   // 0 if empty
	NewestUnix   int64  `json:"newestUnix"`
	Members      int    `json:"members"`
	Epoch        uint64 `json:"epoch"`
	OutOfSync    bool   `json:"outOfSync"`
}

// GuildStats aggregates storage + membership + MLS epoch for one guild/DM.
func (s *Service) GuildStats(guildID string) (GuildStatsView, error) {
	var v GuildStatsView
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	s.mu.RUnlock()
	if !ok {
		return v, fmt.Errorf("app: unknown guild %s", guildID)
	}

	chIDs := make([]string, 0, len(g.Channels))
	for _, c := range g.Channels {
		chIDs = append(chIDs, c.ID)
	}
	v.Channels = len(chIDs)

	st, err := s.store.GuildStorage(chIDs)
	if err != nil {
		return v, err
	}
	v.Messages, v.MessageBytes, v.OldestUnix, v.NewestUnix = st.Messages, st.Bytes, st.Oldest, st.Newest

	if creds, err := s.GuildMembers(guildID); err == nil {
		v.Members = len(creds)
	}
	ctx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
	if ep, err := s.mls.Epoch(ctx, g.GroupID); err == nil {
		v.Epoch = ep
	}
	cancel()
	v.OutOfSync = s.OutOfSync(guildID)
	return v, nil
}

// PeerStatView describes one live connection.
type PeerStatView struct {
	ID        string `json:"id"`
	Transport string `json:"transport"` // quic | tcp | relay
	Relayed   bool   `json:"relayed"`
	Direction string `json:"direction"` // inbound | outbound
}

// NetworkStatsView is a whole-device network + storage snapshot.
type NetworkStatsView struct {
	DBSizeBytes      int64          `json:"dbSizeBytes"`
	AttachmentCount  int64          `json:"attachmentCount"`
	AttachmentBytes  int64          `json:"attachmentBytes"`
	Peers            int            `json:"peers"`
	HasBootstrap     bool           `json:"hasBootstrap"`
	BootstrapReached bool           `json:"bootstrapReached"`
	OutOfSyncGuilds  int            `json:"outOfSyncGuilds"`
	PeerList         []PeerStatView `json:"peerList"`
}

// NetworkStats reports DB/blob size and per-peer connection details.
func (s *Service) NetworkStats() NetworkStatsView {
	var v NetworkStatsView
	if n, err := s.store.DBSizeBytes(); err == nil {
		v.DBSizeBytes = n
	}
	if c, b, err := s.store.AttachmentTotals(); err == nil {
		v.AttachmentCount, v.AttachmentBytes = c, b
	}
	ns := s.NetworkStatus()
	v.Peers, v.HasBootstrap, v.BootstrapReached, v.OutOfSyncGuilds =
		ns.Peers, ns.HasBootstrap, ns.BootstrapReached, ns.OutOfSyncGuilds

	h := s.host.Libp2p()
	for _, p := range s.host.Peers() {
		conns := h.Network().ConnsToPeer(p)
		if len(conns) == 0 {
			continue
		}
		c := conns[0]
		addr := c.RemoteMultiaddr().String()
		pv := PeerStatView{ID: p.String(), Direction: "outbound"}
		switch {
		case strings.Contains(addr, "p2p-circuit"):
			pv.Transport, pv.Relayed = "relay", true
		case strings.Contains(addr, "quic"):
			pv.Transport = "quic"
		case strings.Contains(addr, "tcp"):
			pv.Transport = "tcp"
		default:
			pv.Transport = "p2p"
		}
		if c.Stat().Direction == network.DirInbound {
			pv.Direction = "inbound"
		}
		v.PeerList = append(v.PeerList, pv)
	}
	return v
}
