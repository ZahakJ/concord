// attachments.js — parsing and caching for encrypted attachment tokens.
// Mirrors the token format defined in internal/app/attach.go:
//
//   ![image](concord://attach/v1/<blobID>/<keys>/<subtype>/<w>x<h>)
//
// The token's charset survives the markdown escape-first pipeline, but we
// extract tokens from the PLAIN message content before rendering, so markdown
// never sees them.
import { api } from "./api.js";

export const ATTACH_RE =
  /!\[image\]\(concord:\/\/attach\/v1\/([0-9a-f]{64})\/([A-Za-z0-9_-]{75})\/(png|jpeg|gif|webp)\/(\d{1,5})x(\d{1,5})\)/g;

// parseAttachTokens returns [{blobId, keys, subtype, w, h}] for a message body.
export function parseAttachTokens(content) {
  const out = [];
  for (const m of content.matchAll(ATTACH_RE)) {
    out.push({ blobId: m[1], keys: m[2], subtype: m[3], w: +m[4], h: +m[5] });
  }
  return out;
}

// stripAttachTokens removes tokens from a body (what's left is the caption).
export function stripAttachTokens(content) {
  return content.replace(ATTACH_RE, "").trim();
}

// hasAttachment: cheap check for preview snippets (replies, pins, notifications).
export function hasAttachment(content) {
  ATTACH_RE.lastIndex = 0;
  return ATTACH_RE.test(content);
}

// previewText: body with tokens replaced by a readable placeholder.
export function previewText(content) {
  const stripped = stripAttachTokens(content);
  return hasAttachment(content) ? (stripped ? `🖼 ${stripped}` : "🖼 image") : content;
}

// Decrypted data-URL cache. 5 MB data URLs are memory-heavy, so keep few.
const CACHE_MAX = 30;
const cache = new Map(); // blobId -> Promise<dataURL>

export function loadAttachment(channelId, tok) {
  let p = cache.get(tok.blobId);
  if (!p) {
    p = api.fetchAttachment(channelId, tok.blobId, tok.keys, tok.subtype).catch((err) => {
      cache.delete(tok.blobId); // don't cache failures; Retry refetches
      throw err;
    });
    cache.set(tok.blobId, p);
    if (cache.size > CACHE_MAX) {
      const oldest = cache.keys().next().value;
      cache.delete(oldest);
    }
  }
  return p;
}
