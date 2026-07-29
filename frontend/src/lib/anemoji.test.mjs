// The animated-emoji pack is looked up by twemoji code at render time, but the
// files are named by the build script. If those two ever disagree the feature
// doesn't break loudly — every emoji just silently stops animating, which is
// indistinguishable from "no pack installed". This pins them together.
import { readFileSync, existsSync, readdirSync } from "node:fs";
import { twemojiCode } from "./markdown.js";

const DIR = new URL("../../public/anemoji/", import.meta.url).pathname;
let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

if (!existsSync(`${DIR}manifest.json`)) {
  // A build without the pack is a supported configuration: everything falls
  // back to the static set.
  console.log("anemoji: skipped (no pack in this checkout)");
} else {
  const manifest = JSON.parse(readFileSync(`${DIR}manifest.json`, "utf8"));
  assert(Array.isArray(manifest) && manifest.length > 0, "the manifest lists something");

  // Every code in the manifest must have a file, or the img 404s.
  for (const code of manifest) {
    assert(existsSync(`${DIR}${code}.webp`), `${code} is listed but ${code}.webp is missing`);
  }
  // ...and every file must be listed, or it's dead weight in the bundle.
  const files = readdirSync(DIR)
    .filter((f) => f.endsWith(".webp"))
    .map((f) => f.replace(/\.webp$/, ""));
  const listed = new Set(manifest);
  for (const f of files) assert(listed.has(f), `${f}.webp is shipped but not in the manifest`);

  // The actual contract: the renderer asks for twemojiCode(emoji), so that
  // exact string has to be what the file is called. Spot-checked on the shapes
  // most likely to diverge — a plain emoji, one with a variation selector
  // (twemoji drops FE0F, Noto often keeps it), and a multi-codepoint one.
  const cases = [
    ["😂", "1f602"],
    ["❤️", "2764"],
    ["👍", "1f44d"],
    ["✅", "2705"],
  ];
  for (const [emoji, want] of cases) {
    assert(twemojiCode(emoji) === want, `twemojiCode(${emoji}) should be ${want}, got ${twemojiCode(emoji)}`);
    assert(listed.has(want), `${emoji} (${want}) should be in the animated pack`);
  }

  if (failures) {
    console.error(`\n${failures} anemoji test(s) failed`);
    process.exit(1);
  }
  console.log(`anemoji: all tests passed (${manifest.length} animated)`);
}
