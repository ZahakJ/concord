package bmffmeta

// Inputs for the bytes the cleaner copies as they are: a compressed movie
// in them, boxes in tref that are not references, one index type in each
// region that is scanned, and the times of the movie, track and media
// headers.

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// testdataFile reads a file of testdata, for the input builders.
func testdataFile(name string) []byte {
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		panic(err)
	}
	return b
}

// cmovOf is a compressed movie box holding inner, deflated. A run of zeros
// comes first, so that the deflater codes the bytes rather than storing
// them as they are.
func cmovOf(inner []byte) []byte {
	inner = cat(bx("free", make([]byte, 96)), inner)
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, _ = w.Write(inner)
	_ = w.Close()
	return bx("cmov", bx("dcom", []byte("zlib")), bx("cmvd", u32(len(inner)), buf.Bytes()))
}

// growLastTrak appends x to the last trak of a file whose moov is its last
// box and whose last trak is moov's last child.
func growLastTrak(file, x []byte) []byte {
	mi := bytes.LastIndex(file, []byte("moov")) - 4
	ti := bytes.LastIndex(file, []byte("trak")) - 4
	out := cat(file, x)
	binary.BigEndian.PutUint32(out[mi:], binary.BigEndian.Uint32(out[mi:])+uint32(len(x)))
	binary.BigEndian.PutUint32(out[ti:], binary.BigEndian.Uint32(out[ti:])+uint32(len(x)))
	return out
}

// growFirstTrak appends x to the first trak of a file whose moov comes
// before everything that is addressed.
func growFirstTrak(file, x []byte) []byte {
	mi := bytes.Index(file, []byte("moov")) - 4
	ti := bytes.Index(file, []byte("trak")) - 4
	te := ti + int(binary.BigEndian.Uint32(file[ti:]))
	out := cat(file[:te], x, file[te:])
	binary.BigEndian.PutUint32(out[mi:], binary.BigEndian.Uint32(out[mi:])+uint32(len(x)))
	binary.BigEndian.PutUint32(out[ti:], binary.BigEndian.Uint32(out[ti:])+uint32(len(x)))
	return out
}

// insertInto puts x at the end of the box that starts at i and adds its
// length to every box that holds it, found by walking down from the top.
func insertInto(file []byte, i int, x []byte) []byte {
	end := i + int(binary.BigEndian.Uint32(file[i:]))
	out := cat(file[:end], x, file[end:])
	var fix func(s, e int)
	fix = func(s, e int) {
		for s+8 <= e {
			sz := int(binary.BigEndian.Uint32(file[s:]))
			if sz < 8 || s+sz > e {
				return
			}
			if s <= i && end <= s+sz {
				binary.BigEndian.PutUint32(out[s:], uint32(sz+len(x)))
				if s == i {
					return
				}
				h := 8
				if string(file[s+4:s+8]) == "meta" && string(file[s+12:s+16]) != "hdlr" {
					h = 12
				}
				fix(s+h, s+sz)
				return
			}
			s += sz
		}
	}
	fix(0, len(file))
	return out
}

// realTailWithCmovTrack is android-tail.mp4 with a subtitle track, deflated
// in a cmov, in the tref of its first track. The subtitle's one sample is 16
// bytes of the picture track's data.
func realTailWithCmovTrack() []byte {
	an := testdataFile("android-tail.mp4")
	tk := make([]byte, 80)
	copy(tk[8:], u32(9))
	copy(tk[16:], u32(1000))
	tx := bx("tx3g", make([]byte, 6), u16(1), make([]byte, 30))
	sub := bx("trak", fb("tkhd", 0, 3, tk), bx("mdia",
		fb("mdhd", 0, 0, u32(0), u32(0), u32(1000), u32(1000), u32(0)), hdlrOf("sbtl"),
		bx("minf", fb("nmhd", 0, 0), bx("dinf", fb("dref", 0, 0, u32(1), fb("url ", 0, 1))),
			bx("stbl", fb("stsd", 0, 0, u32(1), tx),
				fb("stts", 0, 0, u32(1), u32(1), u32(1000)),
				fb("stsc", 0, 0, u32(1), u32(1), u32(1), u32(1)),
				fb("stsz", 0, 0, u32(0), u32(1), u32(16)),
				fb("stco", 0, 0, u32(1), u32(64))))))
	return insertInto(an, bytes.Index(an, []byte("trak"))-4, bx("tref", cmovOf(sub)))
}

// The compressed movies of the seeds hold no index type in their bytes but
// their own three, so only those types can refuse them.
func TestCompressedSeedsHideTheirTypes(t *testing.T) {
	inner := map[string][]byte{
		"handler": hdlrOf("clcp"),
		"stco":    fb("stco", 0, 0, u32(1), u32(64)),
		"track":   etrak(2, "sbtl", 64, 26, nil, nil, nil, nil, nil, nil),
	}
	for name, in := range inner {
		c := cmovOf(in)
		for _, ty := range tIndex {
			if ty == "cmov" || ty == "dcom" || ty == "cmvd" {
				continue
			}
			if bytes.Contains(c, []byte(ty)) {
				t.Errorf("%s: the deflated bytes hold %q", name, ty)
			}
		}
	}
}

// referenceInputs are files whose tref holds a box that is not a reference
// to a track of the movie, or whose sound entry holds tags or chapters. A
// player reads each where it finds it.
func referenceInputs() map[string][]byte {
	pl := cat(bytes.Repeat([]byte("V"), 32), []byte("SUBTITLE: hello there....."))
	at := len(isoFtyp) + 8
	vid := etrak(1, "vide", at, 32, nil, nil, nil, nil, nil, nil)
	file := func(traks ...[]byte) []byte { return cat(isoFtyp, bx("mdat", pl), moovOf(traks...)) }
	one := func(trakX []byte) []byte { return file(etrak(1, "vide", at, 32, trakX, nil, nil, nil, nil, nil)) }
	s2 := func(entryX []byte) []byte { return etrak(2, "soun", at+32, 26, nil, nil, nil, nil, nil, entryX) }
	xyz := bx("udta", bx("\xa9xyz", u16(18), u16(0x15c7), []byte("+35.0000+139.0000/")))
	chpl := fb("chpl", 1, 0, u32(0), []byte{1}, u64(0), []byte{11}, []byte("CHAPTERNAME"))
	cases := map[string][]byte{
		"tref holding dref":        one(bx("tref", fb("dref", 0, 0, u32(1), fb("url ", 0, 0, []byte("file:///etc/hostname\x00"))))),
		"tref holding udta":        one(bx("tref", xyz)),
		"tref holding chpl":        one(bx("tref", chpl)),
		"tref holding meta":        one(bx("tref", fb("meta", 0, 0, fb("pitm", 0, 0, u16(1))))),
		"tref, id of no track":     one(bx("tref", bx("chap", u32(7)))),
		"tref, id 0":               one(bx("tref", bx("chap", u32(0)))),
		"tref, part of an id":      one(bx("tref", bx("chap", u16(1)))),
		"tref, a 64-bit size":      one(bx("tref", cat(u32(1), []byte("chap"), u64(20), u32(1)))),
		"sound entry holding udta": file(vid, s2(xyz)),
		"sound entry holding meta": file(vid, s2(fb("meta", 0, 0, bx("ilst", bx("\xa9nam", bx("data", u32(1), u32(0), []byte("TITLE"))))))),
		"sound entry holding chpl": file(vid, s2(chpl)),
	}
	fr := fragFile(fragOpts{tfhdFlags: 0x20000, trunFlags: 0x001, count: 2, defSize: 8})
	cases["fragmented, tref holding trex"] = growFirstTrak(fr, bx("tref", fb("trex", 0, 0, u32(0), u32(1), u32(0), u32(4), u32(0))))
	// frag.mp4: moov is 28..775, its trak 144..638, mvex 638..678 and udta
	// 678..775. The trak grows by a 40-byte tref and udta gives way to a
	// 57-byte free box, so nothing after moov moves.
	rf := testdataFile("frag.mp4")
	if string(rf[32:36]) != "moov" || string(rf[148:152]) != "trak" || string(rf[642:646]) != "mvex" || string(rf[682:686]) != "udta" {
		panic("frag.mp4 is not laid out as this input assumes")
	}
	trak := cat(rf[144:638], bx("tref", fb("trex", 0, 0, u32(1), u32(1), u32(0), u32(4), u32(0))))
	binary.BigEndian.PutUint32(trak, uint32(len(trak)))
	cases["frag.mp4, tref holding trex"] = cat(rf[:144], trak, rf[638:678], bx("free", make([]byte, 49)), rf[775:])
	an := testdataFile("android-tail.mp4")
	ti := bytes.Index(an, []byte("trak")) - 4
	cases["android-tail.mp4, tref holding udta"] = insertInto(an, ti, bx("tref", xyz))
	cases["android-tail.mp4, tref holding chpl"] = insertInto(an, ti, bx("tref", chpl))
	return cases
}

// Reference boxes that name tracks of the movie are written as they are.
func TestReferenceBoxesAreKept(t *testing.T) {
	pl := cat(bytes.Repeat([]byte("V"), 32), []byte("SOUND-SAMPLES-AND-MORE...."))
	at := len(isoFtyp) + 8
	var refs []byte
	for _, ty := range []string{"chap", "tmcd", "hint", "cdsc", "vdep", "sync"} {
		refs = append(refs, bx(ty, u32(2))...)
	}
	refs = append(refs, bx("cdsc", u32(1), u32(2))...)
	refs = append(refs, bx("font")...)
	tref := bx("tref", refs)
	in := cat(isoFtyp, bx("mdat", pl), moovOf(
		etrak(1, "vide", at, 32, tref, nil, nil, nil, nil, nil),
		etrak(2, "soun", at+32, 26, nil, nil, nil, nil, nil, nil)))
	for _, o := range []Options{{}, {DropText: true}} {
		out, _, err := StripBytesWith(in, o)
		if err != nil {
			t.Fatalf("DropText %v: %v", o.DropText, err)
		}
		if !bytes.Contains(out, tref) {
			t.Errorf("DropText %v: tref not written as it was", o.DropText)
		}
		if err := keptChunksSame(in, out); err != nil {
			t.Error(err)
		}
	}
}

// putAfter writes s over the bytes at off after the first occurrence of
// mark.
func putAfter(file []byte, mark string, off int, s string) []byte {
	i := bytes.Index(file, []byte(mark))
	if i < 0 {
		panic("no " + mark)
	}
	out := bytes.Clone(file)
	copy(out[i+off:], s)
	return out
}

// oneFragment is ftyp, moov (one picture track, id 0, and mvex), one moof
// with the given tfhd and trun, an mdat of 16 media bytes, and after.
func oneFragment(tfhd []byte, trun func(dataOff int) []byte, trafExtra []byte, after ...[]byte) []byte {
	ftyp := bx("ftyp", []byte("isom"), u32(512), []byte("isomiso5"))
	trex := fb("trex", 0, 0, u32(0), u32(1), u32(0), u32(0), u32(0))
	moov := moovOf(vtrak("vide", stblOf("stco", nil, nil)), bx("mvex", trex))
	build := func(d int) []byte {
		return bx("moof", fb("mfhd", 0, 0, u32(1)), bx("traf", tfhd, trun(d), trafExtra))
	}
	m := build(0)
	return cat(ftyp, moov, build(len(m)+8), bx("mdat", []byte("SAMPLE-ONESAMPL2")), cat(after...))
}

// regionControls are the plain files the region inputs are made from.
func regionControls() (plain, frag []byte) {
	pl := cat(bytes.Repeat([]byte("V"), 32), []byte("SOUND-SAMPLES-AND-MORE...."))
	at := len(isoFtyp) + 8
	plain = cat(isoFtyp, bx("mdat", pl), moovOf(
		etrak(1, "vide", at, 32, nil, nil, nil, nil, nil, nil),
		etrak(2, "soun", at+32, 26, nil, nil, nil, nil, nil, nil)))
	return plain, oneFragment(regionTfhd, regionTrun, nil)
}

// regionInputs put one index type in each region of a file that is copied
// as it is or with some fields cleared, at a place no other input reaches.
func regionInputs() map[string][]byte {
	pl := cat(bytes.Repeat([]byte("V"), 32), []byte("SOUND-SAMPLES-AND-MORE...."))
	at := len(isoFtyp) + 8
	two := func(trakX, mdiaX, minfX, stblX []byte) []byte {
		return cat(isoFtyp, bx("mdat", pl), moovOf(
			etrak(1, "vide", at, 32, trakX, mdiaX, minfX, nil, stblX, nil),
			etrak(2, "soun", at+32, 26, nil, nil, nil, nil, nil, nil)))
	}
	plain, fplain := regionControls()
	cases := map[string][]byte{
		"region: mvhd, reserved bytes":            putAfter(plain, "mvhd", 4+80, "trak"),
		"region: tkhd, matrix":                    putAfter(plain, "tkhd", 4+44, "moof"),
		"region: mdhd, language and quality":      putAfter(plain, "mdhd", 4+20, "stbl"),
		"region: mdia hdlr, component type":       putAfter(plain, "hdlr", 4+4, "stco"),
		"region: vmhd, colour":                    putAfter(plain, "vmhd", 4+8, "moov"),
		"region: stts, a sample delta":            putAfter(plain, "stts", 4+12, "minf"),
		"region: stsc, description index":         putAfter(plain, "stsc", 4+16, "mdia"),
		"region: stsz, version and flags":         putAfter(plain, "stsz", 4, "trun"),
		"region: picture entry, a field":          putAfter(plain, "avc1", 4+16, "stsd"),
		"region: sound entry, a field":            putAfter(plain, "mp4a", 4+20, "tfhd"),
		"region: ftyp, a brand":                   putAfter(plain, "ftyp", 4+8, "moov"),
		"region: elng":                            two(nil, fb("elng", 0, 0, []byte("trak\x00")), nil, nil),
		"region: sgpd in stbl, across two fields": two(nil, nil, nil, fb("sgpd", 1, 0, []byte("roll"), u32(4), u32(1), []byte("\x00\x00st"), []byte("co\x00\x00"))),
		"region: ctts in stbl, odd offset":        two(nil, nil, nil, fb("ctts", 0, 0, u32(1), []byte("\x00co6"), []byte("4\x00\x00\x00"))),
		"region: minf data handler, component":    two(nil, nil, fb("hdlr", 0, 0, []byte("trak"), []byte("alis"), make([]byte, 12), []byte("\x00")), nil),
		"region: elst, a duration":                two(bx("edts", fb("elst", 0, 0, u32(1), []byte("moof"), u32(0), u32(0x10000))), nil, nil, nil),
		"region: load":                            two(bx("load", u32(0), []byte("stz2"), u32(0), u32(0)), nil, nil, nil),
		"region: tref, a track id":                two(bx("tref", bx("cdsc", []byte("traf"))), nil, nil, nil),
		"region: iods":                            cat(isoFtyp, bx("mdat", pl), bx("moov", fb("mvhd", 0, 0, make([]byte, 96)), etrak(1, "vide", at, 32, nil, nil, nil, nil, nil, nil), fb("iods", 0, 0, []byte("hdlr")))),
		"region: stco, after its entries":         insertInto(plain, bytes.Index(plain, []byte("stco"))-4, []byte("hdlr")),
		"region: mfhd, sequence number":           putAfter(fplain, "mfhd", 4+4, "moov"),
		"region: trex, default flags":             putAfter(fplain, "trex", 4+20, "stsz"),
		"region: tfhd, default sample flags":      oneFragment(fb("tfhd", 0, 0x20020, u32(0), []byte("traf")), regionTrun, nil),
		"region: trun, a sample's flags":          oneFragment(regionTfhd, regionTrunFlags, nil),
		"region: tfdt":                            oneFragment(regionTfhd, regionTrun, fb("tfdt", 0, 0, []byte("trun"))),
		"region: sbgp in traf":                    oneFragment(regionTfhd, regionTrun, fb("sbgp", 0, 0, []byte("roll"), u32(1), u32(2), []byte("tfhd"))),
		"region: styp, a brand":                   oneFragment(regionTfhd, regionTrun, nil, bx("styp", []byte("msdh"), u32(0), []byte("moov"))),
		"region: sidx":                            oneFragment(regionTfhd, regionTrun, nil, fb("sidx", 0, 0, u32(1), u32(1000), u32(0), []byte("trak"), u32(0))),
		"region: mfra":                            oneFragment(regionTfhd, regionTrun, nil, bx("mfra", fb("tfra", 0, 0, u32(0), u32(0), u32(1), []byte("hdlr"), u32(0), []byte{1, 1, 1}))),
	}
	return cases
}

var regionTfhd = fb("tfhd", 0, 0x20000, u32(0))

func regionTrun(d int) []byte { return fb("trun", 0, 0x201, u32(2), u32(d), u32(10), u32(6)) }

// regionTrunFlags is regionTrun with sample flags, the first sample's
// spelling a type.
func regionTrunFlags(d int) []byte {
	return fb("trun", 0, 0x601, u32(2), u32(d), u32(10), []byte("moof"), u32(6), u32(0))
}

// The files the region inputs are made from are written.
func TestRegionControlsAreWritten(t *testing.T) {
	plain, frag := regionControls()
	for name, in := range map[string][]byte{"plain": plain, "fragmented": frag} {
		for _, o := range []Options{{}, {DropText: true}} {
			out, _, err := StripBytesWith(in, o)
			if err != nil {
				t.Errorf("%s (DropText %v): %v", name, o.DropText, err)
				continue
			}
			if err := keptChunksSame(in, out); err != nil {
				t.Errorf("%s: %v", name, err)
			}
			if err := fragRunsSame(in, out); err != nil {
				t.Errorf("%s: %v", name, err)
			}
		}
	}
}

// The second readers see every region input, so the fuzz target can find
// the class in each region.
func TestSecondReadersSeeEveryRegion(t *testing.T) {
	for name, in := range regionInputs() {
		_, e1 := ichunks(in)
		_, e2 := ifruns(in)
		if !errors.Is(e1, errTwoTables) && !errors.Is(e2, errTwoTables) {
			t.Errorf("%s: %v, %v, want errTwoTables", name, e1, e2)
		}
	}
}

// A creation or modification time is a number: one that spells a type is
// cleared like any other, and the file is written as if it were zero.
func TestTimesThatSpellATypeAreWritten(t *testing.T) {
	for _, name := range []string{"android-tail.mp4", "iphone-fast.mov", "frag.mp4"} {
		in := testdataFile(name)
		planted := bytes.Clone(in)
		n := 0
		for _, ty := range []string{"mvhd", "tkhd", "mdhd"} {
			for i := 0; ; {
				j := bytes.Index(planted[i:], []byte(ty))
				if j < 0 {
					break
				}
				p := i + j + 4 // the payload
				// The low half of each time: 32 bits (version 0) or 64.
				c, m := p+4, p+8
				if planted[p] == 1 {
					c, m = p+8, p+16
				}
				copy(planted[c:], "hdlr")
				copy(planted[m:], "stco")
				n++
				i = p
			}
		}
		if n < 3 {
			t.Fatalf("%s: %d headers", name, n)
		}
		for _, o := range []Options{{}, {DropText: true}} {
			want, _, err := StripBytesWith(in, o)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			got, _, err := StripBytesWith(planted, o)
			if err != nil {
				t.Errorf("%s (DropText %v): %v", name, o.DropText, err)
				continue
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s (DropText %v): output differs from the file's own", name, o.DropText)
			}
			if err := keptChunksSame(planted, got); err != nil {
				t.Errorf("%s: %v", name, err)
			}
			if err := fragRunsSame(planted, got); err != nil {
				t.Errorf("%s: %v", name, err)
			}
		}
	}
}
