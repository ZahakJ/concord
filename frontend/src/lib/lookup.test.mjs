// Id lookups that take a string from elsewhere — a profile, a guild record, a
// message — answer only for the ids their table defines. One block per kind
// of input. (`npm test` runs this.)
import { table, own, at } from "./lookup.js";
import { presetOf, BANNER_BY_ID } from "./banners.js";
import { guildPresetOf, guildBannerArt, GUILD_BANNER_BY_ID } from "./guildbanners.js";
import { renderMarkdown } from "./markdown.js";
import { emojiChar, replaceShortcodes } from "./emoji.js";
import { parseGame, encodeGame } from "./games.js";
import { fxEffect, encodeFx } from "./fxtoken.js";
import { resolve as memeResolve } from "./meme.js";
import { tableFor } from "./langs.js";
import { isBuiltinSfx } from "./sounds.js";

let failures = 0;
const ok = (cond, msg) => {
  if (!cond) {
    console.error("  FAIL " + msg);
    failures++;
  }
};
const quietly = (fn) => {
  try {
    return { v: fn() };
  } catch (e) {
    return { err: e };
  }
};

// Names every plain object answers for, and their lowercase forms (several
// inputs are lowercased before the lookup).
const NAMES = [
  "constructor",
  "__proto__",
  "toString",
  "valueOf",
  "hasOwnProperty",
  "isPrototypeOf",
  "propertyIsEnumerable",
  "toLocaleString",
  "__defineGetter__",
  "__lookupGetter__",
];
const PROBES = [...NAMES, ...NAMES.map((n) => n.toLowerCase())];

// ---- the helpers ----------------------------------------------------------
{
  const t = table({ a: 1 });
  for (const n of PROBES) ok(t[n] === undefined, `table() answers for "${n}"`);
  ok(t.a === 1, "table() keeps its entries");
  const plain = { a: 1 };
  for (const n of PROBES) ok(own(plain, n) === undefined, `own() answers for "${n}"`);
  ok(own(plain, "a") === 1 && own(plain, 1) === undefined && own(null, "a") === undefined, "own() basics");
  ok(at([5, 6], 1) === 6 && at([5, 6], "1") === undefined && at([5, 6], "length") === undefined, "at() takes integers only");
}

// ---- profiles: banners -----------------------------------------------------
for (const n of PROBES) {
  ok(BANNER_BY_ID[n] === undefined, `the profile banner table answers for "${n}"`);
  ok(presetOf(`preset:${n}`) === null || presetOf(`preset:${n}`) === undefined, `profile banner "${n}"`);
}

// ---- guild records: guild banners ------------------------------------------
for (const n of PROBES) {
  ok(GUILD_BANNER_BY_ID[n] === undefined, `the guild banner table answers for "${n}"`);
  ok(!guildPresetOf(`preset:${n}`), `guildPresetOf("${n}")`);
  const g = quietly(() => guildBannerArt(`preset:${n}`));
  ok(!g.err, `guildBannerArt("${n}") throws: ${g.err?.message}`);
}

// ---- messages: custom emoji, colour names, shortcodes ----------------------
{
  const custom = { party: "data:image/png;base64,AAAA" };
  for (const n of PROBES) {
    if (!/^[a-z0-9_]{2,32}$/.test(n)) continue;
    const r = quietly(() => renderMarkdown(`hi :${n}: there`, [], custom));
    ok(!r.err, `a message with :${n}: throws while rendering: ${r.err?.message}`);
    ok(r.v && !r.v.includes("<img") && !r.v.includes("function"), `:${n}: must stay text, got ${r.v}`);
    ok(emojiChar(n) === "", `emojiChar("${n}")`);
    ok(replaceShortcodes(`:${n}:`) === `:${n}:`, `replaceShortcodes(":${n}:")`);
  }
  const ok1 = renderMarkdown("hi :party:", [], custom);
  ok(ok1.includes('class="cemoji"'), "a real custom emoji still renders");
  for (const n of PROBES) {
    if (!/^[a-z]{3,10}$/.test(n)) continue;
    const r = quietly(() => renderMarkdown(`{${n}|text}`));
    ok(!r.err && !r.v.includes("style="), `colour name "${n}"`);
  }
  ok(renderMarkdown("{red|text}").includes("style="), "a real colour name still applies");
}

// ---- messages: game tokens, effect tokens, meme recipes, code languages ----
for (const n of PROBES) {
  const g = quietly(() => parseGame(encodeGame({ k: "new", g: n, i: "abcd1234" })));
  ok(!g.err && g.v === null, `a game token for "${n}" must be refused`);
  ok(fxEffect(encodeFx(n)) === "", `an effect token for "${n}" must be refused`);
  const m = quietly(() => memeResolve({ style: n, font: n }));
  ok(!m.err && typeof m.v.family === "string", `meme style/font "${n}" must fall back`);
  ok(tableFor(n) === null, `code language "${n}" must be plain text`);
}

// ---- sound triggers ----------------------------------------------------------
for (const n of PROBES) ok(!isBuiltinSfx(n), `a sound trigger "${n}" is not a built-in sound`);

if (failures) {
  console.error(`lookup: ${failures} failure(s)`);
  process.exit(1);
}
console.log(`lookup: ${PROBES.length} names refused by every table`);
