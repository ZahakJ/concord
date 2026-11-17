package bmffmeta

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func strip(t *testing.T, in []byte) ([]byte, Kind) {
	t.Helper()
	out, k, err := StripBytes(in)
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if len(out) > len(in) {
		t.Fatalf("output %d bytes from %d", len(out), len(in))
	}
	again, _, err := StripBytes(out)
	if err != nil {
		t.Fatalf("cleaned output is refused: %v", err)
	}
	if !bytes.Equal(again, out) {
		t.Fatalf("cleaning the cleaned output changed it")
	}
	return out, k
}

// Real files: ffmpeg encodes, exiftool adds what phones add (©xyz, loci,
// ©mak/©mod, an XMP uuid; QuickTime keys for location, make, model and
// software), and the -tail copies have moov moved after mdat with every chunk
// offset moved with it. All four decode to the same frames (ffmpeg framemd5).
func TestRealVideosLoseLocationAndKeepTheirSamples(t *testing.T) {
	secrets := []string{"SECRET", "48.8584", "+48.", "photoshop:City", "Lavf"}
	for _, name := range []string{"android-fast.mp4", "android-tail.mp4", "iphone-fast.mov", "iphone-tail.mov"} {
		t.Run(name, func(t *testing.T) {
			in, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			if !Sniff(in) {
				t.Fatal("not sniffed")
			}
			hasAny := false
			for _, s := range secrets[:3] {
				hasAny = hasAny || bytes.Contains(in, []byte(s))
			}
			if !hasAny {
				t.Fatal("fixture carries nothing to remove")
			}
			out, k := strip(t, in)
			if k != KindVideo {
				t.Fatalf("kind %v", k)
			}
			noSecrets(t, out, secrets)
			paths := tall(t, out)
			for _, bad := range []string{"/udta", "/uuid", "/loci", "/\xa9xyz", "/keys", "/free"} {
				if hasPath(paths, bad) {
					t.Errorf("%s survived", bad)
				}
			}
			sameChunks(t, in, out, "vide", "soun")
			mv, _ := tfind(out, 0, len(out), "moov", "mvhd")
			if !bytes.Equal(out[mv.start+mv.hdr+4:mv.start+mv.hdr+12], make([]byte, 8)) {
				t.Error("mvhd times kept")
			}
			// The layout is the input's: moov first stays first.
			top, _ := tchildren(out, 0, len(out))
			fast := strings.Contains(name, "fast")
			if (top[1].typ == "moov") != fast {
				t.Errorf("top-level order changed: %v", top)
			}
		})
	}
}

func TestPhoneLayoutsBothWays(t *testing.T) {
	for _, o := range []vopts{
		{moovFirst: true, gps: true},
		{moovFirst: false, gps: true},
		{moovFirst: true, co64: true, gps: true},
		{moovFirst: false, co64: true},
	} {
		in := synthVideo(o)
		out, _ := strip(t, in)
		noSecrets(t, out, videoSecrets)
		noSecrets(t, out, []string{"48.85", "SECRET-T"})
		sameChunks(t, in, out, "vide", "soun")
		if o.gps && !bytes.Contains(in, []byte("GPS-SECRET")) {
			t.Fatal("fixture has no GPS samples")
		}
		// The kept playback hint is still there, under its new number.
		if !bytes.Contains(out, []byte("com.android.capture.fps")) || !bytes.Contains(out, []byte("240.0")) {
			t.Errorf("%+v: capture fps lost", o)
		}
		m, ok := tfind(out, 0, len(out), "moov", "meta", "ilst")
		if !ok {
			t.Fatalf("%+v: no ilst", o)
		}
		items, _ := tchildren(out, m.start+m.hdr, m.end)
		if len(items) != 1 || binary.BigEndian.Uint32([]byte(items[0].typ)) != 1 {
			t.Errorf("%+v: ilst %v", o, items)
		}
		paths := tall(t, out)
		if o.gps == hasPath(paths, "/tref") {
			t.Errorf("%+v: tref kept %v, want it only when no track was dropped", o, hasPath(paths, "/tref"))
		}
		if o.gps {
			// The GPS track's samples are overwritten in place, not moved.
			md, _ := tfind(out, 0, len(out), "mdat")
			if bytes.Contains(out[md.start:md.end], []byte("GPS")) {
				t.Errorf("%+v: GPS samples left in mdat", o)
			}
		}
	}
}

// When moov comes first and shrinks, mdat moves up and so must every chunk
// offset; this is the fixture where a missed move shows.
func TestMoovFirstMovesChunkOffsets(t *testing.T) {
	in := synthVideo(vopts{moovFirst: true, gps: true})
	out, _ := strip(t, in)
	a, _ := tfind(in, 0, len(in), "mdat")
	b, _ := tfind(out, 0, len(out), "mdat")
	if a.start == b.start {
		t.Fatal("mdat did not move; the test proves nothing")
	}
	sameChunks(t, in, out, "vide", "soun")
}

func TestFragmentedVideoKeepsEveryOffset(t *testing.T) {
	in := synthVideo(vopts{moovFirst: true, frag: true})
	out, _ := strip(t, in)
	if len(out) != len(in) {
		t.Fatalf("length %d became %d", len(in), len(out))
	}
	noSecrets(t, out, videoSecrets)
	for _, s := range []string{"MOOF-DATA", "FRAG-SAMPLES"} {
		if bytes.Index(out, []byte(s)) != bytes.Index(in, []byte(s)) {
			t.Errorf("%s moved", s)
		}
	}
	if _, _, err := StripBytes(synthVideo(vopts{moovFirst: true, frag: true, gps: true})); !errors.Is(err, ErrRefused) {
		t.Errorf("fragmented with a GPS track: %v, want refused", err)
	}
}

func TestSniffFindsQuickTimeWithoutFtyp(t *testing.T) {
	in := synthVideo(vopts{moovFirst: true})
	mv, _ := tfind(in, 0, len(in), "moov")
	if !Sniff(in[mv.start:]) {
		t.Fatal("a QuickTime file that starts with moov is not sniffed")
	}
	if Sniff([]byte("hello world, not a video")) || Sniff([]byte{0, 0, 0, 3, 'f', 't', 'y', 'p'}) {
		t.Fatal("sniffed text")
	}
}

// A file too damaged to read whole is still named for what it starts as.
func TestDamagedFilesKeepTheirKind(t *testing.T) {
	v, _ := os.ReadFile(filepath.Join("testdata", "android-fast.mp4"))
	p, _ := os.ReadFile(filepath.Join("testdata", "photo.heic"))
	for name, c := range map[string]struct {
		in   []byte
		want Kind
	}{
		"video with stray bytes": {append(append([]byte(nil), v...), 1, 2, 3), KindVideo},
		"cut photo":              {p[:len(p)-5], KindPhoto},
		"cut video":              {v[:100], KindVideo},
	} {
		if _, k, err := StripBytes(c.in); err == nil || k != c.want {
			t.Errorf("%s: kind %v (%v), want %v and an error", name, k, err, c.want)
		}
	}
}

// Every way a file can point outside what is kept is refused, never written.
func TestUnprovableLayoutsAreRefused(t *testing.T) {
	base := synthVideo(vopts{moovFirst: false, gps: true})
	mutate := func(f func(b []byte)) []byte {
		b := append([]byte(nil), base...)
		f(b)
		return b
	}
	stco := func(b []byte, handler string) int {
		moov, _ := tfind(b, 0, len(b), "moov")
		kids, _ := tchildren(b, moov.start+moov.hdr, moov.end)
		for _, k := range kids {
			if k.typ != "trak" {
				continue
			}
			h, _ := tfind(b, k.start+k.hdr, k.end, "mdia", "hdlr")
			if string(b[h.start+h.hdr+8:h.start+h.hdr+12]) != handler {
				continue
			}
			s, _ := tfind(b, k.start+k.hdr, k.end, "mdia", "minf", "stbl", "stco")
			return s.start + s.hdr + 8
		}
		t.Fatal("no track")
		return 0
	}
	cases := map[string][]byte{
		"chunk outside mdat": mutate(func(b []byte) { binary.BigEndian.PutUint32(b[stco(b, "vide"):], 3) }),
		"kept chunk on a dropped one": mutate(func(b []byte) {
			copy(b[stco(b, "vide"):stco(b, "vide")+4], b[stco(b, "meta"):stco(b, "meta")+4])
		}),
		"dropped chunk outside mdat": mutate(func(b []byte) { binary.BigEndian.PutUint32(b[stco(b, "meta"):], 0) }),
		"external data": mutate(func(b []byte) {
			i := bytes.Index(b, []byte("url \x00\x00\x00\x01"))
			b[i+7] = 0
		}),
		"truncated": base[:len(base)-1],
		"trailing":  append(append([]byte(nil), base...), 1, 2, 3),
		"no tracks kept": func() []byte {
			b := append([]byte(nil), base...)
			for _, h := range []string{"vide", "soun"} {
				i := bytes.Index(b, []byte(h+"\x00\x00\x00\x00"))
				copy(b[i:], "text")
			}
			return b
		}(),
	}
	for name, in := range cases {
		out, _, err := StripBytes(in)
		if err == nil {
			t.Errorf("%s: written (%d bytes), want refused", name, len(out))
		}
	}
}

func TestHugeLengthsAreNotTrusted(t *testing.T) {
	ins := [][]byte{
		cat(bx("ftyp", []byte("isom")), u32(1), []byte("moov"), u64(1<<62)),
		cat(bx("ftyp", []byte("isom")), bx("moov", fb("trak", 0, 0)), bx("mdat")),
		cat(bx("ftyp", []byte("isom")), bx("moov", bx("trak", bx("mdia", fb("hdlr", 0, 0, u32(0), []byte("vide"), make([]byte, 12)),
			bx("minf", bx("stbl", fb("stco", 0, 0, u32(0x7fffffff)))))))),
		cat(bx("ftyp", []byte("isom")), bx("moov", bx("trak", bx("mdia", fb("hdlr", 0, 0, u32(0), []byte("vide"), make([]byte, 12)),
			bx("minf", bx("stbl", fb("stco", 0, 0, u32(1), u32(0)), fb("stsc", 0, 0, u32(1), u32(1), u32(0xffffffff), u32(1)),
				fb("stsz", 0, 0, u32(0), u32(0xffffffff)))))))),
		cat(bx("ftyp", []byte("heic")), fb("meta", 0, 0, fb("hdlr", 0, 0, u32(0), []byte("pict"), make([]byte, 12)),
			fb("pitm", 0, 0, u16(1)), fb("iloc", 1, 0, []byte{0x44, 0x40}, u16(0xffff)))),
	}
	for i, in := range ins {
		if _, _, err := StripBytes(in); err == nil {
			t.Errorf("input %d accepted", i)
		}
	}
}

func TestRealHEICLosesExifAndXMP(t *testing.T) {
	in, err := os.ReadFile(filepath.Join("testdata", "photo.heic"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(in, []byte("SECRETMAKE")) {
		t.Fatal("fixture has no make")
	}
	out, k := strip(t, in)
	if k != KindPhoto {
		t.Fatalf("kind %v", k)
	}
	noSecrets(t, out, []string{"SECRET", "Exif\x00\x00", "x:xmpmeta"})
	a, at := titems(t, in)
	b, bt := titems(t, out)
	for id, ty := range at {
		if ty == "Exif" || ty == "mime" {
			if _, ok := bt[id]; ok {
				t.Errorf("item %d (%s) kept", id, ty)
			}
			continue
		}
		if !bytes.Equal(a[id], b[id]) {
			t.Errorf("item %d (%s) points at other bytes", id, ty)
		}
	}
}

func TestHEICItemsAreRemovedConsistently(t *testing.T) {
	for _, o := range []hopts{{}, {base: true}, {exifInIdat: true}, {twoExtents: true}, {twoExtents: true, base: true}} {
		in := synthHEIC(o)
		out, _ := strip(t, in)
		noSecrets(t, out, photoSecrets)
		noSecrets(t, out, []string{"48.85"})
		a, _ := titems(t, in)
		b, bt := titems(t, out)
		if len(bt) != 2 || bt[1] != "hvc1" || bt[4] != "hvc1" {
			t.Fatalf("%+v: items %v", o, bt)
		}
		for _, id := range []int{1, 4} {
			if !bytes.Equal(a[id], b[id]) {
				t.Fatalf("%+v: item %d points at other bytes\n in  %q\n out %q", o, id, a[id], b[id])
			}
		}
		if _, ok := b[2]; ok {
			t.Fatalf("%+v: Exif location kept", o)
		}
		ir, _ := tfind(out, 0, len(out), "meta")
		ms := ir.start + ir.hdr + 4
		ref, _ := tfind(out, ms, ir.end, "iref")
		refs, _ := tchildren(out, ref.start+ref.hdr+4, ref.end)
		if len(refs) != 1 || refs[0].typ != "thmb" {
			t.Errorf("%+v: iref %v", o, refs)
		}
		pm, _ := tfind(out, ms, ir.end, "iprp")
		ipma, _ := tfind(out, pm.start+pm.hdr, pm.end, "ipma")
		if n := binary.BigEndian.Uint32(out[ipma.start+ipma.hdr+4:]); n != 2 {
			t.Errorf("%+v: ipma has %d entries", o, n)
		}
	}
}

func TestHEICThatCannotBeRewrittenIsRefused(t *testing.T) {
	for name, o := range map[string]hopts{
		"primary is Exif":          {primaryExif: true},
		"an image derives from it": {xmpRefToImage: true},
	} {
		if _, k, err := StripBytes(synthHEIC(o)); !errors.Is(err, ErrRefused) || k != KindPhoto {
			t.Errorf("%s: %v %v, want refused photo", name, k, err)
		}
	}
}

// The writer copies media in pieces: what Strip holds is the index, not the
// file. A 100 MB video (BMFF_BIG=1) is cleaned from disk to disk; the
// numbers are logged.
func TestBigVideoStreams(t *testing.T) {
	if os.Getenv("BMFF_BIG") == "" {
		t.Skip("BMFF_BIG not set")
	}
	bigVideo(t, 100<<20)
}

// FuzzStrip: on any input, no panic, no growth, and a cleaned file is
// accepted and left unchanged by a second pass. Its seed corpus (the real
// files, the built ones, cuts of both, and the hostile inputs above) runs
// with every "go test" of this package, GOARCH=386 included.
func FuzzStrip(f *testing.F) {
	var seeds [][]byte
	for _, n := range []string{"android-fast.mp4", "android-tail.mp4", "iphone-fast.mov", "iphone-tail.mov", "photo.heic"} {
		b, err := os.ReadFile(filepath.Join("testdata", n))
		if err != nil {
			f.Fatal(err)
		}
		seeds = append(seeds, b)
	}
	for _, o := range []vopts{{moovFirst: true, gps: true}, {gps: true}, {co64: true}, {moovFirst: true, frag: true}} {
		seeds = append(seeds, synthVideo(o))
	}
	for _, o := range []hopts{{}, {base: true}, {exifInIdat: true}, {twoExtents: true}} {
		seeds = append(seeds, synthHEIC(o))
	}
	for _, s := range seeds {
		f.Add(s)
		for _, cut := range []int{len(s) / 3, len(s) / 2, len(s) - 9} {
			f.Add(s[:cut])
		}
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		out, _, err := StripBytes(in)
		if err != nil {
			return
		}
		if len(out) > len(in) {
			t.Fatalf("output %d bytes from %d", len(out), len(in))
		}
		again, _, err := StripBytes(out)
		if err != nil {
			t.Fatalf("cleaned output is refused: %v", err)
		}
		if !bytes.Equal(again, out) {
			t.Fatalf("cleaning the cleaned output changed it")
		}
	})
}
