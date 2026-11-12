// pack-retune.mjs — hold every theme pack's text tiers to the reading floor.
//
//   node scripts/pack-retune.mjs           report, change nothing (exit 1 if a pack is short)
//   node scripts/pack-retune.mjs --write   rewrite src/themepacks.css in place
//
// The interface system made --text-faint a text tier: timestamps, "(edited)",
// group labels and meta are words, and words get 4.5:1. The two base themes
// were retuned by hand. The packs were tuned when faint was decoration (3:1 on
// their own --bg-3), and fifty palettes is too many to retune by eye, so this
// does it by measurement and changes as little as it can:
//
//   - --text-faint and --text-muted are measured against every surface the
//     pack paints (--bg-0 to --bg-3 and --bg-elevated). The translucent packs'
//     grounds are composited over the pack's own --bg-elevated, which is
//     opaque in every pack, the same reading tokens.test.mjs rule 8b uses.
//   - A tier that falls short is walked toward --text in sRGB until its worst
//     surface clears 4.6 (a hair over 4.5, so rounding never lands it under).
//     The hue stays the pack's; only the lightness moves, and a tier that
//     already reads is left byte-identical.
//   - A pack with no --border-strong (the control-edge token) gets one: its
//     --text-muted mixed toward --bg-2 as far as it can go while still
//     clearing 3:1 against every surface, so a field's edge is in the pack's
//     own colour rather than the default theme's grey.
//
// Glows need nothing here: --accent-glow and --float-shadow are inert names
// now (tokens.test.mjs rule 12 forbids painting with them), so a pack that
// still declares one compiles and shows nothing.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { contrast, colorsIn, over } from "../src/lib/contrast.mjs";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const PACKS = path.join(HERE, "..", "src", "themepacks.css");
const APP = fs.readFileSync(path.join(HERE, "..", "src", "app.css"), "utf8");
const WRITE = process.argv.includes("--write");
const TEXT_FLOOR = 4.6;
const EDGE_FLOOR = 3.1;

const stripComments = (s) => s.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " "));
let css = fs.readFileSync(PACKS, "utf8");
const clean = stripComments(css);
const rootBlock = APP.slice(APP.indexOf(":root {"), APP.indexOf("\n}", APP.indexOf(":root {")));

// Every palette block, in order, with its offsets (comments blanked, so the
// offsets are valid in the original text too).
const blocks = [];
for (const m of clean.matchAll(/:root\[data-theme-pack="([^"]+)"\]\s*\{([^}]*)\}/g)) {
  blocks.push({ pack: m[1], start: m.index, body: m[2], bodyAt: m.index + m[0].indexOf("{") + 1 });
}
const packs = [...new Set(blocks.map((b) => b.pack))];

// The cascade's answer for a token in one pack: its LAST declaration wins,
// falling back to the default dark :root, following var() references.
function raw(pack, name) {
  let hit = null;
  for (const b of blocks) {
    if (b.pack !== pack) continue;
    for (const m of b.body.matchAll(new RegExp(`(?:^|[;\\s])${name}:\\s*([^;]+);`, "g"))) hit = m[1].trim();
  }
  if (hit) return hit;
  const r = new RegExp(`^\\s*${name}:\\s*([^;]+);`, "m").exec(rootBlock);
  return r ? r[1].trim() : null;
}
function resolve(pack, name, depth = 0) {
  const v = raw(pack, name);
  if (!v || depth > 6) return null;
  const ref = /^var\((--[\w-]+)\)$/.exec(v);
  if (ref) return resolve(pack, ref[1], depth + 1);
  return colorsIn(v)[0] || null;
}

const hex = (rgb) => "#" + rgb.map((c) => Math.round(c).toString(16).padStart(2, "0")).join("");
const mix = (a, b, t) => a.map((c, i) => c + (b[i] - c) * t);
const worst = (ink, grounds) => Math.min(...grounds.map((g) => contrast(over(ink, g), g)));

// The last declaration of `name` in `pack`, as [absolute offset, length].
function lastDecl(pack, name) {
  let at = null;
  for (const b of blocks) {
    if (b.pack !== pack) continue;
    for (const m of b.body.matchAll(new RegExp(`${name}:\\s*([^;]+);`, "g"))) {
      at = { b, from: b.bodyAt + m.index, len: m[0].length };
    }
  }
  return at;
}

const edits = []; // { at, len, text } against the original css
const report = [];
let short = 0;
for (const pack of packs) {
  const elevated = resolve(pack, "--bg-elevated");
  const E = over(elevated, [0, 0, 0]);
  const grounds = [E, ...["--bg-0", "--bg-1", "--bg-2", "--bg-3"].map((n) => over(resolve(pack, n), E))];
  const text = over(resolve(pack, "--text"), E);
  const row = { pack };
  for (const tier of ["--text-muted", "--text-faint"]) {
    const ink = resolve(pack, tier);
    const before = worst(ink, grounds);
    row[tier] = before.toFixed(2);
    if (before >= 4.5) continue;
    short++;
    let lo = 0;
    let hi = 1;
    const base = over(ink, E);
    for (let i = 0; i < 24; i++) {
      const mid = (lo + hi) / 2;
      if (worst([...mix(base, text, mid), 1], grounds) >= TEXT_FLOOR) hi = mid;
      else lo = mid;
    }
    const next = hex(mix(base, text, hi));
    row[tier] += ` → ${worst(colorsIn(next)[0], grounds).toFixed(2)} (${raw(pack, tier)} → ${next})`;
    const d = lastDecl(pack, tier);
    if (d) edits.push({ at: d.from, len: d.len, text: `${tier}: ${next};` });
  }
  if (!blocks.some((b) => b.pack === pack && /--border-strong:/.test(b.body))) {
    const muted = over(resolve(pack, "--text-muted"), E);
    const bg2 = grounds[3];
    let lo = 0;
    let hi = 1;
    for (let i = 0; i < 24; i++) {
      const mid = (lo + hi) / 2;
      if (worst([...mix(muted, bg2, mid), 1], grounds) >= EDGE_FLOOR) lo = mid;
      else hi = mid;
    }
    const edge = hex(mix(muted, bg2, lo));
    row["--border-strong"] = `${edge} (${worst(colorsIn(edge)[0], grounds).toFixed(2)})`;
    // Beside the pack's --text-faint, in the same block, so the palette reads
    // as one list.
    const d = lastDecl(pack, "--text-faint") || lastDecl(pack, "--border");
    if (d) edits.push({ at: d.from + d.len, len: 0, text: `\n  --border-strong: ${edge};` });
  }
  report.push(row);
}

for (const r of report) {
  const parts = [r.pack.padEnd(12), "muted " + r["--text-muted"], "faint " + r["--text-faint"]];
  if (r["--border-strong"]) parts.push("edge " + r["--border-strong"]);
  console.log(parts.join("  "));
}
console.log(`${packs.length} packs, ${short} tier(s) under 4.5`);

if (WRITE) {
  for (const e of edits.sort((a, b) => b.at - a.at)) css = css.slice(0, e.at) + e.text + css.slice(e.at + e.len);
  fs.writeFileSync(PACKS, css);
  console.log(`wrote ${edits.length} change(s) to src/themepacks.css`);
} else if (short) {
  process.exit(1);
}
