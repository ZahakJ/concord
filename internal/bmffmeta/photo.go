package bmffmeta

import (
	"bytes"
	"encoding/binary"
	"io"
)

// HEIF keeps a picture's parts as items: coded images, grids, thumbnails,
// and metadata (Exif, XMP as a mime item). iinf names each item and its type,
// iloc says where its bytes are, iref links items (an Exif item "cdsc"
// describes the image), and ipma gives items their properties. Removing an
// item means removing it from all four and clearing its bytes.

type ilocExt struct{ idx, off, n uint64 }

type ilocItem struct {
	id   uint32
	cm   uint8
	dri  uint16
	base uint64
	ext  []ilocExt
}

type iloc struct {
	ver                  uint8
	vf                   []byte
	offSz, lenSz, baseSz int
	idxSz                int
	items                []ilocItem
}

type photo struct {
	w       walker
	removed map[uint32]bool
}

// dropItemTypes are the item types that carry metadata and nothing to see.
var dropItemTypes = map[string]bool{"Exif": true, "mime": true, "uri ": true}

func planPhoto(r io.ReaderAt, top []box) (*plan, error) {
	var metaBox box
	for _, b := range top {
		if b.typ == "meta" {
			metaBox = b
		}
	}
	mb, err := readBox(r, metaBox)
	if err != nil {
		return nil, err
	}
	mp := mb[metaBox.hdr:]
	if len(mp) < 4 || mp[0] != 0 {
		return nil, ErrRefused
	}
	ph := &photo{removed: map[uint32]bool{}}
	body := mp[4:]
	kids, err := ph.w.children(body)
	if err != nil {
		return nil, err
	}
	var primary uint32
	var hasPitm, hasHdlr bool
	var loc *iloc
	var idat []byte
	for _, k := range kids {
		pl := body[k.payload():k.end]
		switch k.typ {
		case "hdlr":
			if len(pl) < 12 || string(pl[8:12]) != "pict" {
				return nil, ErrRefused
			}
			hasHdlr = true
		case "pitm":
			if len(pl) < 6 {
				return nil, ErrMalformed
			}
			hasPitm = true
			if pl[0] == 0 {
				primary = uint32(be16(pl[4:]))
			} else if len(pl) >= 8 {
				primary = be32(pl[4:])
			} else {
				return nil, ErrMalformed
			}
		case "iinf":
			if err := ph.readIinf(pl); err != nil {
				return nil, err
			}
		case "iloc":
			if loc != nil {
				return nil, ErrRefused
			}
			if loc, err = parseIloc(pl); err != nil {
				return nil, err
			}
		case "idat":
			if idat != nil {
				return nil, ErrRefused
			}
			idat = append([]byte(nil), pl...)
		case "ipro", "fiin", "ipmc":
			return nil, ErrRefused
		}
	}
	if !hasHdlr || !hasPitm || loc == nil {
		return nil, ErrMalformed
	}
	if ph.removed[primary] {
		return nil, ErrRefused
	}

	// Layout first: the meta box's new length does not depend on the offset
	// values in it, so it is built once and patched.
	out, sites, err := ph.emitMeta(mb[:metaBox.hdr], mp[:4], body, kids, loc, idat)
	if err != nil {
		return nil, err
	}
	p := &plan{top: top}
	for i := range top {
		b := &top[i]
		switch b.typ {
		case "ftyp", "mdat":
			p.parts = append(p.parts, part{copyOf: b})
		case "meta":
			p.parts = append(p.parts, part{data: out})
		case "moov", "moof":
			return nil, ErrRefused
		}
	}
	p.layout()

	var keptR []rng
	for _, it := range loc.items {
		for _, e := range it.ext {
			abs := it.base + e.off
			if abs < it.base || e.n == 0 {
				return nil, ErrRefused
			}
			switch it.cm {
			case 0:
				if ph.removed[it.id] {
					if !p.addZero(abs, e.n) {
						return nil, ErrRefused
					}
				} else {
					if _, ok := p.media(abs, e.n); !ok {
						return nil, ErrRefused
					}
					keptR = append(keptR, rng{abs, e.n})
				}
			case 1:
				if e.n > uint64(len(idat)) || abs > uint64(len(idat))-e.n {
					return nil, ErrMalformed
				}
			default:
				return nil, ErrRefused
			}
		}
	}
	if !p.checkKept(keptR) {
		return nil, ErrRefused
	}
	for _, s := range sites {
		if err := s(p, out); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (ph *photo) readIinf(pl []byte) error {
	if len(pl) < 6 {
		return ErrMalformed
	}
	hdr := 6
	if pl[0] != 0 {
		hdr = 8
	}
	if len(pl) < hdr {
		return ErrMalformed
	}
	ents, err := ph.w.children(pl[hdr:])
	if err != nil {
		return err
	}
	for _, e := range ents {
		if e.typ != "infe" {
			continue
		}
		id, typ, err := infe(pl[hdr:][e.payload():e.end])
		if err != nil {
			return err
		}
		if dropItemTypes[typ] {
			ph.removed[id] = true
		}
	}
	return nil
}

// infe reads an item info entry, version 2 or 3.
func infe(p []byte) (uint32, string, error) {
	if len(p) < 4 {
		return 0, "", ErrMalformed
	}
	var id uint32
	var prot uint16
	var typ string
	switch p[0] {
	case 2:
		if len(p) < 12 {
			return 0, "", ErrMalformed
		}
		id, prot, typ = uint32(be16(p[4:])), be16(p[6:]), string(p[8:12])
	case 3:
		if len(p) < 14 {
			return 0, "", ErrMalformed
		}
		id, prot, typ = be32(p[4:]), be16(p[8:]), string(p[10:14])
	default:
		return 0, "", ErrRefused
	}
	if prot != 0 {
		return 0, "", ErrRefused
	}
	return id, typ, nil
}

func sizeOK(n int) bool { return n == 0 || n == 4 || n == 8 }

func parseIloc(p []byte) (*iloc, error) {
	if len(p) < 6 {
		return nil, ErrMalformed
	}
	l := &iloc{ver: p[0], vf: p[:4]}
	if l.ver > 2 {
		return nil, ErrRefused
	}
	l.offSz, l.lenSz = int(p[4]>>4), int(p[4]&15)
	l.baseSz = int(p[5] >> 4)
	if l.ver > 0 {
		l.idxSz = int(p[5] & 15)
	}
	if !sizeOK(l.offSz) || !sizeOK(l.lenSz) || !sizeOK(l.baseSz) || !sizeOK(l.idxSz) {
		return nil, ErrRefused
	}
	rd := bytes.NewReader(p[6:])
	u := func(n int) (uint64, error) {
		var b [8]byte
		if _, err := io.ReadFull(rd, b[:n]); err != nil {
			return 0, ErrMalformed
		}
		v, _ := getN(b[:], n)
		if n == 2 {
			v = uint64(be16(b[:]))
		}
		return v, nil
	}
	cw := 2
	if l.ver == 2 {
		cw = 4
	}
	cnt, err := u(cw)
	if err != nil {
		return nil, err
	}
	for i := uint64(0); i < cnt; i++ {
		var it ilocItem
		id, err := u(cw)
		if err != nil {
			return nil, err
		}
		it.id = uint32(id)
		if l.ver > 0 {
			cm, err := u(2)
			if err != nil {
				return nil, err
			}
			it.cm = uint8(cm & 15)
		}
		dri, err := u(2)
		if err != nil {
			return nil, err
		}
		it.dri = uint16(dri)
		if it.dri != 0 {
			return nil, ErrRefused
		}
		if it.base, err = u(l.baseSz); err != nil {
			return nil, err
		}
		ne, err := u(2)
		if err != nil {
			return nil, err
		}
		// Each extent takes at least one byte when any field is sized, so a
		// count larger than what is left is a lie; with no sized fields it
		// takes none, and is refused.
		per := l.offSz + l.lenSz
		if l.ver > 0 {
			per += l.idxSz
		}
		if per == 0 || ne*uint64(per) > uint64(rd.Len()) {
			return nil, ErrMalformed
		}
		for j := uint64(0); j < ne; j++ {
			var e ilocExt
			if l.ver > 0 && l.idxSz > 0 {
				if e.idx, err = u(l.idxSz); err != nil {
					return nil, err
				}
			}
			if e.off, err = u(l.offSz); err != nil {
				return nil, err
			}
			if e.n, err = u(l.lenSz); err != nil {
				return nil, err
			}
			it.ext = append(it.ext, e)
		}
		l.items = append(l.items, it)
	}
	return l, nil
}

// patcher fills one item's offsets once the layout is known.
type patcher func(p *plan, out []byte) error

func appendN(b []byte, n int, v uint64) []byte {
	switch n {
	case 2:
		return binary.BigEndian.AppendUint16(b, uint16(v))
	case 4:
		return binary.BigEndian.AppendUint32(b, uint32(v))
	case 8:
		return binary.BigEndian.AppendUint64(b, v)
	}
	return b
}

// emitIloc writes the kept items' locations with the input's field sizes and
// returns the patchers that move their file offsets.
func (ph *photo) emitIloc(out []byte, l *iloc) ([]byte, []patcher) {
	var at int
	out, at = begin(out, "iloc")
	out = append(out, l.vf...)
	b5 := byte(l.baseSz << 4)
	if l.ver > 0 {
		b5 |= byte(l.idxSz)
	}
	out = append(out, byte(l.offSz<<4|l.lenSz), b5)
	cw := 2
	if l.ver == 2 {
		cw = 4
	}
	var kept []ilocItem
	for _, it := range l.items {
		if !ph.removed[it.id] {
			kept = append(kept, it)
		}
	}
	out = appendN(out, cw, uint64(len(kept)))
	var ps []patcher
	for _, it := range kept {
		out = appendN(out, cw, uint64(it.id))
		if l.ver > 0 {
			out = appendN(out, 2, uint64(it.cm))
		}
		out = appendN(out, 2, uint64(it.dri))
		basePos := len(out)
		out = appendN(out, l.baseSz, it.base)
		out = appendN(out, 2, uint64(len(it.ext)))
		var offPos []int
		for _, e := range it.ext {
			if l.ver > 0 && l.idxSz > 0 {
				out = appendN(out, l.idxSz, e.idx)
			}
			offPos = append(offPos, len(out))
			out = appendN(out, l.offSz, e.off)
			out = appendN(out, l.lenSz, e.n)
		}
		if it.cm != 0 {
			continue
		}
		it, baseSz, offSz := it, l.baseSz, l.offSz
		ps = append(ps, func(p *plan, out []byte) error {
			return moveItem(p, out, it, baseSz, offSz, basePos, offPos)
		})
	}
	return end(out, at), ps
}

// moveItem rewrites one item's base offset, or its extent offsets, for where
// its bytes land in the output.
func moveItem(p *plan, out []byte, it ilocItem, baseSz, offSz, basePos int, offPos []int) error {
	news := make([]uint64, len(it.ext))
	same := true
	for i, e := range it.ext {
		abs := it.base + e.off
		n, ok := p.mapOff(abs, e.n)
		if !ok {
			return ErrRefused
		}
		news[i] = n
		if n-abs != news[0]-(it.base+it.ext[0].off) {
			same = false
		}
	}
	if len(news) == 0 {
		return nil
	}
	if same && baseSz > 0 {
		// Moving the base moves every extent at once, when the new base is
		// a real (non-negative) offset for all of them.
		nb := it.base + (news[0] - (it.base + it.ext[0].off))
		fits := true
		for i, e := range it.ext {
			if nb > news[i] || news[i]-nb != e.off {
				fits = false
			}
		}
		if fits {
			if !putN(out[basePos:], baseSz, nb) {
				return ErrRefused
			}
			return nil
		}
	}
	for i, e := range it.ext {
		if news[i] < it.base {
			return ErrRefused
		}
		no := news[i] - it.base
		if no == e.off {
			continue
		}
		if !putN(out[offPos[i]:], offSz, no) {
			return ErrRefused
		}
	}
	return nil
}

func (ph *photo) emitMeta(hdr, vf, body []byte, kids []box, loc *iloc, idat []byte) ([]byte, []patcher, error) {
	var out []byte
	var at int
	out, at = begin(out, "meta")
	_ = hdr
	out = append(out, vf...)
	var ps []patcher
	for _, k := range kids {
		b := body[k.start:k.end]
		pl := body[k.payload():k.end]
		switch k.typ {
		case "hdlr", "pitm", "dinf":
			out = append(out, b...)
		case "iinf":
			var err error
			if out, err = ph.emitIinf(out, pl); err != nil {
				return nil, nil, err
			}
		case "iloc":
			var p []patcher
			out, p = ph.emitIloc(out, loc)
			ps = append(ps, p...)
		case "iref":
			var err error
			if out, err = ph.emitIref(out, pl); err != nil {
				return nil, nil, err
			}
		case "iprp":
			var err error
			if out, err = ph.emitIprp(out, pl); err != nil {
				return nil, nil, err
			}
		case "grpl":
			if err := ph.checkGrpl(pl); err != nil {
				return nil, nil, err
			}
			out = append(out, b...)
		case "idat":
			for _, it := range loc.items {
				if it.cm != 1 || !ph.removed[it.id] {
					continue
				}
				for _, e := range it.ext {
					abs := it.base + e.off
					if abs < it.base || e.n > uint64(len(idat)) || abs > uint64(len(idat))-e.n {
						return nil, nil, ErrMalformed
					}
					clear(idat[abs : abs+e.n])
				}
			}
			var ia int
			out, ia = begin(out, "idat")
			out = append(out, idat...)
			out = end(out, ia)
		}
		// xml, bxml, uuid and anything unknown are dropped.
	}
	return end(out, at), ps, nil
}

func (ph *photo) emitIinf(out []byte, pl []byte) ([]byte, error) {
	hdr := 6
	if pl[0] != 0 {
		hdr = 8
	}
	ents, err := ph.w.children(pl[hdr:])
	if err != nil {
		return nil, err
	}
	var at int
	out, at = begin(out, "iinf")
	out = append(out, pl[:4]...)
	var kept [][]byte
	for _, e := range ents {
		eb := pl[hdr:][e.start:e.end]
		if e.typ == "infe" {
			id, _, err := infe(pl[hdr:][e.payload():e.end])
			if err != nil {
				return nil, err
			}
			if ph.removed[id] {
				continue
			}
		}
		kept = append(kept, eb)
	}
	if hdr == 6 {
		out = appendN(out, 2, uint64(len(kept)))
	} else {
		out = appendN(out, 4, uint64(len(kept)))
	}
	for _, eb := range kept {
		out = append(out, eb...)
	}
	return end(out, at), nil
}

func (ph *photo) emitIref(out []byte, pl []byte) ([]byte, error) {
	if len(pl) < 4 {
		return nil, ErrMalformed
	}
	w := 2
	if pl[0] != 0 {
		w = 4
	}
	refs, err := ph.w.children(pl[4:])
	if err != nil {
		return nil, err
	}
	var at int
	out, at = begin(out, "iref")
	out = append(out, pl[:4]...)
	get := func(b []byte) uint32 {
		if w == 2 {
			return uint32(be16(b))
		}
		return be32(b)
	}
	for _, r := range refs {
		rb := pl[4:][r.start:r.end]
		rp := pl[4:][r.payload():r.end]
		if len(rp) < w+2 {
			return nil, ErrMalformed
		}
		from := get(rp)
		cnt := int(be16(rp[w:]))
		if len(rp) < w+2+cnt*w {
			return nil, ErrMalformed
		}
		if ph.removed[from] {
			continue
		}
		for i := 0; i < cnt; i++ {
			if ph.removed[get(rp[w+2+i*w:])] {
				return nil, ErrRefused
			}
		}
		out = append(out, rb...)
	}
	return end(out, at), nil
}

func (ph *photo) emitIprp(out []byte, pl []byte) ([]byte, error) {
	kids, err := ph.w.children(pl)
	if err != nil {
		return nil, err
	}
	var at int
	out, at = begin(out, "iprp")
	for _, k := range kids {
		b := pl[k.start:k.end]
		if k.typ != "ipma" {
			out = append(out, b...)
			continue
		}
		mp := pl[k.payload():k.end]
		if len(mp) < 8 {
			return nil, ErrMalformed
		}
		idw := 2
		if mp[0] >= 1 {
			idw = 4
		}
		aw := 1
		if mp[3]&1 != 0 {
			aw = 2
		}
		n := be32(mp[4:])
		rest := mp[8:]
		var ents [][]byte
		for i := uint32(0); i < n; i++ {
			if len(rest) < idw+1 {
				return nil, ErrMalformed
			}
			var id uint32
			if idw == 2 {
				id = uint32(be16(rest))
			} else {
				id = be32(rest)
			}
			sz := idw + 1 + int(rest[idw])*aw
			if len(rest) < sz {
				return nil, ErrMalformed
			}
			if !ph.removed[id] {
				ents = append(ents, rest[:sz])
			}
			rest = rest[sz:]
		}
		var ia int
		out, ia = begin(out, "ipma")
		out = append(out, mp[:4]...)
		out = appendN(out, 4, uint64(len(ents)))
		for _, e := range ents {
			out = append(out, e...)
		}
		out = end(out, ia)
	}
	return end(out, at), nil
}

// checkGrpl refuses a group that names a removed item.
func (ph *photo) checkGrpl(pl []byte) error {
	gs, err := ph.w.children(pl)
	if err != nil {
		return err
	}
	for _, g := range gs {
		gp := pl[g.payload():g.end]
		if len(gp) < 12 {
			return ErrMalformed
		}
		n := uint64(be32(gp[8:]))
		if n > uint64(len(gp)-12)/4 {
			return ErrMalformed
		}
		for i := uint64(0); i < n; i++ {
			if ph.removed[be32(gp[12+i*4:])] {
				return ErrRefused
			}
		}
	}
	return nil
}
