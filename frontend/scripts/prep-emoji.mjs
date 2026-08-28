// prep-emoji.mjs — build src/lib/emojitable.js from a Unicode emoji-test.txt.
//
// Run BY HAND, not as part of `npm run build`, and the output is COMMITTED:
//   node scripts/prep-emoji.mjs [path/to/emoji-test.txt]
//
// Same shape as prep-anemoji.mjs next door and for the same reason — this needs
// an input file that is not part of the repository, and the build must not.
//
// Why generate at all: the picker shipped 379 hand-curated shortcodes in five
// categories, which is the one place this app is visibly smaller than its
// peers. No flags, no professions, no families, and "taco" or "birthday cake"
// missed. Meanwhile lib/markdown.js already renders ANY Extended_Pictographic
// through the bundled Twemoji set, so the library was the gap and never the
// renderer.
//
// The source is Unicode's own emoji-test.txt: it carries the nine standard
// groups in their canonical order, the fully-qualified sequences, and the CLDR
// short name for every one of them. Names are the search keywords — "flag:
// Saudi Arabia" is what makes "saudi" hit — so nothing else has to be fetched
// and no image is involved anywhere. Native glyphs in the picker, Twemoji SVGs
// (already vendored under node_modules) in the message body.
//
// Two deliberate omissions:
//   * The Component group (skin-tone swatches, hair components). They are
//     modifiers, not emoji you send.
//   * Every explicit skin-tone sequence. The picker applies a tone across the
//     whole grid at render time with applyTone(), so shipping five copies of
//     every human would quintuple the table to say nothing new. Instead each
//     entry records WHETHER a tone sequence exists for it, which is what the
//     grid needs to know.
//
// Output format is one string, parsed at import. A JS object literal of ~1800
// entries costs several times its own content in punctuation and quoting; this
// is two tabs and a newline per emoji.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const OUT = path.join(HERE, "..", "src", "lib", "emojitable.js");

// Where a Linux box tends to have one. Any emoji-test.txt works; pass a path to
// use a newer one.
const CANDIDATES = [
  process.argv[2],
  "/usr/share/unicode/emoji/emoji-test.txt",
  "/usr/share/oh-my-zsh/plugins/emoji/emoji-data.txt",
].filter(Boolean);

const src = CANDIDATES.find((p) => fs.existsSync(p));
if (!src) {
  console.error("no emoji-test.txt found; tried:\n  " + CANDIDATES.join("\n  "));
  process.exit(1);
}

// Stable keys for the nine groups. The picker's tab ids are these, and four of
// them match the keys the old curated table used, so a saved "last tab" and the
// hardcoded "people" default both still resolve.
const GROUP_KEYS = {
  // Tab glyphs are picked to need no variation selector: ☺ and ✈ without one
  // fall back to a monochrome text presentation on several platforms, which in
  // a row of colour tabs reads as a missing icon.
  "Smileys & Emotion": ["smileys", "Smileys", "😀"],
  "People & Body": ["people", "People", "🧑"],
  "Animals & Nature": ["nature", "Nature", "🌿"],
  "Food & Drink": ["food", "Food", "🍔"],
  "Travel & Places": ["travel", "Travel", "🚗"],
  Activities: ["activity", "Activities", "⚽"],
  Objects: ["objects", "Objects", "💡"],
  Symbols: ["symbols", "Symbols", "🔣"],
  Flags: ["flags", "Flags", "🚩"],
};

const TONES = new Set(["1F3FB", "1F3FC", "1F3FD", "1F3FE", "1F3FF"]);

const lines = fs.readFileSync(src, "utf8").split("\n");
let group = "";
const out = []; // [{ group, char, name }]
const tonable = new Set(); // char of the BASE sequence, tone stripped

for (const line of lines) {
  const g = /^#\s*group:\s*(.+?)\s*$/.exec(line);
  if (g) {
    group = g[1];
    continue;
  }
  if (!line || line.startsWith("#")) continue;
  const m = /^([0-9A-F ]+?)\s*;\s*fully-qualified\s*#\s*(\S+)\s+(?:E\d+\.\d+\s+)?(.+?)\s*$/.exec(line);
  if (!m) continue;
  const codes = m[1].trim().split(/\s+/);
  const char = m[2];
  const name = m[3];

  const toned = codes.some((c) => TONES.has(c));
  if (toned) {
    // Record the base as tone-capable and drop the variant itself.
    tonable.add(codes.filter((c) => !TONES.has(c)).map((c) => String.fromCodePoint(parseInt(c, 16))).join(""));
    continue;
  }
  if (!GROUP_KEYS[group]) continue; // Component, and anything a newer file adds
  out.push({ group, char, name });
}

// A base sequence in the file carries a U+FE0F that the tone form drops, so the
// two do not match on a naive comparison. Compare with the selector stripped.
const bare = (s) => s.replace(/️/g, "");
const tonableBare = new Set([...tonable].map(bare));

const body = [];
let last = "";
let n = 0;
let toneCount = 0;
for (const e of out) {
  if (e.group !== last) {
    const [key, label, icon] = GROUP_KEYS[e.group];
    // A TAB, not a "#": the keycap emoji is literally "#\uFE0F\u20E3", so a
    // hash-prefixed header line is ambiguous with a real entry. Nothing this
    // file can emit starts with a tab.
    body.push(`\t${key}\t${label}\t${icon}`);
    last = e.group;
  }
  const t = tonableBare.has(bare(e.char)) ? "\t1" : "";
  if (t) toneCount++;
  body.push(`${e.char}\t${e.name}${t}`);
  n++;
}

const version = (/emoji\/(\d+\.\d+)\//.exec(fs.readFileSync(src, "utf8").slice(0, 400)) || [, "?"])[1];

const js = `// GENERATED by scripts/prep-emoji.mjs — do not edit by hand.
//
// Unicode emoji-test.txt ${version}: ${n} emoji in ${Object.keys(GROUP_KEYS).length} groups,
// ${toneCount} of them tone-capable. Skin-tone sequences and the Component
// group are excluded on purpose — see the generator's header.
//
// One string, not an object literal: at this size the punctuation and quoting
// of ~${n} object entries costs several times the content itself. Lines are
// "\\t<key>\\t<label>\\t<icon>" for a group header and "<char>\\t<name>[\\t1]"
// for an emoji, where the trailing 1 means a skin tone applies. lib/emoji.js
// parses it once, on the first read.
//
// This file is loaded through a dynamic import (lib/emojifull.svelte.js) so it
// lands in its own chunk and costs a cold start nothing.
export const EMOJI_TABLE = ${JSON.stringify(body.join("\n"))};
`;

fs.writeFileSync(OUT, js);
console.log(`wrote ${OUT}`);
console.log(`  source ${src} (emoji ${version})`);
console.log(`  ${n} emoji, ${toneCount} tone-capable, ${(js.length / 1024).toFixed(1)} KB of JS`);
