// mediastrip.js — a video or HEIC photo whose details Concord could not remove.
//
// The core removes location and device details from every video (MP4, MOV)
// and HEIC/AVIF photo it sends (internal/bmffmeta). A file it cannot clean
// safely is not sent: SendFile answers with an error that names what it was
// ("…details can't be removed from this video…", app.MediaAsIsMarker), and the
// failed row says so in the words below, with a button to send it as it is.
// Nothing goes out until that button is pressed, so the person is told at the
// moment it matters, for that file only.

const AS_IS = /details can't be removed from this (video|photo|file)\b/;

// asIsKind: "video", "photo" or "file" when err is that refusal, else "".
export function asIsKind(err) {
  const m = String(err?.message ?? err ?? "").match(AS_IS);
  return m ? m[1] : "";
}

// asIsLine: what the row says. The same words as the gallery's note ("sent as
// they are, including any location stored in them"), for one file.
export function asIsLine(kind) {
  const what = kind === "video" || kind === "photo" ? kind : "file";
  return `Concord couldn't remove the details stored in this ${what}. If you send it, it goes as it is, including any location stored in it.`;
}

export const AS_IS_BUTTON = "Send as it is";
