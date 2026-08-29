package app

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// These tests drive serveGuest over an in-process pipe — the same
// io.ReadWriteCloser shape the rendezvous relay hands it — so the guest half of
// a meeting can be exercised without a browser: what a guest is allowed to see
// before being admitted, and how long a link lives.

// guestPipe starts one guest visit and returns the guest's end of the wire.
func guestPipe(t *testing.T, s *Service) (*bufio.Reader, net.Conn) {
	t.Helper()
	hostEnd, guestEnd := net.Pipe()
	go s.serveGuest(hostEnd)
	t.Cleanup(func() { _ = guestEnd.Close() })
	return bufio.NewReader(guestEnd), guestEnd
}

func sendGuestFrame(t *testing.T, c net.Conn, f guestFrame) {
	t.Helper()
	line, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write(append(line, '\n')); err != nil {
		t.Fatalf("write guest frame: %v", err)
	}
}

// readGuestFrames reads frames until one of `stop` types arrives, asserting the
// framing contract the guest page depends on: every frame is one complete,
// newline-terminated line.
func readGuestFrames(t *testing.T, r *bufio.Reader, c net.Conn, stop ...string) []guestFrame {
	t.Helper()
	want := map[string]bool{}
	for _, s := range stop {
		want[s] = true
	}
	var got []guestFrame
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("after %v: %v (partial %q — the guest page would wait on this forever)", typesOfFrames(got), err, line)
		}
		if !strings.HasSuffix(line, "\n") {
			t.Fatalf("frame not newline-terminated: %q", line)
		}
		var f guestFrame
		if err := json.Unmarshal([]byte(strings.TrimSuffix(line, "\n")), &f); err != nil {
			t.Fatalf("frame is not JSON: %q: %v", line, err)
		}
		got = append(got, f)
		if want[f.Type] {
			return got
		}
	}
}

func typesOfFrames(fs []guestFrame) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Type
	}
	return out
}

// meetingWithGuestLink spins up a meeting and mints a guest link, returning the
// channel and the link's token.
func meetingWithGuestLink(t *testing.T, s *Service, hours int) (channelID, token string) {
	t.Helper()
	t.Setenv("CONCORD_GUEST_BASE", "http://127.0.0.1:1/")
	g, _, err := s.StartMeeting()
	if err != nil {
		t.Fatalf("StartMeeting: %v", err)
	}
	url, err := s.CreateGuestLink(g.ID, hours)
	if err != nil {
		t.Fatalf("CreateGuestLink: %v", err)
	}
	i := strings.Index(url, "&t=")
	if i < 0 {
		t.Fatalf("guest link has no token: %q", url)
	}
	return g.Channels[0].ID, url[i+3:]
}

// TestGuestKnocksAtLockedMeeting is the office-hours path: a locked meeting must
// turn an arriving guest into a knock that the host decides on, and a guest who
// has not been admitted must be able to see and hear NOTHING.
func TestGuestKnocksAtLockedMeeting(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked service test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)

	channelID, token := meetingWithGuestLink(t, s, 0)

	knocks := make(chan string, 8)
	s.OnVoicePresence(func(from, fpr, ch, action, target, dest string) {
		if ch == channelID && action == "knock" {
			knocks <- fpr
		}
	})

	// Lock the call, exactly as the front end's lock button does.
	if err := s.PublishCallControl(channelID, "lock", "", ""); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if !s.guestDoorLocked(channelID) {
		t.Fatal("guest door is open right after locking the call")
	}

	r, c := guestPipe(t, s)
	sendGuestFrame(t, c, guestFrame{Type: "hello", Token: token, Name: "Nate"})

	frames := readGuestFrames(t, r, c, "waiting", "welcome", "end")
	last := frames[len(frames)-1]
	if last.Type != "waiting" {
		t.Fatalf("locked meeting sent %q, want a knock (waiting): %+v", last.Type, last)
	}
	if last.Reason == "" {
		t.Error("waiting frame carries no explanation — the page has nothing to show")
	}

	var fpr string
	select {
	case fpr = <-knocks:
	case <-time.After(5 * time.Second):
		t.Fatal("host was never told anyone is knocking")
	}
	if want := "guest:Nate#"; !strings.HasPrefix(fpr, want) {
		t.Fatalf("knock identified the guest as %q, want %q + session id", fpr, want)
	}

	// A knocking guest is inert. Nothing said in the room reaches them, and the
	// mesh cannot be talked into signalling them (which is what would hand a
	// stranger a media path).
	if _, err := s.SendMessage(channelID, "members-only chatter", "", ""); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if err := s.RelaySignal(peerIDForFpr(t, s, fpr), []byte(`{"x":1}`)); err == nil {
		t.Error("RelaySignal reached a guest who has not been admitted")
	}
	// Nothing may arrive but another knock reminder.
	_ = c.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			break // timed out: silence is the whole point
		}
		var f guestFrame
		_ = json.Unmarshal([]byte(strings.TrimSuffix(line, "\n")), &f)
		if f.Type != "waiting" {
			t.Fatalf("a knocking guest received a %q frame: %+v", f.Type, f)
		}
	}

	// Admit them: the same verb the host's Admit button publishes.
	if err := s.PublishCallControl(channelID, "admit", fpr, ""); err != nil {
		t.Fatalf("admit: %v", err)
	}
	got := readGuestFrames(t, r, c, "welcome", "end")
	if last := got[len(got)-1]; last.Type != "welcome" {
		t.Fatalf("after admission the guest got %q, want welcome: %+v", last.Type, last)
	}

	// And now that they are inside, the room reaches them. (The history they get
	// on admission includes what was said while they knocked — they are a guest
	// arriving now, and history is what every guest is shown.)
	if _, err := s.SendMessage(channelID, "now-you-can-hear-us", "", ""); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	live := false
	for deadline := time.Now().Add(10 * time.Second); !live && time.Now().Before(deadline); {
		for _, f := range readGuestFrames(t, r, c, "msg", "end") {
			if f.Type == "end" {
				t.Fatalf("session ended instead of delivering the message: %s", f.Reason)
			}
			if f.Content == "now-you-can-hear-us" {
				live = true
			}
		}
	}
	if !live {
		t.Fatal("admitted guest never received a message sent after admission")
	}
}

// peerIDForFpr resolves a guest fingerprint to the voice peer id the mesh would
// address, so the test can attempt exactly the call RelaySignal makes.
func peerIDForFpr(t *testing.T, s *Service, fpr string) string {
	t.Helper()
	s.guestMu.Lock()
	defer s.guestMu.Unlock()
	for id, sess := range s.guestByID {
		if sess.fpr == fpr {
			return guestPeerID(id)
		}
	}
	t.Fatalf("no guest session with fingerprint %q", fpr)
	return ""
}

// TestGuestRefusedGetsToldWhy pins the other half of a knock: refusal must end
// the visit with a message, not leave a browser spinning forever.
func TestGuestRefusedGetsToldWhy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked service test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)

	channelID, token := meetingWithGuestLink(t, s, 0)
	knocks := make(chan string, 8)
	s.OnVoicePresence(func(from, fpr, ch, action, target, dest string) {
		if ch == channelID && action == "knock" {
			knocks <- fpr
		}
	})
	if err := s.PublishCallControl(channelID, "lock", "", ""); err != nil {
		t.Fatalf("lock: %v", err)
	}

	r, c := guestPipe(t, s)
	sendGuestFrame(t, c, guestFrame{Type: "hello", Token: token, Name: "Uninvited"})
	readGuestFrames(t, r, c, "waiting", "end")

	var fpr string
	select {
	case fpr = <-knocks:
	case <-time.After(5 * time.Second):
		t.Fatal("host was never told anyone is knocking")
	}
	if err := s.PublishCallControl(channelID, "refuse", fpr, ""); err != nil {
		t.Fatalf("refuse: %v", err)
	}
	frames := readGuestFrames(t, r, c, "end", "welcome")
	last := frames[len(frames)-1]
	if last.Type != "end" {
		t.Fatalf("refused guest got %q, want end", last.Type)
	}
	if last.Reason == "" {
		t.Error("refusal carries no reason — the page shows a blank hang")
	}
}

// TestGuestAdmittedWhenUnlocked keeps the ordinary path honest: with no lock,
// nothing about a guest's arrival changes.
func TestGuestAdmittedWhenUnlocked(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked service test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)

	_, token := meetingWithGuestLink(t, s, 0)
	r, c := guestPipe(t, s)
	sendGuestFrame(t, c, guestFrame{Type: "hello", Token: token, Name: "Walkin"})
	frames := readGuestFrames(t, r, c, "welcome", "waiting", "end")
	if last := frames[len(frames)-1]; last.Type != "welcome" {
		t.Fatalf("unlocked meeting sent %q, want welcome", last.Type)
	}
}

// TestGuestBackfillHidesOtherGuests is the privacy rule on the one surface that
// faces strangers: a guest may see the room from the moment they walk in, and
// must not be handed a roll-call of everyone who was in it earlier. The 30
// messages of history a guest is welcomed with used to include every
// "👤 X joined as a guest" / "👤 X (guest) left" notice, so whoever opened the
// link last read the names of everyone before them.
//
// It asserts on the FRAMES, not on anything rendered: a filter that still ships
// the name and asks the page not to draw it has leaked it.
func TestGuestBackfillHidesOtherGuests(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked service test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)

	channelID, token := meetingWithGuestLink(t, s, 0)

	// Guest one arrives, is seen, says something, and goes.
	r1, c1 := guestPipe(t, s)
	sendGuestFrame(t, c1, guestFrame{Type: "hello", Token: token, Name: "Wilhelmina"})
	readGuestFrames(t, r1, c1, "welcome", "end")
	sendGuestFrame(t, c1, guestFrame{Type: "msg", Content: "hello from the first visitor"})
	// Wait for their arrival notice and their line to be in the room's history,
	// so the backfill guest two gets really does contain both.
	saw := func(want string) bool {
		msgs, err := s.store.Messages(channelID, guestHistoryCount)
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
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		if saw("Wilhelmina joined") && saw("hello from the first visitor") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !saw("Wilhelmina joined") {
		t.Fatal("the first guest's arrival never reached the room — nothing to filter")
	}
	if !saw("hello from the first visitor") {
		t.Fatal("the first guest's message never reached the room")
	}
	_ = c1.Close()

	// Guest two arrives to the same room.
	r2, c2 := guestPipe(t, s)
	sendGuestFrame(t, c2, guestFrame{Type: "hello", Token: token, Name: "Second"})
	got := readGuestFrames(t, r2, c2, "welcome", "end")
	// The welcome is followed by the backfill; give it a moment to land, then
	// read whatever else is queued rather than waiting on a sentinel.
	_ = c2.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		line, err := r2.ReadString('\n')
		if err != nil {
			break
		}
		var f guestFrame
		if json.Unmarshal([]byte(strings.TrimSuffix(line, "\n")), &f) == nil {
			got = append(got, f)
		}
	}

	backfilledLine := false
	for _, f := range got {
		blob, _ := json.Marshal(f)
		if f.Type == "sys" {
			// No presence notice about anyone — not the earlier guest's, and not
			// this guest's own, which is the same frame shape and would otherwise
			// leave the race open.
			if strings.HasPrefix(f.Content, guestNoticeMark) {
				t.Errorf("a guest-presence notice reached a guest: %q", f.Content)
			}
			if strings.Contains(string(blob), "Wilhelmina") {
				t.Errorf("an earlier guest's NAME reached a guest in a room notice: %s", blob)
			}
		}
	}
	// The filter must be surgical. What the earlier guest SAID is the room's
	// transcript — a guest is given history on purpose, and an unattributed
	// backfill is worse than none — so their line, and their name on it, stay.
	for _, f := range got {
		if f.Type == "msg" && f.Content == "hello from the first visitor" {
			backfilledLine = true
			if !strings.Contains(f.From, "Wilhelmina") {
				t.Errorf("the backfilled message lost its author: %+v", f)
			}
		}
	}
	if !backfilledLine {
		t.Errorf("the filter took the room's conversation with it — frames: %v", typesOfFrames(got))
	}

	// The notices are still in the room for MEMBERS. Only the guest leg filters.
	if !saw("Wilhelmina joined") {
		t.Error("the arrival notice was removed from the room itself, not just from the guest leg")
	}
}

// TestGuestSystemNoticesCarryNoAuthor pins the smaller half of the same leak:
// the very first frame a guest received after `welcome` was the notice of their
// OWN arrival, stamped with the host's display name in `from` — so a stranger
// learned who they were talking to before that person had said a word. The page
// renders room notices authorless either way, so the name was pure leakage.
func TestGuestSystemNoticesCarryNoAuthor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked service test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)

	if err := s.SetProfile(Profile{Name: "Dr. Hana Ozturk"}); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	channelID, token := meetingWithGuestLink(t, s, 0)

	r, c := guestPipe(t, s)
	sendGuestFrame(t, c, guestFrame{Type: "hello", Token: token, Name: "Stranger"})
	got := make(chan guestFrame, 32)
	go func() {
		defer close(got)
		for {
			_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			var f guestFrame
			if json.Unmarshal([]byte(strings.TrimSuffix(line, "\n")), &f) == nil {
				got <- f
			}
		}
	}()
	if f := <-got; f.Type != "welcome" {
		t.Fatalf("guest never got in: %q", f.Type)
	}
	// A room notice that is NOT about a guest coming or going (those never reach
	// a guest at all now), plus a real message, so both branches are checked.
	s.sendSystem(channelID, "created this channel")
	if _, err := s.SendMessage(channelID, "and this one is really from me", "", ""); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	sawSys, sawMsg := false, false
	deadline := time.After(10 * time.Second)
	for !sawSys || !sawMsg {
		select {
		case f, ok := <-got:
			if !ok {
				t.Fatal("the guest's socket closed early")
			}
			switch f.Type {
			case "sys":
				sawSys = true
				if f.From != "" {
					t.Errorf("a room notice named %q as its author — a guest learns who the host is from a frame the page draws authorless", f.From)
				}
				if f.Content == "" {
					t.Error("the room notice lost its text along with its author")
				}
			case "msg":
				sawMsg = true
				// A real message still needs its author, or the guest cannot tell
				// who is speaking.
				if f.From == "" {
					t.Errorf("a spoken message arrived with no author: %+v", f)
				}
			}
		case <-deadline:
			t.Fatalf("never saw both a sys and a msg frame (sys=%v msg=%v)", sawSys, sawMsg)
		}
	}
}

// TestGuestEvictedFromTheCallRoster drives the ONE control that removes a guest
// who is already inside: the ✕ on their tile in the call roster. That button
// passes the voice PEER ID ("guest:<session>") — the id the mesh gave them and
// the only one a tile knows — while the knock list passes the FINGERPRINT. Only
// the fingerprint was matched, so the host got a "Removed X" toast and X stayed
// in the room, in the call, able to keep talking and reading.
func TestGuestEvictedFromTheCallRoster(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked service test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)

	channelID, token := meetingWithGuestLink(t, s, 0)
	r, c := guestPipe(t, s)
	sendGuestFrame(t, c, guestFrame{Type: "hello", Token: token, Name: "Dana"})

	// Drain in the background: the room keeps talking (the arrival notice lands
	// as a `sys` frame) and net.Pipe is unbuffered, so a test that stops reading
	// wedges the very write it is waiting for.
	got := make(chan guestFrame, 32)
	go func() {
		defer close(got)
		for {
			_ = c.SetReadDeadline(time.Now().Add(20 * time.Second))
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			var f guestFrame
			if json.Unmarshal([]byte(strings.TrimSuffix(line, "\n")), &f) == nil {
				got <- f
			}
		}
	}()
	if f := <-got; f.Type != "welcome" {
		t.Fatalf("guest never got in: first frame was %q", f.Type)
	}

	// The identifier the roster's Remove button really sends.
	s.guestMu.Lock()
	peerID := ""
	if list := s.guestSessions[channelID]; len(list) > 0 {
		peerID = guestPeerID(list[0].id)
	}
	s.guestMu.Unlock()
	if peerID == "" {
		t.Fatal("no guest session registered")
	}

	if err := s.PublishCallControl(channelID, "disconnect", peerID, ""); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	deadline := time.After(15 * time.Second)
	for {
		select {
		case f, ok := <-got:
			if !ok {
				t.Fatal("the guest's socket closed without an end frame — a removed guest is left staring at a room that has silently stopped working")
			}
			if f.Type != "end" {
				continue
			}
			if !strings.Contains(strings.ToLower(f.Reason), "removed") {
				t.Errorf("removal reason %q does not say they were removed", f.Reason)
			}
			return
		case <-deadline:
			t.Fatal("removing a guest by their voice peer id did nothing: no end frame, the guest is still in the room")
		}
	}
}

// TestGuestLinkExpiryEndsALiveVisit pins the other half of a link's lifetime.
// The expiry was enforced only at the door, and the sweep that deletes an
// expired meeting runs at startup — so a guest who walked in a minute before
// the deadline stayed in a room whose link the host had been told would stop
// working, for as long as they cared to sit there.
func TestGuestLinkExpiryEndsALiveVisit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked service test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)

	t.Setenv("CONCORD_GUEST_BASE", "http://127.0.0.1:1/")
	g, _, err := s.StartMeeting()
	if err != nil {
		t.Fatalf("StartMeeting: %v", err)
	}
	url, err := s.CreateGuestLink(g.ID, 24)
	if err != nil {
		t.Fatalf("CreateGuestLink: %v", err)
	}
	token := url[strings.Index(url, "&t=")+3:]

	r, c := guestPipe(t, s)
	sendGuestFrame(t, c, guestFrame{Type: "hello", Token: token, Name: "Lurker"})
	got := make(chan guestFrame, 32)
	go func() {
		defer close(got)
		for {
			_ = c.SetReadDeadline(time.Now().Add(4 * guestExpiryPoll))
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			var f guestFrame
			if json.Unmarshal([]byte(strings.TrimSuffix(line, "\n")), &f) == nil {
				got <- f
			}
		}
	}()
	if f := <-got; f.Type != "welcome" {
		t.Fatalf("guest never got in: first frame was %q", f.Type)
	}

	// The host cuts the lifetime short while the guest is sitting there — the
	// same call CreateGuestLink makes when a shorter chip is picked.
	s.setMeetingExpiry(g.ID, time.Now().Add(-time.Second))

	deadline := time.After(3 * guestExpiryPoll)
	for {
		select {
		case f, ok := <-got:
			if !ok {
				t.Fatal("the guest's socket closed with no explanation")
			}
			if f.Type != "end" {
				continue
			}
			if !strings.Contains(strings.ToLower(f.Reason), "expired") {
				t.Errorf("end reason %q does not mention the expiry", f.Reason)
			}
			return
		case <-deadline:
			t.Fatal("an expired meeting link left the guest already inside reading and posting to the room")
		}
	}
}

// TestGuestDoorLockIsALease pins why the lock is a lease and not a flag: the
// front end re-announces it every few seconds, so a host that crashes or
// reloads must stop holding the door shut rather than leaving one nobody alive
// can open.
func TestGuestDoorLockIsALease(t *testing.T) {
	s := &Service{guestDoor: map[string]time.Time{}}
	s.noteGuestDoor("ch", true)
	if !s.guestDoorLocked("ch") {
		t.Fatal("door not locked after a lock announcement")
	}
	s.guestMu.Lock()
	s.guestDoor["ch"] = time.Now().Add(-time.Second) // last announcement went stale
	s.guestMu.Unlock()
	if s.guestDoorLocked("ch") {
		t.Fatal("an un-renewed lock still holds the door shut")
	}
	s.noteGuestDoor("ch", true)
	s.noteGuestDoor("ch", false)
	if s.guestDoorLocked("ch") {
		t.Fatal("unlock did not open the door")
	}
}

// TestMeetingLifetimeChoices pins the menu: only the offered lifetimes are
// accepted, and zero means "leave it alone" (what a caller from before
// lifetimes existed sends).
func TestMeetingLifetimeChoices(t *testing.T) {
	for _, h := range meetingLifetimes {
		if d, ok := meetingLifetime(h); !ok || d != time.Duration(h)*time.Hour {
			t.Errorf("offered lifetime %dh rejected", h)
		}
	}
	for _, h := range []int{-1, 0, 2, 25, 24 * 365} {
		if _, ok := meetingLifetime(h); ok {
			t.Errorf("%dh is not on the menu but was accepted", h)
		}
	}
}

// TestGuestLinkOutlivesRestart is the point of a multi-day link: the token, the
// meeting and its chosen lifetime all have to survive the host quitting and
// reopening Concord. Before lifetimes were persisted the startup sweep deleted
// the room after 24h and the token only ever lived in memory, so a "7-day link"
// died the first time the app restarted.
func TestGuestLinkOutlivesRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked service test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir := t.TempDir()
	s := startServiceInDir(t, ctx, dir)
	t.Setenv("CONCORD_GUEST_BASE", "http://127.0.0.1:1/")
	g, _, err := s.StartMeeting()
	if err != nil {
		t.Fatalf("StartMeeting: %v", err)
	}
	link, err := s.CreateGuestLink(g.ID, 24*7)
	if err != nil {
		t.Fatalf("CreateGuestLink: %v", err)
	}
	week := time.Now().Add(24 * 7 * time.Hour)
	if got := time.UnixMilli(s.MeetingExpiry(g.ID)); got.Sub(week) > time.Minute || week.Sub(got) > time.Minute {
		t.Fatalf("expiry %v, want ~%v", got, week)
	}
	// Re-minting must not invalidate the link already sent out.
	again, err := s.CreateGuestLink(g.ID, 24*30)
	if err != nil {
		t.Fatalf("re-mint: %v", err)
	}
	if again != link {
		t.Errorf("changing the lifetime handed back a different link:\n old %s\n new %s", link, again)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2 := startServiceInDir(t, ctx, dir)
	found := false
	for _, gg := range s2.Guilds() {
		if gg.ID == g.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("the meeting was swept despite a 30-day lifetime")
	}
	// The chosen lifetime came back too — without it the next launch would judge
	// the room by the 24h default and delete it a day in.
	month := time.Now().Add(24 * 30 * time.Hour)
	if got := time.UnixMilli(s2.MeetingExpiry(g.ID)); got.Sub(month) > time.Minute || month.Sub(got) > time.Minute {
		t.Fatalf("restored expiry %v, want ~%v", got, month)
	}
	// The token from the first run still opens the door.
	channelID := g.Channels[0].ID
	i := strings.Index(link, "&t=")
	r, c := guestPipe(t, s2)
	sendGuestFrame(t, c, guestFrame{Type: "hello", Token: link[i+3:], Name: "Returning"})
	frames := readGuestFrames(t, r, c, "welcome", "end")
	if last := frames[len(frames)-1]; last.Type != "welcome" {
		t.Fatalf("a link minted before the restart got %q (%s), want welcome", last.Type, last.Reason)
	}
	if channelID == "" {
		t.Fatal("meeting has no channel")
	}
}

// TestMeetingExpiryDefaultAndChosen pins the predicate the startup sweep uses.
// A meeting that never chose a lifetime keeps the historic fixed TTL (so a
// 25-hour-old room is still swept); one that did outlives it.
func TestMeetingExpiryDefaultAndChosen(t *testing.T) {
	created := time.Now().Add(-25 * time.Hour)
	s := &Service{
		guilds: map[string]*domain.Guild{
			"old":     {ID: "old", Kind: "meeting", Created: created},
			"kept":    {ID: "kept", Kind: "meeting", Created: created},
			"notroom": {ID: "notroom", Created: created},
		},
		meetingLife: map[string]time.Time{"kept": created.Add(24 * 30 * time.Hour)},
	}
	if at := s.meetingExpiry("old"); !time.Now().After(at) {
		t.Errorf("a 25h-old meeting with no chosen lifetime expires at %v — the sweep would keep it", at)
	}
	if at := s.meetingExpiry("kept"); time.Now().After(at) {
		t.Errorf("a 30-day meeting expires at %v — the sweep would delete it", at)
	}
	if at := s.meetingExpiry("notroom"); !at.IsZero() {
		t.Errorf("a normal guild reported an expiry (%v); only meetings are disposable", at)
	}
}
