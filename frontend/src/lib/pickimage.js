// One way to turn a file the user chose into a data URI, shared by every
// surface that takes a picture: the guild icon and banner, the icon on the
// create-a-guild dialog, forum art, a story scene, custom emoji and the guild
// GIF pack.
//
// A PNG, JPEG, GIF or WebP is kept RAW — no canvas re-encode — so an animated
// GIF still animates after the round trip, and it is read under the type its
// bytes give rather than the one its name gives (see sniffedType). Any other
// picture the renderer can decode (an AVIF saved as .jpg, say) is drawn again
// on a canvas, as the composer does: the core cleans the four formats of their
// metadata and refuses the rest, and the canvas keeps only the pixels. The size
// checks are on the encoded string rather than on file.size: base64 costs a
// third more than the file on disk, and the number that matters is what has to
// fit in a gossip frame.

import { sniffedType } from "./postdraft.js";

// MAX_IMAGE_CHARS is the data-URI length ceiling. It is deliberately expressed
// in encoded characters, which is the unit the wire actually spends.
export const MAX_IMAGE_CHARS = 500 * 1024;

export function readDataURL(blob) {
  return new Promise((res, rej) => {
    const r = new FileReader();
    r.onload = () => res(String(r.result));
    r.onerror = rej;
    r.readAsDataURL(blob);
  });
}

async function decodePicture(file) {
  try {
    return await createImageBitmap(file);
  } catch {
    // Fallback for formats createImageBitmap rejects but <img> can decode.
    const url = URL.createObjectURL(file);
    try {
      return await new Promise((res, rej) => {
        const img = new Image();
        img.onload = () => res(img);
        img.onerror = rej;
        img.src = url;
      });
    } finally {
      URL.revokeObjectURL(url);
    }
  }
}

// redrawPicture draws a decodable picture on a canvas and returns it as a PNG,
// which keeps transparency, or as a JPEG when the PNG would be over maxChars.
// An animation keeps its first frame.
export async function redrawPicture(file, maxChars = Infinity) {
  const bmp = await decodePicture(file);
  const w = bmp.width || bmp.naturalWidth;
  const h = bmp.height || bmp.naturalHeight;
  if (!w || !h) throw new Error("Couldn't read that image");
  const canvas = document.createElement("canvas");
  canvas.width = w;
  canvas.height = h;
  canvas.getContext("2d").drawImage(bmp, 0, 0);
  bmp.close?.();
  const png = canvas.toDataURL("image/png");
  return png.length <= maxChars ? png : canvas.toDataURL("image/jpeg", 0.85);
}

// pickedPicture: the picture as a data URI the core takes. The bytes alone
// decide the route: a PNG, JPEG, GIF or WebP is read as it is whatever its
// size, and only a picture of another format is redrawn. There is no size gate
// here, unlike the composer's nativePicture, which redraws a picture over 5 MB
// to recompress it: every picker's limit is far below that, so a large picture
// in one of the four formats is refused by the caller, as it always was, rather
// than flattened to a still or decoded at full size first. maxChars only
// chooses the canvas encoding; the caller still checks the length, because a
// raw picture is never shrunk.
export async function pickedPicture(file, maxChars = Infinity, { read = readDataURL, redraw = redrawPicture } = {}) {
  const type = await sniffedType(file);
  if (!type) return redraw(file, maxChars);
  return read(file.type === type ? file : new Blob([file], { type }));
}

// readImageFile hands the data URI to `onOk`, or a human sentence to `onFail`.
// Anything that is not an image is ignored rather than reported: it arrives
// from a paste or a drop, where the user was not claiming to give us a picture.
export function readImageFile(file, onOk, onFail) {
  if (!file || !file.type.startsWith("image/")) return;
  pickedPicture(file, MAX_IMAGE_CHARS).then(
    (uri) => {
      if (uri.length > MAX_IMAGE_CHARS) {
        onFail?.("Image too large — keep it under ~350 KB");
        return;
      }
      onOk(uri);
    },
    () => onFail?.("Couldn't read that image"),
  );
}

// pickImageFile opens the OS picker and calls back with a data URI. The input
// is never attached to the document: a detached <input type=file> still opens
// the picker in every engine this app runs in, and one that IS attached has to
// be cleaned up on every path out.
export function pickImageFile(onOk, onFail) {
  const input = document.createElement("input");
  input.type = "file";
  input.accept = "image/*";
  input.onchange = () => readImageFile(input.files?.[0], onOk, onFail);
  input.click();
}
