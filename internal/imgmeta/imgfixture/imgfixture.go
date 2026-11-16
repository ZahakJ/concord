// Package imgfixture builds test pictures that carry every kind of metadata
// the app removes before sending, and checks a cleaned copy of one. The
// checker parses the output on its own rather than through package imgmeta,
// so a test that uses it does not grade the stripper with the stripper.
package imgfixture

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"image/jpeg"
	"image/png"
)

// Planted strings: none of them may survive cleaning.
const (
	SecretMake      = "SECRET-CAMERA-MAKE"
	SecretGPS       = "GPS-SECRET-DATUM"
	SecretMakerNote = "MAKERNOTE-SECRET"
	SecretThumb     = "THUMB-SECRET"
	SecretXMP       = "XMP-SECRET"
	SecretIPTC      = "IPTC-SECRET"
	SecretComment   = "COMMENT-SECRET"
	SecretText      = "TEXT-SECRET"
	SecretTrailer   = "TRAILER-SECRET"
)

var secrets = []string{
	SecretMake, SecretGPS, SecretMakerNote, SecretThumb, SecretXMP,
	SecretIPTC, SecretComment, SecretText, SecretTrailer,
}

// ICCMarker sits inside each fixture's colour profile, which must survive.
const ICCMarker = "ICC-PROFILE-KEEP"

// Orientation is the EXIF orientation every fixture that can carry one is
// given: 6, "rotate 90° clockwise to display". It must survive as a tag.
const Orientation = 6

// A Fixture is one picture with metadata planted in it.
type Fixture struct {
	Name    string // "jpeg", "png", "gif", "webp"
	Subtype string // as in a data:image/<subtype> URL
	Bytes   []byte
}

// DataURL is the fixture as the composer hands it to the core.
func (f Fixture) DataURL() string {
	return "data:image/" + f.Subtype + ";base64," + base64.StdEncoding.EncodeToString(f.Bytes)
}

// All returns one fixture per format.
func All() []Fixture {
	return []Fixture{
		{"jpeg", "jpeg", JPEG()},
		{"png", "png", PNG()},
		{"gif", "gif", GIF()},
		{"webp", "webp", WebP()},
	}
}

func picture(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 255 / w), uint8(y * 255 / h), uint8((x ^ y) * 7), 255})
		}
	}
	return img
}

// tiffBlock builds a TIFF block like a phone camera writes: IFD0 with a
// camera make, the orientation and pointers to an EXIF IFD (holding a maker
// note) and a GPS IFD, and IFD1 pointing at an embedded thumbnail.
func tiffBlock(bo binary.ByteOrder, orient int) []byte {
	type ent struct {
		tag, typ uint16
		count    uint32
		data     []byte
	}
	u16 := func(v int) []byte { b := make([]byte, 2); bo.PutUint16(b, uint16(v)); return b }
	u32 := func(v int) []byte { b := make([]byte, 4); bo.PutUint32(b, uint32(v)); return b }
	size := func(n int) int { return 2 + 12*n + 4 }
	off0 := 8
	off1 := off0 + size(4)
	offE := off1 + size(2)
	offG := offE + size(1)
	dataOff := offG + size(3)

	thumb := []byte("\xFF\xD8" + SecretThumb + "\xFF\xD9")
	makeStr := []byte(SecretMake + "\x00")
	maker := []byte(SecretMakerNote + "-with-serial-0123456789")
	datum := []byte(SecretGPS + "\x00")
	lat := append(append(append(append(append(u32(48), u32(1)...), u32(51)...), u32(1)...), u32(2959)...), u32(100)...)

	ifds := [][]ent{
		{{0x010F, 2, uint32(len(makeStr)), makeStr}, {0x0112, 3, 1, u16(orient)}, {0x8769, 4, 1, u32(offE)}, {0x8825, 4, 1, u32(offG)}},
		{{0x0201, 4, 1, u32(dataOff)}, {0x0202, 4, 1, u32(len(thumb))}},
		{{0x927C, 7, uint32(len(maker)), maker}},
		{{0x0001, 2, 2, []byte("N\x00")}, {0x0002, 5, 3, lat}, {0x0012, 2, uint32(len(datum)), datum}},
	}
	head := make([]byte, 0, dataOff)
	data := append([]byte{}, thumb...)
	if bo == binary.BigEndian {
		head = append(head, 'M', 'M', 0, 42)
	} else {
		head = append(head, 'I', 'I', 42, 0)
	}
	head = append(head, u32(off0)...)
	for k, ifd := range ifds {
		head = append(head, u16(len(ifd))...)
		for _, e := range ifd {
			head = append(head, u16(int(e.tag))...)
			head = append(head, u16(int(e.typ))...)
			head = append(head, u32(int(e.count))...)
			if len(e.data) <= 4 {
				v := make([]byte, 4)
				copy(v, e.data)
				head = append(head, v...)
			} else {
				head = append(head, u32(dataOff+len(data))...)
				data = append(data, e.data...)
			}
		}
		if k == 0 {
			head = append(head, u32(off1)...)
		} else {
			head = append(head, u32(0)...)
		}
	}
	return append(head, data...)
}

func xmpPacket() []byte {
	return []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF><rdf:Description exif:GPSLatitude="48,51.2959N" photoshop:City="` + SecretXMP + `"/></rdf:RDF></x:xmpmeta>`)
}

func iccProfile() []byte {
	p := make([]byte, 128)
	copy(p[36:], "acsp")
	return append(p, ICCMarker...)
}

func jpegSegment(m byte, payload []byte) []byte {
	n := len(payload) + 2
	return append([]byte{0xFF, m, byte(n >> 8), byte(n)}, payload...)
}

// JPEG is a 64×40 picture from a phone: EXIF (make, orientation 6, maker
// note, GPS, thumbnail), XMP, an ICC profile, IPTC, a comment, and data after
// the end-of-image marker.
func JPEG() []byte {
	var enc bytes.Buffer
	if err := jpeg.Encode(&enc, picture(64, 40), &jpeg.Options{Quality: 90}); err != nil {
		panic(err)
	}
	b := enc.Bytes()
	var out bytes.Buffer
	out.Write(b[:2]) // SOI
	out.Write(jpegSegment(0xE1, append([]byte("Exif\x00\x00"), tiffBlock(binary.LittleEndian, Orientation)...)))
	out.Write(jpegSegment(0xE1, append([]byte("http://ns.adobe.com/xap/1.0/\x00"), xmpPacket()...)))
	out.Write(jpegSegment(0xE2, append([]byte("ICC_PROFILE\x00\x01\x01"), iccProfile()...)))
	out.Write(jpegSegment(0xED, []byte("Photoshop 3.0\x008BIM\x04\x04\x00\x00\x00\x00\x00\x10\x1c\x02\x5a\x00\x0b"+SecretIPTC)))
	out.Write(jpegSegment(0xFE, []byte(SecretComment)))
	out.Write(b[2:])
	out.WriteString(SecretTrailer)
	return out.Bytes()
}

func pngChunk(typ string, data []byte) []byte {
	out := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	out = append(out, typ...)
	out = append(out, data...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(out[4:]))
}

func deflate(b []byte) []byte {
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	w.Write(b)
	w.Close()
	return z.Bytes()
}

// PNG is a 48×32 picture with an iCCP profile, eXIf (orientation 6 and GPS),
// tEXt, iTXt carrying XMP, zTXt and tIME.
func PNG() []byte {
	var enc bytes.Buffer
	if err := png.Encode(&enc, picture(48, 32)); err != nil {
		panic(err)
	}
	b := enc.Bytes()
	ihdrEnd := 8 + 12 + 13
	var out bytes.Buffer
	out.Write(b[:ihdrEnd])
	out.Write(pngChunk("iCCP", append([]byte("profile\x00\x00"), deflate(iccProfile())...)))
	out.Write(pngChunk("eXIf", tiffBlock(binary.BigEndian, Orientation)))
	out.Write(pngChunk("tEXt", []byte("Comment\x00"+SecretText)))
	out.Write(pngChunk("iTXt", append([]byte("XML:com.adobe.xmp\x00\x00\x00\x00\x00"), xmpPacket()...)))
	out.Write(pngChunk("zTXt", append([]byte("Author\x00\x00"), deflate([]byte(SecretComment))...)))
	out.Write(pngChunk("tIME", []byte{0x07, 0xEA, 11, 15, 12, 0, 0}))
	out.Write(b[ihdrEnd:])
	return out.Bytes()
}

// GIF is a two-frame looping animation with a comment and an XMP
// application extension.
func GIF() []byte {
	a := &gif.GIF{LoopCount: 0}
	for k := 0; k < 2; k++ {
		f := image.NewPaletted(image.Rect(0, 0, 16, 12), palette.Plan9)
		for y := 0; y < 12; y++ {
			for x := 0; x < 16; x++ {
				f.SetColorIndex(x, y, uint8((x+y+k*40)%256))
			}
		}
		a.Image = append(a.Image, f)
		a.Delay = append(a.Delay, 10)
	}
	var enc bytes.Buffer
	if err := gif.EncodeAll(&enc, a); err != nil {
		panic(err)
	}
	b := enc.Bytes()
	// After the header, screen descriptor and global table, before the
	// first block (Go's encoder writes a local table per frame).
	at := 13
	if p := b[10]; p&0x80 != 0 {
		at += 3 << ((p & 7) + 1)
	}
	comment := append([]byte{0x21, 0xFE, byte(len(SecretComment))}, SecretComment...)
	comment = append(comment, 0)
	x := xmpPacket()
	xmp := append([]byte{0x21, 0xFF, 11}, "XMP DataXMP"...)
	for len(x) > 0 {
		n := min(len(x), 255)
		xmp = append(xmp, byte(n))
		xmp = append(xmp, x[:n]...)
		x = x[n:]
	}
	xmp = append(xmp, 0)
	var out bytes.Buffer
	out.Write(b[:at])
	out.Write(comment)
	out.Write(xmp)
	out.Write(b[at:])
	out.WriteString(SecretTrailer)
	return out.Bytes()
}

// webpLossless1x1 is a valid 1×1 lossless WebP (VP8L) picture.
const webpLossless1x1 = "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="

func riffChunk(typ string, data []byte) []byte {
	out := append([]byte(typ), binary.LittleEndian.AppendUint32(nil, uint32(len(data)))...)
	out = append(out, data...)
	if len(data)%2 == 1 {
		out = append(out, 0)
	}
	return out
}

// WebPPixels is the VP8L bitstream inside the WebP fixture: the picture
// itself, which must come out byte for byte.
func WebPPixels() []byte {
	b, _ := base64.StdEncoding.DecodeString(webpLossless1x1)
	return b[20 : 20+13]
}

// WebP is an extended WebP: VP8X, ICCP, the VP8L picture, EXIF (orientation
// 6 and GPS) and XMP.
func WebP() []byte {
	vp8x := []byte{0x20 | 0x08 | 0x04, 0, 0, 0, 0, 0, 0, 0, 0, 0} // ICC, EXIF, XMP; 1×1 canvas
	var body bytes.Buffer
	body.WriteString("WEBP")
	body.Write(riffChunk("VP8X", vp8x))
	body.Write(riffChunk("ICCP", iccProfile()))
	body.Write(riffChunk("VP8L", WebPPixels()))
	body.Write(riffChunk("EXIF", tiffBlock(binary.LittleEndian, Orientation)))
	body.Write(riffChunk("XMP ", xmpPacket()))
	out := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(body.Len()))...)
	return append(out, body.Bytes()...)
}

// Check reports what is wrong with a cleaned copy of f, or nil. It demands
// that no planted secret, GPS IFD, maker note, XMP or text chunk survives,
// that the pixels and the ICC profile are kept, and that the orientation
// survives as the only EXIF tag.
func (f Fixture) Check(out []byte) error {
	for _, s := range secrets {
		if bytes.Contains(out, []byte(s)) {
			return fmt.Errorf("%s: %q survived", f.Name, s)
		}
	}
	if bytes.Contains(out, []byte("xmpmeta")) || bytes.Contains(out, []byte("XMP DataXMP")) {
		return fmt.Errorf("%s: XMP survived", f.Name)
	}
	switch f.Name {
	case "jpeg":
		return checkJPEG(f.Bytes, out)
	case "png":
		return checkPNG(f.Bytes, out)
	case "gif":
		return checkGIF(f.Bytes, out)
	case "webp":
		return checkWebP(out)
	}
	return fmt.Errorf("unknown fixture %s", f.Name)
}

func samePixels(a, b image.Image) error {
	if a.Bounds() != b.Bounds() {
		return fmt.Errorf("bounds %v became %v", a.Bounds(), b.Bounds())
	}
	r := a.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			r1, g1, b1, a1 := a.At(x, y).RGBA()
			r2, g2, b2, a2 := b.At(x, y).RGBA()
			if r1 != r2 || g1 != g2 || b1 != b2 || a1 != a2 {
				return fmt.Errorf("pixel %d,%d changed", x, y)
			}
		}
	}
	return nil
}

// onlyOrientation checks a TIFF block holds IFD0 with exactly one entry, the
// orientation, and no further IFD.
func onlyOrientation(t []byte) error {
	if len(t) < 8 {
		return fmt.Errorf("short TIFF block")
	}
	var bo binary.ByteOrder = binary.LittleEndian
	if t[0] == 'M' {
		bo = binary.BigEndian
	}
	off := int(bo.Uint32(t[4:]))
	if off+2 > len(t) {
		return fmt.Errorf("bad IFD0 offset")
	}
	n := int(bo.Uint16(t[off:]))
	if n != 1 || off+2+12+4 > len(t) {
		return fmt.Errorf("IFD0 has %d entries, want only the orientation", n)
	}
	e := t[off+2:]
	if tag, v := bo.Uint16(e), bo.Uint16(e[8:]); tag != 0x0112 || int(v) != Orientation {
		return fmt.Errorf("IFD0 entry is tag %#x value %d, want orientation %d", tag, v, Orientation)
	}
	if next := bo.Uint32(t[off+2+12:]); next != 0 {
		return fmt.Errorf("an IFD1 (thumbnail) survived")
	}
	return nil
}

func checkJPEG(in, out []byte) error {
	a, err := jpeg.Decode(bytes.NewReader(in))
	if err != nil {
		return err
	}
	b, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		return fmt.Errorf("jpeg: cleaned copy does not decode: %v", err)
	}
	if err := samePixels(a, b); err != nil {
		return fmt.Errorf("jpeg: %v", err)
	}
	var exif, icc int
	for i := 2; i+4 <= len(out) && out[i] == 0xFF; {
		m := out[i+1]
		if m == 0xDA || m == 0xD9 {
			break
		}
		l := int(out[i+2])<<8 | int(out[i+3])
		seg := out[i+4 : i+2+l]
		switch {
		case m == 0xE1 && bytes.HasPrefix(seg, []byte("Exif\x00\x00")):
			exif++
			if err := onlyOrientation(seg[6:]); err != nil {
				return fmt.Errorf("jpeg: %v", err)
			}
		case m == 0xE1:
			return fmt.Errorf("jpeg: an APP1 that is not EXIF survived")
		case m == 0xE2 && bytes.HasPrefix(seg, []byte("ICC_PROFILE\x00")):
			icc++
		case m == 0xED || m == 0xFE:
			return fmt.Errorf("jpeg: segment %#x survived", m)
		}
		i += 2 + l
	}
	if exif != 1 {
		return fmt.Errorf("jpeg: %d EXIF blocks, want one holding the orientation", exif)
	}
	if icc != 1 || !bytes.Contains(out, []byte(ICCMarker)) {
		return fmt.Errorf("jpeg: the ICC profile was lost")
	}
	if !bytes.HasSuffix(out, []byte{0xFF, 0xD9}) {
		return fmt.Errorf("jpeg: data after the end of the picture survived")
	}
	return nil
}

func checkPNG(in, out []byte) error {
	a, err := png.Decode(bytes.NewReader(in))
	if err != nil {
		return err
	}
	b, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		return fmt.Errorf("png: cleaned copy does not decode: %v", err)
	}
	if err := samePixels(a, b); err != nil {
		return fmt.Errorf("png: %v", err)
	}
	var exif, icc int
	for i := 8; i+8 <= len(out); {
		n := int(binary.BigEndian.Uint32(out[i:]))
		typ := string(out[i+4 : i+8])
		switch typ {
		case "eXIf":
			exif++
			if err := onlyOrientation(out[i+8 : i+8+n]); err != nil {
				return fmt.Errorf("png: %v", err)
			}
		case "iCCP":
			icc++
		case "tEXt", "iTXt", "zTXt", "tIME":
			return fmt.Errorf("png: %s survived", typ)
		}
		i += 12 + n
	}
	if exif != 1 {
		return fmt.Errorf("png: %d eXIf chunks, want one holding the orientation", exif)
	}
	if icc != 1 {
		return fmt.Errorf("png: the ICC profile was lost")
	}
	return nil
}

func checkGIF(in, out []byte) error {
	a, err := gif.DecodeAll(bytes.NewReader(in))
	if err != nil {
		return err
	}
	b, err := gif.DecodeAll(bytes.NewReader(out))
	if err != nil {
		return fmt.Errorf("gif: cleaned copy does not decode: %v", err)
	}
	if len(a.Image) != len(b.Image) || a.LoopCount != b.LoopCount {
		return fmt.Errorf("gif: %d frames loop %d became %d frames loop %d", len(a.Image), a.LoopCount, len(b.Image), b.LoopCount)
	}
	for k := range a.Image {
		if err := samePixels(a.Image[k], b.Image[k]); err != nil {
			return fmt.Errorf("gif frame %d: %v", k, err)
		}
	}
	if !bytes.Contains(out, []byte("NETSCAPE2.0")) {
		return fmt.Errorf("gif: the loop extension was lost")
	}
	return nil
}

func checkWebP(out []byte) error {
	if len(out) < 12 || string(out[:4]) != "RIFF" || string(out[8:12]) != "WEBP" {
		return fmt.Errorf("webp: not a WebP file")
	}
	if int(binary.LittleEndian.Uint32(out[4:])) != len(out)-8 {
		return fmt.Errorf("webp: RIFF size does not match the file")
	}
	var flags byte
	var exif, icc, pix int
	for i := 12; i+8 <= len(out); {
		typ := string(out[i : i+4])
		n := int(binary.LittleEndian.Uint32(out[i+4:]))
		if i+8+n > len(out) {
			return fmt.Errorf("webp: chunk %q overruns the file", typ)
		}
		d := out[i+8 : i+8+n]
		switch typ {
		case "VP8X":
			flags = d[0]
		case "EXIF":
			exif++
			if err := onlyOrientation(d); err != nil {
				return fmt.Errorf("webp: %v", err)
			}
		case "ICCP":
			icc++
		case "VP8L":
			if !bytes.Equal(d, WebPPixels()) {
				return fmt.Errorf("webp: the picture data changed")
			}
			pix++
		case "XMP ":
			return fmt.Errorf("webp: XMP survived")
		}
		i += 8 + n + n&1
	}
	if exif != 1 || flags&0x08 == 0 {
		return fmt.Errorf("webp: want one EXIF chunk holding the orientation, flagged in VP8X")
	}
	if flags&0x04 != 0 {
		return fmt.Errorf("webp: the XMP flag is still set")
	}
	if icc != 1 || pix != 1 {
		return fmt.Errorf("webp: the ICC profile or the picture was lost")
	}
	return nil
}
