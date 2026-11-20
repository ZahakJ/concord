// mediastrip.js — a file Concord holds back until the person chooses.
//
// The core removes the location and device details stored in a video's or a
// HEIC/AVIF photo's index before it sends it (internal/bmffmeta). It holds a
// file back, and SendFile answers with an error the row matches here, when:
//   - it cannot clean the file safely, or the file is a picture or video
//     format it has no cleaner for ("…details can't be removed from this
//     video…", app.MediaAsIsMarker): the row offers "Send as it is";
//   - the video has subtitle or text tracks, which the person may
//     have meant to send and which can also hold location ("…video has
//     subtitles or other text tracks…", app.MediaTextMarker): the row offers
//     "Remove and send" or "Send as it is".
// Nothing goes out until a button is pressed, so the person is told at the
// moment it matters, for that file only. What is removed is what the file's
// index stores; anything written into the picture or sound itself stays.

const AS_IS = /details can't be removed from this (video|photo|file)\b/;
const TEXT = /video has subtitles or other text tracks/;

// asIsKind: "video", "photo" or "file" when err is that refusal, else "".
export function asIsKind(err) {
  const m = String(err?.message ?? err ?? "").match(AS_IS);
  return m ? m[1] : "";
}

// hasTextTracks: err is the core's question about a video's text tracks.
export function hasTextTracks(err) {
  return TEXT.test(String(err?.message ?? err ?? ""));
}

// asIsLine: what the row says about the one file it holds.
export function asIsLine(kind) {
  const what = kind === "video" || kind === "photo" ? kind : "file";
  return `Concord couldn't remove the details stored in this ${what}. If you send it, it goes as it is, including any location stored in it.`;
}

export const TEXT_LINE =
  "This video has subtitles or other text tracks, which can hold location. Concord can remove them along with the other details, or send the video as it is.";

export const AS_IS_BUTTON = "Send as it is";
export const REMOVE_BUTTON = "Remove and send";

// heldRow: what a failed send patches onto its row when the core held the
// file back, or null when it is some other failure.
export function heldRow(err) {
  const base = { state: "failed", retryAt: 0, offline: false, timedOut: false };
  if (hasTextTracks(err)) return { ...base, error: TEXT_LINE, asIs: "video", textTracks: true };
  const kind = asIsKind(err);
  if (kind) return { ...base, error: asIsLine(kind), asIs: kind, textTracks: false };
  return null;
}

// fileSendOp: which core call sends a file attachment, after the person's
// choice on its row (none, "Send as it is", or "Remove and send").
export function fileSendOp(att) {
  if (att?.asIsChosen) return "sendFileAsIs";
  if (att?.dropTextChosen) return "sendFileWithoutText";
  return "sendFile";
}
