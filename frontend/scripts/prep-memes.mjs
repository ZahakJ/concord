// prep-memes.mjs — build the bundled meme template pack.
//
// Run by hand, NOT as part of `npm run build`: it needs the network and
// ImageMagick, and its output is committed.
//
//   node scripts/prep-memes.mjs
//
// Sources the well-known templates from imgflip's public catalogue and
// re-encodes them to WebP at 600px. See README.md in public/memes for the
// provenance and licensing position — these are third-party images and the pack
// is deliberately optional.
//
// It MERGES rather than overwrites. Entries already in manifest.json keep their
// hand-placed caption boxes and hand-written tags, because those were checked by
// rendering each template with sample text and looking at the result — the only
// way to catch a caption box sitting half off its panel. Regenerating must never
// silently undo that.

import { execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync, readFileSync, existsSync, statSync, readdirSync } from "node:fs";
import { join } from "node:path";

const OUT = new URL("../public/memes/", import.meta.url).pathname;
const TMP = "/tmp/meme-src";
const MAX_EDGE = 600;

// Words that carry no search value, plus the ones every second template has.
const STOP = new Set(["the", "a", "an", "of", "and", "for", "in", "is", "it", "to", "my", "your", "meme", "guy", "man"]);

// Search terms people actually type, which are almost never the title. Nobody
// looks up "Drake Hotline Bling"; they type "yes no". This is the difference
// between a gallery you can use and a wall of thumbnails.
const EXTRA = {
  "drake hotline bling": ["yes", "no", "prefer", "reject", "approve"],
  "two buttons": ["choice", "decide", "sweating", "dilemma", "hard choice"],
  "distracted boyfriend": ["cheating", "tempted", "looking", "distracted"],
  "uno draw 25 cards": ["refuse", "avoid", "rather", "draw 25"],
  "left exit 12 off ramp": ["swerve", "detour", "avoid", "car"],
  "anakin padme 4 panel": ["right", "for the better", "silence", "star wars"],
  "epic handshake": ["agree", "common ground", "both", "same"],
  "always has been": ["astronaut", "wait its all", "space", "betrayal"],
  "running away balloon": ["want", "held back", "obstacle", "reach"],
  "grus plan": ["backfire", "plan", "steps", "gru"],
  "gru's plan": ["backfire", "plan", "steps", "gru"],
  "waiting skeleton": ["still waiting", "forever", "skeleton", "wait"],
  "surprised pikachu": ["shocked", "consequences", "obvious", "pikachu"],
  "change my mind": ["debate", "sign", "opinion", "prove me wrong"],
  "one does not simply": ["boromir", "cannot", "lotr", "simply"],
  "batman slapping robin": ["slap", "shut up", "batman", "correction"],
  "expanding brain": ["galaxy brain", "smart", "levels", "enlightenment"],
  "this is fine": ["fire", "denial", "dog", "everything is fine"],
  "woman yelling at a cat": ["argue", "cat", "yelling", "confrontation"],
  "buff doge vs cheems": ["strong", "weak", "then vs now", "doge"],
  "panik kalm panik": ["panic", "calm", "relief", "worry"],
  "theyre the same picture": ["same", "no difference", "differences", "pam", "office"],
  "they're the same picture": ["same", "no difference", "differences", "pam", "office"],
  "success kid": ["win", "yes", "nailed it", "baby"],
  "ancient aliens": ["conspiracy", "therefore aliens", "history channel"],
  "roll safe think about it": ["smart", "cant lose", "clever", "big brain"],
  "mocking spongebob": ["mocking", "sarcasm", "spongebob", "imitate"],
  "sad pablo escobar": ["waiting", "bored", "lonely", "alone"],
  "bernie i am once again asking for your support": ["asking", "again", "please", "bernie"],
  "is this a pigeon": ["mistaken", "confused", "butterfly", "is this"],
  "trade offer": ["deal", "i receive", "you receive", "trade"],
  "monkey puppet": ["awkward", "side eye", "uh oh", "puppet"],
  "disaster girl": ["fire", "smirk", "chaos", "girl"],
  "hide the pain harold": ["pain", "smile", "harold", "suffering"],
  "third world skeptical kid": ["skeptical", "so you're telling me", "doubt"],
  "y u no": ["why", "angry", "y u no"],
  "the rock driving": ["rock", "shocked", "turn", "car"],
  "boardroom meeting suggestion": ["thrown out window", "meeting", "suggestion", "boardroom"],
  "inhaling seagull": ["scream", "shout", "seagull", "inhale"],
  "american chopper argument": ["argue", "shouting", "debate", "table"],
  "megamind peeking": ["no bitches", "megamind", "peek", "none"],
  "clown applying makeup": ["clown", "getting worse", "makeup", "denial"],
  "bike fall": ["self sabotage", "blame", "stick", "bike"],
  "sleeping shaq": ["awake", "asleep", "shaq", "wakes up"],
  "laughing leo": ["laughing", "pointing", "leonardo", "cheers"],
  "marked safe from": ["safe", "facebook", "marked safe"],
  "car salesman slaps roof": ["this bad boy", "salesman", "fit so much"],
  "blank nut button": ["press", "button", "nut", "smash"],
  "unsettled tom": ["suspicious", "tom", "cat", "unsettled"],
  "spongebob ight imma head out": ["leave", "head out", "spongebob", "nope"],
};

const stripArticle = (s) => s.toLowerCase().replace(/[^a-z0-9 ]+/g, " ").replace(/\s+/g, " ").trim();

function tagsFor(name) {
  const key = stripArticle(name);
  const words = key.split(" ").filter((w) => w.length > 2 && !STOP.has(w));
  return [...new Set([...words, ...(EXTRA[key] || [])])];
}

const sh = (args) => execFileSync("magick", args, { stdio: ["ignore", "pipe", "ignore"] });

mkdirSync(OUT, { recursive: true });
mkdirSync(TMP, { recursive: true });

const manifestPath = join(OUT, "manifest.json");
const existing = existsSync(manifestPath) ? JSON.parse(readFileSync(manifestPath, "utf8")) : [];
const byFile = new Map(existing.map((e) => [e.file, e]));

const catalogue = (await (await fetch("https://api.imgflip.com/get_memes")).json()).data.memes;

let added = 0;
let kept = 0;
for (const m of catalogue) {
  // imgflip's own id is not the filename: the URL basename is, and that is what
  // the existing hand-tuned entries are keyed on.
  const base = m.url.split("/").pop().replace(/\.[a-z]+$/i, "");
  const file = `${base}.webp`;
  if (byFile.has(file)) {
    // Never clobber a checked placement; only fill in tags if it has none.
    const e = byFile.get(file);
    if (!e.tags?.length) e.tags = tagsFor(m.name);
    kept++;
    continue;
  }
  const ext = m.url.split(".").pop().toLowerCase();
  const src = join(TMP, `${base}.${ext}`);
  const bytes = Buffer.from(await (await fetch(m.url)).arrayBuffer());
  if (bytes.length < 2000) continue; // a placeholder or an error page, not an image
  writeFileSync(src, bytes);
  try {
    sh([src, "-resize", `${MAX_EDGE}x${MAX_EDGE}>`, "-quality", "82", join(OUT, file)]);
  } catch {
    continue; // unconvertible source; skip rather than ship a broken tile
  }
  byFile.set(file, {
    file,
    label: m.name,
    tags: tagsFor(m.name),
    // No caption preset: the editor's default top-and-bottom is right for an
    // ordinary image macro, and inventing panel coordinates for a template
    // nobody has looked at would put boxes in the wrong place with confidence.
    // Presets are added by hand, after rendering and checking.
  });
  added++;
}

const merged = [...byFile.values()];
writeFileSync(manifestPath, JSON.stringify(merged, null, 2) + "\n");

const bytes = readdirSync(OUT)
  .filter((f) => f.endsWith(".webp"))
  .reduce((n, f) => n + statSync(join(OUT, f)).size, 0);
console.log(`templates: ${merged.length} total (${added} new, ${kept} kept), ${(bytes / 1048576).toFixed(2)} MB`);
