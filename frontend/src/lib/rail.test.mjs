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
