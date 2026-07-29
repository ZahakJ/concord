// prep-anemoji.mjs — build the animated-emoji pack.
//
// Run by hand, NOT as part of `npm run build`: it needs the network and
// ImageMagick, and its output is committed. There is no npm package for an
// animated emoji set, so the sources come from Google's Noto Animated Emoji
// (CC BY 4.0) at fonts.gstatic.com.
//
//   node scripts/prep-anemoji.mjs
//
// Only a curated list is fetched. The full set is ~1,400 emoji and, even
// re-encoded, would add tens of megabytes to every build for emoji nobody
// sends. These ~150 cover the overwhelming bulk of real use; everything else
// keeps rendering as the static Twemoji already bundled.
//
// Each source is 512px and up to a hundred frames — 60 KB even after
// downscaling. Halving the frame rate (and doubling the frame delay to keep
// the timing) halves that again with no visible cost at emoji size.

import { execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync, rmSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { twemojiCode } from "../src/lib/markdown.js";

const OUT = new URL("../public/anemoji/", import.meta.url).pathname;
const TMP = "/tmp/anemoji-src";
const SIZE = 80;
const QUALITY = 38;

// Roughly usage-ranked. Anything Noto doesn't have is reported and skipped.
const EMOJI = [
  "😂","❤️","🤣","👍","😭","🙏","😘","🥰","😍","😊","🎉","😁","💕","🥺","😅","🔥","☺️","🤦","🤷","🙄",
  "😆","🤗","😉","🎂","🤔","👏","🙂","😳","🥳","😴","💜","🙈","🤞","😢","🤪","😔","👀","😋","😏","😒",
  "⭐","💖","😀","😃","😄","😎","😡","😱","🤩","🥵","🥶","😇","🙃","😜","😝","😤","😩","😫","🥱","😪",
  "😵","🤯","🤠","😈","👻","💀","👽","🤖","💩","👋","🤝","👌","✌️","🤟","🤘","👊","✊","👎","💪","🧠",
  "👑","💯","💥","✨","🌟","⚡","☀️","🌈","❄️","🌙","🍀","🌹","🌸","🎁","🎈","🎊","🏆","🥇","⚽","🏀",
  "🎮","🎧","🎵","🎸","📷","💻","📱","⏰","💰","💸","🎯","🚀","✈️","🚗","🍕","🍔","🍟","🌮","🍩","🍪",
  "🍫","🍎","🍌","🍉","☕","🍺","🍻","🥂","🍷","🐶","🐱","🐼","🦊","🐸","🐢","🦄","🐝","🦋","✅","❌",
  "⚠️","❓","❗","💤","👉","👈","😬","😐","😑","🥹","🫡","🤤","🤢","🥴","😷","🤒","🎃","🎄","🔔","📌",
];

// Noto names a file by its codepoints joined with "_", but is inconsistent
// about keeping the FE0F variation selector, so both spellings are tried.
function notoCandidates(seq) {
  const cps = [...seq].map((c) => c.codePointAt(0).toString(16));
  const withFe = cps.join("_");
  const without = cps.filter((c) => c !== "fe0f").join("_");
  return withFe === without ? [withFe] : [withFe, without];
}

async function fetchFirst(seq) {
  for (const name of notoCandidates(seq)) {
    const url = `https://fonts.gstatic.com/s/e/notoemoji/latest/${name}/512.webp`;
    const r = await fetch(url);
    if (r.ok) return Buffer.from(await r.arrayBuffer());
  }
  return null;
}

const sh = (args) => execFileSync("magick", args, { stdio: ["ignore", "pipe", "ignore"] }).toString();

mkdirSync(OUT, { recursive: true });
mkdirSync(TMP, { recursive: true });
for (const f of readdirSync(OUT)) if (f.endsWith(".webp")) rmSync(join(OUT, f));

const made = [];
const missing = [];
for (const seq of EMOJI) {
  const buf = await fetchFirst(seq);
  if (!buf) {
    missing.push(seq);
    continue;
  }
  const code = twemojiCode(seq); // named for the renderer's lookup, not Noto's
  const src = join(TMP, `${code}.webp`);
  writeFileSync(src, buf);
  const frames = sh(["identify", src]).trim().split("\n").length;
  const delay = parseInt(sh(["identify", "-format", "%T ", src]).trim().split(/\s+/)[0], 10) || 3;
  // Drop every other frame, then double the delay so the animation still runs
  // at its original speed — just at half the frame rate.
  const drop = [];
  for (let i = 1; i < frames; i += 2) drop.push(i);
  const dst = join(OUT, `${code}.webp`);
  sh([
    src, "-coalesce",
    ...(drop.length ? ["-delete", drop.join(",")] : []),
    "-resize", `${SIZE}x${SIZE}`,
    "-set", "delay", String(delay * 2),
    "-quality", String(QUALITY),
    "-define", "webp:method=6",
    dst,
  ]);
  made.push(code);
}

writeFileSync(join(OUT, "manifest.json"), JSON.stringify(made.sort()));
const bytes = readdirSync(OUT).reduce((n, f) => n + statSync(join(OUT, f)).size, 0);
rmSync(TMP, { recursive: true, force: true });
console.log(`animated emoji: ${made.length} built, ${(bytes / 1048576).toFixed(2)} MB`);
if (missing.length) console.log(`no Noto animation for: ${missing.join(" ")}`);
