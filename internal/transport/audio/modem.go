// Package audio is an acoustic modem: Concord's sealed frames as sound, from
// one device's speaker to another's microphone across a room. No pairing, no
// radio, nothing to configure — two laptops on a table, or a laptop and a
// phone, and the ciphertext crosses as a few seconds of chirping.
//
// The modem is multi-tone FSK in the style of ggwave. Four sub-bands sit side
// by side between 1.9 and 4.8 kHz, where every laptop speaker and phone
// microphone is at its best and speech is not; each sub-band plays exactly one
// of sixteen tones per symbol, so a 42.7 ms symbol carries four nibbles = two
// bytes. Tones sit on exact FFT bin centres of the 1024-sample analysis window,
// so the receiver needs no filters — one FFT per symbol and an argmax per
// sub-band. Each symbol is 2048 samples long but analysed over only its middle
// 1024: the rest is guard time that soaks up room echo and the slow timing
// drift between two sound cards' clocks.
//
// A frame on the air is
//
//	preamble   8 symbols of a fixed tone pattern (sync)
//	header     3 symbols: version, length, + 4 Reed–Solomon parity bytes
//	body       data ‖ CRC-32 ‖ 24 Reed–Solomon parity bytes
//
// The Reed–Solomon code repairs up to 12 bad bytes per frame; the CRC refuses
// what it could not repair, so a garbled frame is dropped rather than handed
// up. (MLS authenticates every message above this anyway, and the transport
// Manager deduplicates, but garbage has no business leaving the modem.)
package audio

import (
	"encoding/binary"
	"hash/crc32"
	"math"
)

const (
	SampleRate = 48000

	winLen   = 1024 // analysis window: bin spacing 46.875 Hz
	symLen   = 2048 // one symbol on the air, ≈42.7 ms
	winOff   = (symLen - winLen) / 2
	hop      = 128 // sync search resolution, samples
	symHops  = symLen / hop
	offHops  = winOff / hop
	bands    = 4
	tones    = 16
	firstBin = 40 // 1875 Hz; the top tone is bin 103 ≈ 4828 Hz
	nbins    = bands * tones

	preambleSyms = 8
	headerData   = 2
	headerParity = 4
	headerSyms   = (headerData + headerParity) / 2
	bodyParity   = 24
	crcLen       = 4
	version      = 1

	// MaxData is the most data one audio frame carries; the transport
	// Manager fragments anything bigger.
	MaxData = 128

	// toneAmp is the per-sub-band amplitude; four together peak at 0.88 of
	// full scale before the edge fades.
	toneAmp = 0.22
	fadeLen = 480 // 10 ms in and out, so a frame starts without a click

	syncThreshold = 0.5
)

// preamble is a fixed pseudo-random tone per symbol and sub-band. It must not
// look like itself shifted by a symbol, or the receiver could lock one symbol
// early or late.
var preamble = [preambleSyms][bands]byte{
	{3, 12, 7, 0}, {14, 1, 9, 6}, {5, 10, 2, 15}, {11, 4, 13, 8},
	{0, 15, 6, 3}, {9, 7, 1, 12}, {13, 2, 11, 5}, {6, 8, 14, 10},
}

func binFreq(bin int) float64 { return float64(bin) * SampleRate / winLen }

// frameSymbols is the symbol sequence (one tone per sub-band) for data.
func frameSymbols(data []byte) [][bands]byte {
	hdr := rsEncode([]byte{version, byte(len(data))}, headerParity)
	body := make([]byte, 0, len(data)+crcLen)
	body = append(body, data...)
	body = binary.BigEndian.AppendUint32(body, crc32.ChecksumIEEE(data))
	body = rsEncode(body, bodyParity)
	if len(body)%2 == 1 {
		body = append(body, 0)
	}
	syms := make([][bands]byte, 0, preambleSyms+headerSyms+len(body)/2)
	syms = append(syms, preamble[:]...)
	for _, b := range [][]byte{hdr, body} {
		for i := 0; i+1 < len(b); i += 2 {
			syms = append(syms, [bands]byte{b[i] >> 4, b[i] & 15, b[i+1] >> 4, b[i+1] & 15})
		}
	}
	return syms
}

// bodySyms is how many symbols the body of an n-byte frame takes.
func bodySyms(n int) int { return (n + crcLen + bodyParity + 1) / 2 }

// FrameSamples is how many samples one frame of n data bytes takes on the air.
func FrameSamples(n int) int { return (preambleSyms + headerSyms + bodySyms(n)) * symLen }

// Modulate renders one frame of at most MaxData bytes as mono samples in
// [-1, 1] at SampleRate. Each sub-band is a continuous-phase oscillator, so
// symbol changes do not click and splatter into the neighbouring tones.
func Modulate(data []byte) []float32 {
	if len(data) == 0 || len(data) > MaxData {
		return nil
	}
	syms := frameSymbols(data)
	out := make([]float32, len(syms)*symLen)
	var phase [bands]float64
	for s, sym := range syms {
		for b := 0; b < bands; b++ {
			step := 2 * math.Pi * binFreq(firstBin+b*tones+int(sym[b])) / SampleRate
			ph := phase[b]
			o := out[s*symLen : (s+1)*symLen]
			for i := range o {
				o[i] += float32(toneAmp * math.Sin(ph))
				ph += step
			}
			phase[b] = math.Mod(ph, 2*math.Pi)
		}
	}
	for i := 0; i < fadeLen && i < len(out); i++ {
		g := float32(0.5 - 0.5*math.Cos(math.Pi*float64(i)/fadeLen))
		out[i] *= g
		out[len(out)-1-i] *= g
	}
	return out
}

// PCM16 converts samples to signed 16-bit little-endian, the raw format every
// Linux audio tool takes on stdin.
func PCM16(samples []float32) []byte {
	out := make([]byte, 2*len(samples))
	for i, s := range samples {
		v := math.Round(float64(s) * 32767)
		v = max(-32768, min(32767, v))
		binary.LittleEndian.PutUint16(out[2*i:], uint16(int16(v)))
	}
	return out
}
