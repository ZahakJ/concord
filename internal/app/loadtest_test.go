package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/crypto/mls"
	"github.com/libp2p/go-libp2p/core/peer"
)

// loadtest_test.go answers "how large can a guild grow today", with numbers
// rather than opinions. Nothing in here runs on CI, ever:
//
//	go test ./internal/app/ -run TestScale -v          # skipped
//	CONCORD_LOADTEST=1 go test ./internal/app/ -run TestScale -v -timeout 30m
//	CONCORD_LOADTEST=1 CONCORD_LOADTEST_N=50 go test ...
//
// The two tests measure different things on purpose. TestScaleAdmissionWave
// isolates the admission path — one real Service admitting a crowd of MLS-only
// "ghost" joiners over the invite handler, with no libp2p and no second
// process — so the epoch count, the commit sizes and the response sizes are
// exact and N can go high. TestScaleRealPeers runs whole Services against each
// other, which is the honest end-to-end number and is what the box's memory
// actually limits.
//
// Both A/B against the pre-batch behaviour by pinning maxAdmissionBatch to 1,
// which is precisely "one joiner per commit" — what this code did before.

func requireLoadtest(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("load test")
	}
	if os.Getenv("CONCORD_LOADTEST") != "1" {
		t.Skip("set CONCORD_LOADTEST=1 to run the load tests")
	}
}

// loadtestN reads the joiner count from the environment, defaulting low enough
// that a first run cannot take the machine down.
func loadtestN(def int) int {
	if v := os.Getenv("CONCORD_LOADTEST_N"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// pinAdmissionBatch honours CONCORD_LOADTEST_BATCH for one test: set it to 1
// to reproduce, end to end, what a join wave cost before admissions were
// batched (one joiner per commit).
func pinAdmissionBatch(t *testing.T) {
	t.Helper()
	v := os.Getenv("CONCORD_LOADTEST_BATCH")
	if v == "" {
		return
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		t.Fatalf("CONCORD_LOADTEST_BATCH=%q is not a positive number", v)
	}
	was := maxAdmissionBatch
	maxAdmissionBatch = n
	t.Cleanup(func() { maxAdmissionBatch = was })
	t.Logf("admission batch pinned to %d", n)
}

// startLoadPeer starts a Service pinned to loopback.
//
// The pin is load-bearing, and why is a finding in its own right. go-libp2p's
// resource manager refuses more than EIGHT concurrent inbound connections from
// any one IPv4 /32 (and eight per IPv6 /56); loopback is the only exempt range.
// Every simulated peer here shares the machine's LAN address, so on the LAN
// interface the owner starts resetting handshakes at about sixteen members and
// the joiners see "all dials failed" before a single MLS operation happens —
// the harness would be measuring go-libp2p's DoS protection rather than
// Concord. The limit is real for a real case too (a crowd joining from behind
// one office NAT), which is why it is written down here rather than tuned away.
func startLoadPeer(t *testing.T, ctx context.Context, dir, boot string) *Service {
	t.Helper()
	svc, err := Start(ctx, Config{
		DataDir: dir, Passphrase: "test-pass", DisableMDNS: true,
		BootstrapPeers: []string{boot},
		listenAddrs: []string{
			"/ip4/127.0.0.1/tcp/0",
			"/ip4/127.0.0.1/udp/0/quic-v1",
		},
	})
	if err != nil {
		t.Fatalf("Start load peer in %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

// rssKB is the process's resident set, which for these tests is every simulated
// peer at once. Linux only; 0 elsewhere, and the memory lines are then skipped.
func rssKB() int64 {
	b, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	var total, resident int64
	if _, err := fmt.Sscanf(string(b), "%d %d", &total, &resident); err != nil {
		return 0
	}
	return resident * int64(os.Getpagesize()) / 1024
}

// ghostJoiner is a joiner that exists only as an MLS engine: a key package, a
// welcome, and the ability to prove it can read the group afterwards. It costs
// a few hundred kilobytes instead of a whole libp2p host, which is what lets
// this harness go past the point where real peers stop fitting.
type ghostJoiner struct {
	eng  mls.Engine
	kp   []byte
	from peer.ID
}

func newGhostJoiner(t *testing.T, ctx context.Context) *ghostJoiner {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := mls.New(pub, priv)
	if err != nil {
		t.Fatalf("ghost engine: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	kp, err := eng.KeyPackage(ctx)
	if err != nil {
		t.Fatalf("ghost key package: %v", err)
	}
	// A PeerID the handler can Protect. It is never dialed.
	idPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := peer.IDFromBytes(append([]byte{0x00, 0x24, 0x08, 0x01, 0x12, 0x20}, idPub...))
	if err != nil {
		// Fall back to hashing; the exact identity does not matter.
		pid = peer.ID(idPub)
	}
	return &ghostJoiner{eng: eng, kp: kp, from: pid}
}

// admissionWave fires n concurrent invite requests at one Service and reports
// what the wave cost: how long the slowest joiner waited, how many epochs the
// group burned, and the size of one joiner's response.
type waveResult struct {
	wall         time.Duration
	epochs       uint64
	responseMax  int
	welcomeMax   int
	rosterBytes  int
	failures     int
	joinFailures int
}

func admissionWave(t *testing.T, ctx context.Context, owner *Service, guildID string, ghosts []*ghostJoiner) waveResult {
	t.Helper()
	before := epochOf(t, owner, guildID)

	type outcome struct {
		resp    []byte
		welcome []byte
		err     error
	}
	outs := make([]outcome, len(ghosts))
	var wg sync.WaitGroup
	start := time.Now()
	for i, gh := range ghosts {
		wg.Add(1)
		go func(i int, gh *ghostJoiner) {
			defer wg.Done()
			req, _ := json.Marshal(inviteRequest{GuildID: guildID, KeyPackage: gh.kp})
			resp, err := owner.handleInviteRequest(ctx, gh.from, req)
			outs[i] = outcome{resp: resp, err: err}
		}(i, gh)
	}
	wg.Wait()
	res := waveResult{wall: time.Since(start)}
	res.epochs = epochOf(t, owner, guildID) - before

	for i, o := range outs {
		if o.err != nil {
			res.failures++
			continue
		}
		var parsed inviteResponse
		if json.Unmarshal(o.resp, &parsed) != nil || parsed.Error != "" {
			res.failures++
			continue
		}
		if len(o.resp) > res.responseMax {
			res.responseMax = len(o.resp)
		}
		if len(parsed.Welcome) > res.welcomeMax {
			res.welcomeMax = len(parsed.Welcome)
		}
		if raw, err := json.Marshal(parsed.Profiles); err == nil && len(raw) > res.rosterBytes {
			res.rosterBytes = len(raw)
		}
		if _, err := ghosts[i].eng.Join(ctx, parsed.Welcome); err != nil {
			res.joinFailures++
		}
	}
	return res
}

// TestScaleAdmissionWave is the headline measurement: what a wave of N joiners
// costs the guild, batched and unbatched.
func TestScaleAdmissionWave(t *testing.T) {
	requireLoadtest(t)
	n := loadtestN(25)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, mode := range []struct {
		name  string
		batch int
	}{
		{"one-commit-per-joiner", 1},
		{"batched", 32},
	} {
		t.Run(mode.name, func(t *testing.T) {
			was := maxAdmissionBatch
			maxAdmissionBatch = mode.batch
			defer func() { maxAdmissionBatch = was }()

			owner := startService(t, ctx)
			if err := owner.SetDisplayName("Owner"); err != nil {
				t.Fatalf("SetDisplayName: %v", err)
			}
			g, err := owner.CreateGuild("crowd")
			if err != nil {
				t.Fatalf("CreateGuild: %v", err)
			}
			ghosts := make([]*ghostJoiner, 0, n)
			for i := 0; i < n; i++ {
				ghosts = append(ghosts, newGhostJoiner(t, ctx))
			}

			res := admissionWave(t, ctx, owner, g.ID, ghosts)
			t.Logf("N=%d  wall=%s  epochs=%d  response=%dB  welcome=%dB  roster=%dB  refused=%d  join-failed=%d",
				n, res.wall.Round(time.Millisecond), res.epochs,
				res.responseMax, res.welcomeMax, res.rosterBytes,
				res.failures, res.joinFailures)
			if res.failures > 0 || res.joinFailures > 0 {
				t.Fatalf("%d joiners refused, %d could not open their welcome", res.failures, res.joinFailures)
			}
			if got, _ := owner.MemberCount(g.ID); got != n+1 {
				t.Fatalf("guild holds %d members, want %d", got, n+1)
			}

			// Somebody admitted in the first batch of a wave is behind by the
			// time the last one lands, exactly as a real member would be — and
			// catches up the same way, by applying the commits from the log.
			// Doing it here proves the property the whole batch rests on: the
			// commits a wave produces still form a gapless chain that anyone
			// can walk, whatever size the batches came out.
			groupID := ownerGroupID(t, owner, g.ID)
			for i, gh := range ghosts {
				at, err := gh.eng.Epoch(ctx, groupID)
				if err != nil {
					t.Fatalf("joiner %d Epoch: %v", i, err)
				}
				rows, err := owner.store.CommitsAfter(groupID, at)
				if err != nil {
					t.Fatalf("CommitsAfter: %v", err)
				}
				for _, r := range rows {
					if err := gh.eng.ApplyCommit(ctx, groupID, r.Commit); err != nil {
						t.Fatalf("joiner %d could not apply the commit for epoch %d: %v", i, r.Epoch, err)
					}
				}
			}
			// And then everybody reads the same thing.
			ct, err := owner.mls.Encrypt(ctx, groupID, []byte("welcome, everybody"))
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}
			for i, gh := range ghosts {
				if _, err := gh.eng.Decrypt(ctx, groupID, ct); err != nil {
					t.Fatalf("joiner %d cannot read the group: %v", i, err)
				}
			}
		})
	}
}

func ownerGroupID(t *testing.T, s *Service, guildID string) []byte {
	t.Helper()
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.guilds[guildID].GroupID
}

// TestScaleRosterDiet measures the join-time payload against a guild whose
// members all have avatars — the case the diet was written for.
func TestScaleRosterDiet(t *testing.T) {
	requireLoadtest(t)
	roster := loadtestN(50)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	owner := startService(t, ctx)
	if err := owner.SetDisplayName("Owner"); err != nil {
		t.Fatalf("SetDisplayName: %v", err)
	}
	g, err := owner.CreateGuild("portraits")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	// A 12 KiB avatar each: well under the 64 KiB ceiling, and about what a
	// real one weighs.
	avatar := "data:image/png;base64,"
	for len(avatar) < 12*1024 {
		avatar += "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg"
	}
	for i := 0; i < roster; i++ {
		fpr := fmt.Sprintf("%064x", i+1)
		owner.mu.Lock()
		owner.profiles[fpr] = Profile{Name: fmt.Sprintf("member-%d", i), Avatar: avatar, Color: "#8844ff"}
		owner.mu.Unlock()
	}

	full, err := json.Marshal(owner.profileRoster())
	if err != nil {
		t.Fatal(err)
	}
	lean, err := json.Marshal(owner.joinRoster(g.ID))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("roster of %d: whole=%dB  join-time=%dB  (%.1f%% of the old payload)",
		roster, len(full), len(lean), 100*float64(len(lean))/float64(len(full)))

	var names map[string]Profile
	if err := json.Unmarshal(lean, &names); err != nil {
		t.Fatal(err)
	}
	if len(names) != len(owner.profileRoster()) {
		t.Fatalf("the diet dropped %d people from the roster", len(owner.profileRoster())-len(names))
	}
	for fpr, p := range names {
		if p.Name == "" {
			t.Fatalf("%s lost its name", fpr)
		}
	}
}

// TestScaleRealPeers is the end-to-end ceiling: whole Services, real libp2p,
// real gossip. It reports where the box gives out, which is the honest answer
// to how large a guild can be tested rather than how large one can be.
func TestScaleRealPeers(t *testing.T) {
	requireLoadtest(t)
	pinAdmissionBatch(t)
	n := loadtestN(10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	rssBefore := rssKB()
	owner := startLoadPeer(t, ctx, t.TempDir(), boot)
	if err := owner.SetDisplayName("Owner"); err != nil {
		t.Fatalf("SetDisplayName: %v", err)
	}
	g, err := owner.CreateGuild("townhall")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := g.Channels[0].ID
	code, err := owner.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}

	peers := make([]*Service, 0, n)
	for i := 0; i < n; i++ {
		p := startLoadPeer(t, ctx, t.TempDir(), boot)
		if err := p.SetDisplayName(fmt.Sprintf("member-%d", i)); err != nil {
			t.Fatalf("SetDisplayName: %v", err)
		}
		peers = append(peers, p)
	}

	// Everybody dials at once, which is the wave the batch exists for.
	// CONCORD_LOADTEST_CONC caps it for a run that wants to see the
	// arrive-in-groups case instead.
	conc := n
	if v := os.Getenv("CONCORD_LOADTEST_CONC"); v != "" {
		if c, err := strconv.Atoi(v); err == nil && c > 0 {
			conc = c
		}
	}
	epochBefore := epochOf(t, owner, g.ID)
	start := time.Now()
	var wg sync.WaitGroup
	errs := make([]error, n)
	gate := make(chan struct{}, conc)
	for i, p := range peers {
		wg.Add(1)
		go func(i int, p *Service) {
			defer wg.Done()
			gate <- struct{}{}
			defer func() { <-gate }()
			_, errs[i] = p.JoinViaInvite(code)
		}(i, p)
	}
	wg.Wait()
	wall := time.Since(start)
	epochs := epochOf(t, owner, g.ID) - epochBefore

	failed := 0
	reasons := map[string]int{}
	for _, err := range errs {
		if err != nil {
			failed++
			reasons[err.Error()]++
		}
	}
	for why, count := range reasons {
		t.Logf("N=%d  %d joins failed: %s", n, count, why)
	}
	runtime.GC()
	rssAfter := rssKB()
	mem := ""
	if grew := rssAfter - rssBefore; grew > 0 {
		// Only meaningful when the process actually grew. A run that follows a
		// heavier one starts with the allocator holding memory back, and the
		// difference then says more about the previous test than this one.
		mem = fmt.Sprintf("  rss=%dMB (+%dMB, %.1fMB/peer)",
			rssAfter/1024, grew/1024, float64(grew)/1024/float64(n+1))
	}
	t.Logf("N=%d real peers: join wall=%s  epochs=%d  failed=%d%s",
		n, wall.Round(time.Millisecond), epochs, failed, mem)
	if failed > 0 {
		t.Fatalf("%d of %d joins failed", failed, n)
	}

	waitUntil(t, 3*time.Minute, func() bool {
		want := n + 1
		if got, _ := owner.MemberCount(g.ID); got != want {
			return false
		}
		for _, p := range peers {
			if got, _ := p.MemberCount(g.ID); got != want {
				return false
			}
		}
		return true
	}, "the guild never converged on one roster")
	t.Logf("N=%d converged on %d members after %s", n, n+1, time.Since(start).Round(time.Millisecond))

	// Fan-out: one message from the owner to everybody.
	recs := make([]*recorder, len(peers))
	for i, p := range peers {
		recs[i] = &recorder{}
		p.OnMessage(recs[i].add)
	}
	fanStart := time.Now()
	if _, err := owner.SendMessage(channel, "roll call", "", ""); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	waitUntil(t, 2*time.Minute, func() bool {
		for _, r := range recs {
			if !r.has("roll call") {
				return false
			}
		}
		return true
	}, "the message never reached everyone")
	t.Logf("N=%d fan-out to the slowest member: %s", n, time.Since(fanStart).Round(time.Millisecond))

	// And the commit log is bounded rather than unbounded.
	owner.pruneCommitLogs()
	rows, err := owner.store.CommitsAfter(ownerGroupID(t, owner, g.ID), 0)
	if err != nil {
		t.Fatalf("CommitsAfter: %v", err)
	}
	t.Logf("N=%d owner commit log holds %d rows for %d joiners", n, len(rows), n)
}
