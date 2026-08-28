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
