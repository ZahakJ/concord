package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// A copy that CannotRepairHere answers no catch-up: a member that asks it
// directly gets the empty "declined" answer, whatever the copy holds. Its
// local creators refuse with the sentence before storing anything. The same
// request is answered once the copy is healthy.
func TestBarredCopyDeclinesSyncAndCreatesNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	u, p, gid, ch := guardedPair(t, ctx, boot)
	groupID := rhGroup(u, gid)
	ask := func() []byte {
		t.Helper()
		ep, err := p.mls.Epoch(ctx, groupID)
		if err != nil {
			t.Fatal(err)
		}
		req, _ := json.Marshal(syncRequest{GuildID: gid, Epoch: ep})
		raw, err := u.handleSyncRequest(ctx, p.host.PeerID(), req)
		if err != nil {
			t.Fatalf("handleSyncRequest: %v", err)
		}
		return raw
	}
	if len(ask()) == 0 {
		t.Fatal("setup: a healthy copy declined a member's catch-up")
	}

	latch(t, u, gid)
	start := time.Now().Add(24 * time.Hour).Unix()
	refusals := map[string]error{}
	_, refusals["event"] = u.CreateEvent(gid, "dead-copy event", "details", start, start+3600, "", "", "", 0)
	refusals["nickname"] = u.SetNickname(gid, "dead-copy nick")
	refusals["channel links"] = u.SetChannelLinks(gid, ch, []string{"https://example.org/"})
	refusals["story"] = u.PostStory([]string{gid}, "preset:aurora", "dead-copy caption")
	for what, err := range refusals {
		if !errors.Is(err, ErrBehindOnlyCommitter) {
			t.Errorf("%s on a barred copy: err=%v, want ErrBehindOnlyCommitter", what, err)
		}
	}
	if evs, _ := u.Events(gid); len(evs) != 0 {
		t.Errorf("the barred copy stored an event: %v", evs)
	}
	if raw := ask(); len(raw) != 0 {
		t.Errorf("a barred copy answered a member's catch-up with %d bytes", len(raw))
	}

	u.clearBehind(gid)
	if len(ask()) == 0 {
		t.Fatal("a healthy copy declined a member's catch-up")
	}
	if _, err := u.CreateEvent(gid, "healthy event", "", start, start+3600, "", "", "", 0); err != nil {
		t.Errorf("CreateEvent on a healthy copy: %v", err)
	}
}
