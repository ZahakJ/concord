// recent.js — the pure half of the attach menu's "Recent" strip (Composer).
//
// The core lists the photos, videos and files this account sent or received
// lately in any conversation it still belongs to (api.recentMedia, over the
// gallery index: deleted, hidden, expired, unsigned and guest rows, left
// guilds, blocked senders and spoilers are already out). This file repeats
// the checks that can change while a menu is open, drops repeats, caps the
// strip, and turns a pick into the File the composer stages.
//
// A pick is NOT a reference to the old message's attachment. Its plaintext is
// fetched the way the feed opens it and handed to attachFile like a file
// picked from disk, so the send seals it again as a new blob under a new key
// (internal/app/attach.go, sealBlob). Reusing the old blob id and key would
// tell the second conversation which picture of the first it was, and give
// its members a key that opens the first conversation's copy.

export const RECENT_MAX = 12;

const KIND_PHOTO = 1;

// recentAllowed: whether the strip may be drawn at all. It shows pictures,
// names and senders from every conversation, DMs included, so not while this
// device shares its screen, and not in a call a guest is in (a guest may be
// watching someone's shared screen, and is outside every conversation here).
export function recentAllowed({ sharing = false, callMembers = [] } = {}) {
  if (sharing) return false;
  return !callMembers.some((m) => String(m?.fingerprint || "").startsWith("guest:"));
}

// sameItem: whether a fresh listing still offers this item, the same message
// slot and the same blob. A pick is checked against one before anything is
// fetched, so an item deleted, hidden or expired since the strip was drawn
// cannot be added.
export const recentKeyOf = (it) => `${it.messageId}:${it.ord}`;
export function stillOffered(fresh, it) {
  return Array.isArray(fresh) && fresh.some((x) => recentKeyOf(x) === recentKeyOf(it) && x.blobId === it.blobId && x.keys === it.keys);
}

// recentStrip: what the strip shows, newest first (the core's order).
export function recentStrip(items, nowSec) {
  if (!Array.isArray(items)) return [];
  const seen = new Set();
  const out = [];
  for (const it of items) {
    if (!it?.blobId || !it.keys) continue;
    if (it.expires > 0 && it.expires <= nowSec) continue;
    if (it.spoiler || it.unverified || it.guest) continue;
    if (/^audio\//.test(it.mime || "")) continue;
    if (seen.has(it.blobId)) continue;
    seen.add(it.blobId);
    out.push(it);
    if (out.length === RECENT_MAX) break;
  }
  return out;
}

export const isPhoto = (it) => it.kind === KIND_PHOTO;

const EXT = { jpeg: "jpg", png: "png", gif: "gif", webp: "webp" };

export function recentMime(it) {
  if (isPhoto(it)) return `image/${it.subtype || "png"}`;
  return it.mime || "application/octet-stream";
}

export function recentFileName(it) {
  if (it.name) return it.name;
  if (isPhoto(it)) return `photo.${EXT[it.subtype] || "png"}`;
  return "file";
}

// recentRequest: the api call that opens a pick's plaintext, the feed's own.
export function recentRequest(it) {
  if (isPhoto(it)) return { method: "fetchAttachment", args: [it.channelId, it.blobId, it.keys, it.subtype] };
  return { method: "fetchFile", args: [it.channelId, it.blobId, it.keys, recentMime(it)] };
}

// dataUrlToFile: a base64 data URL as a File, for attachFile.
export function dataUrlToFile(dataUrl, name, type) {
  const m = /^data:[^;,]*;base64,([A-Za-z0-9+/=]*)$/.exec(String(dataUrl));
  if (!m) throw new Error("not a data URL");
  const bin = atob(m[1]);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  return new File([bytes], name, { type });
}
