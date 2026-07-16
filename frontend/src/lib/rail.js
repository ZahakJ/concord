// rail.js — the guild rail's layout model: ordering + Discord-style folders.
//
// This is a DEVICE-LOCAL preference (persisted to localStorage by state), not
// part of any guild's cryptographic state. A "layout" is an ordered array of
// entries, each either a guild reference or a folder that groups guilds:
//
//   { t: "g", id }                                   — a guild at top level
//   { t: "f", id, name, color, open, ids: [gid,...] } — a folder of guilds
//
// Every function here is pure (returns a new array) so the logic is testable
// in isolation and the reactive layer stays a thin wrapper.

export const DEFAULT_FOLDER_COLOR = "#5865f2";

export function makeFolderId() {
  return "fld_" + Math.random().toString(36).slice(2, 9);
}

const isFolder = (e) => e && e.t === "f";
const isGuild = (e) => e && e.t === "g";

/** Every guild id that appears anywhere in the layout, in visual order. */
export function guildIdsInLayout(items) {
  const out = [];
  for (const e of items) {
    if (isGuild(e)) out.push(e.id);
    else if (isFolder(e)) out.push(...e.ids);
  }
  return out;
}

/**
 * Normalize a layout against the live set of guild ids:
 *  - drop ids that no longer exist (top level and inside folders)
 *  - de-duplicate (keep first occurrence)
 *  - dissolve folders that fall below 2 guilds (survivor kept at the folder's
 *    slot), matching Discord
 *  - append genuinely new guilds at the end as top-level entries
 */
export function reconcile(items, liveIds) {
  const live = new Set(liveIds);
  const seen = new Set();
  const out = [];
  for (const e of items || []) {
    if (isGuild(e)) {
      if (live.has(e.id) && !seen.has(e.id)) {
        seen.add(e.id);
        out.push({ t: "g", id: e.id });
      }
    } else if (isFolder(e)) {
      const ids = e.ids.filter((id) => live.has(id) && !seen.has(id));
      for (const id of ids) seen.add(id);
      if (ids.length >= 2) {
        out.push({
          t: "f",
          id: e.id || makeFolderId(),
          name: e.name ?? "",
          color: e.color || DEFAULT_FOLDER_COLOR,
          open: !!e.open,
          ids,
        });
      } else {
        // Dissolve: survivors (0 or 1) drop to top level in place.
        for (const id of ids) out.push({ t: "g", id });
      }
    }
  }
  // Append new guilds not yet placed anywhere.
  for (const id of liveIds) {
    if (!seen.has(id)) {
      seen.add(id);
      out.push({ t: "g", id });
    }
  }
  return out;
}

/** Remove a guild from wherever it sits; returns items without it. */
function extractGuild(items, id) {
  const out = [];
  for (const e of items) {
    if (isGuild(e)) {
      if (e.id !== id) out.push(e);
    } else if (isFolder(e)) {
      const ids = e.ids.filter((x) => x !== id);
      if (ids.length === e.ids.length) out.push(e);
      else if (ids.length >= 2) out.push({ ...e, ids });
      else out.push(...ids.map((x) => ({ t: "g", id: x }))); // dissolve
    }
  }
  return out;
}

/**
 * Move a guild to a destination:
 *   { kind: "top", index }                 — top level at index
 *   { kind: "folder", folderId, index? }   — into a folder (append if no index)
 * Index is interpreted against the layout AFTER removal.
 */
export function moveGuild(items, id, dest) {
  const without = extractGuild(items, id);
  if (dest.kind === "folder") {
    return without.map((e) => {
      if (!isFolder(e) || e.id !== dest.folderId) return e;
      const ids = e.ids.slice();
      const at = dest.index == null ? ids.length : Math.max(0, Math.min(ids.length, dest.index));
      ids.splice(at, 0, id);
      return { ...e, ids };
    });
  }
  const at = Math.max(0, Math.min(without.length, dest.index ?? without.length));
  const out = without.slice();
  out.splice(at, 0, { t: "g", id });
  return out;
}

/**
 * Drop guild `dragId` onto top-level guild `targetId`: create a folder holding
 * [targetId, dragId] at the target's slot. If the target is already a folder's
 * member this is a no-op-ish (handled by moveGuild elsewhere).
 */
export function combineGuilds(items, dragId, targetId, color = DEFAULT_FOLDER_COLOR) {
  if (dragId === targetId) return items;
  const without = extractGuild(items, dragId);
  const idx = without.findIndex((e) => isGuild(e) && e.id === targetId);
  if (idx === -1) return items; // target not a top-level guild
  const folder = {
    t: "f",
    id: makeFolderId(),
    name: "",
    color,
    open: false,
    ids: [targetId, dragId],
  };
  const out = without.slice();
  out.splice(idx, 1, folder);
  return out;
}

/** Move a top-level folder to a new top-level index. */
export function moveFolder(items, folderId, index) {
  const cur = items.findIndex((e) => isFolder(e) && e.id === folderId);
  if (cur === -1) return items;
  const folder = items[cur];
  const without = items.filter((_, i) => i !== cur);
  const at = Math.max(0, Math.min(without.length, index));
  const out = without.slice();
  out.splice(at, 0, folder);
  return out;
}

const mapFolder = (items, folderId, fn) =>
  items.map((e) => (isFolder(e) && e.id === folderId ? fn(e) : e));

export function renameFolder(items, folderId, name) {
  return mapFolder(items, folderId, (f) => ({ ...f, name }));
}
export function setFolderColor(items, folderId, color) {
  return mapFolder(items, folderId, (f) => ({ ...f, color: color || DEFAULT_FOLDER_COLOR }));
}
export function toggleFolder(items, folderId, open) {
  return mapFolder(items, folderId, (f) => ({ ...f, open: open == null ? !f.open : open }));
}

/** Replace a folder with its guilds, at the folder's position. */
export function dissolveFolder(items, folderId) {
  const out = [];
  for (const e of items) {
    if (isFolder(e) && e.id === folderId) out.push(...e.ids.map((id) => ({ t: "g", id })));
    else out.push(e);
  }
  return out;
}
