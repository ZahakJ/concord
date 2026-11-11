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

// ---- the shortcut strip ----
{
  const { stripFit, stripTiles, sharedOrder } = await import("./rail.js");
  const fail = (msg) => {
    console.error("  FAIL", msg);
    process.exitCode = 1;
  };
  const same = (a, b, msg) => JSON.stringify(a) !== JSON.stringify(b) && fail(`${msg}: got ${JSON.stringify(a)}, want ${JSON.stringify(b)}`);

  same([stripFit(280), stripFit(240), stripFit(360), stripFit(264)], [6, 5, 8, 5], "tiles that fit 280/240/360/264");
  // The measured width is a pixel under the nominal one (the sidebar's border).
  same([stripFit(279), stripFit(239), stripFit(359), stripFit(263)], [6, 5, 8, 5], "the same counts a border's pixel narrower");

  const ids = ["a", "b", "c", "d", "e", "f", "g", "h"];
  const ctx = (o = {}) => ({ pins: [], mentions: new Set(), unread: new Set(), opened: new Map(), ...o });

  // One order everywhere: pins first in pin order, then the rail order.
  same(sharedOrder(ids, ["h", "gone", "b"]), ["h", "b", "a", "c", "d", "e", "f", "g"], "pins, then the rail order");

  // Which guilds earn a tile: pins, mentions, unread, recent; laid out in the
  // shared order, not by class.
  {
    const c = ctx({ pins: ["h", "b"], mentions: new Set(["e"]), unread: new Set(["c"]), opened: new Map([["a", 5], ["d", 9]]) });
    same(stripTiles(ids, c, [], "", 6).shown, ["h", "b", "a", "c", "d", "e"], "the picked six, in the shared order");
    same(stripTiles(ids, c, [], "", 4).shown, ["h", "b", "c", "e"], "pins and the most pressing when room is short");
  }

  // Reading a guild never moves it or drops it: the tile stays where it was.
  {
    const before = ctx({ mentions: new Set(["f"]), unread: new Set(["c"]), opened: new Map([["a", 1]]) });
    const first = stripTiles(ids, before, [], "", 3).shown;
    same(first, ["a", "c", "f"], "mention, unread, recent");
    const read = ctx({ opened: new Map([["a", 1], ["f", 2], ["c", 3]]) });
    same(stripTiles(ids, read, first, "", 3).shown, first, "reading them changes nothing");
  }

  // A shown tile leaves only when the row overflows, and the least pressing goes.
  {
    const c0 = ctx({ opened: new Map([["a", 1], ["b", 2]]) });
    const first = stripTiles(ids, c0, [], "", 2).shown;
    same(first, ["a", "b"], "two recent");
    const c1 = ctx({ mentions: new Set(["g"]), opened: new Map([["a", 1], ["b", 2]]) });
    same(stripTiles(ids, c1, first, "", 2).shown, ["b", "g"], "a mention takes the slot of the oldest-opened");
  }

  // Free slots go to the rest in the shared order.
  {
    const c = ctx({ opened: new Map([["c", 3]]) });
    same(stripTiles(ids, c, [], "", 3).shown, ["a", "b", "c"], "free slots fill in order");
  }

  // The current guild is always present.
  {
    const c = ctx({ pins: ["a", "b", "c"] });
    const r = stripTiles(ids, c, [], "g", 3);
    same(r.shown, ["a", "b", "g"], "current guild takes the last slot");
    same(r.hidden, ["c", "d", "e", "f", "h"], "the rest are hidden, in the shared order");
  }

  // Ids that no longer exist are ignored, never shown.
  {
    const c = ctx({ pins: ["gone", "a"] });
    same(stripTiles(ids, c, ["gone"], "", 2).shown, ["a", "b"], "a guild you left is skipped");
  }
  if (!process.exitCode) console.log("rail.test.mjs: strip ok");
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
