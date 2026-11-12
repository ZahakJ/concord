// guildmenu.js — the menus a guild and a guild folder open, wherever they are
// opened from: a strip tile, a switcher row, the classic rail. Written once so
// the three can never offer different verbs for the same guild.
//
// It also owns the two device-local lists the strip and the switcher share:
// which guilds are pinned to the strip (pref `stripPins`, the user's order) and
// when each guild was last opened on this device (localStorage, for the strip's
// "most recently opened" class). Neither is part of any guild's state.
import { S, openContextMenu, guildMenuItems, commitRail, setPref, selectGuild, openGuildHub } from "./state.svelte.js";
import {
  combineGuilds,
  moveGuild,
  moveFolder,
  toggleFolder,
  renameFolder,
  setFolderColor,
  dissolveFolder,
  DEFAULT_FOLDER_COLOR,
} from "./rail.js";

// ---- pins ----

export function stripPins() {
  const p = S.prefs.stripPins;
  return Array.isArray(p) ? p : [];
}
export function isPinned(id) {
  return stripPins().includes(id);
}
export function togglePin(id) {
  const pins = stripPins();
  setPref("stripPins", pins.includes(id) ? pins.filter((x) => x !== id) : [...pins, id]);
}
// pinAt puts a guild among the pins at an index (a drag on the strip): an
// unpinned tile dropped there becomes a pin, a pinned one moves.
export function pinAt(id, index) {
  const pins = stripPins().filter((x) => x !== id);
  const at = Math.max(0, Math.min(pins.length, index));
  pins.splice(at, 0, id);
  setPref("stripPins", pins);
}

// ---- last opened ----

const OPENED_KEY = "concord.guildOpened";
let opened = null;
function loadOpened() {
  if (opened) return opened;
  try {
    const raw = JSON.parse(localStorage.getItem(OPENED_KEY) || "{}");
    opened = new Map(Object.entries(raw && typeof raw === "object" ? raw : {}).filter(([, t]) => Number.isFinite(t)));
  } catch {
    opened = new Map();
  }
  return opened;
}
export function openedTimes() {
  return loadOpened();
}
export function noteOpened(id, at = Date.now()) {
  if (!id) return;
  const m = loadOpened();
  m.set(id, at);
  try {
    localStorage.setItem(OPENED_KEY, JSON.stringify(Object.fromEntries(m)));
  } catch {
    /* storage full or blocked: the strip just forgets recency */
  }
}

// ---- menus ----

// openContextMenu positions at the event, so a menu opened FROM a menu item
// needs the original point carried forward.
const atPoint = (e) => ({
  clientX: e.clientX,
  clientY: e.clientY,
  currentTarget: e.currentTarget,
  preventDefault() {},
  stopPropagation() {},
});

const topIndexOfFolder = (fid) => S.rail.findIndex((e) => e.t === "f" && e.id === fid);

// Every drag on the rail and in the switcher has a menu equivalent: HTML5
// drag-and-drop does not exist under a finger, and a keyboard has no drag.
function arrangeItems(g, folder) {
  const items = [];
  const moveTop = (index) => commitRail(moveGuild(S.rail, g.id, { kind: "top", index }));
  if (folder) {
    const i = folder.ids.indexOf(g.id);
    const inFolder = (index) => commitRail(moveGuild(S.rail, g.id, { kind: "folder", folderId: folder.id, index }));
    if (i > 0) items.push({ label: "Move up", icon: "chevron", onClick: () => inFolder(i - 1) });
    // moveGuild reads its index against the layout BEFORE the guild is lifted
    // out, so one step down is i+2, not i+1.
    if (i > -1 && i < folder.ids.length - 1) items.push({ label: "Move down", icon: "chevron", onClick: () => inFolder(i + 2) });
    items.push({ label: "Take out of folder", icon: "door", onClick: () => moveTop(topIndexOfFolder(folder.id) + 1) });
    return items;
  }
  const i = S.rail.findIndex((e) => e.t === "g" && e.id === g.id);
  if (i > 0) items.push({ label: "Move up", icon: "chevron", onClick: () => moveTop(i - 1) });
  if (i > -1 && i < S.rail.length - 1) items.push({ label: "Move down", icon: "chevron", onClick: () => moveTop(i + 2) });
  const folders = S.rail.filter((e) => e.t === "f");
  if (folders.length) {
    items.push({ label: "Move to folder", header: true });
    for (const f of folders)
      items.push({
        label: f.name || "Folder",
        icon: "folder",
        onClick: () => commitRail(moveGuild(S.rail, g.id, { kind: "folder", folderId: f.id })),
      });
  }
  const others = S.rail
    .filter((e) => e.t === "g" && e.id !== g.id)
    .map((e) => S.guilds.find((x) => x.id === e.id))
    .filter(Boolean);
  if (others.length) {
    items.push({ label: "New folder with…", header: true });
    for (const o of others)
      items.push({
        label: o.name,
        icon: "diamond",
        onClick: () => commitRail(combineGuilds(S.rail, g.id, o.id, DEFAULT_FOLDER_COLOR)),
      });
  }
  return items;
}

// guildMenu: the guild's own menu (lib/state's guildMenuItems), with the pin
// toggle first and the rearranging behind one row — "New folder with…" lists
// every other guild, and a menu you scroll past to reach Leave is worse than
// one more press.
export function guildMenu(e, g, folder = null) {
  if (!g) return;
  const pt = atPoint(e);
  const arrange = arrangeItems(g, folder);
  const pinned = isPinned(g.id);
  openContextMenu(
    e,
    [
      { label: pinned ? "Unpin from shortcuts" : "Pin to shortcuts", icon: pinned ? "pinFilled" : "pin", onClick: () => togglePin(g.id) },
      {
        label: "Guild settings",
        icon: "gear",
        onClick: async () => {
          if (S.activeGuildId !== g.id) await selectGuild(g.id);
          openGuildHub();
        },
      },
      { sep: true },
      ...guildMenuItems(g),
      arrange.length && { sep: true },
      arrange.length && {
        label: "Rearrange…",
        icon: "folder",
        onClick: () => openContextMenu(pt, arrange, { title: `Rearrange ${g.name}` }),
      },
    ].filter(Boolean),
    { title: g.name },
  );
}

// Folder colours mark the folder's own dot and nothing else (a guild's colour
// and a folder's colour are decoration; the accent is yours).
const SWATCHES = [
  ["Accent", "var(--accent)"],
  ["Teal", "#2dd4bf"],
  ["Violet", "#a78bfa"],
  ["Amber", "#f5a524"],
  ["Rose", "#f472b6"],
  ["Sage", "#7bb87b"],
  ["Slate", "#7c8798"],
];

// folderMenu: rename (handed back to the caller, which owns the field), mark
// read, colour, reorder, ungroup.
export function folderMenu(e, folder, { onRename } = {}) {
  const idx = topIndexOfFolder(folder.id);
  const guilds = folder.ids.map((id) => S.guilds.find((x) => x.id === id)).filter(Boolean);
  openContextMenu(
    e,
    [
      {
        label: folder.open ? "Collapse folder" : "Expand folder",
        icon: "folder",
        onClick: () => commitRail(toggleFolder(S.rail, folder.id)),
      },
      onRename && { label: "Rename folder", icon: "edit", onClick: () => onRename(folder) },
      {
        label: "Mark folder read",
        icon: "check",
        onClick: () => guilds.forEach((g) => guildMenuItems(g).find((it) => it.label === "Mark as read")?.onClick()),
      },
      idx > 0 && { label: "Move up", icon: "chevron", onClick: () => commitRail(moveFolder(S.rail, folder.id, idx - 1)) },
      idx > -1 && idx < S.rail.length - 1 && { label: "Move down", icon: "chevron", onClick: () => commitRail(moveFolder(S.rail, folder.id, idx + 2)) },
      { sep: true },
      { label: "Colour", header: true },
      ...SWATCHES.map(([name, color]) => ({
        label: name,
        swatch: color,
        active: (folder.color || "var(--accent)") === color,
        onClick: () => commitRail(setFolderColor(S.rail, folder.id, color)),
      })),
      { sep: true },
      { label: "Ungroup folder", icon: "trash", danger: true, onClick: () => commitRail(dissolveFolder(S.rail, folder.id)) },
    ].filter(Boolean),
    { title: folder.name || "Folder" },
  );
}

export function renameFolderTo(folder, name) {
  commitRail(renameFolder(S.rail, folder.id, String(name || "").slice(0, 40)));
}
