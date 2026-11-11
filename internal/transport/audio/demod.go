package audio

import (
	"encoding/binary"
	"hash/crc32"
	"math"
	"math/cmplx"
)

// Demodulator turns a stream of microphone samples back into frames. Push
// samples in any chunk size; every frame that decodes and passes its CRC is
// handed to the callback. Its memory is bounded: it keeps only the samples the
// frame it is reading (or the sync search) still needs.
type Demodulator struct {
	onFrame func([]byte)

	buf  []float32
	base int64 // absolute sample index of buf[0]

	search int64 // next candidate preamble start, in hops

	// spectra is a memo of band energies per hop, indexed by hop % len.
	spectra [512]specEntry

	// While reading a frame: where its preamble began, and its length once
	// the header is in (-1 before).
	inFrame bool
	t0      int64
	n       int

	fft *fft
	re  []complex128

	// Counters for tests and diagnostics.
	Syncs, BadHeaders, BadFrames, Frames int
}

type specEntry struct {
	hop int64
	ok  bool
	e   [nbins]float32
}

// maxBuffered bounds memory against a stream that never yields a frame: the
// longest frame plus a little.
const maxBuffered = 64 * symLen * 4

// NewDemodulator returns a demodulator that calls onFrame for each frame.
func NewDemodulator(onFrame func([]byte)) *Demodulator {
	return &Demodulator{onFrame: onFrame, fft: newFFT(winLen), re: make([]complex128, winLen)}
}

// PushPCM16 takes raw signed 16-bit little-endian mono samples.
func (d *Demodulator) PushPCM16(raw []byte) {
	s := make([]float32, len(raw)/2)
	for i := range s {
		s[i] = float32(int16(binary.LittleEndian.Uint16(raw[2*i:]))) / 32768
	}
	d.Push(s)
}

// Push takes samples in [-1, 1].
func (d *Demodulator) Push(s []float32) {
	d.buf = append(d.buf, s...)
	for d.step() {
	}
	d.trim()
}

func (d *Demodulator) end() int64 { return d.base + int64(len(d.buf)) }

// energies returns the 64 band-tone energies of the window starting at
// absolute sample at, or false if those samples are not (or no longer) held.
func (d *Demodulator) energies(at int64, out *[nbins]float32) bool {
	lo := at - d.base
	if lo < 0 || lo+winLen > int64(len(d.buf)) {
		return false
	}
	w := d.buf[lo : lo+winLen]
	for i, v := range w {
		d.re[i] = complex(float64(v), 0)
	}
	d.fft.transform(d.re)
	for k := 0; k < nbins; k++ {
		c := d.re[firstBin+k]
		out[k] = float32(real(c)*real(c) + imag(c)*imag(c))
	}
	return true
}

func (d *Demodulator) spectrum(h int64) *[nbins]float32 {
	if h < 0 {
		return nil
	}
	e := &d.spectra[h%int64(len(d.spectra))]
	if e.ok && e.hop == h {
		return &e.e
	}
	if !d.energies(h*hop, &e.e) {
		e.ok = false
		return nil
	}
	e.hop, e.ok = h, true
	return &e.e
}

// share is how much of sub-band b's energy sits in tone t.
func share(e *[nbins]float32, b int, t byte) float64 {
	var sum float64
	for i := 0; i < tones; i++ {
		sum += float64(e[b*tones+i])
	}
	return float64(e[b*tones+int(t)]) / (sum + 1e-12)
}

// score is how well a preamble starting at hop h matches: the mean share of
// energy in the expected tone, 1/16 for noise and near 1 for a clean lock.
func (d *Demodulator) score(h int64) (float64, bool) {
	var s float64
	for k := 0; k < preambleSyms; k++ {
		e := d.spectrum(h + int64(k*symHops+offHops))
		if e == nil {
			return 0, false
		}
		for b := 0; b < bands; b++ {
			s += share(e, b, preamble[k][b])
		}
	}
	return s / (preambleSyms * bands), true
}

// symbol decodes the two bytes of data symbol i of the frame at d.t0.
func (d *Demodulator) symbol(i int) (byte, byte, bool) {
	var e [nbins]float32
	at := d.t0 + int64((preambleSyms+i)*symLen+winOff)
	if !d.energies(at, &e) {
		return 0, 0, false
	}
	var nib [bands]byte
	for b := 0; b < bands; b++ {
		best := 0
		for t := 1; t < tones; t++ {
			if e[b*tones+t] > e[b*tones+best] {
				best = t
			}
		}
		nib[b] = byte(best)
	}
	return nib[0]<<4 | nib[1], nib[2]<<4 | nib[3], true
}

func (d *Demodulator) readBytes(fromSym, nsyms int) ([]byte, bool) {
	if d.end() < d.t0+int64((preambleSyms+fromSym+nsyms)*symLen) {
		return nil, false
	}
	out := make([]byte, 0, 2*nsyms)
	for i := fromSym; i < fromSym+nsyms; i++ {
		a, b, ok := d.symbol(i)
		if !ok {
			return nil, false
		}
		out = append(out, a, b)
	}
	return out, true
}

// step makes one unit of progress and reports whether it did.
func (d *Demodulator) step() bool {
	if d.inFrame {
		return d.readFrame()
	}
	h := d.search
	// Look far enough ahead to see the whole plateau around a lock.
	if d.end() < (h+2*symHops+int64((preambleSyms-1)*symHops+offHops))*hop+winLen {
		return false
	}
	sc, ok := d.score(h)
	if !ok {
		d.search++
		return true
	}
	if sc < syncThreshold {
		d.search++
		return true
	}
	// A lock: the score stays high while the analysis windows sit inside the
	// symbols, a plateau about winOff*2 samples wide. Aim for its middle, the
	// alignment with the most margin on both sides for echo and clock drift.
	lo, hi := h-symHops/2, h+symHops
	best, at := sc, h
	scores := make(map[int64]float64, hi-lo+1)
	for x := lo; x <= hi; x++ {
		if v, ok := d.score(x); ok {
			scores[x] = v
			if v > best {
				best, at = v, x
			}
		}
	}
	a, b := at, at
	for scores[a-1] >= 0.85*best && a-1 >= lo {
		a--
	}
	for scores[b+1] >= 0.85*best && b+1 <= hi {
		b++
	}
	d.Syncs++
	d.inFrame = true
	d.t0 = (a + b) * hop / 2
	d.n = -1
	d.search = max(b, h) + 1
	return true
}

func (d *Demodulator) readFrame() bool {
	if d.n < 0 {
		raw, ok := d.readBytes(0, headerSyms)
		if !ok {
			return false
		}
		hdr, err := rsDecode(raw, headerParity)
		if err != nil || hdr[0] != version || hdr[1] == 0 || int(hdr[1]) > MaxData {
			d.BadHeaders++
			d.inFrame = false
			d.skipPreamble()
			return true
		}
		d.n = int(hdr[1])
	}
	nb := bodySyms(d.n)
	raw, ok := d.readBytes(headerSyms, nb)
	if !ok {
		return false
	}
	d.inFrame = false
	frameEnd := d.t0 + int64(FrameSamples(d.n))
	cw := raw[:d.n+crcLen+bodyParity]
	body, err := rsDecode(cw, bodyParity)
	if err != nil || crc32.ChecksumIEEE(body[:d.n]) != binary.BigEndian.Uint32(body[d.n:]) {
		d.BadFrames++
		d.skipPreamble()
		return true
	}
	d.Frames++
	d.search = max(d.search, frameEnd/hop-symHops/2)
	if d.onFrame != nil {
		d.onFrame(append([]byte(nil), body[:d.n]...))
	}
	return true
}

// skipPreamble moves the search a symbol past a lock that came to nothing,
// so the same preamble is not found again.
func (d *Demodulator) skipPreamble() {
	d.search = max(d.search, d.t0/hop+symHops)
}

// trim drops samples nothing will look at again.
func (d *Demodulator) trim() {
	keep := (d.search - symHops) * hop
	if d.inFrame {
		keep = min(keep, d.t0)
	}
	if over := int64(len(d.buf)) - maxBuffered; over > 0 {
		// Never reached by a well-formed stream; a hard cap regardless.
		keep = max(keep, d.base+over)
		if d.inFrame && d.t0 < keep {
			d.inFrame = false
		}
		d.search = max(d.search, keep/hop+symHops)
	}
	if drop := keep - d.base; drop > 0 {
		if drop > int64(len(d.buf)) {
			drop = int64(len(d.buf))
		}
		d.buf = append(d.buf[:0], d.buf[drop:]...)
		d.base += drop
	}
}

// fft is an iterative radix-2 complex FFT of a fixed size.
type fft struct {
	n   int
	rev []int
	tw  []complex128
}

func newFFT(n int) *fft {
	f := &fft{n: n, rev: make([]int, n), tw: make([]complex128, n/2)}
	bits := 0
	for 1<<bits < n {
		bits++
	}
	for i := range f.rev {
		r := 0
		for b := 0; b < bits; b++ {
			if i&(1<<b) != 0 {
				r |= 1 << (bits - 1 - b)
			}
		}
		f.rev[i] = r
	}
	for i := range f.tw {
		f.tw[i] = cmplx.Exp(complex(0, -2*math.Pi*float64(i)/float64(n)))
	}
	return f
}

func (f *fft) transform(x []complex128) {
	for i, r := range f.rev {
		if i < r {
			x[i], x[r] = x[r], x[i]
		}
	}
	for size := 2; size <= f.n; size <<= 1 {
		half, step := size/2, f.n/size
		for start := 0; start < f.n; start += size {
			for k := 0; k < half; k++ {
				t := f.tw[k*step] * x[start+k+half]
				x[start+k+half] = x[start+k] - t
				x[start+k] += t
			}
		}
	}
}
