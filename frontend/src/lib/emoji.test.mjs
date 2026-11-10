// The generated Unicode table, and the promise that it does not move anything
// the curated 379 already meant.
import { EMOJI_TABLE } from "./emojitable.js";
import {
  installFullEmoji, emojiCategories, emojiChar, emojiTonable, emojiName,
  searchEmoji, replaceShortcodes, EMOJI, TONABLE,
} from "./emoji.js";

let failures = 0;
const ok = (cond, msg) => {
  if (!cond) {
    console.error("  FAIL " + msg);
    failures++;
  }
};

// Before installing: the curated set is what answers.
ok(emojiCategories().length === 5, "five curated categories before install");
ok(searchEmoji("saudi", 8).length === 0, "no flags before install");

const full = installFullEmoji(EMOJI_TABLE);

ok(emojiCategories().length === 9, `nine Unicode groups, got ${emojiCategories().length}`);
ok(
  emojiCategories().map((g) => g.key).join(",") ===
    "smileys,people,nature,food,travel,activity,objects,symbols,flags",
  "groups in Unicode's own order",
);
ok(full.items.length > 1500, `table has ${full.items.length} entries`);

// Every category name resolves to a char, and every char is a real emoji.
let bad = 0;
for (const g of emojiCategories()) for (const n of g.names) if (!emojiChar(n)) bad++;
ok(bad === 0, `${bad} category names resolve to nothing`);

// THE invariant: a curated shortcode keeps its curated character. This is what
// stops an old `:tongue:` (👅 in gemoji, 😛 here) changing meaning under people.
let moved = [];
for (const [name, char] of Object.entries(EMOJI)) {
  if (emojiChar(name) !== char) moved.push(name);
}
ok(moved.length === 0, `curated shortcodes moved: ${moved.slice(0, 8).join(", ")}`);

// …and replaceShortcodes agrees with it.
ok(replaceShortcodes(":fire: :tongue:") === `${EMOJI.fire} ${EMOJI.tongue}`, "curated codes still expand");
ok(replaceShortcodes(":not_a_real_emoji_name:") === ":not_a_real_emoji_name:", "unknown codes untouched");

// The four searches the picker was measurably failing.
const hits = (q) => searchEmoji(q, 12).map(([, c]) => c);
ok(hits("saudi").includes("🇸🇦"), "search: saudi -> the flag");
ok(hits("taco").includes("🌮"), "search: taco");
ok(hits("birthday").includes("🎂"), "search: birthday cake");
ok(hits("family").length > 0, "search: family");


// The obvious emoji for the obvious word comes first, with the full table in.
const first = (q) => searchEmoji(q, 8)[0]?.[1];
ok(first("wave") === "👋", `:wave -> 👋, got ${first("wave")}`);
ok(first("smile") === "😄", `:smile -> 😄, got ${first("smile")}`);
ok(first("heart") === "❤️", `:heart -> ❤️, got ${first("heart")}`);
ok(first("+1") === "👍", `:+1 -> 👍, got ${first("+1")}`);
ok(first("thumbs")?.startsWith("👍"), `:thumbs -> 👍, got ${first("thumbs")}`);
ok(first("joy") === "😂", `:joy -> 😂, got ${first("joy")}`);
ok(hits("wave").includes("🌊"), "water_wave is still found, just not first");
// One entry per character.
const chars = searchEmoji("smile", 20).map(([, c]) => c);
ok(new Set(chars).size === chars.length, `duplicate characters in results: ${chars.join(" ")}`);
// A prefix outranks a match in the middle of a word.
const lan = searchEmoji("lan", 8).map(([n]) => n);
ok(lan.every((n, i) => i === 0 || !n.startsWith("lan") || lan[i - 1].startsWith("lan")), `prefix hits first: ${lan}`);
// The guild's own emoji, by prefix, ahead of Unicode names that merely contain it.
const custom = { lantern: "data:image/png;base64,AAAA", spark: "data:image/png;base64,BBBB" };
const withCustom = searchEmoji("lan", 8, custom);
ok(withCustom[0][0] === "lantern" && withCustom[0][1] === ":lantern:" && withCustom[0][2] === custom.lantern, `custom first: ${JSON.stringify(withCustom[0])}`);
ok(searchEmoji("spark", 8, custom)[0][1] === ":spark:", "an exact custom name wins");
ok(!searchEmoji("zzz", 8, custom).some(([n]) => n === "lantern"), "custom emoji only when they match");

// Names round-trip, curated first.
ok(emojiName(EMOJI.fire) === "fire", "emojiName prefers the curated name");
ok(emojiName("🇸🇦") !== "", "emojiName knows a generated char");
ok(emojiName("👍🏾") === "thumbsup", "emojiName strips a tone modifier");

// Tone-ability comes from Unicode now, and still covers everything the curated
// list hand-picked.
let lost = [...TONABLE].filter((n) => !emojiTonable(n));
ok(lost.length === 0, `curated tonable names lost the tone: ${lost.join(", ")}`);
ok(emojiTonable(emojiName("👋")), "a hand takes a tone");
ok(!emojiTonable(emojiName("🌮")), "a taco does not");

// No duplicate shortcodes: the grid uses the name as a key.
const seen = new Set();
let dupes = 0;
for (const e of full.items) {
  if (seen.has(e.n)) dupes++;
  seen.add(e.n);
}
ok(dupes === 0, `${dupes} duplicate shortcodes in the generated table`);

if (failures) {
  console.error(`emoji.test.mjs: ${failures} failure(s)`);
  process.exit(1);
}
console.log(
  `emoji: ok (${full.items.length} emoji, ${emojiCategories().length} groups, ` +
    `${full.tonable.size} tone-capable, ${Object.keys(EMOJI).length} curated codes intact)`,
);
