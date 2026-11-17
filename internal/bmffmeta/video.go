package bmffmeta

import (
	"bytes"
	"encoding/binary"
	"io"
)

// track is what the planner needs to know of one trak.
type track struct {
	id      uint32
	handler string
	keep    bool
	fixed   bool     // stsz gives one size for all samples: lengths not proven
	offs    []uint64 // chunk offsets, in the input
	lens    []uint64 // chunk byte lengths
	hasOff  bool     // has a stco or co64
}

type video struct {
	w          walker
	opts       Options
	tracks     []*track
	anyDropped bool
	textGone   bool // a subtitle, text or timecode track is dropped
	audioOnly  bool // every kept track is sound
	chunks     int  // chunks and fragment runs counted so far
	samples    uint64
	out        []byte
	sites      []site
	trackIdx   int
}

// fragBoxes are the top-level boxes of a fragmented file that are copied in
// place; emsg and prft (an event payload, a wall-clock time) become zeros.
var fragBoxes = map[string]bool{"moof": true, "mfra": true, "sidx": true, "styp": true, "ssix": true}

func planVideo(r io.ReaderAt, top []box, o Options) (*plan, error) {
	var moovBox box
	frag := false
	for _, b := range top {
		switch b.typ {
		case "moov":
			moovBox = b
		case "moof", "mfra", "sidx", "styp", "ssix", "emsg", "prft":
			frag = true
		}
	}
	moov, err := readBox(r, moovBox)
	if err != nil {
		return nil, err
	}
	v := &video{opts: o}
	mp := moov[moovBox.hdr:]
	kids, err := v.w.children(mp)
	if err != nil {
		return nil, err
	}
	trex := map[uint32]uint32{} // track id -> default sample size
	for _, k := range kids {
		switch k.typ {
		case "mvex":
			frag = true
			if err := v.readMvex(mp[k.payload():k.end], trex); err != nil {
				return nil, err
			}
		case "cmov", "rmra":
			return nil, ErrRefused // compressed or reference movies: nothing to see into
		case "trak":
			t, err := v.analyzeTrak(mp[k.payload():k.end])
			if err != nil {
				return nil, err
			}
			v.tracks = append(v.tracks, t)
			if !t.keep {
				v.anyDropped = true
				if textHandlers[t.handler] {
					v.textGone = true
				}
			}
		}
	}
	kept := 0
	v.audioOnly = true
	for _, t := range v.tracks {
		if t.keep {
			kept++
			if t.handler != "soun" {
				v.audioOnly = false
			}
		}
	}
	if kept == 0 {
		return nil, ErrRefused
	}
	if frag && v.anyDropped {
		// The dropped track's samples live in the fragments; clearing them
		// would mean rewriting the fragments' tables.
		return nil, ErrRefused
	}
	if err := v.emitMoov(mp, kids); err != nil {
		return nil, err
	}

	p := &plan{top: top}
	ftyps := 0
	for i := range top {
		b := &top[i]
		switch {
		case b.typ == "ftyp":
			ftyps++
			if ftyps > 1 {
				return nil, ErrRefused
			}
			p.parts = append(p.parts, part{copyOf: b})
		case b.typ == "moov":
			p.parts = append(p.parts, part{data: v.out})
			if frag {
				pad := (b.end - b.start) - int64(len(v.out))
				switch {
				case pad == 0:
				case pad >= 8:
					p.parts = append(p.parts, part{zeros: pad})
				default:
					return nil, ErrRefused
				}
			}
		case b.typ == "mdat":
			p.parts = append(p.parts, part{copyOf: b})
		case frag && fragBoxes[b.typ]:
			p.parts = append(p.parts, part{copyOf: b})
		default:
			// udta, meta, uuid, free, skip, wide, pnot, emsg, prft and
			// anything unknown: none of it is media. Kept as zeros where
			// offsets must hold.
			if frag {
				p.parts = append(p.parts, part{zeros: b.end - b.start})
			}
		}
	}
	p.layout()

	var keptR []rng
	provable := true
	for _, t := range v.tracks {
		if t.keep && t.fixed && len(t.offs) > 0 {
			provable = false
		}
		for i, off := range t.offs {
			n := t.lens[i]
			if t.keep {
				if _, ok := p.media(off, n); !ok {
					return nil, ErrRefused
				}
				keptR = append(keptR, rng{off, n})
			} else if !p.addZero(off, n) {
				return nil, ErrRefused
			}
		}
	}
	if frag {
		ids := map[uint32]bool{}
		for _, t := range v.tracks {
			ids[t.id] = true
		}
		for _, b := range top {
			if b.typ != "moof" {
				continue
			}
			mb, err := readBox(r, b)
			if err != nil {
				return nil, err
			}
			runs, err := v.fragRuns(mb[b.hdr:], uint64(b.start), trex, ids)
			if err != nil {
				return nil, err
			}
			for _, rn := range runs {
				if _, ok := p.media(rn.off, rn.n); !ok {
					return nil, ErrRefused
				}
			}
			keptR = append(keptR, runs...)
		}
	}
	if !p.checkKept(keptR) {
		return nil, ErrRefused
	}
	if p.clearUnkept(keptR) && !provable {
		// Bytes that no kept chunk covers, in a file where a kept chunk's
		// length is a guess: clearing them may clear media, keeping them may
		// keep something unlisted.
		return nil, ErrRefused
	}
	if !p.patch(v.out, v.sites) {
		return nil, ErrRefused
	}
	if v.textGone && !v.opts.DropText {
		return nil, ErrTextTracks
	}
	return p, nil
}

// readMvex notes each track's default sample size (trex), which fragment
// runs use when they give none.
func (v *video) readMvex(p []byte, trex map[uint32]uint32) error {
	kids, err := v.w.children(p)
	if err != nil {
		return err
	}
	for _, k := range kids {
		if k.typ != "trex" {
			continue
		}
		pl := p[k.payload():k.end]
		if len(pl) < 24 {
			return ErrMalformed
		}
		trex[be32(pl[4:])] = be32(pl[16:])
	}
	return nil
}

// fragRuns reads one moof and returns the byte range of each track run's
// samples (trun), in the input. A moof with anything but mfhd and traf, or a
// traf with anything that is not a sample table of its own, is refused: the
// fragment is copied as it is, so what is in it must be known.
func (v *video) fragRuns(p []byte, moofAt uint64, trex map[uint32]uint32, ids map[uint32]bool) ([]rng, error) {
	kids, err := v.w.children(p)
	if err != nil {
		return nil, err
	}
	var out []rng
	prevEnd := moofAt // where the previous traf's data ended
	firstTraf := true
	for _, k := range kids {
		switch k.typ {
		case "mfhd":
			continue
		case "traf":
		default:
			return nil, ErrRefused
		}
		tk, err := v.w.children(p[k.payload():k.end])
		if err != nil {
			return nil, err
		}
		tp := p[k.payload():k.end]
		haveTfhd := false
		var base, defSize uint64
		var pos uint64 // where the next run without a data offset starts
		firstRun := true
		for _, c := range tk {
			pl := tp[c.payload():c.end]
			switch c.typ {
			case "tfhd":
				if haveTfhd || len(pl) < 8 {
					return nil, ErrMalformed
				}
				haveTfhd = true
				fl := be32(pl) & 0xFFFFFF
				id := be32(pl[4:])
				if !ids[id] {
					return nil, ErrRefused
				}
				q := pl[8:]
				take := func(n int) ([]byte, bool) {
					if len(q) < n {
						return nil, false
					}
					b := q[:n]
					q = q[n:]
					return b, true
				}
				switch {
				case fl&1 != 0:
					b, ok := take(8)
					if !ok {
						return nil, ErrMalformed
					}
					base = binary.BigEndian.Uint64(b)
				case fl&0x20000 != 0 || firstTraf:
					base = moofAt
				default:
					base = prevEnd
				}
				defSize = uint64(trex[id])
				for _, f := range []uint32{2, 8} {
					if fl&f != 0 {
						if _, ok := take(4); !ok {
							return nil, ErrMalformed
						}
					}
				}
				if fl&0x10 != 0 {
					b, ok := take(4)
					if !ok {
						return nil, ErrMalformed
					}
					defSize = uint64(be32(b))
				}
				pos = base
			case "trun":
				if !haveTfhd {
					return nil, ErrMalformed
				}
				rn, err := v.trun(pl, base, pos, firstRun, defSize)
				if err != nil {
					return nil, err
				}
				firstRun = false
				out = append(out, rn)
				pos = rn.off + rn.n
				prevEnd = pos
			case "tfdt", "sbgp", "sgpd", "subs", "sdtp":
			default:
				// saio holds file offsets, senc encrypted sample data; udta,
				// meta, uuid and the rest are not media.
				return nil, ErrRefused
			}
		}
		if !haveTfhd {
			return nil, ErrMalformed
		}
		if firstRun {
			prevEnd = base
		}
		firstTraf = false
	}
	return out, nil
}

// trun reads one track run: where its samples start and how many bytes they
// take.
func (v *video) trun(pl []byte, base, pos uint64, first bool, defSize uint64) (rng, error) {
	if len(pl) < 8 {
		return rng{}, ErrMalformed
	}
	fl := be32(pl) & 0xFFFFFF
	cnt := uint64(be32(pl[4:]))
	q := pl[8:]
	v.chunks++
	if v.chunks > maxChunks || cnt > maxSamples-v.samples {
		return rng{}, ErrRefused
	}
	v.samples += cnt
	at := pos
	if first {
		at = base
	}
	if fl&1 != 0 {
		if len(q) < 4 {
			return rng{}, ErrMalformed
		}
		d := int64(int32(be32(q)))
		q = q[4:]
		if d < 0 && uint64(-d) > base || d >= 0 && uint64(d) > ^uint64(0)-base {
			return rng{}, ErrMalformed
		}
		at = uint64(int64(base) + d)
	}
	if fl&4 != 0 {
		if len(q) < 4 {
			return rng{}, ErrMalformed
		}
		q = q[4:]
	}
	per := 0
	for _, f := range []uint32{0x100, 0x200, 0x400, 0x800} {
		if fl&f != 0 {
			per += 4
		}
	}
	if cnt > uint64(len(q))/uint64(max(per, 1)) && per > 0 {
		return rng{}, ErrMalformed
	}
	var n uint64
	if fl&0x200 != 0 {
		so := 0
		if fl&0x100 != 0 {
			so = 4
		}
		for i := uint64(0); i < cnt; i++ {
			n += uint64(be32(q[i*uint64(per)+uint64(so):]))
		}
	} else {
		if defSize != 0 && cnt > ^uint64(0)/defSize {
			return rng{}, ErrMalformed
		}
		n = cnt * defSize
	}
	if n > ^uint64(0)-at {
		return rng{}, ErrMalformed
	}
	return rng{at, n}, nil
}

// analyzeTrak reads a track's id, handler, data references and chunk table.
func (v *video) analyzeTrak(p []byte) (*track, error) {
	t := &track{}
	kids, err := v.w.children(p)
	if err != nil {
		return nil, err
	}
	var mdia []byte
	for _, k := range kids {
		switch k.typ {
		case "mdia":
			if mdia != nil {
				return nil, ErrRefused
			}
			mdia = p[k.payload():k.end]
		case "tkhd":
			pl := p[k.payload():k.end]
			at := 12
			if len(pl) > 0 && pl[0] == 1 {
				at = 20
			}
			if len(pl) < at+4 {
				return nil, ErrMalformed
			}
			t.id = be32(pl[at:])
		}
	}
	if mdia == nil {
		return nil, ErrMalformed
	}
	mk, err := v.w.children(mdia)
	if err != nil {
		return nil, err
	}
	var minf []byte
	for _, k := range mk {
		pl := mdia[k.payload():k.end]
		switch k.typ {
		case "hdlr":
			if len(pl) < 12 {
				return nil, ErrMalformed
			}
			t.handler = string(pl[8:12])
		case "minf":
			if minf != nil {
				return nil, ErrRefused
			}
			minf = pl
		}
	}
	t.keep = keepHandlers[t.handler]
	if minf == nil {
		return t, nil
	}
	nk, err := v.w.children(minf)
	if err != nil {
		return nil, err
	}
	var stbl []byte
	for _, k := range nk {
		pl := minf[k.payload():k.end]
		switch k.typ {
		case "dinf":
			if err := checkDinf(&v.w, pl); err != nil {
				return nil, err
			}
		case "stbl":
			if stbl != nil {
				return nil, ErrRefused
			}
			stbl = pl
		}
	}
	if stbl == nil {
		return t, nil
	}
	return t, v.chunkTable(t, stbl)
}

// checkDinf refuses data references that point at another file, or that
// carry anything (an alias record can hold a volume name and a path).
func checkDinf(w *walker, p []byte) error {
	kids, err := w.children(p)
	if err != nil {
		return err
	}
	for _, k := range kids {
		if k.typ != "dref" {
			continue
		}
		pl := p[k.payload():k.end]
		if len(pl) < 8 {
			return ErrMalformed
		}
		ents, err := w.children(pl[8:])
		if err != nil {
			return err
		}
		for _, e := range ents {
			ep := pl[8:][e.payload():e.end]
			// Self-contained (flag 1), and nothing after the flags but
			// the empty string some writers add.
			if len(ep) < 4 || ep[3]&1 == 0 || len(bytes.Trim(ep[4:], "\x00")) > 0 {
				return ErrRefused
			}
		}
	}
	return nil
}

// chunkTable reads stco/co64, stsc and stsz into chunk offsets and lengths.
func (v *video) chunkTable(t *track, p []byte) error {
	kids, err := v.w.children(p)
	if err != nil {
		return err
	}
	var stsc, stsz []byte
	for _, k := range kids {
		pl := p[k.payload():k.end]
		switch k.typ {
		case "stco", "co64":
			if t.hasOff {
				return ErrRefused
			}
			t.hasOff = true
			w := 4
			if k.typ == "co64" {
				w = 8
			}
			if len(pl) < 8 {
				return ErrMalformed
			}
			n := uint64(be32(pl[4:]))
			if n > uint64(len(pl)-8)/uint64(w) {
				return ErrMalformed
			}
			if n > uint64(maxChunks-v.chunks) {
				return ErrRefused
			}
			v.chunks += int(n)
			t.offs = make([]uint64, n)
			for i := range t.offs {
				t.offs[i], _ = getN(pl[8+i*w:], w)
			}
		case "stsc":
			stsc = pl
		case "stsz":
			stsz = pl
		case "stz2", "saio":
			// Compact sizes, and auxiliary data addressed by file offset:
			// neither is read here, so neither is rewritten.
			return ErrRefused
		}
	}
	if len(t.offs) == 0 {
		return nil
	}
	if stsc == nil || stsz == nil || len(stsc) < 8 || len(stsz) < 12 {
		return ErrMalformed
	}
	ne := uint64(be32(stsc[4:]))
	if ne == 0 || ne > uint64(len(stsc)-8)/12 {
		return ErrMalformed
	}
	sampleSize := uint64(be32(stsz[4:]))
	sampleCount := uint64(be32(stsz[8:]))
	sizes := stsz[12:]
	if sampleSize == 0 && sampleCount > uint64(len(sizes))/4 {
		return ErrMalformed
	}
	if sampleCount > maxSamples-v.samples {
		return ErrRefused
	}
	v.samples += sampleCount
	t.fixed = sampleSize != 0
	nc := uint64(len(t.offs))
	t.lens = make([]uint64, nc)
	si := uint64(0)
	prev := uint64(0)
	for e := uint64(0); e < ne; e++ {
		ent := stsc[8+e*12:]
		first := uint64(be32(ent))
		spc := uint64(be32(ent[4:]))
		if first <= prev || (e == 0 && first != 1) {
			return ErrMalformed
		}
		prev = first
		if first > nc {
			break
		}
		last := nc
		if e+1 < ne {
			if nf := uint64(be32(stsc[8+(e+1)*12:])); nf > first && nf-1 < last {
				last = nf - 1
			}
		}
		for c := first; c <= last; c++ {
			if spc > sampleCount-si {
				return ErrMalformed
			}
			if sampleSize != 0 {
				t.lens[c-1] = spc * sampleSize
			} else {
				var sum uint64
				for s := si; s < si+spc; s++ {
					sum += uint64(be32(sizes[s*4:]))
				}
				t.lens[c-1] = sum
			}
			si += spc
		}
	}
	for i, off := range t.offs {
		if t.lens[i] > ^uint64(0)-off {
			return ErrMalformed
		}
	}
	return nil
}

func (v *video) raw(b []byte) { v.out = append(v.out, b...) }

// zeroTimes copies a full box whose payload starts with creation and
// modification times (mvhd, tkhd, mdhd) with both set to zero.
func (v *video) zeroTimes(b []byte, hdr int64) error {
	pl := b[hdr:]
	if len(pl) < 4 {
		return ErrMalformed
	}
	n := 0
	switch pl[0] {
	case 0:
		n = 8
	case 1:
		n = 16
	default:
		return ErrRefused
	}
	if len(pl) < 4+n {
		return ErrMalformed
	}
	at := len(v.out) + int(hdr) + 4
	v.raw(b)
	clear(v.out[at : at+n])
	return nil
}

// hdlr copies a handler box with its name cleared in place ("Core Media
// Video", "GoPro AVC" name the device). The length stays, so a fragmented
// file's moov keeps its size: an MP4 name reads as empty (it ends at the
// first zero), a QuickTime one too (its length byte is now zero).
func (v *video) hdlr(b []byte, hdr int64) {
	at := len(v.out)
	v.raw(b)
	if name := at + int(hdr) + 24; name < len(v.out) {
		clear(v.out[name:])
	}
}

func (v *video) emitMoov(p []byte, kids []box) error {
	var at int
	v.out, at = begin(v.out, "moov")
	for _, k := range kids {
		b := p[k.start:k.end]
		switch k.typ {
		case "mvhd":
			if err := v.zeroTimes(b, k.hdr); err != nil {
				return err
			}
		case "trak":
			t := v.tracks[v.trackIdx]
			v.trackIdx++
			if t.keep {
				if err := v.emitTrak(p[k.payload():k.end], t); err != nil {
					return err
				}
			}
		case "meta":
			if err := v.emitKeysMeta(p[k.payload():k.end]); err != nil {
				return err
			}
		case "udta":
			if v.audioOnly {
				v.emitAudioTags(p[k.payload():k.end])
			}
		case "iods", "mvex", "ctab":
			v.raw(b)
		}
	}
	v.out = end(v.out, at)
	return nil
}

func (v *video) emitTrak(p []byte, t *track) error {
	kids, err := v.w.children(p)
	if err != nil {
		return err
	}
	var at int
	v.out, at = begin(v.out, "trak")
	for _, k := range kids {
		b := p[k.start:k.end]
		switch k.typ {
		case "tkhd":
			if err := v.zeroTimes(b, k.hdr); err != nil {
				return err
			}
		case "edts", "tapt", "clip", "matt", "kmat", "load", "imap", "txas":
			v.raw(b)
		case "tref":
			// References may name a dropped track (a metadata track that
			// describes this one, a timecode, chapters).
			if !v.anyDropped {
				v.raw(b)
			}
		case "mdia":
			if err := v.emitMdia(p[k.payload():k.end], t); err != nil {
				return err
			}
		}
	}
	v.out = end(v.out, at)
	return nil
}

func (v *video) emitMdia(p []byte, t *track) error {
	kids, err := v.w.children(p)
	if err != nil {
		return err
	}
	var at int
	v.out, at = begin(v.out, "mdia")
	for _, k := range kids {
		b := p[k.start:k.end]
		switch k.typ {
		case "mdhd":
			if err := v.zeroTimes(b, k.hdr); err != nil {
				return err
			}
		case "hdlr":
			v.hdlr(b, k.hdr)
		case "elng":
			v.raw(b)
		case "minf":
			if err := v.emitMinf(p[k.payload():k.end], t); err != nil {
				return err
			}
		}
	}
	v.out = end(v.out, at)
	return nil
}

func (v *video) emitMinf(p []byte, t *track) error {
	kids, err := v.w.children(p)
	if err != nil {
		return err
	}
	var at int
	v.out, at = begin(v.out, "minf")
	for _, k := range kids {
		b := p[k.start:k.end]
		switch k.typ {
		case "vmhd", "smhd", "hmhd", "nmhd", "sthd", "dinf":
			v.raw(b)
		case "hdlr":
			v.hdlr(b, k.hdr)
		case "gmhd":
			// Only its gmin: the rest (text, tmcd) belongs to tracks that
			// are not kept, and a vendor may put anything beside it.
			gk, err := v.w.children(p[k.payload():k.end])
			if err != nil {
				return err
			}
			var ga int
			v.out, ga = begin(v.out, "gmhd")
			for _, g := range gk {
				if g.typ == "gmin" {
					v.raw(p[k.payload():k.end][g.start:g.end])
				}
			}
			v.out = end(v.out, ga)
		case "stbl":
			if err := v.emitStbl(p[k.payload():k.end], t); err != nil {
				return err
			}
		}
	}
	v.out = end(v.out, at)
	return nil
}

// stblKeep are the sample table boxes copied; anything else in stbl (a
// vendor's udta or uuid) is dropped.
var stblKeep = map[string]bool{
	"stsd": true, "stts": true, "ctts": true, "cslg": true, "stss": true, "stps": true,
	"sdtp": true, "stsc": true, "stsz": true, "stco": true, "co64": true, "sgpd": true,
	"sbgp": true, "subs": true, "stsh": true, "padb": true,
}

// emitStbl copies the sample table and notes where the chunk offsets are,
// to be moved once the layout is known.
func (v *video) emitStbl(p []byte, t *track) error {
	kids, err := v.w.children(p)
	if err != nil {
		return err
	}
	var at int
	v.out, at = begin(v.out, "stbl")
	for _, k := range kids {
		b := p[k.start:k.end]
		if !stblKeep[k.typ] {
			continue
		}
		switch k.typ {
		case "stco", "co64":
			w := 4
			if k.typ == "co64" {
				w = 8
			}
			first := len(v.out) + int(k.hdr) + 8
			for i, off := range t.offs {
				v.sites = append(v.sites, site{pos: first + i*w, width: w, old: off, n: t.lens[i]})
			}
		case "stsd":
			if t.handler == "vide" || t.handler == "auxv" {
				if err := v.emitVisualStsd(p[k.payload():k.end]); err != nil {
					return err
				}
				continue
			}
		}
		v.raw(b)
	}
	v.out = end(v.out, at)
	return nil
}

// stsdDrop are the boxes dropped from a visual sample entry: vendor data,
// not what a decoder needs.
var stsdDrop = map[string]bool{"uuid": true, "udta": true, "meta": true, "free": true, "skip": true, "wide": true}

// emitVisualStsd copies the sample descriptions of a picture track with the
// compressor name cleared (it names the camera's encoder) and vendor boxes
// dropped.
func (v *video) emitVisualStsd(p []byte) error {
	if len(p) < 8 {
		return ErrMalformed
	}
	ents, err := v.w.children(p[8:])
	if err != nil {
		return err
	}
	var at int
	v.out, at = begin(v.out, "stsd")
	v.raw(p[:8])
	for _, e := range ents {
		ep := p[8:][e.payload():e.end]
		// SampleEntry (8) and VisualSampleEntry (70): the name is 32 bytes
		// at 42.
		if len(ep) < 78 {
			return ErrMalformed
		}
		var ea int
		v.out, ea = begin(v.out, e.typ)
		head := len(v.out)
		v.raw(ep[:78])
		clear(v.out[head+42 : head+74])
		tail := ep[78:]
		var term []byte
		kids, err := v.w.children(tail)
		if err != nil && len(tail) >= 4 && be32(tail[len(tail)-4:]) == 0 {
			// Older QuickTime ends the extensions with four zero bytes.
			term = tail[len(tail)-4:]
			kids, err = v.w.children(tail[:len(tail)-4])
		}
		if err != nil {
			return ErrRefused
		}
		for _, c := range kids {
			if !stsdDrop[c.typ] {
				v.raw(tail[c.start:c.end])
			}
		}
		v.raw(term)
		v.out = end(v.out, ea)
	}
	v.out = end(v.out, at)
	return nil
}

// metaParts splits a meta box's payload into its version/flags (absent in
// QuickTime's form) and its children.
func metaParts(p []byte) (vf, body []byte, ok bool) {
	switch {
	case len(p) >= 8 && string(p[4:8]) == "hdlr":
		return nil, p, true
	case len(p) >= 12 && string(p[8:12]) == "hdlr":
		return p[:4], p[4:], true
	}
	return nil, nil, false
}

// emitAudioTags keeps, for an audio-only file, the title, artist and album
// of its iTunes-style tags (udta/meta/ilst) and nothing else of udta.
func (v *video) emitAudioTags(p []byte) {
	kids, err := v.w.children(p)
	if err != nil {
		return
	}
	for _, k := range kids {
		if k.typ != "meta" {
			continue
		}
		vf, body, ok := metaParts(p[k.payload():k.end])
		if !ok {
			continue
		}
		mk, err := v.w.children(body)
		if err != nil {
			continue
		}
		var hdlr, ilst []byte
		var hdlrHdr int64
		for _, m := range mk {
			switch m.typ {
			case "hdlr":
				pl := body[m.payload():m.end]
				if len(pl) < 12 || string(pl[8:12]) != "mdir" {
					return
				}
				hdlr, hdlrHdr = body[m.start:m.end], m.hdr
			case "ilst":
				ilst = body[m.payload():m.end]
			}
		}
		if hdlr == nil || ilst == nil {
			continue
		}
		items, err := v.w.children(ilst)
		if err != nil {
			continue
		}
		var keep [][]byte
		for _, it := range items {
			if audioTags[it.typ] {
				keep = append(keep, ilst[it.start:it.end])
			}
		}
		if len(keep) == 0 {
			continue
		}
		var ua, ma, la int
		v.out, ua = begin(v.out, "udta")
		v.out, ma = begin(v.out, "meta")
		v.raw(vf)
		v.hdlr(hdlr, hdlrHdr)
		v.out, la = begin(v.out, "ilst")
		for _, b := range keep {
			v.raw(b)
		}
		v.out = end(v.out, la)
		v.out = end(v.out, ma)
		v.out = end(v.out, ua)
		return
	}
}

// emitKeysMeta keeps a QuickTime metadata box (handler mdta) with only the
// keys in keepKeys (and audioKeys for an audio-only file), renumbered. Any
// other meta is dropped whole.
func (v *video) emitKeysMeta(p []byte) error {
	vf, body, ok := metaParts(p)
	if !ok {
		return nil
	}
	kids, err := v.w.children(body)
	if err != nil {
		return nil // unreadable metadata is simply not kept
	}
	var hdlr, keys, ilst []byte
	var hdlrHdr int64
	for _, k := range kids {
		switch k.typ {
		case "hdlr":
			hdlr, hdlrHdr = body[k.start:k.end], k.hdr
			pl := body[k.payload():k.end]
			if len(pl) < 12 || string(pl[8:12]) != "mdta" {
				return nil
			}
		case "keys":
			keys = body[k.payload():k.end]
		case "ilst":
			ilst = body[k.payload():k.end]
		}
	}
	if hdlr == nil || keys == nil || ilst == nil || len(keys) < 8 {
		return nil
	}
	// Old 1-based index -> new index, for the keys kept.
	renum := map[uint32]uint32{}
	var kept [][]byte
	n := be32(keys[4:])
	rest := keys[8:]
	for i := uint32(1); i <= n; i++ {
		if len(rest) < 8 {
			return nil
		}
		sz := be32(rest)
		if sz < 8 || uint64(sz) > uint64(len(rest)) {
			return nil
		}
		ent := rest[:sz]
		rest = rest[sz:]
		name := string(ent[8:])
		if string(ent[4:8]) == "mdta" && (keepKeys[name] || v.audioOnly && audioKeys[name]) {
			kept = append(kept, ent)
			renum[i] = uint32(len(kept))
		}
	}
	if len(kept) == 0 {
		return nil
	}
	items, err := v.w.children(ilst)
	if err != nil {
		return nil
	}
	var at int
	v.out, at = begin(v.out, "meta")
	v.raw(vf)
	v.hdlr(hdlr, hdlrHdr)
	var ka int
	v.out, ka = begin(v.out, "keys")
	v.raw([]byte{0, 0, 0, 0})
	v.out = binary.BigEndian.AppendUint32(v.out, uint32(len(kept)))
	for _, e := range kept {
		v.raw(e)
	}
	v.out = end(v.out, ka)
	var la int
	v.out, la = begin(v.out, "ilst")
	for _, it := range items {
		idx := be32([]byte(it.typ))
		nw, ok := renum[idx]
		if !ok {
			continue
		}
		var ia int
		v.out, ia = begin(v.out, "    ")
		binary.BigEndian.PutUint32(v.out[ia+4:], nw)
		v.raw(ilst[it.payload():it.end])
		v.out = end(v.out, ia)
	}
	v.out = end(v.out, la)
	v.out = end(v.out, at)
	return nil
}
