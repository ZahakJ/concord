package app

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZahakJ/concord/internal/bmffmeta"
)

// Videos and HEIC photos go out as files (the composer attach, the share
// sheet and the photo picker all stage them through SendFile). What members
// receive has no location or device details in it.
func TestVideosAndHEICPhotosAreSentWithoutLocation(t *testing.T) {
	svc, _, ch := cleanFixture(t)
	for name, mime := range map[string]string{
		"android-fast.mp4": "video/mp4", "android-tail.mp4": "video/mp4",
		"iphone-fast.mov": "video/quicktime", "iphone-tail.mov": "video/quicktime",
		// The page's guess at a type does not matter: the bytes decide.
		"photo.heic": "application/octet-stream",
	} {
		in, err := os.ReadFile(filepath.Join("..", "bmffmeta", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		msg, err := svc.SendFile(ch, "data:"+mime+";base64,"+base64.StdEncoding.EncodeToString(in), name, "", "", "")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := sealedBytes(t, svc, ch, msg.Content, sentFileRe)
		for _, s := range []string{"SECRET", "48.8584", "+48."} {
			if bytes.Contains(got, []byte(s)) {
				t.Errorf("%s: %q was sent", name, s)
			}
		}
		want, _, err := bmffmeta.StripBytes(in)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s: sent bytes are not the cleaned file (%v)", name, err)
		}
	}
}

// A video the cleaner refuses is not sent until the person chooses to send it
// as it is; then it goes byte for byte.
func TestUncleanableVideoWaitsForTheChoice(t *testing.T) {
	svc, _, ch := cleanFixture(t)
	in, err := os.ReadFile(filepath.Join("..", "bmffmeta", "testdata", "android-fast.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	in = append(in, 1, 2, 3) // stray bytes after the last box: refused
	if _, _, err := bmffmeta.StripBytes(in); err == nil {
		t.Fatal("fixture is not refused; the test proves nothing")
	}
	uri := "data:video/mp4;base64," + base64.StdEncoding.EncodeToString(in)
	before, _ := svc.store.AttachmentBlobs()
	_, err = svc.SendFile(ch, uri, "clip.mp4", "", "", "")
	if err == nil || !strings.Contains(err.Error(), MediaAsIsMarker+"video") {
		t.Fatalf("SendFile: %v, want the as-is refusal for a video", err)
	}
	after, _ := svc.store.AttachmentBlobs()
	if len(after) != len(before) {
		t.Fatal("a blob was sealed for a refused video")
	}
	msg, err := svc.SendFileAsIs(ch, uri, "clip.mp4", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := sealedBytes(t, svc, ch, msg.Content, sentFileRe); !bytes.Equal(got, in) {
		t.Fatal("the chosen file was not sent as it is")
	}
	// "As is" covers only what the cleaner refused: a clean video sent that
	// way is still cleaned.
	good, _ := os.ReadFile(filepath.Join("..", "bmffmeta", "testdata", "android-fast.mp4"))
	msg, err = svc.SendFileAsIs(ch, "data:video/mp4;base64,"+base64.StdEncoding.EncodeToString(good), "c.mp4", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := sealedBytes(t, svc, ch, msg.Content, sentFileRe); bytes.Contains(got, []byte("SECRET")) {
		t.Fatal("a cleanable video sent 'as is' kept its details")
	}
}

// A picture format that can carry location and that nothing here cleans
// (TIFF and camera raw, JPEG XL, JPEG 2000) waits for the same choice.
func TestUncleanablePictureFormatsWaitForTheChoice(t *testing.T) {
	svc, _, ch := cleanFixture(t)
	for name, b := range map[string][]byte{
		"tiff": []byte("II*\x00\x08\x00\x00\x00GPS-SECRET"),
		"dng":  []byte("MM\x00*\x00\x00\x00\x08GPS-SECRET"),
		"jxl":  []byte("\x00\x00\x00\x0cJXL \r\n\x87\nGPS-SECRET"),
		"jp2":  []byte("\x00\x00\x00\x0cjP  \r\n\x87\nGPS-SECRET"),
	} {
		uri := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(b)
		_, err := svc.SendFile(ch, uri, "pic."+name, "", "", "")
		if err == nil || !strings.Contains(err.Error(), MediaAsIsMarker+"photo") {
			t.Fatalf("%s: %v, want the as-is refusal for a photo", name, err)
		}
		msg, err := svc.SendFileAsIs(ch, uri, "pic."+name, "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if got := sealedBytes(t, svc, ch, msg.Content, sentFileRe); !bytes.Equal(got, b) {
			t.Fatalf("%s: the chosen file was not sent as it is", name)
		}
	}
	// A document is not a picture: it goes as it always did.
	if _, err := svc.SendFile(ch, "data:text/plain;base64,"+base64.StdEncoding.EncodeToString([]byte("II, a note")), "n.txt", "", "", ""); err != nil {
		t.Fatal(err)
	}
}
