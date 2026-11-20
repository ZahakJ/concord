package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// The guest leg end to end: what the rendezvous relays is ciphertext, every
// failed join looks the same, and the host's controls over a link (limited
// use, removal, history, revoke) hold. See docs/dev/guest-e2e.md.

func skipShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping networked service test in -short mode")
	}
}

// newGuestMeeting starts a meeting with a guest link and returns it.
func newGuestMeeting(t *testing.T, s *Service) (domain.Guild, string) {
	t.Helper()
	t.Setenv("CONCORD_GUEST_BASE", "http://127.0.0.1:1/")
	g, _, err := s.StartMeeting()
	if err != nil {
		t.Fatalf("StartMeeting: %v", err)
	}
	url, err := s.CreateGuestLink(g.ID, 0)
	if err != nil {
		t.Fatalf("CreateGuestLink: %v", err)
	}
	return g, url
}

// joinGuest walks a raw guest through the handshake into an open meeting.
func joinGuest(t *testing.T, s *Service, secret []byte, name, gid string) *guestClient {
	t.Helper()
	gc := dialGuest(t, s)
	if err := gc.hello(secret); err != nil {
		t.Fatalf("hello: %v", err)
	}
	if err := gc.readChal(); err != nil {
		t.Fatalf("chal: %v", err)
	}
	if err := gc.join(name, gid); err != nil {
		t.Fatalf("join: %v", err)
	}
	return gc
}

// drainGuest reads records until the host goes quiet for `quiet`.
func drainGuest(gc *guestClient, quiet time.Duration) ([]guestFrame, error) {
	var got []guestFrame
	for {
		f, err := gc.recv(quiet)
		if err != nil {
			if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline") {
				return got, nil
			}
			return got, err
		}
		got = append(got, f)
	}
}

// waitClosed reads (and discards) records until the host closes the stream.
func waitClosed(t *testing.T, gc *guestClient) {
	t.Helper()
	for {
		_, err := gc.recv(10 * time.Second)
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) || strings.Contains(err.Error(), "closed") {
			return
		}
		t.Fatalf("host did not close the visit: %v", err)
	}
}

func roomHas(s *Service, channelID, want string) bool {
	msgs, err := s.store.Messages(channelID, 100)
	if err != nil {
		return false
	}
	for _, m := range msgs {
		if strings.Contains(m.Content, want) {
			return true
		}
	}
	return false
}

func waitRoomHas(t *testing.T, s *Service, channelID, want string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if roomHas(s, channelID, want) {
			return
		}
	}
	t.Fatalf("%q never reached the room", want)
}

func guestSessionCount(s *Service, channelID string) int {
	s.guestMu.Lock()
	defer s.guestMu.Unlock()
	return len(s.guestSessions[channelID])
}

func waitNoGuests(t *testing.T, s *Service, channelID string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if guestSessionCount(s, channelID) == 0 {
			return
		}
	}
	t.Fatal("a closed guest session was never unregistered")
}

func onlyGuestPeer(t *testing.T, s *Service, channelID string) string {
	t.Helper()
	s.guestMu.Lock()
	defer s.guestMu.Unlock()
	list := s.guestSessions[channelID]
	if len(list) != 1 {
		t.Fatalf("%d guest sessions, want 1", len(list))
	}
	return guestPeerID(list[0].id)
}

// TestGuestWireCarriesNoPlaintext is the capture test: everything a guest
// visit puts through the rendezvous, with markers in every kind of content,
// and none of it in the clear.
func TestGuestWireCarriesNoPlaintext(t *testing.T) {
	skipShort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)
	g, url := newGuestMeeting(t, s)
	channelID := g.Channels[0].ID
	if err := s.RenameGuild(g.ID, "MeetNameMarker7741"); err != nil {
		t.Fatalf("RenameGuild: %v", err)
	}
	if err := s.SetGuestLinkHistory(g.ID, true); err != nil {
		t.Fatalf("SetGuestLinkHistory: %v", err)
	}
	if _, err := s.SendMessage(channelID, "HistoryMarker8812 said before anyone came", "", ""); err != nil {
		t.Fatal(err)
	}
	knocks := make(chan string, 8)
	s.OnVoicePresence(func(from, fpr, ch, action, target, dest string) {
		if ch == channelID && action == "knock" {
			select {
			case knocks <- fpr:
			default:
			}
		}
	})
	if err := s.PublishCallControl(channelID, "lock", "", ""); err != nil {
		t.Fatal(err)
	}

	secret := linkSecretBytes(t, url)
	gid := "GidMarker5519abcdef"
	gc := joinGuest(t, s, secret, "GuestNameMarker", gid)
	gc.recvUntil(t, "waiting")
	fpr := <-knocks
	if err := s.PublishCallControl(channelID, "admit", fpr, ""); err != nil {
		t.Fatal(err)
	}
	got := gc.recvUntil(t, "welcome")
	sawHistory := false
	more, err := drainGuest(gc, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range append(got, more...) {
		if strings.Contains(f.Content, "HistoryMarker8812") {
			sawHistory = true
		}
	}
	if !sawHistory {
		t.Fatal("history was on but the guest never got it — the capture would prove nothing about history")
	}

	if err := gc.send(guestFrame{Type: "msg", Content: "GuestMsgMarker3307"}); err != nil {
		t.Fatal(err)
	}
	waitRoomHas(t, s, channelID, "GuestMsgMarker3307")
	if _, err := s.SendMessage(channelID, "LiveMarker6620 from a member", "", ""); err != nil {
		t.Fatal(err)
	}
	for {
		f, err := gc.recv(10 * time.Second)
		if err != nil {
			t.Fatalf("live message never reached the guest: %v", err)
		}
		if strings.Contains(f.Content, "LiveMarker6620") {
			break
		}
	}

	// A call: SDP both ways.
	if err := gc.send(guestFrame{Type: "call", Action: "join"}); err != nil {
		t.Fatal(err)
	}
	peer := onlyGuestPeer(t, s, channelID)
	sigs := make(chan string, 4)
	s.OnVoiceSignal(func(from string, data []byte) {
		if from == peer {
			sigs <- string(data)
		}
	})
	time.Sleep(200 * time.Millisecond)
	if err := gc.send(guestFrame{Type: "signal", Data: json.RawMessage(`{"sdp":"SdpGuestMarker1234"}`)}); err != nil {
		t.Fatal(err)
	}
	select {
	case d := <-sigs:
		if !strings.Contains(d, "SdpGuestMarker1234") {
			t.Fatalf("signal arrived altered: %s", d)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the guest's SDP never reached the mesh")
	}
	if err := s.relayToGuest(peer, []byte(`{"sdp":"SdpHostMarker5678"}`)); err != nil {
		t.Fatal(err)
	}
	for {
		f, err := gc.recv(10 * time.Second)
		if err != nil {
			t.Fatalf("host SDP never reached the guest: %v", err)
		}
		if f.Type == "signal" && strings.Contains(string(f.Data), "SdpHostMarker5678") {
			break
		}
	}

	// Typing too fast earns an info frame. The flood goes from its own
	// goroutine: net.Pipe is unbuffered, and the host's reply would otherwise
	// wait on a reader that is itself stuck writing.
	flood := make(chan error, 1)
	go func() {
		for i := 0; i < guestBurst+3; i++ {
			if err := gc.send(guestFrame{Type: "msg", Content: "spam"}); err != nil {
				flood <- err
				return
			}
		}
		flood <- nil
	}()
	for {
		f, err := gc.recv(10 * time.Second)
		if err != nil {
			t.Fatalf("no info frame for flooding: %v", err)
		}
		if f.Type == "info" {
			break
		}
	}
	for done := false; !done; {
		select {
		case err := <-flood:
			if err != nil {
				t.Fatalf("flooding: %v", err)
			}
			done = true
		default:
			_, _ = gc.recv(200 * time.Millisecond)
		}
	}
	// And removal ends it with an end frame.
	if err := s.PublishCallControl(channelID, "disconnect", peer, ""); err != nil {
		t.Fatal(err)
	}
	gc.recvUntil(t, "end")
	waitClosed(t, gc)

	wire := gc.tap.bytes()
	k := guestLinkKey(secret)
	for _, m := range []string{
		"MeetNameMarker7741", "HistoryMarker8812", "GuestMsgMarker3307", "LiveMarker6620",
		"SdpGuestMarker1234", "SdpHostMarker5678", "GuestNameMarker", gid, channelID,
		b64url.EncodeToString(secret), b64url.EncodeToString(k), hex.EncodeToString(k),
		`"waiting"`, `"welcome"`, `"end"`, `"info"`, `"msg"`, `"signal"`, `"join"`,
		"locked", "Slow down", "removed",
	} {
		if bytes.Contains(wire, []byte(m)) {
			t.Errorf("the rendezvous saw %q in the clear", m)
		}
	}
	if bytes.Contains(wire, secret) || bytes.Contains(wire, k) {
		t.Error("the rendezvous saw raw key bytes")
	}
	checkRecords := func(lines [][]byte, clear string) {
		t.Helper()
		for i, l := range lines {
			var f struct{ Type, C string }
			if err := json.Unmarshal(l, &f); err != nil {
				t.Fatalf("relayed line is not JSON: %q", l)
			}
			if i == 0 {
				if f.Type != clear {
					t.Errorf("first line is %q, want clear %q", f.Type, clear)
				}
				continue
			}
			if f.Type != "e" {
				t.Errorf("line %d is a clear %q frame: %q", i, f.Type, l)
				continue
			}
			ct, err := b64url.DecodeString(f.C)
			if err != nil || len(ct)%guestPadBlock != 16 || len(ct) < guestPadBlock+16 {
				t.Errorf("record %d is %d bytes, not 256k+16", i, len(ct))
			}
		}
	}
	toHost, toGuest := gc.tap.lines(false), gc.tap.lines(true)
	if len(toHost) < 5 || len(toGuest) < 5 {
		t.Fatalf("the tap caught too little: %d up, %d down", len(toHost), len(toGuest))
	}
	checkRecords(toHost, "hello")
	checkRecords(toGuest, "chal")

	// Forward secrecy: with S (so K) but not the ephemeral secret, nothing in
	// the recording opens, whatever Z the operator guesses at.
	var hello struct{ Link, Gn, Gpk string }
	var chal struct{ Hn, Hpk string }
	_ = json.Unmarshal(toHost[0], &hello)
	_ = json.Unmarshal(toGuest[0], &chal)
	gn, _ := b64url.DecodeString(hello.Gn)
	gpk, _ := b64url.DecodeString(hello.Gpk)
	hn, _ := b64url.DecodeString(chal.Hn)
	hpk, _ := b64url.DecodeString(chal.Hpk)
	tryOpen := func(z []byte) bool {
		g2h, h2g := guestSessionKeys(k, z, hello.Link, gc.hostPeer, gn, hn, gpk, hpk)
		var up, down struct{ C string }
		_ = json.Unmarshal(toHost[1], &up)
		_ = json.Unmarshal(toGuest[1], &down)
		_, e1 := newGuestCipher(g2h).open(up.C)
		_, e2 := newGuestCipher(h2g).open(down.C)
		return e1 == nil || e2 == nil
	}
	for _, z := range [][]byte{nil, make([]byte, 32), gpk[1:33], hpk[1:33]} {
		if tryOpen(z) {
			t.Fatal("a recorded visit opened with the link secret alone")
		}
	}
	pub, _ := guestPublicKey(hpk)
	realZ, _ := gc.gsk.ECDH(pub)
	if !tryOpen(realZ) {
		t.Fatal("sanity: the recording does not open even with the real ephemeral secret")
	}
}

// TestGuestFailedJoinsLookTheSame: a live link with the wrong key, an unknown
// link, a revoked one and an expired one all answer one chal and then hang up
// with nothing said; so do the dead ones with the right key.
func TestGuestFailedJoinsLookTheSame(t *testing.T) {
	skipShort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)

	_, liveURL := newGuestMeeting(t, s)
	live := linkSecretBytes(t, liveURL)
	revokedG, revokedURL := newGuestMeeting(t, s)
	revoked := linkSecretBytes(t, revokedURL)
	if err := s.RevokeGuestLink(revokedG.ID); err != nil {
		t.Fatalf("RevokeGuestLink: %v", err)
	}
	expiredG, expiredURL := newGuestMeeting(t, s)
	expired := linkSecretBytes(t, expiredURL)
	s.setMeetingExpiry(expiredG.ID, time.Now().Add(-time.Second))
	unknown, _ := guestRandom(guestSecretBytes)

	try := func(name string, secret []byte, wrongKey bool) {
		t.Run(name, func(t *testing.T) {
			gc := dialGuest(t, s)
			if err := gc.hello(secret); err != nil {
				t.Fatal(err)
			}
			if wrongKey {
				gc.k, _ = guestRandom(32)
			}
			if err := gc.readChal(); err != nil {
				t.Fatalf("no uniform chal: %v", err)
			}
			if err := gc.join("Probe", testGid()); err != nil {
				t.Fatal(err)
			}
			gc.expectClosed(t)
			if n := len(gc.tap.lines(true)); n != 1 {
				t.Fatalf("host sent %d lines, want exactly one chal", n)
			}
		})
	}
	try("live/wrong-key", live, true)
	try("unknown/wrong-key", unknown, true)
	try("revoked/wrong-key", revoked, true)
	try("expired/wrong-key", expired, true)
	try("unknown", unknown, false)
	try("revoked", revoked, false)
	try("expired", expired, false)

	// A v1 hello (the bearer token) is not answered at all.
	gc := dialGuest(t, s)
	if err := gc.sendLine([]byte(`{"type":"hello","token":"` + b64url.EncodeToString(live) + `","name":"Old"}`)); err != nil {
		t.Fatal(err)
	}
	gc.expectClosed(t)
}

// TestGuestReplayAndTampering: a recorded handshake replayed, a skipped
// counter, a reflected host record and clear frames injected after chal.
func TestGuestReplayAndTampering(t *testing.T) {
	skipShort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)
	g, url := newGuestMeeting(t, s)
	channelID := g.Channels[0].ID
	secret := linkSecretBytes(t, url)

	t.Run("replayed handshake", func(t *testing.T) {
		gc := joinGuest(t, s, secret, "First", testGid())
		gc.recvUntil(t, "welcome")
		rec := gc.tap.lines(false)
		replay := dialGuest(t, s)
		_ = replay.sendLine(rec[0])
		if l, err := replay.readLine(10 * time.Second); err != nil || !bytes.Contains(l, []byte(`"chal"`)) {
			t.Fatalf("replayed hello got %q (%v), want a fresh chal", l, err)
		}
		_ = replay.sendLine(rec[1])
		replay.expectClosed(t)
	})

	t.Run("counter skip", func(t *testing.T) {
		gc := joinGuest(t, s, secret, "Skipper", testGid())
		gc.recvUntil(t, "welcome")
		gc.tx.ctr++
		_ = gc.send(guestFrame{Type: "msg", Content: "SkippedCounterMarker"})
		waitClosed(t, gc)
		time.Sleep(300 * time.Millisecond)
		if roomHas(s, channelID, "SkippedCounterMarker") {
			t.Fatal("a record with a skipped counter was posted")
		}
	})

	t.Run("reflected host record", func(t *testing.T) {
		gc := joinGuest(t, s, secret, "Mirror", testGid())
		line, err := gc.readLine(10 * time.Second)
		if err != nil {
			t.Fatal(err)
		}
		_ = gc.sendLine(line)
		for {
			if _, err := gc.readLine(10 * time.Second); err != nil {
				if !errors.Is(err, io.EOF) {
					t.Fatalf("host kept the session after a reflected record: %v", err)
				}
				break
			}
		}
	})

	t.Run("clear frames after chal", func(t *testing.T) {
		gc := dialGuest(t, s)
		_ = gc.hello(secret)
		if err := gc.readChal(); err != nil {
			t.Fatal(err)
		}
		_ = gc.sendLine([]byte(`{"type":"msg","content":"ClearMarkerOne"}`))
		_ = gc.sendLine([]byte(`{"type":"join","name":"Clear","gid":"x"}`))
		if err := gc.join("Sealed", testGid()); err != nil {
			t.Fatal(err)
		}
		gc.recvUntil(t, "welcome")
		_ = gc.sendLine([]byte(`{"type":"msg","content":"ClearMarkerTwo"}`))
		_ = gc.send(guestFrame{Type: "msg", Content: "SealedMarkerOK"})
		waitRoomHas(t, s, channelID, "SealedMarkerOK")
		if roomHas(s, channelID, "ClearMarker") {
			t.Fatal("a clear frame after chal was acted on")
		}
	})
}

// TestGuestLinkLimitedUseAndRemoval: five distinct guests, then the link is
// full to new ones but not to a returning gid; a removed gid stays out.
func TestGuestLinkLimitedUseAndRemoval(t *testing.T) {
	skipShort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)
	g, url := newGuestMeeting(t, s)
	channelID := g.Channels[0].ID
	secret := linkSecretBytes(t, url)

	var gids []string
	for i := 0; i < maxGuestsPerLink; i++ {
		gid := testGid()
		gids = append(gids, gid)
		gc := joinGuest(t, s, secret, "Guest", gid)
		gc.recvUntil(t, "welcome")
		_ = gc.wire.Close()
		waitNoGuests(t, s, channelID)
	}
	gc := joinGuest(t, s, secret, "Sixth", testGid())
	if f := gc.recvUntil(t, "end", "welcome"); f[len(f)-1].Type != "end" {
		t.Fatal("a sixth distinct guest got in on a five-guest link")
	}
	waitNoGuests(t, s, channelID)
	back := joinGuest(t, s, secret, "Returning", gids[0])
	back.recvUntil(t, "welcome")

	// Removal bans that gid on this link.
	peer := onlyGuestPeer(t, s, channelID)
	if err := s.PublishCallControl(channelID, "disconnect", peer, ""); err != nil {
		t.Fatal(err)
	}
	if f := back.recvUntil(t, "end"); !strings.Contains(strings.ToLower(f[len(f)-1].Reason), "removed") {
		t.Errorf("removal said %q", f[len(f)-1].Reason)
	}
	waitNoGuests(t, s, channelID)
	again := joinGuest(t, s, secret, "Returning", gids[0])
	if f := again.recvUntil(t, "end", "welcome"); f[len(f)-1].Type != "end" {
		t.Fatal("a removed guest walked back in with the same gid")
	}
	waitNoGuests(t, s, channelID)
	other := joinGuest(t, s, secret, "Other", gids[1])
	other.recvUntil(t, "welcome")
}

// TestGuestHistoryOffByDefault: a guest sees only what is said after they
// arrive unless the host turns history on for the link.
func TestGuestHistoryOffByDefault(t *testing.T) {
	skipShort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)
	g, url := newGuestMeeting(t, s)
	channelID := g.Channels[0].ID
	secret := linkSecretBytes(t, url)
	if _, err := s.SendMessage(channelID, "EarlierMarker4411", "", ""); err != nil {
		t.Fatal(err)
	}
	if on, err := s.GuestLinkHistory(g.ID); err != nil || on {
		t.Fatalf("history defaults to %v (%v), want off", on, err)
	}
	seen := func() bool {
		gc := joinGuest(t, s, secret, "Reader", testGid())
		got := gc.recvUntil(t, "welcome")
		more, _ := drainGuest(gc, 2*time.Second)
		_ = gc.wire.Close()
		waitNoGuests(t, s, channelID)
		for _, f := range append(got, more...) {
			if strings.Contains(f.Content, "EarlierMarker4411") {
				return true
			}
		}
		return false
	}
	if seen() {
		t.Fatal("a guest read history the host never offered")
	}
	if err := s.SetGuestLinkHistory(g.ID, true); err != nil {
		t.Fatal(err)
	}
	if on, _ := s.GuestLinkHistory(g.ID); !on {
		t.Fatal("history did not turn on")
	}
	if !seen() {
		t.Fatal("history on, but the guest was not shown it")
	}
}

// TestGuestRevoke: revoke in the room, at the door, and between chal and
// join; the old link is dead and the next one is new.
func TestGuestRevoke(t *testing.T) {
	skipShort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)

	t.Run("in the room", func(t *testing.T) {
		g, url := newGuestMeeting(t, s)
		channelID := g.Channels[0].ID
		secret := linkSecretBytes(t, url)
		gc := joinGuest(t, s, secret, "Inside", testGid())
		gc.recvUntil(t, "welcome")
		peer := onlyGuestPeer(t, s, channelID)
		var mu sync.Mutex
		left := false
		s.OnVoicePresence(func(from, fpr, ch, action, target, dest string) {
			if from == peer && action == "leave" {
				mu.Lock()
				left = true
				mu.Unlock()
			}
		})
		_ = gc.send(guestFrame{Type: "call", Action: "join"})
		time.Sleep(200 * time.Millisecond)
		if err := s.RevokeGuestLink(g.ID); err != nil {
			t.Fatal(err)
		}
		gc.recvUntil(t, "end")
		waitClosed(t, gc)
		time.Sleep(200 * time.Millisecond)
		mu.Lock()
		if !left {
			t.Error("revoke ended the visit but left the guest in the call")
		}
		mu.Unlock()
		dead := joinGuest(t, s, secret, "Again", testGid())
		dead.expectClosed(t)
		fresh, err := s.CreateGuestLink(g.ID, 0)
		if err != nil || fresh == url {
			t.Fatalf("after revoke Copy link gave %q (%v), want a new link", fresh, err)
		}
	})

	t.Run("at the door", func(t *testing.T) {
		g, url := newGuestMeeting(t, s)
		if err := s.PublishCallControl(g.Channels[0].ID, "lock", "", ""); err != nil {
			t.Fatal(err)
		}
		gc := joinGuest(t, s, linkSecretBytes(t, url), "Knocker", testGid())
		gc.recvUntil(t, "waiting")
		if err := s.RevokeGuestLink(g.ID); err != nil {
			t.Fatal(err)
		}
		if f := gc.recvUntil(t, "end", "welcome"); f[len(f)-1].Type != "end" {
			t.Fatal("a knocking guest was let in after revoke")
		}
	})

	// Revoke does not touch a handshake still before its join (closing it would
	// tell an operator the link had been live); the join then fails silently,
	// exactly as on a link that never existed.
	t.Run("between chal and join", func(t *testing.T) {
		g, url := newGuestMeeting(t, s)
		gc := dialGuest(t, s)
		_ = gc.hello(linkSecretBytes(t, url))
		if err := gc.readChal(); err != nil {
			t.Fatal(err)
		}
		if err := s.RevokeGuestLink(g.ID); err != nil {
			t.Fatal(err)
		}
		_ = gc.join("Late", testGid())
		gc.expectClosed(t)
	})
}

// TestEventGuestRevokeStampsNewURL: revoking an event room's link mints a new
// one onto the event; the old one is dead.
func TestEventGuestRevokeStampsNewURL(t *testing.T) {
	skipShort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t.Setenv("CONCORD_GUEST_BASE", "http://127.0.0.1:1")
	s := startService(t, ctx)
	g, ev := eventForGuests(t, s)
	opened, err := s.OpenEventGuests(g.ID, ev.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	old := linkSecretBytes(t, opened.GuestURL)
	inside := joinGuest(t, s, old, "Early", testGid())
	inside.recvUntil(t, "welcome")

	updated, err := s.RevokeEventGuestLink(g.ID, ev.ID)
	if err != nil {
		t.Fatalf("RevokeEventGuestLink: %v", err)
	}
	if updated.GuestURL == "" || updated.GuestURL == opened.GuestURL {
		t.Fatalf("event URL after revoke: %q", updated.GuestURL)
	}
	stored, _, _ := s.store.EventByID(ev.ID)
	if stored.GuestURL != updated.GuestURL {
		t.Fatal("the new link was not stamped on the stored event")
	}
	inside.recvUntil(t, "end")
	joinGuest(t, s, old, "Old", testGid()).expectClosed(t)
	joinGuest(t, s, linkSecretBytes(t, updated.GuestURL), "New", testGid()).recvUntil(t, "welcome")
}

// TestGuestOldLinksDroppedOnLoad: a link saved by an older build (a bearer
// token with no secret) does not survive a restart.
func TestGuestOldLinksDroppedOnLoad(t *testing.T) {
	skipShort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s := startServiceInDir(t, ctx, dir)
	g, _ := newGuestMeeting(t, s)
	old := map[string]any{"oldBearerToken": map[string]any{
		"guild": g.ID, "channel": g.Channels[0].ID, "expires": time.Now().Add(time.Hour),
	}}
	blob, _ := json.Marshal(old)
	if err := s.store.SetSetting(guestTokensKey, string(blob)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2 := startServiceInDir(t, ctx, dir)
	s2.guestMu.Lock()
	n := len(s2.guestTokens)
	s2.guestMu.Unlock()
	if n != 0 {
		t.Fatalf("%d old-format links survived the restart", n)
	}
}

// TestGuestRefusedAtAdmissionFreesItsSeat: a knock whose link expires before
// the host clicks Admit is turned away, and its seat in the meeting is given
// back. (The refusal once zeroed the link the cleanup used to find the seat,
// so the session stayed counted against the five for good.)
func TestGuestRefusedAtAdmissionFreesItsSeat(t *testing.T) {
	skipShort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)
	g, url := newGuestMeeting(t, s)
	channelID := g.Channels[0].ID
	if err := s.PublishCallControl(channelID, "lock", "", ""); err != nil {
		t.Fatal(err)
	}
	secret := linkSecretBytes(t, url)
	gc := joinGuest(t, s, secret, "Knocker", testGid())
	gc.recvUntil(t, "waiting")
	id := guestLinkID(secret)
	s.guestMu.Lock()
	tok := s.guestTokens[id]
	tok.Expires = time.Now().Add(-time.Second)
	s.guestTokens[id] = tok
	var fpr string
	for _, gs := range s.guestSessions[channelID] {
		fpr = gs.fpr
	}
	s.guestMu.Unlock()
	s.decideGuest(channelID, "admit", fpr)
	if f := gc.recvUntil(t, "end", "welcome"); f[len(f)-1].Type != "end" {
		t.Fatal("a guest whose link expired at the door was let in")
	}
	waitClosed(t, gc)
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.guestMu.Lock()
		n := len(s.guestSessions[channelID])
		s.guestMu.Unlock()
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d session(s) still hold a seat after the refusal", n)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
