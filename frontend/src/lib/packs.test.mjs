// The theme-pack table (lib/packs.js), checked (`npm test` runs it).
//
// Three things live in that table and nowhere else: which packs exist, which
// of them are LIGHT grounds, and where a removed pack's users go. Each has a
// way of going wrong quietly, and this is what makes it loud.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { contrast, colorsIn } from "./contrast.mjs";
import {
  PACKS,
  PACK_IDS,
  LIGHT_PACKS,
  ANIMATED_PACKS,
  PACK_MIGRATION,
  PREFS_VERSION,
  groundOf,
  migratePrefs,
  packIdOrDefault,
} from "./packs.js";
import { THEME_FX, validFx, resolveFx, seasonFx } from "./themefx.js";

const SRC = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const packsCss = fs.readFileSync(path.join(SRC, "themepacks.css"), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");

let failures = 0;
const fail = (msg) => {
  console.error("  FAIL " + msg);
  failures++;
};
const eq = (got, want, msg) => {
  if (JSON.stringify(got) !== JSON.stringify(want)) fail(`${msg}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
};

// ---- 1. the table and the stylesheet name the same packs -------------------
{
  const inCss = new Set([...packsCss.matchAll(/data-theme-pack="([\w-]+)"/g)].map((m) => m[1]));
  for (const id of PACK_IDS) if (!inCss.has(id)) fail(`"${id}" is in lib/packs.js but themepacks.css never styles it`);
  for (const id of inCss) if (!PACK_IDS.has(id)) fail(`themepacks.css styles "${id}", which lib/packs.js does not offer`);
}

// ---- 2. a light pack is in the light list, and only a light pack is --------
//
// The bug this guards: a pack with a bright ground that the table calls dark
// gets no [data-ground="light"], so under the Light theme the light palette
// does not apply to it and its own palette is all there is; under Dark, the
// daylight block (status colours, shadows, scrim) never reaches it. The
// pack's own --bg-1 decides, read out of the stylesheet.
{
  const bg1 = (id) => {
    let found = null;
    const re = new RegExp(`:root\\[data-theme-pack="${id}"\\]\\s*\\{([^}]*)\\}`, "g");
    for (const m of packsCss.matchAll(re)) {
      const d = m[1].match(/--bg-1:\s*([^;]+);/);
      if (d) found = colorsIn(d[1])[0];
    }
    return found;
  };
  for (const p of PACKS) {
    const c = bg1(p.id);
    if (!c) {
      fail(`cannot read --bg-1 for "${p.id}" from themepacks.css`);
      continue;
    }
    // Brighter than mid-grey, measured as contrast against black vs white.
    const light = contrast(c.slice(0, 3), [0, 0, 0]) > contrast(c.slice(0, 3), [255, 255, 255]);
    if (light && !LIGHT_PACKS.has(p.id)) fail(`"${p.id}" has a light ground (--bg-1) but lib/packs.js does not mark it day: true`);
    if (!light && LIGHT_PACKS.has(p.id)) fail(`"${p.id}" is marked day: true but its ground (--bg-1) is dark`);
  }
}

// ---- 3. no stylesheet or component lists packs by name ---------------------
//
// [data-ground] is the only way to ask "is this a light ground". A list of
// pack names anywhere else is the seven-places problem coming back.
{
  const offenders = [];
  const walk = (dir) => {
    for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
      const p = path.join(dir, e.name);
      if (e.isDirectory()) walk(p);
      else if (/\.(css|svelte|js)$/.test(e.name) && e.name !== "themepacks.css" && !e.name.endsWith(".test.mjs")) {
        const text = fs.readFileSync(p, "utf8");
        if (/data-theme-pack="[\w-]+"/.test(text)) offenders.push(path.relative(SRC, p));
      }
    }
  };
  walk(SRC);
  if (offenders.length) fail(`these name a theme pack in a selector: ${offenders.join(", ")} — key on [data-ground] instead`);
}

// ---- 4. groundOf -----------------------------------------------------------
eq(groundOf("dark", ""), "dark", "no pack, Dark");
eq(groundOf("light", ""), "light", "no pack, Light");
for (const id of PACK_IDS) {
  const want = LIGHT_PACKS.has(id) ? "light" : "dark";
  eq(groundOf("dark", id), want, `${id} under Dark`);
  eq(groundOf("light", id), want, `${id} under Light`);
}
eq([...ANIMATED_PACKS], ["aurora"], "the animated packs");

// ---- 5. every removed pack lands on a kept one -----------------------------
//
// The fifty packs v0.60.2 shipped, frozen here: a later edit to the table
// must still account for every one of them.
const V0602_PACKS = (
  "abyss atrium aurora bloom blossom canopy cosmos datastream daybreak dracula dunes dusk eclipse fathom " +
  "forest frost glacier grape gruvbox harbor harvest hearth lantern meadow meridian midnight molten monsoon " +
  "nebula nord observatory oceanic orbit paper phosphor porcelain prism radiant rose sakura silicon skyline " +
  "solarized sonar storm synthwave tundra uptime vinyl zellige"
).split(" ");
eq(V0602_PACKS.length, 50, "the v0.60.2 pack list");

// One expectation per removed pack, written out rather than derived from the
// table, so a changed mapping is a deliberate edit in two places.
const EXPECT = {
  nebula: ["dracula"], grape: ["dracula"], synthwave: ["dracula"], skyline: ["dracula"],
  sakura: ["rose"], dusk: ["rose"], blossom: ["rose"],
  hearth: ["gruvbox"], harvest: ["gruvbox"], molten: ["gruvbox"], vinyl: ["gruvbox"], dunes: ["gruvbox"],
  sonar: ["gruvbox"], bloom: ["gruvbox"],
  oceanic: ["nord"], tundra: ["nord"], harbor: ["nord"], frost: ["nord"], glacier: ["nord"], storm: ["nord"],
  monsoon: ["nord"], meridian: ["nord"], silicon: ["nord"],
  observatory: ["midnight"], cosmos: ["midnight"], eclipse: ["midnight"], lantern: ["midnight"],
  orbit: ["midnight"], radiant: ["midnight"],
  phosphor: ["abyss"], datastream: ["abyss"], uptime: ["abyss"],
  fathom: ["aurora"], canopy: ["aurora"], zellige: ["aurora"],
  forest: [""], prism: [""],
  // Light grounds that go to the default palette take the Light theme with
  // them: under Dark, the default palette is black.
  meadow: ["", "light"], porcelain: ["", "light"], daybreak: ["", "light"],
  atrium: ["paper"],
};
{
  const removed = V0602_PACKS.filter((id) => !PACK_IDS.has(id));
  eq(removed.length, 41, "packs removed since v0.60.2");
  eq(Object.keys(EXPECT).sort(), removed.sort(), "a written expectation for every removed pack");
  eq(Object.keys(PACK_MIGRATION).sort(), removed.sort(), "a migration for every removed pack");
  for (const id of removed) {
    const [pack, forcedTheme] = EXPECT[id];
    for (const theme of ["dark", "light", "system"]) {
      const { prefs, changed } = migratePrefs({ themePack: id, theme, prefsV: PREFS_VERSION });
      if (!changed) fail(`${id}: migrating it reported no change`);
      eq(prefs.themePack, pack, `${id} (theme ${theme}) migrates to`);
      eq(prefs.theme, forcedTheme || theme, `${id} (theme ${theme}) leaves the theme at`);
      if (prefs.themePack && !PACK_IDS.has(prefs.themePack)) fail(`${id} migrates to "${prefs.themePack}", which does not exist`);
      // Readable: the ground the user ends on is the ground they chose. A
      // light pack must not land on a dark ground, nor a dark one on light.
      const resolved = forcedTheme || (theme === "light" ? "light" : "dark");
      const was = ["meadow", "porcelain", "daybreak", "atrium", "paper", "solarized"].includes(id) ? "light" : "dark";
      const now = groundOf(resolved, prefs.themePack);
      if (theme !== "system" && was === "light" && now !== "light") fail(`${id}: a light pack lands on a ${now} ground`);
      if (was === "dark" && prefs.themePack && now !== "dark") fail(`${id}: a dark pack lands on a ${now} ground`);
    }
  }
  // A kept pack is left alone, and so is no pack.
  for (const id of PACK_IDS) eq(migratePrefs({ themePack: id, prefsV: 2 }).prefs.themePack, id, `kept pack ${id}`);
  eq(migratePrefs({ themePack: "", theme: "light", prefsV: 2 }).prefs, { themePack: "", theme: "light", prefsV: 2 }, "no pack");
  // Anything unknown goes to the default palette, theme untouched.
  // R1: an id this build did not REMOVE is not this build's to rewrite. It may
  // be a pack a newer build added; it stays saved and draws as the default.
  for (const id of ["vaporwave2026", "constructor", "toString", "__proto__"]) {
    const { prefs } = migratePrefs({ themePack: id, theme: "light", prefsV: 2 });
    eq(prefs.themePack, id, `an unknown pack "${id}" is kept in storage`);
    eq(prefs.theme, "light", `an unknown pack "${id}" leaves the theme`);
    eq(packIdOrDefault(id), "", `an unknown pack "${id}" draws as the default`);
  }
  for (const id of PACK_IDS) eq(packIdOrDefault(id), id, `kept pack ${id} draws as itself`);
  eq(migratePrefs({ themePack: 7 }).prefs.themePack, "", "a non-string pack");
}

// ---- 6. the rest of the migration ------------------------------------------
{
  // Sweeping highlights: the old default (true) is turned off once…
  eq(migratePrefs({ themeShine: true }).prefs.themeShine, false, "the stored old default");
  eq(migratePrefs({}).prefs.themeShine, false, "a fresh install");
  // …and a choice made after that sticks.
  eq(migratePrefs({ themeShine: true, prefsV: 2 }).prefs.themeShine, true, "an explicit on after the flip");
  eq(migratePrefs({ prefsV: 2 }).changed, false, "an up-to-date profile is not rewritten");
  // Seasonal touches → the Seasonal effect, unless an effect was already on.
  // R2: it only ever drew on the Rail layout, so only Rail users get it.
  eq(migratePrefs({ seasonal: true, navModel: "rail" }).prefs.themeFx, "seasonal", "seasonal on, Rail");
  eq(migratePrefs({ seasonal: true }).prefs.themeFx, undefined, "seasonal on, Sidebar (never saw it)");
  eq(migratePrefs({ seasonal: true, navModel: "sidebar", themeFx: "" }).prefs.themeFx, "", "seasonal on, Sidebar, no effect");
  eq(migratePrefs({ seasonal: true, navModel: "rail", themeFx: "rain" }).prefs.themeFx, "rain", "seasonal on, Rail, rain kept");
  eq(migratePrefs({ seasonal: true, navModel: "rail", themeFx: "crt" }).prefs.themeFx, "crt", "an effect picked is never replaced");
  eq("seasonal" in migratePrefs({ seasonal: false }).prefs, false, "the seasonal key goes");
  // Layout › Compact → Sidebar; density and the panel already say the rest.
  eq(migratePrefs({ layout: "compact", density: "compact", memberPanel: false }).prefs, {
    layout: "sidebar",
    density: "compact",
    memberPanel: false,
    themeShine: false,
    prefsV: 2,
  }, "compact layout");
  // Dead keys go.
  const d = migratePrefs({ colChannels: 220, colMembers: 260, guildAccents: true, sidebarW: 300 }).prefs;
  eq([d.colChannels, d.colMembers, d.guildAccents, d.sidebarW], [undefined, undefined, undefined, 300], "dead keys");
  // Idempotent, and safe on junk.
  const once = migratePrefs({ themePack: "porcelain", theme: "dark", seasonal: true, navModel: "rail", layout: "compact" }).prefs;
  eq(migratePrefs(once).prefs, once, "migrating twice");
  eq(migratePrefs(once).changed, false, "migrating twice changes nothing");
  for (const junk of [null, undefined, 3, "x", []]) eq(migratePrefs(junk).prefs.prefsV, 2, `junk ${JSON.stringify(junk)}`);
}

// ---- 7. an older build reading what this one wrote --------------------------
//
// Appearance prefs are device-local (localStorage "concord.prefs"): never in
// the profile, never in linked-device sync, never in a backup. The only older
// reader is this same device going back to an earlier build. These are the
// values v0.60.2 understands (its themefx.js validFx, its layout.js, its pack
// stylesheet); everything a migration writes must be one of them, or a value
// v0.60.2 provably treats as off.
{
  const V0602_FX = new Set(["", "snow", "rain", "stars", "embers", "crt", "leaves"]);
  const V0602_LAYOUTS = new Set(["sidebar", "compact"]); // + navModel "rail"
  const samples = [
    ...V0602_PACKS.map((id) => ({ themePack: id, theme: "dark" })),
    { seasonal: true, navModel: "rail" },
    { layout: "compact", density: "compact" },
    { themeShine: true },
  ];
  for (const s of samples) {
    const p = migratePrefs(s).prefs;
    if (p.themePack && !V0602_PACKS.includes(p.themePack)) fail(`v0.60.2 has no pack "${p.themePack}"`);
    if (p.layout && !V0602_LAYOUTS.has(p.layout)) fail(`v0.60.2 has no layout "${p.layout}"`);
    // v0.60.2's validFx is `BY_ID.has(id) ? id : ""`: an id it does not know
    // reads as no effect at all, which is the defined fallback, not a break.
    if (p.themeFx && !V0602_FX.has(p.themeFx) && p.themeFx !== "seasonal") fail(`unexpected effect "${p.themeFx}"`);
    // themeShine false: v0.60.2 stamps shine-off on the literal false only.
    if ("themeShine" in p && typeof p.themeShine !== "boolean") fail("themeShine is not a boolean");
  }
}

// ---- 8. Seasonal resolves by month -----------------------------------------
{
  const at = (m) => new Date(2026, m, 15);
  const want = ["snow", "", "petals", "petals", "", "", "", "", "", "leaves", "leaves", "snow"];
  for (let m = 0; m < 12; m++) eq(resolveFx("seasonal", at(m)), want[m], `Seasonal in month ${m + 1}`);
  eq(seasonFx(at(5)), "", "June is quiet");
  eq(validFx("seasonal"), "seasonal", "Seasonal is a valid pref");
  eq(validFx("petals"), "", "petals is not offered on its own");
  eq(THEME_FX.map((f) => f.id), ["", "snow", "rain", "stars", "embers", "crt", "leaves", "seasonal"], "the effects offered");
}

if (failures) {
  console.error(`packs: ${failures} failure(s)`);
  process.exit(1);
}
console.log(`packs: ${PACKS.length} packs, ${LIGHT_PACKS.size} light, ${Object.keys(PACK_MIGRATION).length} migrations checked`);
