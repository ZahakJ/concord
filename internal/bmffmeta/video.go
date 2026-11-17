package bmffmeta

import (
	"encoding/binary"
	"io"
)

// track is what the planner needs to know of one trak.
type track struct {
	keep   bool
	offs   []uint64 // chunk offsets, in the input
	lens   []uint64 // chunk byte lengths
	hasOff bool     // has a stco or co64
}

type video struct {
	w          walker
	tracks     []*track
	anyDropped bool
	out        []byte
	sites      []site
	trackIdx   int
}

func planVideo(r io.ReaderAt, top []box) (*plan, error) {
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
	v := &video{}
	mp := moov[moovBox.hdr:]
	kids, err := v.w.children(mp)
	if err != nil {
		return nil, err
	}
	for _, k := range kids {
		switch k.typ {
		case "mvex":
			frag = true
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
			}
		}
	}
	kept := 0
	for _, t := range v.tracks {
		if t.keep {
			kept++
		}
	}
	if kept == 0 {
		return nil, ErrRefused
	}
	if frag && v.anyDropped {
		// The dropped track's samples live in the fragments, whose tables
		// this package does not read.
		return nil, ErrRefused
	}
	if err := v.emitMoov(mp, kids); err != nil {
		return nil, err
	}

	p := &plan{top: top}
	ftyps := 0
	for i := range top {
		b := &top[i]
		switch b.typ {
		case "ftyp":
			ftyps++
			if ftyps > 1 {
				return nil, ErrRefused
			}
			p.parts = append(p.parts, part{copyOf: b})
		case "moov":
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
		case "mdat":
			p.parts = append(p.parts, part{copyOf: b})
		case "moof", "mfra", "sidx", "styp", "ssix", "emsg", "prft":
			p.parts = append(p.parts, part{copyOf: b})
		default:
			// udta, meta, uuid, free, skip, wide, pnot and anything unknown:
			// none of it is media. Kept as zeros where offsets must hold.
			if frag {
				p.parts = append(p.parts, part{zeros: b.end - b.start})
			}
		}
	}
	p.layout()

	var keptR []rng
	for _, t := range v.tracks {
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
	if !p.checkKept(keptR) {
		return nil, ErrRefused
	}
	if !p.patch(v.out, v.sites) {
		return nil, ErrRefused
	}
	return p, nil
}

// analyzeTrak reads a track's handler, data references and chunk table.
func (v *video) analyzeTrak(p []byte) (*track, error) {
	t := &track{}
	kids, err := v.w.children(p)
	if err != nil {
		return nil, err
	}
	var mdia []byte
	for _, k := range kids {
		if k.typ == "mdia" {
			if mdia != nil {
				return nil, ErrRefused
			}
			mdia = p[k.payload():k.end]
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
	handler := ""
	for _, k := range mk {
		pl := mdia[k.payload():k.end]
		switch k.typ {
		case "hdlr":
			if len(pl) < 12 {
				return nil, ErrMalformed
			}
			handler = string(pl[8:12])
		case "minf":
			if minf != nil {
				return nil, ErrRefused
			}
			minf = pl
		}
	}
	t.keep = keepHandlers[handler]
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
			if err := v.checkDinf(pl); err != nil {
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
	return t, v.chunks(t, stbl)
}

// checkDinf refuses a track whose samples may live in another file.
func (v *video) checkDinf(p []byte) error {
	kids, err := v.w.children(p)
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
		ents, err := v.w.children(pl[8:])
		if err != nil {
			return err
		}
		for _, e := range ents {
			ep := pl[8:][e.payload():e.end]
			if len(ep) < 4 || ep[3]&1 == 0 {
				return ErrRefused
			}
		}
	}
	return nil
}

// chunks reads stco/co64, stsc and stsz into chunk offsets and lengths.
func (v *video) chunks(t *track, p []byte) error {
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
			if err := v.emitMdia(p[k.payload():k.end]); err != nil {
				return err
			}
		}
	}
	v.out = end(v.out, at)
	return nil
}

func (v *video) emitMdia(p []byte) error {
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
		case "hdlr", "elng":
			v.raw(b)
		case "minf":
			if err := v.emitMinf(p[k.payload():k.end]); err != nil {
				return err
			}
		}
	}
	v.out = end(v.out, at)
	return nil
}

func (v *video) emitMinf(p []byte) error {
	kids, err := v.w.children(p)
	if err != nil {
		return err
	}
	var at int
	v.out, at = begin(v.out, "minf")
	for _, k := range kids {
		b := p[k.start:k.end]
		switch k.typ {
		case "vmhd", "smhd", "hmhd", "nmhd", "gmhd", "sthd", "hdlr", "dinf":
			v.raw(b)
		case "stbl":
			if err := v.emitStbl(p[k.payload():k.end]); err != nil {
				return err
			}
		}
	}
	v.out = end(v.out, at)
	return nil
}

// emitStbl copies the sample table whole and notes where the chunk offsets
// are, to be moved once the layout is known.
func (v *video) emitStbl(p []byte) error {
	kids, err := v.w.children(p)
	if err != nil {
		return err
	}
	t := v.tracks[v.trackIdx-1]
	var at int
	v.out, at = begin(v.out, "stbl")
	for _, k := range kids {
		b := p[k.start:k.end]
		if k.typ == "stco" || k.typ == "co64" {
			w := 4
			if k.typ == "co64" {
				w = 8
			}
			first := len(v.out) + int(k.hdr) + 8
			for i, off := range t.offs {
				v.sites = append(v.sites, site{pos: first + i*w, width: w, old: off, n: t.lens[i]})
			}
		}
		v.raw(b)
	}
	v.out = end(v.out, at)
	return nil
}

// emitKeysMeta keeps a QuickTime metadata box (handler mdta) with only the
// keys in keepKeys, renumbered. Any other meta is dropped whole.
func (v *video) emitKeysMeta(p []byte) error {
	var vf []byte
	body := p
	switch {
	case len(p) >= 8 && string(p[4:8]) == "hdlr":
	case len(p) >= 12 && string(p[8:12]) == "hdlr":
		vf, body = p[:4], p[4:]
	default:
		return nil
	}
	kids, err := v.w.children(body)
	if err != nil {
		return nil // unreadable metadata is simply not kept
	}
	var hdlr, keys, ilst []byte
	for _, k := range kids {
		switch k.typ {
		case "hdlr":
			hdlr = body[k.start:k.end]
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
		if string(ent[4:8]) == "mdta" && keepKeys[string(ent[8:])] {
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
	v.raw(hdlr)
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
