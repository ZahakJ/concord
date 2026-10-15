// sharein.js — the page's half of the Android share sheet.
//
// MainActivity hands the page one "shareIn" event per share. Text arrives as
// `text`; pictures and videos arrive as `files`, each a copy the native side
// made in the app's private cache (the original content:// URI is readable
// only by the activity, and only for a while), plus `skipped` for anything it
// refused. The page fetches each copy through Capacitor's local file route,
// stages it with the composer's attachFile — the same door a picked or dropped
// file uses — and then asks the native side to delete the copies.
//
// Everything here is pure, so the rules can be tested without a phone.

// Mirrors Composer.svelte's MAX_FILE_BYTES and ShareIntake.MAX_BYTES.
export const SHARE_MAX_BYTES = 25 * 1024 * 1024;

const MEDIA = /^(image|video)\//;

// parseShare reads an event into { text, files, skipped }, dropping anything
// malformed. A file entry must name a path, and a media type the composer can
// stage; anything else is reported as skipped rather than vanishing.
export function parseShare(ev) {
  const out = { text: "", files: [], skipped: [] };
  if (!ev || typeof ev !== "object") return out;
  if (typeof ev.text === "string") out.text = ev.text.trim();
  for (const f of Array.isArray(ev.files) ? ev.files : []) {
    if (!f || typeof f.path !== "string" || !f.path) continue;
    const type = typeof f.type === "string" ? f.type : "";
    const name = typeof f.name === "string" && f.name.trim() ? f.name.trim() : "shared";
    const size = Number(f.size) || 0;
    if (!MEDIA.test(type)) {
      out.skipped.push({ name, reason: "type", path: f.path });
      continue;
    }
    if (size > SHARE_MAX_BYTES) {
      out.skipped.push({ name, reason: "size", path: f.path });
      continue;
    }
    out.files.push({ path: f.path, name, type, size });
  }
  for (const s of Array.isArray(ev.skipped) ? ev.skipped : []) {
    if (!s || typeof s !== "object") continue;
    out.skipped.push({ name: typeof s.name === "string" ? s.name : "", reason: String(s.reason || "unreadable") });
  }
  return out;
}

// fileSrc turns a native path into a URL the page can fetch. Capacitor's own
// convertFileSrc when it is there; the same shape by hand when it is not.
export function fileSrc(path, cap) {
  if (cap?.convertFileSrc) return cap.convertFileSrc(path);
  return `${typeof location !== "undefined" ? location.origin : ""}/_capacitor_file_${path}`;
}

// skippedNote says, in one line, what of a share did not make it — or "" when
// everything did. The reasons are the native side's and this file's own.
export function skippedNote(skipped) {
  if (!skipped?.length) return "";
  const by = { size: 0, type: 0, count: 0, unreadable: 0 };
  for (const s of skipped) by[s.reason in by ? s.reason : "unreadable"]++;
  const parts = [];
  const n = (k, one, many) => (by[k] === 1 ? one : `${by[k]} ${many}`);
  if (by.size) parts.push(`${n("size", "1 file", "files")} over 25 MB`);
  if (by.type) parts.push(`${n("type", "1 file", "files")} not a picture or video`);
  if (by.count) parts.push(`${n("count", "1 more", "more")} past the 10-item limit`);
  if (by.unreadable) parts.push(`${n("unreadable", "1 file", "files")} couldn't be read`);
  return `Not attached: ${parts.join(", ")}`;
}
