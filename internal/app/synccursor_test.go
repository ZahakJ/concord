package app

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// guildOf starts n services and puts them all in one guild, returning the
// services (the first is the owner), the guild ID and its first channel.
func guildOf(t *testing.T, ctx context.Context, n int) ([]*Service, string, string) {
	t.Helper()
	svcs := []*Service{startService(t, ctx)}
	g, err := svcs[0].CreateGuild("cursor")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, err := svcs[0].InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	for i := 1; i < n; i++ {
		s := startService(t, ctx)
		if _, err := s.JoinViaInvite(code); err != nil {
			t.Fatalf("JoinViaInvite: %v", err)
		}
		svcs = append(svcs, s)
		waitMembers(t, 30*time.Second, i+1, svcs...)
	}
	return svcs, g.ID, g.Channels[0].ID
}

// missing lists which of the rows the service does not hold.
func missing(t *testing.T, s *Service, ids []string) []string {
	t.Helper()
	var out []string
	for _, id := range ids {
		_, ok, err := s.store.MessageByID(id)
		if err != nil {
			t.Fatalf("MessageByID: %v", err)
		}
		if !ok {
			out = append(out, id)
		}
	}
	return out
}

func mustSync(t *testing.T, s *Service, guildID string, from *Service) {
	t.Helper()
	if err := s.syncGuildFromPeer(guildID, from.host.PeerID()); err != nil {
		t.Fatalf("sync: %v", err)
	}
}

// TestSyncBacklogSurvivesALiveArrival: a member back from hours offline hears
// one live message — or sends one of its own — before its catch-up runs. The
// cursor was the newest stamp it held, so that one message moved it to now,
// and everything written while it was away fell below the cursor and was
// never asked for again.
func TestSyncBacklogSurvivesALiveArrival(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svcs, guildID, channel := guildOf(t, ctx, 2)
	a, b := svcs[0], svcs[1]
	mustSync(t, b, guildID, a)

	away := time.Now().Add(-2 * time.Hour)
	var backlog []string
	for i := 0; i < 5; i++ {
		backlog = append(backlog, plantRow(t, a, channel, "", fmt.Sprintf("while away %d", i), away.Add(time.Duration(i)*time.Minute)).ID)
	}
	// The live arrival: on both sides, as gossip would leave it.
	live := plantRow(t, a, channel, "", "live", time.Now())
	if _, err := b.store.SaveMessage(live); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	mustSync(t, b, guildID, a)
	if m := missing(t, b, backlog); len(m) > 0 {
		t.Fatalf("after the catch-up B is missing %d of %d rows written while it was away", len(m), len(backlog))
	}
}

// TestSyncDoesNotTakeOnePeersWordForAnothers: B and A were both offline while
// C wrote. A comes back and posts; B catches up from A, which is complete as
// far as A knows. Then C reconnects. Asked from a cursor B moved to A's post,
// C served nothing older, and what C wrote while the other two were away was
// gone for good.
func TestSyncDoesNotTakeOnePeersWordForAnothers(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svcs, guildID, channel := guildOf(t, ctx, 3)
	a, b, c := svcs[0], svcs[1], svcs[2]
	if err := b.host.Connect(ctx, c.host.AddrInfo()); err != nil {
		t.Fatalf("connect B to C: %v", err)
	}
	mustSync(t, b, guildID, a)
	mustSync(t, b, guildID, c)

	fromC := plantRow(t, c, channel, "", "written while A and B were away", time.Now().Add(-time.Hour)).ID
	fromA := plantRow(t, a, channel, "", "A is back", time.Now()).ID

	mustSync(t, b, guildID, a)
	if m := missing(t, b, []string{fromA}); len(m) > 0 {
		t.Fatal("B did not get A's post from A")
	}
	mustSync(t, b, guildID, c)
	if m := missing(t, b, []string{fromC}); len(m) > 0 {
		t.Fatal("B never got what C wrote while A and B were away")
	}
}

// TestSyncTruncationLosesNothing: more rows past the cursor than one answer
// carries (syncMessagesPerChannel), and a reaction on an old row stamped with
// a fresh update time. The old row sorts first, its fresh stamp became the
// cursor, and the rows the answer had no room for were below it.
func TestSyncTruncationLosesNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svcs, guildID, channel := guildOf(t, ctx, 2)
	a, b := svcs[0], svcs[1]
	// A row both already hold, as live delivery left it.
	old := plantRow(t, a, channel, "", "an old row", time.Now().Add(-3*time.Hour))
	if _, err := b.store.SaveMessage(old); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	mustSync(t, b, guildID, a)

	// Every new row is sent after anything B holds, so the old cursor would
	// ask for all of them; the reaction lands after all of them.
	start := time.Now()
	var rows []string
	n := syncMessagesPerChannel + 50
	for i := 0; i < n; i++ {
		rows = append(rows, plantRow(t, a, channel, "", fmt.Sprintf("row %d", i), start.Add(time.Duration(i)*time.Microsecond)).ID)
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := a.store.ToggleReaction(old.ID, a.Fingerprint(), "👍"); err != nil {
		t.Fatalf("ToggleReaction: %v", err)
	}

	// One catch-up, and then whatever the next beat would ask.
	mustSync(t, b, guildID, a)
	mustSync(t, b, guildID, a)
	if m := missing(t, b, rows); len(m) > 0 {
		t.Fatalf("B is missing %d of %d rows after the catch-up", len(m), n)
	}
}

// askDirect builds b's request to a the way a sync would (or, with legacy, the
// way a peer on the previous release would), has a answer it in-process, and
// returns what b would read out of the payload.
func askDirect(t *testing.T, ctx context.Context, b, a *Service, guildID string, legacy bool) syncPayload {
	t.Helper()
	b.mu.RLock()
	guild := b.guilds[guildID].Clone()
	b.mu.RUnlock()
	epoch, err := b.mls.Epoch(ctx, guild.GroupID)
	if err != nil {
		t.Fatalf("epoch: %v", err)
	}
	cur := b.syncCursorFor(guild, a.host.PeerID())
	req := syncRequest{GuildID: guildID, Epoch: epoch, Since: cur.since, Marks: cur.marks}
	if legacy {
		req.Marks = nil
	}
	raw, _ := json.Marshal(req)
	respBytes, err := a.handleSyncRequest(ctx, b.host.PeerID(), raw)
	if err != nil {
		t.Fatalf("handleSyncRequest: %v", err)
	}
	var resp syncResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil || len(resp.Payload) == 0 {
		t.Fatalf("no payload in the answer (%v)", err)
	}
	dec, err := b.mls.Decrypt(ctx, guild.GroupID, resp.Payload)
	if err != nil {
		t.Fatalf("decrypt payload: %v", err)
	}
	var p syncPayload
	if err := json.Unmarshal(dec.Plaintext, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return p
}

// TestMarkedSyncDoesNotSendBackWhatWeHold: asking a peer for everything it
// changed since we last talked would re-send every live message the two of us
// both heard in between. The request says which of those rows we hold, and
// the answer skips them — while still counting them as reached. A peer on the
// previous release, asking the old way, is still answered the old way.
func TestMarkedSyncDoesNotSendBackWhatWeHold(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svcs, guildID, channel := guildOf(t, ctx, 2)
	a, b := svcs[0], svcs[1]
	mustSync(t, b, guildID, a)

	var both []string
	for i := 0; i < 30; i++ {
		m := plantRow(t, a, channel, "", fmt.Sprintf("heard live %d", i), time.Now())
		if _, err := b.store.SaveMessage(m); err != nil {
			t.Fatalf("SaveMessage: %v", err)
		}
		both = append(both, m.ID)
	}
	onlyA := plantRow(t, a, channel, "", "missed", time.Now()).ID

	p := askDirect(t, ctx, b, a, guildID, false)
	served := map[string]bool{}
	for _, m := range p.Messages[channel] {
		served[m.ID] = true
	}
	for _, id := range both {
		if served[id] {
			t.Fatalf("a row both peers hold was sent back (%d rows in the answer)", len(p.Messages[channel]))
		}
	}
	if !served[onlyA] {
		t.Fatal("the row only A holds was not served")
	}
	head, _ := a.store.ChannelSeqHead(channel)
	if th := p.Through[channel]; th.Seq != head || !th.Done {
		t.Fatalf("answer reached %+v, want done at A's head %d", th, head)
	}

	legacy := askDirect(t, ctx, b, a, guildID, true)
	if legacy.Space != "" || legacy.Through != nil {
		t.Fatal("a requester from before marks was answered with marks")
	}
	got := map[string]bool{}
	for _, m := range legacy.Messages[channel] {
		got[m.ID] = true
	}
	if !got[onlyA] {
		t.Fatal("a requester from before marks was not served the row it lacks")
	}
}

// TestFloorFromAnOlderResponder: a peer on the previous release answers in send
// order with no marks. The floor moves to the newest send time it served above
// our cursor, however the answer was cut, and never to an update stamp.
func TestFloorFromAnOlderResponder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := Start(ctx, Config{DataDir: t.TempDir(), Passphrase: "test-pass", DisableMDNS: true})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()
	base := time.Now().Add(-time.Hour)
	since := base.UnixNano()
	cur := syncCursor{since: map[string]int64{"ch": since}}
	payload := &syncPayload{Messages: map[string][]domain.Message{"ch": {
		// An old row with a fresh reaction: sorts first, says nothing.
		{ID: "old", Sent: base.Add(-time.Hour), Updated: time.Now()},
		{ID: "n1", Sent: base.Add(time.Minute)},
		{ID: "n2", Sent: base.Add(2 * time.Minute)},
	}}}
	s.advanceSyncCursor(s.host.PeerID(), cur, payload, nil)
	if f, _ := s.store.SyncFloor("ch"); f != base.Add(2*time.Minute).UnixNano() {
		t.Fatalf("floor = %d, want the newest send time served (%d)", f, base.Add(2*time.Minute).UnixNano())
	}
	if _, seq, _, _ := s.store.SyncMarks(s.host.PeerID().String(), []string{"ch"}); len(seq) != 0 {
		t.Fatal("an answer without marks left a mark")
	}
	// A channel whose rows could not all be stored does not move.
	s.advanceSyncCursor(s.host.PeerID(), cur, &syncPayload{Messages: map[string][]domain.Message{"ch2": {
		{ID: "x", Sent: time.Now()},
	}}}, map[string]bool{"ch2": true})
	if f, _ := s.store.SyncFloor("ch2"); f != 0 {
		t.Fatal("a channel that failed to store moved its floor")
	}
}
