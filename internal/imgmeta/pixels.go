package imgmeta

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	_ "image/gif"  // DecodeConfig
	_ "image/jpeg" // DecodeConfig
	_ "image/png"  // DecodeConfig
)

// Size reads a picture's width and height from its header alone, without
// decoding any pixels: PNG, JPEG and GIF through the standard decoders' configs,
// WebP from its first chunk (simple lossy, lossless, or extended).
func Size(b []byte) (w, h int, err error) {
	if Format(b) == "webp" {
		return webpSize(b)
	}
	c, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return 0, 0, err
	}
	return c.Width, c.Height, nil
}

var errWebPHeader = errors.New("imgmeta: bad webp header")

func webpSize(b []byte) (int, int, error) {
	if len(b) < 30 {
		return 0, 0, errWebPHeader
	}
	switch string(b[12:16]) {
	case "VP8 ": // lossy: a key frame header, then 14-bit sizes
		if b[23] != 0x9d || b[24] != 0x01 || b[25] != 0x2a {
			return 0, 0, errWebPHeader
		}
		return int(binary.LittleEndian.Uint16(b[26:]) & 0x3fff), int(binary.LittleEndian.Uint16(b[28:]) & 0x3fff), nil
	case "VP8L": // lossless: a signature byte, then 14-bit sizes minus one
		if b[20] != 0x2f {
			return 0, 0, errWebPHeader
		}
		v := binary.LittleEndian.Uint32(b[21:])
		return int(v&0x3fff) + 1, int(v>>14&0x3fff) + 1, nil
	case "VP8X": // extended: 24-bit canvas sizes minus one
		w := int(b[24]) | int(b[25])<<8 | int(b[26])<<16
		h := int(b[27]) | int(b[28])<<8 | int(b[29])<<16
		return w + 1, h + 1, nil
	}
	return 0, 0, errWebPHeader
}
