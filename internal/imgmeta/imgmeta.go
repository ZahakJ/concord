// Package imgmeta removes metadata from pictures before they leave the device.
//
// Every format is handled by walking its container and copying the parts that
// make up the picture, so the compressed pixel data is never decoded or
// re-encoded: the output shows the same pixels as the input, byte for byte.
// What is kept, per format:
//
//   - JPEG: the JFIF header (without its thumbnail), the ICC profile (APP2
//     ICC_PROFILE), the Adobe APP14 colour-transform marker and every segment
//     the decoder needs (tables, frame and scan headers, entropy data). EXIF
//     is replaced by a 32-byte EXIF block that holds the orientation tag and
//     nothing else, and only when the orientation is not already upright.
//     XMP, extended XMP, IPTC/Photoshop (APP13), MPF and other APPn segments,
//     comments, and anything after the end-of-image marker are dropped.
//   - PNG: the critical chunks, colour chunks (iCCP, sRGB, gAMA, cHRM, cICP,
//     mDCV, cLLI, sBIT), tRNS, bKGD, hIST, pHYs, sPLT and the APNG chunks.
//     eXIf becomes an orientation-only eXIf as for JPEG. tEXt, iTXt, zTXt,
//     tIME and every other ancillary chunk are dropped.
//   - WebP: VP8X, VP8, VP8L, ALPH, ANIM, ANMF and ICCP. EXIF becomes an
//     orientation-only EXIF chunk, XMP and unknown chunks are dropped, and the
//     VP8X flags and RIFF size are rewritten to match.
//   - GIF: image data, graphic control and plain-text extensions, and the
//     NETSCAPE2.0 loop extension (plus ICCRGBG1 when a GIF carries a colour
//     profile). Comment and other application extensions (XMP among them) are
//     dropped, and so is anything after the trailer.
//
// Orientation is kept as a tag rather than applied to the pixels. Turning a
// JPEG would mean re-encoding it, which costs quality, and every renderer the
// app ships in applies the tag on display. A 32-byte tag tells nothing beyond
// which way up the picture goes.
//
// The work is one linear pass over the input with one output buffer no larger
// than the input plus 32 bytes; nothing else is allocated per byte of image.
package imgmeta

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
)

// ErrUnsupported is returned for a picture in a container this package cannot
// clean (AVIF and HEIC, which keep EXIF in an ISO-BMFF item).
var ErrUnsupported = errors.New("imgmeta: this picture format cannot be cleaned")

// ErrMalformed is returned when bytes that start like a known format do not
// follow its structure closely enough to separate metadata from pixels.
var ErrMalformed = errors.New("imgmeta: malformed picture")

// Format names the container a picture is in, by its first bytes: "jpeg",
// "png", "gif", "webp", "isobmff" for AVIF/HEIC (and video), or "".
func Format(b []byte) string {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "jpeg"
	case len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		return "png"
	case len(b) >= 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a"):
		return "gif"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "webp"
	case len(b) >= 12 && string(b[4:8]) == "ftyp":
		return "isobmff"
	}
	return ""
}

// Strip returns the picture without its metadata. The format is read from the
// bytes, not from any name or declared type, because a file called .png is
// often a JPEG. Bytes in no format this package knows come back unchanged:
// they carry no metadata container it could find. AVIF and HEIC return
// ErrUnsupported.
func Strip(b []byte) ([]byte, error) {
	switch Format(b) {
	case "jpeg":
		return stripJPEG(b)
	case "png":
		return stripPNG(b)
	case "gif":
		return stripGIF(b)
	case "webp":
		return stripWebP(b)
	case "isobmff":
		return nil, ErrUnsupported
	}
	return b, nil
}

// orientationTIFF is a big-endian TIFF block with one IFD entry: Orientation.
func orientationTIFF(o int) []byte {
	return []byte{
		'M', 'M', 0, 42, 0, 0, 0, 8, // header, IFD0 at offset 8
		0, 1, // one entry
		0x01, 0x12, 0, 3, 0, 0, 0, 1, 0, byte(o), 0, 0, // Orientation, SHORT, 1, value
		0, 0, 0, 0, // no next IFD
	}
}

// tiffOrientation reads the Orientation tag from IFD0 of a TIFF block, or 0.
func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 0
	}
	var bo binary.ByteOrder
	switch string(t[:4]) {
	case "II*\x00":
		bo = binary.LittleEndian
	case "MM\x00*":
		bo = binary.BigEndian
	default:
		return 0
	}
	off := int(bo.Uint32(t[4:8]))
	if off < 8 || off+2 > len(t) {
		return 0
	}
	n := int(bo.Uint16(t[off:]))
	for i := 0; i < n; i++ {
		e := off + 2 + 12*i
		if e+12 > len(t) {
			return 0
		}
		if bo.Uint16(t[e:]) == 0x0112 && bo.Uint16(t[e+2:]) == 3 {
			o := int(bo.Uint16(t[e+8:]))
			if o >= 1 && o <= 8 {
				return o
			}
			return 0
		}
	}
	return 0
}

// keepOrientation reports whether an orientation must be carried: anything
// but absent or upright.
func keepOrientation(o int) bool { return o >= 2 && o <= 8 }

// ---- JPEG ----

func stripJPEG(in []byte) ([]byte, error) {
	out := make([]byte, 0, len(in)+32)
	out = append(out, 0xFF, 0xD8)
	i := 2
	inScan := false
	exifDone := false
	for i < len(in) {
		if inScan {
			// Entropy-coded data: copy up to the next marker that is neither a
			// stuffed zero nor a restart marker.
			j := i
			for {
				k := bytes.IndexByte(in[j:], 0xFF)
				if k < 0 {
					// No end-of-image: a truncated file. What is left is scan
					// data, so it is kept as it is.
					return append(out, in[i:]...), nil
				}
				j += k
				if j+1 >= len(in) {
					return append(out, in[i:]...), nil
				}
				n := in[j+1]
				if n == 0x00 || (n >= 0xD0 && n <= 0xD7) {
					j += 2
					continue
				}
				if n == 0xFF { // fill byte before a marker
					j++
					continue
				}
				break
			}
			out = append(out, in[i:j]...)
			i = j
			inScan = false
			continue
		}
		if in[i] != 0xFF {
			return nil, ErrMalformed
		}
		for i < len(in) && in[i] == 0xFF {
			i++
		}
		if i >= len(in) {
			break
		}
		m := in[i]
		i++
		switch {
		case m == 0xD9: // end of image; whatever follows is not the picture
			return append(out, 0xFF, 0xD9), nil
		case m == 0x01 || (m >= 0xD0 && m <= 0xD7):
			out = append(out, 0xFF, m)
			continue
		}
		if i+2 > len(in) {
			break
		}
		l := int(in[i])<<8 | int(in[i+1])
		if l < 2 {
			return nil, ErrMalformed
		}
		end := i + l
		truncated := end > len(in)
		if truncated {
			end = len(in)
		}
		seg := in[i+2 : end]
		switch keepJPEGSegment(m, seg) {
		case segKeep:
			out = append(out, 0xFF, m)
			out = append(out, in[i:end]...)
		case segJFIF:
			// JFIF header with its thumbnail cut: 14 bytes of identifier,
			// version, units and density, and a 0x0 thumbnail.
			out = append(out, 0xFF, 0xE0, 0, 16)
			out = append(out, seg[:12]...)
			out = append(out, 0, 0)
		case segExif:
			if !exifDone {
				exifDone = true
				if o := tiffOrientation(seg[6:]); keepOrientation(o) {
					t := orientationTIFF(o)
					out = append(out, 0xFF, 0xE1, 0, byte(2+6+len(t)))
					out = append(out, "Exif\x00\x00"...)
					out = append(out, t...)
				}
			}
		}
		if truncated {
			return out, nil
		}
		i = end
		if m == 0xDA {
			inScan = true
		}
	}
	return out, nil
}

const (
	segDrop = iota
	segKeep
	segJFIF
	segExif
)

func keepJPEGSegment(m byte, seg []byte) int {
	switch {
	case m == 0xE0:
		if len(seg) >= 14 && string(seg[:5]) == "JFIF\x00" {
			if len(seg) == 14 && seg[12] == 0 && seg[13] == 0 {
				return segKeep
			}
			return segJFIF
		}
		return segDrop // JFXX thumbnails, AVI1 and the like
	case m == 0xE1:
		if len(seg) >= 6 && string(seg[:6]) == "Exif\x00\x00" {
			return segExif
		}
		return segDrop // XMP and extended XMP
	case m == 0xE2:
		if len(seg) >= 12 && string(seg[:12]) == "ICC_PROFILE\x00" {
			return segKeep
		}
		return segDrop // MPF, FlashPix
	case m == 0xEE:
		if len(seg) >= 5 && string(seg[:5]) == "Adobe" {
			return segKeep // colour transform; the decoder needs it for CMYK
		}
		return segDrop
	case m >= 0xE3 && m <= 0xEF:
		return segDrop // APP13 (IPTC, Photoshop) and other application data
	case m == 0xFE:
		return segDrop // comment
	}
	return segKeep // tables, frame and scan headers, restart interval
}

// ---- PNG ----

var pngKeep = map[string]bool{
	"IHDR": true, "PLTE": true, "IDAT": true, "IEND": true,
	"tRNS": true, "cHRM": true, "gAMA": true, "iCCP": true, "sBIT": true,
	"sRGB": true, "cICP": true, "mDCV": true, "cLLI": true,
	"bKGD": true, "hIST": true, "pHYs": true, "sPLT": true,
	"acTL": true, "fcTL": true, "fdAT": true,
}

func pngChunk(out []byte, typ string, data []byte) []byte {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(data)))
	out = append(out, n[:]...)
	start := len(out)
	out = append(out, typ...)
	out = append(out, data...)
	binary.BigEndian.PutUint32(n[:], crc32.ChecksumIEEE(out[start:]))
	return append(out, n[:]...)
}

func stripPNG(in []byte) ([]byte, error) {
	out := make([]byte, 0, len(in)+32)
	out = append(out, in[:8]...)
	i := 8
	exifDone := false
	for i+8 <= len(in) {
		n := int(binary.BigEndian.Uint32(in[i:]))
		typ := string(in[i+4 : i+8])
		if n < 0 || n > 1<<31-1 {
			return nil, ErrMalformed
		}
		end := i + 12 + n
		// A chunk is critical when the first letter of its type is upper
		// case. An unknown critical chunk is kept: a decoder needs it.
		keep := pngKeep[typ] || typ[0]&0x20 == 0
		if end > len(in) { // truncated: keep a picture chunk as it is
			if keep {
				out = append(out, in[i:]...)
			}
			return out, nil
		}
		switch {
		case typ == "eXIf":
			if !exifDone {
				exifDone = true
				if o := tiffOrientation(in[i+8 : i+8+n]); keepOrientation(o) {
					out = pngChunk(out, "eXIf", orientationTIFF(o))
				}
			}
		case keep:
			out = append(out, in[i:end]...)
		}
		i = end
		if typ == "IEND" {
			return out, nil
		}
	}
	return out, nil
}

// ---- WebP ----

var webpKeep = map[string]bool{
	"VP8X": true, "VP8 ": true, "VP8L": true, "ALPH": true,
	"ANIM": true, "ANMF": true, "ICCP": true,
}

func stripWebP(in []byte) ([]byte, error) {
	out := make([]byte, 0, len(in)+32)
	out = append(out, in[:12]...)
	riffEnd := 8 + int(binary.LittleEndian.Uint32(in[4:8]))
	if riffEnd > len(in) || riffEnd < 12 {
		riffEnd = len(in)
	}
	i := 12
	vp8x := -1
	hasExif := false
	exifDone := false
	for i+8 <= riffEnd {
		typ := string(in[i : i+4])
		n := int(binary.LittleEndian.Uint32(in[i+4:]))
		end := i + 8 + n + n&1
		if n < 0 || end < i {
			return nil, ErrMalformed
		}
		truncated := end > riffEnd
		if truncated {
			end = riffEnd
		}
		switch {
		case typ == "EXIF":
			if !exifDone && !truncated {
				exifDone = true
				t := in[i+8 : i+8+n]
				t = bytes.TrimPrefix(t, []byte("Exif\x00\x00"))
				// Only an extended file (VP8X first) may carry EXIF at all.
				if o := tiffOrientation(t); keepOrientation(o) && vp8x >= 0 {
					tt := orientationTIFF(o)
					var hdr [8]byte
					copy(hdr[:], "EXIF")
					binary.LittleEndian.PutUint32(hdr[4:], uint32(len(tt)))
					out = append(out, hdr[:]...)
					out = append(out, tt...)
					hasExif = true
				}
			}
		case webpKeep[typ]:
			if typ == "VP8X" && vp8x < 0 {
				vp8x = len(out)
			}
			out = append(out, in[i:end]...)
		}
		if truncated {
			break
		}
		i = end
	}
	if vp8x >= 0 && vp8x+9 <= len(out) {
		f := out[vp8x+8] &^ 0x0C // XMP and EXIF flags
		if hasExif {
			f |= 0x08
		}
		out[vp8x+8] = f
	}
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)-8))
	return out, nil
}

// ---- GIF ----

// gifSubBlocks returns the index just past a run of data sub-blocks starting
// at i (the zero-length terminator included), or -1 when the input ends first.
func gifSubBlocks(in []byte, i int) int {
	for i < len(in) {
		n := int(in[i])
		i++
		if n == 0 {
			return i
		}
		i += n
	}
	return -1
}

func stripGIF(in []byte) ([]byte, error) {
	if len(in) < 13 {
		return nil, ErrMalformed
	}
	out := make([]byte, 0, len(in))
	i := 13
	if p := in[10]; p&0x80 != 0 {
		i += 3 << ((p & 7) + 1)
	}
	if i > len(in) {
		return nil, ErrMalformed
	}
	out = append(out, in[:i]...)
	for i < len(in) {
		switch in[i] {
		case 0x3B: // trailer
			return append(out, 0x3B), nil
		case 0x21: // extension
			if i+2 > len(in) {
				return append(out, 0x3B), nil
			}
			label := in[i+1]
			end := gifSubBlocks(in, i+2)
			if end < 0 {
				return append(out, 0x3B), nil
			}
			keep := label == 0xF9 || label == 0x01
			if label == 0xFF && i+3+11 <= len(in) && in[i+2] == 11 {
				id := string(in[i+3 : i+3+11])
				keep = id == "NETSCAPE2.0" || id == "ICCRGBG1012"
			}
			if keep {
				out = append(out, in[i:end]...)
			}
			i = end
		case 0x2C: // image descriptor, colour table, LZW data
			j := i + 10
			if j > len(in) {
				return append(append(out, in[i:]...), 0x3B), nil
			}
			if p := in[i+9]; p&0x80 != 0 {
				j += 3 << ((p & 7) + 1)
			}
			j++ // LZW minimum code size
			end := -1
			if j <= len(in) {
				end = gifSubBlocks(in, j)
			}
			if end < 0 { // truncated image data: keep it, it is all pixels
				return append(append(out, in[i:]...), 0x3B), nil
			}
			out = append(out, in[i:end]...)
			i = end
		default: // past the last block; a decoder stops here too
			return append(out, 0x3B), nil
		}
	}
	return append(out, 0x3B), nil
}
