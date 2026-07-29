package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// TestAttachmentSendAndFetch is the attachments acceptance test: A sends an
// image as an encrypted blob reference; B (who never held the bytes) resolves
// the token by fetching the blob from A over the attach protocol; then C —
// who joins later — fetches the same blob from B while A is gone, proving
// availability spreads with viewers.
func TestAttachmentSendAndFetch(t *testing.T) {
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
	channel := g.Channels[0].ID
	code, _ := a.InviteCode(g.ID)
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B JoinViaInvite: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, a, b)

	// A "3 MB image" (bytes don't need to be a real image at this layer).
	plain := make([]byte, 3<<20)
	if _, err := rand.Read(plain); err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(plain)

	rb := &recorder{}
	b.OnMessage(rb.add)
	msg, err := a.SendAttachment(channel, dataURL, 800, 600, "", false, "", "")
	if err != nil {
		t.Fatalf("SendAttachment: %v", err)
	}
	if len(msg.Content) > 512 || !strings.Contains(msg.Content, "concord://attach/v1/") {
		t.Fatalf("token not compact: %d bytes: %.120s", len(msg.Content), msg.Content)
	}

	// B receives the tiny token over gossip...
	waitUntil(t, 20*time.Second, func() bool { return rb.has(msg.Content) }, "B never received the token message")

	// ...and resolves it to the original bytes by fetching the blob from A.
	parts := strings.Split(strings.TrimSuffix(strings.SplitAfter(msg.Content, "concord://attach/v1/")[1], ")"), "/")
	blobID, keys := parts[0], parts[1]
	got, err := b.FetchAttachment(channel, blobID, keys, "png")
	if err != nil {
		t.Fatalf("B FetchAttachment: %v", err)
	}
	if got != dataURL {
		t.Fatal("fetched attachment does not match the original")
	}

	// C joins, A leaves; C must still get the image — from B.
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

	got, err = c.FetchAttachment(channel, blobID, keys, "png")
	if err != nil {
		t.Fatalf("C FetchAttachment (from B, A offline): %v", err)
	}
	if got != dataURL {
		t.Fatal("C's fetched attachment does not match the original")
	}

	// Garbage keys must fail cleanly without poisoning the blob.
	if _, err := b.FetchAttachment(channel, blobID, base64.RawURLEncoding.EncodeToString(make([]byte, 56)), "png"); err == nil {
		t.Fatal("wrong key unexpectedly decrypted the blob")
	}
	if _, err := b.FetchAttachment(channel, blobID, keys, "png"); err != nil {
		t.Fatalf("valid fetch after bad-key attempt: %v", err)
	}
}

// TestFileAttachment covers the generic (non-image) file path: send a PDF-ish
// blob with a filename/mime, then fetch it back as a data URL from another
// member over the attach stream.
func TestFileAttachment(t *testing.T) {
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
	channel := g.Channels[0].ID
	code, _ := a.InviteCode(g.ID)
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B JoinViaInvite: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, a, b)

	blob := make([]byte, 800*1024)
	if _, err := rand.Read(blob); err != nil {
		t.Fatal(err)
	}
	dataURL := "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(blob)

	rb := &recorder{}
	b.OnMessage(rb.add)
	msg, err := a.SendFile(channel, dataURL, "report.pdf", "")
	if err != nil {
		t.Fatalf("SendFile: %v", err)
	}
	if !strings.Contains(msg.Content, "concord://file/v1/") {
		t.Fatalf("not a file token: %.120s", msg.Content)
	}
	waitUntil(t, 20*time.Second, func() bool { return rb.has(msg.Content) }, "B never received the file token")

	// Parse blobID/keys from [file](concord://file/v1/<blob>/<keys>/...).
	inner := strings.TrimSuffix(strings.SplitAfter(msg.Content, "concord://file/v1/")[1], ")")
	parts := strings.Split(inner, "/")
	blobID, keys := parts[0], parts[1]
	got, err := b.FetchFile(channel, blobID, keys, "application/pdf")
	if err != nil {
		t.Fatalf("B FetchFile: %v", err)
	}
	if got != dataURL {
		t.Fatal("fetched file does not match the original")
	}
	// A bad mime is rejected before any fetch.
	if _, err := b.FetchFile(channel, blobID, keys, "not a mime"); err == nil {
		t.Fatal("invalid mime accepted")
	}
}

// The v1/v2 split IS the compatibility contract: a peer on an older build can
// only render v1, so a plain image must never be sent as v2 just because the
// newer code path exists. Only an image that actually uses one of the composer's
// controls is allowed to change shape.
func TestAttachmentTokenVersionFollowsOptions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	guild, err := a.CreateGuild("G")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := guild.Channels[0].ID
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not-really-a-png"))

	cases := []struct {
		name    string
		spoiler bool
		fname   string
		desc    string
		want    string
	}{
		{"plain stays v1", false, "", "", "concord://attach/v1/"},
		{"spoiler forces v2", true, "", "", "concord://attach/v2/"},
		{"a filename forces v2", false, "cat.png", "", "concord://attach/v2/"},
		{"a description forces v2", false, "", "a cat", "concord://attach/v2/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, err := a.SendAttachment(channel, dataURL, 10, 10, "", tc.spoiler, tc.fname, tc.desc)
			if err != nil {
				t.Fatalf("SendAttachment: %v", err)
			}
			if !strings.Contains(msg.Content, tc.want) {
				t.Fatalf("token = %.160s, want it to contain %s", msg.Content, tc.want)
			}
		})
	}

	// The description rides in the message body, so it has to be bounded.
	long := strings.Repeat("x", maxAttachDescLen+500)
	msg, err := a.SendAttachment(channel, dataURL, 10, 10, "", false, "", long)
	if err != nil {
		t.Fatalf("SendAttachment: %v", err)
	}
	if len(msg.Content) > maxAttachDescLen*2 {
		t.Fatalf("an over-long description was not truncated: token is %d bytes", len(msg.Content))
	}
}
