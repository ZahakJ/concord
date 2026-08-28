// One way to turn a file the user chose into a data URI, shared by every
// surface that takes a picture: the guild icon and banner, and now the icon
// on the create-a-guild dialog.
//
// The bytes are kept RAW — no canvas re-encode — so an animated GIF still
// animates after the round trip. That is why the size check is on the encoded
// string rather than on file.size: base64 costs a third more than the file on
// disk, and the number that matters is what has to fit in a gossip frame.

// MAX_IMAGE_CHARS is the data-URI length ceiling. It is deliberately expressed
// in encoded characters, which is the unit the wire actually spends.
export const MAX_IMAGE_CHARS = 500 * 1024;

// readImageFile hands the data URI to `onOk`, or a human sentence to `onFail`.
// Anything that is not an image is ignored rather than reported: it arrives
// from a paste or a drop, where the user was not claiming to give us a picture.
export function readImageFile(file, onOk, onFail) {
  if (!file || !file.type.startsWith("image/")) return;
  const reader = new FileReader();
  reader.onload = () => {
    const uri = String(reader.result);
    if (uri.length > MAX_IMAGE_CHARS) {
      onFail?.("Image too large — keep it under ~350 KB");
      return;
    }
    onOk(uri);
  };
  reader.readAsDataURL(file);
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
