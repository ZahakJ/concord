// Zero-dependency test for the guild banner templates (`npm test` runs it).
//
// Three things can break a template library silently, so all three are checked
// here rather than in a browser:
//
//   1. The WIRE. A banner travels as "preset:<id>" and the backend rejects
//      anything outside 1–32 chars of a-z/0-9/'-' (internal/app/guild.go,
//      validPresetID) — a template whose id doesn't fit simply cannot be saved,
//      and the failure would land on the user, not on us. Ids also have to be
//      unique across BOTH catalogues, because Banner.svelte resolves the profile
//      one first: a guild template that duplicates a profile id would silently
//      render the profile scene instead.
//   2. The ENGINE. An fx kind is a string; a typo produces a container with no
//      keyframes — a base gradient that quietly never animates. So every kind is
//      checked against what fx.js and app.css can actually paint.
//   3. CONTRAST. A guild banner has a name and an icon printed on it. Every
//      template is composited under the header's scrim here and must clear
//      WCAG AA (4.5:1) for the ink it asks for — see contrast() below for how
//      the worst case is derived rather than eyeballed.
import { readFileSync } from "node:fs";
import { contrast, worstBackdrop } from "./contrast.mjs";
import { LAYERED } from "./fx.js";
import { BANNERS } from "./banners.js";
import {
  GUILD_BANNERS,
  GUILD_BANNER_BY_ID,
  GUILD_BANNER_GROUPS,
  SCRIM_ALPHA,
  guildBannerArt,
  guildPresetOf,
} from "./guildbanners.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

// --- 1. the wire -------------------------------------------------------------

// The exact charset internal/app/guild.go accepts after "preset:".
const WIRE_ID = /^[a-z0-9-]{1,32}$/;
const seen = new Set(BANNERS.map((b) => b.id));
for (const t of GUILD_BANNERS) {
  assert(WIRE_ID.test(t.id), `id "${t.id}" must be 1–32 chars of a-z, 0-9 or -`);
  assert(!seen.has(t.id), `id "${t.id}" is already taken (guild or profile catalogue)`);
  seen.add(t.id);
  assert(!!t.name && t.name.length <= 24, `"${t.id}" needs a short display name`);
  assert(typeof t.base === "string" && t.base.length > 0, `"${t.id}" needs a painted base`);
  assert(t.ink === undefined || t.ink === "dark", `"${t.id}" ink must be omitted or "dark"`);
}
assert(GUILD_BANNERS.length === Object.keys(GUILD_BANNER_BY_ID).length, "GUILD_BANNER_BY_ID lost an entry to a duplicate id");

// --- groups ------------------------------------------------------------------

for (const t of GUILD_BANNERS) {
  assert(GUILD_BANNER_GROUPS.includes(t.group), `"${t.id}" is in group "${t.group}", which no shelf shows`);
}
for (const grp of GUILD_BANNER_GROUPS) {
  assert(
    GUILD_BANNERS.some((t) => t.group === grp),
    `group "${grp}" is empty — the picker would print a heading over nothing`,
  );
}

// --- 2. the engine -----------------------------------------------------------

// A kind exists if lib/fx.js knows it's layered or app.css has keyframes bound
// to a `.fx-<kind>` rule. Reading the stylesheet is the point: the data half and
// the painting half of an effect live in different files and either can rot.
const css = readFileSync(new URL("../app.css", import.meta.url), "utf8");
const painted = new Set([...css.matchAll(/\.fx-([a-z]+)\b/g)].map((m) => m[1]));
const KINDS = new Set([...LAYERED, ...painted]);
for (const t of GUILD_BANNERS) {
  if (!t.fx) continue;
  assert(KINDS.has(t.fx.kind), `"${t.id}" uses fx kind "${t.fx.kind}", which the engine cannot paint`);
}
// Guard the guard: if the scrape ever stops finding kinds, the check above turns
// into a no-op that passes forever.
for (const k of ["fall", "rise", "twinkle", "streak", "matrix", "rain"]) {
  assert(KINDS.has(k), `kind scrape is broken — app.css should paint "${k}"`);
}

// --- 3. contrast under the header scrim --------------------------------------

// The maths lives in contrast.mjs now — the same worst-case compositing walk
// answers "can you read the guild name over this banner?" here and "can you see
// this ring at all on a daylight pack?" in cosmeticcontrast.test.mjs, and two
// copies of it would have drifted the first time either was tuned.

// The two inks the header prints in (ChannelList.svelte .guild-header).
const INK = { light: [255, 255, 255], dark: [0x12, 0x16, 0x1a] };

// The scrim: opaque black (light ink) or white (dark ink) at SCRIM_ALPHA.
const scrimmed = (rgb, ink) => {
  const a = SCRIM_ALPHA[ink];
  const over = ink === "dark" ? 255 : 0;
  return rgb.map((c) => a * over + (1 - a) * c);
};

for (const t of GUILD_BANNERS) {
  const ink = t.ink || "light";
  const worst = worstBackdrop(t.base, ink === "dark" ? -1 : 1);
  const ratio = contrast(INK[ink], scrimmed(worst, ink));
  assert(
    ratio >= 4.5,
    `"${t.id}": header text over its worst pixel is only ${ratio.toFixed(2)}:1 (need 4.5) — ` +
      `worst backdrop rgb(${worst.map(Math.round).join(",")})`,
  );
  // Particles are specks rather than a field, so they answer to the large-text
  // floor (3:1) instead — but a blazing white effect still can't wash out a name.
  if (t.fx?.colors) {
    const fxWorst = worstBackdrop(t.fx.colors.join(","), ink === "dark" ? -1 : 1);
    const fxRatio = contrast(INK[ink], scrimmed(fxWorst, ink));
    assert(fxRatio >= 3, `"${t.id}": its fx colours only reach ${fxRatio.toFixed(2)}:1 behind the name (need 3)`);
  }
}

// Sanity-check the contrast maths itself against known values.
assert(Math.abs(contrast([255, 255, 255], [0, 0, 0]) - 21) < 0.01, "white on black must be 21:1");
assert(Math.abs(contrast([255, 255, 255], [255, 255, 255]) - 1) < 0.01, "white on white must be 1:1");
assert(contrast([255, 255, 255], [128, 128, 128]) < 4.5, "white on mid grey must not pass AA");

// --- 4. what an unknown template does ----------------------------------------

// The whole point of guildBannerArt: a value we can't safely paint yields null,
// and the header renders with no banner at all. Never a broken box, and never a
// raw string on its way into a CSS declaration.
for (const bad of [
  "",
  "preset:",
  "preset:nope",
  "preset:galaxy-2099", // a template a newer peer has and we don't
  "preset:Neon-Coliseum", // right template, wrong case: not the wire charset
  'preset:x");background:url(//evil',
  "url(//evil/x.png)",
  "data:image/svg+xml;base64,AAAA", // an image type that can carry script
  "data:image/png;base64,AA AA", // whitespace: not plain base64
  "https://example.com/x.png",
  "linear-gradient(red, blue)",
]) {
  assert(guildBannerArt(bad) === null, `guildBannerArt(${JSON.stringify(bad)}) must degrade to no banner`);
}
assert(guildPresetOf("preset:nope") === null, "guildPresetOf must be null for an unknown id");
assert(guildBannerArt("preset:neon-coliseum")?.kind === "preset", "a known template resolves as a preset");
assert(guildBannerArt("preset:neon-coliseum")?.ink === "light", "templates default to light ink");
assert(guildBannerArt("preset:linen-press")?.ink === "dark", "a pale template asks for dark ink");
assert(guildBannerArt("data:image/gif;base64,R0lGOD")?.kind === "image", "an uploaded GIF still paints as an image");
assert(guildBannerArt("data:image/gif;base64,R0lGOD")?.template === null, "an image has no template");

if (failures) {
  console.error(`\n${failures} guildbanners test(s) failed`);
  process.exit(1);
}
console.log(`guildbanners.test.mjs: all passed (${GUILD_BANNERS.length} templates)`);
