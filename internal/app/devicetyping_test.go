package app

import (
	"context"
	"crypto/rand"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

// These tests pin how typing from a linked device is attributed.
//
// The typing topic carries no payload — attribution is entirely
// s.presence(from), which maps a DEVICE PeerID to its account via
// s.deviceAccounts and otherwise falls back to the fingerprint of the raw
// device key, a fingerprint belonging to no member. receiveTyping must
// attribute every surfaced signal to a member ACCOUNT (re-reading the roster
// once on a miss) and drop what cannot be attributed — never emit a raw
// device-key fingerprint, which the composer would render as a truncated key
// ("TQVB GV2Y is typing…").
//
// Two refinements over v0.49, both user-driven:
//   - Your OWN account's other device is surfaced too, attributed to the
//     account. v0.49 suppressed it; the user overruled that — typing on the
//     phone must light up the account name on the desktop.
//   - An unattributable signal still shows nothing, but now actively solicits
//     a hello from the sender, so a member device our roster copy lags behind
//     becomes attributable in seconds instead of at the next reconnect.

// typingCapture wires an OnTyping listener and returns getters for the last
// fingerprint seen on the wanted channel and the number of events.
func typingCapture(svc *Service, channelID string) (last func() (string, bool), count func() int) {
	var mu sync.Mutex
	var got string
	n := 0
	svc.OnTyping(func(from, ch string) {
		if ch != channelID {
			return
		}
		mu.Lock()
		got = from
		n++
		mu.Unlock()
	})
	last = func() (string, bool) {
		mu.Lock()
		defer mu.Unlock()
		return got, n > 0
	}
	count = func() int {
		mu.Lock()
		defer mu.Unlock()
		return n
	}
	return last, count
}

// pumpTyping publishes typing hints until stop() reports true or the deadline
// passes; typing is fire-and-forget gossip and the mesh may still be forming.
func pumpTyping(t *testing.T, from *Service, channelID string, stop func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if err := from.SendTyping(channelID); err != nil {
			t.Fatalf("SendTyping: %v", err)
		}
		if stop() || time.Now().After(deadline) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// TestTypingFromOwnPhoneNamesTheAccountEverywhere: you type on your phone.
// Your desktop MUST surface it, attributed to your ACCOUNT fingerprint (the
// bridge/frontend then render the account display name, never the phone's
// device key) — and a friend in the same guild must see exactly the same
// attribution.
func TestTypingFromOwnPhoneNamesTheAccountEverywhere(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	desk, phone, textCh, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)
	guildID := theGuild(t, desk).ID

	// The friend joins the shared guild, so the same typing gossip reaches a
	// second account and we can assert both audiences see the same attribution.
	friend := startServiceOn(t, ctx, t.TempDir(), boot)
	code, err := desk.InviteCode(guildID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := friend.JoinViaInvite(code); err != nil {
		t.Fatalf("friend JoinViaInvite: %v", err)
	}
	waitUntil(t, 60*time.Second, func() bool {
		n, _ := friend.MemberCount(guildID)
		return n == 3
	}, "the friend never converged on the 3-leaf roster")

	deskLast, _ := typingCapture(desk, textCh)
	friendLast, _ := typingCapture(friend, textCh)

	pumpTyping(t, phone, textCh, func() bool {
		_, d := deskLast()
		_, f := friendLast()
		return d && f
	})
	got, ok := friendLast()
	if !ok {
		t.Fatal("the friend never saw the phone typing at all")
	}
	if got != desk.Fingerprint() {
		t.Fatalf("friend saw typing attributed to %q; want the account %q — the phone's device key leaked through", got, desk.Fingerprint())
	}

	// The user's own desktop: the phone typing must show, and as the ACCOUNT.
	dgot, dok := deskLast()
	if !dok {
		t.Fatal("the desktop never surfaced its own phone's typing; the user explicitly wants to see it")
	}
	if dgot != desk.Fingerprint() {
		t.Fatalf("desktop attributed its own phone's typing to %q; want the account %q", dgot, desk.Fingerprint())
	}
}

// TestTypingFromFriendsPhoneNamesTheFriend: a friend types on THEIR phone in a
// shared guild; we must see the friend's account, not their phone's raw key.
func TestTypingFromFriendsPhoneNamesTheFriend(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)

	// The friend: a desktop with a linked phone (their own guild comes along;
	// we ignore it).
	fDesk, fPhone, _, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)
	if err := fDesk.SetDisplayName("Friend"); err != nil {
		t.Fatalf("SetDisplayName: %v", err)
	}

	// Us: a separate account with a guild both friend devices join.
	me := startServiceOn(t, ctx, t.TempDir(), boot)
	if err := me.SetDisplayName("Me"); err != nil {
		t.Fatalf("SetDisplayName: %v", err)
	}
	g, err := me.CreateGuild("Party")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	textCh := g.Channels[0].ID
	code, err := me.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := fDesk.JoinViaInvite(code); err != nil {
		t.Fatalf("friend desktop JoinViaInvite: %v", err)
	}
	// The friend's phone follows via the own-device guild handover.
	waitUntil(t, 60*time.Second, func() bool {
		for _, fg := range fPhone.Guilds() {
			if fg.ID == g.ID {
				return true
			}
		}
		return false
	}, "the friend's phone never received the shared guild")
	waitUntil(t, 60*time.Second, func() bool {
		n, _ := me.MemberCount(g.ID)
		return n == 3
	}, "our view never reached 3 leaves (us, friend desktop, friend phone)")

	last, _ := typingCapture(me, textCh)
	pumpTyping(t, fPhone, textCh, func() bool { _, ok := last(); return ok })
	got, ok := last()
	if !ok {
		t.Fatal("we never saw the friend's phone typing at all")
	}
	if got != fDesk.Fingerprint() {
		t.Fatalf("typing attributed to %q; want the friend account %q — their phone's device key leaked through", got, fDesk.Fingerprint())
	}

	// The failure mode the fallback used to hide: the device→account map has
	// not been populated (the map is memory-only, and the phone's add-commit
	// races its first keystroke). receiveTyping must recover the mapping from
	// the roster in hand rather than surface the device key's fingerprint.
	me.deviceMu.Lock()
	me.deviceAccounts = map[string]string{}
	me.deviceMu.Unlock()
	last2, _ := typingCapture(me, textCh)
	me.receiveTyping(g.ID, g.GroupID, textCh, fPhone.host.AddrInfo().ID)
	got2, ok2 := last2()
	if !ok2 {
		t.Fatal("typing from a member device was dropped even though its cert is in the roster")
	}
	if got2 != fDesk.Fingerprint() {
		t.Fatalf("with the device map cold, typing attributed to %q; want %q via the roster", got2, fDesk.Fingerprint())
	}

	// And a signal that cannot be attributed at all — the typing topic is
	// plaintext gossip, so any key can publish to it — must surface NOTHING,
	// not an encoded stranger. Watch specifically for a non-friend attribution:
	// the friend's phone is still pumping real gossip at this mesh, so a plain
	// event count would race a legitimate late "Friend is typing…" delivery.
	strangerKey, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatalf("generate stranger key: %v", err)
	}
	strangerID, err := peer.IDFromPrivateKey(strangerKey)
	if err != nil {
		t.Fatalf("stranger peer id: %v", err)
	}
	var strangerMu sync.Mutex
	strangerSeen := ""
	me.OnTyping(func(from, ch string) {
		if ch != textCh || from == fDesk.Fingerprint() {
			return
		}
		strangerMu.Lock()
		strangerSeen = from
		strangerMu.Unlock()
	})
	me.receiveTyping(g.ID, g.GroupID, textCh, strangerID) // synchronous: emits during the call or not at all
	strangerMu.Lock()
	seen := strangerSeen
	strangerMu.Unlock()
	if seen != "" {
		t.Fatalf("an outsider's typing signal was surfaced as %q; it belongs to no member and must be dropped", seen)
	}

	// The active half of the unlearned window: a signal we cannot attribute
	// even after the roster re-read must SOLICIT a hello from its sender, so a
	// member device our roster copy lags behind converges in seconds. Stage the
	// lag with a guild whose roster genuinely lacks the phone's cert: to
	// relearnDevices this is indistinguishable from an add-commit we have not
	// applied yet.
	g2, err := me.CreateGuild("Solo")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	me.deviceMu.Lock()
	me.deviceAccounts = map[string]string{}
	me.deviceMu.Unlock()
	// In a real unlearned window the connection is fresh on both sides; this
	// test mesh has been chatting for a minute, so reset the per-connection
	// hello bookkeeping to match the window being simulated.
	fPhone.answered.release(me.host.AddrInfo().ID)
	me.solicited.release(fPhone.host.AddrInfo().ID)

	_, count4 := typingCapture(me, g2.Channels[0].ID)
	me.receiveTyping(g2.ID, g2.GroupID, g2.Channels[0].ID, fPhone.host.AddrInfo().ID)
	if n := count4(); n != 0 {
		t.Fatalf("typing surfaced %d time(s) in a guild the sender is no member of", n)
	}
	waitUntil(t, 30*time.Second, func() bool {
		return me.presence(fPhone.host.AddrInfo().ID).Fingerprint == fDesk.Fingerprint()
	}, "the unattributable signal never solicited the phone's certificate; attribution must converge without waiting for a reconnect")
}
