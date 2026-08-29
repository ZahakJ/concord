// attachments.js — parsing and caching for encrypted attachment tokens.
// Mirrors the token format defined in internal/app/attach.go:
//
//   ![image](concord://attach/v1/<blobID>/<keys>/<subtype>/<w>x<h>)
//
// The token's charset survives the markdown escape-first pipeline, but we
// extract tokens from the PLAIN message content before rendering, so markdown
// never sees them.
import { api } from "./api.js";
import { saveImage } from "./savefile.js";
import { parsePoll } from "./polls.js";

// v1 and v2 in one pattern. v2 appends the composer's per-image options —
// a flag bitmask (1 = spoiler), a filename and a description — and is only
// emitted when one of them is actually set, so ordinary images stay v1 and
// keep rendering on peers running an older build.
export const ATTACH_RE =
  /!\[image\]\(concord:\/\/attach\/v1\/([0-9a-f]{64})\/([A-Za-z0-9_-]{75})\/(png|jpeg|gif|webp)\/(\d{1,5})x(\d{1,5})\)|!\[image\]\(concord:\/\/attach\/v2\/([0-9a-f]{64})\/([A-Za-z0-9_-]{75})\/(png|jpeg|gif|webp)\/(\d{1,5})x(\d{1,5})\/(\d{1,3})\/([A-Za-z0-9_-]*)\/([A-Za-z0-9_-]*)\)/g;

export const ATTACH_SPOILER = 1;

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

// parseAttachTokens returns inline IMAGE tokens
// [{blobId, keys, subtype, w, h, spoiler, name, desc}]. v1 tokens fill the
// group 1-5 slots and v2 the 6-13 ones, so which alternative matched is simply
// which half is defined.
export function parseAttachTokens(content) {
  const out = [];
  for (const m of content.matchAll(ATTACH_RE)) {
    if (m[1]) {
      out.push({ blobId: m[1], keys: m[2], subtype: m[3], w: +m[4], h: +m[5], spoiler: false, name: "", desc: "" });
    } else {
      const flags = +m[11] || 0;
      out.push({
        blobId: m[6],
        keys: m[7],
        subtype: m[8],
        w: +m[9],
        h: +m[10],
        spoiler: (flags & ATTACH_SPOILER) !== 0,
        name: b64urlDecode(m[12]),
        desc: b64urlDecode(m[13]),
      });
    }
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
  const poll = parsePoll(content);
  if (poll) return `📊 ${poll.q || "Poll"}`;
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

// ---- "why isn't this here?" ------------------------------------------------

// fmtBytes renders a byte count the way an attachment card labels one.
export function fmtBytes(n) {
  if (!(n > 0)) return "";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

// unavailableNote turns a failed blob fetch into the sentence a reader can act
// on. A blob lives only on the peers that have held it, so "not found" is not
// a broken link — it is "nobody holding these bytes can be reached right now",
// which is a wait, not a loss. `who` is {name, self}: name the sender when we
// know them, because "wait for Amina" is a far better instruction than "wait".
//
// It deliberately does NOT consult the member roster's online flag. A peer that
// dies without closing its connections keeps its row marked online for the best
// part of a minute, so "they're online, try again" was routinely printed about
// somebody whose process had already exited. What we DO know for certain is the
// thing the fetch just proved: everyone reachable was asked, and nobody had it.
export function unavailableNote(err, who = {}) {
  const msg = String(err?.message || err || "");
  if (!/not found/.test(msg)) return "Couldn't load this one";
  const { name = "", self = false } = who;
  if (self) return "this device doesn't have the file any more, and nobody else reachable does";
  if (name) return `${name} isn't reachable right now — it'll load when they're back`;
  return "nobody who has it is reachable right now — it'll load when they're back";
}

// placeholderName: what to call an attachment that will not load.
//
// A spoiler is deliberately anonymous. Marking one is a promise that the
// surprise survives until the reader asks for it, and a file called
// THE-KILLER-IS-THE-BUTLER.png printed across a placeholder breaks that promise
// more completely than showing the picture would have — the cover is exactly
// the state in which the reader is still looking.
export function placeholderName(tok) {
  if (tok?.spoiler) return "Spoiler";
  return tok?.name || "";
}

// arrived reports whether `now` contains a fingerprint `prev` did not — i.e.
// whether somebody JOINED rather than left.
export function arrived(prev, now) {
  if (!prev) return false;
  for (const fp of now) if (!prev.has(fp)) return true;
  return false;
}

// How long a failed attachment waits before another roster refresh is allowed
// to make it try again. One doomed fetch costs a round of peer requests, so a
// channel full of broken pictures must not be able to turn a flapping guild
// into a fetch storm.
export const RETRY_COOLDOWN_MS = 20000;

// worthRetrying: should this roster refresh make a failed attachment try again?
//
// Watching for an arrival alone is not enough, and the reason is worth writing
// down: a peer that is killed rather than closed keeps its connections — and so
// its "online" row — for up to a minute. A peer that dies and comes back inside
// that window is therefore never observed to have left, its fingerprint never
// leaves the set, and an arrival never happens. So an arrival retries at once,
// and any other refresh retries once the cooldown has passed. `prev` is null on
// the very first observation, when nothing has changed yet.
export function worthRetrying(prev, now, sinceLastTry) {
  if (prev === null || prev === undefined) return false;
  if (arrived(prev, now)) return true;
  return sinceLastTry >= RETRY_COOLDOWN_MS;
}

// ---- image clipboard/save helpers (right-click menu on images) ------------

// copyImageToClipboard puts an image (any src the webview can draw) on the
// system clipboard as a real PNG — pasteable inside Concord or in any other
// app — the "Copy Image" a browser or an image viewer offers.
export async function copyImageToClipboard(src) {
  const img = new Image();
  await new Promise((res, rej) => {
    img.onload = res;
    img.onerror = rej;
    img.src = src;
  });
  const c = document.createElement("canvas");
  c.width = img.naturalWidth;
  c.height = img.naturalHeight;
  c.getContext("2d").drawImage(img, 0, 0);
  const blob = await new Promise((res) => c.toBlob(res, "image/png"));
  await navigator.clipboard.write([new ClipboardItem({ "image/png": blob })]);
}

// saveImageSrc hands an image src to the user. See lib/savefile.js for why
// this is not simply an <a download>: on Android that is a silent no-op, and
// "Save Image" was doing nothing at all there.
export function saveImageSrc(src, name = "concord-image.png") {
  return saveImage(src, name);
}
