package bmffmeta

// Timecode tracks, the titles of voice recordings and songs, and the maker
// codes a sample entry or a handler carries.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Real ffmpeg clips (picture, sound, a timecode track, a location in udta):
// one with a timecode alone is cleaned without asking; one that also has
// subtitles is asked about.
func TestRealTimecodeClips(t *testing.T) {
	in := readTestdata(t, "tmcd.mov")
	if !bytes.Contains(in, []byte("+48.8584")) {
		t.Fatal("fixture has no location")
	}
	if ch, _ := ichunks(in); len(ch["tmcd"]) == 0 || len(ch["vide"]) == 0 || len(ch["soun"]) == 0 {
		t.Fatalf("fixture tracks: %d tmcd, %d vide, %d soun", len(ch["tmcd"]), len(ch["vide"]), len(ch["soun"]))
	}
	out, k := strip(t, in)
	if k != KindVideo {
		t.Fatalf("kind %d", k)
	}
	noSecrets(t, out, []string{"+48.8584"})
	if err := keptChunksSame(in, out); err != nil {
		t.Fatal(err)
	}
	if ch, _ := ichunks(out); len(ch["tmcd"]) != 0 {
		t.Fatal("the timecode track is still in the index")
	}

	in = readTestdata(t, "tmcd-subs.mov")
	if ch, _ := ichunks(in); len(ch["tmcd"]) == 0 || len(ch["text"]) == 0 {
		t.Fatal("fixture has no timecode or no subtitles")
	}
	if out, _, err := StripBytes(in); !errors.Is(err, ErrTextTracks) || out != nil {
		t.Fatalf("subtitles and timecode: %v, want ErrTextTracks", err)
	}
	out, _, err := StripBytesWith(in, Options{DropText: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := keptChunksSame(in, out); err != nil {
		t.Fatal(err)
	}
	noSecrets(t, out, []string{"meet at home"})
}

// ilstTag is an iTunes-style tag with a text value.
func ilstTag(typ, v string) []byte { return bx(typ, bx("data", u32(1), u32(0), []byte(v))) }

// freeform is an iTunes "----" tag: mean, name, value.
func freeform(mean, name, v string) []byte {
	return bx("----", fb("mean", 0, 0, []byte(mean)), fb("name", 0, 0, []byte(name)), bx("data", u32(1), u32(0), []byte(v)))
}

// m4a is an audio-only file shaped like what a phone or a music app writes:
// its tags in udta/meta/ilst, and optionally QuickTime keys at moov level
// (the meta box without version and flags, as Android writes it).
func m4a(tags [][]byte, keys []string, vals []string) []byte {
	ftyp := bx("ftyp", []byte("M4A "), u32(0), []byte("M4A isommp42"))
	pl := bytes.Repeat([]byte("A"), 32)
	mdatAt := uint64(len(ftyp) + 8)
	udta := bx("udta", bx("meta", u32(0), fb("hdlr", 0, 0, u32(0), []byte("mdir"), []byte("appl"), make([]byte, 8), []byte("\x00")),
		bx("ilst", cat(tags...))))
	var meta []byte
	if len(keys) > 0 {
		var kb, ib []byte
		for i, k := range keys {
			kb = append(kb, cat(u32(8+len(k)), []byte("mdta"), []byte(k))...)
			ib = append(ib, cat(u32(8+16+len(vals[i])), u32(i+1), u32(16+len(vals[i])), []byte("data"), u32(1), u32(0), []byte(vals[i]))...)
		}
		meta = bx("meta", fb("hdlr", 0, 0, u32(0), []byte("mdta"), make([]byte, 12), []byte("\x00")),
			fb("keys", 0, 0, u32(len(keys)), kb), bx("ilst", ib))
	}
	return cat(ftyp, bx("mdat", pl), moovOf(vtrak("soun", stblOf("stco", []uint64{mdatAt}, []int{32})), udta, meta))
}

// A voice recording loses its title, a song keeps its title, artist and
// album, and a file that says nothing of what made it keeps its title. What
// decides is the file's own tags, never its name.
func TestVoiceRecordingsLoseTheirTitle(t *testing.T) {
	place := "PLACE-12 Rue de Rivoli"
	memo := "com.apple.VoiceMemos (iPhone Version 17.1 (Build 21B74))"
	for name, c := range map[string]struct {
		in         []byte
		gone, kept []string
	}{
		"Voice Memos, encoder tag": {
			in:   m4a([][]byte{ilstTag("\xa9nam", place), ilstTag("\xa9day", "2026-11-16T09:12:00Z"), ilstTag("\xa9too", memo)}, nil, nil),
			gone: []string{place, "VoiceMemos"},
		},
		"Voice Memos, uuid tag only": {
			in:   m4a([][]byte{ilstTag("\xa9nam", place), freeform("com.apple.iTunes", "voice-memo-uuid", "6F1C2B4E-0000-4000-8000-1234567890AB")}, nil, nil),
			gone: []string{place, "6F1C2B4E"},
		},
		"Voice Memos, title key": {
			in:   m4a([][]byte{ilstTag("\xa9too", memo)}, []string{"com.apple.quicktime.title"}, []string{place}),
			gone: []string{place},
		},
		"Android recorder": {
			in:   m4a([][]byte{ilstTag("\xa9nam", place)}, []string{"com.android.version", "com.apple.quicktime.title"}, []string{"14", place}),
			gone: []string{place},
		},
		"song from a music app": {
			in:   m4a([][]byte{ilstTag("\xa9nam", "SONG-TITLE"), ilstTag("\xa9ART", "SONG-ARTIST"), ilstTag("\xa9alb", "SONG-ALBUM"), ilstTag("\xa9too", "iTunes 12.13.1.3"), ilstTag("\xa9day", "2019")}, []string{"com.apple.quicktime.title"}, []string{"KEY-SONG-TITLE"}),
			kept: []string{"SONG-TITLE", "SONG-ARTIST", "SONG-ALBUM", "KEY-SONG-TITLE"},
		},
		"no mark either way": {
			in:   m4a([][]byte{ilstTag("\xa9nam", "Voice 001 recording"), ilstTag("\xa9too", "Lavf60.16.100")}, nil, nil),
			kept: []string{"Voice 001 recording"},
		},
	} {
		out, _ := strip(t, c.in)
		for _, s := range c.gone {
			if !bytes.Contains(c.in, []byte(s)) {
				t.Fatalf("%s: fixture lacks %q", name, s)
			}
			if bytes.Contains(out, []byte(s)) {
				t.Errorf("%s: %q survived", name, s)
			}
		}
		for _, s := range c.kept {
			if !bytes.Contains(out, []byte(s)) {
				t.Errorf("%s: %q went", name, s)
			}
		}
	}

	// The same with real files: AAC from ffmpeg, tagged as Voice Memos
	// (exiftool wrote its encoder tag), as Android's recorder (QuickTime
	// keys at moov level), and as a song.
	for name, c := range map[string]struct{ gone, kept []string }{
		"voicememo.m4a": {gone: []string{"12 Rue de Rivoli", "VoiceMemos"}},
		"android.m4a":   {gone: []string{"Kitchen, 4 Elm Street", "com.android.version"}},
		"music.m4a":     {kept: []string{"Song Title", "Some Artist", "Some Album"}},
	} {
		in := readTestdata(t, name)
		out, _ := strip(t, in)
		if err := keptChunksSame(in, out); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		for _, s := range c.gone {
			if !bytes.Contains(in, []byte(s)) {
				t.Fatalf("%s: fixture lacks %q", name, s)
			}
			if bytes.Contains(out, []byte(s)) {
				t.Errorf("%s: %q survived", name, s)
			}
		}
		for _, s := range c.kept {
			if !bytes.Contains(out, []byte(s)) {
				t.Errorf("%s: %q went", name, s)
			}
		}
	}
}

// The four-byte maker code of a sample entry (QuickTime's vendor, "appl" or
// "FFMP") and a QuickTime handler's manufacturer code and flags are cleared,
// in picture and sound tracks.
func TestVendorAndHandlerMakerFieldsAreCleared(t *testing.T) {
	pl := bytes.Repeat([]byte("V"), 64)
	mdatAt := uint64(len(isoFtyp) + 8)
	visual := bx("avc1", make([]byte, 6), u16(1), u16(0), u16(0), []byte("VNDV"), make([]byte, 8), u16(64), u16(48),
		u32(0x480000), u32(0x480000), u32(0), u16(1), make([]byte, 32), u16(24), u16(0xffff), bx("avcC", []byte("CODEC-CONFIG")))
	sound := bx("mp4a", make([]byte, 6), u16(1), u16(0), u16(0), []byte("VNDS"), u16(2), u16(16), u16(0), u16(0), u16(44100), u16(0),
		bx("esds", []byte("SOUND-CONFIG")))
	trak := func(h string, entry []byte, off uint64) []byte {
		stbl := bx("stbl", fb("stsd", 0, 0, u32(1), entry), fb("stts", 0, 0, u32(0)),
			fb("stsc", 0, 0, u32(1), u32(1), u32(1), u32(1)),
			fb("stsz", 0, 0, u32(0), u32(1), u32(32)),
			fb("stco", 0, 0, u32(1), u32(int(off))))
		minf := bx("minf", bx("dinf", fb("dref", 0, 0, u32(1), fb("url ", 0, 1))),
			fb("hdlr", 0, 0, []byte("dhlr"), []byte("alis"), []byte("MKRD"), u32(0x40000000), u32(0x00010000), []byte("\x00")), stbl)
		mdia := bx("mdia", fb("mdhd", 0, 0, make([]byte, 20)),
			fb("hdlr", 0, 0, []byte("mhlr"), []byte(h), []byte("MKRM"), u32(0x40000000), u32(0x00010000), []byte("\x00")), minf)
		return bx("trak", fb("tkhd", 0, 3, make([]byte, 80)), mdia)
	}
	in := cat(isoFtyp, bx("mdat", pl), moovOf(trak("vide", visual, mdatAt), trak("soun", sound, mdatAt+32)))
	out, _ := strip(t, in)
	noSecrets(t, out, []string{"VNDV", "VNDS", "MKRD", "MKRM"})
	for _, s := range []string{"CODEC-CONFIG", "SOUND-CONFIG"} {
		if !bytes.Contains(out, []byte(s)) {
			t.Errorf("%s went", s)
		}
	}
	if err := keptChunksSame(in, out); err != nil {
		t.Fatal(err)
	}
	// The handler types stay: they say what the track is.
	ch, _ := ichunks(out)
	if len(ch["vide"]) != 1 || len(ch["soun"]) != 1 {
		t.Fatalf("tracks after cleaning: %v", ch)
	}
	// The real files' vendor codes go too.
	for _, n := range []string{"tmcd.mov", "iphone-fast.mov", "android-fast.mp4"} {
		out, _ := strip(t, readTestdata(t, n))
		noSecrets(t, out, []string{"FFMP", "appl"})
	}
}
