package app

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	p2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"

	"github.com/zahak/concord/internal/identity"
	cnet "github.com/zahak/concord/internal/net"
)

func testPeerID(t *testing.T) string {
	t.Helper()
	_, pub, err := p2pcrypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	id, err := peer.IDFromPublicKey(pub)
	if err != nil {
		t.Fatalf("peer id: %v", err)
	}
	return id.String()
}

func TestPeerCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	a, b := testPeerID(t), testPeerID(t)

	c := LoadPeerCache(dir)
	c.Remember(a, []string{"/ip4/198.51.100.7/tcp/4001"})
	c.Remember(b, []string{"/ip4/203.0.113.9/udp/4001/quic-v1"})
	if err := c.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}

	got := LoadPeerCache(dir).AddrInfos()
	if len(got) != 2 {
		t.Fatalf("want 2 remembered peers, got %d", len(got))
	}
	addrs := map[string]string{}
	for _, pi := range got {
		if len(pi.Addrs) != 1 {
			t.Fatalf("want 1 address for %s, got %v", pi.ID, pi.Addrs)
		}
		addrs[pi.ID.String()] = pi.Addrs[0].String()
	}
	if addrs[a] != "/ip4/198.51.100.7/tcp/4001" || addrs[b] != "/ip4/203.0.113.9/udp/4001/quic-v1" {
		t.Fatalf("addresses did not survive the round trip: %v", addrs)
	}
}

func TestPeerCacheRememberReplacesAddrsAndClearsFailures(t *testing.T) {
	dir := t.TempDir()
	id := testPeerID(t)

	c := LoadPeerCache(dir)
	c.Remember(id, []string{"/ip4/198.51.100.7/tcp/4001"})
	c.DialFailed(id)
	c.DialFailed(id)
	c.Remember(id, []string{"/ip4/198.51.100.8/tcp/4001"})

	if len(c.peers) != 1 {
		t.Fatalf("want 1 entry, got %d", len(c.peers))
	}
	if c.peers[0].Fails != 0 {
		t.Fatalf("a successful connection must clear the failure count, got %d", c.peers[0].Fails)
	}
	if len(c.peers[0].Addrs) != 1 || c.peers[0].Addrs[0] != "/ip4/198.51.100.8/tcp/4001" {
		t.Fatalf("want the fresh address to replace the old one, got %v", c.peers[0].Addrs)
	}
}

func TestPeerCacheDropsRepeatedFailures(t *testing.T) {
	dir := t.TempDir()
	keep, dead := testPeerID(t), testPeerID(t)

	c := LoadPeerCache(dir)
	c.Remember(keep, []string{"/ip4/198.51.100.7/tcp/4001"})
	c.Remember(dead, []string{"/ip4/198.51.100.8/tcp/4001"})
	for i := 0; i < maxPeerFails-1; i++ {
		c.DialFailed(dead)
	}
	if len(c.peers) != 2 {
		t.Fatalf("dropped a peer before the failure limit: %d left", len(c.peers))
	}
	c.DialFailed(dead)
	if len(c.peers) != 1 || c.peers[0].ID != keep {
		t.Fatalf("want only the reachable peer left, got %+v", c.peers)
	}

	if err := c.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := LoadPeerCache(dir).AddrInfos(); len(got) != 1 {
		t.Fatalf("dropped peer came back after a reload: %d entries", len(got))
	}
}

func TestPeerCachePrunesStaleAndCapsSize(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	c := &PeerCache{}
	// One entry too old to be worth a dial.
	c.peers = append(c.peers, RememberedPeer{
		ID:    testPeerID(t),
		Addrs: []string{"/ip4/198.51.100.1/tcp/4001"},
		Seen:  now.Add(-maxPeerAge - time.Hour).Unix(),
	})
	// More recent peers than the cap allows, oldest first.
	total := maxCachedPeers + 10
	newest, stalestKept := "", ""
	for i := 0; i < total; i++ {
		id := testPeerID(t)
		newest = id
		if i == total-maxCachedPeers {
			stalestKept = id
		}
		c.peers = append(c.peers, RememberedPeer{
			ID:    id,
			Addrs: []string{fmt.Sprintf("/ip4/198.51.100.2/tcp/%d", 4000+i)},
			Seen:  now.Add(-time.Duration(total-i) * time.Minute).Unix(),
		})
	}
	if err := c.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}

	got := LoadPeerCache(dir).AddrInfos()
	if len(got) != maxCachedPeers {
		t.Fatalf("want the cache capped at %d, got %d", maxCachedPeers, len(got))
	}
	if got[0].ID.String() != newest {
		t.Fatalf("want the newest peer kept and first, got %s", got[0].ID)
	}
	if got[len(got)-1].ID.String() != stalestKept {
		t.Fatalf("want the cap to drop the stalest, got %s at the tail", got[len(got)-1].ID)
	}
}

func TestPeerCacheRejectsJunk(t *testing.T) {
	dir := t.TempDir()
	id := testPeerID(t)

	c := LoadPeerCache(dir)
	c.Remember(id, []string{"not-a-multiaddr", "/ip4/198.51.100.7/tcp/4001", "/ip4/198.51.100.7/tcp/4001"})
	if len(c.peers) != 1 || len(c.peers[0].Addrs) != 1 {
		t.Fatalf("want unparseable and duplicate addresses dropped, got %+v", c.peers)
	}

	// A peer with no usable address is not worth an entry.
	c.Remember(testPeerID(t), []string{"nonsense"})
	if len(c.peers) != 1 {
		t.Fatalf("want addressless peer ignored, got %d entries", len(c.peers))
	}

	// More addresses than we keep per peer.
	var many []string
	for i := 0; i < maxAddrsPerPeer+5; i++ {
		many = append(many, fmt.Sprintf("/ip4/198.51.100.7/tcp/%d", 4000+i))
	}
	c.Remember(id, many)
	if len(c.peers[0].Addrs) != maxAddrsPerPeer {
		t.Fatalf("want addresses capped at %d, got %d", maxAddrsPerPeer, len(c.peers[0].Addrs))
	}
}

func TestLoadPeerCacheSurvivesCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "peers.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := LoadPeerCache(dir).AddrInfos(); len(got) != 0 {
		t.Fatalf("want an empty cache from a corrupt file, got %d entries", len(got))
	}
}

func TestLoadPeerCacheSkipsUndialableEntries(t *testing.T) {
	dir := t.TempDir()
	good := testPeerID(t)
	body := fmt.Sprintf(`{"peers":[
	  {"id":%q,"addrs":["/ip4/198.51.100.7/tcp/4001"],"seen":%d},
	  {"id":"not-a-peer-id","addrs":["/ip4/198.51.100.8/tcp/4001"],"seen":%d},
	  {"id":%q,"addrs":[],"seen":%d}
	]}`, good, time.Now().Unix(), time.Now().Unix(), testPeerID(t), time.Now().Unix())
	if err := os.WriteFile(filepath.Join(dir, "peers.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := LoadPeerCache(dir).AddrInfos()
	if len(got) != 1 || got[0].ID.String() != good {
		t.Fatalf("want only the dialable entry, got %+v", got)
	}
}

func TestPeerCacheFlushThrottles(t *testing.T) {
	dir := t.TempDir()
	c := LoadPeerCache(dir)
	c.Remember(testPeerID(t), []string{"/ip4/198.51.100.7/tcp/4001"})
	if err := c.Flush(dir); err != nil {
		t.Fatalf("first flush: %v", err)
	}

	// A second change inside the throttle window must not rewrite the file, or a
	// flapping connection would hammer the disk.
	before, err := os.Stat(filepath.Join(dir, "peers.json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	c.Remember(testPeerID(t), []string{"/ip4/198.51.100.8/tcp/4001"})
	if err := c.Flush(dir); err != nil {
		t.Fatalf("second flush: %v", err)
	}
	after, err := os.Stat(filepath.Join(dir, "peers.json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if after.Size() != before.Size() {
		t.Fatal("throttled flush rewrote the file")
	}

	// Save ignores the throttle, so shutdown never loses the last peers learned.
	if err := c.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := LoadPeerCache(dir).AddrInfos(); len(got) != 2 {
		t.Fatalf("want both peers after an unthrottled save, got %d", len(got))
	}
}

// TestPeerCacheFailedSaveIsRetried: a save whose bytes never reached the disk
// used to be marked clean anyway, so Flush — which only writes when dirty —
// never retried it. On a quiet install nothing else dirties the cache, so the
// peers learned this session are gone, and the discovery-with-no-server story
// fails on the launch it was written for.
func TestPeerCacheFailedSaveIsRetried(t *testing.T) {
	dir := t.TempDir()
	id := testPeerID(t)

	c := LoadPeerCache(dir)
	c.Remember(id, []string{"/ip4/198.51.100.7/tcp/4001"})

	// A directory that does not exist: the temp file cannot even be created.
	if err := c.Save(filepath.Join(dir, "not-a-directory")); err == nil {
		t.Fatal("want an error saving into a missing directory")
	}
	c.mu.Lock()
	dirty := c.dirty
	c.mu.Unlock()
	if !dirty {
		t.Fatal("a save that wrote nothing left the cache clean; the peer is lost")
	}

	// The ordinary throttled path must now pick the change back up — this is the
	// call that silently did nothing before.
	if err := c.Flush(dir); err != nil {
		t.Fatalf("flush: %v", err)
	}
	got := LoadPeerCache(dir).AddrInfos()
	if len(got) != 1 || got[0].ID.String() != id {
		t.Fatalf("want the peer written by the retry, got %+v", got)
	}
}

// TestPeerCacheConcurrentSavesNeverTearTheFile hammers the two write paths at
// once. They used to check the throttle under one lock and then write outside
// it, so two truncating writes could interleave — and a half-written peers.json
// does not read back as "slightly stale", it reads back as "no peers at all",
// losing the whole fallback on the launch that needs it.
func TestPeerCacheConcurrentSavesNeverTearTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "peers.json")

	c := LoadPeerCache(dir)
	const writers, rounds = 8, 40
	ids := make([][]string, writers)
	for i := range ids {
		for j := 0; j < rounds; j++ {
			ids[i] = append(ids[i], testPeerID(t))
		}
	}
	// Prime the file so a reader that sees no entries has really seen a torn one.
	c.Remember(ids[0][0], []string{"/ip4/198.51.100.7/tcp/4001"})
	if err := c.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}

	var readers, writersWG sync.WaitGroup
	stop := make(chan struct{})
	readers.Add(1)
	go func() {
		defer readers.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			b, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var f peerCacheFile
			if err := json.Unmarshal(b, &f); err != nil {
				t.Errorf("read a torn peers.json (%d bytes): %v", len(b), err)
				return
			}
			if len(f.Peers) == 0 {
				t.Error("read a peers.json with no entries at all")
				return
			}
		}
	}()

	for i := 0; i < writers; i++ {
		writersWG.Add(1)
		go func(mine []string) {
			defer writersWG.Done()
			for _, id := range mine {
				c.Remember(id, []string{"/ip4/198.51.100.7/tcp/4001"})
				_ = c.Flush(dir)
				_ = c.Save(dir)
			}
		}(ids[i])
	}
	writersWG.Wait()
	close(stop)
	readers.Wait()

	if got := LoadPeerCache(dir).AddrInfos(); len(got) != maxCachedPeers {
		t.Fatalf("want a full cache of %d after the storm, got %d", maxCachedPeers, len(got))
	}
}

// TestWantDHTOnlyForRoutesOffTheLAN: the DHT does not arrive alone — hole
// punching, AutoRelay and a pinned-private reachability come with it — so an
// install whose only remembered peers are LAN machines mDNS already finds must
// not end up starting one to bootstrap off an empty set.
func TestWantDHTOnlyForRoutesOffTheLAN(t *testing.T) {
	rendezvous := []peer.AddrInfo{{ID: peer.ID("rendezvous")}}
	lanFriend := []peer.AddrInfo{{ID: peer.ID("friend"), Addrs: []multiaddr.Multiaddr{
		multiaddr.StringCast("/ip4/192.168.1.9/tcp/4001"),
	}}}
	wanFriend := []peer.AddrInfo{{ID: peer.ID("friend"), Addrs: []multiaddr.Multiaddr{
		multiaddr.StringCast("/ip4/192.168.1.9/tcp/4001"),
		multiaddr.StringCast("/ip4/8.8.8.8/tcp/4001"),
	}}}

	cases := []struct {
		name       string
		bootstrap  []peer.AddrInfo
		publicDHT  bool
		remembered []peer.AddrInfo
		want       bool
	}{
		{"nothing at all", nil, false, nil, false},
		{"lan-only install that has met a peer", nil, false, lanFriend, false},
		{"a rendezvous is configured", rendezvous, false, nil, true},
		{"the public-DHT opt-in is on", nil, true, nil, true},
		{"a friend we reached over the internet", nil, false, wanFriend, true},
	}
	for _, tc := range cases {
		if got := wantDHT(tc.bootstrap, tc.publicDHT, tc.remembered); got != tc.want {
			t.Errorf("%s: wantDHT = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestInviteJoinPopulatesPeerCacheSameSession drives the REAL population path —
// two Services, one invite code — because that is the path that broke: the cache
// is written from a connection callback, but the callback fires during the
// invite handshake, before the joiner is a member, and remembering is gated on
// membership. Nothing rechecked it, so the pair that met today only landed in
// peers.json after a reconnect or a restart. Every other test here injects peers
// with svc.peers.Remember, which is exactly why that got through.
//
// Nothing is slept on and no reconnect is allowed to help: mDNS is off and there
// is no DHT, so the only connection either side ever has is the one the join
// made. Both ends must have written it by the time JoinViaInvite returns.
func TestInviteJoinPopulatesPeerCacheSameSession(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	owner := startService(t, ctx)
	joiner := startService(t, ctx)

	g, err := owner.CreateGuild("cache-guild")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, err := owner.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := joiner.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}

	// On disk, not just in memory: the next launch reads the file.
	assertRemembered(t, "joiner", joiner, owner.host.PeerID().String())
	assertRemembered(t, "owner", owner, joiner.host.PeerID().String())
}

// assertRemembered fails unless svc's peers.json holds want with an address.
func assertRemembered(t *testing.T, who string, svc *Service, want string) {
	t.Helper()
	for _, pi := range LoadPeerCache(svc.dataDir).AddrInfos() {
		if pi.ID.String() == want {
			if len(pi.Addrs) == 0 {
				t.Fatalf("%s remembered %s with no address", who, want)
			}
			return
		}
	}
	t.Fatalf("%s did not remember %s in the session they met", who, want)
}

// TestStrangersCannotEvictRememberedFriends is the reason remembering is gated
// on sharing a guild. Every connection the host makes lands in OnPeerConnected —
// DHT routing peers, whoever answers the rendezvous key, and with the public-DHT
// opt-in on, unrelated IPFS nodes — while the cache is a 64-entry list evicted
// by recency. Remembering all of them means an evening of churn quietly deletes
// the friends the cache exists to keep.
func TestStrangersCannotEvictRememberedFriends(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	lan := lanIP(t)
	if lan == "" {
		t.Skip("no private non-loopback interface; strangers would have no address worth remembering")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := startService(t, ctx)

	// Five friends from previous sessions, last reached yesterday — which is
	// what makes them evictable by a crowd seen just now.
	friends := map[string]bool{}
	for i := 0; i < 5; i++ {
		id := testPeerID(t)
		friends[id] = true
		svc.peers.Remember(id, []string{fmt.Sprintf("/ip4/198.51.100.%d/tcp/4001", i+1)})
	}
	svc.peers.mu.Lock()
	for i := range svc.peers.peers {
		svc.peers.peers[i].Seen = time.Now().Add(-24 * time.Hour).Unix()
	}
	svc.peers.mu.Unlock()

	// A handful of peers we share nothing with, connecting the way DHT routing
	// peers and rendezvous-key answers do. Only a handful: libp2p's resource
	// manager caps inbound connections per source IP, and every stranger here
	// shares this machine's. The count does not matter to the property being
	// tested — none of them may reach the cache at all.
	var reachable []multiaddr.Multiaddr
	for _, a := range svc.host.AddrInfo().Addrs {
		if v, err := a.ValueForProtocol(multiaddr.P_IP4); err == nil && v == lan {
			reachable = append(reachable, a)
		}
	}
	if len(reachable) == 0 {
		t.Skip("service is not listening on the LAN interface")
	}
	target := peer.AddrInfo{ID: svc.host.PeerID(), Addrs: reachable}
	strangerIDs := map[string]bool{}
	for i := 0; i < 5; i++ {
		id, err := identity.Generate()
		if err != nil {
			t.Fatalf("generate stranger identity: %v", err)
		}
		h, err := cnet.New(ctx, cnet.Config{Identity: id, ListenAddrs: []string{"/ip4/" + lan + "/tcp/0"}})
		if err != nil {
			t.Fatalf("start stranger %d: %v", i, err)
		}
		t.Cleanup(func() { _ = h.Close() })
		if err := h.Connect(ctx, target); err != nil {
			t.Fatalf("stranger %d could not connect: %v", i, err)
		}
		strangerIDs[h.PeerID().String()] = true
	}

	// rememberPeer runs off the connection callback, so give it room to happen.
	time.Sleep(3 * time.Second)

	svc.peers.mu.Lock()
	defer svc.peers.mu.Unlock()
	survived := 0
	for _, p := range svc.peers.peers {
		if friends[p.ID] {
			survived++
		}
		if strangerIDs[p.ID] {
			t.Errorf("a peer we share no guild with was remembered: %s", p.ID)
		}
	}
	if survived != len(friends) {
		t.Fatalf("friends surviving %d stranger connections: %d/%d", len(strangerIDs), survived, len(friends))
	}
}

// lanIP is a private, non-loopback IPv4 this machine holds, or "" if it has
// none — loopback is useless here because DialableAddrs drops it.
func lanIP(t *testing.T) string {
	t.Helper()
	addrs, err := manet.InterfaceMultiaddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if !manet.IsPrivateAddr(a) || manet.IsIPLoopback(a) {
			continue
		}
		if v, err := a.ValueForProtocol(multiaddr.P_IP4); err == nil {
			return v
		}
	}
	return ""
}
