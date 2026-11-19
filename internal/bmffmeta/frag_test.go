package bmffmeta

// Movie fragments: files built at the edges of what the fragment reader
// accepts, and a second reader of fragment runs, written apart from the
// package, for the fuzz targets' oracle.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
)

// realFixtures are the files in testdata, all of which the fuzz targets
// start from.
var realFixtures = []string{
	"android-fast.mp4", "android-tail.mp4", "iphone-fast.mov", "iphone-tail.mov", "photo.heic",
	"tmcd.mov", "tmcd-subs.mov", "frag.mp4", "voicememo.m4a", "android.m4a", "music.m4a",
}

// fragSeeds are fragment shapes for the fuzz targets.
func fragSeeds() [][]byte {
	return [][]byte{
		fragFile(fragGood()),
		fragFile(fragOpts{tfhdFlags: 1, base: 1, trunFlags: 0x201, sizes: []int{10, 6}}),
		fragFile(fragOpts{trunFlags: 0x201, sizes: []int{10, 6}}),
		fragFile(fragOpts{tfhdFlags: 0x20000, trunFlags: 0x001, count: 2, defSize: 8}),
	}
}

// fragOpts shape a file with one fragment (fragFile).
type fragOpts struct {
	tfhdFlags, trunFlags int
	base                 uint64 // with tfhdFlags&1: 1 means "the moof's own offset"
	dataOff              int    // with trunFlags&1: from the first byte of the mdat payload
	rawDataOff           bool   // dataOff is the trun's field as it is
	count                int    // samples in the run; 0: len(sizes)
	sizes                []int
	defSize              int // the trex default sample size
	trackID              int
	trafExtra, moofExtra []byte
	noTfhd, shortTfhd    bool
	twice                bool // the trun twice
	trexTwice            bool // a second trex for the track, with another default size
}

func fragGood() fragOpts { return fragOpts{tfhdFlags: 0x20000, trunFlags: 0x201, sizes: []int{10, 6}} }

// fragFile: ftyp, moov (one picture track, id 0, empty tables, mvex/trex, a
// location in udta), moof, and an mdat with 16 media bytes and 8 bytes that
// nothing lists.
func fragFile(o fragOpts) []byte {
	ftyp := bx("ftyp", []byte("isom"), u32(512), []byte("isomiso5"))
	trex := fb("trex", 0, 0, u32(0), u32(1), u32(0), u32(o.defSize), u32(0))
	if o.trexTwice {
		trex = cat(trex, fb("trex", 0, 0, u32(0), u32(1), u32(0), u32(o.defSize+4), u32(0)))
	}
	moov := moovOf(vtrak("vide", stblOf("stco", nil, nil)),
		bx("mvex", trex),
		bx("udta", bx("\xa9xyz", []byte("+48.85+2.29/"))))
	moofAt := len(ftyp) + len(moov)
	build := func(dataOff int) []byte {
		var tf []byte
		if !o.noTfhd {
			body := u32(o.trackID)
			if o.tfhdFlags&1 != 0 {
				b := o.base
				if b == 1 {
					b = uint64(moofAt)
				}
				body = append(body, u64(b)...)
			}
			if o.shortTfhd {
				body = body[:2]
			}
			tf = fb("tfhd", 0, o.tfhdFlags, body)
		}
		cnt := o.count
		if cnt == 0 {
			cnt = len(o.sizes)
		}
		tr := u32(cnt)
		if o.trunFlags&1 != 0 {
			tr = append(tr, u32(dataOff)...)
		}
		if o.trunFlags&0x200 != 0 {
			for _, z := range o.sizes {
				tr = append(tr, u32(z)...)
			}
		}
		trun := fb("trun", 0, o.trunFlags, tr)
		if o.twice {
			trun = cat(trun, trun)
		}
		return bx("moof", fb("mfhd", 0, 0, u32(1)), bx("traf", tf, trun, o.trafExtra), o.moofExtra)
	}
	m := build(0)
	d := o.dataOff
	if !o.rawDataOff {
		d = len(m) + 8 + o.dataOff
	}
	return cat(ftyp, moov, build(d), bx("mdat", []byte("SAMPLE-ONESAMPL2ORPHANXX")))
}

// The fragment reader keeps each accepted shape's samples in place and
// refuses runs it cannot place, with the error that says why.
func TestFragmentRunEdges(t *testing.T) {
	for name, o := range map[string]fragOpts{
		"default base is the moof": fragGood(),
		"explicit base, the moof":  {tfhdFlags: 1, base: 1, trunFlags: 0x201, sizes: []int{10, 6}},
		"no flags, first traf":     {trunFlags: 0x201, sizes: []int{10, 6}},
		"sample size from trex":    {tfhdFlags: 0x20000, trunFlags: 0x001, count: 2, defSize: 8},
	} {
		in := fragFile(o)
		out, _ := strip(t, in)
		if len(out) != len(in) {
			t.Errorf("%s: length %d -> %d", name, len(in), len(out))
		}
		i := bytes.Index(in, []byte("SAMPLE-ONESAMPL2"))
		if !bytes.Equal(out[i:i+16], []byte("SAMPLE-ONESAMPL2")) {
			t.Errorf("%s: media bytes changed: %q", name, out[i:i+16])
		}
		noSecrets(t, out, []string{"ORPHANXX", "+48.85"})
		if err := fragRunsSame(in, out); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	with := func(f func(*fragOpts)) []byte { o := fragGood(); f(&o); return fragFile(o) }
	for name, in := range map[string][]byte{
		"data offset 2^31-1":        with(func(o *fragOpts) { o.rawDataOff = true; o.dataOff = 0x7fffffff }),
		"run starts in the moof":    with(func(o *fragOpts) { o.rawDataOff = true; o.dataOff = 0 }),
		"run starts in mdat header": with(func(o *fragOpts) { o.dataOff = -4 }),
		"run past mdat end":         with(func(o *fragOpts) { o.sizes = []int{10, 60} }),
		"sample size 2^32-1":        with(func(o *fragOpts) { o.sizes = []int{-1, -1} }),
		"count 2^32-1 with sizes":   with(func(o *fragOpts) { o.count = -1 }),
		"count 2^32-1 x def 2^32-1": with(func(o *fragOpts) { o.trunFlags = 1; o.count = -1; o.defSize = -1 }),
		"count 2^32-1 x def 0":      with(func(o *fragOpts) { o.trunFlags = 1; o.count = -1; o.defSize = 0 }),
		"base 2^64-1":               with(func(o *fragOpts) { o.tfhdFlags = 1; o.base = ^uint64(0) }),
		"base 2^63":                 with(func(o *fragOpts) { o.tfhdFlags = 1; o.base = 1 << 63 }),
		"uuid in traf":              with(func(o *fragOpts) { o.trafExtra = bx("uuid", make([]byte, 16), []byte("X")) }),
		"saio in traf":              with(func(o *fragOpts) { o.trafExtra = fb("saio", 0, 0, u32(1), u32(0)) }),
		"udta in moof":              with(func(o *fragOpts) { o.moofExtra = bx("udta", []byte("X")) }),
		"trun without tfhd":         with(func(o *fragOpts) { o.noTfhd = true }),
		"tfhd cut short":            with(func(o *fragOpts) { o.shortTfhd = true }),
	} {
		mustRefuse(t, name, in)
	}
	// A data offset that would place the run before the start of the file
	// is a contradiction in the index, not a run outside the media.
	in := with(func(o *fragOpts) { o.rawDataOff = true; o.dataOff = -1000000000 })
	if _, _, err := StripBytes(in); !errors.Is(err, ErrMalformed) {
		t.Errorf("data offset before the file: %v, want ErrMalformed", err)
	}
	// A run of a track the moov does not have is refused, even when its
	// bytes lie in the mdat: nothing says whether that track is kept.
	in = with(func(o *fragOpts) { o.trackID = 7; o.defSize = 0 })
	if _, _, err := StripBytes(in); !errors.Is(err, ErrRefused) {
		t.Errorf("a track the moov does not have: %v, want ErrRefused", err)
	}
	// Two runs over the same bytes: either answer will do, but an accepted
	// file keeps its media.
	in = with(func(o *fragOpts) { o.twice = true })
	if out, _, err := StripBytes(in); err == nil {
		i := bytes.Index(in, []byte("SAMPLE-ONESAMPL2"))
		if !bytes.Equal(out[i:i+16], []byte("SAMPLE-ONESAMPL2")) {
			t.Errorf("two runs: media changed")
		}
	}
}

// A real fragmented file (ffmpeg, frag_keyframe+empty_moov+default_base_moof)
// keeps every run's bytes where they were.
func TestRealFragmentedFileKeepsItsRuns(t *testing.T) {
	in := readTestdata(t, "frag.mp4")
	runs, err := ifruns(in)
	if err != nil || len(runs) < 2 {
		t.Fatalf("fixture: %d runs, %v", len(runs), err)
	}
	out, _ := strip(t, in)
	if err := fragRunsSame(in, out); err != nil {
		t.Fatal(err)
	}
}

// ifruns lists the byte ranges of every fragment run (trun) in b, read
// apart from the package. It never panics: a file it cannot read is an
// error.
func ifruns(b []byte) (runs [][2]uint64, err error) {
	defer func() {
		if r := recover(); r != nil {
			runs, err = nil, fmt.Errorf("reader: %v", r)
		}
	}()
	b = b[:len(b):len(b)]
	top, err := tchildren(b, 0, len(b))
	if err != nil {
		return nil, err
	}
	be := binary.BigEndian
	trex := map[uint32]uint64{}
	if mvex, ok := tfind(b, 0, len(b), "moov", "mvex"); ok {
		kids, err := tchildren(b, mvex.start+mvex.hdr, mvex.end)
		if err != nil {
			return nil, err
		}
		for _, k := range kids {
			if k.typ == "trex" {
				p := b[k.start+k.hdr : k.end]
				if _, dup := trex[be.Uint32(p[4:])]; dup {
					return nil, errTwoTables
				}
				trex[be.Uint32(p[4:])] = uint64(be.Uint32(p[16:]))
			}
		}
	}
	for _, m := range top {
		if m.typ != "moof" {
			continue
		}
		trafs, err := tchildren(b, m.start+m.hdr, m.end)
		if err != nil {
			return nil, err
		}
		prev := uint64(m.start)
		first := true
		for _, tr := range trafs {
			if tr.typ != "traf" {
				continue
			}
			kids, err := tchildren(b, tr.start+tr.hdr, tr.end)
			if err != nil {
				return nil, err
			}
			var base, def, next uint64
			started := false
			for _, k := range kids {
				p := b[k.start+k.hdr : k.end]
				fl := be.Uint32(p) & 0xFFFFFF
				switch k.typ {
				case "tfhd":
					id := be.Uint32(p[4:])
					q := p[8:]
					switch {
					case fl&1 != 0:
						base, q = be.Uint64(q), q[8:]
					case fl&0x20000 != 0 || first:
						base = uint64(m.start)
					default:
						base = prev
					}
					def = trex[id]
					if fl&2 != 0 {
						q = q[4:]
					}
					if fl&8 != 0 {
						q = q[4:]
					}
					if fl&0x10 != 0 {
						def = uint64(be.Uint32(q))
					}
					next = base
				case "trun":
					n := uint64(be.Uint32(p[4:]))
					q := p[8:]
					at := next
					if !started {
						at = base
					}
					if fl&1 != 0 {
						at = uint64(int64(base) + int64(int32(be.Uint32(q))))
						q = q[4:]
					}
					if fl&4 != 0 {
						q = q[4:]
					}
					var size uint64
					for i := uint64(0); i < n; i++ {
						if fl&0x100 != 0 {
							q = q[4:]
						}
						if fl&0x200 != 0 {
							size += uint64(be.Uint32(q))
							q = q[4:]
						} else {
							size += def
						}
						if fl&0x400 != 0 {
							q = q[4:]
						}
						if fl&0x800 != 0 {
							q = q[4:]
						}
					}
					runs = append(runs, [2]uint64{at, size})
					next = at + size
					prev = next
					started = true
				}
			}
			if !started {
				prev = base
			}
			first = false
		}
	}
	return runs, nil
}

// fragRunsSame: every fragment run of in has the same bytes at the same place
// in out. A file the second reader cannot read is not judged.
func fragRunsSame(in, out []byte) error {
	runs, err := ifruns(in)
	if errors.Is(err, errTwoTables) {
		return fmt.Errorf("input accepted: %v", err)
	}
	if err != nil || len(runs) == 0 {
		return nil
	}
	if len(out) != len(in) {
		return fmt.Errorf("fragmented file changed length %d -> %d", len(in), len(out))
	}
	for i, r := range runs {
		if r[0] > uint64(len(in)) || r[1] > uint64(len(in))-r[0] {
			return nil // a run the reader places outside the file: it cannot say
		}
		if !bytes.Equal(in[r[0]:r[0]+r[1]], out[r[0]:r[0]+r[1]]) {
			return fmt.Errorf("fragment run %d (%d bytes at %d) changed", i, r[1], r[0])
		}
	}
	return nil
}
