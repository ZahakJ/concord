// The settings table, held in place (`npm test` runs it).
//
// Three things can silently break this table, and all three have a shape a test
// can see. An entry can name a modal kind App.svelte cannot load, in which case
// the rail draws a door that opens onto an error. It can name an icon Icon.svelte
// does not have, in which case the row draws with a hole where its glyph should
// be — a hole exactly like the unlabelled two-tone block this table replaced.
// And two entries can claim the same kind, in which case two rail rows light up
// at once and neither is wrong.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { SETTINGS_GROUPS, SETTINGS_ITEMS, settingsItem, inSettings } from "./settingsnav.js";

const SRC = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
let failures = 0;
const fail = (msg) => {
  console.error("  FAIL " + msg);
  failures++;
};
const check = (name, ok) => {
  if (!ok) fail(name);
};

// ---- shape ----------------------------------------------------------------
check("there are groups", SETTINGS_GROUPS.length > 0);
for (const g of SETTINGS_GROUPS) {
  if (!g.label) fail("a group with no label");
  if (!g.items?.length) fail(`group "${g.label}" has no items`);
  for (const it of g.items || []) {
    if (!it.kind) fail(`an item in "${g.label}" has no kind`);
    if (!it.title) fail(`${it.kind} has no title`);
    if (!it.icon) fail(`${it.kind} has no icon`);
    if (!it.sub) fail(`${it.kind} has no sub`);
    // The sub is the phone list's second line and the rail's tooltip. A sub
    // that merely repeats the title tells nobody anything.
    if (it.sub && it.title && it.sub.toLowerCase() === it.title.toLowerCase()) {
      fail(`${it.kind}'s sub only repeats its title`);
    }
  }
}

// ---- no duplicates --------------------------------------------------------
{
  const seen = new Set();
  for (const it of SETTINGS_ITEMS) {
    if (seen.has(it.kind)) fail(`${it.kind} appears twice — two rail rows would light up`);
    seen.add(it.kind);
  }
}

// ---- every kind is a modal App.svelte can actually load --------------------
{
  const app = fs.readFileSync(path.join(SRC, "App.svelte"), "utf8");
  // MODAL_LOADERS is written `kind: () => import(...)`. Reading the keys out of
  // the source rather than importing App.svelte keeps this a plain node test.
  const block = /const MODAL_LOADERS\s*=\s*\{([\s\S]*?)\n\s*\};/.exec(app);
  if (!block) fail("cannot find MODAL_LOADERS in App.svelte");
  else {
    const kinds = new Set([...block[1].matchAll(/^\s*([A-Za-z][A-Za-z0-9]*):/gm)].map((m) => m[1]));
    for (const it of SETTINGS_ITEMS) {
      if (!kinds.has(it.kind)) fail(`${it.kind} is not a modal App.svelte can load`);
    }
  }
}

// ---- every icon exists ----------------------------------------------------
{
  const icon = fs.readFileSync(path.join(SRC, "Icon.svelte"), "utf8");
  const names = new Set([...icon.matchAll(/^\s{4}([A-Za-z][A-Za-z0-9]*):/gm)].map((m) => m[1]));
  for (const it of SETTINGS_ITEMS) {
    if (!names.has(it.icon)) fail(`${it.kind} asks for the icon "${it.icon}", which Icon.svelte has not got`);
  }
}

// ---- the lookups ----------------------------------------------------------
check("settingsItem finds a known kind", settingsItem("appearance")?.title === "Appearance");
check("settingsItem returns null for a stranger", settingsItem("emoji") === null);
check("inSettings knows the account page is one of ours", inSettings("settings"));
check("inSettings says no to a dialog that is not", !inSettings("poll"));

console.log(
  failures === 0 ? "settingsnav.test.mjs: OK" : `settingsnav.test.mjs: ${failures} failure(s)`,
);
process.exit(failures ? 1 : 0);
