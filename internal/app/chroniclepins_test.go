package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/store"
)

// A member who reads the archive passes its pictures on while the importer is
// offline, up to the guild's bound; past it a picture is still shown but not
// passed on; and when the archive is removed, the pictures are no longer
// served by anyone who held them.
func TestArchivePicturesReadByAMemberArePassedOn(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx) // the owner, who imports the archive
	b := startService(t, ctx) // a member who reads it
	g, err := a.CreateGuild("g")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := a.InviteCode(g.ID)
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B JoinViaInvite: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, a, b)
	liveCh := theGuild(t, a).Channels[0].ID

	// Three pictures, sealed and pinned on A as an import seals them.
	type pic struct{ id, keys string }
	var pics []pic
	var msgs []chronicleMessage
	for i := 0; i < 3; i++ {
		id, keys, err := a.sealBlob([]byte(fmt.Sprintf("archived picture %d", i)))
		if err != nil {
			t.Fatal(err)
		}
		if err := a.store.PinImportedBlob(g.ID, id); err != nil {
			t.Fatal(err)
		}
		pics = append(pics, pic{id, keys})
		msgs = append(msgs, chronicleMessage{
			ID: fmt.Sprintf("p%d", i), Nano: int64(1_300_000_000_000_000_000 + i*1_000_000_000),
			Content: "photo", Attach: []string{fmt.Sprintf("concord://attach/v1/%s/%s/png/0x0", id, keys)},
		})
	}
	raw, chunks, err := a.buildChronicle(g.ID, "the old forum", "", []chronicleChannel{{ID: "src-pics", Name: "pics", Type: "text"}},
		[]chronicleAuthor{{Name: "ada"}}, map[string][]chronicleMessage{"src-pics": msgs}, 3, 100)
	if err != nil {
		t.Fatalf("buildChronicle: %v", err)
	}
	if err := a.AttachChronicle(g.ID, raw, chunks); err != nil {
		t.Fatalf("AttachChronicle: %v", err)
	}
	own, _ := a.ChronicleInfo(g.ID)
	waitUntil(t, 25*time.Second, func() bool {
		info, _ := b.ChronicleInfo(g.ID)
		return info.ID == own.ID
	}, "B never learned the archive")

	// B already holds pins of another archive of the guild, one short of the
	// bound: the first picture fits, the other two do not.
	for i := 0; i < store.ChroniclePinsPerGuild-1; i++ {
		id := fmt.Sprintf("%064x", i)
		if err := b.store.SaveAttachment(id, []byte("x")); err != nil {
			t.Fatal(err)
		}
		if ok, err := b.store.PinChronicleBlob(g.ID, "an-earlier-archive", id); err != nil || !ok {
			t.Fatalf("filler pin %d: %v %v", i, ok, err)
		}
	}

	page, err := b.ChronicleMessages(g.ID, "src-pics", 0, 10, true)
	if err != nil || len(page) != 3 {
		t.Fatalf("B read %d archive messages (err %v)", len(page), err)
	}
	for i, p := range pics {
		if _, err := b.FetchAttachment(liveCh, p.id, p.keys, "png"); err != nil {
			t.Fatalf("B could not show picture %d: %v", i, err)
		}
	}
	for i, p := range pics {
		pinned, held, err := b.store.AttachmentPinned(p.id)
		if err != nil || !held {
			t.Fatalf("B does not hold picture %d: %v", i, err)
		}
		if want := i == 0; pinned != want {
			t.Fatalf("B: picture %d pinned %v, want %v", i, pinned, want)
		}
	}
	if n, _, _ := b.store.ChroniclePinned(g.ID); n != store.ChroniclePinsPerGuild {
		t.Fatalf("B holds %d pins for the guild, want the bound %d", n, store.ChroniclePinsPerGuild)
	}

	// C joins and reads with A offline: the picture B pinned comes from B,
	// the ones past B's bound come from nobody.
	c := startService(t, ctx)
	if _, err := c.JoinViaInvite(code); err != nil {
		t.Fatalf("C JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 3, a, b, c)
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
	if page, err := c.ChronicleMessages(g.ID, "src-pics", 0, 10, false); err != nil || len(page) != 3 {
		t.Fatalf("C read %d archive messages (err %v)", len(page), err)
	}
	var fetchErr error
	waitUntil(t, 20*time.Second, func() bool {
		_, fetchErr = c.FetchAttachment(liveCh, pics[0].id, pics[0].keys, "png")
		return fetchErr == nil
	}, "C could not get the picture B read, with the importer offline")
	if _, err := c.FetchAttachment(liveCh, pics[1].id, pics[1].keys, "png"); err == nil {
		t.Fatal("B passed on a picture past its bound")
	}

	// The archive is removed: each device that held its pictures lets them go.
	served := func(s *Service, id string) bool { return serves(t, s, id) }
	if !served(b, pics[0].id) || !served(c, pics[0].id) {
		t.Fatal("before the removal, B and C do not both pass on the picture")
	}
	for _, s := range []*Service{b, c} {
		s.releaseChronicleBlobs(g.ID, own.ID)
	}
	for name, s := range map[string]*Service{"B": b, "C": c} {
		if _, held, _ := s.store.AttachmentPinned(pics[0].id); held {
			t.Errorf("%s still holds the removed archive's picture", name)
		}
		if served(s, pics[0].id) {
			t.Errorf("%s still serves the removed archive's picture", name)
		}
		if !s.store.BlobBanned(pics[0].id) {
			t.Errorf("%s did not ban the removed archive's picture", name)
		}
	}
	if n, _, _ := b.store.ChroniclePinned(g.ID); n != store.ChroniclePinsPerGuild-1 {
		t.Fatalf("B holds %d pins after the removal, want the other archive's %d", n, store.ChroniclePinsPerGuild-1)
	}
}

// When a newer archive of the guild arrives, the pictures pinned for the old
// one go back to the cache and are no longer passed on from here.
func TestAReplacedArchiveStopsPassingItsPicturesOn(t *testing.T) {
	owner := mustID(t)
	s := chronicleTestService(t, owner, owner)
	first, _ := sampleChronicle(t, s, "g1")
	if !s.ingestChronicle("g1", first) {
		t.Fatal("the first archive was refused")
	}
	_, firstID, _ := s.guildChronicle("g1")
	pic := fmt.Sprintf("%064x", 7)
	if err := s.store.SaveAttachment(pic, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.store.PinChronicleBlob("g1", firstID, pic); err != nil || !ok {
		t.Fatalf("pin: %v %v", ok, err)
	}
	if !s.store.BlobServable(pic) {
		t.Fatal("the pinned picture is not served")
	}
	time.Sleep(1100 * time.Millisecond) // a manifest's stored time is in seconds
	second, _ := sampleChronicle(t, s, "g1")
	if !s.ingestChronicle("g1", second) {
		t.Fatal("the second archive was refused")
	}
	if _, cur, _ := s.guildChronicle("g1"); cur == firstID {
		t.Fatal("the guild still shows the first archive")
	}
	if pinned, held, _ := s.store.AttachmentPinned(pic); pinned || !held {
		t.Fatalf("after the replacement: pinned %v, held %v; want unpinned and held", pinned, held)
	}
	if s.store.BlobServable(pic) {
		t.Fatal("the replaced archive's picture is still served")
	}
}
