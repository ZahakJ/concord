// Package bmffmeta removes location and device details from videos (MP4 and
// QuickTime MOV) and from HEIF pictures (HEIC, AVIF) before they leave the
// device. All of them are ISO base media files: a tree of length-prefixed
// boxes, with the media itself in "mdat" boxes that the index boxes point
// into by absolute file offset.
//
// How it works. The top-level boxes are read by their headers only. The one
// index box (moov for a video, meta for a picture) is read into memory, at
// most maxIndex bytes, and rebuilt without the metadata; the media boxes are
// then copied from the input in pieces, never read whole. Every offset the
// rebuilt index holds (stco/co64 chunk offsets, iloc extents) is moved by
// where its media box lands in the output. Before a byte is written the whole
// output is planned and checked: every offset of a kept track or item must
// fall inside one media box, and nothing that is cleared may overlap anything
// that is kept. A file that fails any check is refused (ErrRefused or
// ErrMalformed) and the caller decides what to do; this package never writes
// a file it is unsure of.
//
// What a video loses:
//   - moov/udta and trak/udta whole: ©xyz, 3GPP loci, ©mak, ©mod, ©swr, XMP_,
//     and every other user-data box, which none of the players need;
//   - every uuid box (the XMP uuid among them), file-level meta, and the
//     free/skip/wide padding, whose bytes can hold stale data;
//   - in moov/meta (QuickTime keys and ilst), every key but the two playback
//     hints in keepKeys: com.apple.quicktime.location.*, make, model,
//     software, creation date and the rest go;
//   - every track that is not picture or sound (timed metadata such as
//     Apple's mebx tracks and GoPro GPMF, which carry GPS per frame, subtitles
//     and timecode), and the bytes of its samples, which are overwritten with
//     zeros in the copied mdat;
//   - the creation and modification times in mvhd, tkhd and mdhd (set to 0).
//
// What a picture loses: its Exif items (GPS, camera make and model), its mime
// items (XMP) and its uri items, with their entries in iinf, iloc, iref and
// ipma; item names; the handler's name; and every item property that is not
// on an allow-list (a user description, a uuid), which becomes a free box so
// the property indexes still hold.
//
// Media bytes that nothing kept points at are zeroed, in mdat and in idat:
// the samples of dropped tracks and items, and anything a camera or an
// earlier tool left there unlisted (a GPS block whose index box is gone, an
// Exif payload without an item). For a video this needs every kept track's
// chunk lengths; where one is not provable (a single sample size, which old
// QuickTime sound entries do not mean literally) and there are unlisted
// bytes, the file is refused.
//
// Only the file's index and the bytes outside kept samples are touched.
// Nothing inside the coded picture or sound is read or changed: an
// encoder's settings string, or a location a camera writes into the video
// stream itself, stays.
//
// A video with subtitle, text or timecode tracks is cleaned only when the
// caller asks for those tracks to go (Options.DropText): they are content
// the person may have meant to send, and they can also hold location.
// Without it Strip answers ErrTextTracks. An audio-only file keeps its
// title, artist and album tags and loses every other tag.
//
// Fragmented MP4 (moof boxes) keeps every byte where it was: the rebuilt moov
// is padded with a free box to its old length and a dropped top-level box
// becomes a free box of zeros, so the offsets in the fragments stay true.
package bmffmeta

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"sort"
)

var (
	// ErrMalformed: the boxes do not tile the file, or an index is cut short
	// or contradicts itself.
	ErrMalformed = errors.New("bmffmeta: malformed file")
	// ErrRefused: a file this package will not rewrite (an index too large,
	// external data, protected items, layouts it cannot prove it keeps).
	ErrRefused = errors.New("bmffmeta: file layout this cleaner does not rewrite")
	// ErrTextTracks: a video that can be cleaned, but only by dropping its
	// subtitle, text or timecode tracks, which the caller did not allow.
	ErrTextTracks = errors.New("bmffmeta: video has subtitle, text or timecode tracks")
)

// Options change what Strip may remove.
type Options struct {
	// DropText lets a video lose its subtitle, text and timecode tracks.
	DropText bool
}

// Kind says what a file is, as far as its boxes tell: it picks the words the
// app uses ("this video", "this photo") even for a file that was refused.
type Kind int

const (
	KindUnknown Kind = iota
	KindVideo
	KindPhoto
)

// Bounds on what an index may make this package hold. A real phone file is
// far below each of them (an hour of 60 fps video with its sound is about
// 400 000 samples, a 48-tile HEIC about 60 items); above them the file is
// refused before anything is allocated for it, so a small file cannot make
// the cleaner hold hundreds of megabytes.
const (
	maxIndex    = 32 << 20 // the most of moov or meta read into memory
	maxTopBoxes = 4096
	maxBoxes    = 1 << 17 // child boxes visited, all levels together
	maxChunks   = 1 << 19 // chunks and fragment runs, all tracks together
	maxSamples  = 1 << 22 // samples, all tracks together
	maxItems    = 1 << 16 // HEIF items in iloc, and entries in ipma
	maxExtents  = 1 << 18 // HEIF item extents, all items together
)

// keepKeys are the QuickTime metadata keys a video keeps: how fast it was
// filmed, so a slow-motion clip still plays slowed. Nothing in them says
// where, when, or on what.
var keepKeys = map[string]bool{
	"com.android.capture.fps":                             true,
	"com.apple.quicktime.full-frame-rate-playback-intent": true,
}

// audioKeys are the QuickTime metadata keys an audio-only file also keeps.
var audioKeys = map[string]bool{
	"com.apple.quicktime.title":  true,
	"com.apple.quicktime.artist": true,
	"com.apple.quicktime.album":  true,
}

// audioTags are the iTunes-style tags (moov/udta/meta/ilst) an audio-only
// file keeps: title, artist, album. Every other tag goes, the date, the
// encoder, a comment, cover art and any location among them.
var audioTags = map[string]bool{"\xa9nam": true, "\xa9ART": true, "\xa9alb": true}

// keepHandlers are the tracks a video keeps.
var keepHandlers = map[string]bool{"vide": true, "soun": true, "auxv": true}

// textHandlers are the dropped tracks a person may have meant to send:
// subtitles, captions, text and timecode. See ErrTextTracks.
var textHandlers = map[string]bool{"sbtl": true, "subt": true, "text": true, "clcp": true, "tmcd": true}

// Sniff reports whether b starts like an ISO base media file: an ftyp box,
// one of the boxes an older QuickTime file starts with when it has no ftyp,
// or any run of plausible box headers with ftyp, moov or mdat among the
// first four (a segment that starts with styp, a uuid or a vendor box first).
// A file it names is cleaned or refused, never sent unread.
func Sniff(b []byte) bool {
	off := uint64(0)
	for i := 0; i < 4; i++ {
		if uint64(len(b))-off < 8 {
			return false
		}
		h := b[off:]
		sz := uint64(binary.BigEndian.Uint32(h))
		typ := string(h[4:8])
		switch {
		case !boxType(typ):
			return false
		case sz == 0:
			sz = uint64(len(h)) // to the end
		case sz == 1:
			if len(h) < 16 {
				return false
			}
			if sz = binary.BigEndian.Uint64(h[8:]); sz < 16 {
				return false
			}
		case sz < 8:
			return false
		}
		switch typ {
		case "ftyp", "moov", "mdat":
			return true
		case "wide", "free", "skip", "pnot":
			if i == 0 {
				return true
			}
		}
		if sz > uint64(len(h)) {
			return false
		}
		off += sz
	}
	return false
}

// boxType: four characters a box type is made of (letters, digits, space,
// a few marks, and the © that QuickTime user data starts with).
func boxType(t string) bool {
	for i := 0; i < len(t); i++ {
		c := t[i]
		if (c < 0x20 || c > 0x7e) && c != 0xa9 {
			return false
		}
	}
	return true
}

// box is a box's place in its parent, in the parent's coordinates.
type box struct {
	typ        string
	start, hdr int64
	end        int64
}

func (b box) payload() int64 { return b.start + b.hdr }

// scanTop reads the headers of the top-level boxes. They must tile the file
// exactly.
func scanTop(r io.ReaderAt, size int64) ([]box, error) {
	var out []box
	var h [16]byte
	for off := int64(0); off < size; {
		if len(out) >= maxTopBoxes {
			return nil, ErrRefused
		}
		if size-off < 8 {
			return nil, ErrMalformed
		}
		n := int64(16)
		if size-off < n {
			n = size - off
		}
		if _, err := r.ReadAt(h[:n], off); err != nil && !(errors.Is(err, io.EOF) && n > 0) {
			return nil, err
		}
		b, err := header(h[:n], uint64(size-off))
		if err != nil {
			return nil, err
		}
		b.start += off
		b.end += off
		out = append(out, b)
		off = b.end
	}
	return out, nil
}

// header reads a box header at the start of h; avail is how many bytes the
// box may take (the rest of its parent). A size of 0 means "to the end".
func header(h []byte, avail uint64) (box, error) {
	if len(h) < 8 {
		return box{}, ErrMalformed
	}
	sz := uint64(binary.BigEndian.Uint32(h))
	b := box{typ: string(h[4:8]), hdr: 8}
	switch sz {
	case 0:
		sz = avail
	case 1:
		if len(h) < 16 {
			return box{}, ErrMalformed
		}
		sz = binary.BigEndian.Uint64(h[8:16])
		b.hdr = 16
	}
	if sz < uint64(b.hdr) || sz > avail {
		return box{}, ErrMalformed
	}
	b.end = int64(sz)
	return b, nil
}

// walker counts the boxes it has listed, so that no input makes the work
// grow faster than its index.
type walker struct{ boxes int }

// children lists the boxes that tile p exactly.
func (w *walker) children(p []byte) ([]box, error) {
	var out []box
	for off := 0; off < len(p); {
		w.boxes++
		if w.boxes > maxBoxes {
			return nil, ErrRefused
		}
		h := p[off:]
		if len(h) > 16 {
			h = h[:16]
		}
		b, err := header(h, uint64(len(p)-off))
		if err != nil {
			return nil, err
		}
		b.start += int64(off)
		b.end += int64(off)
		out = append(out, b)
		off = int(b.end)
	}
	return out, nil
}

// rng is a byte range [off, off+n) of the input file.
type rng struct{ off, n uint64 }

// site is a field in the rebuilt index that holds a file offset, to be filled
// in once the output layout is known.
type site struct {
	pos   int    // where the field is, in the rebuilt index
	width int    // 4 or 8 bytes
	old   uint64 // the input offset the field holds
	n     uint64 // the length of what it points at
}

// part is one piece of the output: bytes made here, a box copied from the
// input (with ranges in it zeroed), or a free box of zeros.
type part struct {
	data   []byte
	copyOf *box
	zeros  int64 // a free box of this many bytes, zero-filled
}

func (p part) size() int64 {
	switch {
	case p.copyOf != nil:
		return p.copyOf.end - p.copyOf.start
	case p.zeros > 0:
		return p.zeros
	}
	return int64(len(p.data))
}

// plan is the whole output, checked before anything is written.
type plan struct {
	top   []box
	parts []part
	newAt map[int64]int64 // input start -> output start, for each copied box
	zero  []rng           // input ranges written as zeros
}

// media finds the top-level media box that holds [off, off+n) in its payload.
func (p *plan) media(off, n uint64) (*box, bool) {
	i := sort.Search(len(p.top), func(i int) bool { return uint64(p.top[i].end) > off })
	if i == len(p.top) {
		return nil, false
	}
	b := &p.top[i]
	if b.typ != "mdat" || off < uint64(b.payload()) || n > uint64(b.end)-off {
		return nil, false
	}
	if _, kept := p.newAt[b.start]; !kept {
		return nil, false
	}
	return b, true
}

// mapOff moves an input offset to the output, or fails when [off, off+n) is
// not inside one copied media box.
func (p *plan) mapOff(off, n uint64) (uint64, bool) {
	b, ok := p.media(off, n)
	if !ok {
		return 0, false
	}
	return off - uint64(b.start) + uint64(p.newAt[b.start]), true
}

// layout places the parts and records where each copied box lands.
func (p *plan) layout() {
	p.newAt = map[int64]int64{}
	at := int64(0)
	for _, pt := range p.parts {
		if pt.copyOf != nil {
			p.newAt[pt.copyOf.start] = at
		}
		at += pt.size()
	}
}

// addZero marks an input range to be written as zeros; it must lie in the
// payload of a copied media box.
func (p *plan) addZero(off, n uint64) bool {
	if n == 0 {
		return true
	}
	if _, ok := p.media(off, n); !ok {
		return false
	}
	p.zero = append(p.zero, rng{off, n})
	return true
}

// union sorts rs and merges the ranges that overlap or touch, in place, so
// the result is disjoint and searchable.
func union(rs []rng) []rng {
	sort.Slice(rs, func(i, j int) bool { return rs[i].off < rs[j].off })
	m := rs[:0]
	for _, z := range rs {
		if z.n == 0 {
			continue
		}
		if len(m) > 0 && z.off <= m[len(m)-1].off+m[len(m)-1].n {
			last := &m[len(m)-1]
			if e := z.off + z.n; e > last.off+last.n {
				last.n = e - last.off
			}
			continue
		}
		m = append(m, z)
	}
	return m
}

// meets reports whether r overlaps any range of u, a union.
func meets(u []rng, r rng) bool {
	if r.n == 0 {
		return false
	}
	i := sort.Search(len(u), func(i int) bool { return u[i].off+u[i].n > r.off })
	return i < len(u) && u[i].off < r.off+r.n
}

// gaps calls f for each part of [at, end) that no range of u (a union)
// covers.
func gaps(u []rng, at, end uint64, f func(off, n uint64)) {
	i := sort.Search(len(u), func(i int) bool { return u[i].off+u[i].n > at })
	for ; at < end; i++ {
		if i >= len(u) || u[i].off >= end {
			f(at, end-at)
			return
		}
		if u[i].off > at {
			f(at, u[i].off-at)
		}
		at = u[i].off + u[i].n
	}
}

// checkKept fails when a kept range overlaps a zeroed one.
func (p *plan) checkKept(kept []rng) bool {
	p.zero = union(p.zero)
	for _, k := range kept {
		if meets(p.zero, k) {
			return false
		}
	}
	return true
}

// clearUnkept zeroes every byte of each copied mdat's payload that no kept
// range covers, and reports whether there was any. kept is made a union.
func (p *plan) clearUnkept(kept []rng) bool {
	u := union(kept)
	found := false
	for _, pt := range p.parts {
		b := pt.copyOf
		if b == nil || b.typ != "mdat" {
			continue
		}
		gaps(u, uint64(b.payload()), uint64(b.end), func(off, n uint64) {
			found = true
			p.zero = append(p.zero, rng{off, n})
		})
	}
	p.zero = union(p.zero)
	return found
}

// patch fills the offset fields of the rebuilt index for its new place.
func (p *plan) patch(idx []byte, sites []site) bool {
	for _, s := range sites {
		v, ok := p.mapOff(s.old, s.n)
		if !ok {
			return false
		}
		if !putN(idx[s.pos:], s.width, v) {
			return false
		}
	}
	return true
}

func putN(b []byte, width int, v uint64) bool {
	switch width {
	case 4:
		if v > 0xFFFFFFFF {
			return false
		}
		binary.BigEndian.PutUint32(b, uint32(v))
	case 8:
		binary.BigEndian.PutUint64(b, v)
	case 0:
		return v == 0
	default:
		return false
	}
	return true
}

func getN(b []byte, width int) (uint64, bool) {
	if len(b) < width {
		return 0, false
	}
	switch width {
	case 0:
		return 0, true
	case 4:
		return uint64(binary.BigEndian.Uint32(b)), true
	case 8:
		return binary.BigEndian.Uint64(b), true
	}
	return 0, false
}

// write runs the plan. Errors here are the reader's or the writer's only.
func (p *plan) write(r io.ReaderAt, w io.Writer) error {
	buf := make([]byte, 1<<20)
	for _, pt := range p.parts {
		switch {
		case pt.copyOf != nil:
			if err := p.copyBox(r, w, pt.copyOf, buf); err != nil {
				return err
			}
		case pt.zeros > 0:
			if err := writeFree(w, pt.zeros, buf); err != nil {
				return err
			}
		default:
			if _, err := w.Write(pt.data); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *plan) copyBox(r io.ReaderAt, w io.Writer, b *box, buf []byte) error {
	off, end := uint64(b.start), uint64(b.end)
	i := sort.Search(len(p.zero), func(i int) bool { return p.zero[i].off+p.zero[i].n > off })
	for off < end {
		next := end
		if i < len(p.zero) && p.zero[i].off < end {
			z := p.zero[i]
			if z.off <= off {
				ze := z.off + z.n
				if ze > end {
					ze = end
				}
				if err := writeZeros(w, int64(ze-off), buf); err != nil {
					return err
				}
				off = ze
				i++
				continue
			}
			next = z.off
		}
		// plainWriter: an *os.File's ReadFrom would bring its own buffer for
		// every piece, which on a file with many chunks is most of the memory
		// this package uses.
		if _, err := io.CopyBuffer(plainWriter{w}, io.NewSectionReader(r, int64(off), int64(next-off)), buf); err != nil {
			return err
		}
		off = next
	}
	return nil
}

// plainWriter hides any ReadFrom method, so io.CopyBuffer uses the buffer
// it is given.
type plainWriter struct{ w io.Writer }

func (p plainWriter) Write(b []byte) (int, error) { return p.w.Write(b) }

func writeZeros(w io.Writer, n int64, buf []byte) error {
	clear(buf)
	for n > 0 {
		k := int64(len(buf))
		if n < k {
			k = n
		}
		if _, err := w.Write(buf[:k]); err != nil {
			return err
		}
		n -= k
	}
	return nil
}

// freeHeader is the header of a free box of n bytes (n >= 8).
func freeHeader(n int64) []byte {
	if n <= 0xFFFFFFFF {
		h := binary.BigEndian.AppendUint32(nil, uint32(n))
		return append(h, "free"...)
	}
	h := binary.BigEndian.AppendUint32(nil, 1)
	h = append(h, "free"...)
	return binary.BigEndian.AppendUint64(h, uint64(n))
}

func writeFree(w io.Writer, n int64, buf []byte) error {
	h := freeHeader(n)
	if _, err := w.Write(h); err != nil {
		return err
	}
	return writeZeros(w, n-int64(len(h)), buf)
}

func readBox(r io.ReaderAt, b box) ([]byte, error) {
	if b.end-b.start > maxIndex {
		return nil, ErrRefused
	}
	buf := make([]byte, b.end-b.start)
	if _, err := r.ReadAt(buf, b.start); err != nil && !(errors.Is(err, io.EOF)) {
		return nil, err
	}
	return buf, nil
}

// Strip writes to w the file in r (size bytes) without its location and
// device details, and says what kind of file it was. On any error nothing
// useful has been written and w's contents must be thrown away; every check
// runs before the first write, so after a successful plan only the reader
// or the writer can fail.
func Strip(r io.ReaderAt, size int64, w io.Writer) (Kind, error) {
	return StripWith(r, size, w, Options{})
}

// StripWith is Strip with options.
func StripWith(r io.ReaderAt, size int64, w io.Writer, o Options) (Kind, error) {
	top, err := scanTop(r, size)
	if err != nil {
		return headKind(r, size), err
	}
	kind, err := classify(r, top)
	if err != nil {
		return kind, err
	}
	var p *plan
	if kind == KindVideo {
		p, err = planVideo(r, top, o)
	} else {
		p, err = planPhoto(r, top)
	}
	if err != nil {
		return kind, err
	}
	return kind, p.write(r, w)
}

// StripBytes is Strip for a file held in memory.
func StripBytes(b []byte) ([]byte, Kind, error) {
	return StripBytesWith(b, Options{})
}

// StripBytesWith is StripWith for a file held in memory.
func StripBytesWith(b []byte, o Options) ([]byte, Kind, error) {
	var out bytes.Buffer
	k, err := StripWith(bytes.NewReader(b), int64(len(b)), &growWriter{&out, len(b)}, o)
	if err != nil {
		return nil, k, err
	}
	return out.Bytes(), k, nil
}

var photoBrands = map[string]bool{
	"heic": true, "heix": true, "heim": true, "heis": true, "mif1": true,
	"mif2": true, "avif": true, "avci": true, "jpeg": true, "miaf": true,
}

// headKind guesses, from the first box alone, what a file that could not be
// read whole was meant to be, so the person is told "this video" rather than
// "this file".
func headKind(r io.ReaderAt, size int64) Kind {
	var h [64]byte
	n := int64(len(h))
	if size < n {
		n = size
	}
	if n < 8 {
		return KindUnknown
	}
	if _, err := r.ReadAt(h[:n], 0); err != nil && !errors.Is(err, io.EOF) {
		return KindUnknown
	}
	b := h[:n]
	if string(b[4:8]) != "ftyp" {
		return KindVideo // an older QuickTime file starts at moov, mdat or wide
	}
	end := int64(binary.BigEndian.Uint32(b))
	if end > n {
		end = n
	}
	for j := int64(8); j+4 <= end; j += 4 {
		if j != 12 && photoBrands[string(b[j:j+4])] {
			return KindPhoto
		}
	}
	return KindVideo
}

func classify(r io.ReaderAt, top []box) (Kind, error) {
	var moov, meta int
	photoBrand, misplaced := false, false
	for i, b := range top {
		switch b.typ {
		case "moov":
			moov++
		case "meta":
			meta++
		case "ftyp":
			if i != 0 || b.end-b.start > 4096 {
				misplaced = true
				continue
			}
			fb, err := readBox(r, b)
			if err != nil {
				return KindUnknown, err
			}
			pl := fb[b.hdr:]
			for j := 0; j+4 <= len(pl); j += 4 {
				if j == 4 {
					continue // minor version
				}
				if photoBrands[string(pl[j:j+4])] {
					photoBrand = true
				}
			}
		}
	}
	switch {
	case misplaced && moov > 0:
		return KindVideo, ErrRefused
	case misplaced && meta > 0:
		return KindPhoto, ErrRefused
	case misplaced:
		return KindUnknown, ErrRefused
	case moov == 1 && !(photoBrand && meta > 0):
		return KindVideo, nil
	case moov == 0 && meta == 1:
		return KindPhoto, nil
	case photoBrand:
		return KindPhoto, ErrRefused
	case moov > 0:
		return KindVideo, ErrRefused
	}
	return KindUnknown, ErrRefused
}

// growWriter grows its buffer to the input's size on the first write: a file
// that is refused (every check runs before the first write) costs nothing.
type growWriter struct {
	b *bytes.Buffer
	n int
}

func (g *growWriter) Write(p []byte) (int, error) {
	if g.n > 0 {
		g.b.Grow(g.n)
		g.n = 0
	}
	return g.b.Write(p)
}

func be32(b []byte) uint32 { return binary.BigEndian.Uint32(b) }
func be16(b []byte) uint16 { return binary.BigEndian.Uint16(b) }

// begin writes a placeholder box header and returns where it starts; end
// writes the size once the children are in.
func begin(out []byte, typ string) ([]byte, int) {
	at := len(out)
	out = append(out, 0, 0, 0, 0)
	return append(out, typ...), at
}

func end(out []byte, at int) []byte {
	binary.BigEndian.PutUint32(out[at:], uint32(len(out)-at))
	return out
}
