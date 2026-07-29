// animated.js — does this image file actually move?
//
// Custom emoji are uploaded through a canvas so they can be downscaled, and a
// canvas holds exactly one frame: drawing an animated GIF through it and
// exporting a PNG silently produces a still. So an animated upload has to be
// passed through byte-for-byte instead, and that means knowing which is which.
//
// Decided from the bytes, not the filename or the MIME type. All three formats
// below are equally valid as still images, and a still is better off taking the
// downscale path — passing everything through untouched would blow the size
// budget on ordinary PNGs.

const ascii = (b, off, s) => s.split("").every((c, i) => b[off + i] === c.charCodeAt(0));

export function isAnimated(bytes) {
  const b = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes || []);
  if (b.length < 16) return false;

  // GIF: animated iff it carries more than one Graphic Control Extension
  // (21 F9 04). Every animated GIF has one per frame; a still may still have a
  // single one, since that block is also where transparency is declared.
  if (ascii(b, 0, "GIF8")) {
    let n = 0;
    for (let i = 0; i < b.length - 2; i++) {
      if (b[i] === 0x21 && b[i + 1] === 0xf9 && b[i + 2] === 0x04 && ++n > 1) return true;
    }
    return false;
  }

  // WebP: only the extended form (VP8X) can animate, and bit 1 of its flag byte
  // is the ANIM flag. A plain VP8/VP8L file is always a still.
  if (ascii(b, 0, "RIFF") && ascii(b, 8, "WEBP")) {
    return ascii(b, 12, "VP8X") && (b[20] & 0x02) !== 0;
  }

  // APNG: a PNG carrying an acTL chunk, which the spec requires to appear
  // before the first IDAT. Reaching IDAT first means it's an ordinary PNG.
  if (b[0] === 0x89 && ascii(b, 1, "PNG")) {
    for (let i = 8; i < b.length - 4; i++) {
      if (ascii(b, i, "IDAT")) return false;
      if (ascii(b, i, "acTL")) return true;
    }
  }
  return false;
}
