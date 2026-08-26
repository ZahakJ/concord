package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// TestChronicleAttachSyncAndFetch is the chronicle acceptance test, and it is
// shaped exactly like the attachment one because the availability property is
// the same: A (the owner) attaches an archive, B learns it exists from a
// manifest that weighs a few kilobytes, B reads a page by fetching a chunk from
// A, and then C — who joins later — reads the same page from B with A gone.
// Along the way: a member who is not the owner cannot attach one, and a manifest
// signed by somebody who is not the owner is dropped on the sync path.
func TestChronicleAttachSyncAndFetch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)

	g, err := a.CreateGuild("g")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := a.InviteCode(g.ID)
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B JoinViaInvite: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, a, b)

	// A real archive: two channels, five hundred messages, several pages.
	raw, chunks := sampleChronicle(t, a, g.ID)
	if len(chunks) < 3 {
		t.Fatalf("the sample archive is %d pages; the point of this test is that it is several", len(chunks))
	}
	if len(raw) > maxChronicleManifestBytes {
		t.Fatalf("the index is %d bytes, over its own cap", len(raw))
	}

	// A member who is not the owner cannot attach one, whoever signed it.
	if err := b.AttachChronicle(g.ID, raw, chunks); err == nil {
		t.Fatal("a non-owner attached an archive")
	}
	if info, _ := b.ChronicleInfo(g.ID); info.ID != "" {
		t.Fatal("a refused attach left an archive behind at B")
	}

	if err := a.AttachChronicle(g.ID, raw, chunks); err != nil {
		t.Fatalf("A AttachChronicle: %v", err)
	}
	own, err := a.ChronicleInfo(g.ID)
	if err != nil || own.Messages != 500 || !own.Pinned || own.ChunksCached != own.Chunks {
		t.Fatalf("the importing device does not hold its own archive whole: %+v (err %v)", own, err)
	}

	// B learns the archive exists from the manifest alone — over the guild-meta
	// topic, the same lane a nickname or a story travels.
	waitUntil(t, 25*time.Second, func() bool {
		info, _ := b.ChronicleInfo(g.ID)
		return info.ID == own.ID
	}, "B never learned the archive")

	atB, err := b.ChronicleInfo(g.ID)
	if err != nil {
		t.Fatalf("B ChronicleInfo: %v", err)
	}
	if atB.Messages != 500 || len(atB.Channels) != 2 || atB.Source != "the old forum" {
		t.Fatalf("B's view of the archive is wrong: %+v", atB)
	}
	if atB.ChunksCached != 0 {
		t.Fatalf("B holds %d pages already; the index must travel without the archive", atB.ChunksCached)
	}

	// THE METERED RULE. A page is a megabyte nobody asked for by name, so on a
	// billed connection it is refused until the reader says otherwise — and the
	// refusal is its own sentinel, not a generic failure.
	b.SetMetered(true)
	if _, err := b.ChronicleMessages(g.ID, "src-general", 0, 20, false); !errors.Is(err, ErrChronicleMetered) {
		t.Fatalf("a metered fetch returned %v, want ErrChronicleMetered", err)
	}
	if info, _ := b.ChronicleInfo(g.ID); info.ChunksCached != 0 {
		t.Fatal("the refused fetch still pulled a page")
	}

	// With the override, the same call fetches from A over the chronicle stream.
	page, err := b.ChronicleMessages(g.ID, "src-general", 0, 20, true)
	if err != nil {
		t.Fatalf("B ChronicleMessages (override): %v", err)
	}
	b.SetMetered(false)
	if len(page) != 20 {
		t.Fatalf("B read %d messages, want 20", len(page))
	}
	if page[len(page)-1].ID != "m0498" || page[len(page)-1].Author == "" {
		t.Fatalf("B's newest archived message is %+v", page[len(page)-1])
	}
	if info, _ := b.ChronicleInfo(g.ID); info.ChunksCached == 0 {
		t.Fatal("B read a page without caching it; availability would never spread")
	}

	// The second channel too, so that B holds a page of each before A goes: what
	// C reads afterwards has to come from somewhere.
	if photos, err := b.ChronicleMessages(g.ID, "src-photos", 0, 5, false); err != nil || len(photos) != 5 {
		t.Fatalf("B read %d messages of the second channel (err %v)", len(photos), err)
	}

	// A manifest signed by somebody who is not the owner, arriving the way a
	// hostile member would actually send one — inside a sync payload, where the
	// responder attests nothing and only the record's own signature counts.
	forged, _ := sampleChronicle(t, b, g.ID)
	b.applySyncedChronicles(g.ID, []json.RawMessage{forged})
	if info, _ := b.ChronicleInfo(g.ID); info.ID != own.ID {
		t.Fatal("a non-owner's archive was adopted through history sync")
	}
	rows, _ := b.store.ChronicleManifests(g.ID)
	if len(rows) != 1 {
		t.Fatalf("B holds %d manifests; the forged one was stored", len(rows))
	}

	// C joins, A leaves; C must still be able to read the archive — index and
	// pages both, entirely from B.
	c := startService(t, ctx)
	if _, err := c.JoinViaInvite(code); err != nil {
		t.Fatalf("C JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 3, a, b, c)
	// Tests run without mDNS/DHT, so C only auto-dialed A (the invite path).
	// Connect C↔B directly — in production discovery does this.
	if err := c.host.Connect(ctx, b.host.AddrInfo()); err != nil {
		t.Fatalf("connect C to B: %v", err)
	}
	_ = a.Close()

	bID := b.host.AddrInfo().ID
	waitUntil(t, 30*time.Second, func() bool {
		_ = c.syncGuildFromPeer(g.ID, bID)
		info, _ := c.ChronicleInfo(g.ID)
		return info.ID == own.ID
	}, "C never learned the archive from B with A offline")

	got, err := c.ChronicleMessages(g.ID, "src-general", 0, 20, false)
	if err != nil {
		t.Fatalf("C ChronicleMessages (from B, A offline): %v", err)
	}
	if len(got) != len(page) {
		t.Fatalf("C read %d messages where B read %d", len(got), len(page))
	}
	for i := range got {
		if got[i].ID != page[i].ID || got[i].Content != page[i].Content || got[i].Author != page[i].Author {
			t.Fatalf("C's copy of message %d differs from B's:\n%+v\n%+v", i, got[i], page[i])
		}
	}
	// The other channel comes from a different page, still served by B.
	if photos, err := c.ChronicleMessages(g.ID, "src-photos", 0, 5, false); err != nil || len(photos) != 5 {
		t.Fatalf("C read %d messages of the second channel (err %v)", len(photos), err)
	}
}
