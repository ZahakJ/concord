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
import { contrast, colorsIn, over } from "./contrast.mjs";

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


// Count badges never read as an alarm (spec §5.10: red is never a count). A
// pack whose --accent hue is within 30° of its --danger hue must give the
// badge a neutral ground (--badge-bg), or every mention count on that pack is
// a red-looking dot.
{
  const plain = packs.replace(/\/\*[\s\S]*?\*\//g, "");
  const rgb = (h) => {
    h = h.replace("#", "");
    if (h.length === 3) h = [...h].map((c) => c + c).join("");
    return [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16) / 255);
  };
  const hueOf = ([r, g, b]) => {
    const mx = Math.max(r, g, b);
    const d = mx - Math.min(r, g, b);
    if (!d) return null;
    let h = mx === r ? ((g - b) / d) % 6 : mx === g ? (b - r) / d + 2 : (r - g) / d + 4;
    h *= 60;
    return h < 0 ? h + 360 : h;
  };
  const seen = {};
  for (const m of plain.matchAll(/:root\[data-theme-pack="([\w-]+)"\]\s*\{([^}]*)\}/g)) {
    const a = m[2].match(/--accent:\s*(#[0-9a-fA-F]{3,6})\b/);
    const d = m[2].match(/--danger:\s*(#[0-9a-fA-F]{3,6})\b/);
    const e = (seen[m[1]] ||= {});
    if (a) e.accent = a[1];
    if (d) e.danger = d[1];
  }
  const neutral = new Set();
  for (const r of rules(plain)) {
    if (!/--badge-bg\s*:/.test(r.body)) continue;
    for (const m of r.head.matchAll(/data-theme-pack="([\w-]+)"/g)) neutral.add(m[1]);
  }
  for (const [pack, v] of Object.entries(seen)) {
    if (!v.accent) continue;
    const ha = hueOf(rgb(v.accent));
    const hd = hueOf(rgb(v.danger || "#e5636a"));
    if (ha == null || hd == null) continue;
    const dist = Math.min(Math.abs(ha - hd), 360 - Math.abs(ha - hd));
    if (dist < 30 && !neutral.has(pack))
      fail(`${pack}: accent ${v.accent} is ${Math.round(dist)}° from danger — set --badge-bg so counts don't read as alarms`);
  }
}

// 5. A pack's text fields are legible under BOTH base themes.
//
// The base theme and the pack are two separate choices, and the light theme's
// block used to apply under every pack. It sets --bg-input to white and no dark
// pack sets its own, so Light + Midnight drew every field — composer, palette,
// dialog inputs — white, with Midnight's near-white ink typed into it at about
// 1.1:1. Measuring the packs on their own could never see that: it only exists
// in the cascade of the two stylesheets together. So this resolves the tokens
// the way the browser does, for every pack under each base theme, and measures
// what a field actually shows: typed text (--text) and its placeholder
// (--text-muted) on --bg-input, composited over the dialog surface it sits on.
{
  const STATE = fs.readFileSync(path.join(SRC, "lib/state.svelte.js"), "utf8");
  const setOf = (name) => {
    const m = STATE.match(new RegExp(`${name} = new Set\\(\\[([\\s\\S]*?)\\]\\)`));
    if (!m) throw new Error(`themepacks: cannot read ${name} from state.svelte.js`);
    return new Set([...m[1].matchAll(/"([\w-]+)"/g)].map((x) => x[1]));
  };
  const ANIMATED = setOf("ANIMATED_PACKS");
  const TEXTURED = setOf("TEXTURED_PACKS");

  // Split at top-level commas.
  const commaSplit = (s) => {
    const out = [];
    let d = 0;
    let st = 0;
    for (let i = 0; i < s.length; i++) {
      if (s[i] === "(") d++;
      else if (s[i] === ")") d--;
      else if (s[i] === "," && d === 0) {
        out.push(s.slice(st, i).trim());
        st = i + 1;
      }
    }
    out.push(s.slice(st).trim());
    return out;
  };
  // Does a compound selector made only of :root, [attr], [attr="v"], :not(),
  // :is() and :where() match a root carrying `attrs`? Returns { ok, spec }, or
  // null when the selector says anything else (a descendant, a class) — such a
  // rule does not set the root's tokens.
  const evalSel = (sel, attrs) => {
    let i = 0;
    let ok = true;
    let spec = 0;
    const s = sel.trim();
    while (i < s.length) {
      if (s.startsWith(":root", i)) {
        spec++;
        i += 5;
        continue;
      }
      if (s[i] === "[") {
        const end = s.indexOf("]", i);
        const m = /^\[([\w-]+)(?:="([^"]*)")?\]$/.exec(s.slice(i, end + 1));
        if (!m) return null;
        const v = attrs[m[1]];
        if (v === undefined || (m[2] !== undefined && v !== m[2])) ok = false;
        spec++;
        i = end + 1;
        continue;
      }
      const fn = /^:(not|is|where)\(/.exec(s.slice(i));
      if (fn) {
        let d = 1;
        let j = i + fn[0].length;
        while (j < s.length && d) {
          if (s[j] === "(") d++;
          else if (s[j] === ")") d--;
          j++;
        }
        const args = commaSplit(s.slice(i + fn[0].length, j - 1)).map((a) => evalSel(a, attrs));
        if (args.some((a) => a === null)) return null;
        const any = args.some((a) => a.ok);
        if (fn[1] === "not" ? any : !any) ok = false;
        if (fn[1] !== "where") spec += Math.max(...args.map((a) => a.spec));
        i = j;
        continue;
      }
      return null;
    }
    return { ok, spec };
  };
  const decls = (body) =>
    [...body.replace(/\/\*[\s\S]*?\*\//g, "").matchAll(/(?:^|[;{])\s*(--[\w-]+)\s*:\s*([^;}]*)/g)].map((m) => [
      m[1],
      m[2].trim(),
    ]);
  const sheet = [...rules(app), ...rules(packs)].filter((r) => !r.head.startsWith("@"));

  const tokensFor = (attrs) => {
    const hits = [];
    sheet.forEach((r, order) => {
      let best = null;
      for (const sel of selectors(r.head)) {
        const e = evalSel(sel, attrs);
        if (e && e.ok && (best === null || e.spec > best)) best = e.spec;
      }
      if (best !== null) hits.push({ spec: best, order, body: r.body });
    });
    hits.sort((a, b) => a.spec - b.spec || a.order - b.order);
    const tok = new Map();
    for (const h of hits) for (const [k, v] of decls(h.body)) tok.set(k, v);
    return tok;
  };
  // A token's colour as [r,g,b,a], following var() and the one color-mix
  // shape the token sheets use.
  const colour = (raw, tok, depth = 0) => {
    if (!raw || depth > 8) return null;
    raw = raw.trim();
    const ref = /^var\((--[\w-]+)(?:,\s*(.+))?\)$/.exec(raw);
    if (ref) return colour(tok.get(ref[1]) ?? ref[2], tok, depth + 1);
    const mix = /^color-mix\(in srgb,\s*(.+?)\s+([\d.]+)%,\s*(.+)\)$/.exec(raw);
    if (mix) {
      const a = colour(mix[1], tok, depth + 1);
      const b = colour(mix[3], tok, depth + 1);
      if (!a || !b) return null;
      const p = +mix[2] / 100;
      const al = a[3] * p + b[3] * (1 - p);
      if (!al) return [0, 0, 0, 0];
      return [0, 1, 2].map((k) => (a[k] * a[3] * p + b[k] * b[3] * (1 - p)) / al).concat(al);
    }
    return colorsIn(raw)[0] || null;
  };

  const packNames = new Set([...packs.matchAll(/data-theme-pack="([\w-]+)"/g)].map((m) => m[1]));
  let measured = 0;
  for (const pack of ["", ...packNames]) {
    for (const theme of ["dark", "light"]) {
      const attrs = { "data-theme": theme };
      if (pack) {
        attrs["data-theme-pack"] = pack;
        if (ANIMATED.has(pack)) attrs["data-anim-bg"] = "1";
        if (TEXTURED.has(pack)) attrs["data-textured"] = "1";
      }
      const tok = tokensFor(attrs);
      const get = (n) => colour(tok.get(n), tok);
      const where = `${pack || "no pack"} under the ${theme} theme`;
      const elevated = get("--bg-elevated");
      const input = get("--bg-input");
      const text = get("--text");
      const muted = get("--text-muted");
      if (!elevated || !input || !text || !muted) {
        fail(`fields: cannot resolve the field tokens for ${where}`);
        continue;
      }
      measured++;
      const ground = over(input, over(elevated, [0, 0, 0]));
      const t = contrast(over(text, ground), ground);
      const p = contrast(over(muted, ground), ground);
      if (t < 4.5) fail(`fields: typed text (--text on --bg-input) is ${t.toFixed(2)}:1 for ${where}`);
      if (p < 4.5) fail(`fields: placeholder (--text-muted on --bg-input) is ${p.toFixed(2)}:1 for ${where}`);
    }
  }
  if (measured < 80) fail(`fields: only ${measured} pack/theme pairs measured — the selectors have changed shape`);
}

if (failures) {
  console.error(`themepacks: ${failures} failure(s)`);
  process.exit(1);
}
console.log(
  `themepacks: ok (app.css ${Math.round(app.length / 1024)}KB, themepacks.css ${Math.round(packs.length / 1024)}KB deferred)`,
);
