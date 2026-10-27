package audio

import (
	"bytes"
	"math"
	"math/rand"
	"testing"
)

func TestReedSolomonCorrectsUpToHalfParity(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 300; trial++ {
		n := 1 + rng.Intn(150)
		msg := make([]byte, n)
		rng.Read(msg)
		cw := rsEncode(msg, bodyParity)
		bad := append([]byte(nil), cw...)
		nerr := rng.Intn(bodyParity/2 + 1)
		for _, p := range rng.Perm(len(bad))[:nerr] {
			bad[p] ^= byte(1 + rng.Intn(255))
		}
		got, err := rsDecode(bad, bodyParity)
		if err != nil {
			t.Fatalf("trial %d: %d errors in %d bytes: %v", trial, nerr, len(cw), err)
		}
		if !bytes.Equal(got, msg) {
			t.Fatalf("trial %d: corrected to the wrong message", trial)
		}
	}
}

func TestReedSolomonRefusesTooManyErrors(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	msg := make([]byte, 100)
	rng.Read(msg)
	refused := 0
	for trial := 0; trial < 200; trial++ {
		bad := rsEncode(msg, bodyParity)
		for _, p := range rng.Perm(len(bad))[:30] {
			bad[p] ^= byte(1 + rng.Intn(255))
		}
		if got, err := rsDecode(bad, bodyParity); err != nil || !bytes.Equal(got, msg) {
			refused++
		}
	}
	if refused != 200 {
		t.Fatalf("30 errors came back as the original message %d times", 200-refused)
	}
}

// channel is the room between two devices: attenuation, a clock that runs at
// a slightly different rate, an echo off a wall, and noise.
type channel struct {
	gain    float64
	ppm     float64 // receiver clock error
	echo    float64 // echo gain
	echoMS  float64
	snrDB   float64 // relative to the signal's mean power
	lead    int     // silence before, samples
	seed    int64
	noiseFn func(i int) float64 // real recorded noise, optional
}

func (c channel) apply(sig []float32) []float32 {
	rng := rand.New(rand.NewSource(c.seed))
	x := make([]float64, c.lead+len(sig)+SampleRate/2)
	for i, v := range sig {
		x[c.lead+i] = float64(v) * c.gain
	}
	if c.echo > 0 {
		d := int(c.echoMS * SampleRate / 1000)
		for i := len(x) - 1; i >= d; i-- {
			x[i] += c.echo * x[i-d]
		}
	}
	// Resample: the receiver's sample k is the sender's time k·(1+ppm).
	r := 1 + c.ppm*1e-6
	y := make([]float64, int(float64(len(x))/r)-1)
	for k := range y {
		t := float64(k) * r
		i := int(t)
		f := t - float64(i)
		y[k] = x[i]*(1-f) + x[i+1]*f
	}
	var p float64
	for _, v := range sig {
		p += float64(v) * float64(v) * c.gain * c.gain
	}
	p /= float64(len(sig))
	sigma := math.Sqrt(p / math.Pow(10, c.snrDB/10))
	out := make([]float32, len(y))
	for i, v := range y {
		n := sigma * rng.NormFloat64()
		if c.noiseFn != nil {
			n = c.noiseFn(i)
		}
		out[i] = float32(v + n)
	}
	return out
}

func randFrames(seed int64, sizes ...int) [][]byte {
	rng := rand.New(rand.NewSource(seed))
	var out [][]byte
	for _, n := range sizes {
		f := make([]byte, n)
		rng.Read(f)
		out = append(out, f)
	}
	return out
}

func transmit(frames [][]byte, gap int) []float32 {
	var sig []float32
	for _, f := range frames {
		sig = append(sig, Modulate(f)...)
		sig = append(sig, make([]float32, gap)...)
	}
	return sig
}

func receive(t *testing.T, rx []float32, chunk int) ([][]byte, *Demodulator) {
	t.Helper()
	var got [][]byte
	d := NewDemodulator(func(f []byte) { got = append(got, f) })
	for i := 0; i < len(rx); i += chunk {
		d.Push(rx[i:min(i+chunk, len(rx))])
	}
	return got, d
}

func TestModemRoundTripThroughARoom(t *testing.T) {
	frames := randFrames(3, 1, 17, 64, 127, MaxData)
	sig := transmit(frames, 4800)
	cases := map[string]channel{
		"clean":                 {gain: 1, snrDB: 60},
		"quiet and far":         {gain: 0.01, snrDB: 10},
		"noisy":                 {gain: 0.3, snrDB: 0},
		"fast clock":            {gain: 0.5, ppm: 1000, snrDB: 10},
		"slow clock":            {gain: 0.5, ppm: -1000, snrDB: 10},
		"echo off a wall":       {gain: 0.5, echo: 0.6, echoMS: 7, snrDB: 10},
		"everything at once":    {gain: 0.05, ppm: 500, echo: 0.5, echoMS: 11, snrDB: 3, lead: 12345},
		"odd chunks and offset": {gain: 0.2, snrDB: 12, lead: 777},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			c.seed = int64(len(name))
			rx := c.apply(sig)
			got, d := receive(t, rx, 333)
			if len(got) != len(frames) {
				t.Fatalf("decoded %d of %d frames (syncs %d, bad headers %d, bad bodies %d)",
					len(got), len(frames), d.Syncs, d.BadHeaders, d.BadFrames)
			}
			for i := range frames {
				if !bytes.Equal(got[i], frames[i]) {
					t.Fatalf("frame %d differs", i)
				}
			}
		})
	}
}

// Under noise far too heavy to decode, the modem must deliver nothing rather
// than something wrong.
func TestModemDeliversNothingFromNoise(t *testing.T) {
	frames := randFrames(4, 64, 64, 64, 64)
	sig := transmit(frames, 2400)
	for _, snr := range []float64{-15, -10, -8} {
		rx := channel{gain: 1, snrDB: snr, seed: int64(-snr)}.apply(sig)
		got, _ := receive(t, rx, 4096)
		want := map[string]bool{}
		for _, f := range frames {
			want[string(f)] = true
		}
		for _, g := range got {
			if !want[string(g)] {
				t.Fatalf("snr %v dB: delivered a frame that was never sent", snr)
			}
		}
	}
	rng := rand.New(rand.NewSource(5))
	noise := make([]float32, 30*SampleRate)
	for i := range noise {
		noise[i] = float32(0.3 * rng.NormFloat64())
	}
	if got, d := receive(t, noise, 4800); len(got) != 0 {
		t.Fatalf("30 s of pure noise produced %d frames (%d syncs)", len(got), d.Syncs)
	}
}

// A frame whose body was damaged beyond repair is dropped, and one whose
// damage RS can repair arrives intact — the CRC is the last word.
func TestModemRejectsCorruptedFrame(t *testing.T) {
	data := randFrames(6, 80)[0]
	sig := Modulate(data)
	// Blank out eight body symbols (16 bytes > 12 correctable).
	start := (preambleSyms + headerSyms + 4) * symLen
	wrecked := append([]float32(nil), sig...)
	rng := rand.New(rand.NewSource(7))
	for i := start; i < start+8*symLen; i++ {
		wrecked[i] = float32(rng.NormFloat64())
	}
	got, d := receive(t, append(wrecked, make([]float32, SampleRate)...), 1000)
	if len(got) != 0 || d.BadFrames != 1 {
		t.Fatalf("wrecked frame: got %d frames, %d bad", len(got), d.BadFrames)
	}
	// Four bad symbols (8 bytes) are within the code's reach.
	hurt := append([]float32(nil), sig...)
	for i := start; i < start+4*symLen; i++ {
		hurt[i] = float32(rng.NormFloat64())
	}
	got, _ = receive(t, append(hurt, make([]float32, SampleRate)...), 1000)
	if len(got) != 1 || !bytes.Equal(got[0], data) {
		t.Fatal("repairable frame was not repaired")
	}
}

func TestModulateRefusesOversize(t *testing.T) {
	if Modulate(make([]byte, MaxData+1)) != nil || Modulate(nil) != nil {
		t.Fatal("modulated a frame outside 1..MaxData")
	}
}

func TestThroughputIsHonest(t *testing.T) {
	secs := float64(FrameSamples(MaxData)) / SampleRate
	bps := float64(MaxData) / secs
	t.Logf("full frame: %d bytes in %.2f s on the air = %.1f B/s (%.0f bit/s) before gaps", MaxData, secs, bps, bps*8)
	if bps < 8 || bps > 64 {
		t.Fatalf("throughput %.1f B/s outside what the docs promise", bps)
	}
}
