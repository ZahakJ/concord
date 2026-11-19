package bmffmeta

// Files built to sit at the edges of what the cleaner accepts, read back by a
// second reader written apart from the package (ichunks), and the fuzz
// target that compares every kept chunk byte for byte.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// errTwoTables: a track with two handlers or two of a chunk table, which
// two readers can read two ways. The cleaner must refuse such a file.
var errTwoTables = errors.New("a track with two of one table")

// ichunks returns, per track handler, the bytes of every chunk. It never
// panics: a file it cannot read is an error.
func ichunks(b []byte) (m map[string][][]byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			m, err = nil, fmt.Errorf("reader: %v", r)
		}
	}()
	// No reading past the end into the slice's spare capacity: the fuzzer
	// hands in inputs whose capacity runs far past their length.
	b = b[:len(b):len(b)]
	moov, ok := tfind(b, 0, len(b), "moov")
	if !ok {
		return nil, fmt.Errorf("no moov")
	}
	out := map[string][][]byte{}
	kids, e := tchildren(b, moov.start+moov.hdr, moov.end)
	if e != nil {
		return nil, e
	}
	for _, k := range kids {
		if k.typ != "trak" {
			continue
		}
		s, e := k.start+k.hdr, k.end
		md, ok := tfind(b, s, e, "mdia")
		if !ok {
			return nil, fmt.Errorf("no mdia")
		}
		mk, er := tchildren(b, md.start+md.hdr, md.end)
		if er != nil {
			return nil, er
		}
		handler := ""
		var minf tbox
		for _, x := range mk {
			if x.typ == "hdlr" {
				if handler != "" {
					return nil, errTwoTables
				}
				handler = string(b[x.start+x.hdr+8 : x.start+x.hdr+12])
			}
			if x.typ == "minf" {
				minf = x
			}
		}
		if minf.end == 0 {
			continue
		}
		stbl, ok := tfind(b, minf.start+minf.hdr, minf.end, "stbl")
		if !ok {
			continue
		}
		sk, er := tchildren(b, stbl.start+stbl.hdr, stbl.end)
		if er != nil {
			return nil, er
		}
		seen := map[string]int{}
		for _, x := range sk {
			switch x.typ {
			case "stco", "co64":
				seen["offsets"]++
			case "stsc", "stsz":
				seen[x.typ]++
			}
		}
		for _, n := range seen {
			if n > 1 {
				return nil, errTwoTables
			}
		}
		get := func(n string) []byte {
			x, ok := tfind(b, stbl.start+stbl.hdr, stbl.end, n)
			if !ok {
				return nil
			}
			return b[x.start+x.hdr : x.end]
		}
		var offs []uint64
		if p := get("stco"); p != nil {
			for i := 0; i < int(binary.BigEndian.Uint32(p[4:])); i++ {
				offs = append(offs, uint64(binary.BigEndian.Uint32(p[8+4*i:])))
			}
		} else if p := get("co64"); p != nil {
			for i := 0; i < int(binary.BigEndian.Uint32(p[4:])); i++ {
				offs = append(offs, binary.BigEndian.Uint64(p[8+8*i:]))
			}
		}
		if len(offs) == 0 {
			continue
		}
		stsc, stsz := get("stsc"), get("stsz")
		fixed := uint64(binary.BigEndian.Uint32(stsz[4:]))
		sample := 0
		ne := int(binary.BigEndian.Uint32(stsc[4:]))
		for c := range offs {
			spc := 0
			for i := 0; i < ne; i++ {
				if int(binary.BigEndian.Uint32(stsc[8+12*i:])) <= c+1 {
					spc = int(binary.BigEndian.Uint32(stsc[8+12*i+4:]))
				}
			}
			var n uint64
			if fixed != 0 {
				n = fixed * uint64(spc)
				sample += spc
			} else {
				for j := 0; j < spc; j++ {
					n += uint64(binary.BigEndian.Uint32(stsz[12+4*sample:]))
					sample++
				}
			}
			out[handler] = append(out[handler], b[offs[c]:offs[c]+n])
		}
	}
	return out, nil
}

// keptChunksSame: every picture, sound and auxiliary chunk of in is in out
// with the same bytes, and out has no other track.
func keptChunksSame(in, out []byte) error {
	a, err := ichunks(in)
	if errors.Is(err, errTwoTables) {
		return fmt.Errorf("input accepted: %v", err)
	}
	if err != nil {
		return nil // the second reader cannot say
	}
	b, err := ichunks(out)
	if err != nil {
		return fmt.Errorf("output unreadable by the second reader: %v", err)
	}
	for _, h := range []string{"vide", "soun", "auxv"} {
		if len(a[h]) != len(b[h]) {
			return fmt.Errorf("%s: %d chunks became %d", h, len(a[h]), len(b[h]))
		}
		for i := range a[h] {
			if !bytes.Equal(a[h][i], b[h][i]) {
				return fmt.Errorf("%s chunk %d differs", h, i)
			}
		}
	}
	for h := range b {
		if !keepHandlers[h] {
			return fmt.Errorf("track %q still in output", h)
		}
	}
	return nil
}

// FuzzStripKeepsSamples: on any input, an accepted output tiles, keeps every
// kept chunk's bytes and every fragment run's bytes (by the second readers),
// and a second pass leaves it as it is. Seeded with the built edge cases.
func FuzzStripKeepsSamples(f *testing.F) {
	for _, n := range realFixtures {
		b, err := os.ReadFile(filepath.Join("testdata", n))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	for _, o := range []vopts{{moovFirst: true, gps: true}, {gps: true}, {co64: true}, {moovFirst: true, co64: true, gps: true}, {moovFirst: true, frag: true}} {
		f.Add(synthVideo(o))
	}
	for _, o := range []hopts{{}, {base: true}, {exifInIdat: true}, {twoExtents: true}, {base: true, twoExtents: true}} {
		f.Add(synthHEIC(o))
	}
	for _, in := range edgeInputs() {
		f.Add(in)
	}
	f.Add(audioOnly(true))
	f.Add(fragShrinks(4))
	for _, in := range fragSeeds() {
		f.Add(in)
	}
	for _, in := range twoTableInputs() {
		f.Add(in)
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		for _, o := range []Options{{}, {DropText: true}} {
			out, kind, err := StripBytesWith(in, o)
			if err != nil {
				if out != nil {
					t.Fatal("output with an error")
				}
				continue
			}
			if len(out) > len(in) {
				t.Fatalf("grew")
			}
			if _, err := tchildren(out, 0, len(out)); err != nil {
				t.Fatalf("output does not tile: %v", err)
			}
			if kind == KindVideo {
				if err := keptChunksSame(in, out); err != nil {
					t.Fatal(err)
				}
				if err := fragRunsSame(in, out); err != nil {
					t.Fatal(err)
				}
			}
			again, _, err := StripBytesWith(out, o)
			if err != nil || !bytes.Equal(again, out) {
				t.Fatalf("second pass: %v", err)
			}
		}
	})
}

// A sample-to-chunk table is read to its end. An entry that starts past the
// last chunk used to end the reading there, and the entries after it, which
// a player still reads, went unchecked: the file was accepted and part of a
// kept sound track was cleared. Two fuzz inputs from the round 3 review, kept
// in testdata/fuzz as seeds too.
func TestChunkTableIsReadToTheEnd(t *testing.T) {
	for _, n := range []string{"stsc-entry-past-last-chunk-1", "stsc-entry-past-last-chunk-2"} {
		in := fuzzSeed(t, n)
		for _, o := range []Options{{}, {DropText: true}} {
			if _, _, err := StripBytesWith(in, o); !errors.Is(err, ErrMalformed) {
				t.Errorf("%s (DropText %v): %v, want ErrMalformed", n, o.DropText, err)
			}
		}
	}
}

// fuzzSeed reads a one-value input kept in testdata/fuzz/FuzzStripKeepsSamples.
func fuzzSeed(t *testing.T, n string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "fuzz", "FuzzStripKeepsSamples", n))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[1], "[]byte(") {
		t.Fatalf("%s: not a one-value fuzz input", n)
	}
	in, err := strconv.Unquote(strings.TrimSuffix(strings.TrimPrefix(lines[1], "[]byte("), ")"))
	if err != nil {
		t.Fatalf("%s: %v", n, err)
	}
	return []byte(in)
}

// twoTableInputs are files with two of a box a track has once: a chunk
// table, a handler, a track header, a fragment default. The table the
// cleaner checks need not be the one a player reads.
func twoTableInputs() map[string][]byte {
	pl := bytes.Repeat([]byte("V"), 64)
	mdatAt := uint64(len(isoFtyp) + 8)
	offs := cat(u32(int(mdatAt)), u32(int(mdatAt)+32))
	stbl := func(tables ...[]byte) []byte {
		return bx("stbl", fb("stsd", 0, 0, u32(0)), fb("stts", 0, 0, u32(0)), cat(tables...),
			fb("stco", 0, 0, u32(2), offs))
	}
	stsc := func(spc int) []byte { return fb("stsc", 0, 0, u32(1), u32(1), u32(spc), u32(1)) }
	stsz := func(sizes ...int) []byte {
		var sb []byte
		for _, z := range sizes {
			sb = append(sb, u32(z)...)
		}
		return fb("stsz", 0, 0, u32(0), u32(len(sizes)), sb)
	}
	trak := func(head []byte, hdlrs ...string) []byte {
		var hb []byte
		for _, h := range hdlrs {
			hb = append(hb, fb("hdlr", 0, 0, u32(0), []byte(h), make([]byte, 12), []byte("h\x00"))...)
		}
		minf := bx("minf", bx("dinf", fb("dref", 0, 0, u32(1), fb("url ", 0, 1))), stbl(stsc(1), stsz(32, 32)))
		return bx("trak", head, bx("mdia", fb("mdhd", 0, 0, make([]byte, 20)), hb, minf))
	}
	tkhd := fb("tkhd", 0, 3, make([]byte, 80))
	tail := func(tr []byte) []byte { return cat(isoFtyp, bx("mdat", pl), moovOf(tr)) }
	return map[string][]byte{
		"two stsc": tail(vtrak("vide", stbl(stsc(2), stsc(1), stsz(16, 16, 16, 16)))),
		"two stsz": tail(vtrak("vide", stbl(stsc(1), stsz(32, 32), stsz(16, 16)))),
		"two hdlr": tail(trak(tkhd, "meta", "vide")),
		"two tkhd": tail(trak(cat(tkhd, tkhd), "vide")),
		"two trex": fragFile(fragOpts{tfhdFlags: 0x20000, trunFlags: 0x001, count: 2, defSize: 4, trexTwice: true}),
	}
}

// A track with two of a table is refused, with or without its text tracks.
// The first such file came from the fuzz target: its picture track had two
// stsc and two stsz boxes, the cleaner checked the last of each and a reader
// of the first found chunks megabytes long.
func TestTwoOfATableAreRefused(t *testing.T) {
	cases := twoTableInputs()
	cases["fuzz input"] = fuzzSeed(t, "stbl-two-sample-to-chunk-tables")
	for name, in := range cases {
		for _, o := range []Options{{}, {DropText: true}} {
			if _, _, err := StripBytesWith(in, o); !errors.Is(err, ErrRefused) {
				t.Errorf("%s (DropText %v): %v, want ErrRefused", name, o.DropText, err)
			}
		}
	}
	// The same shapes with one of each are written.
	pl := bytes.Repeat([]byte("V"), 64)
	mdatAt := uint64(len(isoFtyp) + 8)
	for name, in := range map[string][]byte{
		"one of each": simpleTail("stco", pl, []uint64{mdatAt, mdatAt + 32}, []int{32, 32}),
		"one trex":    fragFile(fragOpts{tfhdFlags: 0x20000, trunFlags: 0x001, count: 2, defSize: 4}),
	} {
		if _, _, err := StripBytesWith(in, Options{}); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// The second reader reads a file's bytes, not what lies past them in the
// slice: a chunk past the end is an error even when the capacity is there.
func TestSecondReaderStopsAtTheEnd(t *testing.T) {
	pl := bytes.Repeat([]byte("V"), 64)
	mdatAt := uint64(len(isoFtyp) + 8)
	in := simpleTail("stco", pl, []uint64{mdatAt, 1 << 12}, []int{32, 32})
	roomy := append(make([]byte, 0, 1<<14), in...)
	if _, err := ichunks(roomy); err == nil {
		t.Error("a chunk past the end was read from the spare capacity")
	}
}

func vtrak(handler string, stbl []byte, extra ...[]byte) []byte {
	minf := bx("minf", bx("dinf", fb("dref", 0, 0, u32(1), fb("url ", 0, 1))), stbl)
	mdia := bx("mdia", fb("mdhd", 0, 0, make([]byte, 20)),
		fb("hdlr", 0, 0, u32(0), []byte(handler), make([]byte, 12), []byte("h\x00")), minf)
	return bx("trak", cat(fb("tkhd", 0, 3, make([]byte, 80)), mdia, cat(extra...)))
}

func stblOf(co string, offs []uint64, sizes []int) []byte {
	var ob, sb []byte
	for _, o := range offs {
		if co == "co64" {
			ob = append(ob, u64(o)...)
		} else {
			ob = append(ob, u32(int(uint32(o)))...)
		}
	}
	for _, s := range sizes {
		sb = append(sb, u32(s)...)
	}
	return bx("stbl", fb("stsd", 0, 0, u32(0)), fb("stts", 0, 0, u32(0)),
		fb("stsc", 0, 0, u32(1), u32(1), u32(1), u32(1)),
		fb("stsz", 0, 0, u32(0), u32(len(sizes)), sb),
		fb(co, 0, 0, u32(len(offs)), ob))
}

// stblFixed is a table whose stsz gives one size for every sample.
func stblFixed(offs []uint64, size int) []byte {
	var ob []byte
	for _, o := range offs {
		ob = append(ob, u32(int(uint32(o)))...)
	}
	return bx("stbl", fb("stsd", 0, 0, u32(0)), fb("stts", 0, 0, u32(0)),
		fb("stsc", 0, 0, u32(1), u32(1), u32(1), u32(1)),
		fb("stsz", 0, 0, u32(size), u32(len(offs))),
		fb("stco", 0, 0, u32(len(offs)), ob))
}

var isoFtyp = bx("ftyp", []byte("isom"), u32(512), []byte("isom"))

func moovOf(tr ...[]byte) []byte {
	return bx("moov", cat(fb("mvhd", 0, 0, make([]byte, 96)), cat(tr...)))
}

// simpleTail: ftyp, mdat(payload), moov with one picture track.
func simpleTail(co string, payload []byte, offs []uint64, sizes []int) []byte {
	return cat(isoFtyp, bx("mdat", payload), moovOf(vtrak("vide", stblOf(co, offs, sizes))))
}

func mustRefuse(t *testing.T, name string, in []byte) {
	t.Helper()
	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		defer func() {
			if r := recover(); r != nil {
				err = nil
				out = []byte(fmt.Sprintf("PANIC %v", r))
			}
			close(done)
		}()
		out, _, err = StripBytesWith(in, Options{DropText: true})
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Errorf("%s: no answer in 20s", name)
		return
	}
	if err == nil {
		t.Errorf("%s: written (%d bytes, %.40q), want refused", name, len(out), out)
	}
}

// edgeInputs are files whose offsets or sizes point where they must not.
func edgeInputs() map[string][]byte {
	pl := bytes.Repeat([]byte("V"), 64)
	mdatAt := uint64(len(isoFtyp) + 8)
	good := simpleTail("stco", pl, []uint64{mdatAt, mdatAt + 32}, []int{32, 32})
	big := func(typ string, size uint64, body []byte) []byte {
		return cat(u32(1), []byte(typ), u64(size), body)
	}
	cases := map[string][]byte{
		"largesize 2^64-1":           cat(isoFtyp, big("mdat", ^uint64(0), pl)),
		"largesize 2^63":             cat(isoFtyp, big("mdat", 1<<63, pl)),
		"largesize 15":               cat(isoFtyp, big("mdat", 15, pl)),
		"largesize 8":                cat(isoFtyp, big("moov", 8, nil)),
		"size 2":                     cat(isoFtyp, u32(2), []byte("mdat"), pl),
		"size 7":                     cat(isoFtyp, u32(7), []byte("moov")),
		"moov size 0 then more":      cat(isoFtyp, bx("mdat", pl), u32(0), []byte("moov"), fb("mvhd", 0, 0, make([]byte, 96))),
		"offset past the end":        simpleTail("stco", pl, []uint64{mdatAt, 1 << 30}, []int{32, 32}),
		"offset 2^32-1":              simpleTail("stco", pl, []uint64{mdatAt, 0xFFFFFFFF}, []int{32, 32}),
		"co64 2^64-1":                simpleTail("co64", pl, []uint64{mdatAt, ^uint64(0)}, []int{32, 32}),
		"co64 2^63":                  simpleTail("co64", pl, []uint64{mdatAt, 1 << 63}, []int{32, 32}),
		"co64 wraps with its length": simpleTail("co64", pl, []uint64{mdatAt, ^uint64(0) - 15}, []int{32, 32}),
		"chunk in the mdat header":   simpleTail("stco", pl, []uint64{mdatAt - 8, mdatAt + 32}, []int{32, 32}),
		"chunk past the mdat end":    simpleTail("stco", pl, []uint64{mdatAt, mdatAt + 40}, []int{32, 32}),
		"chunk in ftyp":              simpleTail("stco", pl, []uint64{0, mdatAt + 32}, []int{8, 32}),
		"chunk in moov":              simpleTail("stco", pl, []uint64{mdatAt, mdatAt + 64 + 8}, []int{32, 8}),
		"sample size 2^32-1":         simpleTail("stco", pl, []uint64{mdatAt, mdatAt + 32}, []int{-1, 32}),
		"dropped track past the end": cat(isoFtyp, bx("mdat", pl), moovOf(
			vtrak("vide", stblOf("stco", []uint64{mdatAt}, []int{32})),
			vtrak("meta", stblOf("co64", []uint64{1 << 40}, []int{32})))),
		"dropped track over a kept one": cat(isoFtyp, bx("mdat", pl), moovOf(
			vtrak("vide", stblOf("stco", []uint64{mdatAt}, []int{32})),
			vtrak("meta", stblOf("stco", []uint64{mdatAt + 31}, []int{4})))),
		"dropped track on the mdat header": cat(isoFtyp, bx("mdat", pl), moovOf(
			vtrak("vide", stblOf("stco", []uint64{mdatAt}, []int{32})),
			vtrak("meta", stblOf("stco", []uint64{mdatAt - 4}, []int{4})))),
		"two moov":        cat(good, moovOf(vtrak("vide", stblOf("stco", nil, nil)))),
		"moov in moov":    cat(isoFtyp, bx("mdat", pl), bx("moov", moovOf(vtrak("vide", stblOf("stco", []uint64{mdatAt}, []int{32}))))),
		"cmov":            cat(isoFtyp, bx("mdat", pl), bx("moov", bx("cmov", pl))),
		"mvhd version 9":  cat(isoFtyp, bx("mdat", pl), bx("moov", fb("mvhd", 9, 0, make([]byte, 96)), vtrak("vide", stblOf("stco", []uint64{mdatAt}, []int{32})))),
		"trailing byte":   append(append([]byte(nil), good...), 0),
		"cut mid mdat":    good[:len(isoFtyp)+20],
		"empty":           nil,
		"8 zero bytes":    make([]byte, 8),
		"mdat only, size": cat(isoFtyp, u32(0), []byte("mdat"), pl),
	}
	s := stblOf("stco", []uint64{mdatAt}, []int{32})
	s = bx("stbl", append(append([]byte(nil), s[8:]...), fb("stz2", 0, 0, u32(0))...))
	cases["stz2"] = cat(isoFtyp, bx("mdat", pl), moovOf(vtrak("vide", s)))
	return cases
}

func TestOffsetsAndSizesOutsideTheirBoxesAreRefused(t *testing.T) {
	pl := bytes.Repeat([]byte("V"), 64)
	mdatAt := uint64(len(isoFtyp) + 8)
	good := simpleTail("stco", pl, []uint64{mdatAt, mdatAt + 32}, []int{32, 32})
	if out, _, err := StripBytes(good); err != nil {
		t.Fatalf("the well-formed file is refused: %v", err)
	} else if err := keptChunksSame(good, out); err != nil {
		t.Fatal(err)
	}
	for name, in := range edgeInputs() {
		mustRefuse(t, name, in)
	}
}

// Deep nesting and long runs of boxes: answered in bounded time.
func TestDeepAndWideIndexesAnswerQuickly(t *testing.T) {
	const levels = 100000
	deep := make([]byte, 0, 12*levels)
	for i := 0; i < levels; i++ {
		deep = append(append(append(deep, u32(12*(levels-i))...), "meta"...), 0, 0, 0, 0)
	}
	deepT := make([]byte, 0, 8*levels)
	for i := 0; i < levels; i++ {
		deepT = append(append(deepT, u32(8*(levels-i))...), []string{"trak", "mdia", "minf", "stbl", "moov", "udta"}[i%6]...)
	}
	for name, in := range map[string][]byte{
		"deep meta at the top": cat(isoFtyp, deep),
		"deep meta in moov":    cat(isoFtyp, bx("moov", deep)),
		"deep containers":      cat(isoFtyp, bx("moov", deepT)),
		"500k empty traks":     cat(isoFtyp, bx("moov", bytes.Repeat(bx("trak"), 500000))),
		"200k top free boxes":  cat(isoFtyp, bytes.Repeat(bx("free"), 200000), bx("moov")),
		"deep meta in a HEIC":  cat(bx("ftyp", []byte("heic"), u32(0), []byte("mif1")), deep),
	} {
		t0 := time.Now()
		mustRefuse(t, name, in)
		if d := time.Since(t0); d > 5*time.Second {
			t.Errorf("%s: %v", name, d)
		}
	}
}

// allocDuring is how many bytes f allocates.
func allocDuring(f func()) uint64 {
	runtime.GC()
	var a, b runtime.MemStats
	runtime.ReadMemStats(&a)
	f()
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc
}

// A file under the app's 25 MiB cap whose index lists millions of chunks,
// items or extents is refused before they are held: what Strip allocates
// stays near the index's own size.
func TestIndexCountsAreBoundedBeforeAllocating(t *testing.T) {
	const fileCap = 25 << 20
	limit := func(in []byte) uint64 { return uint64(len(in)) + 8<<20 }

	// One picture track whose stco lists as many empty chunks as fit.
	{
		n := (fileCap - 4096) / 4
		mdatAt := uint32(len(isoFtyp) + 8)
		ob := make([]byte, 4*n)
		for i := 0; i < n; i++ {
			binary.BigEndian.PutUint32(ob[4*i:], mdatAt)
		}
		stbl := bx("stbl", fb("stsd", 0, 0, u32(0)),
			fb("stsc", 0, 0, u32(1), u32(1), u32(0), u32(1)),
			fb("stsz", 0, 0, u32(1), u32(0)),
			fb("stco", 0, 0, u32(n), ob))
		in := cat(isoFtyp, bx("mdat", []byte("x")), moovOf(vtrak("vide", stbl)))
		var err error
		a := allocDuring(func() { _, _, err = StripBytes(in) })
		if !errors.Is(err, ErrRefused) || a > limit(in) {
			t.Errorf("video, %d chunks in %d bytes: %v, %d MiB allocated", n, len(in), err, a>>20)
		}
	}
	// iloc version 2 with as many items (no extents) as fit.
	{
		n := (fileCap - 4096) / 10
		ents := make([]byte, 10*n)
		for i := 0; i < n; i++ {
			binary.BigEndian.PutUint32(ents[10*i:], uint32(i+10))
		}
		meta := fb("meta", 0, 0,
			fb("hdlr", 0, 0, u32(0), []byte("pict"), make([]byte, 12), []byte("\x00")),
			fb("pitm", 0, 0, u16(1)),
			fb("iloc", 2, 0, []byte{0x44, 0x00}, u32(n), ents))
		in := cat(bx("ftyp", []byte("heic"), u32(0), []byte("mif1heic")), meta, bx("mdat", []byte("x")))
		var err error
		a := allocDuring(func() { _, _, err = StripBytes(in) })
		if !errors.Is(err, ErrRefused) || a > limit(in) {
			t.Errorf("photo, %d items in %d bytes: %v, %d MiB allocated", n, len(in), err, a>>20)
		}
	}
	// Items of 65535 extents each, as many as fit.
	{
		ne := 65535
		itemLen := 6 + ne*4
		n := (fileCap - 4096) / itemLen
		one := make([]byte, itemLen)
		var ents []byte
		for i := 0; i < n; i++ {
			binary.BigEndian.PutUint16(one[0:], uint16(i+10))
			binary.BigEndian.PutUint16(one[4:], uint16(ne))
			ents = append(ents, one...)
		}
		meta := fb("meta", 0, 0,
			fb("hdlr", 0, 0, u32(0), []byte("pict"), make([]byte, 12), []byte("\x00")),
			fb("pitm", 0, 0, u16(1)),
			fb("iloc", 0, 0, []byte{0x04, 0x00}, u16(n), ents))
		in := cat(bx("ftyp", []byte("heic"), u32(0), []byte("mif1heic")), meta, bx("mdat", []byte("x")))
		var err error
		a := allocDuring(func() { _, _, err = StripBytes(in) })
		if !errors.Is(err, ErrRefused) || a > limit(in) {
			t.Errorf("photo, %d items x %d extents in %d bytes: %v, %d MiB allocated", n, ne, len(in), err, a>>20)
		}
	}
	// Below the bounds a long table is still cleaned.
	{
		n := 100000
		mdatAt := len(isoFtyp) + 8
		offs := make([]uint64, n)
		sizes := make([]int, n)
		for i := range offs {
			offs[i], sizes[i] = uint64(mdatAt+i), 1
		}
		in := simpleTail("stco", bytes.Repeat([]byte("v"), n), offs, sizes)
		if _, _, err := StripBytes(in); err != nil {
			t.Errorf("%d one-byte chunks: %v", n, err)
		}
	}
}

// Bytes in idat that a removed item and a kept item both claim: the file is
// refused, not written with the kept item's bytes cleared.
func TestIdatItemsThatShareBytesAreRefused(t *testing.T) {
	grid := []byte("GRID-DERIVED-ITEM-DATA!!")
	infe := func(id int, typ string) []byte {
		return fb("infe", 2, 0, u16(id), u16(0), []byte(typ), []byte("\x00"))
	}
	img := pad("IMAGE", 16)
	build := func(mdatPayload, exifOff int) []byte {
		ent := func(id, cm, off, n int) []byte { return cat(u16(id), u16(cm), u16(0), u16(1), u32(off), u32(n)) }
		iloc := fb("iloc", 1, 0, []byte{0x44, 0x00}, u16(3),
			ent(1, 0, mdatPayload, len(img)),
			ent(2, 1, 0, len(grid)),
			ent(3, 1, exifOff, 8))
		return fb("meta", 0, 0,
			fb("hdlr", 0, 0, u32(0), []byte("pict"), make([]byte, 12), []byte("\x00")),
			fb("pitm", 0, 0, u16(2)),
			iloc,
			fb("iinf", 0, 0, u16(3), infe(1, "hvc1"), infe(2, "grid"), infe(3, "Exif")),
			fb("iref", 0, 0, bx("dimg", u16(2), u16(1), u16(1))),
			bx("idat", grid, []byte("EXIF-GPS")))
	}
	ftyp := bx("ftyp", []byte("heic"), u32(0), []byte("mif1heic"))
	file := func(exifOff int) []byte {
		m := build(0, exifOff)
		return cat(ftyp, build(len(ftyp)+len(m)+8, exifOff), bx("mdat", img))
	}
	if out, _, err := StripBytes(file(4)); err == nil {
		d, _ := titems(t, out)
		t.Fatalf("written; the kept item reads %q", d[2])
	}
	// Apart, the removed item's bytes are cleared and the kept one's stay.
	out, _ := strip(t, file(len(grid)))
	d, _ := titems(t, out)
	if !bytes.Equal(d[2], grid) {
		t.Errorf("kept item reads %q", d[2])
	}
	noSecrets(t, out, []string{"EXIF-GPS"})
}

// Names in a video's index (handler names, the compressor name, boxes a
// vendor adds to a sample entry, to stbl or to gmhd) do not survive, and
// bytes in mdat that no kept track points at are cleared.
func TestVideoIndexKeepsNoDeviceNames(t *testing.T) {
	pl := cat(bytes.Repeat([]byte("V"), 32), []byte("ORPHAN-GPS+48.85+2.29-ORPHAN"))
	mdatAt := uint64(len(isoFtyp) + 8)
	comp := make([]byte, 32)
	copy(comp, "\x11SECRETCAM AVC enc")
	entry := bx("avc1", make([]byte, 6), u16(1), make([]byte, 16), u16(64), u16(48), u32(0x480000), u32(0x480000), u32(0), u16(1), comp, u16(24), u16(0xffff),
		bx("avcC", []byte("CODEC-CONFIG")),
		bx("uuid", bytes.Repeat([]byte{7}, 16), []byte("STSD-UUID-SECRET")),
		bx("udta", []byte("STSD-UDTA-SECRET")))
	stbl := bx("stbl", fb("stsd", 0, 0, u32(1), entry), fb("stts", 0, 0, u32(0)),
		fb("stsc", 0, 0, u32(1), u32(1), u32(1), u32(1)),
		fb("stsz", 0, 0, u32(0), u32(1), u32(32)),
		fb("stco", 0, 0, u32(1), u32(int(mdatAt))),
		bx("udta", []byte("STBL-UDTA-SECRET")),
		bx("uuid", bytes.Repeat([]byte{9}, 16), []byte("STBL-UUID-SECRET")))
	build := func(dref []byte) []byte {
		minf := bx("minf", fb("vmhd", 0, 1, make([]byte, 8)),
			bx("dinf", fb("dref", 0, 0, u32(1), dref)),
			bx("gmhd", fb("gmin", 0, 0, make([]byte, 12)), bx("udta", []byte("GMHD-SECRET"))),
			fb("hdlr", 0, 0, u32(0), []byte("dhlr"), make([]byte, 12), []byte("\x0eMINF-HDLR-NAME")),
			stbl)
		mdia := bx("mdia", fb("mdhd", 0, 0, make([]byte, 20)),
			fb("hdlr", 0, 0, u32(0), []byte("vide"), make([]byte, 12), []byte("HDLRNAME-SECRETCAM AVC\x00")), minf)
		return cat(isoFtyp, bx("mdat", pl), moovOf(bx("trak", fb("tkhd", 0, 3, make([]byte, 80)), mdia)))
	}
	in := build(fb("url ", 0, 1))
	out, _ := strip(t, in)
	noSecrets(t, out, []string{"HDLRNAME-SECRETCAM", "MINF-HDLR-NAME", "SECRETCAM AVC enc", "STSD-UUID-SECRET", "STSD-UDTA-SECRET", "STBL-UDTA-SECRET", "STBL-UUID-SECRET", "GMHD-SECRET", "ORPHAN-GPS"})
	if !bytes.Contains(out, []byte("CODEC-CONFIG")) {
		t.Error("the codec configuration went too")
	}
	if err := keptChunksSame(in, out); err != nil {
		t.Error(err)
	}
	mustRefuse(t, "data reference with a path", build(fb("alis", 0, 1, []byte("/Volumes/DREF-PATH-SECRET/clip.mov"))))
}

// A track whose stsz gives one size for all samples does not prove its
// chunk lengths: the file is cleaned only when no mdat byte is left
// unaccounted for.
func TestUnprovenChunkLengthsWithUnlistedBytesAreRefused(t *testing.T) {
	mdatAt := uint64(len(isoFtyp) + 8)
	exact := cat(isoFtyp, bx("mdat", bytes.Repeat([]byte("S"), 32)),
		moovOf(vtrak("soun", stblFixed([]uint64{mdatAt, mdatAt + 16}, 16))))
	strip(t, exact)
	extra := cat(isoFtyp, bx("mdat", bytes.Repeat([]byte("S"), 32), []byte("UNLISTED")),
		moovOf(vtrak("soun", stblFixed([]uint64{mdatAt, mdatAt + 16}, 16))))
	mustRefuse(t, "fixed sizes and unlisted bytes", extra)
	// With per-sample sizes the same bytes are simply cleared.
	listed := cat(isoFtyp, bx("mdat", bytes.Repeat([]byte("S"), 32), []byte("UNLISTED")),
		moovOf(vtrak("soun", stblOf("stco", []uint64{mdatAt, mdatAt + 16}, []int{16, 16}))))
	out, _ := strip(t, listed)
	noSecrets(t, out, []string{"UNLISTED"})
}

// fragShrinks is a fragmented file whose rebuilt moov is shrink bytes
// shorter than the original (its keys box has that many bytes after the
// last key).
func fragShrinks(shrink int) []byte {
	key := cat(u32(8+len("com.android.capture.fps")), []byte("mdta"), []byte("com.android.capture.fps"))
	item := cat(u32(8+16+4), u32(1), u32(16+4), []byte("data"), u32(1), u32(0), []byte("60.0"))
	meta := bx("meta", fb("hdlr", 0, 0, u32(0), []byte("mdta"), make([]byte, 12), []byte("\x00")),
		fb("keys", 0, 0, u32(1), key, make([]byte, shrink)), bx("ilst", item))
	moov := moovOf(vtrak("vide", stblOf("stco", nil, nil)), meta, bx("mvex", fb("trex", 0, 0, u32(0), make([]byte, 16))))
	return cat(isoFtyp, moov, synthFragment(0, []byte("FRAGMENT-SAMPLES")))
}

// In a fragmented file every byte keeps its place: a moov that shrinks by
// less than a box header cannot be padded, and is refused.
func TestFragmentedMoovThatCannotBePaddedIsRefused(t *testing.T) {
	out, _ := strip(t, fragShrinks(0))
	if !bytes.Contains(out, []byte("FRAGMENT-SAMPLES")) {
		t.Fatal("the fragment's samples are gone")
	}
	for _, n := range []int{1, 4, 7} {
		mustRefuse(t, fmt.Sprintf("moov %d bytes shorter", n), fragShrinks(n))
	}
	out, _ = strip(t, fragShrinks(8))
	if len(out) != len(fragShrinks(8)) {
		t.Fatal("length changed")
	}
}

// What a fragment holds is known or the file is refused: its runs' samples
// are kept, the rest of its mdat is cleared, emsg becomes zeros, and a moof
// or traf with anything else is refused.
func TestFragmentsKeepOnlyTheirSamples(t *testing.T) {
	base := synthVideo(vopts{moovFirst: true, frag: true})
	// A fragment of track 1 whose mdat has bytes after its run.
	moof := func(dataOff int, extra ...[]byte) []byte {
		return bx("moof", fb("mfhd", 0, 0, u32(2)),
			bx("traf", cat(fb("tfhd", 0, 0x20000, u32(1)),
				fb("trun", 0, 0x201, u32(1), u32(dataOff), u32(12)), cat(extra...))))
	}
	frag := func(extra ...[]byte) []byte {
		m := moof(0, extra...)
		return cat(moof(len(m)+8, extra...), bx("mdat", []byte("RUN-SAMPLES!"), []byte("UNLISTED-IN-FRAGMENT")))
	}
	in := cat(base, bx("emsg", []byte("EMSG-SECRET")), frag())
	out, _ := strip(t, in)
	if len(out) != len(in) || bytes.Index(out, []byte("RUN-SAMPLES!")) != bytes.Index(in, []byte("RUN-SAMPLES!")) {
		t.Fatal("the run's samples moved or went")
	}
	noSecrets(t, out, []string{"EMSG-SECRET", "UNLISTED-IN-FRAGMENT"})

	mustRefuse(t, "udta in moof", cat(base, bx("moof", fb("mfhd", 0, 0, u32(2)), bx("udta", []byte("FRAG-XYZ")))))
	mustRefuse(t, "meta in traf", cat(base, frag(bx("meta", []byte("FRAG-META")))))
	mustRefuse(t, "saio in traf", cat(base, frag(fb("saio", 0, 0, u32(1), u32(0)))))
	other := bx("moof", fb("mfhd", 0, 0, u32(2)), bx("traf", fb("tfhd", 0, 0x20000, u32(9)), fb("trun", 0, 0x201, u32(1), u32(0), u32(1))))
	mustRefuse(t, "a track the moov does not have", cat(base, other, bx("mdat", []byte("x"))))
	past := bx("moof", fb("mfhd", 0, 0, u32(2)), bx("traf", fb("tfhd", 0, 0x20000, u32(1)), fb("trun", 0, 0x201, u32(1), u32(64), u32(100))))
	mustRefuse(t, "a run past its mdat", cat(base, past, bx("mdat", []byte("x"))))
}

// HEIF: what is left after its metadata items go carries nothing more: no
// item names, no handler name, no description or vendor properties, no
// bytes in mdat that no kept item uses.
func TestHEICKeepsOnlyWhatItsItemsUse(t *testing.T) {
	infe := func(id int, typ, name string) []byte {
		return fb("infe", 2, 0, u16(id), u16(0), []byte(typ), []byte(name+"\x00"))
	}
	img := pad("IMAGE", 16)
	orphan := []byte("ORPHAN-EXIF-GPS+48.85")
	noinfe := []byte("NOINFE-EXIF-GPS+48.85")
	build := func(mp int, unnamed bool, essential byte) []byte {
		ent := func(id, off, n int) []byte { return cat(u16(id), u16(0), u16(0), u16(1), u32(off), u32(n)) }
		ents := [][]byte{ent(1, mp, len(img))}
		if unnamed {
			ents = append(ents, ent(7, mp+len(img)+len(orphan), len(noinfe)))
		}
		iloc := fb("iloc", 1, 0, []byte{0x44, 0x00}, u16(len(ents)), cat(ents...))
		return fb("meta", 0, 0,
			fb("hdlr", 0, 0, u32(0), []byte("pict"), make([]byte, 12), []byte("HEIF-HDLR-NAME-SECRET\x00")),
			fb("pitm", 0, 0, u16(1)),
			iloc,
			fb("iinf", 0, 0, u16(1), infe(1, "hvc1", "ITEMNAME-IMG_4032-SECRET")),
			bx("iprp",
				bx("ipco", fb("ispe", 0, 0, u32(64), u32(48)),
					fb("udes", 0, 0, []byte("en\x00UDES-TITLE-SECRET\x00home\x00tags\x00")),
					bx("uuid", bytes.Repeat([]byte{3}, 16), []byte("IPCO-UUID-SECRET"))),
				fb("ipma", 0, 0, u32(1), cat(u16(1), []byte{3, 0x81, 0x02, essential}))))
	}
	ftyp := bx("ftyp", []byte("heic"), u32(0), []byte("mif1heic"))
	file := func(unnamed bool, essential byte) []byte {
		m := build(0, unnamed, essential)
		return cat(ftyp, build(len(ftyp)+len(m)+8, unnamed, essential), bx("mdat", img, orphan, noinfe))
	}
	in := file(false, 0x03)
	out, _ := strip(t, in)
	noSecrets(t, out, []string{"HEIF-HDLR-NAME-SECRET", "ITEMNAME-IMG_4032-SECRET", "UDES-TITLE-SECRET", "IPCO-UUID-SECRET", "ORPHAN-EXIF-GPS", "NOINFE-EXIF-GPS"})
	a, _ := titems(t, in)
	b, bt := titems(t, out)
	if !bytes.Equal(a[1], b[1]) || bt[1] != "hvc1" {
		t.Errorf("the picture reads %q (%q)", b[1], bt[1])
	}
	pc, _ := tfind(out, 0, len(out), "meta")
	ipco, _ := tfind(out, pc.start+pc.hdr+4, pc.end, "iprp", "ipco")
	props, _ := tchildren(out, ipco.start+ipco.hdr, ipco.end)
	if len(props) != 3 || props[0].typ != "ispe" || props[1].typ != "free" || props[2].typ != "free" {
		t.Errorf("properties %v", props)
	}
	mustRefuse(t, "an item iinf does not name", file(true, 0x03))
	mustRefuse(t, "an essential property that would go", file(false, 0x83))
}

// Sniff names ISO files that do not start with ftyp or an old QuickTime
// box, so they are cleaned or held rather than sent unread.
func TestSniffNamesISOFilesWithOtherFirstBoxes(t *testing.T) {
	loc := bx("moov", bx("udta", bx("\xa9xyz", []byte("+48.85+2.29/"))))
	for name, in := range map[string][]byte{
		"styp first":         cat(bx("styp", []byte("msdh"), u32(0)), loc),
		"uuid first":         cat(bx("uuid", make([]byte, 16)), bx("ftyp", []byte("isom"), u32(0)), loc),
		"a vendor box first": cat(bx("junk"), bx("ftyp", []byte("qt  "), u32(0)), loc),
		"PICT first":         cat(bx("PICT"), loc),
	} {
		if !Sniff(in) {
			t.Errorf("%s: not sniffed", name)
		}
		if _, _, err := StripBytes(in); err == nil {
			t.Errorf("%s: cleaned, want refused", name)
		}
	}
}

// Subtitles and text are not dropped unless the caller allows it; a
// timecode goes without asking, and does not make a file with subtitles go
// unasked either.
func TestTextTracksGoOnlyWhenAllowed(t *testing.T) {
	pl := cat(bytes.Repeat([]byte("V"), 32), []byte("SUBTITLE: hello there....."))
	mdatAt := uint64(len(isoFtyp) + 8)
	for _, h := range []string{"sbtl", "text", "subt", "clcp"} {
		in := cat(isoFtyp, bx("mdat", pl), moovOf(
			vtrak("vide", stblOf("stco", []uint64{mdatAt}, []int{32})),
			vtrak(h, stblOf("stco", []uint64{mdatAt + 32}, []int{26}))))
		if out, _, err := StripBytes(in); !errors.Is(err, ErrTextTracks) || out != nil {
			t.Errorf("%s: %v, want ErrTextTracks and no output", h, err)
		}
		out, k, err := StripBytesWith(in, Options{DropText: true})
		if err != nil || k != KindVideo {
			t.Fatalf("%s, allowed: %v %v", h, k, err)
		}
		noSecrets(t, out, []string{"SUBTITLE"})
	}
	// A timed GPS track and a timecode go without asking, their samples
	// cleared.
	for _, h := range []string{"meta", "tmcd"} {
		in := cat(isoFtyp, bx("mdat", pl), moovOf(
			vtrak("vide", stblOf("stco", []uint64{mdatAt}, []int{32})),
			vtrak(h, stblOf("stco", []uint64{mdatAt + 32}, []int{26}))))
		out, _, err := StripBytes(in)
		if err != nil {
			t.Fatalf("%s track: %v", h, err)
		}
		noSecrets(t, out, []string{"SUBTITLE"})
		if ch, _ := ichunks(out); len(ch[h]) != 0 {
			t.Errorf("%s track still in the index", h)
		}
	}
	// Subtitles beside a timecode: asked about.
	both := cat(pl[:len(pl)-4], []byte("TC01"))
	in := cat(isoFtyp, bx("mdat", both), moovOf(
		vtrak("vide", stblOf("stco", []uint64{mdatAt}, []int{32})),
		vtrak("sbtl", stblOf("stco", []uint64{mdatAt + 32}, []int{22})),
		vtrak("tmcd", stblOf("stco", []uint64{mdatAt + 54}, []int{4}))))
	if _, _, err := StripBytes(in); !errors.Is(err, ErrTextTracks) {
		t.Errorf("subtitles and timecode: %v, want ErrTextTracks", err)
	}
	if out, _, err := StripBytesWith(in, Options{DropText: true}); err != nil {
		t.Errorf("subtitles and timecode, allowed: %v", err)
	} else {
		noSecrets(t, out, []string{"SUBTITLE", "TC01"})
	}
	// A file that cannot be cleaned is refused as such, not asked about.
	bad := cat(isoFtyp, bx("mdat", pl), moovOf(
		vtrak("vide", stblOf("stco", []uint64{mdatAt}, []int{32})),
		vtrak("sbtl", stblOf("stco", []uint64{mdatAt + 16}, []int{26}))))
	if _, _, err := StripBytes(bad); !errors.Is(err, ErrRefused) {
		t.Errorf("subtitles over the picture: %v, want ErrRefused", err)
	}
}

// audioOnly is an M4A (or, with a picture track, an MP4) with the tags a
// music app or a phone writes.
func audioOnly(sound bool) []byte {
	pl := bytes.Repeat([]byte("A"), 32)
	mdatAt := uint64(len(isoFtyp) + 8)
	tag := func(typ, v string) []byte { return bx(typ, bx("data", u32(1), u32(0), []byte(v))) }
	udta := bx("udta",
		bx("meta", u32(0), fb("hdlr", 0, 0, u32(0), []byte("mdir"), []byte("appl"), make([]byte, 8), []byte("HDLR-NAME-SECRET\x00")),
			bx("ilst",
				tag("\xa9nam", "TITLE-KEPT"), tag("\xa9ART", "ARTIST-KEPT"), tag("\xa9alb", "ALBUM-KEPT"),
				tag("\xa9day", "DAY-SECRET"), tag("\xa9xyz", "+48.85+002.29/XYZ-SECRET"), tag("\xa9too", "ENCODER-SECRET"),
				tag("\xa9cmt", "COMMENT-SECRET"), tag("covr", "COVER-SECRET"), tag("----", "FREEFORM-SECRET"))),
		bx("loci", []byte("LOCI-SECRET")), bx("XMP_", []byte("XMP-SECRET")))
	key := func(name string) []byte { return cat(u32(8+len(name)), []byte("mdta"), []byte(name)) }
	item := func(idx int, val string) []byte {
		return cat(u32(8+16+len(val)), u32(idx), u32(16+len(val)), []byte("data"), u32(1), u32(0), []byte(val))
	}
	meta := bx("meta", fb("hdlr", 0, 0, u32(0), []byte("mdta"), make([]byte, 12), []byte("\x00")),
		fb("keys", 0, 0, u32(2), key("com.apple.quicktime.location.ISO6709"), key("com.apple.quicktime.title")),
		bx("ilst", item(1, "ISO6709-VALUE-GONE"), item(2, "KEY-TITLE-KEPT")))
	h := "soun"
	if !sound {
		h = "vide"
	}
	return cat(isoFtyp, bx("mdat", pl), moovOf(vtrak(h, stblOf("stco", []uint64{mdatAt}, []int{32})), udta, meta))
}

// An audio-only file keeps its title, artist and album and nothing else of
// its tags; a video keeps none of them.
func TestAudioKeepsTitleArtistAndAlbumOnly(t *testing.T) {
	secrets := []string{"DAY-SECRET", "XYZ-SECRET", "+48.85", "ENCODER-SECRET", "COMMENT-SECRET", "COVER-SECRET", "FREEFORM-SECRET", "LOCI-SECRET", "XMP-SECRET", "HDLR-NAME-SECRET", "ISO6709-VALUE-GONE"}
	out, _ := strip(t, audioOnly(true))
	noSecrets(t, out, secrets)
	for _, s := range []string{"TITLE-KEPT", "ARTIST-KEPT", "ALBUM-KEPT", "KEY-TITLE-KEPT"} {
		if !bytes.Contains(out, []byte(s)) {
			t.Errorf("%s went", s)
		}
	}
	if _, ok := tfind(out, 0, len(out), "moov", "udta", "meta"); !ok {
		t.Error("no udta/meta")
	}
	out, _ = strip(t, audioOnly(false))
	noSecrets(t, out, append(secrets, "TITLE-KEPT", "ARTIST-KEPT", "ALBUM-KEPT", "KEY-TITLE-KEPT"))
}

// Several mdat boxes with dropped boxes before, between and after them, moov
// in the middle, one mdat with a 64-bit header: every kept chunk holds the
// same bytes and a dropped track's bytes are gone.
func TestSeveralMdatBoxesKeepTheirChunks(t *testing.T) {
	for _, co := range []string{"stco", "co64"} {
		uu := bx("uuid", make([]byte, 16), []byte("XMP-GPS-SECRET"))
		a := []byte("VIDEO-CHUNK-A...GPSTRACK-SECRET-1AUDIO-CHUNK-A.")
		b := []byte("AUDIO-CHUNK-B...VIDEO-CHUNK-B.GPSTRACK-SECRET-2")
		c := []byte("VIDEO-CHUNK-C.....")
		m1 := bx("mdat", a)
		m2 := cat(u32(1), []byte("mdat"), u64(uint64(16+len(b))), b)
		m3 := cat(u32(0), []byte("mdat"), c)
		build := func(m1at, m2at, m3at uint64) []byte {
			return moovOf(
				vtrak("vide", stblOf(co, []uint64{m1at + 8, m2at + 16 + 16, m3at + 8}, []int{16, 14, 18})),
				vtrak("meta", stblOf(co, []uint64{m1at + 8 + 16, m2at + 16 + 30}, []int{17, 17})),
				vtrak("soun", stblOf(co, []uint64{m1at + 8 + 33, m2at + 16}, []int{14, 16})),
				bx("udta", bx("\xa9xyz", []byte("+48.85+2.29/"))))
		}
		mv := build(0, 0, 0)
		m1at := uint64(len(isoFtyp) + len(uu))
		m2at := m1at + uint64(len(m1)+len(uu)+len(mv)+len(uu))
		m3at := m2at + uint64(len(m2)+len(uu))
		in := cat(isoFtyp, uu, m1, uu, build(m1at, m2at, m3at), uu, m2, uu, m3)
		out, _ := strip(t, in)
		if err := keptChunksSame(in, out); err != nil {
			t.Errorf("%s: %v", co, err)
		}
		ch, _ := ichunks(out)
		if len(ch["vide"]) != 3 || string(ch["vide"][2]) != "VIDEO-CHUNK-C....." || string(ch["soun"][1]) != "AUDIO-CHUNK-B..." {
			t.Errorf("%s: chunks %q", co, ch)
		}
		noSecrets(t, out, []string{"GPSTRACK-SECRET", "XMP-GPS-SECRET", "+48.85"})
	}
}

// Cleaned copies of the files in $BMFF_IN, written to $BMFF_OUT, for checks
// with tools outside Go (ffprobe, ffmpeg framemd5, exiftool, heif-info).
// Subtitle and text tracks are dropped, as after "Remove and send".
func TestRewriteDirForOutsideTools(t *testing.T) {
	in, outd := os.Getenv("BMFF_IN"), os.Getenv("BMFF_OUT")
	if in == "" || outd == "" {
		t.Skip("BMFF_IN/BMFF_OUT not set")
	}
	ents, err := os.ReadDir(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(in, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		_, _, plainErr := StripBytes(b)
		out, kind, err := StripBytesWith(b, Options{DropText: true})
		if err != nil {
			fmt.Printf("REWRITE %s kind=%d REFUSED %v\n", e.Name(), kind, err)
			continue
		}
		chk := ""
		if kind == KindVideo {
			if err := keptChunksSame(b, out); err != nil {
				chk = " CHUNKS-DIFFER " + err.Error()
			}
		}
		if plainErr != nil {
			chk += " (asks first: " + plainErr.Error() + ")"
		}
		fmt.Printf("REWRITE %s kind=%d %d -> %d bytes%s\n", e.Name(), kind, len(b), len(out), chk)
		if err := os.WriteFile(filepath.Join(outd, e.Name()), out, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
