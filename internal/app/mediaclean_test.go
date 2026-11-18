package app

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
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

func tbox(typ string, ps ...[]byte) []byte {
	var body []byte
	for _, p := range ps {
		body = append(body, p...)
	}
	return append(append(binary.BigEndian.AppendUint32(nil, uint32(8+len(body))), typ...), body...)
}

func tfull(typ string, ps ...[]byte) []byte {
	return tbox(typ, append([][]byte{{0, 0, 0, 0}}, ps...)...)
}

func t32(v int) []byte { return binary.BigEndian.AppendUint32(nil, uint32(v)) }

// subtitledVideo is an MP4 with a picture track, a subtitle track (of the
// given handler) and a location in its user data.
func subtitledVideo(handler string) []byte {
	ftyp := tbox("ftyp", []byte("isom"), t32(512), []byte("isom"))
	at := len(ftyp) + 8
	trak := func(h string, off, n int) []byte {
		stbl := tbox("stbl", tfull("stsd", t32(0)), tfull("stts", t32(0)),
			tfull("stsc", t32(1), t32(1), t32(1), t32(1)),
			tfull("stsz", t32(0), t32(1), t32(n)),
			tfull("stco", t32(1), t32(off)))
		minf := tbox("minf", tbox("dinf", tfull("dref", t32(1), tbox("url ", []byte{0, 0, 0, 1}))), stbl)
		mdia := tbox("mdia", tfull("mdhd", make([]byte, 20)), tfull("hdlr", t32(0), []byte(h), make([]byte, 13)), minf)
		return tbox("trak", tfull("tkhd", make([]byte, 80)), mdia)
	}
	moov := tbox("moov", tfull("mvhd", make([]byte, 96)), trak("vide", at, 32), trak(handler, at+32, 24),
		tbox("udta", tbox("\xa9xyz", []byte("+48.8584+002.2945/"))))
	return append(append(ftyp, tbox("mdat", bytes.Repeat([]byte("V"), 32), []byte("SUBTITLE: meet at home.."))...), moov...)
}

// A video with subtitles or text is not sent until the person chooses:
// "Remove and send" cleans it and drops those tracks, "Send as it is" sends
// the file unchanged. A timecode track is not asked about.
func TestVideoWithTextTracksWaitsForTheChoice(t *testing.T) {
	svc, _, ch := cleanFixture(t)
	for _, h := range []string{"sbtl", "text"} {
		in := subtitledVideo(h)
		uri := "data:video/mp4;base64," + base64.StdEncoding.EncodeToString(in)
		before, _ := svc.store.AttachmentBlobs()
		_, err := svc.SendFile(ch, uri, "clip.mp4", "", "", "")
		if err == nil || !strings.Contains(err.Error(), MediaTextMarker) {
			t.Fatalf("%s: SendFile: %v, want the text-track question", h, err)
		}
		if err := svc.CheckFile(uri); err == nil || !strings.Contains(err.Error(), MediaTextMarker) {
			t.Fatalf("%s: CheckFile: %v", h, err)
		}
		after, _ := svc.store.AttachmentBlobs()
		if len(after) != len(before) {
			t.Fatalf("%s: a blob was sealed before the choice", h)
		}
		msg, err := svc.SendFileWithoutText(ch, uri, "clip.mp4", "", "", "")
		if err != nil {
			t.Fatalf("%s: %v", h, err)
		}
		got := sealedBytes(t, svc, ch, msg.Content, sentFileRe)
		for _, s := range []string{"SUBTITLE", "48.8584"} {
			if bytes.Contains(got, []byte(s)) {
				t.Errorf("%s: %q was sent after Remove and send", h, s)
			}
		}
		if !bytes.Contains(got, bytes.Repeat([]byte("V"), 32)) {
			t.Errorf("%s: the picture went too", h)
		}
		msg, err = svc.SendFileAsIs(ch, uri, "clip.mp4", "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if got := sealedBytes(t, svc, ch, msg.Content, sentFileRe); !bytes.Equal(got, in) {
			t.Fatalf("%s: Send as it is did not send the file unchanged", h)
		}
	}
	// A camera's timecode track goes with the other details, unasked.
	in := subtitledVideo("tmcd")
	uri := "data:video/mp4;base64," + base64.StdEncoding.EncodeToString(in)
	if err := svc.CheckFile(uri); err != nil {
		t.Fatalf("timecode: CheckFile: %v", err)
	}
	msg, err := svc.SendFile(ch, uri, "clip.mp4", "", "", "")
	if err != nil {
		t.Fatalf("timecode: %v", err)
	}
	got := sealedBytes(t, svc, ch, msg.Content, sentFileRe)
	for _, s := range []string{"SUBTITLE", "48.8584"} {
		if bytes.Contains(got, []byte(s)) {
			t.Errorf("timecode: %q was sent", s)
		}
	}
}

// Video containers nothing here cleans are held for the choice, and so is
// an ISO file that starts with another box; the composer's own voice clips
// are not.
func TestOtherVideoContainersWaitForTheChoice(t *testing.T) {
	svc, _, ch := cleanFixture(t)
	ts := make([]byte, 188*3)
	ts[0], ts[188], ts[376] = 0x47, 0x47, 0x47
	m2ts := make([]byte, 192*3+4)
	m2ts[4], m2ts[196], m2ts[388] = 0x47, 0x47, 0x47
	iso := append(tbox("styp", []byte("msdh"), t32(0)), subtitledVideo("meta")...)
	for name, b := range map[string][]byte{
		"mkv":      append([]byte("\x1a\x45\xdf\xa3\x9f\x42\x86\x81\x01"), "GPS-SECRET"...),
		"avi":      []byte("RIFF\x00\x10\x00\x00AVI LIST GPS-SECRET"),
		"ts":       ts,
		"m2ts":     m2ts,
		"asf":      []byte("\x30\x26\xb2\x75\x8e\x66\xcf\x11\xa6\xd9\x00\xaa\x00\x62\xce\x6c"),
		"mpg":      []byte("\x00\x00\x01\xba\x44\x00\x04\x00\x04\x01"),
		"flv":      []byte("FLV\x01\x05\x00\x00\x00\x09"),
		"styp-iso": iso,
		"junk-iso": append(tbox("junk"), subtitledVideo("meta")...),
	} {
		uri := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(b)
		_, err := svc.SendFile(ch, uri, "v."+name, "", "", "")
		if err == nil || !strings.Contains(err.Error(), MediaAsIsMarker+"video") {
			t.Fatalf("%s: %v, want the as-is refusal for a video", name, err)
		}
		msg, err := svc.SendFileAsIs(ch, uri, "v."+name, "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if got := sealedBytes(t, svc, ch, msg.Content, sentFileRe); !bytes.Equal(got, b) {
			t.Fatalf("%s: the chosen file was not sent as it is", name)
		}
	}
	for name, b := range map[string][]byte{
		"webm": []byte("\x1a\x45\xdf\xa3\x9f\x42\x86\x81\x01 opus"),
		"ogg":  []byte("OggS\x00\x02 opus"),
	} {
		uri := "data:audio/" + name + ";base64," + base64.StdEncoding.EncodeToString(b)
		msg, err := svc.SendVoiceClip(ch, uri, "voice."+name)
		if err != nil {
			t.Fatalf("voice %s: %v", name, err)
		}
		if got := sealedBytes(t, svc, ch, msg.Content, sentFileRe); !bytes.Equal(got, b) {
			t.Fatalf("voice %s changed", name)
		}
	}
	if _, err := svc.SendVoiceClip(ch, "data:video/mp4;base64,"+base64.StdEncoding.EncodeToString(subtitledVideo("meta")), "v.mp4"); err == nil {
		t.Fatal("a video went out as a voice clip")
	}
	// Nothing else changes: a document, and a video that cleans, still go.
	if err := svc.CheckFile("data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte("Good morning"))); err != nil {
		t.Fatal(err)
	}
	if err := svc.CheckFile("data:video/mp4;base64," + base64.StdEncoding.EncodeToString(subtitledVideo("meta"))); err != nil {
		t.Fatal(err)
	}
}
