package net

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
)

// DefaultRendezvous namespaces Concord peers in the shared DHT so they find each
// other and not unrelated applications.
const DefaultRendezvous = "concord/dht/v1"

// startDHT brings up the Kademlia DHT for internet-wide discovery: it
// bootstraps off the configured nodes, advertises this peer under the
// rendezvous key, and continuously connects to other peers advertising it.
func (n *Host) startDHT(cfg Config) error {
	rendezvous := cfg.Rendezvous
	if rendezvous == "" {
		rendezvous = DefaultRendezvous
	}

	boot := bootstrapSet(cfg)
	kdht, err := dht.New(n.h,
		dht.Mode(dht.ModeAuto),
		dht.BootstrapPeers(boot...),
	)
	if err != nil {
		return fmt.Errorf("net: create dht: %w", err)
	}
	if err := kdht.Bootstrap(n.ctx); err != nil {
		return fmt.Errorf("net: bootstrap dht: %w", err)
	}
	n.kdht = kdht

	disc := drouting.NewRoutingDiscovery(kdht)
	n.disc = kdht
	go n.advertiseLoop(kdht, disc, rendezvous)
	go n.keepBootstrapped(kdht, boot, cfg.RememberedPeers)
	go n.discoverLoop(disc, rendezvous)
	return nil
}

// advertiseLoop publishes this peer under the rendezvous key, forever.
//
// It replaces discovery/util.Advertise, whose failure path costs two minutes of
// invisibility every single launch. That helper retries a failed Advertise after
// a FLAT two-minute sleep, and the first Advertise always fails: startDHT runs it
// the instant the DHT is created, when the routing table is empty and the
// bootstrap dial (keepBootstrapped, a goroutine started on the next line) has not
// even been attempted — "failed to find any peer in table". So a client that had
// just started, or had just come back from a dropped network, was absent from the
// rendezvous key for 120s while believing it had announced itself.
//
// Measured against the symptom the user reported: a phone waking up and a desktop
// that had been running took 120.06s to connect. Everything downstream inherits
// that — voice presence, gossip, history catch-up — because none of it can happen
// before the two peers have a connection. This is the single largest cause of the
// "I join voice on my phone and my desktop sees it a minute later" delay.
//
// So: wait for a routing table before the first attempt, retry on a short
// backoff, and re-announce immediately whenever we regain the network (the kick).
func (n *Host) advertiseLoop(kdht *dht.IpfsDHT, disc *drouting.RoutingDiscovery, rendezvous string) {
	const (
		minRetry = 2 * time.Second
		maxRetry = 30 * time.Second
	)
	backoff := minRetry
	for {
		// Advertising into an empty routing table cannot succeed, and every failed
		// attempt is a wasted DHT query. Wait for a peer first — but not forever:
		// the timeout falls through to an attempt anyway, so a routing table that
		// is technically empty while a bootstrap connection exists still gets tried.
		n.waitRoutingTable(kdht, 30*time.Second)
		ttl, err := disc.Advertise(n.ctx, rendezvous)
		wait := backoff
		if err == nil {
			backoff = minRetry
			// 7/8 of the TTL, as the upstream helper does: re-announce before the
			// record expires rather than after.
			wait = 7 * ttl / 8
			if wait <= 0 {
				wait = maxRetry
			}
		} else if backoff < maxRetry {
			backoff *= 2
		}
		select {
		case <-n.ctx.Done():
			return
		case <-n.netKick():
			// We just (re)gained a way into the network. Whatever we were waiting
			// out is stale — announce again now, which is the difference between a
			// returning phone appearing in seconds and appearing on the next TTL.
		case <-time.After(n.pace(wait)):
			// pace only bites the failure backoff: the success wait is hours of
			// TTL already, but an offline backgrounded phone retrying a failed
			// announce every 30s all night is the radio drain background mode
			// exists to stop.
		}
	}
}

// waitRoutingTable blocks until the DHT knows at least one peer, or the timeout
// expires. Polling is fine here: it runs once per advertise attempt, not per tick.
func (n *Host) waitRoutingTable(kdht *dht.IpfsDHT, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if kdht.RoutingTable().Size() > 0 {
			return
		}
		select {
		case <-n.ctx.Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// backgroundBeat is the shared slow cadence for every discovery loop while the
// app is backgrounded on a phone. What it protects is the radio, not the CPU:
// a DHT walk every 15s plus redials every 30s keeps a cellular modem
// permanently in its high-power state. One beat, shared, so the wakes coincide
// and the radio pays for one burst instead of several staggered ones.
const backgroundBeat = 3 * time.Minute

// SetBackground tells the host whether the app is off screen. Backgrounded,
// the discovery loops slow to backgroundBeat; existing connections, the relay
// reservation and gossipsub are untouched, so the node stays reachable and
// messages keep arriving. The background→foreground edge kicks every loop
// awake so a returning user gets the eager cadence immediately.
func (n *Host) SetBackground(bg bool) {
	n.mu.Lock()
	changed := n.background != bg
	n.background = bg
	n.mu.Unlock()
	if changed && !bg {
		n.kickNetwork()
	}
}

func (n *Host) backgrounded() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.background
}

// meteredFloor is the fastest any periodic loop in this file runs while the OS
// reports the connection metered, even with the app on screen.
//
// A minute rather than the background beat on purpose. Background mode says
// nobody is looking, so three minutes of latency costs nothing; metered says
// somebody IS looking and the bytes are billed, and the two are independent —
// a phone on cellular with the app open is the case this exists for. Fifteen
// seconds of Kademlia walks is the shape of traffic a cellular modem is worst
// at (many small packets to many hosts, so the radio never leaves its
// high-power state), and a minute is still eager enough that nobody watching
// the member list notices.
//
// What this does NOT touch is the part that matters: connections, the gossip
// mesh, message delivery, mailbox drains and sync all run exactly as they do on
// Wi-Fi. Metered slows the search for peers we do not have; it never delays a
// byte somebody sent us.
const meteredFloor = time.Minute

// SetMetered tells the host whether the OS says this connection is billed by
// the byte. The unmetered edge kicks every loop awake: walking back onto Wi-Fi
// is exactly when the round we have been holding back becomes free.
func (n *Host) SetMetered(m bool) {
	n.mu.Lock()
	changed := n.metered != m
	n.metered = m
	n.mu.Unlock()
	if changed && !m {
		n.kickNetwork()
	}
}

func (n *Host) meteredNet() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.metered
}

// Metered is meteredNet for the layers above. The flag was unexported because
// its only consumer was the pacing arithmetic right here, and the rule it obeys
// is deliberately narrow: metered may delay SEARCHING for peers, never
// delivering a message. Bulk history is the first thing above net that has a
// legitimate claim on the answer — a chronicle chunk is a megabyte nobody asked
// for by name, fetched to fill a screen of history from years ago, and that is
// exactly the traffic a data plan should not silently absorb. It is neither a
// search nor a delivery, so reading the flag here breaks no promise; what would
// break one is ever consulting it on the message path.
func (n *Host) Metered() bool { return n.meteredNet() }

// pace stretches a wait to whichever floor the current conditions impose.
func (n *Host) pace(wait time.Duration) time.Duration {
	return paceWait(wait, n.backgrounded(), n.meteredNet())
}

// paceWait composes the two floors a periodic loop can be held to: the
// background beat while the app is off screen, and meteredFloor while the bytes
// are billed. They compose rather than replace — a backgrounded phone on
// cellular gets the slower of the two, not the more recent of the two — and a
// wait already longer than both is left alone, so an advertise loop parked on
// 7/8 of a DHT TTL is never dragged FASTER by either.
//
// Pure so the composition can be asserted directly; the alternative is
// discovering on a battery graph that one flag quietly cancelled the other.
func paceWait(wait time.Duration, background, metered bool) time.Duration {
	var floor time.Duration
	if metered {
		floor = meteredFloor
	}
	if background && backgroundBeat > floor {
		floor = backgroundBeat
	}
	if wait < floor {
		return floor
	}
	return wait
}

// netKick returns the channel closed-and-replaced whenever we regain a way into
// the network, so the advertise and discovery loops can restart at once instead
// of waiting out a timer that was scheduled while we were offline.
func (n *Host) netKick() <-chan struct{} {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.kick == nil {
		n.kick = make(chan struct{})
	}
	return n.kick
}

// kickNetwork wakes every loop parked on netKick.
func (n *Host) kickNetwork() {
	n.mu.Lock()
	if n.kick != nil {
		close(n.kick)
	}
	n.kick = make(chan struct{})
	n.mu.Unlock()
}

// FindPeer asks the DHT where a specific peer is right now.
//
// This is a targeted Kademlia lookup, not a rendezvous-key provider query: it
// answers "where is THIS peer id" without depending on that peer having
// successfully re-advertised under the shared key. That distinction is what lets
// an account reach its own linked devices promptly — we know their peer ids from
// their certificates, so we never have to wait for their advertisement to land.
func (n *Host) FindPeer(ctx context.Context, p peer.ID) (peer.AddrInfo, error) {
	if n.disc == nil {
		return peer.AddrInfo{}, fmt.Errorf("net: no DHT")
	}
	return n.disc.FindPeer(ctx, p)
}

// bootstrapSet is the list of nodes that can let us into a DHT: the user's own
// rendezvous, plus — only when explicitly asked for — the public IPFS
// bootstrappers.
//
// The public list is the one fallback that works between two peers who have
// never met and have no server of their own. Its price is metadata: joining a
// public DHT tells strangers this peer ID exists at these addresses, and the
// rendezvous key we advertise under is guessable, so an observer can enumerate
// Concord nodes. Messages stay sealed; the fact of running Concord does not.
// That trade is the user's to make, so it is off unless they turn it on.
func bootstrapSet(cfg Config) []peer.AddrInfo {
	if !cfg.PublicBootstrap {
		return cfg.BootstrapPeers
	}
	out := append([]peer.AddrInfo{}, cfg.BootstrapPeers...)
	return append(out, dht.GetDefaultBootstrapPeerAddrInfos()...)
}

// keepBootstrapped keeps at least one bootstrap node connected, for as long as
// the host lives.
//
// This has to be a loop, not a one-shot dial at startup. The first attempt
// routinely fails through no fault of ours — the app launches before Windows
// has finished bringing the network up, a laptop resumes from sleep, a VPN
// flaps, the rendezvous is briefly restarting. And a failure there is total,
// not partial: the DHT can only refresh a routing table that already has
// someone in it, so a node that never reached a bootstrap peer has no way back
// in. It looks exactly like "the internet works, but this app can't see
// anyone", and the only cure used to be restarting the app.
//
// So: retry with backoff while disconnected, re-check on a slow beat while
// connected, and kick the DHT after a reconnection so discovery restarts
// immediately instead of waiting out the next refresh cycle.
//
// Remembered peers ride the same loop rather than getting one of their own:
// they answer the same question ("do we have a way into the network?"), and one
// loop means one backoff and no two schedulers fighting over the same dials.
func (n *Host) keepBootstrapped(kdht *dht.IpfsDHT, peers, remembered []peer.AddrInfo) {
	if len(peers) == 0 && len(remembered) == 0 {
		return // LAN-only node (nothing configured, nobody met); mDNS is its discovery
	}
	const (
		minBackoff   = 2 * time.Second
		maxBackoff   = 2 * time.Minute
		whileHealthy = 30 * time.Second
	)
	backoff := minBackoff
	up, failed, first := false, false, true
	for {
		now := n.dialBootstrap(peers)
		// Peers we have actually met are the fallback that survives the loss of
		// the rendezvous entirely. Dial them on the first pass no matter what — a
		// restart should reach yesterday's friends immediately, well before any
		// DHT lookup completes — and after that only while the rendezvous is
		// unreachable, since discovery already covers us when it is up.
		if first || !now {
			reached := n.dialRemembered(remembered)
			if len(peers) == 0 {
				now = reached // no rendezvous configured: a friend IS the way in
			}
		}
		first = false
		if now && !up {
			// We just (re)gained a way into the network. The routing table may be
			// empty or stale, so refresh it rather than waiting for the DHT's own
			// timer — that's the difference between "connects in seconds" and
			// "connects in an hour".
			if kdht != nil {
				_ = kdht.Bootstrap(n.ctx)
			}
			// …and tell the advertise/discover loops, which are otherwise parked on
			// a timer that was scheduled while there was no network to use.
			n.kickNetwork()
			// We had no route out at all and now we do, which usually means the
			// machine woke up, or moved. Whoever reached us before reached us on
			// the far side of that, so the relay stops claiming to be reachable
			// until somebody reaches it again. Not folded into kickNetwork:
			// that also fires when the user hits reconnect and when the app
			// joins a guild, neither of which says anything about who can get in.
			n.forgetInboundProof()
			if failed {
				log.Printf("concord/net: back on the network, discovery resumed")
			}
		}
		up = now
		wait := whileHealthy
		if now {
			backoff = minBackoff
		} else {
			if !failed {
				log.Printf("concord/net: no bootstrap node or known peer reachable, retrying in the background")
			}
			failed = true
			wait = backoff
			if backoff < maxBackoff {
				backoff *= 2
			}
		}
		select {
		case <-n.ctx.Done():
			return
		case <-time.After(n.pace(wait)):
			// Backgrounded (phone in a pocket), both the healthy re-check and
			// the offline redial stretch to the shared beat; the kickNetwork on
			// return to foreground makes the next check immediate. Note the
			// kick channel below is read BEFORE this loop causes its own kicks
			// (kickNetwork replaces the channel), so waking ourselves is not a
			// livelock risk.
		case <-n.netKick():
		}
	}
}

// dialBootstrap dials every bootstrap node we aren't already connected to and
// reports whether at least one connection is up afterwards.
func (n *Host) dialBootstrap(peers []peer.AddrInfo) bool {
	var wg sync.WaitGroup
	for _, p := range peers {
		if n.h.Network().Connectedness(p.ID) == network.Connected {
			continue
		}
		wg.Add(1)
		go func(pi peer.AddrInfo) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(n.ctx, 20*time.Second)
			defer cancel()
			_ = n.h.Connect(ctx, pi)
		}(p)
	}
	wg.Wait()
	for _, p := range peers {
		if n.h.Network().Connectedness(p.ID) == network.Connected {
			return true
		}
	}
	return false
}

// dialRemembered dials peers we have connected to before and reports whether
// any of them answered. Failures are handed up — once per outage, see
// redialFailed — so the app layer can retire dead addresses; a friend who
// changed networks should stop costing a dial forever.
//
// The timeout is shorter than dialBootstrap's: these addresses are guesses from
// a previous session, there can be dozens of them, and unlike the rendezvous
// nothing else depends on any single one succeeding.
func (n *Host) dialRemembered(peers []peer.AddrInfo) bool {
	var wg sync.WaitGroup
	for _, p := range peers {
		if n.h.Network().Connectedness(p.ID) == network.Connected {
			continue
		}
		wg.Add(1)
		go func(pi peer.AddrInfo) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
			defer cancel()
			if err := n.h.Connect(ctx, pi); err != nil {
				n.redialFailed(pi.ID)
			}
		}(p)
	}
	wg.Wait()
	reached := false
	for _, p := range peers {
		if n.h.Network().Connectedness(p.ID) == network.Connected {
			n.redialReached(p.ID)
			reached = true
		}
	}
	return reached
}

// The discovery schedule. Fast while rounds are still turning up peers we had
// not seen, slower and slower once they stop.
//
// discoverWanted is the floor the backoff cannot pass while the app layer says
// it is still short of a peer it expects (SetPeerDemand): a desktop of yours
// that is not here yet is worth a lookup a minute, and is not worth one every
// fifteen seconds forever.
const (
	discoverMin    = 15 * time.Second
	discoverWanted = time.Minute
	discoverMax    = 4 * time.Minute
)

// discoverPace returns how long to wait before the next discovery round, given
// the wait just served, whether that round turned up a peer the round before it
// had not, and whether the app is still missing a peer it wants.
//
// Kept pure so the schedule can be asserted without waiting minutes of wall
// clock for it — the previous behaviour was a flat constant, which is exactly
// the kind of thing that is only ever noticed on a battery graph.
func discoverPace(prev time.Duration, foundNew, wanted bool) time.Duration {
	if foundNew {
		return discoverMin
	}
	ceiling := discoverMax
	if wanted {
		ceiling = discoverWanted
	}
	next := prev * 2
	if next < discoverMin {
		next = discoverMin
	}
	if next > ceiling {
		next = ceiling
	}
	return next
}

// newCandidate reports whether a round produced a peer the previous round had
// not. Comparing against the previous round rather than against "are we
// connected" is what stops a peer we can see and can never reach — a friend
// behind a firewall that eats our dials — from holding the loop at its fastest
// cadence forever.
func newCandidate(prev map[peer.ID]bool, now []peer.ID) bool {
	for _, p := range now {
		if !prev[p] {
			return true
		}
	}
	return false
}

// SetPeerDemand registers the app layer's answer to "are you still waiting on
// somebody?". Discovery stays eager while it says yes. nil (the default) means
// no demand, i.e. the backoff runs to its full length.
func (n *Host) SetPeerDemand(f func() bool) {
	n.mu.Lock()
	n.peerDemand = f
	n.mu.Unlock()
}

func (n *Host) peerWanted() bool {
	n.mu.RLock()
	f := n.peerDemand
	n.mu.RUnlock()
	return f != nil && f()
}

// discoverLoop looks for peers advertising the rendezvous key, on a cadence
// that follows whether looking is achieving anything.
//
// It used to be a flat 15 seconds, forever. That is right for the first minute
// after launch, when the mesh is warming and every round finds somebody, and
// wrong for every minute after: an idle node — everyone it knows already
// connected, or no network at all — still paid a full Kademlia walk, plus up to
// addrlessLookups more walks and an unbounded fan of dials, four times a
// minute, all night. advertiseLoop and keepBootstrapped in this same file had
// both had proper backoff for a long time; this loop was the one that never
// got it, and on a phone it is the one that costs the most, because a DHT walk
// is many small packets to many hosts and that is the worst possible shape of
// traffic for a cellular radio trying to go back to sleep.
//
// Backgrounded, pace still stretches whatever this decides to backgroundBeat,
// and on a metered connection to meteredFloor; all of them compose rather than
// competing. None of them touches the kick, which sets wait back to zero and
// runs a round immediately — so joining a guild, or walking back into range,
// still looks instant on cellular.
func (n *Host) discoverLoop(disc peerFinder, rendezvous string) {
	var wait time.Duration
	seen := map[peer.ID]bool{}
	for {
		found := n.findAndConnect(disc, rendezvous)
		wait = discoverPace(wait, newCandidate(seen, found), n.peerWanted())
		seen = make(map[peer.ID]bool, len(found))
		for _, p := range found {
			seen[p] = true
		}
		select {
		case <-n.ctx.Done():
			return
		case <-n.netKick():
			// Back on the network, foregrounded, or the app just joined
			// something. Whatever we had backed off to is about a world that no
			// longer applies: start over at the eager cadence.
			wait = 0
			seen = map[peer.ID]bool{}
		case <-time.After(n.pace(wait)):
		}
	}
}

// KickDiscovery restarts the eager discovery cadence. The app layer calls it
// when it has just given itself a reason to look — joining a guild, accepting
// an invite — where waiting out a backoff that was earned while idle would show
// up as "I joined and nobody was there".
func (n *Host) KickDiscovery() { n.kickNetwork() }

// peerFinder is the discovery half of RoutingDiscovery, named as an interface
// so a test can hand findAndConnect the address-less provider record that a
// real DHT only produces intermittently.
type peerFinder interface {
	FindPeers(ctx context.Context, ns string, opts ...discovery.Option) (<-chan peer.AddrInfo, error)
}

// addrlessLookups bounds the extra Kademlia lookups one discovery round may
// start. A provider record with no addresses costs a full DHT walk to resolve,
// and a large mesh can hand us dozens at once.
const addrlessLookups = 8

// findAndConnect runs one discovery round and returns the peers it acted on —
// everyone the rendezvous key offered that we were not already connected to.
// The caller uses that to decide how soon to run the next one; a round whose
// every answer we already had is a round that bought nothing.
func (n *Host) findAndConnect(disc peerFinder, rendezvous string) []peer.ID {
	ctx, cancel := context.WithTimeout(n.ctx, 20*time.Second)
	defer cancel()
	peers, err := disc.FindPeers(ctx, rendezvous)
	if err != nil {
		return nil
	}
	var acted []peer.ID
	// A provider record frequently arrives with the peer id and NO addresses —
	// the record is in the DHT but the addresses were never cached, or expired.
	// Skipping those outright (which this did) is survivable on a desktop, which
	// also has mDNS and a remembered-peer cache to find the same person by
	// another route. On Android it is fatal: SELinux denies the netlink bind
	// zeroconf needs, so mDNS never starts, and the DHT is the ONLY discovery
	// path there. The symptom is a phone that connects to its rendezvous and
	// then sits there seeing nobody, while the desktop shows the same contact
	// online.
	//
	// The host is not a routed host, so Connect will not resolve an empty
	// AddrInfo for us. FindPeer is a targeted Kademlia lookup that answers
	// exactly this question, so ask it.
	slots := make(chan struct{}, addrlessLookups)
	for p := range peers {
		if p.ID == n.h.ID() {
			continue
		}
		if n.h.Network().Connectedness(p.ID) == network.Connected {
			continue
		}
		acted = append(acted, p.ID)
		if len(p.Addrs) > 0 {
			go func(pi peer.AddrInfo) { _ = n.h.Connect(n.ctx, pi) }(p)
			continue
		}
		// We may already know where they are — from a previous connection, a
		// remembered-peer entry, or an invite. Connect consults the peerstore,
		// so spending a Kademlia walk to re-learn an address we are holding is
		// pure latency. Try what we have first.
		if len(n.h.Peerstore().Addrs(p.ID)) > 0 {
			go func(id peer.ID) { _ = n.h.Connect(n.ctx, peer.AddrInfo{ID: id}) }(p.ID)
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			continue // already resolving as many as we allow this round
		}
		go func(id peer.ID) {
			defer func() { <-slots }()
			// Derived from n.ctx, not from ctx: the caller cancels that one on
			// return and this outlives it.
			lc, lcancel := context.WithTimeout(n.ctx, 20*time.Second)
			defer lcancel()
			pi, err := n.FindPeer(lc, id)
			if err != nil || len(pi.Addrs) == 0 {
				return
			}
			_ = n.h.Connect(n.ctx, pi)
		}(p.ID)
	}
	return acted
}
