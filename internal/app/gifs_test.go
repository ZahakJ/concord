package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// gifDataURL builds an n-byte "image" data URL of the given subtype. The bytes
// don't have to decode as a real image at this layer — the pack stores them as
// an opaque sealed blob, exactly like an attachment.
func gifDataURL(t *testing.T, subtype string, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return "data:image/" + subtype + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// TestGuildGifAddListRemove covers the local lifecycle: adding a GIF stores a
// small reference record (never the bytes), listing finds it with its tags, and
// removing it takes it out of the pack.
func TestGuildGifAddListRemove(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	g, err := a.CreateGuild("g")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}

	gif, err := a.AddGuildGif(g.ID, "Cat Vibing", []string{"Cat, dance", "cat"}, gifDataURL(t, "gif", 4096), 320, 240)
	if err != nil {
		t.Fatalf("AddGuildGif: %v", err)
	}
	if !blobIDRe.MatchString(gif.ID) {
		t.Fatalf("gif id %q is not an attachment blob id", gif.ID)
	}
	// Tags are normalized locally: lowercased, split on commas/spaces, deduped.
	if got := strings.Join(gif.Tags, ","); got != "cat,dance" {
		t.Fatalf("tags = %q, want cat,dance", got)
	}

	list, err := a.GuildGifs(g.ID)
	if err != nil {
		t.Fatalf("GuildGifs: %v", err)
	}
	if len(list) != 1 || list[0].ID != gif.ID || list[0].Name != "Cat Vibing" {
		t.Fatalf("list = %+v, want the one GIF we added", list)
	}
	if list[0].Width != 320 || list[0].Height != 240 || list[0].Subtype != "gif" {
		t.Fatalf("reference fields lost: %+v", list[0])
	}

	// Sending posts an ordinary v1 image-attachment token pointing at the SAME
	// blob — no re-seal — so every existing client renders it with no new code.
	msg, err := a.SendGuildGif(g.Channels[0].ID, gif.ID, "")
	if err != nil {
		t.Fatalf("SendGuildGif: %v", err)
	}
	want := "![image](concord://attach/v1/" + gif.ID + "/" + gif.Keys + "/gif/320x240)"
	if msg.Content != want {
		t.Fatalf("token = %q, want %q", msg.Content, want)
	}

	if err := a.RemoveGuildGif(g.ID, gif.ID); err != nil {
		t.Fatalf("RemoveGuildGif: %v", err)
	}
	if list, err := a.GuildGifs(g.ID); err != nil || len(list) != 0 {
		t.Fatalf("after remove: list = %+v err = %v, want empty", list, err)
	}
	// The blob survives the removal: messages already posted from it must keep
	// resolving.
	if _, err := a.FetchAttachment(g.Channels[0].ID, gif.ID, gif.Keys, "gif"); err != nil {
		t.Fatalf("blob gone after removing the pack entry: %v", err)
	}
	// And the record itself is gone, so it can no longer be posted.
	if _, err := a.SendGuildGif(g.Channels[0].ID, gif.ID, ""); err == nil {
		t.Fatal("SendGuildGif succeeded for a removed GIF")
	}
}

// TestGuildGifRejectsBadInput checks the local add path refuses images and
// names it cannot represent.
func TestGuildGifRejectsBadInput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	g, err := a.CreateGuild("g")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	ok := gifDataURL(t, "gif", 64)

	cases := []struct {
		what    string
		name    string
		tags    []string
		dataURL string
	}{
		{"empty name", "", nil, ok},
		{"name is whitespace", "   ", nil, ok},
		{"newline in name", "cat\nvibing", nil, ok},
		{"name too long", strings.Repeat("x", maxGifNameRunes+1), nil, ok},
		{"svg", "cat", nil, "data:image/svg+xml,<svg onload=alert(1)>"},
		{"not an image", "cat", nil, "data:text/html;base64,PHNjcmlwdD4="},
		{"remote url", "cat", nil, "https://evil.example/x.gif"},
		{"empty image", "cat", nil, "data:image/gif;base64,"},
	}
	for _, c := range cases {
		if _, err := a.AddGuildGif(g.ID, c.name, c.tags, c.dataURL, 10, 10); err == nil {
			t.Errorf("AddGuildGif accepted %s", c.what)
		}
	}
	if list, _ := a.GuildGifs(g.ID); len(list) != 0 {
		t.Fatalf("rejected adds still stored %d records", len(list))
	}
}

// TestGuildGifPermissionGate: managing the pack is a guild-management action.
// A member without MANAGE_GUILD may look and post, but not add or remove.
func TestGuildGifPermissionGate(t *testing.T) {
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

	gif, err := a.AddGuildGif(g.ID, "owner gif", []string{"ok"}, gifDataURL(t, "gif", 1024), 8, 8)
	if err != nil {
		t.Fatalf("owner AddGuildGif: %v", err)
	}
	if _, err := b.AddGuildGif(g.ID, "sneaky", nil, gifDataURL(t, "gif", 1024), 8, 8); err == nil {
		t.Fatal("a plain member was allowed to add a GIF")
	}
	if err := b.RemoveGuildGif(g.ID, gif.ID); err == nil {
		t.Fatal("a plain member was allowed to remove a GIF")
	}
}

// TestGuildGifPropagates: a GIF added on one peer becomes visible on another
// over the guild-meta topic, and removing it propagates too. The record is a
// reference — B fetches the actual bytes from A through the attachment path.
func TestGuildGifPropagates(t *testing.T) {
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

	gif, err := a.AddGuildGif(g.ID, "party parrot", []string{"party", "bird"}, gifDataURL(t, "gif", 64<<10), 128, 128)
	if err != nil {
		t.Fatalf("AddGuildGif: %v", err)
	}

	var seen GuildGif
	waitUntil(t, 25*time.Second, func() bool {
		list, err := b.GuildGifs(g.ID)
		if err != nil || len(list) != 1 {
			return false
		}
		seen = list[0]
		return seen.ID == gif.ID
	}, "B never learned A's GIF")
	if seen.Name != "party parrot" || strings.Join(seen.Tags, ",") != "party,bird" {
		t.Fatalf("B learned a mangled record: %+v", seen)
	}

	// B never held the bytes: it resolves them from A over the attach protocol,
	// which is the whole point of keeping them out of the meta topic.
	if _, err := b.FetchAttachment(g.Channels[0].ID, seen.ID, seen.Keys, seen.Subtype); err != nil {
		t.Fatalf("B could not fetch the GIF blob: %v", err)
	}

	if err := a.RemoveGuildGif(g.ID, gif.ID); err != nil {
		t.Fatalf("RemoveGuildGif: %v", err)
	}
	waitUntil(t, 25*time.Second, func() bool {
		list, err := b.GuildGifs(g.ID)
		return err == nil && len(list) == 0
	}, "B never saw the GIF removed")
}

// TestGuildGifReceiveValidation is the hostile-peer test. applyGuildGif is the
// receive path; every record here would break something downstream (an
// unresolvable token, a picker layout blown apart by control characters, or a
// write into another guild's pack) and must be dropped rather than stored.
func TestGuildGifReceiveValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	g, err := a.CreateGuild("g")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}

	good, err := a.AddGuildGif(g.ID, "good", []string{"ok"}, gifDataURL(t, "gif", 512), 10, 10)
	if err != nil {
		t.Fatalf("AddGuildGif: %v", err)
	}
	// The one record we know is well-formed must be accepted by the same
	// receive path, or the test below proves nothing.
	if !a.applyGuildGif(g.ID, good) {
		t.Fatal("receive path rejected a record it produced itself")
	}

	base := func() GuildGif {
		c := good
		c.Tags = append([]string(nil), good.Tags...)
		return c
	}
	mut := func(f func(*GuildGif)) GuildGif { c := base(); f(&c); return c }

	hostile := map[string]GuildGif{
		"id not a blob id":   mut(func(g *GuildGif) { g.ID = "../../etc/passwd" }),
		"id with a quote":    mut(func(g *GuildGif) { g.ID = `x" onerror="alert(1)` }),
		"empty id":           mut(func(g *GuildGif) { g.ID = "" }),
		"short key":          mut(func(g *GuildGif) { g.Keys = "abcd" }),
		"key not base64url":  mut(func(g *GuildGif) { g.Keys = strings.Repeat("!", 75) }),
		"path in subtype":    mut(func(g *GuildGif) { g.Subtype = "gif/../png" }),
		"svg subtype":        mut(func(g *GuildGif) { g.Subtype = "svg+xml" }),
		"empty subtype":      mut(func(g *GuildGif) { g.Subtype = "" }),
		"negative width":     mut(func(g *GuildGif) { g.Width = -1 }),
		"absurd height":      mut(func(g *GuildGif) { g.Height = 1 << 30 }),
		"empty name":         mut(func(g *GuildGif) { g.Name = "" }),
		"newline name":       mut(func(g *GuildGif) { g.Name = "a\nb" }),
		"bidi override name": mut(func(g *GuildGif) { g.Name = "cat‮gnicnad" }),
		"giant name":         mut(func(g *GuildGif) { g.Name = strings.Repeat("x", 5000) }),
		"tag with markup":    mut(func(g *GuildGif) { g.Tags = []string{"<img src=x onerror=alert(1)>"} }),
		"tag with a quote":   mut(func(g *GuildGif) { g.Tags = []string{`a"b`} }),
		"too many tags":      mut(func(g *GuildGif) { g.Tags = make([]string, maxGifTags+1) }),
		"uppercase tag":      mut(func(g *GuildGif) { g.Tags = []string{"CAT"} }),
	}
	for what, rec := range hostile {
		// Give each attempt a distinct id where the id itself isn't the hostile
		// part, so an accepted record can't be mistaken for the good one.
		if rec.ID == good.ID {
			rec.ID = strings.Repeat("a", 64)
		}
		if a.applyGuildGif(g.ID, rec) {
			t.Errorf("receive path accepted a hostile record: %s (%+v)", what, rec)
		}
	}

	list, err := a.GuildGifs(g.ID)
	if err != nil {
		t.Fatalf("GuildGifs: %v", err)
	}
	if len(list) != 1 || list[0].ID != good.ID {
		t.Fatalf("pack = %+v, want only the well-formed record", list)
	}

	// A record claiming to belong to another guild is bound to the guild whose
	// (MLS-encrypted, per-guild) topic actually carried it — never the claim.
	other, err := a.CreateGuild("other")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	crossed := base()
	crossed.ID = strings.Repeat("b", 64)
	crossed.GuildID = g.ID
	if !a.applyGuildGif(other.ID, crossed) {
		t.Fatal("valid record rejected")
	}
	if list, _ := a.GuildGifs(g.ID); len(list) != 1 {
		t.Fatalf("a record announced in another guild leaked into this pack: %+v", list)
	}
	if list, _ := a.GuildGifs(other.ID); len(list) != 1 || list[0].GuildID != other.ID {
		t.Fatalf("record not bound to the announcing guild: %+v", list)
	}
}
