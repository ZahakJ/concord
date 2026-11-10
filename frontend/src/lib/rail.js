// rail.js — the guild rail's layout model: ordering and folders.
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

// Default to the app accent, not a fixed hue — a folder should read as Concord,
// and follow whatever accent the user has themed.
export const DEFAULT_FOLDER_COLOR = "var(--accent)";

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
 *    slot): a folder of one is a guild with extra chrome
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
 *
 * Index is interpreted against the layout the CALLER sees — i.e. BEFORE the
 * dragged guild is removed. The drop hints in GuildRail are computed from the
 * rendered (pre-drag) rail, so compensating for the removal here rather than
 * at every call site is what keeps a downward drag from landing one slot too
 * far (the classic reorder off-by-one).
 */
export function moveGuild(items, id, dest) {
  const without = extractGuild(items, id);
  if (dest.kind === "folder") {
    // Where the guild sat inside the TARGET folder before removal, if it did —
    // a downward reorder within the same folder must account for its own gap.
    const orig = items.find((e) => isFolder(e) && e.id === dest.folderId);
    const from = orig ? orig.ids.indexOf(id) : -1;
    return without.map((e) => {
      if (!isFolder(e) || e.id !== dest.folderId) return e;
      const ids = e.ids.slice();
      let at = dest.index == null ? ids.length : dest.index;
      if (from !== -1 && from < at) at -= 1;
      at = Math.max(0, Math.min(ids.length, at));
      ids.splice(at, 0, id);
      return { ...e, ids };
    });
  }
  // Same compensation at top level: dragging a top-level guild downward means
  // every slot past its old position shifted up by one when it was removed.
  let at = dest.index ?? without.length;
  const from = items.findIndex((e) => isGuild(e) && e.id === id);
  if (from !== -1 && from < at) at -= 1;
  at = Math.max(0, Math.min(without.length, at));
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

/**
 * Move a top-level folder to a new top-level index. As with moveGuild, the
 * index is against the pre-removal layout the caller computed it from, so a
 * downward move compensates for the folder's own vacated slot.
 */
export function moveFolder(items, folderId, index) {
  const cur = items.findIndex((e) => isFolder(e) && e.id === folderId);
  if (cur === -1) return items;
  const folder = items[cur];
  const without = items.filter((_, i) => i !== cur);
  let at = index;
  if (cur < at) at -= 1;
  at = Math.max(0, Math.min(without.length, at));
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

// guildInitials is ONE rule for the two letters a guild wears when it has no
// icon. The rail already had it; the settings panel had its own, which took
// only the first character — so the same guild read "RM" in the rail and "R"
// in settings, three inches apart, and looked like two different places.
//
// Uppercased, because a lowercase guild name ("dar al-hikma") should still
// produce a plate that reads as a monogram, and trimmed of the whitespace that
// makes `split` emit an empty first word.
export function guildInitials(name) {
  const letters = String(name || "")
    .trim()
    .split(/\s+/)
    .map((w) => [...w][0] || "");
  // Sliced by CODEPOINT, not by UTF-16 unit: an emoji first letter is two
  // units, so a plain slice(0, 2) returned the emoji alone and dropped the
  // second word's initial entirely.
  return [...letters.join("")].slice(0, 2).join("").toUpperCase();
}

// ————————————————— the shortcut strip —————————————————
//
// The sidebar's strip is a SHORTCUT row, not the inventory (the guild switcher
// is): Messages, then as many guild tiles as fit, then an overflow tile that
// opens the switcher. Which guilds earn a tile is a stated rule, so the row is
// predictable, and the rule is written so that a tile never moves because a
// number changed under it.

// How many guild tiles fit a sidebar this wide: n guild tiles plus the
// Messages and overflow tiles are (n + 2) × 28px with (n + 1) 4px gaps, inside
// 10px of padding each side — so 32n + 80 ≤ width. 6 at the default 280px, 5
// at 240 and at the tablet's 264, 8 at 360.
export function stripFit(width) {
  return Math.max(1, Math.floor((width - 80) / 32));
}

// classOf answers which of the four strip classes a guild is in right now, or
// "" for none: pinned by the user, else a mention aimed at you, else plain
// unread, else opened at some point on this device.
export function stripClass(id, { pins, mentions, unread, opened }) {
  if (pins.includes(id)) return "pin";
  if (mentions.has(id)) return "mention";
  if (unread.has(id)) return "unread";
  if (opened.has(id)) return "recent";
  return "";
}

// noteEntries records WHEN each guild entered the class it is in now — the
// stability half of the rule. `prev` is the caller's map from the last pass
// ({id: {cls, at}}); a guild whose class is unchanged keeps its old time, one
// that moved gets `now`. Returns a new map; guilds in no class are dropped.
export function noteEntries(prev, ids, ctx, now) {
  const out = {};
  for (const id of ids) {
    const cls = stripClass(id, ctx);
    if (!cls) continue;
    const was = prev?.[id];
    out[id] = was && was.cls === cls ? was : { cls, at: now };
  }
  return out;
}

// stripTiles picks and orders the strip's guild tiles.
//
//   ids      every guild id, in the switcher's order (the tie-break)
//   ctx      { pins: [ids in the user's order], mentions: Set, unread: Set,
//             opened: Map id → last opened time }
//   entries  noteEntries' map: when each guild entered its class
//   current  the guild on screen ("" for none)
//   n        how many tiles fit
//
// Pinned guilds come first in the user's order; then mentions, then unread,
// then the most recently opened — each of those three ordered by when the
// guild ENTERED the class, oldest first, so a newcomer joins at the right and
// nothing to its left moves — then, while slots remain, the rest in the
// switcher's order. The current guild is always on the strip: if the
// rule did not pick it, it takes the last slot. Returns { shown, hidden }.
export function stripTiles(ids, ctx, entries, current, n) {
  const pos = new Map(ids.map((id, i) => [id, i]));
  const live = (id) => pos.has(id);
  const byEntry = (a, b) =>
    (entries[a]?.at ?? 0) - (entries[b]?.at ?? 0) || pos.get(a) - pos.get(b);
  const inClass = (cls) => ids.filter((id) => entries[id]?.cls === cls);
  const pins = ctx.pins.filter(live);
  const mention = inClass("mention").sort(byEntry);
  const unread = inClass("unread").sort(byEntry);
  // Recent is the only class with more members than slots as a rule, so the
  // slots go to the most recently OPENED, and only then are they laid out by
  // when they arrived.
  const room = Math.max(0, n - pins.length - mention.length - unread.length);
  const recent = inClass("recent")
    .sort((a, b) => (ctx.opened.get(b) ?? 0) - (ctx.opened.get(a) ?? 0) || pos.get(a) - pos.get(b))
    .slice(0, room)
    .sort(byEntry);
  // A device that has opened few guilds yet would leave the strip half empty:
  // the slots still free go to the rest in the switcher's own order, which is
  // the user's order and so does not move on its own either.
  const picked = new Set([...pins, ...mention, ...unread, ...recent]);
  const rest = ids.filter((id) => !picked.has(id));
  let shown = [...pins, ...mention, ...unread, ...recent, ...rest].slice(0, Math.max(0, n));
  if (current && live(current) && !shown.includes(current) && n > 0) {
    shown = [...shown.slice(0, n - 1), current];
  }
  const set = new Set(shown);
  return { shown, hidden: ids.filter((id) => !set.has(id)) };
}
