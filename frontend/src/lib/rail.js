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

// seedOrder is the order guilds the layout has never placed are appended in:
// by name, then id. The guild list arrives from the core in no particular
// order — it is read off a map, and differs from one call to the next — so
// on a device with no stored layout yet (a fresh install, a new browser) the
// first reconcile used to seed the rail, and with it the strip and the
// switcher, in whatever order that one call happened to return, and save it.
// One load in several came up with the open guild at the end and kept it.
// Placed guilds keep their place; this only decides where new ones land.
export function seedOrder(guilds) {
  return [...guilds]
    .sort((a, b) => String(a.name || "").localeCompare(String(b.name || ""), undefined, { sensitivity: "base" }) || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0))
    .map((g) => g.id);
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

// ————————————————— the strip: a shelf of every guild —————————————————
//
// The sidebar's strip is a SHELF: Messages fixed at the leading end, Add fixed
// at the trailing end, and between them every guild, in one row that scrolls
// sideways. It used to be a fitted shortcut row — as many tiles as the width
// allowed, picked by a rule, and an overflow tile for the rest — which meant
// the guild you wanted was often behind a click, and which ones were on the
// row depended on what had happened lately. All of them, in one order you
// set, is something a hand can learn. The switcher stays the inventory
// (search, folders, rearranging).

// sharedOrder is the ONE order every view of the guild inventory uses — the
// strip, the switcher and the rail: the user's pins first, in the order they
// pinned them, then everything else in the user's own rail order. Three views
// of one inventory in three orders meant nobody could learn where anything was.
export function sharedOrder(ids, pins) {
  const live = new Set(ids);
  const p = (pins || []).filter((id) => live.has(id));
  const ps = new Set(p);
  return [...p, ...ids.filter((id) => !ps.has(id))];
}

// shelfView answers which tiles of the shelf you can actually see. The row
// fades out at an edge where more tiles exist, so a tile under the fade does
// not count as seen: a tile is in view when its centre is inside the shelf
// less the faded band, and the band only exists on a side that can still
// scroll. Geometry is the shelf's own (every tile the same pitch), so this is
// arithmetic on numbers the caller already has, not a layout read per tile.
//
//   n      tiles on the shelf
//   g      { lead: x of the first tile's left edge, pitch, size, left: scrollLeft,
//            width: the shelf's clientWidth, max: the furthest scrollLeft, fade }
//
// Returns { first, last } — the first and last index in view (last < first
// when none is).
export function shelfView(n, g) {
  const fadeL = g.left > 0.5 ? g.fade : 0;
  const fadeR = g.left < g.max - 0.5 ? g.fade : 0;
  const lo = g.left + fadeL;
  const hi = g.left + g.width - fadeR;
  const centre = (i) => g.lead + i * g.pitch + g.size / 2;
  let first = 0;
  while (first < n && centre(first) < lo) first++;
  let last = n - 1;
  while (last >= 0 && centre(last) > hi) last--;
  return { first, last };
}

// edgeNews is what the hidden ends of the shelf are holding back, for the
// small mark at each faded edge: mentions aimed at you as one number, else a
// dot when anything there is unread. Never how many guilds are off-screen —
// that is not news. A muted guild's mentions still count (it shows them
// outlined on its own tile), its plain unread does not.
//
//   order   guild ids in shelf order
//   view    { first, last } from shelfView
//   marks   id → { count, dot, muted }
export function edgeNews(order, view, marks) {
  const side = (ids) => {
    let count = 0;
    let dot = false;
    for (const id of ids) {
      const m = marks(id);
      if (!m) continue;
      count += m.count || 0;
      dot = dot || (!m.muted && !!m.dot);
    }
    return { count, dot: !count && dot };
  };
  return { left: side(order.slice(0, view.first)), right: side(order.slice(Math.max(view.first, view.last + 1))) };
}

// revealLeft is where to scroll so tile i sits in view clear of the fades:
// unchanged when it already is, otherwise the nearest position that shows it
// whole with the fade's width of room beside it. g as for shelfView.
export function revealLeft(i, g) {
  const x = g.lead + i * g.pitch;
  const lo = g.left + (g.left > 0.5 ? g.fade : 0);
  const hi = g.left + g.width - (g.left < g.max - 0.5 ? g.fade : 0);
  let to = g.left;
  if (x < lo) to = x - g.fade;
  else if (x + g.size > hi) to = x + g.size + g.fade - g.width;
  return Math.max(0, Math.min(g.max, Math.round(to)));
}

// snapLeft is the resting place for a scroll position: the nearest snap stop
// (where the shelf's CSS snap would put each tile), clamped to the ends. A
// vertical wheel is mapped to the row by the strip itself, which turns snap
// off while it drives the scroll and glides here when the wheel goes quiet.
// The stops are read from the tiles as drawn, because the browser snaps to
// the transformed box: a tile eased aside by 2px snaps 2px further along, and
// a landing computed from the layout alone was corrected by a visible 2px
// jump the moment snap came back on.
export function snapLeft(left, stops, max) {
  if (left >= max - 0.5) return max;
  let best = 0;
  let d = Infinity;
  for (const s of stops) {
    const c = Math.max(0, Math.min(max, s));
    if (Math.abs(c - left) < d) {
      d = Math.abs(c - left);
      best = c;
    }
  }
  return Math.round(best);
}

// ————————————————— a guild's mark without an icon —————————————————
//
// Two-letter initials on a flat colour square are the most generic tile there
// is, and at thirty guilds they were a wall of look-alikes with two "CA"s. A
// guild without an icon now shows ONE letter, and when an earlier guild (by
// id, so the answer never depends on the order on screen) already has that
// letter, a plain shape instead, chosen from its id.
// Neutral shapes only (Icon.svelte's sh*): a glyph that already means
// something in the sidebar (megaphone = announcements) cannot also be a name.
const MARK_GLYPHS = ["shRing", "shTriangle", "shSquare", "shHex", "shStar", "shPentagon", "shHalf", "shOctagon"];
const hashId = (id) => {
  let h = 0;
  for (const c of String(id)) h = (h * 31 + c.codePointAt(0)) >>> 0;
  return h;
};
export function guildMarks(guilds) {
  const out = new Map();
  const byLetter = new Map();
  for (const g of [...(guilds || [])].filter((x) => x && x.kind !== "dm").sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0))) {
    const letter = ([...String(g.name || "").trim()][0] || "?").toUpperCase();
    const taken = byLetter.get(letter);
    if (!taken) {
      byLetter.set(letter, new Set());
      out.set(g.id, { letter, glyph: "" });
      continue;
    }
    let i = hashId(g.id) % MARK_GLYPHS.length;
    while (taken.has(MARK_GLYPHS[i]) && taken.size < MARK_GLYPHS.length) i = (i + 1) % MARK_GLYPHS.length;
    taken.add(MARK_GLYPHS[i]);
    out.set(g.id, { letter, glyph: MARK_GLYPHS[i] });
  }
  return out;
}
