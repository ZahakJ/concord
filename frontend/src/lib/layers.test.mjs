// The layer scale, and the rule that a portalled surface draws above whatever
// opened it (`npm test` runs it).
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { Z, zAboveChain, appCovered } from "./layers.js";
import { menuKeyTarget, nextStop } from "./menukeys.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

// ---- zAboveChain ----
// A menu on the bare page still clears the floating call window.
assert(zAboveChain([]) === Z.layer, "no stacking ancestors: the layer floor");
assert(zAboveChain([]) > Z.dock, "the floor is above the dock");
// The device picker in Settings: its trigger sits in a dialog overlay at 100+.
// It used to open at a fixed 80, underneath the dialog.
assert(zAboveChain(["3", "104"]) === 105, "a menu opened in a dialog at 104 draws at 105");
assert(zAboveChain([2, "auto", 250]) === 251, "a menu opened from the profile card draws above it");
assert(zAboveChain(["400", "401"]) === 402, "a menu opened in a studio draws above it");
assert(zAboveChain(["5"]) === Z.layer, "a sticky toolbar's z does not pull a menu under the floor");
assert(zAboveChain([NaN, null, undefined, "auto"]) === Z.layer, "junk entries are ignored");

// ---- the order itself ----
const order = ["page", "sticky", "raised", "dock", "layer", "veil", "studio", "fx", "toast", "tooltip"];
for (let i = 1; i < order.length; i++) assert(Z[order[i]] > Z[order[i - 1]], `${order[i]} is above ${order[i - 1]}`);

// app.css carries the same numbers as --z-* tokens for stylesheets; the two
// must not drift.
const css = fs.readFileSync(path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../app.css"), "utf8");
for (const [k, v] of Object.entries(Z)) {
  if (k === "page") continue;
  const m = css.match(new RegExp(`--z-${k}:\\s*(\\d+);`));
  assert(m && Number(m[1]) === v, `app.css --z-${k} matches layers.js (${v}), got ${m?.[1]}`);
}

// ---- menuKeyTarget ----
const row = (label) => ({ textContent: label, getAttribute: () => null });
const items = ["Poll", "Doodle", "Sound", "Delete", "Seal the send time"].map(row);
const k = (key, mods = {}) => ({ key, ...mods });
assert(menuKeyTarget(items, null, k("ArrowDown")) === items[0], "↓ with nothing focused: first row");
assert(menuKeyTarget(items, null, k("ArrowUp")) === items[4], "↑ with nothing focused: last row");
assert(menuKeyTarget(items, items[4], k("ArrowDown")) === items[0], "↓ wraps");
assert(menuKeyTarget(items, items[0], k("ArrowUp")) === items[4], "↑ wraps");
assert(menuKeyTarget(items, items[2], k("Home")) === items[0], "Home");
assert(menuKeyTarget(items, items[2], k("End")) === items[4], "End");
assert(menuKeyTarget(items, items[0], k("d")) === items[1], "typeahead d -> Doodle");
assert(menuKeyTarget(items, items[1], k("d")) === items[3], "typeahead d again -> Delete");
assert(menuKeyTarget(items, items[3], k("D")) === items[1], "typeahead wraps, case-insensitive");
assert(menuKeyTarget(items, items[0], k("s")) === items[2], "typeahead s -> Sound");
assert(menuKeyTarget(items, items[0], k("z")) === null, "no match: no move");
assert(menuKeyTarget(items, items[0], k(" ")) === null, "Space activates, it does not search");
assert(menuKeyTarget(items, items[0], k("d", { ctrlKey: true })) === null, "chords are not typeahead");
assert(menuKeyTarget([row("✓ System default"), row("Fake mic")], null, k("s"))?.textContent === "✓ System default", "leading glyphs are skipped");

// ---- nextStop ----
const L = ["a", "b", "c"];
assert(nextStop(L, "b", 1, false) === "c", "Tab: next");
assert(nextStop(L, "b", -1, false) === "a", "Shift+Tab: previous");
assert(nextStop(L, "c", 1, false) === null, "Tab off the end of the page: nothing (the browser chrome)");
assert(nextStop(L, "c", 1, true) === "a", "Tab off the end of a dialog wraps, like its trap");
assert(nextStop(L, "a", -1, true) === "c", "Shift+Tab wraps in a dialog");

// ---- appCovered: navigation keys stand down under a covering layer ----
assert(appCovered({ kind: "settings" }, ""), "a dialog covers the app");
assert(appCovered(null, "popover"), "a profile card covers the app");
assert(appCovered(null, "menu"), "an open menu covers the app");
assert(!appCovered(null, ""), "nothing open: the app answers");
assert(!appCovered(null, "reply"), "writing a reply is not a cover");
assert(!appCovered(null, "call-stage"), "the call stage is not a cover");

if (failures) {
  console.error(`${failures} layers/menukeys test(s) failed`);
  process.exit(1);
}
console.log("layers/menukeys: all tests passed");
