package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

// RTT is measured lazily and cached: pinging every peer on every 2s poll would
// be its own traffic, so we reuse a reading for up to rttTTL and refresh stale
// ones in the background (never blocking the stats call).
const rttTTL = 15 * time.Second

var (
	rttMu    sync.Mutex
	rttCache = map[peer.ID]rttEntry{}
)

type rttEntry struct {
	ms int64
	at time.Time
}

// cachedRTT returns the last known RTT in ms (0 if none yet) and kicks off a
// background refresh when the reading is stale.
func (s *Service) cachedRTT(p peer.ID) int64 {
	rttMu.Lock()
	e, ok := rttCache[p]
	fresh := ok && time.Since(e.at) < rttTTL
	if !fresh {
		rttCache[p] = rttEntry{ms: e.ms, at: time.Now()} // claim the refresh slot
	}
	rttMu.Unlock()
	if fresh {
		return e.ms
	}
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
		defer cancel()
		if d, err := s.host.PingRTT(ctx, p); err == nil {
			rttMu.Lock()
			rttCache[p] = rttEntry{ms: d.Milliseconds(), at: time.Now()}
			rttMu.Unlock()
		}
	}()
	return e.ms
}

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
	Name      string `json:"name"`      // resolved display name, "" if unknown/infra
	Role      string `json:"role"`      // "rendezvous" (infra) | "peer"
	Transport string `json:"transport"` // quic | tcp | relay
	Relayed   bool   `json:"relayed"`
	Direction string `json:"direction"` // inbound | outbound
	RTTms     int64  `json:"rttMs"`     // 0 until first measured
}

// NetworkStatsView is a whole-device network + storage snapshot.
type NetworkStatsView struct {
	DBSizeBytes      int64          `json:"dbSizeBytes"`
	AttachmentCount  int64          `json:"attachmentCount"`
	AttachmentBytes  int64          `json:"attachmentBytes"`
	Peers            int            `json:"peers"`       // all libp2p connections
	MemberPeers      int            `json:"memberPeers"` // excluding rendezvous/relay infra
	HasBootstrap     bool           `json:"hasBootstrap"`
	BootstrapReached bool           `json:"bootstrapReached"`
	OutOfSyncGuilds  int            `json:"outOfSyncGuilds"`
	RateIn           float64        `json:"rateIn"`  // bytes/sec, live
	RateOut          float64        `json:"rateOut"` // bytes/sec, live
	TotalIn          int64          `json:"totalIn"` // cumulative bytes
	TotalOut         int64          `json:"totalOut"`
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

	bw := s.host.Bandwidth()
	v.RateIn, v.RateOut, v.TotalIn, v.TotalOut = bw.RateIn, bw.RateOut, bw.TotalIn, bw.TotalOut

	// Most connections are infrastructure (the rendezvous/relay node, DHT peers),
	// not friends — so mark the rendezvous nodes explicitly, or "3 peers" with
	// nobody online is baffling.
	infra := map[peer.ID]bool{}
	for _, b := range s.bootstrap {
		infra[b.ID] = true
	}
	memberPeers := 0

	h := s.host.Libp2p()
	for _, p := range s.host.Peers() {
		conns := h.Network().ConnsToPeer(p)
		if len(conns) == 0 {
			continue
		}
		c := conns[0]
		addr := c.RemoteMultiaddr().String()
		pv := PeerStatView{ID: p.String(), Direction: "outbound", Role: "peer"}
		if infra[p] {
			pv.Role = "rendezvous"
		} else {
			memberPeers++
			// Resolve the connection to a name you'd recognize, so the peer list
			// reads "Alice / Bob", not two anonymous key hashes — and a stray test
			// instance or stranger stands out immediately.
			if fpr := s.presence(p).Fingerprint; fpr != "" {
				if name := s.ProfileOf(fpr).Name; name != "" {
					pv.Name = name
				}
			}
		}
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
		pv.RTTms = s.cachedRTT(p)
		v.PeerList = append(v.PeerList, pv)
	}
	v.MemberPeers = memberPeers
	return v
}
