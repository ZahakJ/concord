// attachopts.js — the composer's per-image options, and the one rule that keeps
// them from breaking older peers.
//
// internal/app/attach.go emits two token formats. The v1 form is what every
// build that has ever shipped can render:
//
//   ![image](concord://attach/v1/<blobID>/<keys>/<subtype>/<w>x<h>)
//
// The v2 form carries spoiler/name/description, and a client that predates it
// cannot parse it at all — it shows ~190 characters of raw token where the
// picture should be. The backend therefore only emits v2 when one of those three
// options is actually set (internal/app/attach.go, sealAttachment).
//
// That contract is only worth anything if the composer holds up its end, so the
// staged-attachment defaults live here rather than inline in Composer.svelte,
// where a plausible-looking "prefill the file name" once quietly opted every
// ordinary image into v2.
//
// Deliberately dependency-free so it can be unit-tested without a DOM.

// stagedImage builds the pending-attachment record for a freshly staged image.
//
// `name` is empty even when the OS gave us a file name: the file name is kept
// separately as `origName`, purely to prefill the rename field's PLACEHOLDER.
// Only a name the user actually typed should travel, because a non-empty name is
// enough on its own to force the v2 token.
export function stagedImage({ id, dataUrl, w, h, fileName = "" }) {
  return {
    id,
    dataUrl,
    w,
    h,
    bytes: dataUrlBytes(dataUrl),
    isImage: true,
    spoiler: false,
    name: "",
    origName: fileName,
    desc: "",
  };
}

// How many bytes this attachment will actually cost, from the data URL that
// will be sent — NOT the size of the file that was picked. An image is often
// re-encoded on the way in, so the two are different numbers and only one of
// them is the answer to "will this go through". A base64 payload is 4 bytes per
// 3, minus whatever the padding claims.
export function dataUrlBytes(dataUrl) {
  const i = String(dataUrl || "").indexOf(",");
  if (i < 0) return 0;
  const b64 = dataUrl.slice(i + 1);
  const pad = b64.endsWith("==") ? 2 : b64.endsWith("=") ? 1 : 0;
  return Math.max(0, Math.floor((b64.length * 3) / 4) - pad);
}

// emitsLegacyToken reports whether a staged attachment will go out as a v1
// token — i.e. whether a peer on an older build will see the image rather than
// raw token text. Mirrors the `!spoiler && name == "" && desc == ""` condition
// in sealAttachment.
export function emitsLegacyToken(a) {
  return !a.spoiler && !a.name && !a.desc;
}

// What the core sends as it is, with whatever is stored in it: a document,
// audio other than M4A and WebM, anything it does not recognise. Pictures, MP4 and
// MOV videos, HEIC/HEIF/AVIF photos and M4A audio are cleaned; the formats
// it cannot clean are held with their own row and "Send as it is"
// (lib/mediastrip.js). The core decides from the file's first bytes
// (app.FileGoesAsIs), not from its name or type, so the page asks it, and
// the composer shows AS_IS_NOTE under the staged file before the send.
export const AS_IS_NOTE =
  "Files other than pictures, videos and M4A audio are sent as they are, including any details stored in them, such as a location or an author's name.";

// FILE_HEAD_CHARS: base64 characters for the first 64 KiB of a file
// (app.FileHeadBytes), the most the core reads to decide.
export const FILE_HEAD_CHARS = 87384;

// fileHead: the start of a base64 data URL's payload, or null when it is not
// base64.
export function fileHead(dataUrl) {
  const s = String(dataUrl || "");
  const comma = s.indexOf(",");
  if (comma < 0 || !s.slice(0, comma).endsWith(";base64")) return null;
  return s.slice(comma + 1, comma + 1 + FILE_HEAD_CHARS);
}

// AS_IS_WAIT_MS: how long staging waits for the core's answer.
export const AS_IS_WAIT_MS = 3000;

// sentAsIs(dataUrl, goesAsIs): does this staged file go out as it is?
// goesAsIs is api.fileGoesAsIs. When the core cannot be asked, or does not
// answer within waitMs, the line shows: saying it once too often is the safe
// side, and the file is staged either way.
export async function sentAsIs(dataUrl, goesAsIs, { waitMs = AS_IS_WAIT_MS } = {}) {
  const head = fileHead(dataUrl);
  if (head === null) return true;
  let timer;
  const late = new Promise((resolve) => {
    timer = setTimeout(() => resolve(true), waitMs);
  });
  try {
    const answer = Promise.resolve()
      .then(() => goesAsIs(head))
      .then((v) => v !== false);
    return await Promise.race([answer, late]);
  } catch {
    return true;
  } finally {
    clearTimeout(timer);
  }
}
