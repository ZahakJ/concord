package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ZahakJ/concord/internal/chronimport"
	"github.com/ZahakJ/concord/internal/imgmeta"
	"github.com/ZahakJ/concord/internal/imgmeta/imgfixture"
	cnet "github.com/ZahakJ/concord/internal/net"
)

// One test per way a picture leaves this device. Each sends the fixtures
// (metadata of every kind planted in a JPEG, PNG, GIF and WebP) through the
// public send path and checks what was sealed or stored, which is exactly
// what other members receive.

func cleanFixture(t *testing.T) (*Service, string, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc := startService(t, ctx)
	g, err := svc.CreateGuild("pictures")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	return svc, g.ID, g.Channels[0].ID
}

var (
	sentImageRe = regexp.MustCompile(`concord://attach/v[12]/([0-9a-f]{64})/([A-Za-z0-9_-]+)/`)
	sentFileRe  = regexp.MustCompile(`concord://file/v1/([0-9a-f]{64})/([A-Za-z0-9_-]+)/`)
)

// sealedBytes opens the blob a message token points at.
func sealedBytes(t *testing.T, svc *Service, channel, content string, re *regexp.Regexp) []byte {
	t.Helper()
	m := re.FindStringSubmatch(content)
	if m == nil {
		t.Fatalf("no token in %q", content)
	}
	plain, err := svc.fetchBlobPlaintext(channel, m[1], m[2])
	if err != nil {
		t.Fatal(err)
	}
	return plain
}

func uriBytes(t *testing.T, uri string) []byte {
	t.Helper()
	_, b64, ok := strings.Cut(uri, ";base64,")
	if !ok {
		t.Fatalf("not a data URI: %.40q", uri)
	}
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fixtureNamed(name string) imgfixture.Fixture {
	for _, f := range imgfixture.All() {
		if f.Name == name {
			return f
		}
	}
	panic(name)
}

// Share sheet, Files, paste, drag and drop and the desktop pickers all stage
// through the composer and reach the core here; so does the meme editor's
// edit of a sent picture.
func TestChatPicturesAreSentWithoutMetadata(t *testing.T) {
	svc, _, ch := cleanFixture(t)
	for _, f := range imgfixture.All() {
		msg, err := svc.SendAttachment(ch, f.DataURL(), 64, 40, "", false, "", "", "", "")
		if err != nil {
			t.Fatalf("%s: SendAttachment: %v", f.Name, err)
		}
		if err := f.Check(sealedBytes(t, svc, ch, msg.Content, sentImageRe)); err != nil {
			t.Errorf("sent picture: %v", err)
		}
		if _, err := svc.EditAttachment(ch, msg.ID, f.DataURL(), 64, 40); err != nil {
			t.Fatalf("%s: EditAttachment: %v", f.Name, err)
		}
		edited, ok, _ := svc.store.MessageByID(msg.ID)
		if !ok {
			t.Fatalf("%s: edited message not found", f.Name)
		}
		if err := f.Check(sealedBytes(t, svc, ch, edited.Content, sentImageRe)); err != nil {
			t.Errorf("edited picture: %v", err)
		}
	}
}

// A picture can reach the file path without anyone choosing it: one the
// renderer cannot decode, or a file the system gave no type. It is cleaned
// all the same; other files keep every byte.
func TestPicturesSentAsFilesAreSentWithoutMetadata(t *testing.T) {
	svc, _, ch := cleanFixture(t)
	for _, f := range imgfixture.All() {
		for _, mime := range []string{"image/" + f.Subtype, "application/octet-stream"} {
			uri := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(f.Bytes)
			msg, err := svc.SendFile(ch, uri, "photo."+f.Subtype, "", "", "")
			if err != nil {
				t.Fatalf("%s as %s: %v", f.Name, mime, err)
			}
			if err := f.Check(sealedBytes(t, svc, ch, msg.Content, sentFileRe)); err != nil {
				t.Errorf("as %s: %v", mime, err)
			}
		}
	}
	doc := []byte("%PDF-1.4 " + imgfixture.SecretText)
	msg, err := svc.SendFile(ch, "data:application/pdf;base64,"+base64.StdEncoding.EncodeToString(doc), "a.pdf", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := sealedBytes(t, svc, ch, msg.Content, sentFileRe); !bytes.Equal(got, doc) {
		t.Fatal("a document lost bytes on the file path")
	}
}

func TestProfilePicturesAreSentWithoutMetadata(t *testing.T) {
	svc, _, _ := cleanFixture(t)
	jpg, png := fixtureNamed("jpeg"), fixtureNamed("png")
	if err := svc.SetProfile(Profile{Name: "Sina", Avatar: jpg.DataURL(), Banner: png.DataURL()}); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	p := svc.SelfProfile()
	if err := jpg.Check(uriBytes(t, p.Avatar)); err != nil {
		t.Errorf("avatar: %v", err)
	}
	if err := png.Check(uriBytes(t, p.Banner)); err != nil {
		t.Errorf("banner: %v", err)
	}
}

func TestGuildPicturesAreSentWithoutMetadata(t *testing.T) {
	svc, gid, _ := cleanFixture(t)
	jpg, webp := fixtureNamed("jpeg"), fixtureNamed("webp")
	if err := svc.SetGuildProfile(gid, "pictures", jpg.DataURL(), webp.DataURL(), ""); err != nil {
		t.Fatalf("SetGuildProfile: %v", err)
	}
	for _, g := range svc.Guilds() {
		if g.ID != gid {
			continue
		}
		if err := jpg.Check(uriBytes(t, g.Icon)); err != nil {
			t.Errorf("icon: %v", err)
		}
		if err := webp.Check(uriBytes(t, g.Banner)); err != nil {
			t.Errorf("banner: %v", err)
		}
		return
	}
	t.Fatal("guild not found")
}

func TestForumBannerIsSentWithoutMetadata(t *testing.T) {
	svc, gid, fid := forumFixture(t)
	f := fixtureNamed("gif")
	if err := svc.SetForumBanner(gid, fid, f.DataURL()); err != nil {
		t.Fatalf("SetForumBanner: %v", err)
	}
	for _, g := range svc.Guilds() {
		for _, c := range g.Channels {
			if c.ID == fid {
				if err := f.Check(uriBytes(t, c.Banner)); err != nil {
					t.Error(err)
				}
				return
			}
		}
	}
	t.Fatal("forum not found")
}

func TestStoryPictureIsSentWithoutMetadata(t *testing.T) {
	svc, gid, _ := cleanFixture(t)
	f := fixtureNamed("png")
	if err := svc.PostStory([]string{gid}, f.DataURL(), "today"); err != nil {
		t.Fatalf("PostStory: %v", err)
	}
	stories, err := svc.GuildStories(gid)
	if err != nil || len(stories) != 1 {
		t.Fatalf("GuildStories: %d, %v", len(stories), err)
	}
	if err := f.Check(uriBytes(t, stories[0].Preset)); err != nil {
		t.Error(err)
	}
}

func TestCustomEmojiIsSentWithoutMetadata(t *testing.T) {
	svc, gid, _ := cleanFixture(t)
	for _, f := range imgfixture.All() {
		if err := svc.AddCustomEmoji(gid, "pic_"+f.Name, f.DataURL()); err != nil {
			t.Fatalf("%s: AddCustomEmoji: %v", f.Name, err)
		}
	}
	got, err := svc.CustomEmoji(gid)
	if err != nil || len(got) != len(imgfixture.All()) {
		t.Fatalf("CustomEmoji: %d, %v", len(got), err)
	}
	for _, e := range got {
		f := fixtureNamed(strings.TrimPrefix(e.Name, "pic_"))
		if err := f.Check(uriBytes(t, e.Image)); err != nil {
			t.Error(err)
		}
	}
}

func TestGuildGifIsSentWithoutMetadata(t *testing.T) {
	svc, gid, ch := cleanFixture(t)
	for _, f := range []imgfixture.Fixture{fixtureNamed("gif"), fixtureNamed("webp")} {
		g, err := svc.AddGuildGif(gid, "pic "+f.Name, nil, f.DataURL(), 16, 12)
		if err != nil {
			t.Fatalf("%s: AddGuildGif: %v", f.Name, err)
		}
		plain, err := svc.fetchBlobPlaintext(ch, g.ID, g.Keys)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Check(plain); err != nil {
			t.Error(err)
		}
	}
}

// A chat export imported into a guild is shared with its members, so its
// pictures are cleaned on the way into the archive.
func TestImportedPicturesAreSharedWithoutMetadata(t *testing.T) {
	svc, _, ch := cleanFixture(t)
	dir := t.TempDir()
	for _, f := range imgfixture.All() {
		path := filepath.Join(dir, "photo."+f.Subtype)
		if err := os.WriteFile(path, f.Bytes, 0o600); err != nil {
			t.Fatal(err)
		}
		tok, ok := svc.sealImportedFile(path, chronimport.Attachment{FileName: "photo." + f.Subtype}, int64(len(f.Bytes)))
		if !ok {
			t.Fatalf("%s: not sealed", f.Name)
		}
		if err := f.Check(sealedBytes(t, svc, ch, tok, sentImageRe)); err != nil {
			t.Error(err)
		}
	}
}

// An exported picture the stripper cannot read is not archived with its
// metadata: the attachment is dropped and counted as a placeholder, and the
// message still imports.
func TestImportedPictureThatCannotBeCleanedIsDropped(t *testing.T) {
	s := importTestService(t)
	dir := t.TempDir()
	good := fixtureNamed("jpeg")
	// The fixture JPEG with a segment whose length field is below its own two
	// bytes, which the stripper refuses as malformed. Every secret stays in it.
	bad := append([]byte{0xFF, 0xD8, 0xFF, 0xFE, 0x00, 0x01}, good.Bytes[2:]...)
	if _, err := imgmeta.Strip(bad); err == nil {
		t.Fatal("the damaged JPEG was cleaned; the test needs one the stripper refuses")
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, b := range map[string][]byte{"good.jpg": good.Bytes, "bad.jpg": bad} {
		if err := os.WriteFile(filepath.Join(dir, "assets", name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	export := map[string]any{
		"guild":   map[string]any{"id": "g1", "name": "Pictures"},
		"channel": map[string]any{"id": "c1", "type": "GuildTextChat", "name": "photos"},
		"messages": []map[string]any{
			{"id": "m1", "type": "Default", "timestamp": "2020-01-01T10:00:00Z", "content": "the good one",
				"author":      map[string]any{"id": "u1", "name": "ada"},
				"attachments": []map[string]any{{"url": "assets/good.jpg", "fileName": "good.jpg", "fileSizeBytes": len(good.Bytes)}}},
			{"id": "m2", "type": "Default", "timestamp": "2020-01-01T10:01:00Z", "content": "the broken one",
				"author":      map[string]any{"id": "u1", "name": "ada"},
				"attachments": []map[string]any{{"url": "assets/bad.jpg", "fileName": "bad.jpg", "fileSizeBytes": len(bad)}}},
		},
	}
	raw, err := json.Marshal(export)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "photos.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	_, acc := runImportOffline(t, s, dir, chronimport.DefaultPolicy())

	if acc.sealed != 1 || acc.placeholders != 1 {
		t.Fatalf("sealed %d, placeholders %d; want the good picture sealed and the broken one a placeholder",
			acc.sealed, acc.placeholders)
	}
	var tokens []string
	var brokenImported bool
	for _, msgs := range acc.byChannel {
		for _, m := range msgs {
			tokens = append(tokens, m.Attach...)
			if strings.Contains(m.Content, "the broken one") {
				brokenImported = true
				if len(m.Attach) != 0 {
					t.Errorf("the broken picture's message carries %d attachments", len(m.Attach))
				}
			}
		}
	}
	if !brokenImported {
		t.Fatal("the message with the broken picture was not imported")
	}
	if len(tokens) != 1 {
		t.Fatalf("%d attachment tokens archived, want 1", len(tokens))
	}
	if err := good.Check(sealedBytes(t, s, "c1", tokens[0], sentImageRe)); err != nil {
		t.Error(err)
	}
	// Every blob in the store is the one the archive points at, so the broken
	// picture's bytes were stored nowhere.
	blobs, err := s.store.AttachmentBlobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(blobs) != 1 {
		t.Fatalf("%d blobs stored, want only the cleaned picture's", len(blobs))
	}
	if id := sentImageRe.FindStringSubmatch(tokens[0])[1]; blobs[id] == nil {
		t.Fatal("the stored blob is not the archived picture's")
	}
}

// The portraits and custom emoji an export brought with it are shared with
// the guild too: the portraits in the archive manifest, the emoji in the
// guild's emoji set. Both are cleaned at import.
func TestImportedPortraitsAndEmojiAreSharedWithoutMetadata(t *testing.T) {
	svc, gid, _ := cleanFixture(t)
	dir := t.TempDir()
	var stats chronimport.Stats
	for _, f := range imgfixture.All() {
		name := "face-" + f.Name + "." + f.Subtype
		if err := os.WriteFile(filepath.Join(dir, name), f.Bytes, 0o600); err != nil {
			t.Fatal(err)
		}
		got := svc.importedAvatar(dir, chronimport.Author{ID: "1", Name: "Sina", AvatarURL: name})
		if got == "" {
			t.Fatalf("%s: portrait dropped", f.Name)
		}
		if err := f.Check(uriBytes(t, got)); err != nil {
			t.Errorf("portrait: %v", err)
		}
		stats.Emoji = append(stats.Emoji, chronimport.EmojiStat{
			Name: "pic_" + f.Name, Sanitized: "pic_" + f.Name, Local: true,
			Bytes: int64(len(f.Bytes)), ImageURL: name,
		})
	}
	in, skipped := svc.importCustomEmoji(gid, dir, &stats, &chatImportJob{id: "pictures", guildID: gid})
	if in != len(imgfixture.All()) || skipped != 0 {
		t.Fatalf("emoji imported %d, skipped %d", in, skipped)
	}
	got, err := svc.CustomEmoji(gid)
	if err != nil || len(got) != len(imgfixture.All()) {
		t.Fatalf("CustomEmoji: %d, %v", len(got), err)
	}
	for _, e := range got {
		if err := fixtureNamed(strings.TrimPrefix(e.Name, "pic_")).Check(uriBytes(t, e.Image)); err != nil {
			t.Errorf("emoji: %v", err)
		}
	}
}

// A GIF picked from search is sent and saved like any other picture: what
// the members receive is cleaned.
func TestSearchedGifsAreSentWithoutMetadata(t *testing.T) {
	svc, gid, ch := cleanFixture(t)
	if err := svc.SetGifSearchEnabled(true); err != nil {
		t.Fatal(err)
	}
	for _, f := range []imgfixture.Fixture{fixtureNamed("gif"), fixtureNamed("webp")} {
		fakeProxy(t, func(_ context.Context, req cnet.GifRequest) (cnet.GifResponse, string, error) {
			return cnet.GifResponse{Status: cnet.GifStatusOK, Subtype: f.Subtype, Media: f.Bytes}, "12D3KooWfake", nil
		})
		msg, err := svc.SendSearchedGif(context.Background(), ch, "ZnVsbA.c2ln", "", 16, 12, "")
		if err != nil {
			t.Fatalf("%s: SendSearchedGif: %v", f.Name, err)
		}
		if err := f.Check(sealedBytes(t, svc, ch, msg.Content, sentImageRe)); err != nil {
			t.Errorf("sent: %v", err)
		}
		saved, err := svc.SaveSearchedGif(context.Background(), gid, "pic "+f.Name, nil, "ZnVsbA.c2ln", 16, 12)
		if err != nil {
			t.Fatalf("%s: SaveSearchedGif: %v", f.Name, err)
		}
		plain, err := svc.fetchBlobPlaintext(ch, saved.ID, saved.Keys)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Check(plain); err != nil {
			t.Errorf("saved: %v", err)
		}
	}
}

// The link card's picture is fetched on this device and re-encoded from its
// pixels before it is sealed, which already drops everything. This keeps it
// that way.
func TestLinkCardPictureCarriesNoMetadata(t *testing.T) {
	for _, f := range []imgfixture.Fixture{fixtureNamed("jpeg"), fixtureNamed("png"), fixtureNamed("gif")} {
		out, _, _, err := cleanCardImage(context.Background(), f.Bytes, false)
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		for _, s := range []string{imgfixture.SecretGPS, imgfixture.SecretXMP, imgfixture.SecretMakerNote, imgfixture.SecretText, imgfixture.SecretComment, "Exif\x00\x00"} {
			if bytes.Contains(out, []byte(s)) {
				t.Errorf("%s: %q survived the link-card re-encode", f.Name, s)
			}
		}
	}
}

// The format is read from the bytes, not from the URI's declared subtype: a
// JPEG declared as image/jpg (which the setters' validators accept, and which
// some document providers report) is cleaned like any other, and keeps the
// subtype it was declared with.
func TestPicturesDeclaredAsJpgAreSentWithoutMetadata(t *testing.T) {
	svc, gid, _ := cleanFixture(t)
	jpg := fixtureNamed("jpeg")
	uri := "data:image/jpg;base64," + base64.StdEncoding.EncodeToString(jpg.Bytes)
	if err := svc.SetProfile(Profile{Name: "Sina", Avatar: uri}); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	if err := svc.SetGuildProfile(gid, "pictures", uri, "", ""); err != nil {
		t.Fatalf("SetGuildProfile: %v", err)
	}
	stored := map[string]string{"avatar": svc.SelfProfile().Avatar}
	for _, g := range svc.Guilds() {
		if g.ID == gid {
			stored["guild icon"] = g.Icon
		}
	}
	if len(stored) != 2 {
		t.Fatal("guild not found")
	}
	for what, got := range stored {
		if !strings.HasPrefix(got, "data:image/jpg;base64,") {
			t.Errorf("%s: declared subtype changed: %.30q", what, got)
		}
		if err := jpg.Check(uriBytes(t, got)); err != nil {
			t.Errorf("%s: %v", what, err)
		}
	}
	// Base64 written without its padding is still a picture to a browser.
	unpadded := "data:image/jpeg;base64," + base64.RawStdEncoding.EncodeToString(jpg.Bytes)
	if err := svc.SetGuildProfile(gid, "pictures", unpadded, "", ""); err != nil {
		t.Fatalf("SetGuildProfile, unpadded: %v", err)
	}
	for _, g := range svc.Guilds() {
		if g.ID == gid {
			if err := jpg.Check(uriBytes(t, g.Icon)); err != nil {
				t.Errorf("unpadded guild icon: %v", err)
			}
		}
	}
}

// The orbit ring's sprite is a picture every member sees, so it is cleaned
// with the avatar and banner.
func TestRingSpriteIsSentWithoutMetadata(t *testing.T) {
	svc, _, _ := cleanFixture(t)
	png := fixtureNamed("png")
	in := &Style{Sat: png.DataURL()}
	if err := svc.SetProfile(Profile{Name: "Sina", Style: in}); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	if in.Sat != png.DataURL() {
		t.Error("SetProfile changed the caller's Style")
	}
	p := svc.SelfProfile()
	if p.Style == nil || p.Style.Sat == "" {
		t.Fatal("sprite not stored")
	}
	if err := png.Check(uriBytes(t, p.Style.Sat)); err != nil {
		t.Error(err)
	}
}

// A HEIC or AVIF picture cannot be cleaned, so it is refused with an error
// that says what the file is, whatever it is called.
func TestUncleanablePicturesAreRefusedByName(t *testing.T) {
	svc, _, ch := cleanFixture(t)
	avif := append([]byte{0, 0, 0, 28}, "ftypavif"...)
	avif = append(avif, make([]byte, 64)...)
	_, err := svc.SendAttachment(ch, "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString(avif), 8, 8, "", false, "", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "HEIC or AVIF") {
		t.Fatalf("err = %v, want one naming HEIC or AVIF", err)
	}
}

// The avatar is held to the same whole-URI check as every other picture, so
// a URI the cleaner cannot read is refused rather than stored as it came.
func TestAvatarMustBeAPlainBase64Picture(t *testing.T) {
	svc, _, _ := cleanFixture(t)
	b64 := base64.StdEncoding.EncodeToString(fixtureNamed("jpeg").Bytes)
	for name, uri := range map[string]string{
		"upper-case BASE64": "data:image/jpeg;BASE64," + b64,
		"a name parameter":  "data:image/jpeg;name=a.jpg;base64," + b64,
		"a newline inside":  "data:image/jpeg;base64," + b64[:40] + "\n" + b64[40:],
		"an svg":            "data:image/svg+xml;base64,PHN2Zy8+",
		"not base64 at all": "data:image/png,rawbytes",
	} {
		if err := svc.SetProfile(Profile{Name: "Sina", Avatar: uri}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := svc.SetProfile(Profile{Name: "Sina", Avatar: "data:image/jpeg;base64," + b64}); err != nil {
		t.Fatalf("a plain JPEG avatar: %v", err)
	}
}
