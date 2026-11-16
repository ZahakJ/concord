package imgmeta

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math/rand"
	"testing"

	"github.com/ZahakJ/concord/internal/imgmeta/imgfixture"
)

func TestStripRemovesMetadataAndKeepsThePicture(t *testing.T) {
	for _, f := range imgfixture.All() {
		t.Run(f.Name, func(t *testing.T) {
			if err := f.Check(f.Bytes); err == nil {
				t.Fatal("the fixture passes the check before cleaning; the check proves nothing")
			}
			out, err := Strip(f.Bytes)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.Check(out); err != nil {
				t.Fatal(err)
			}
			again, err := Strip(out)
			if err != nil || !bytes.Equal(again, out) {
				t.Fatalf("cleaning a clean picture changed it (err %v)", err)
			}
		})
	}
}

// A picture already upright needs no EXIF at all, so none is written.
func TestUprightJPEGCarriesNoExif(t *testing.T) {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	in := append([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0, 34}, "Exif\x00\x00"...)
	in = append(in, orientationTIFF(1)...)
	in = append(in, b.Bytes()[2:]...)
	out, err := Strip(in)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, b.Bytes()) {
		t.Fatal("an upright picture kept an EXIF block, or lost a byte of its own")
	}
}

// The format comes from the bytes: a JPEG labelled PNG is still cleaned.
func TestFormatIsReadFromTheBytes(t *testing.T) {
	if Format(imgfixture.JPEG()) != "jpeg" || Format(imgfixture.PNG()) != "png" ||
		Format(imgfixture.GIF()) != "gif" || Format(imgfixture.WebP()) != "webp" {
		t.Fatal("a fixture was not recognised")
	}
	heic := append([]byte{0, 0, 0, 24}, "ftypheic"...)
	heic = append(heic, make([]byte, 16)...)
	if _, err := Strip(heic); err != ErrUnsupported {
		t.Fatalf("HEIC: err %v, want ErrUnsupported", err)
	}
	junk := []byte("not a picture at all")
	if out, err := Strip(junk); err != nil || !bytes.Equal(out, junk) {
		t.Fatalf("unknown bytes: %q, %v", out, err)
	}
}

// A file cut short still comes out without its metadata, and what remains of
// the picture is kept.
func TestTruncatedPicturesAreStillCleaned(t *testing.T) {
	for _, f := range imgfixture.All() {
		for _, cut := range []int{len(f.Bytes) / 2, len(f.Bytes) - 30} {
			out, err := Strip(f.Bytes[:cut])
			if err != nil {
				t.Fatalf("%s cut at %d: %v", f.Name, cut, err)
			}
			for _, s := range []string{imgfixture.SecretGPS, imgfixture.SecretXMP, imgfixture.SecretMakerNote, imgfixture.SecretComment} {
				if bytes.Contains(out, []byte(s)) {
					t.Fatalf("%s cut at %d: %q survived", f.Name, cut, s)
				}
			}
		}
	}
}

// bigJPEG is a ~5 MB phone-style JPEG: noise at high quality, with the
// fixture's metadata in front.
func bigJPEG(tb testing.TB) []byte {
	tb.Helper()
	r := rand.New(rand.NewSource(1))
	img := image.NewRGBA(image.Rect(0, 0, 2000, 2560))
	for i := range img.Pix {
		img.Pix[i] = uint8(r.Intn(256))
	}
	for y := 0; y < 2560; y++ {
		img.Set(0, y, color.White)
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 92}); err != nil {
		tb.Fatal(err)
	}
	head := imgfixture.JPEG()
	sos := bytes.Index(head, []byte{0xFF, 0xDB}) // first table: end of the metadata
	return append(append([]byte{}, head[:sos]...), b.Bytes()[2:]...)
}

func BenchmarkStrip5MBJPEG(b *testing.B) {
	in := bigJPEG(b)
	b.SetBytes(int64(len(in)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Strip(in); err != nil {
			b.Fatal(err)
		}
	}
}

func TestBenchInputIsFiveMegabytes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 5 MB picture")
	}
	if n := len(bigJPEG(t)); n < 4<<20 || n > 5<<20 {
		t.Fatalf("benchmark input is %d bytes, want about 5 MB", n)
	}
}

// ---- damaged and hostile input ----

const secret = "LEFTOVER-SECRET"

func be32(n uint32) []byte { return binary.BigEndian.AppendUint32(nil, n) }
func le32(n uint32) []byte { return binary.LittleEndian.AppendUint32(nil, n) }

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func pngChunkT(typ string, data []byte) []byte {
	c := cat([]byte(typ), data)
	return cat(be32(uint32(len(data))), c, be32(crc32.ChecksumIEEE(c)))
}

// tiffHugeOffset is a TIFF header whose IFD0 offset points far past its end.
func tiffHugeOffset(off uint32) []byte {
	return cat([]byte("MM\x00*"), be32(off), make([]byte, 4))
}

// hugeLengthInputs are short pictures whose length fields claim more than
// 2 GB, or offsets near the top of a 32-bit int. Each is a few dozen bytes.
func hugeLengthInputs() map[string][]byte {
	pngSig := []byte("\x89PNG\r\n\x1a\n")
	riff := func(chunks ...[]byte) []byte {
		body := cat(append([][]byte{[]byte("WEBP")}, chunks...)...)
		return cat([]byte("RIFF"), le32(uint32(len(body))), body)
	}
	vp8x := cat([]byte("VP8X"), le32(10), []byte{0x08, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	exifSeg := func(t []byte) []byte {
		p := cat([]byte("Exif\x00\x00"), t)
		return cat([]byte{0xFF, 0xE1, byte((len(p) + 2) >> 8), byte(len(p) + 2)}, p)
	}
	return map[string][]byte{
		"png chunk 2^31-16":     cat(pngSig, be32(0x7FFFFFF0), []byte("tEXt"), []byte(secret)),
		"png chunk 2^32-16":     cat(pngSig, be32(0xFFFFFFF0), []byte("tEXt"), []byte(secret)),
		"png chunk 2^31-1":      cat(pngSig, be32(0x7FFFFFFF), []byte("IDAT"), []byte(secret)),
		"png eXIf offset 2^31":  cat(pngSig, pngChunkT("eXIf", tiffHugeOffset(0x7FFFFFFF))),
		"png eXIf offset 2^32":  cat(pngSig, pngChunkT("eXIf", tiffHugeOffset(0xFFFFFFFF))),
		"jpeg exif offset 2^31": cat([]byte{0xFF, 0xD8}, exifSeg(tiffHugeOffset(0x7FFFFFFF)), []byte{0xFF, 0xD9}),
		"jpeg exif offset 2^32": cat([]byte{0xFF, 0xD8}, exifSeg(tiffHugeOffset(0xFFFFFFFE)), []byte{0xFF, 0xD9}),
		"webp chunk 2^31-1":     riff(vp8x, []byte("EXIF"), le32(0x7FFFFFFF), []byte(secret)),
		"webp chunk 2^32-1":     riff(vp8x, []byte("ICCP"), le32(0xFFFFFFFF), []byte(secret)),
		"webp riff 2^32-1":      cat([]byte("RIFF"), le32(0xFFFFFFFF), []byte("WEBP"), vp8x),
		"webp exif offset 2^31": riff(vp8x, cat([]byte("EXIF"), le32(12), tiffHugeOffset(0x7FFFFFFF))),
	}
}

// Length fields are never trusted: on a 32-bit build (run this package with
// GOARCH=386) a length or offset near 2^31 must not wrap an index around.
func TestHugeLengthsAreNotTrusted(t *testing.T) {
	for name, in := range hugeLengthInputs() {
		t.Run(name, func(t *testing.T) {
			out, err := Strip(in)
			if err != nil && err != ErrMalformed {
				t.Fatalf("err %v", err)
			}
			if bytes.Contains(out, []byte(secret)) {
				t.Fatal("bytes past a length that overruns the file were carried through")
			}
		})
	}
}

// Some cameras and editors leave bytes between JPEG segments. Decoders skip
// them, and so does the stripper, without copying them.
func TestStrayBytesBetweenJPEGSegmentsAreSkipped(t *testing.T) {
	f := imgfixture.All()[0]
	if f.Name != "jpeg" {
		t.Fatal("fixture order changed")
	}
	// After the first segment (the EXIF APP1), insert two zero bytes and a
	// stray 0xFF 0x00 pair.
	l := int(f.Bytes[4])<<8 | int(f.Bytes[5])
	at := 4 + l
	in := cat(f.Bytes[:at], []byte{0x00, 0x00, 0xFF, 0x00, 'x'}, f.Bytes[at:])
	if _, err := jpeg.Decode(bytes.NewReader(in)); err != nil {
		t.Fatalf("the test input does not decode, so it proves nothing: %v", err)
	}
	out, err := Strip(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Check(out); err != nil {
		t.Fatal(err)
	}
}

// A segment or chunk whose length runs past the end of the file is dropped
// with everything after it; nothing it claims to hold is carried through.
func TestOverrunningSegmentsCarryNothingThrough(t *testing.T) {
	jpg := imgfixture.JPEG()
	exif := []byte("\xFF\xE1\x00\x1FExif\x00\x00" + secret + "........")
	pngSig := []byte("\x89PNG\r\n\x1a\n")
	ihdr := pngChunkT("IHDR", []byte{0, 0, 0, 1, 0, 0, 0, 1, 8, 0, 0, 0, 0})
	tEXt := pngChunkT("tEXt", []byte("Comment\x00"+secret))
	riff := func(chunks ...[]byte) []byte {
		body := cat(append([][]byte{[]byte("WEBP")}, chunks...)...)
		return cat([]byte("RIFF"), le32(uint32(len(body))), body)
	}
	vp8x := cat([]byte("VP8X"), le32(10), make([]byte, 10))
	gifHead := cat([]byte("GIF89a"), []byte{1, 0, 1, 0, 0, 0, 0})
	desc := []byte{0x2C, 0, 0, 0, 0, 1, 0, 1, 0, 0}
	cases := map[string][]byte{
		// A quantisation table claiming 65535 bytes, then an EXIF segment.
		"jpeg DQT":      cat(jpg[:2], []byte{0xFF, 0xDB, 0xFF, 0xFF}, make([]byte, 65), exif, []byte{0xFF, 0xD9}),
		"png pHYs":      cat(pngSig, ihdr, be32(1000), []byte("pHYs"), make([]byte, 9), tEXt),
		"png IDAT":      cat(pngSig, ihdr, be32(1000), []byte("IDAT"), make([]byte, 9), tEXt),
		"webp ICCP":     riff(vp8x, []byte("ICCP"), le32(1000), make([]byte, 8), []byte("EXIF"), le32(16), []byte(secret+"\x00")),
		"webp VP8L":     riff(vp8x, []byte("VP8L"), le32(1000), make([]byte, 8), []byte("XMP "), le32(16), []byte(secret+"\x00")),
		"gif LZW block": cat(gifHead, desc, []byte{2, 200}, make([]byte, 10), []byte{0x21, 0xFE, 15}, []byte(secret), []byte{0, 0x3B}),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := Strip(in)
			if err != nil && err != ErrMalformed {
				t.Fatalf("err %v", err)
			}
			if bytes.Contains(out, []byte(secret)) {
				t.Fatal("bytes inside an overrunning segment were carried through")
			}
		})
	}
}

// sPLT carries a free-text palette name and changes nothing on screen.
func TestPNGSuggestedPaletteIsDropped(t *testing.T) {
	var enc bytes.Buffer
	if err := png.Encode(&enc, image.NewGray(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	b := enc.Bytes()
	ihdrEnd := 8 + 12 + 13
	splt := pngChunkT("sPLT", cat([]byte(secret+"\x00"), []byte{8, 1, 2, 3, 255, 0, 1}))
	in := cat(b[:ihdrEnd], splt, b[ihdrEnd:])
	out, err := Strip(in)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte(secret)) {
		t.Fatal("the sPLT palette name survived")
	}
	if !bytes.Equal(out, b) {
		t.Fatal("the picture changed")
	}
}

// ANIMEXTS1.0 is the other loop extension browsers honour; dropping it would
// stop such a GIF from looping.
func TestGIFAnimextsLoopIsKept(t *testing.T) {
	in := bytes.Replace(imgfixture.GIF(), []byte("NETSCAPE2.0"), []byte("ANIMEXTS1.0"), 1)
	if !bytes.Contains(in, []byte("ANIMEXTS1.0")) {
		t.Fatal("fixture has no loop extension to rename")
	}
	out, err := Strip(in)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("ANIMEXTS1.0")) {
		t.Fatal("the ANIMEXTS1.0 loop extension was dropped")
	}
	if g, err := gif.DecodeAll(bytes.NewReader(out)); err != nil || len(g.Image) != 2 {
		t.Fatalf("cleaned GIF: %v", err)
	}
}

// FuzzStrip checks, on any input, that the stripper does not panic, does not
// grow its input by more than a fixed amount, accepts its own output, and,
// wherever the standard library decodes the input, gives a picture that
// decodes to the same pixels. Its seed corpus (the fixtures, cuts of them,
// and the hostile inputs above) runs with every "go test" of this package,
// GOARCH=386 included.
func FuzzStrip(f *testing.F) {
	for _, fx := range imgfixture.All() {
		f.Add(fx.Bytes)
		for _, cut := range []int{len(fx.Bytes) / 3, len(fx.Bytes) / 2, len(fx.Bytes) - 30} {
			f.Add(fx.Bytes[:cut])
		}
	}
	for _, in := range hugeLengthInputs() {
		f.Add(in)
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		out, err := Strip(in)
		if err != nil {
			return
		}
		if len(out) > len(in)+64 {
			t.Fatalf("output %d bytes from %d", len(out), len(in))
		}
		if _, err := Strip(out); err != nil {
			t.Fatalf("cleaned output is refused: %v", err)
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(in))
		if err != nil || cfg.Width*cfg.Height > 1<<20 {
			return
		}
		a, _, err := image.Decode(bytes.NewReader(in))
		if err != nil {
			return
		}
		b, _, err := image.Decode(bytes.NewReader(out))
		if err != nil {
			t.Fatalf("input decodes, cleaned output does not: %v", err)
		}
		if a.Bounds() != b.Bounds() {
			t.Fatalf("bounds %v became %v", a.Bounds(), b.Bounds())
		}
		r := a.Bounds()
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				if a.At(x, y) != b.At(x, y) {
					t.Fatalf("pixel %d,%d changed", x, y)
				}
			}
		}
	})
}
