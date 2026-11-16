package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ZahakJ/concord/internal/chronimport"
	"github.com/ZahakJ/concord/internal/imgmeta/imgfixture"
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

// The link card's picture is fetched on this device and re-encoded from its
// pixels before it is sealed, which already drops everything. This keeps it
// that way.
func TestLinkCardPictureCarriesNoMetadata(t *testing.T) {
	for _, f := range []imgfixture.Fixture{fixtureNamed("jpeg"), fixtureNamed("png"), fixtureNamed("gif")} {
		out, _, _, err := cleanPreviewImage(f.Bytes)
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
