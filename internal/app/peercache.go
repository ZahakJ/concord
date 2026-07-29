package app

import (
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

// The peer cache is the first and most important answer to "what happens when
// the rendezvous server goes away". Discovery normally runs through a server we
// don't control; the addresses of peers we have actually talked to are
// discovery we already own. Re-dialling them at startup means an existing group
// of friends keeps working with no server in the loop at all.
//
// It sits beside netconfig.json in plaintext for the same reason that file
// does: connection state must be usable before the identity is unlocked. None
// of it is secret either — these are addresses both ends already exchanged over
// the wire. What it *is* is a local record of who this install talks to, so it
// is bounded and expires rather than growing forever.
const (
	// maxCachedPeers caps the file. Concord meshes are friend-sized; past this
	// the extra entries are mostly one-off encounters, and every one of them
	// costs a dial on a cold start.
	maxCachedPeers = 64
	// maxAddrsPerPeer bounds one entry. A peer advertises every interface and
	// every transport it has; we only need enough of them to find it again.
	maxAddrsPerPeer = 8
	// maxPeerFails forgets a peer after this many consecutive outages in which
	// we could not reach it — one per launch at most, never one per dial
	// attempt (see Host.redialFailed). Addresses go stale for boring reasons (a
	// friend changed networks), and a dead address should stop costing a dial on
	// every launch; a friend who is merely asleep tonight must still be here
	// tomorrow.
	maxPeerFails = 5
	// maxPeerAge forgets peers we have not reached in a month.
	maxPeerAge = 30 * 24 * time.Hour
	// peerSaveInterval throttles rewrites. A flapping connection would otherwise
	// rewrite the file in a tight loop.
	peerSaveInterval = 30 * time.Second
	// rememberSettleTries × rememberSettleStep is how long rememberPeer keeps
	// watching a peer it could not place for its account to resolve. Same shape
	// and same budget as the DM handler's settle-wait: long enough to cover the
	// startup ordering, short enough that a genuine stranger is forgotten while
	// the connection is still young.
	rememberSettleTries = 20
	rememberSettleStep  = 500 * time.Millisecond
)

// RememberedPeer is one peer we have successfully connected to.
type RememberedPeer struct {
	ID    string   `json:"id"`
	Addrs []string `json:"addrs"`
	// Seen is the unix time of the last successful connection; it is the
	// recency key that decides who survives the cap.
	Seen int64 `json:"seen"`
	// Fails counts consecutive failed re-dials since the last success.
	Fails int `json:"fails,omitempty"`
}

// PeerCache is the bounded, plaintext set of peers worth re-dialling. Its
// methods are safe for concurrent use: entries are recorded from libp2p's
// connection callbacks, which fire on their own goroutines.
type PeerCache struct {
	mu       sync.Mutex
	peers    []RememberedPeer
	dirty    bool
	lastSave time.Time
	// saveMu serialises writers. mu guards the slice and is dropped before the
	// file is touched, so on its own it would let two savers interleave a
	// truncate-and-write of the same path and leave a torn file behind — which
	// on the next launch reads back as "no remembered peers at all", i.e. the
	// fallback gone exactly when it is needed.
	saveMu sync.Mutex
}

type peerCacheFile struct {
	Peers []RememberedPeer `json:"peers"`
}

func peerCachePath(dataDir string) string {
	return filepath.Join(dataDir, "peers.json")
}

// LoadPeerCache reads the remembered peers, returning an empty cache if the
// file is absent or unreadable. A corrupt cache is never fatal — the worst it
// costs is one launch that has to fall back to the rendezvous.
func LoadPeerCache(dataDir string) *PeerCache {
	c := &PeerCache{}
	b, err := os.ReadFile(peerCachePath(dataDir))
	if err != nil {
		return c
	}
	var f peerCacheFile
	if json.Unmarshal(b, &f) != nil {
		return c
	}
	for _, p := range f.Peers {
		if _, err := peer.Decode(p.ID); err != nil {
			continue // not a peer ID we could dial anyway
		}
		p.Addrs = cleanAddrs(p.Addrs)
		if len(p.Addrs) == 0 {
			continue
		}
		c.peers = append(c.peers, p)
	}
	c.prune(time.Now())
	return c
}

// Remember records a live connection: the peer's current addresses replace
// whatever we had, the failure count resets, and it becomes the most recent
// entry. Addresses are the caller's dialable set — see Host.DialableAddrs.
func (c *PeerCache) Remember(id string, addrs []string) {
	addrs = cleanAddrs(addrs)
	if id == "" || len(addrs) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.peers {
		if c.peers[i].ID == id {
			c.peers[i].Addrs = addrs
			c.peers[i].Seen = time.Now().Unix()
			c.peers[i].Fails = 0
			c.dirty = true
			return
		}
	}
	c.peers = append(c.peers, RememberedPeer{ID: id, Addrs: addrs, Seen: time.Now().Unix()})
	c.prune(time.Now())
	c.dirty = true
}

// DialFailed records that a re-dial of a remembered peer did not connect.
// After maxPeerFails in a row the entry is dropped.
func (c *PeerCache) DialFailed(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.peers {
		if c.peers[i].ID != id {
			continue
		}
		c.peers[i].Fails++
		if c.peers[i].Fails >= maxPeerFails {
			c.peers = slices.Delete(c.peers, i, i+1)
		}
		c.dirty = true
		return
	}
}

// AddrInfos returns the remembered peers as dial targets, most recently seen
// first, so the startup re-dial spends its attempts on the likeliest peers.
func (c *PeerCache) AddrInfos() []peer.AddrInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]peer.AddrInfo, 0, len(c.peers))
	for _, p := range c.peers {
		id, err := peer.Decode(p.ID)
		if err != nil {
			continue
		}
		pi := peer.AddrInfo{ID: id}
		for _, a := range p.Addrs {
			ma, err := multiaddr.NewMultiaddr(a)
			if err != nil {
				continue
			}
			pi.Addrs = append(pi.Addrs, ma)
		}
		if len(pi.Addrs) > 0 {
			out = append(out, pi)
		}
	}
	return out
}

// Flush writes the cache if it changed and the throttle has elapsed. Save
// bypasses both, for shutdown and for tests.
func (c *PeerCache) Flush(dataDir string) error {
	// The throttle is checked under saveMu, not just mu: two callers that both
	// pass the check would otherwise both go on to write.
	c.saveMu.Lock()
	defer c.saveMu.Unlock()
	c.mu.Lock()
	skip := !c.dirty || time.Since(c.lastSave) < peerSaveInterval
	c.mu.Unlock()
	if skip {
		return nil
	}
	return c.save(dataDir)
}

// Save writes the cache to disk unconditionally.
func (c *PeerCache) Save(dataDir string) error {
	c.saveMu.Lock()
	defer c.saveMu.Unlock()
	return c.save(dataDir)
}

// save snapshots the cache and writes it. Caller holds c.saveMu.
func (c *PeerCache) save(dataDir string) error {
	c.mu.Lock()
	c.prune(time.Now())
	b, err := json.MarshalIndent(peerCacheFile{Peers: c.peers}, "", "  ")
	if err != nil {
		c.mu.Unlock()
		return err
	}
	// Marked clean here, before the write, so a Remember that lands DURING the
	// write is not swallowed: it is not in b, and it re-dirties the cache for the
	// next save. A write that never reaches the disk undoes this below.
	prevSave := c.lastSave
	c.dirty = false
	c.lastSave = time.Now()
	c.mu.Unlock()

	// Temp file then rename, as elsewhere: a reader never sees a half-written
	// file, and a crash mid-write leaves the previous cache intact rather than a
	// truncated one that parses as empty. The fixed .tmp name is safe because
	// saveMu is the only writer.
	path := peerCachePath(dataDir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		c.saveFailed(prevSave)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		c.saveFailed(prevSave)
		return err
	}
	return nil
}

// saveFailed re-arms a save whose bytes never reached the disk. A failed write
// left clean is a silently lost cache: Flush only writes when dirty, so the
// peers learned this session sit in memory until something else happens to
// dirty the file — and on a quiet install nothing does, so the loss surfaces
// as an empty peers.json on the launch that needed it. The throttle is wound
// back too, because a write that did not happen is not a write.
func (c *PeerCache) saveFailed(prevSave time.Time) {
	c.mu.Lock()
	c.dirty = true
	c.lastSave = prevSave
	c.mu.Unlock()
}

// prune enforces the bounds: drop the expired and the repeatedly-failing, then
// keep the most recently seen up to the cap. Caller holds c.mu.
func (c *PeerCache) prune(now time.Time) {
	cutoff := now.Add(-maxPeerAge).Unix()
	c.peers = slices.DeleteFunc(c.peers, func(p RememberedPeer) bool {
		return p.Fails >= maxPeerFails || p.Seen < cutoff
	})
	slices.SortStableFunc(c.peers, func(a, b RememberedPeer) int {
		return cmp.Compare(b.Seen, a.Seen)
	})
	if len(c.peers) > maxCachedPeers {
		c.peers = c.peers[:maxCachedPeers]
	}
}

// rememberPeer records how we reached a live peer we share a guild with, and
// re-applies the connection-manager protection that lets them use us as a
// circuit relay. Protection is otherwise only set at join time, so without this
// a member who reconnects after a restart looks like a stranger.
//
// Sharing a guild is the whole test. OnPeerConnected fires for every connection
// the host makes — DHT routing peers, whoever else answers the rendezvous key,
// and with the public-DHT opt-in on, arbitrary IPFS nodes — and the cache is a
// 64-entry list evicted by recency. Remembering all of them means an evening of
// DHT churn quietly evicts the handful of friends the cache exists to keep, so
// the one thing it is for (getting back to your people with no server alive) is
// the first thing it loses. A stranger costs us nothing to forget: we have no
// reason to dial them next launch anyway.
//
// It runs on its own goroutine (see the OnPeerConnected registration) because it
// waits: the fingerprint a connection resolves to at connect time may not be the
// account's yet.
func (s *Service) rememberPeer(p peer.ID, fingerprint string) {
	if s.recordPeer(p, fingerprint) {
		return
	}
	// Judged a stranger — which may be the truth, or may be that this is a
	// friend's LINKED DEVICE whose certificate we have not learned yet. Until
	// learnDeviceCert has seen it, presence() answers with the DEVICE's own
	// fingerprint, which matches no member and no contact. The window is real:
	// the host starts dialling remembered peers before the guilds are tracked,
	// and it is trackGuild → relearnDevices that teaches us the mapping. Nothing
	// re-runs this judgement on its own, so losing that race used to mean a
	// friend's phone stayed a stranger for the whole session.
	//
	// Waiting on a CHANGE of fingerprint rather than re-testing the predicate
	// keeps this free for the genuine strangers it also runs for: a DHT routing
	// peer costs a map lookup per tick, not a database query. (The DM handler
	// takes the same settle-wait for the same reason — see dm.go.)
	for i := 0; i < rememberSettleTries; i++ {
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(rememberSettleStep):
		}
		if fpr := s.presence(p).Fingerprint; fpr != fingerprint {
			s.recordPeer(p, fpr)
			return
		}
	}
}

// recordPeer is one judgement of a live peer, with no waiting. It reports
// whether the peer was placed — recorded as a contact or protected as a member —
// so the caller can tell "not one of ours" from "we could not tell yet".
func (s *Service) recordPeer(p peer.ID, fingerprint string) bool {
	placed := false
	// Trust-on-first-use, but only for someone we actually have a relationship
	// with. presence() derives a perfectly valid-looking fingerprint from ANY
	// peer's key, so recording unconditionally fills the table with strangers.
	//
	// The predicate is knownContact, NOT sharesGuild: a friend you only ever DM
	// shares no guild with you, and gating on guild membership would both stop
	// recording them and let the prune below forget them.
	if st := s.store; st != nil && s.knownContact(fingerprint) {
		_ = st.RecordContact(p.String(), fingerprint)
		placed = true
	}
	// Address caching and relay protection stay guild-scoped on purpose — see
	// the security note on peer relays.
	if !s.sharesGuild(fingerprint) {
		return placed
	}
	s.host.Protect(p)

	addrs := s.host.DialableAddrs(p)
	strs := make([]string, 0, len(addrs))
	for _, a := range addrs {
		strs = append(strs, a.String())
	}
	s.peers.Remember(p.String(), strs)
	_ = s.peers.Flush(s.dataDir)
	return true
}

// rememberMembers re-judges every live connection because guild membership just
// changed.
//
// OnPeerConnected is not enough on its own. It fires while the invite handshake
// is still in flight — before the joiner is in the group — so sharesGuild is
// false for the one peer the session is all about, and nothing rechecks it. The
// pair heals on any later connect event (a reconnect, the next launch), which
// means the promise the cache exists to keep ("meet someone today, reach them
// tomorrow with no server alive") skipped exactly the session in which they met.
// Membership is the gate, so re-evaluate when membership moves — for everyone
// connected, not just the peer that dialed: joining a guild can turn a whole
// set of peers we already had connections to into members at once.
func (s *Service) rememberMembers() {
	// recordPeer, not rememberPeer: this runs synchronously on the join path (a
	// caller may read peers.json the moment it returns) and the settle-wait would
	// stall it for ten seconds per stranger still connected.
	for _, p := range s.host.Peers() {
		s.recordPeer(p, s.presence(p).Fingerprint)
	}
}

// pruneContacts drops the strangers an older build recorded. Contacts were once
// written for every peer the transport connected to, so an install that ever
// enabled the public DHT accumulated hundreds of unrelated IPFS nodes, each
// with a real-looking fingerprint and no relationship behind it.
//
// The keep set is everyone we have a relationship with, everyone we are
// connected to, and everyone worth re-dialling. Verified contacts survive
// regardless — PruneContacts won't touch them — so a friend whose guild you've
// since left doesn't quietly lose the verification you did by hand.
func (s *Service) pruneContacts() int {
	if s.store == nil {
		return 0
	}
	known, err := s.store.Contacts()
	if err != nil {
		return 0
	}
	// knownContact is the same predicate that decides whether a DM from someone
	// needs your approval: verified, or shares a guild, or you wrote to them
	// first. Anyone it accepts is a relationship, so anyone it rejects is one of
	// the strangers this exists to clear.
	keep := map[string]bool{s.id.Fingerprint(): true}
	// Whoever we are connected to RIGHT NOW is not a stale stranger row, whatever
	// the predicate thinks.
	for _, p := range s.host.Peers() {
		if fpr := s.presence(p).Fingerprint; fpr != "" {
			keep[fpr] = true
		}
	}
	// …and neither is anyone in the peer cache. That file is not a log of who
	// dialled us: an entry is only written for a peer we shared a guild with when
	// we met, and it expires in a month. Somebody in it who no longer passes
	// knownContact — you left their server, they removed you from theirs — is
	// precisely a person you have met, not a stranger, and we are still re-dialling
	// them on every launch. Deleting their row anyway made them disappear from the
	// contacts list with the connection still up, and verifying them then failed
	// with a raw store error, because verification is an UPDATE of the row that had
	// just been deleted.
	cached := map[string]bool{}
	if s.peers != nil {
		for _, pi := range s.peers.AddrInfos() {
			cached[pi.ID.String()] = true
		}
	}
	for _, c := range known {
		if keep[c.Fingerprint] {
			continue
		}
		if cached[c.PeerID] || s.knownContact(c.Fingerprint) {
			keep[c.Fingerprint] = true
		}
	}
	n, err := s.store.PruneContacts(keep)
	if err != nil {
		return 0
	}
	return n
}

// wantDHT reports whether this install has a route to anything past its own
// LAN, and so a reason to pay for a Kademlia DHT — which does not come alone:
// hole punching, AutoRelay and a reachability pinned to private ride with it.
//
// A configured rendezvous or the public-DHT opt-in obviously qualify. A
// remembered peer only qualifies if we know a public address for it. On a
// LAN-only install every remembered peer is a machine mDNS finds anyway, and
// standing up a DHT to re-find it — with an empty bootstrap set, so it can
// never even join a network — is pure waste on behalf of a user who asked for
// nothing but mDNS.
func wantDHT(bootstrap []peer.AddrInfo, publicDHT bool, remembered []peer.AddrInfo) bool {
	if len(bootstrap) > 0 || publicDHT {
		return true
	}
	for _, pi := range remembered {
		for _, a := range pi.Addrs {
			if manet.IsPublicAddr(a) {
				return true
			}
		}
	}
	return false
}

// cleanAddrs drops unparseable and duplicate addresses and caps the list.
func cleanAddrs(addrs []string) []string {
	seen := make(map[string]bool, len(addrs))
	out := make([]string, 0, min(len(addrs), maxAddrsPerPeer))
	for _, a := range addrs {
		if len(out) >= maxAddrsPerPeer {
			break
		}
		if seen[a] {
			continue
		}
		if _, err := multiaddr.NewMultiaddr(a); err != nil {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	return out
}
