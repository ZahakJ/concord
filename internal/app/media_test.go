package app

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/mediatok"
)

// The gallery at a receiving member: the picture A sent is an item of B's
// gallery as soon as B holds the message, and the blob behind it is fetched
// only as the metered rule allows. On a billed connection a tile scrolling by
// fetches nothing and says so; a tap (allowMetered) fetches; after that the
// blob is local. On an unmetered connection a visible tile fetches at once.
func TestMediaGalleryAtReceiver(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)
	b := startService(t, ctx)
	g, err := a.CreateGuild("g")
	if err != nil {
		t.Fatal(err)
	}
	channel := g.Channels[0].ID
	code, _ := a.InviteCode(g.ID)
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMembers(t, 20*time.Second, 2, a, b)

	send := func(text string) string {
		t.Helper()
		dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte(text))
		msg, err := a.SendAttachment(channel, dataURL, 40, 30, "", false, "", "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		var blob string
		waitUntil(t, 20*time.Second, func() bool {
			items, _, err := b.MediaPage(channel, mediatok.TabVisual, "", 50)
			if err != nil {
				return false
			}
			for _, it := range items {
				if it.MessageID == msg.ID {
					blob = it.Item.BlobID
					return it.Item.W == 40 && it.Item.H == 30 && it.Item.Keys != ""
				}
			}
			return false
		}, "B's gallery never listed A's picture")
		return blob
	}

	first := send("first picture")
	if b.store.HasAttachment(first) {
		t.Fatal("B holds the blob before anything asked for it")
	}

	b.SetMetered(true)
	if got, err := b.EnsureMedia(channel, first, false); err != nil || got != MediaMetered {
		t.Fatalf("metered, not asked: %q %v, want %q", got, err, MediaMetered)
	}
	if b.store.HasAttachment(first) {
		t.Fatal("a metered tile fetched the blob anyway")
	}
	if got, err := b.EnsureMedia(channel, first, true); err != nil || got != MediaFetched {
		t.Fatalf("metered, tapped: %q %v, want %q", got, err, MediaFetched)
	}
	if got, err := b.EnsureMedia(channel, first, false); err != nil || got != MediaLocal {
		t.Fatalf("after the fetch: %q %v, want %q", got, err, MediaLocal)
	}

	b.SetMetered(false)
	second := send("second picture")
	if got, err := b.EnsureMedia(channel, second, false); err != nil || got != MediaFetched {
		t.Fatalf("unmetered tile: %q %v, want %q", got, err, MediaFetched)
	}
	if !b.store.HasAttachment(second) {
		t.Fatal("the unmetered fetch left nothing on B")
	}

	if _, err := b.EnsureMedia(channel, "not-a-blob", true); err == nil {
		t.Fatal("a malformed blob id was accepted")
	}
	if _, _, err := b.MediaPage(channel, mediatok.Tab(9), "", 10); err == nil {
		t.Fatal("an unknown tab was accepted")
	}
}
