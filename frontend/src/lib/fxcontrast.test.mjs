// Can you SEE the window effects, and a pale emoji on a daylight pill?
// (`npm test` runs this.)
//
// Two questions:
//
//   1. NIGHT. Does every window effect (lib/themefx.js) have SOMETHING in it
//      you can see against every dark ground? Per-entry rather than
//      per-colour, because one faint colour among bright ones is a choice —
//      but an entry with nothing above 3:1 is invisible. Colours given as a
//      theme token flip with the ground by construction and are not measured.
//   2. EMOJI. On a daylight ground a near-white glyph on a pale pill rests on
//      the --emoji-edge outline; the edge has to clear 3:1 and be applied.
//
// 3:1 is the WCAG floor for non-text, which is what a drifting speck is.
import { readFileSync } from "node:fs";
import { contrast, colorsIn, over, DAYLIGHT } from "./contrast.mjs";
import { groundOf } from "./packs.js";
import { THEME_FX } from "./themefx.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

// Both halves of the stylesheet: the pack palettes live in their own file so
// that a session which never chooses one does not download them, and the gate
// has to keep reading the palettes wherever they live.
const css =
  readFileSync(new URL("../app.css", import.meta.url), "utf8") +
  "\n" +
  readFileSync(new URL("../themepacks.css", import.meta.url), "utf8");
const FLOOR = 3;

const deferred = (c) => String(c).includes("var(");

// A colour source (a hex, an rgba(), a gradient) expanded to the opaque
// triples it can put on screen. Alpha is dropped: the question is whether the
// PIGMENT differs from the page.
function triples(src) {
  return colorsIn(src)
    .filter((c) => c[3] > 0)
    .map(([r, g, b]) => [r, g, b]);
}

// --- 1. window effects at night ------------------------------------------------

const ALL = THEME_FX.filter((f) => f.particle?.colors).map((f) => ({ id: f.id, colors: f.particle.colors }));
assert(ALL.length > 0, "no window effect with particle colours found — the scrape is broken");

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
assert(darkGrounds.length > 1, "no dark pack grounds could be read out of the stylesheets");

for (const e of ALL) {
  const own = e.colors.filter((c) => !deferred(c)).flatMap(triples);
  if (!own.length) continue; // wholly token-driven: correct by construction
  for (const g of darkGrounds) {
    assert(
      own.some((rgb) => contrast(rgb, g.rgb) >= FLOOR),
      `themefx "${e.id}" has nothing above ${FLOOR}:1 on ${g.name} — it is invisible at night`,
    );
  }
}

// --- 2. an emoji on a daylight pill ------------------------------------------
//
// Reactions draw the emoji set's own colours on a pill of --bg-3 or, for one
// you made, the accent tint — both pale on a daylight ground, and several
// glyphs (the skull, the ghost) are near-white. Their visibility there rests
// on the --emoji-edge outline, so the gate measures it: the edge, laid over
// the palest thing it can sit on (white), must clear the non-text floor.
// And it must be read: a token nothing applies is not a fix.
{
  // Keyed on the ground, not on a list of names: lib/packs.js stamps
  // [data-ground="light"] for the light theme and for every light pack.
  const block = css.match(/:root:root\[data-ground="light"\] \{[\s\S]*?\n\}/);
  assert(!!block, "app.css: the grouped daylight block is gone — --emoji-edge has nowhere to live");
  const decl = block && block[0].match(/--emoji-edge:\s*([^;]+);/);
  assert(!!decl, "the daylight block declares no --emoji-edge — pale emoji on light pills lose their edge");
  if (decl) {
    const shadows = colorsIn(decl[1]);
    assert(shadows.length > 0, `--emoji-edge (${decl[1].trim()}) has no colour to measure`);
    for (const c of shadows) {
      const ratio = contrast(over(c, [255, 255, 255]), [255, 255, 255]);
      assert(
        ratio >= FLOOR,
        `--emoji-edge ${JSON.stringify(c)} is ${ratio.toFixed(2)}:1 against a white glyph (need ${FLOOR})`,
      );
    }
    for (const g of DAYLIGHT) {
      const ground = g.name === "light" ? groundOf("light", "") : groundOf("dark", g.name);
      assert(ground === "light", `the daylight block no longer covers ${g.name} (its ground is ${ground})`);
    }
  }
  const msg = readFileSync(new URL("../Message.svelte", import.meta.url), "utf8");
  assert(
    /\.remoji\s*\{[^}]*filter:\s*var\(--emoji-edge/.test(msg),
    "Message.svelte .remoji no longer applies var(--emoji-edge)",
  );
}

// --- 3. the maths itself -----------------------------------------------------

assert(Math.abs(contrast([255, 255, 255], [0, 0, 0]) - 21) < 0.01, "white on black must be 21:1");
assert(Math.abs(contrast([255, 255, 255], [255, 255, 255]) - 1) < 0.01, "white on white must be 1:1");
assert(contrast([255, 255, 255], [128, 128, 128]) < 4.5, "white on mid grey must not pass AA");

if (failures) {
  console.error(`\n${failures} fx-contrast test(s) failed`);
  process.exit(1);
}
console.log(`fxcontrast.test.mjs: all passed (${ALL.length} window effects, ${darkGrounds.length} dark grounds)`);
