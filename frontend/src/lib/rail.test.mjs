// Zero-dependency test for the guild-rail layout model (`npm test` runs it).
import {
  reconcile,
  combineGuilds,
  moveGuild,
  moveFolder,
  dissolveFolder,
  toggleFolder,
  renameFolder,
  setFolderColor,
  guildIdsInLayout,
  guildInitials,
  seedOrder,
  sharedOrder,
} from "./rail.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};
const eq = (a, b, msg) => assert(JSON.stringify(a) === JSON.stringify(b), `${msg}\n  got ${JSON.stringify(a)}\n  exp ${JSON.stringify(b)}`);

const G = (id) => ({ t: "g", id });
const F = (id, ids, extra = {}) => ({ t: "f", id, name: "", color: "#5865f2", open: false, ids, ...extra });

// reconcile: append new guilds, drop missing, keep order
eq(
  guildIdsInLayout(reconcile([G("a"), G("b")], ["a", "b", "c"])),
  ["a", "b", "c"],
  "reconcile appends new guilds at the end",
);
eq(
  reconcile([G("a"), G("gone"), G("b")], ["a", "b"]),
  [G("a"), G("b")],
  "reconcile drops guilds that no longer exist",
);
// reconcile: dissolve a folder that drops below 2 members (survivor kept in place)
eq(
  reconcile([F("f1", ["a", "gone"]), G("b")], ["a", "b"]),
  [G("a"), G("b")],
  "reconcile dissolves a folder left with one guild",
);
// reconcile: de-duplicate a guild that appears twice
eq(
  guildIdsInLayout(reconcile([G("a"), F("f1", ["a", "b"])], ["a", "b"])),
  ["a", "b"],
  "reconcile de-duplicates ids (keeps first)",
);
// reconcile preserves a healthy folder
{
  const out = reconcile([F("f1", ["a", "b"]), G("c")], ["a", "b", "c"]);
  assert(out[0].t === "f" && out[0].ids.join() === "a,b", "reconcile keeps a 2+ folder intact");
}

// combineGuilds: drop b onto a → folder [a,b] at a's slot
{
  const out = combineGuilds([G("a"), G("b"), G("c")], "b", "a");
  assert(out.length === 2, "combine removes the dragged guild from top level");
  assert(out[0].t === "f" && out[0].ids.join() === "a,b", "combine makes [target, dragged]");
  assert(out[1].id === "c", "combine leaves other guilds untouched");
}

// moveGuild: into a folder, then back out to top level
{
  let out = moveGuild([F("f1", ["a", "b"]), G("c")], "c", { kind: "folder", folderId: "f1" });
  eq(out[0].ids, ["a", "b", "c"], "moveGuild appends into a folder");
  out = moveGuild(out, "a", { kind: "top", index: 0 });
  assert(out[0].t === "g" && out[0].id === "a", "moveGuild pulls a guild out to top level");
  assert(out.find((e) => e.t === "f").ids.join() === "b,c", "folder keeps the remaining members");
}

// moveGuild: reorder at top level
eq(
  guildIdsInLayout(moveGuild([G("a"), G("b"), G("c")], "c", { kind: "top", index: 0 })),
  ["c", "a", "b"],
  "moveGuild reorders to the front",
);

// moveGuild: downward reorders — the drop index is computed against the rail
// as rendered (dragged guild still in place), so "drop a after c" is index 3
// in [a,b,c,d] and must land a at slot 2 of the result, not at the end.
eq(
  guildIdsInLayout(moveGuild([G("a"), G("b"), G("c"), G("d")], "a", { kind: "top", index: 3 })),
  ["b", "c", "a", "d"],
  "moveGuild downward lands after the target, not one slot too far",
);
eq(
  guildIdsInLayout(moveGuild([G("a"), G("b"), G("c")], "a", { kind: "top", index: 3 })),
  ["b", "c", "a"],
  "moveGuild to the very end still works",
);
// Same rule inside a folder: pre-removal member indices.
eq(
  moveGuild([F("f1", ["a", "b", "c"])], "a", { kind: "folder", folderId: "f1", index: 3 })[0].ids,
  ["b", "c", "a"],
  "moveGuild downward within a folder compensates for its own gap",
);
eq(
  moveGuild([F("f1", ["a", "b", "c"])], "c", { kind: "folder", folderId: "f1", index: 0 })[0].ids,
  ["c", "a", "b"],
  "moveGuild upward within a folder is unchanged",
);

// moveFolder: downward move with pre-removal index.
eq(
  moveFolder([F("f1", ["a", "b"]), G("c"), G("d")], "f1", 2).map((e) => e.id ?? e.t),
  ["c", "f1", "d"],
  "moveFolder downward lands before the slot it was aimed at, not past it",
);

// moveFolder + folder edits
{
  const base = [G("a"), F("f1", ["b", "c"])];
  eq(moveFolder(base, "f1", 0)[0].t, "f", "moveFolder moves the folder to the front");
  eq(dissolveFolder(base, "f1"), [G("a"), G("b"), G("c")], "dissolveFolder inlines its guilds in place");
  assert(toggleFolder(base, "f1")[1].open === true, "toggleFolder flips open");
  assert(renameFolder(base, "f1", "Study")[1].name === "Study", "renameFolder sets the name");
  assert(setFolderColor(base, "f1", "#3ba55d")[1].color === "#3ba55d", "setFolderColor sets the colour");
}

if (failures) {
  console.error(`\n${failures} rail test(s) failed`);
  process.exit(1);
}
console.log("rail.test.mjs: all passed");

// ---- one initials rule ----------------------------------------------------
// The rail and the settings panel drew the same guild differently ("RM" vs
// "R"), three inches apart, which read as two different places.
{
  const cases = [
    ["Riverside Makers", "RM"],
    ["  Dar al-Hikma  ", "DA"],
    ["general", "G"],
    ["", ""],
    ["a b c d", "AB"],
    // A codepoint, not a UTF-16 unit: an emoji or an astral letter is one
    // character to a person.
    ["🌙 Night", "🌙N"],
    ["دار الحكمة", "دا"],
  ];
  for (const [input, want] of cases) {
    const got = guildInitials(input);
    if (got !== want) {
      console.error(`  FAIL guildInitials(${JSON.stringify(input)}) = ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
      process.exitCode = 1;
    }
  }
  console.log("rail.test.mjs: guildInitials ok");
}

// ---- the strip: a shelf of every guild ----
{
  const { sharedOrder, shelfView, edgeNews, revealLeft, snapLeft } = await import("./rail.js");
  const fail = (msg) => {
    console.error("  FAIL", msg);
    process.exitCode = 1;
  };
  const same = (a, b, msg) => JSON.stringify(a) !== JSON.stringify(b) && fail(`${msg}: got ${JSON.stringify(a)}, want ${JSON.stringify(b)}`);

  const ids = ["a", "b", "c", "d", "e", "f", "g", "h"];
  // One order everywhere: pins first in pin order, then the rail order.
  same(sharedOrder(ids, ["h", "gone", "b"]), ["h", "b", "a", "c", "d", "e", "f", "g"], "pins, then the rail order");

  // 20 tiles of 28px on a 32px pitch, from x=4, in a 200px shelf with 16px fades.
  const n = 20;
  const geo = (left) => ({ lead: 4, pitch: 32, size: 28, left, width: 200, max: 4 + 20 * 32 + 4 - 4 - 200, fade: 16 });
  // At the start there is no left fade: tiles 0..4 have their centres inside
  // 0..184 (tile 5's centre is at 178, inside; tile 6's at 210, outside).
  same(shelfView(n, geo(0)), { first: 0, last: 5 }, "the start of the shelf");
  // Scrolled: both fades apply.
  same(shelfView(n, geo(96)), { first: 3, last: 8 }, "the middle, clear of both fades");
  // At the end there is no right fade.
  const end = geo(geo(0).max);
  same(shelfView(n, end).last, n - 1, "the last tile is in view at the end");

  // What the hidden ends hold back: mentions as one number, else a dot.
  const order = Array.from({ length: n }, (_, i) => `g${i}`);
  const M = { g1: { count: 2 }, g2: { dot: true }, g12: { dot: true }, g15: { dot: true, muted: true }, g18: { count: 1, muted: true } };
  const marks = (id) => M[id] || { count: 0, dot: false };
  same(edgeNews(order, { first: 3, last: 8 }, marks), { left: { count: 2, dot: false }, right: { count: 1, dot: false } }, "mentions win over dots, and a muted guild's mention still counts");
  same(edgeNews(order, { first: 0, last: 5 }, (id) => (id === "g12" ? { dot: true } : null)), { left: { count: 0, dot: false }, right: { count: 0, dot: true } }, "plain unread past the right edge is a dot");
  same(edgeNews(order, { first: 0, last: 5 }, (id) => (id === "g15" ? { dot: true, muted: true } : null)).right.dot, false, "a muted guild's plain unread is not news");
  same(edgeNews(order, { first: 0, last: n - 1 }, marks), { left: { count: 0, dot: false }, right: { count: 0, dot: false } }, "nothing hidden, nothing to say");

  // Revealing a tile: no scroll when it is already clear of the fades, else
  // the least scroll that shows it whole with a fade's room beside it.
  same(revealLeft(2, geo(0)), 0, "already in view: no movement");
  same(revealLeft(10, geo(0)), 4 + 10 * 32 + 28 + 16 - 200, "to the right: just enough to clear the fade");
  same(revealLeft(1, geo(200)), 4 + 32 - 16, "to the left: just enough to clear the fade");
  same(revealLeft(19, geo(0)), geo(0).max, "never past the end");

  // Snapping a free scroll position to the nearest stop, clamped to the ends.
  const stops = [-12, 20, 52, 86, 116];
  same(snapLeft(0, stops, 100), 0, "a stop before the start is the start");
  same(snapLeft(40, stops, 100), 52, "the nearest stop");
  same(snapLeft(35, stops, 100), 20, "just under half way rounds back");
  same(snapLeft(110, stops, 100), 100, "a stop past the end is the end");
  same(snapLeft(99.8, stops, 100), 100, "the end stays the end");
  // A wheel notch is one tile; a trackpad's pixel stream is not a notch.
  const { wheelNotch, nextStop, glideStep } = await import("./rail.js");
  same([wheelNotch(100, 0), wheelNotch(120, 0), wheelNotch(53, 0), wheelNotch(3, 1)], [true, true, true, true], "wheel notches");
  same([wheelNotch(4, 0), wheelNotch(12.5, 0), wheelNotch(60.3, 0)], [false, false, false], "trackpad deltas");
  same(nextStop(52, stops, 1, 100), 86, "one tile on");
  same(nextStop(52.4, stops, -1, 100), 20, "one tile back");
  same(nextStop(100, stops, 1, 100), 100, "nothing past the end");
  same(nextStop(0, stops, -1, 100), 0, "nothing before the start");
  same(nextStop(0, [-30, 2, 34, 66], 1, 100, 16), 34, "a stop 2px away is not a tile on");
  // The glide: the first frame already moves, a quarter of the way.
  same(glideStep(0, 32), 8, "a quarter of the way in the first frame");
  same(glideStep(31.5, 32), 32, "arrives exactly");
  let x = 0, frames = 0;
  while (x !== 96 && frames < 100) (x = glideStep(x, 96)), frames++;
  if (frames > 20) fail(`the glide takes ${frames} frames to arrive`);
  if (!process.exitCode) console.log("rail.test.mjs: shelf ok");
}

// ---- guild marks ----
{
  const { guildMarks } = await import("./rail.js");
  const fail = (msg) => {
    console.error("  FAIL", msg);
    process.exitCode = 1;
  };
  const gs = [
    { id: "b", name: "Choir Altos" },
    { id: "a", name: "Chess After Dark" },
    { id: "c", name: "Night Owls" },
    { id: "d", name: "chess club" },
    { id: "dm1", name: "Someone", kind: "dm" },
  ];
  const m = guildMarks(gs);
  if (m.get("a").letter !== "C" || m.get("a").glyph) fail("the first C (by id) keeps its letter");
  if (!m.get("b").glyph || !m.get("d").glyph) fail("later Cs get a glyph");
  if (m.get("b").glyph === m.get("d").glyph) fail("two glyphs under one letter differ");
  if (m.get("c").letter !== "N" || m.get("c").glyph) fail("a unique letter is just the letter");
  if (m.has("dm1")) fail("direct messages have no guild mark");
  // The same answer whatever order the list is in.
  const again = guildMarks([...gs].reverse());
  for (const id of ["a", "b", "c", "d"]) if (JSON.stringify(again.get(id)) !== JSON.stringify(m.get(id))) fail(`stable for ${id}`);
  if (!process.exitCode) console.log("rail.test.mjs: guild marks ok");
}

// A device with no stored layout seeds the rail from the guild list, which the
// core returns in a different order on every call (it is read off a map). The
// seeded layout — and so the strip and the switcher — must be the same whatever
// order that list arrived in, including the first load.
{
  const fail = (m) => {
    console.error("FAIL " + m);
    process.exitCode = 1;
  };
  const gs = [
    { id: "g1", name: "Lantern Makers" },
    { id: "g2", name: "Riverside Choir" },
    { id: "g3", name: "Pixel Garden" },
    { id: "g4", name: "pixel garden" },
  ];
  const perms = [[0, 1, 2, 3], [3, 2, 1, 0], [2, 0, 3, 1], [1, 3, 0, 2]];
  const layouts = perms.map((p) => JSON.stringify(reconcile([], seedOrder(p.map((i) => gs[i])))));
  if (new Set(layouts).size !== 1) fail("a fresh layout depends on the order the guild list arrived in");
  const ids = guildIdsInLayout(JSON.parse(layouts[0]));
  if (JSON.stringify(sharedOrder(ids, [])) !== JSON.stringify(ids)) fail("the strip is laid out in the rail order on first load");
  // A guild that arrives later is appended; the placed ones do not move.
  const later = reconcile(JSON.parse(layouts[0]), seedOrder([...gs, { id: "g5", name: "Aardvarks" }]));
  if (guildIdsInLayout(later).join() !== [...ids, "g5"].join()) fail("a new guild is appended without moving the placed ones");
  if (!process.exitCode) console.log("rail.test.mjs: first-load order ok");
}
