package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"regexp"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/identity"
	"github.com/ZahakJ/concord/internal/store"
)

// tokenRe reads the blob id and key out of an image or file token.
var tokenRe = regexp.MustCompile(`concord://(?:attach|file)/v[12]/([0-9a-f]{64})/([A-Za-z0-9_-]{75})/`)

func blobAndKey(t *testing.T, content string) (string, string) {
	t.Helper()
	m := tokenRe.FindStringSubmatch(content)
	if m == nil {
		t.Fatalf("no attachment token in %.120q", content)
	}
	return m[1], m[2]
}

func recentHas(items []store.MediaItem, blobID string) bool {
	for _, it := range items {
		if it.Item.BlobID == blobID {
			return true
		}
	}
	return false
}

// The attach menu's Recent strip and the re-send it starts.
//
// C sends A a picture in guild one; A, who also shares guild two with B (who
// is not in guild one), sees it in Recent along with a file A sent there, and
// not the spoilered picture. A re-sends the picture into guild two the way
// the page does: the plaintext through FetchAttachment, then SendAttachment,
// exactly as for a picked file. The message B receives names a new blob under
// a new key; B opens it; B asking for the original blob gets nothing, because
// A serves that one only to guild one's members, and C gets nothing for the
// new one. After A leaves guild one, its items leave Recent and the re-sent
// copy stays.
func TestRecentMediaAndResend(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)
	b := startService(t, ctx)
	c := startService(t, ctx)

	g1, err := a.CreateGuild("one")
	if err != nil {
		t.Fatal(err)
	}
	ch1 := g1.Channels[0].ID
	code1, _ := a.InviteCode(g1.ID)
	if _, err := c.JoinViaInvite(code1); err != nil {
		t.Fatal(err)
	}
	g2, err := a.CreateGuild("two")
	if err != nil {
		t.Fatal(err)
	}
	ch2 := g2.Channels[0].ID
	code2, _ := a.InviteCode(g2.ID)
	if _, err := b.JoinViaInvite(code2); err != nil {
		t.Fatal(err)
	}
	members := func(g string, svcs ...*Service) bool {
		for _, s := range svcs {
			if n, _ := s.MemberCount(g); n != 2 {
				return false
			}
		}
		return true
	}
	waitUntil(t, 20*time.Second, func() bool { return members(g1.ID, a, c) && members(g2.ID, a, b) }, "the two guilds never had their two members each")

	plain := make([]byte, 64<<10)
	if _, err := rand.Read(plain); err != nil {
		t.Fatal(err)
	}
	picURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(plain)
	pic, err := c.SendAttachment(ch1, picURL, 40, 30, "", false, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	srcBlob, srcKey := blobAndKey(t, pic.Content)
	spoiler, err := c.SendAttachment(ch1, "data:image/png;base64,"+base64.StdEncoding.EncodeToString([]byte("hush")), 4, 3, "", true, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	spoilerBlob, _ := blobAndKey(t, spoiler.Content)
	doc, err := a.SendFile(ch1, "data:application/pdf;base64,"+base64.StdEncoding.EncodeToString([]byte("%PDF-1.4 notes")), "notes.pdf", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	docBlob, _ := blobAndKey(t, doc.Content)

	var recent []store.MediaItem
	waitUntil(t, 20*time.Second, func() bool {
		recent, err = a.RecentMedia(RecentMax)
		return err == nil && recentHas(recent, srcBlob) && recentHas(recent, docBlob)
	}, "A's Recent never listed C's picture and A's file")
	if recentHas(recent, spoilerBlob) {
		t.Fatal("Recent offers a spoilered picture")
	}
	if recent[0].Item.BlobID != docBlob {
		t.Fatalf("Recent is not newest first: %s first, want the file", recent[0].Item.BlobID)
	}

	// The re-send, as the page does it.
	var src store.MediaItem
	for _, it := range recent {
		if it.Item.BlobID == srcBlob {
			src = it
		}
	}
	if src.Item.Keys != srcKey || src.ChannelID != ch1 {
		t.Fatalf("Recent item %+v does not carry the source's own key and channel", src)
	}
	got, err := a.FetchAttachment(src.ChannelID, src.Item.BlobID, src.Item.Keys, src.Item.Subtype)
	if err != nil {
		t.Fatal(err)
	}
	rb := &recorder{}
	b.OnMessage(rb.add)
	sent, err := a.SendAttachment(ch2, got, src.Item.W, src.Item.H, "", false, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	newBlob, newKey := blobAndKey(t, sent.Content)
	if newBlob == srcBlob {
		t.Fatal("the re-send reused the source blob")
	}
	if newKey == srcKey || newKey[:43] == srcKey[:43] {
		t.Fatal("the re-send reused the source key")
	}
	waitUntil(t, 20*time.Second, func() bool { return rb.has(sent.Content) }, "B never received the re-sent picture")
	opened, err := b.FetchAttachment(ch2, newBlob, newKey, "png")
	if err != nil {
		t.Fatalf("B cannot open the re-sent picture: %v", err)
	}
	if opened != picURL {
		t.Fatal("B's copy is not the picture C sent")
	}

	// No capability for the first conversation came with it.
	if _, err := b.FetchAttachment(ch2, srcBlob, srcKey, "png"); err == nil {
		t.Fatal("B fetched the original blob through guild two")
	}
	if b.store.HasAttachment(srcBlob) {
		t.Fatal("B holds the original blob")
	}
	if len(a.blobFor(identity.FingerprintOf(b.id.PublicKey()), srcBlob)) != 0 {
		t.Fatal("A serves guild one's blob to B")
	}
	if len(a.blobFor(identity.FingerprintOf(c.id.PublicKey()), newBlob)) != 0 {
		t.Fatal("A serves guild two's blob to C")
	}

	// Leaving guild one takes its items out of Recent; the copy in two stays.
	if err := a.LeaveGuild(g1.ID); err != nil {
		t.Fatal(err)
	}
	recent, err = a.RecentMedia(RecentMax)
	if err != nil {
		t.Fatal(err)
	}
	if recentHas(recent, srcBlob) || recentHas(recent, docBlob) {
		t.Fatal("Recent still offers items from a guild A left")
	}
	if !recentHas(recent, newBlob) {
		t.Fatal("Recent lost the copy in guild two")
	}
	for _, it := range recent {
		if it.ChannelID == ch1 {
			t.Fatalf("Recent offers %+v from a left guild", it)
		}
	}
}

// The strip is capped, whatever the page asks for.
func TestRecentMediaCap(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)
	g, err := a.CreateGuild("g")
	if err != nil {
		t.Fatal(err)
	}
	ch := g.Channels[0].ID
	for i := 0; i < RecentMax+3; i++ {
		if _, err := a.SendAttachment(ch, "data:image/png;base64,"+base64.StdEncoding.EncodeToString([]byte{byte(i), 1, 2, 3}), 2, 2, "", false, "", "", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, ask := range []int{0, 100, RecentMax} {
		got, err := a.RecentMedia(ask)
		if err != nil || len(got) != RecentMax {
			t.Fatalf("asked %d: %d items, %v; want %d", ask, len(got), err, RecentMax)
		}
	}
	got, _ := a.RecentMedia(3)
	if len(got) != 3 {
		t.Fatalf("asked 3: %d items", len(got))
	}
}
