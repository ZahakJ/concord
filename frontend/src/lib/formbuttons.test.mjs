// Every button that ends up inside a <form> says what kind of button it is.
//
// A <button> with no `type` is a SUBMIT button, and a form's submit handler
// does not care which button it was. The dropdown trigger in Menu.svelte had
// no type, so opening the composer's "+" menu posted the half-written draft to
// the channel, and opening the Category picker in the create-channel dialog
// created the channel on the spot, uncategorised. Neither call site wrote a
// button: the button came from inside a shared component the form rendered.
//
// So this walks every <form> in the source, and every component it renders —
// recursively, because Select renders Menu renders the trigger — and fails on
// a <button> that leaves its type to the browser. A real submit button says
// type="submit"; anything else says type="button".
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const SRC = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function walk(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) walk(p, out);
    else if (e.name.endsWith(".svelte")) out.push(p);
  }
  return out;
}

const files = walk(SRC);
const byName = new Map(files.map((p) => [path.basename(p, ".svelte"), p]));
const src = new Map(files.map((p) => [p, fs.readFileSync(p, "utf8")]));

// Markup only: scripts and styles can mention "<button" in a comment.
const markup = (text) =>
  text.replace(/<script[\s\S]*?<\/script>/g, "").replace(/<style[\s\S]*?<\/style>/g, "").replace(/<!--[\s\S]*?-->/g, "");

// The opening tags of every <button …> in a chunk of markup. Attribute values
// can hold `>` inside `{…}` expressions, so the tag ends at the first `>` that
// is outside braces and quotes.
function openTags(text, name) {
  const out = [];
  const re = new RegExp(`<${name}[\\s>]`, "g");
  let m;
  while ((m = re.exec(text))) {
    let depth = 0;
    let quote = "";
    let i = m.index + 1;
    for (; i < text.length; i++) {
      const c = text[i];
      if (quote) {
        if (c === quote) quote = "";
      } else if (c === '"' || c === "'") quote = c;
      else if (c === "{") depth++;
      else if (c === "}") depth--;
      else if (c === ">" && depth === 0) break;
    }
    out.push({ tag: text.slice(m.index, i + 1), at: m.index });
  }
  return out;
}

// Components a chunk of markup renders: capitalised tags that resolve to a file.
const componentsIn = (text) =>
  [...text.matchAll(/<([A-Z][A-Za-z0-9]*)[\s/>]/g)].map((m) => m[1]).filter((n) => byName.has(n));

const failures = [];
const seen = new Set();

function checkChunk(file, text, via) {
  for (const { tag } of openTags(text, "button")) {
    if (/\stype=/.test(tag)) continue;
    failures.push(`${path.relative(SRC, file)} (${via}): ${tag.replace(/\s+/g, " ").slice(0, 100)}`);
  }
  for (const name of componentsIn(text)) {
    const p = byName.get(name);
    if (seen.has(p)) continue;
    seen.add(p);
    checkChunk(p, markup(src.get(p)), `${via} > ${name}`);
  }
}

let forms = 0;
for (const [file, text] of src) {
  const m = markup(text);
  const re = /<form[\s>]/g;
  let f;
  while ((f = re.exec(m))) {
    const end = m.indexOf("</form>", f.index);
    if (end < 0) continue;
    forms++;
    checkChunk(file, m.slice(f.index, end), `<form> in ${path.basename(file)}`);
  }
}

if (forms < 5) failures.push(`expected to find the app's forms, found ${forms} — has the scan broken?`);
if (!seen.has(byName.get("Menu"))) failures.push("Menu.svelte is rendered inside a form and was not scanned");

if (failures.length) {
  console.error(`formbuttons: ${failures.length} button(s) inside a form without a type:`);
  for (const f of failures) console.error("  FAIL " + f);
  process.exit(1);
}
console.log(`formbuttons: ${forms} forms, ${seen.size} components reached, every button typed`);
