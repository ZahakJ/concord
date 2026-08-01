package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	"github.com/zahak/concord/internal/crypto/mls"
	"github.com/zahak/concord/internal/domain"
)

// The intermittent-sync report, reduced: with every device online, a message
// sometimes arrives instantly and sometimes takes minutes. The suspect is MLS
// epoch drift — a member that missed one membership commit can decrypt nothing
// encrypted after it until something converges the epochs.
//
// driftTrio is desk+phone (one account, desk owns the guild) plus a friend.
func driftTrio(t *testing.T, ctx context.Context) (desk, phone, friend *Service, guildID, textCh string) {
	t.Helper()
	boot := testRendezvous(t, ctx)
	desk, phone, textCh, _ = linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)
	guildID = desk.Guilds()[0].ID

	friend = startServiceOn(t, ctx, t.TempDir(), boot)
	if err := friend.SetDisplayName("Friend"); err != nil {
		t.Fatalf("SetDisplayName: %v", err)
	}
	code, err := desk.InviteCode(guildID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := friend.JoinViaInvite(code); err != nil {
		t.Fatalf("friend JoinViaInvite: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		n, _ := desk.MemberCount(guildID)
		return n == 3
	}, "the friend never joined the guild")
	return desk, phone, friend, guildID, textCh
}

// sees reports whether svc holds a message with the given content.
func sees(svc *Service, channelID, content string) bool {
	msgs, err := svc.Messages(channelID, 100)
	if err != nil {
		return false
	}
	for _, m := range msgs {
		if m.Content == content {
			return true
		}
	}
	return false
}

// deliverAll sends from `from` and waits until every receiver has the message,
// returning the end-to-end latency (to the slowest receiver).
func deliverAll(t *testing.T, from *Service, channelID, body string, timeout time.Duration, to ...*Service) time.Duration {
	t.Helper()
	t0 := time.Now()
	if _, err := from.SendMessage(channelID, body, ""); err != nil {
		t.Fatalf("SendMessage(%q): %v", body, err)
	}
	waitUntil(t, timeout, func() bool {
		for _, svc := range to {
			if !sees(svc, channelID, body) {
				return false
			}
		}
		return true
	}, "message "+body+" never arrived everywhere")
	return time.Since(t0)
}

// loseOneCommit makes desk privately advance the group by one epoch — an Add
// commit that is logged (so history sync can serve it) but never published,
// exactly what a dropped control-topic gossip frame leaves behind. Everyone
// else is now one epoch behind the committer.
func loseOneCommit(t *testing.T, ctx context.Context, desk *Service, guildID string) {
	t.Helper()
	g := func() []byte {
		desk.mu.RLock()
		defer desk.mu.RUnlock()
		return desk.guilds[guildID].GroupID
	}()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ghost, err := mls.New(pub, priv)
	if err != nil {
		t.Fatalf("ghost engine: %v", err)
	}
	kp, err := ghost.KeyPackage(ctx)
	if err != nil {
		t.Fatalf("ghost key package: %v", err)
	}
	commit, _, err := desk.mls.Invite(ctx, g, kp)
	if err != nil {
		t.Fatalf("drift commit: %v", err)
	}
	desk.logCommit(g, commit)
}

// TestEpochDriftDelivery measures both halves of the report on one guild:
// the same-epoch send (instant) and the send right after a lost membership
// commit (stranded until something converges the epochs).
func TestEpochDriftDelivery(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	desk, phone, friend, guildID, textCh := driftTrio(t, ctx)

	// Warm up the meshes, then measure the good case.
	deliverAll(t, desk, textCh, "warmup", 60*time.Second, phone, friend)
	good := deliverAll(t, desk, textCh, "good case", 30*time.Second, phone, friend)
	t.Logf("good case (same epoch): %v to the slowest of 2 receivers", good)

	// One membership commit falls on the floor; the very next message is
	// encrypted at an epoch the phone and the friend have not reached.
	loseOneCommit(t, ctx, desk, guildID)
	bad := deliverAll(t, desk, textCh, "sent after the lost commit", 180*time.Second, phone, friend)
	t.Logf("bad case (receivers one epoch behind): %v to the slowest of 2 receivers", bad)

	// And the guild must not be left wearing the banner.
	waitUntil(t, 60*time.Second, func() bool {
		return !phone.OutOfSync(guildID) && !friend.OutOfSync(guildID)
	}, "a guild stayed flagged out of sync after delivery converged")
}

// TestMessageRacingItsCommitNeedsNoReAdd pins the transient case: a message
// encrypted right after a membership commit travels a different gossip topic
// than the commit, so it can arrive first. That two-millisecond race must be
// absorbed (stash + retry, or a one-round-trip commit bridge) — NOT answered
// with a Remove+re-Add heal, which costs two more commits that every other
// member must gaplessly apply and was itself a source of the drift it claimed
// to repair.
func TestMessageRacingItsCommitNeedsNoReAdd(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	desk, phone, friend, guildID, textCh := driftTrio(t, ctx)
	deliverAll(t, desk, textCh, "warmup", 60*time.Second, phone, friend)

	groupID := desk.Guilds()[0].GroupID
	base, err := desk.mls.Epoch(ctx, groupID)
	if err != nil {
		t.Fatalf("epoch: %v", err)
	}

	// The commit exists at desk but has not reached anyone yet…
	loseOneCommit(t, ctx, desk, guildID)
	msg, err := domain.NewMessage(textCh, desk.PublicKey(), "raced its commit")
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(msg)
	ct, err := desk.mls.Encrypt(ctx, groupID, payload)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// …and its message lands at the friend FIRST, exactly what out-of-order
	// topic delivery does.
	t0 := time.Now()
	friend.receiveCiphertext(groupID, ct)
	waitUntil(t, 30*time.Second, func() bool {
		return sees(friend, textCh, "raced its commit")
	}, "a message that raced its commit was never delivered")
	t.Logf("raced message delivered in %v", time.Since(t0))

	// Everyone converges on base+1 — the one real commit. A re-add heal would
	// have pushed the epoch to base+3 or beyond.
	waitUntil(t, 60*time.Second, func() bool {
		fe, err1 := friend.mls.Epoch(ctx, groupID)
		de, err2 := desk.mls.Epoch(ctx, groupID)
		return err1 == nil && err2 == nil && fe == base+1 && de == base+1
	}, "epochs never converged on the single genuine commit")
	if e, err := desk.mls.Epoch(ctx, groupID); err != nil || e != base+1 {
		t.Fatalf("desk epoch %d (want %d): the transient race triggered membership churn", e, base+1)
	}
	if friend.OutOfSync(guildID) {
		t.Fatal("friend still wearing the catching-up banner after convergence")
	}
}

// TestHelloDoesNotRejoinHeldGuilds pins the commit storm at its source. The
// hello exchange offers guild invite codes to a linked device; redeeming a
// code for a guild the device is ALREADY in makes the owner treat it as a
// stale join retry — Remove leaf, re-Add leaf, two commits — and the join's
// own re-greet handed the same codes straight back, so two linked devices
// ping-ponged re-joins forever, advancing the guild epoch several times a
// second. Every other member had to apply that commit stream gaplessly over
// gossip; whoever dropped one frame was stranded. Here: force the hello
// exchange repeatedly and require the epoch to sit still.
func TestHelloDoesNotRejoinHeldGuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	desk, phone, _, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)

	groupID := desk.Guilds()[0].GroupID
	base, err := desk.mls.Epoch(ctx, groupID)
	if err != nil {
		t.Fatalf("epoch: %v", err)
	}
	for i := 0; i < 3; i++ {
		desk.offerGuildsToOwnDevices()
		phone.offerGuildsToOwnDevices()
		time.Sleep(time.Second)
	}
	// Give any ping-pong time to show itself, then require stillness.
	time.Sleep(2 * time.Second)
	now, err := desk.mls.Epoch(ctx, groupID)
	if err != nil {
		t.Fatalf("epoch: %v", err)
	}
	if now != base {
		t.Fatalf("guild epoch moved %d -> %d with no membership change: hello re-joined a guild the device already holds", base, now)
	}
}

// TestEpochDriftSingleMessage is the sharper version of the user's words: "I
// send ONE message and nothing arrives". No follow-up traffic, so recovery
// cannot ride later decrypt failures — whatever converges the epochs has to be
// triggered by that first failure alone.
func TestEpochDriftSingleMessage(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	desk, phone, friend, guildID, textCh := driftTrio(t, ctx)
	deliverAll(t, desk, textCh, "warmup", 60*time.Second, phone, friend)

	loseOneCommit(t, ctx, desk, guildID)
	bad := deliverAll(t, desk, textCh, "the only message", 180*time.Second, phone, friend)
	t.Logf("single message after drift: %v to the slowest of 2 receivers", bad)
	if bad > 5*time.Second {
		t.Errorf("recovery took %v; a drifted member with everyone online should re-converge in seconds", bad)
	}
}
