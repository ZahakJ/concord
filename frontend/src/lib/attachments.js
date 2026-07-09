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

// File tokens: [file](concord://file/v1/<blobID>/<keys>/<size>/<mimeB64url>/<nameB64url>)
export const FILE_RE =
  /\[file\]\(concord:\/\/file\/v1\/([0-9a-f]{64})\/([A-Za-z0-9_-]{75})\/(\d{1,9})\/([A-Za-z0-9_-]+)\/([A-Za-z0-9_-]*)\)/g;

const ANY_RE = new RegExp(`${ATTACH_RE.source}|${FILE_RE.source}`, "g");

function b64urlDecode(s) {
  if (!s) return "";
  try {
    return decodeURIComponent(escape(atob(s.replace(/-/g, "+").replace(/_/g, "/"))));
  } catch {
    return "";
  }
}

// parseAttachTokens returns inline IMAGE tokens [{blobId, keys, subtype, w, h}].
export function parseAttachTokens(content) {
  const out = [];
  for (const m of content.matchAll(ATTACH_RE)) {
    out.push({ blobId: m[1], keys: m[2], subtype: m[3], w: +m[4], h: +m[5] });
  }
  return out;
}

// parseFileTokens returns FILE tokens [{blobId, keys, size, mime, name}].
export function parseFileTokens(content) {
  const out = [];
  for (const m of content.matchAll(FILE_RE)) {
    out.push({
      blobId: m[1],
      keys: m[2],
      size: +m[3],
      mime: b64urlDecode(m[4]),
      name: b64urlDecode(m[5]) || "file",
    });
  }
  return out;
}

// stripAttachTokens removes all attachment tokens (what's left is the caption).
export function stripAttachTokens(content) {
  return content.replace(ANY_RE, "").trim();
}

// hasAttachment: cheap check for preview snippets (replies, pins, notifications).
export function hasAttachment(content) {
  ANY_RE.lastIndex = 0;
  return ANY_RE.test(content);
}

// previewText: body with tokens replaced by a readable placeholder.
export function previewText(content) {
  const stripped = stripAttachTokens(content);
  if (!hasAttachment(content)) return content;
  // Reset lastIndex: these are global (/g) regexes, and .test() advances it —
  // leaving it non-zero would make a later matchAll() (parseAttachTokens etc.)
  // start mid-string and silently drop early tokens.
  FILE_RE.lastIndex = 0;
  ATTACH_RE.lastIndex = 0;
  const glyph = FILE_RE.test(content) && !ATTACH_RE.test(content) ? "📎" : "🖼";
  FILE_RE.lastIndex = 0;
  ATTACH_RE.lastIndex = 0;
  const label = glyph === "📎" ? "file" : "image";
  return stripped ? `${glyph} ${stripped}` : `${glyph} ${label}`;
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
