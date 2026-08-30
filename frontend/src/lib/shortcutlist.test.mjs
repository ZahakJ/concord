// The cheat sheet must describe the keymap, and go on describing it.
//
// modals/ModalShortcuts.svelte now renders lib/shortcuts.js's SHORTCUTS array,
// so those two cannot disagree by construction. What they CAN still do is fall
// behind the handlers: a new binding is added to the keydown code and nobody
// remembers there is a list. That is exactly how the sheet lost nine entries,
// `?` among them, so the one screen whose job is teaching shortcuts could not
// teach how to reopen itself.
//
// So this reads the handlers as text, pulls out every key literal they compare
// against, and insists the registry knows about each one. It is a lint, not a
// proof — it cannot tell that a label is ACCURATE — but it catches the failure
// that actually happened.
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const src = (p) => readFileSync(join(here, p), "utf8");

let failures = 0;
const fail = (msg) => {
  console.error("  FAIL: " + msg);
  failures++;
};

// ---- the registry, parsed out of the module ----
//
// Importing it would drag in state.svelte.js and the whole rune runtime, which
// plain node cannot run. The array is a literal, so read it as one.
const shortcutsSrc = src("shortcuts.js");
const listBody = shortcutsSrc.slice(
  shortcutsSrc.indexOf("export const SHORTCUTS = ["),
  shortcutsSrc.indexOf("export const SHORTCUT_GROUPS"),
);
const SHORTCUTS = JSON.parse(
  "[" +
    listBody
      .slice(listBody.indexOf("[") + 1, listBody.lastIndexOf("]"))
      // { group: "x" } -> { "group": "x" }. Anchored to the four field names
      // after a brace or comma, so a colon inside a LABEL is left alone.
      .replace(/([{,]\s*)(group|chords|label|typed):/g, '$1"$2":')
      .replace(/,\s*\]/g, "]")
      .replace(/,\s*\}/g, "}")
      .trim()
      .replace(/,$/, "") +
    "]",
);

if (SHORTCUTS.length < 30) fail(`only parsed ${SHORTCUTS.length} entries — the reader is broken`);

// Every printable token the registry mentions, lower-cased.
const known = new Set();
for (const s of SHORTCUTS) {
  if (!s.group || !s.label || !Array.isArray(s.chords)) fail(`malformed entry: ${JSON.stringify(s)}`);
  for (const chord of s.chords) for (const k of chord) known.add(String(k).toLowerCase());
}

// ---- what the handlers actually answer ----
//
// `e.key === "x"`, `e.key.toLowerCase() === "x"` and `k === "x"` where k is a
// lower-cased e.key. Everything else (e.code, e.repeat, modifiers) is not a
// printable token and has nothing to appear in a list as.
function keysUsedIn(file) {
  const text = src(file);
  const out = new Set();
  const re = /(?:e\.key(?:\.toLowerCase\(\))?|\bk)\s*===\s*"([^"]+)"/g;
  let m;
  while ((m = re.exec(text))) out.add(m[1].toLowerCase());
  return out;
}

// Tokens the registry spells differently from the DOM, or deliberately omits.
const SPELLED = {
  arrowup: "↑",
  arrowdown: "↓",
  escape: "esc",
  "+": "=", // the same physical key; the sheet shows the unshifted legend
  "<": ",", // Ctrl+Shift+, reports "<" on a US layout
  ">": ".", // Ctrl+Shift+. likewise
  tab: null, // list navigation inside a popup, not a global binding
  " ": null,
};

const scope = [
  ["shortcuts.js", keysUsedIn("shortcuts.js")],
  ["../Composer.svelte", keysUsedIn("../Composer.svelte")],
  // The formatting chords moved out of the composer and into the module the
  // composer and the edit box now share. They are still bindings the sheet has
  // to describe, so the reader follows them.
  ["mdformat.js", keysUsedIn("mdformat.js")],
];

for (const [file, keys] of scope) {
  for (const raw of keys) {
    const mapped = raw in SPELLED ? SPELLED[raw] : raw;
    if (mapped === null) continue;
    if (!known.has(mapped)) {
      fail(`${file} answers "${raw}" but SHORTCUTS never mentions it (looked for "${mapped}")`);
    }
  }
}

// Sanity: the entries the sheet lost once, by name.
for (const label of [
  "Member panel",
  "Deafen",
  "This list",
]) {
  if (!SHORTCUTS.some((s) => s.label === label)) fail(`SHORTCUTS is missing "${label}"`);
}
// `?` reopens the sheet — the entry whose absence was the joke.
if (!known.has("?")) fail("SHORTCUTS never mentions `?`, which is how this sheet is opened");

// Groups must be contiguous, because the sheet renders them in registry order
// and a stray entry would appear under a heading it does not belong to.
const seen = [];
for (const s of SHORTCUTS) {
  if (seen[seen.length - 1] !== s.group) {
    if (seen.includes(s.group)) fail(`group "${s.group}" is split across the registry`);
    seen.push(s.group);
  }
}

if (failures) {
  console.error(`shortcutlist: ${failures} failure(s)`);
  process.exit(1);
}
console.log(
  `shortcutlist: ok (${SHORTCUTS.length} entries, ${seen.length} groups, ${known.size} distinct keys)`,
);
