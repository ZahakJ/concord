package bmffmeta

// Test files built box by box, and a reader of them written apart from the
// package, so a test does not grade the stripper with the stripper.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

func u16(v int) []byte { return binary.BigEndian.AppendUint16(nil, uint16(v)) }
func u32(v int) []byte {
	return /* -1 is 0xffffffff, for 32-bit builds */ binary.BigEndian.AppendUint32(nil, uint32(v))
}
func u64(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }

func cat(ps ...[]byte) []byte {
	var b []byte
	for _, p := range ps {
		b = append(b, p...)
	}
	return b
}

func bx(typ string, ps ...[]byte) []byte {
	body := cat(ps...)
	return cat(u32(8+len(body)), []byte(typ), body)
}

// fb is a full box: version and flags first.
func fb(typ string, ver byte, flags int, ps ...[]byte) []byte {
	return bx(typ, append([]byte{ver, byte(flags >> 16), byte(flags >> 8), byte(flags)}, cat(ps...)...))
}

func pad(s string, n int) []byte {
	b := []byte(s)
	for len(b) < n {
		b = append(b, '.')
	}
	return b[:n]
}

// Planted strings: none may survive.
var videoSecrets = []string{
	"SECRET-XYZ", "SECRET-LOCI", "SECRET-MAKE", "SECRET-DAY", "SECRET-ISO6709",
	"SECRET-KEYMAKE", "SECRET-XMP", "SECRET-FREE", "SECRET-TRAK", "GPS-SECRET",
	"SECRET-T", "SECRET-TOPMETA",
}

type vopts struct {
	moovFirst, co64, frag, gps bool
}

type tchunk struct {
	handler string
	samples []int // sizes
	tag     string
}

// synthVideo is a phone-shaped video: a picture track and a sound track whose
// chunks are interleaved in mdat, optionally a timed GPS track among them,
// and every place a phone or an editor leaves location and device details.
func synthVideo(o vopts) []byte {
	type trk struct {
		handler string
		chunks  [][]byte // sample bytes, one slice per sample... flattened below
		sizes   [][]int
	}
	handlers := []string{"vide", "soun"}
	if o.gps {
		handlers = append(handlers, "meta")
	}
	trks := make([]*trk, len(handlers))
	for i, h := range handlers {
		trks[i] = &trk{handler: h}
	}
	// Four rounds of interleaved chunks.
	var mdatBody []byte
	chunkAt := make([][]int, len(trks)) // offsets within mdat payload
	for round := 0; round < 4; round++ {
		for ti, t := range trks {
			var sizes []int
			var data []byte
			switch t.handler {
			case "vide":
				sizes = []int{20 + round, 30}
				data = cat(pad(fmt.Sprintf("VID-%d-a", round), sizes[0]), pad(fmt.Sprintf("VID-%d-b", round), sizes[1]))
			case "soun":
				sizes = []int{16}
				data = pad(fmt.Sprintf("AUD-%d", round), 16)
			case "meta":
				sizes = []int{24}
				data = pad(fmt.Sprintf("GPS-SECRET-%d+48.85+2.29", round), 24)
			}
			chunkAt[ti] = append(chunkAt[ti], len(mdatBody))
			t.sizes = append(t.sizes, sizes)
			mdatBody = append(mdatBody, data...)
		}
	}
	mdat := bx("mdat", mdatBody)

	buildMoov := func(mdatPayload int) []byte {
		var traks [][]byte
		for ti, t := range trks {
			var stsz []byte
			n := 0
			for _, s := range t.sizes {
				for _, z := range s {
					stsz = append(stsz, u32(z)...)
					n++
				}
			}
			// stsc: one run per distinct samples-per-chunk.
			var stsc []byte
			runs := 0
			prev := -1
			for c, s := range t.sizes {
				if len(s) != prev {
					stsc = append(stsc, cat(u32(c+1), u32(len(s)), u32(1))...)
					runs++
					prev = len(s)
				}
			}
			var offs []byte
			box := "stco"
			if o.co64 {
				box = "co64"
			}
			for _, a := range chunkAt[ti] {
				if o.co64 {
					offs = append(offs, u64(uint64(mdatPayload+a))...)
				} else {
					offs = append(offs, u32(mdatPayload+a)...)
				}
			}
			if o.frag {
				offs = nil
			}
			cnt := len(chunkAt[ti])
			if o.frag {
				cnt = 0
			}
			stbl := bx("stbl",
				fb("stsd", 0, 0, u32(0)),
				fb("stts", 0, 0, u32(0)),
				fb("stsc", 0, 0, u32(runs), stsc),
				fb("stsz", 0, 0, u32(0), u32(n), stsz),
				fb(box, 0, 0, u32(cnt), offs),
			)
			mh := fb("vmhd", 0, 1, make([]byte, 8))
			if t.handler != "vide" {
				mh = fb("smhd", 0, 0, make([]byte, 4))
			}
			minf := bx("minf", mh, bx("dinf", fb("dref", 0, 0, u32(1), fb("url ", 0, 1))), stbl)
			mdia := bx("mdia",
				fb("mdhd", 0, 0, []byte("SECRET-TIME!"[:8]), make([]byte, 12)),
				fb("hdlr", 0, 0, u32(0), []byte(t.handler), make([]byte, 12), []byte("h\x00")),
				minf)
			trak := bx("trak",
				fb("tkhd", 0, 3, pad("SECRET-TIME", 8), u32(ti+1), make([]byte, 72)),
				bx("edts", fb("elst", 0, 0, u32(0))),
				bx("tref", bx("cdsc", u32(1))),
				mdia,
				bx("udta", bx("name", []byte("SECRET-TRAK"))),
			)
			traks = append(traks, trak)
		}
		key := func(name string) []byte { return cat(u32(8+len(name)), []byte("mdta"), []byte(name)) }
		item := func(idx int, val string) []byte {
			return cat(u32(8+16+len(val)), u32(idx), u32(16+len(val)), []byte("data"), u32(1), u32(0), []byte(val))
		}
		meta := bx("meta",
			fb("hdlr", 0, 0, u32(0), []byte("mdta"), make([]byte, 12), []byte("\x00")),
			fb("keys", 0, 0, u32(3),
				key("com.apple.quicktime.location.ISO6709"),
				key("com.apple.quicktime.make"),
				key("com.android.capture.fps")),
			bx("ilst", item(1, "SECRET-ISO6709+48.85+2.29/"), item(2, "SECRET-KEYMAKE"), item(3, "240.0")),
		)
		udta := bx("udta",
			bx("\xa9xyz", u16(17), u16(0), []byte("SECRET-XYZ+48.85")),
			bx("loci", []byte("SECRET-LOCI")),
			bx("\xa9mak", []byte("SECRET-MAKE")),
			bx("meta", u32(0), fb("hdlr", 0, 0, u32(0), []byte("mdir"), make([]byte, 12)), bx("ilst", bx("\xa9day", []byte("SECRET-DAY")))),
			bx("XMP_", []byte("SECRET-XMP")),
		)
		parts := [][]byte{fb("mvhd", 0, 0, pad("SECRET-TIME", 8), make([]byte, 88))}
		parts = append(parts, traks...)
		parts = append(parts, udta, meta, bx("uuid", []byte("\xbe\x7a\xcf\xcb\x97\xa9\x42\xe8\x9c\x71\x99\x94\x91\xe3\xaf\xac"), []byte("SECRET-XMP")))
		if o.frag {
			parts = append(parts, bx("mvex", fb("trex", 0, 0, u32(1), make([]byte, 16))))
		}
		return bx("moov", parts...)
	}

	ftyp := bx("ftyp", []byte("isom"), u32(512), []byte("isomiso2mp41"))
	xmp := bx("uuid", []byte("\xbe\x7a\xcf\xcb\x97\xa9\x42\xe8\x9c\x71\x99\x94\x91\xe3\xaf\xac"), []byte("<x:xmpmeta>SECRET-XMP</x:xmpmeta>"))
	free := bx("free", []byte("SECRET-FREE"))
	topMeta := fb("meta", 0, 0, fb("hdlr", 0, 0, u32(0), []byte("ID32"), make([]byte, 12)), bx("ID32", []byte("SECRET-TOPMETA")))
	head := cat(ftyp, xmp, free)
	if o.moovFirst {
		m := buildMoov(0)
		start := len(head) + len(m) + len(topMeta) + 8
		m = buildMoov(start)
		out := cat(head, m, topMeta, mdat)
		if o.frag {
			out = cat(out, bx("moof", []byte("MOOF-DATA")), bx("mdat", []byte("FRAG-SAMPLES")))
		}
		return out
	}
	start := len(head) + 8
	return cat(head, mdat, topMeta, buildMoov(start))
}

// ---- an independent reader ----

type tbox struct {
	typ        string
	start, end int
	hdr        int
}

func tchildren(b []byte, s, e int) ([]tbox, error) {
	var out []tbox
	for s < e {
		if e-s < 8 {
			return nil, fmt.Errorf("short header at %d", s)
		}
		sz := int(binary.BigEndian.Uint32(b[s:]))
		h := 8
		if sz == 1 {
			sz = int(binary.BigEndian.Uint64(b[s+8:]))
			h = 16
		} else if sz == 0 {
			sz = e - s
		}
		if sz < h || sz > e-s {
			return nil, fmt.Errorf("box %q at %d size %d past %d", b[s+4:s+8], s, sz, e)
		}
		out = append(out, tbox{string(b[s+4 : s+8]), s, s + sz, h})
		s += sz
	}
	return out, nil
}

var tcontainers = map[string]bool{
	"moov": true, "trak": true, "mdia": true, "minf": true, "stbl": true, "dinf": true,
	"udta": true, "edts": true, "ilst": true, "iprp": true, "ipco": true, "tref": true, "mvex": true,
}

// tall lists every box in the tree, as "path/type".
func tall(t *testing.T, b []byte) []string {
	t.Helper()
	var out []string
	var walk func(s, e int, path string)
	walk = func(s, e int, path string) {
		kids, err := tchildren(b, s, e)
		if err != nil {
			t.Fatalf("box tree: %v", err)
		}
		for _, k := range kids {
			p := path + "/" + k.typ
			out = append(out, p)
			switch {
			case tcontainers[k.typ]:
				walk(k.start+k.hdr, k.end, p)
			case k.typ == "meta":
				st := k.start + k.hdr
				if string(b[st+4:st+8]) != "hdlr" {
					st += 4
				}
				walk(st, k.end, p)
			}
		}
	}
	walk(0, len(b), "")
	return out
}

func tfind(b []byte, s, e int, path ...string) (tbox, bool) {
	kids, err := tchildren(b, s, e)
	if err != nil {
		return tbox{}, false
	}
	for _, k := range kids {
		if k.typ == path[0] {
			if len(path) == 1 {
				return k, true
			}
			return tfind(b, k.start+k.hdr, k.end, path[1:]...)
		}
	}
	return tbox{}, false
}

// tchunks returns, per track handler, the bytes of every chunk, read through
// stco/co64, stsc and stsz.
func tchunks(t *testing.T, b []byte) map[string][][]byte {
	t.Helper()
	moov, ok := tfind(b, 0, len(b), "moov")
	if !ok {
		t.Fatal("no moov")
	}
	out := map[string][][]byte{}
	kids, _ := tchildren(b, moov.start+moov.hdr, moov.end)
	for _, k := range kids {
		if k.typ != "trak" {
			continue
		}
		s, e := k.start+k.hdr, k.end
		h, _ := tfind(b, s, e, "mdia", "hdlr")
		handler := string(b[h.start+h.hdr+8 : h.start+h.hdr+12])
		stbl, _ := tfind(b, s, e, "mdia", "minf", "stbl")
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
		stsc, stsz := get("stsc"), get("stsz")
		fixed := int(binary.BigEndian.Uint32(stsz[4:]))
		sample := 0
		size := func() int {
			if fixed != 0 {
				return fixed
			}
			z := int(binary.BigEndian.Uint32(stsz[12+4*sample:]))
			return z
		}
		ne := int(binary.BigEndian.Uint32(stsc[4:]))
		for c := range offs {
			spc := 0
			for i := 0; i < ne; i++ {
				if int(binary.BigEndian.Uint32(stsc[8+12*i:])) <= c+1 {
					spc = int(binary.BigEndian.Uint32(stsc[8+12*i+4:]))
				}
			}
			n := 0
			for j := 0; j < spc; j++ {
				n += size()
				sample++
			}
			out[handler] = append(out[handler], b[offs[c]:offs[c]+uint64(n)])
		}
	}
	return out
}

func noSecrets(t *testing.T, b []byte, secrets []string) {
	t.Helper()
	for _, s := range secrets {
		if bytes.Contains(b, []byte(s)) {
			t.Errorf("%q survived", s)
		}
	}
}

func sameChunks(t *testing.T, in, out []byte, handlers ...string) {
	t.Helper()
	a, b := tchunks(t, in), tchunks(t, out)
	for _, h := range handlers {
		if len(a[h]) == 0 {
			t.Fatalf("fixture has no %s chunks", h)
		}
		if len(a[h]) != len(b[h]) {
			t.Fatalf("%s: %d chunks became %d", h, len(a[h]), len(b[h]))
		}
		for i := range a[h] {
			if !bytes.Equal(a[h][i], b[h][i]) {
				t.Fatalf("%s chunk %d points at other bytes:\n in  %q\n out %q", h, i, a[h][i], b[h][i])
			}
		}
	}
	if len(b["meta"]) != 0 {
		t.Fatalf("a timed metadata track is still in the index")
	}
}

func hasPath(paths []string, suffix string) bool {
	for _, p := range paths {
		if strings.HasSuffix(p, suffix) {
			return true
		}
	}
	return false
}

// ---- HEIF ----

type hopts struct {
	exifInIdat, base, primaryExif, twoExtents, xmpRefToImage bool
}

var photoSecrets = []string{"SECRET-EXIF", "SECRET-XMP-HEIC", "SECRET-NAME"}

// synthHEIC is a picture with a primary image, a thumbnail, an Exif item and
// an XMP item, the way phones write them: metadata items described by cdsc
// references, the thumbnail by thmb, all data in one mdat after meta.
func synthHEIC(o hopts) []byte {
	img := pad("IMAGE-DATA-PRIMARY", 40)
	thumb := pad("THUMB-DATA", 12)
	exif := cat(u32(0), pad("MM\x00*SECRET-EXIF-GPS+48.85", 30))
	xmp := []byte("<x:xmpmeta>SECRET-XMP-HEIC</x:xmpmeta>")

	// mdat payload: image, exif (unless in idat), thumb, xmp.
	var md []byte
	at := map[int]int{}
	put := func(id int, b []byte) { at[id] = len(md); md = append(md, b...) }
	put(1, img)
	if !o.exifInIdat {
		put(2, exif)
	}
	put(4, thumb)
	put(3, xmp)

	infe := func(id int, typ string, extra ...[]byte) []byte {
		return fb("infe", 2, 0, u16(id), u16(0), []byte(typ), []byte("\x00"), cat(extra...))
	}
	primary := 1
	if o.primaryExif {
		primary = 2
	}
	buildMeta := func(mdatPayload int) []byte {
		// iloc v1: offset 4, length 4, base 4 (or 0), index 0.
		bs := 0
		if o.base {
			bs = 4
		}
		var ents []byte
		n := 0
		ent := func(id, cm int, base int, exts ...[2]int) {
			n++
			e := cat(u16(id), u16(cm), u16(0))
			if bs == 4 {
				e = append(e, u32(base)...)
			}
			e = append(e, u16(len(exts))...)
			for _, x := range exts {
				e = append(e, cat(u32(x[0]), u32(x[1]))...)
			}
			ents = append(ents, e...)
		}
		loc := func(id, n int) {
			abs := mdatPayload + at[id]
			switch {
			case o.twoExtents && id == 1:
				h := n / 2
				if o.base {
					ent(id, 0, abs, [2]int{0, h}, [2]int{h, n - h})
				} else {
					ent(id, 0, 0, [2]int{abs, h}, [2]int{abs + h, n - h})
				}
			case o.base:
				ent(id, 0, abs, [2]int{0, n})
			default:
				ent(id, 0, 0, [2]int{abs, n})
			}
		}
		loc(1, len(img))
		if o.exifInIdat {
			ent(2, 1, 0, [2]int{0, len(exif)})
		} else {
			loc(2, len(exif))
		}
		loc(3, len(xmp))
		loc(4, len(thumb))
		iloc := fb("iloc", 1, 0, []byte{0x44, byte(bs << 4)}, u16(n), ents)
		refs := [][]byte{
			bx("cdsc", u16(2), u16(1), u16(1)),
			bx("thmb", u16(4), u16(1), u16(1)),
		}
		if o.xmpRefToImage {
			refs = append(refs, bx("dimg", u16(1), u16(1), u16(3)))
		} else {
			refs = append(refs, bx("cdsc", u16(3), u16(1), u16(1)))
		}
		parts := [][]byte{
			fb("hdlr", 0, 0, u32(0), []byte("pict"), make([]byte, 12), []byte("\x00")),
			fb("pitm", 0, 0, u16(primary)),
			iloc,
			fb("iinf", 0, 0, u16(4),
				infe(1, "hvc1"),
				infe(2, "Exif"),
				infe(3, "mime", []byte("application/rdf+xml\x00")),
				infe(4, "hvc1")),
			fb("iref", 0, 0, cat(refs...)),
			bx("iprp",
				bx("ipco", fb("ispe", 0, 0, u32(64), u32(48))),
				fb("ipma", 0, 0, u32(3),
					cat(u16(1), []byte{1, 0x81}),
					cat(u16(2), []byte{1, 0x01}),
					cat(u16(4), []byte{1, 0x81}))),
			bx("xml ", []byte("SECRET-NAME")),
		}
		if o.exifInIdat {
			parts = append(parts, bx("idat", exif))
		}
		return fb("meta", 0, 0, cat(parts...))
	}
	ftyp := bx("ftyp", []byte("heic"), u32(0), []byte("mif1heicmiaf"))
	free := bx("free", []byte("SECRET-NAME"))
	m := buildMeta(0)
	m = buildMeta(len(ftyp) + len(m) + len(free) + 8)
	return cat(ftyp, m, free, bx("mdat", md))
}

// titems reads every item's bytes by id, through iloc (and idat), and the
// item types from iinf.
func titems(t *testing.T, b []byte) (data map[int][]byte, types map[int]string) {
	t.Helper()
	meta, ok := tfind(b, 0, len(b), "meta")
	if !ok {
		t.Fatal("no meta")
	}
	ms := meta.start + meta.hdr + 4
	get := func(n string) []byte {
		x, ok := tfind(b, ms, meta.end, n)
		if !ok {
			return nil
		}
		return b[x.start+x.hdr : x.end]
	}
	types = map[int]string{}
	iinf := get("iinf")
	hdr := 6
	if iinf[0] != 0 {
		hdr = 8
	}
	ents, err := tchildren(iinf, hdr, len(iinf))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		p := iinf[e.start+e.hdr : e.end]
		types[int(binary.BigEndian.Uint16(p[4:]))] = string(p[8:12])
	}
	idat := get("idat")
	l := get("iloc")
	ver := l[0]
	os, ls, bs := int(l[4]>>4), int(l[4]&15), int(l[5]>>4)
	rd := func(p []byte, n int) (uint64, []byte) {
		switch n {
		case 2:
			return uint64(binary.BigEndian.Uint16(p)), p[2:]
		case 4:
			return uint64(binary.BigEndian.Uint32(p)), p[4:]
		case 8:
			return binary.BigEndian.Uint64(p), p[8:]
		}
		return 0, p
	}
	p := l[6:]
	cnt, p := rd(p, 2)
	data = map[int][]byte{}
	for i := 0; i < int(cnt); i++ {
		var id, cm, base, ne, off, n uint64
		id, p = rd(p, 2)
		if ver > 0 {
			cm, p = rd(p, 2)
		}
		_, p = rd(p, 2)
		base, p = rd(p, bs)
		ne, p = rd(p, 2)
		var d []byte
		for j := 0; j < int(ne); j++ {
			off, p = rd(p, os)
			n, p = rd(p, ls)
			src := b
			if cm == 1 {
				src = idat
			}
			d = append(d, src[base+off:base+off+n]...)
		}
		data[int(id)] = d
	}
	return data, types
}
