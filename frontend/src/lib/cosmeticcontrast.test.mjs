// Can you SEE the cosmetics? (`npm test` runs this.)
//
// The card-effect and ring libraries were both written against #16181c, and it
// shows: they are palettes of white snow, neon cyan and pastel confetti. Nobody
// ever measured them against a bright ground, and on the seven daylight
// surfaces the app ships — the plain light theme plus the six bright packs —
// 26 of the 29 card effects and 19 of the 47 rings had NOT ONE colour above
// 2.2:1 against the page. Not "hard to see". Not there. The picker showed two
// dozen empty tiles and a light-theme user picking from them was choosing
// between different kinds of nothing.
//
// The fix is a single darkening applied to the effect layer on daylight
// grounds (app.css, --fx-dim). This file is the other half of it: the factor
// lives in the stylesheet and the colours live in the data, and the only thing
// that can keep the two honest is a test that reads both.
//
// Two questions, one for each end of the collection:
//
//   1. DAYLIGHT. With --fx-dim applied, does EVERY colour in both libraries
//      clear 3:1 against every bright ground? This is what makes the fix a
//      fix rather than a hope, and it is what fails if the factor is ever
//      relaxed for looks.
//   2. NIGHT. Untouched by the dim, does every effect and every ring still
//      have SOMETHING in it you can see against every dark ground? Per-entry
//      rather than per-colour, because a deliberate near-black stop in a conic
//      gradient is the night in a night scene, not a bug — but an entry with
//      nothing above 3:1 is invisible, which is.
//
// 3:1 is the WCAG floor for non-text, which is what a drifting speck is.
import { readFileSync } from "node:fs";
import { contrast, colorsIn, daylightGrounds } from "./contrast.mjs";
import { CARD_EFFECTS } from "./cardfx.js";
import { RINGS, PALETTES } from "./rings.js";
import { THEME_FX } from "./themefx.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

const css = readFileSync(new URL("../app.css", import.meta.url), "utf8");
const FLOOR = 3;

// --- what the collection is made of ------------------------------------------

// Colours that resolve at paint time — the wearer's own two profile colours, or
// a theme token. Neither can be measured here and neither needs to be: a token
// flips with the ground by construction (that is the whole point of using one),
// and a profile colour is the wearer's business. Counted, though: if this ever
// swallows a whole library the gate has stopped measuring anything.
const deferred = (c) => String(c).includes("var(");

// Every entry in both libraries, flattened to { lib, id, colors }.
function entries() {
  const out = [];
  for (const e of CARD_EFFECTS) out.push({ lib: "cardfx", id: e.id, colors: e.fx.colors || [] });
  for (const p of PALETTES) {
    // A palette stop is "<color> <position>" — the position is not a colour.
    out.push({ lib: "rings/palette", id: p.id, colors: p.stops.map((s) => s.trim().split(/\s+/)[0]) });
  }
  for (const r of RINGS) {
    const cols = [];
    if (r.art) cols.push(r.art);
    if (r.fx?.colors) cols.push(...r.fx.colors);
    if (typeof r.band === "string" && r.band) cols.push(r.band);
    if (r.halo) cols.push(r.halo);
    if (r.orbit?.dot) cols.push(r.orbit.dot);
    if (r.orbit?.dot2) cols.push(r.orbit.dot2);
    if (cols.length) out.push({ lib: "rings", id: r.id, colors: cols });
  }
  for (const f of THEME_FX) {
    if (f.particle?.colors) out.push({ lib: "themefx", id: f.id, colors: f.particle.colors });
  }
  return out;
}

// A colour source (a hex, an rgba(), or a whole conic-gradient) expanded to the
// opaque triples it can put on screen. Alpha is deliberately dropped: what is
// being asked is whether the PIGMENT differs from the page, not whether one
// pass of a 30%-alpha wash does — an aurora curtain is meant to be faint, and
// measuring it as painted would only prove that.
function triples(src) {
  return colorsIn(src)
    .filter((c) => c[3] > 0)
    .map(([r, g, b]) => [r, g, b]);
}

const ALL = entries();
assert(ALL.length > 90, `only ${ALL.length} cosmetic entries found — the scrape is broken`);

// --- 1. the daylight dim -----------------------------------------------------

// Read the factor back out of app.css. Two rules, both required: the token has
// to exist on the daylight block, and .fxfield has to actually apply it. A
// perfect number in a stylesheet nothing reads is worth nothing.
const dimDecl = css.match(/--fx-dim:\s*brightness\(([\d.]+)\)/);
assert(!!dimDecl, "app.css no longer declares --fx-dim: brightness(N) for the daylight packs");
assert(
  /\.fxfield\s*\{[^}]*filter:\s*var\(--fx-dim/.test(css),
  ".fxfield no longer applies filter: var(--fx-dim) — the token is declared but nothing reads it",
);
const DIM = dimDecl ? Number(dimDecl[1]) : 1;
assert(DIM > 0 && DIM <= 1, `--fx-dim brightness(${DIM}) is not a darkening`);

// CSS filter functions operate on sRGB channels, so brightness(k) is exactly a
// per-channel multiply — which is why this is modelable at all.
const dim = ([r, g, b]) => [r * DIM, g * DIM, b * DIM];

const GROUNDS = daylightGrounds(css);
assert(GROUNDS.length === 7, `expected 7 daylight grounds, read ${GROUNDS.length}`);

let measured = 0;
let deferredCount = 0;
let worstDay = { ratio: Infinity };
for (const e of ALL) {
  for (const src of e.colors) {
    if (deferred(src)) {
      deferredCount++;
      continue;
    }
    for (const rgb of triples(src)) {
      measured++;
      for (const g of GROUNDS) {
        const ratio = contrast(dim(rgb), g.rgb);
        if (ratio < worstDay.ratio) worstDay = { ratio, id: e.id, lib: e.lib, src, pack: g.name };
        assert(
          ratio >= FLOOR,
          `${e.lib} "${e.id}": ${src} is only ${ratio.toFixed(2)}:1 on the ${g.name} ground ` +
            `even after --fx-dim (need ${FLOOR}) — darken the colour or the dim`,
        );
      }
    }
  }
}
assert(measured > 250, `only ${measured} colours measured — the parse is dropping entries`);
assert(
  deferredCount < measured,
  `${deferredCount} colours defer to a token and only ${measured} were measured — ` +
    "the libraries have stopped being checkable",
);

// --- 2. still visible at night -----------------------------------------------

// The dark grounds the app actually ships. Read the same way, for the same
// reason: a retuned pack must retune the gate.
const DARK = [
  ':root[data-theme-pack="nord"]',
  ':root[data-theme-pack="gruvbox"]',
  ':root[data-theme-pack="dracula"]',
];
const darkGrounds = [{ name: "default", rgb: [0x16, 0x18, 0x1c] }];
for (const sel of DARK) {
  const at = css.indexOf(sel);
  if (at < 0) continue;
  const m = css.slice(at, css.indexOf("\n}", at)).match(/--bg-1:\s*(#[0-9a-f]{6})/i);
  if (m) darkGrounds.push({ name: sel, rgb: triples(m[1])[0] });
}
assert(darkGrounds.length > 1, "no dark pack grounds could be read out of app.css");

for (const e of ALL) {
  const own = e.colors.filter((c) => !deferred(c)).flatMap(triples);
  if (!own.length) continue; // wholly token-driven: correct by construction
  for (const g of darkGrounds) {
    assert(
      own.some((rgb) => contrast(rgb, g.rgb) >= FLOOR),
      `${e.lib} "${e.id}" has nothing above ${FLOOR}:1 on ${g.name} — it is invisible at night`,
    );
  }
}

// --- 3. the maths itself -----------------------------------------------------

assert(Math.abs(contrast([255, 255, 255], [0, 0, 0]) - 21) < 0.01, "white on black must be 21:1");
assert(Math.abs(contrast([255, 255, 255], [255, 255, 255]) - 1) < 0.01, "white on white must be 1:1");
assert(contrast([255, 255, 255], [128, 128, 128]) < 4.5, "white on mid grey must not pass AA");
// Guard the guard: without the dim the daylight half MUST fail, or it is
// measuring something other than what it claims to.
assert(
  ALL.some((e) =>
    e.colors
      .filter((c) => !deferred(c))
      .flatMap(triples)
      .some((rgb) => GROUNDS.some((g) => contrast(rgb, g.rgb) < FLOOR)),
  ),
  "every raw colour already clears the daylight floor — this test would pass with --fx-dim removed",
);

if (failures) {
  console.error(`\n${failures} cosmetic-contrast test(s) failed`);
  process.exit(1);
}
console.log(
  `cosmeticcontrast.test.mjs: all passed (${ALL.length} entries, ${measured} colours, ` +
    `${deferredCount} deferred to tokens; worst daylight ${worstDay.ratio.toFixed(2)}:1 — ` +
    `${worstDay.lib} "${worstDay.id}" ${worstDay.src} on ${worstDay.pack})`,
);
