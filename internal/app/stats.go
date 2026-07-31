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
	ID   string `json:"id"`
	Name string `json:"name"` // resolved display name, "" if unknown/infra
	// The ACCOUNT behind the connection. Two peers sharing one fingerprint are
	// one person on two devices, which the list has no other way to tell.
	Fingerprint string `json:"fingerprint"`
	// Self marked another device of THIS account. Kept for compatibility with an
	// older front end; it is now always false, because a device of yours is not
	// in this list at all — see DeviceList.
	Self      bool   `json:"self,omitempty"`
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
	// DeviceList is THIS account's own devices — a separate list, not rows in
	// PeerList. See LinkedDeviceView.
	DeviceList []LinkedDeviceView `json:"deviceList"`
	// Connections that never identified themselves as a Concord account: the
	// Kademlia DHT's own mesh. Counted, not listed — see NetworkStats.
	BackgroundPeers int `json:"backgroundPeers"`
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
	for _, b := range s.bootstrapPeers() {
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
			// Your own devices have their own section (DeviceList / LinkedDevices)
			// and are not "peers" here. Listing them among the connections your
			// rendezvous introduced you to is what produced the "unknown peer" row
			// people reported: your phone, filed under strangers, and then
			// unnameable because s.profiles deliberately holds no row for you.
			// They are not strangers, and the questions you have about them — is
			// it online, when was it last here — are not the questions this list
			// answers. So: out of it entirely.
			if fpr := s.presence(p).Fingerprint; fpr == s.id.Fingerprint() {
				continue
			}
			// A connection that has never identified itself as a Concord account
			// is not a person: it is the DHT. Kademlia keeps connections to
			// whatever shares its keyspace, and with the public-DHT opt-in that
			// is hundreds of unrelated IPFS nodes. Listing them drowned the
			// handful of rows that actually mean something — the report was
			// "hundreds of peers, and I can't see my friend among them".
			//
			// They are still counted, because "am I connected to anything at
			// all" is a real diagnostic question; they just do not get a row.
			fpr := s.presence(p).Fingerprint
			if fpr == "" {
				v.BackgroundPeers++
				continue
			}
			memberPeers++
			// Resolve the connection to a name you'd recognize, so the peer list
			// reads "Alice / Bob", not two anonymous key hashes — and a stray test
			// instance or stranger stands out immediately.
			pv.Fingerprint = fpr
			if name := s.ProfileOf(fpr).Name; name != "" {
				pv.Name = name
			}
		}
		pv.Transport, pv.Relayed = transportOf(addr), isRelayed(addr)
		pv.Direction = directionOf(c)
		pv.RTTms = s.cachedRTT(p)
		v.PeerList = append(v.PeerList, pv)
	}
	v.MemberPeers = memberPeers
	v.DeviceList = s.LinkedDevices()
	return v
}

// transportOf / isRelayed / directionOf describe one connection, shared by the
// peer list and the device list so both read the same way.
func transportOf(addr string) string {
	switch {
	case strings.Contains(addr, "p2p-circuit"):
		return "relay"
	case strings.Contains(addr, "quic"):
		return "quic"
	case strings.Contains(addr, "tcp"):
		return "tcp"
	default:
		return "p2p"
	}
}

func isRelayed(addr string) bool { return strings.Contains(addr, "p2p-circuit") }

func directionOf(c network.Conn) string {
	if c.Stat().Direction == network.DirInbound {
		return "inbound"
	}
	return "outbound"
}
