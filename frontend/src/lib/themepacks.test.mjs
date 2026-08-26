// The theme-pack stylesheet split, checked (`npm test` runs it).
//
// The fifty pack palettes live in src/themepacks.css and are fetched only when
// somebody has chosen one. That saves a fifth of the boot stylesheet, and it
// costs one invariant: themepacks.css now loads AFTER app.css, so any rule in
// app.css that used to beat a pack rule by sitting later in the same file no
// longer does.
//
// There were thirteen of those — the shape, typeface and density overrides —
// and they are now written `:root:root[...]` so they win on specificity
// instead. This test is what stops the next one being written the old way and
// only being discovered by somebody whose "Sharp corners" quietly stopped
// working on one pack out of fifty.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const SRC = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const app = fs.readFileSync(path.join(SRC, "app.css"), "utf8");
const packs = fs.readFileSync(path.join(SRC, "themepacks.css"), "utf8");

let failures = 0;
const fail = (msg) => {
  console.error("  FAIL " + msg);
  failures++;
};

// Top-level rules, as (selectorList, body) with comments removed.
function rules(css) {
  const out = [];
  let i = 0;
  let start = 0;
  while (i < css.length) {
    const c = css[i];
    if (c === "/" && css[i + 1] === "*") {
      i = css.indexOf("*/", i) + 2;
      continue;
    }
    if (c === "{") {
      let depth = 1;
      let j = i + 1;
      while (j < css.length && depth) {
        if (css[j] === "/" && css[j + 1] === "*") {
          j = css.indexOf("*/", j) + 2;
          continue;
        }
        if (css[j] === "{") depth++;
        else if (css[j] === "}") depth--;
        j++;
      }
      const head = css.slice(start, i).replace(/\/\*[\s\S]*?\*\//g, "").trim();
      out.push({ head, body: css.slice(i + 1, j - 1), at: start });
      i = j;
      start = j;
      continue;
    }
    i++;
  }
  return out;
}

const isPack = (s) => /data-theme-pack\s*=/.test(s);
const selectors = (head) => head.split(",").map((s) => s.trim()).filter(Boolean);
// Specificity, ids/classes only — everything here is :root-anchored, so the
// element count is always zero.
function spec(sel) {
  const ids = (sel.match(/(?<![\w-])#[\w-]+/g) || []).length;
  const cls =
    (sel.match(/\.[\w-]+/g) || []).length +
    (sel.match(/\[[^\]]+\]/g) || []).length +
    (sel.match(/:(?!:)[\w-]+/g) || []).length;
  return `${ids}.${cls}`;
}
const props = (body) =>
  new Set(
    [...body.matchAll(/(?:^|[;{])\s*(--?[\w-]+)\s*:\s*([^;}]*)/g)]
      .filter((m) => !/!important/.test(m[2]))
      .map((m) => m[1]),
  );
// The part of the selector after the :root[...] prefix — two rules can only
// fight over an element if they end up pointing at the same one.
const tail = (sel) => sel.replace(/^:root(:root)?(\[[^\]]*\])*/, "").trim();

// 1. No pack-only rule left behind in app.css.
{
  const stragglers = rules(app).filter(
    (r) => !r.head.startsWith("@") && selectors(r.head).length && selectors(r.head).every(isPack),
  );
  if (stragglers.length)
    fail(
      `${stragglers.length} pack-only rule(s) still in app.css, e.g. ${stragglers[0].head.slice(0, 70)} — they belong in themepacks.css`,
    );
}

// 2. Nothing in app.css depends on coming after a pack rule.
//
// A tie is only a problem when app.css is the side that has to win. The base
// palettes — `:root` and `:root[data-theme="light"|"dark"]` — are exactly what a
// pack exists to replace, and they sat before the packs in the old single file
// too, so they lose now as they lost then. Every OTHER attribute on :root is a
// choice the person made on top of their pack, and those have to win.
{
  const packRules = rules(packs).filter((r) => !r.head.startsWith("@"));
  const appRules = rules(app).filter((r) => !r.head.startsWith("@"));
  const clashes = [];
  for (const p of packRules) {
    const pp = props(p.body);
    if (!pp.size) continue;
    for (const a of appRules) {
      const shared = [...props(a.body)].filter((x) => pp.has(x));
      if (!shared.length) continue;
      for (const ps of selectors(p.head)) {
        for (const as of selectors(a.head)) {
          if (isPack(as)) continue;
          if (!as.startsWith(":root")) continue;
          const attrs = as.match(/\[([\w-]+)/g) || [];
          if (attrs.every((a2) => a2 === "[data-theme")) continue;
          if (spec(ps) !== spec(as)) continue;
          if (tail(ps) !== tail(as)) continue;
          clashes.push(`${as.slice(0, 48)} vs ${ps.slice(0, 48)} over ${shared.slice(0, 3).join(", ")}`);
        }
      }
    }
  }
  const uniq = [...new Set(clashes)];
  if (uniq.length) {
    fail(`${uniq.length} rule(s) in app.css tie with a theme-pack rule and now lose:`);
    for (const c of uniq.slice(0, 8)) console.error("       " + c);
    console.error("       Give the app.css side a doubled :root so it wins on specificity.");
  }
}

// 3. The split is worth having.
{
  const share = packs.length / (app.length + packs.length);
  if (share < 0.2) fail(`themepacks.css is only ${(share * 100).toFixed(1)}% of the stylesheet — has it been merged back?`);
}

if (failures) {
  console.error(`themepacks: ${failures} failure(s)`);
  process.exit(1);
}
console.log(
  `themepacks: ok (app.css ${Math.round(app.length / 1024)}KB, themepacks.css ${Math.round(packs.length / 1024)}KB deferred)`,
);
