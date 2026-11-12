// The design tokens, held in place (`npm test` runs it).
//
// A token system only works while people reach for it. Every one of the rules
// below is here because the codebase had already drifted past it once: 245
// literal border-radius values against 275 token uses, so the Appearance
// dialog's Shape setting was a no-op on roughly half the corners in the app;
// 164 literal font-sizes that the phone type scale could not reach; twenty-one
// distinct easing curves expressing two intents; four hundred spacing
// declarations spelling out a number a token already held.
//
// The migration is the easy half. This file is the half that lasts — it fails
// the build on a literal that is byte-identical to a token, which is the only
// kind of drift that is unambiguous. Values that are NOT on the scale are left
// alone on purpose: a rule saying "every number must be a token" would be
// wrong, and would be worked around within a week.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { contrast, colorsIn, over } from "./contrast.mjs";

const SRC = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

let failures = 0;
const fail = (msg) => {
  console.error("  FAIL " + msg);
  failures++;
};

// Every .svelte / .css file under src, plus the lib .js files that build CSS
// strings by hand (drag ghosts, the tooltip stylesheet, the sheet physics).
function walk(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) walk(p, out);
    else if (/\.(svelte|css)$/.test(e.name) || /^(sheet|tooltip)\.js$/.test(e.name)) out.push(p);
  }
  return out;
}
const FILES = walk(SRC).map((p) => [path.relative(SRC, p), fs.readFileSync(p, "utf8")]);
const APP = fs.readFileSync(path.join(SRC, "app.css"), "utf8");

// Strip comments before scanning: a comment quoting an old literal (there are
// several, explaining what a rule used to be) is documentation, not drift.
const strip = (s) =>
  s
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/<!--[\s\S]*?-->/g, "")
    // Line comments too — several explain what a value USED to be. The lookbehind
    // spares the // in a URL, which is the only other place a double slash lands.
    .replace(/(?<!:)\/\/[^\n]*/g, "");

// ---- 0. the tokens themselves exist ---------------------------------------
// Read the scale out of :root rather than hardcoding it here, so this file
// cannot disagree with the stylesheet it is policing. Retuning a token
// automatically retunes the rule.
function tokenValue(name) {
  const m = new RegExp(`^\\s*${name}:\\s*([^;]+);`, "m").exec(APP);
  return m ? m[1].trim() : null;
}
const NEEDED = [
  "--radius-sm", "--radius-md", "--radius-lg", "--radius-sheet",
  "--fs-micro", "--fs-tiny", "--fs-small", "--fs-compact", "--fs-ui", "--fs-body", "--fs-title", "--fs-display",
  "--sp-1", "--sp-2", "--sp-3", "--sp-4", "--sp-5", "--sp-6",
  "--ease-spring", "--ease-out", "--dur-quick", "--dur-standard",
  "--focus-ring", "--focus-ring-offset", "--scrim", "--bg-elevated",
  // The interface system's own few: control edges, the mention row tint, the
  // titles' face, the one curve allowed to overshoot, the shared header row.
  "--border-strong", "--mention-wash", "--display-font", "--ease-overshoot", "--header-h",
  // The motion vocabulary's other verbs and amounts (app.css › Motion).
  "--ease-lift", "--ease-in", "--dur-move", "--lift-y", "--lift-scale", "--current-scale", "--aside-x", "--press-scale", "--rise-y", "--enter-scale",
];
for (const t of NEEDED) if (!tokenValue(t)) fail(`app.css :root does not define ${t}`);

const byValue = (names) => {
  const m = new Map();
  for (const n of names) {
    const v = tokenValue(n);
    if (v && !m.has(v)) m.set(v, n);
  }
  return m;
};
const RADIUS = byValue(["--radius-sm", "--radius-md", "--radius-lg"]);
const FS = byValue(["--fs-micro", "--fs-tiny", "--fs-small", "--fs-compact", "--fs-ui", "--fs-body", "--fs-title", "--fs-display"]);
const SP = byValue(["--sp-1", "--sp-2", "--sp-3", "--sp-4", "--sp-5", "--sp-6"]);

// The :root blocks and the [data-shape] / phone blocks in app.css are where the
// scale is DEFINED; they are allowed to say 6px.
const isAppCss = (rel) => rel === "app.css" || rel === "themepacks.css";

// ---- 1. border-radius ------------------------------------------------------
// 999px (pills), 50% (circles) and hairline 1–3px corners stay literal: a pill
// is a shape, not a size, and a 2px corner on a 3px progress bar is geometry
// that a theme has no business restyling.
for (const [rel, raw] of FILES) {
  if (isAppCss(rel)) continue;
  for (const m of strip(raw).matchAll(/border-radius:\s*([0-9]+px)\s*;/g)) {
    const tok = RADIUS.get(m[1]);
    if (tok) fail(`${rel}: border-radius: ${m[1]} is var(${tok}) — the Shape setting cannot reach a literal`);
  }
}

// ---- 2. font-size ----------------------------------------------------------
// Byte-identical to a token, or under the legibility floor the type-scale
// comment in app.css sets for itself ("the bottom of the range (8–10px) is
// simply illegible"). One exemption is text drawn inside a fixed box small
// enough that the phone step would not fit: see the comment at the site.
//
// The create dialog's two are a DIAGRAM, not text: the starter-layout tiles
// draw the sidebar each template would build, at about a sixth of size, and a
// miniature that rescales with the phone type step is no longer a miniature.
// Nothing in it has to be read to use the dialog — the channel count under each
// tile says the same thing in real type.
const FS_FLOOR = 11;
const FS_EXEMPT = new Set([
  "modals/InfoDot.svelte:9.5px",
  "modals/ModalCreate.svelte:8px",
  "modals/ModalCreate.svelte:9.5px",
]);
for (const [rel, raw] of FILES) {
  if (isAppCss(rel)) continue;
  for (const m of strip(raw).matchAll(/font-size:\s*([0-9.]+)px\s*;/g)) {
    const [full, num] = [m[1] + "px", parseFloat(m[1])];
    if (FS_EXEMPT.has(`${rel}:${full}`)) continue;
    const tok = FS.get(full);
    if (tok) fail(`${rel}: font-size: ${full} is var(${tok}) — a literal does not rescale on a phone`);
    else if (num < FS_FLOOR) fail(`${rel}: font-size: ${full} is below the ${FS_FLOOR}px floor app.css sets for itself`);
  }
}

// ---- 3. spacing ------------------------------------------------------------
// Only the byte-identical cases, and only where EVERY value in the declaration
// is on the scale. Off-grid numbers are a separate argument and not this one.
const SP_PROP = "(?:gap|row-gap|column-gap|padding|margin|padding-top|padding-right|padding-bottom|padding-left|margin-top|margin-right|margin-bottom|margin-left|padding-block|padding-inline|margin-block|margin-inline)";
const SP_RE = new RegExp(`(?<![-\\w])(${SP_PROP}):\\s*((?:[0-9]+px)(?:\\s[0-9]+px){0,3})\\s*;`, "g");
for (const [rel, raw] of FILES) {
  if (isAppCss(rel)) continue;
  for (const m of strip(raw).matchAll(SP_RE)) {
    const vals = m[2].split(/\s+/);
    if (vals.every((v) => SP.has(v))) {
      fail(`${rel}: ${m[1]}: ${m[2]} is ${vals.map((v) => `var(${SP.get(v)})`).join(" ")}`);
    }
  }
}

// ---- 4. motion -------------------------------------------------------------
// The app's verbs, each with its curve (app.css › Motion): arrive/lift
// (--ease-lift, a bounded settle), move (--ease-out), leave (--ease-in), and
// the older non-overshooting arrival (--ease-spring). The celebration's
// overshoot is --ease-overshoot (rule 12). A literal spelling of any of them is
// drift. Anything genuinely else — the shake, the editorial --ease-calm —
// stays a literal.
const norm = (s) => s.replace(/\s+/g, "");
const SPRING = new Set(["cubic-bezier(0.34,1.56,0.64,1)", "cubic-bezier(0.34,1.4,0.5,1)", "cubic-bezier(0.34,1.3,0.5,1)", "cubic-bezier(0.34,1.4,0.64,1)", "cubic-bezier(0.34,1.5,0.5,1)", "cubic-bezier(0.22,1.1,0.36,1)"]);
const DECEL = new Set(["cubic-bezier(0.2,0.9,0.3,1)", "cubic-bezier(0.2,0.8,0.2,1)", "cubic-bezier(0.22,1,0.36,1)", "cubic-bezier(0.2,0.8,0.25,1)", "cubic-bezier(0.16,0.84,0.44,1)", "cubic-bezier(0.2,0.7,0.45,1)", "cubic-bezier(0.2,0.7,0.3,1)"]);
const NAMED = new Map([
  [norm(tokenValue("--ease-lift") || "-"), "--ease-lift"],
  [norm(tokenValue("--ease-in") || "-"), "--ease-in"],
]);
for (const [rel, raw] of FILES) {
  for (const line of strip(raw).split("\n")) {
    if (/--ease-(spring|out|calm|overshoot|lift|in):/.test(line)) continue; // the definitions
    for (const m of line.matchAll(/cubic-bezier\([^)]*\)/g)) {
      const n = norm(m[0]);
      if (SPRING.has(n)) fail(`${rel}: ${m[0]} is the overshoot family — chrome uses var(--ease-lift) or var(--ease-spring); a celebration uses var(--ease-overshoot)`);
      if (DECEL.has(n)) fail(`${rel}: ${m[0]} is the decelerate family — use var(--ease-out)`);
      if (NAMED.has(n)) fail(`${rel}: ${m[0]} is var(${NAMED.get(n)})`);
    }
  }
}

const DUR = new Map();
for (const n of ["--dur-quick", "--dur-standard", "--dur-move"]) {
  const v = tokenValue(n);
  if (!v) continue;
  const ms = parseFloat(v);
  DUR.set(`${ms}ms`, n);
  DUR.set(`${ms / 1000}s`, n); // 120ms and 0.12s are the same duration
}
for (const [rel, raw] of FILES) {
  for (const line of strip(raw).split("\n")) {
    if (/--dur-(quick|standard|calm|move):/.test(line)) continue;
    for (const m of line.matchAll(/(?<![\w.])([0-9.]+m?s)(?![\w.])/g)) {
      const tok = DUR.get(m[1]);
      if (tok) fail(`${rel}: ${m[1]} is var(${tok})`);
    }
  }
}

// ---- 5. the phone breakpoint -----------------------------------------------
// Not "every query must be 768px" — 400px and 1150px are real tiers and a
// component may have a local one. What is forbidden is a query NEAR the
// contract but not on it: that is the band where CSS and detectMobile()
// disagree about whether this is a phone, and it is invisible until somebody
// drags a window to 760px. Same for the narrow tier and detectNarrow().
for (const [rel, raw] of FILES) {
  for (const m of strip(raw).matchAll(/@media[^{]*?(?:max|min)-width:\s*([0-9]+)px/g)) {
    const w = +m[1];
    if (w >= 700 && w <= 850 && w !== 768 && w !== 769) {
      fail(`${rel}: @media …${w}px sits beside the 768px phone contract without being on it`);
    }
    if (w >= 1100 && w <= 1200 && w !== 1150 && w !== 1151) {
      fail(`${rel}: @media …${w}px sits beside the 1150px narrow contract without being on it`);
    }
  }
}

// 5b. A coarse pointer is not a layout. Layout is chosen by width alone
// (detectMobile is `(max-width: 768px)`), and a tablet or touchscreen laptop at
// a desktop width keeps the desktop layout with bigger targets. So a query
// that names `pointer: coarse` may enlarge targets and reveal hover-only
// controls, but may not change display, grid-template*, flex-direction or
// width. `display: none` (withdrawing a hover-only control a finger cannot
// reach) and a width of exactly var(--tap-min) (a target) are what a touch
// rule is for, and pass.
{
  const LAYOUT = /(?:^|[;{\s])(display|grid-template[\w-]*|flex-direction|width)\s*:\s*([^;}]*)/g;
  // A target-size rule whose number is not the token: the GIF picker's remove
  // button, sized for a fingertip on a thumbnail.
  const COARSE_EXEMPT = new Set(["modals/ModalGifs.svelte"]);
  for (const [rel, raw] of FILES) {
    if (COARSE_EXEMPT.has(rel)) continue;
    const css = strip(raw);
    for (const m of css.matchAll(/@media([^{]*pointer:\s*coarse[^{]*)\{/g)) {
      let depth = 1;
      let j = m.index + m[0].length;
      for (; j < css.length && depth; j++) {
        if (css[j] === "{") depth++;
        else if (css[j] === "}") depth--;
      }
      const body = css.slice(m.index + m[0].length, j - 1);
      for (const d of body.matchAll(LAYOUT)) {
        const [prop, val] = [d[1], d[2].trim()];
        if (prop === "display" && val === "none") continue;
        if (prop === "width" && val === "var(--tap-min)") continue;
        fail(`${rel}: @media${m[1].trimEnd()} sets ${prop}: ${val} — a coarse pointer may size targets, not change layout; the phone layout query is (max-width: 768px)`);
      }
    }
  }
}

// ---- 6. the reduced-motion stand-down --------------------------------------
// The global block zeroed duration and left delay alone for a long time, which
// on a staggered `animation: … both` entrance means the element is held at its
// FROM frame — invisible — for the length of the delay. Both lines, or neither
// is doing its job.
//
// And ONE switch: the stand-downs key off the root attribute lib/motion.js
// stamps (the OS setting or Appearance › Motion), never the media query
// directly — a `@media (prefers-reduced-motion)` block cannot hear the in-app
// choice, so it would be a stand-down that half the people asking never get.
const nuke = /:root\[data-motion="reduced"\],\s*:root\[data-motion="reduced"\] \*,\s*:root\[data-motion="reduced"\] \*::before,\s*:root\[data-motion="reduced"\] \*::after \{([\s\S]*?)\}/.exec(APP);
if (!nuke) fail("app.css has no global reduced-motion stand-down on :root[data-motion=\"reduced\"]");
else {
  for (const prop of ["animation-duration", "animation-delay", "transition-duration", "transition-delay"]) {
    if (!new RegExp(`${prop}:[^;]*!important`).test(nuke[1])) {
      fail(`app.css reduced-motion block does not neutralise ${prop}`);
    }
  }
}
{
  const amounts = /:root\[data-motion="reduced"\] \{([^}]*)\}/.exec(APP);
  for (const t of ["--lift-y", "--lift-scale", "--current-scale", "--aside-x", "--press-scale", "--rise-y", "--enter-scale"]) {
    if (!amounts || !new RegExp(`${t}:\\s*(0px|1);`).test(amounts[1])) fail(`reduced motion does not return ${t} to identity`);
  }
  for (const [rel, raw] of FILES) {
    if (/@media[^{]*prefers-reduced-motion/.test(strip(raw))) fail(`${rel}: a @media (prefers-reduced-motion) block — key it on :root[data-motion="reduced"] (lib/motion.js) so Appearance › Motion reaches it`);
  }
}

// ---- 6b. motion moves things, it never re-lays them out ---------------------
// The vocabulary's verbs (--ease-lift, --ease-in, --dur-move) animate transform
// and opacity — what the compositor can move without asking layout. A
// transition on width, height, an inset or a margin re-lays out the page on
// every frame of it, and in the guild strip or a list that is the difference
// between a lift and a stutter. Colour and filter fades are allowed (they
// repaint, they do not reflow).
{
  const LAYOUT = /^(all|width|height|min-width|min-height|max-width|max-height|top|left|right|bottom|inset[\w-]*|margin[\w-]*|padding[\w-]*|gap|flex[\w-]*|grid[\w-]*)$/;
  for (const [rel, raw] of FILES) {
    for (const m of strip(raw).matchAll(/transition(?:-property)?\s*:\s*([^;{}]+);/g)) {
      for (const item of m[1].split(/,(?![^(]*\))/)) {
        if (!/var\(--(ease-lift|ease-in|dur-move)\)/.test(item)) continue;
        const prop = item.trim().split(/\s+/)[0];
        if (LAYOUT.test(prop)) fail(`${rel}: "${item.trim()}" — the motion verbs move transform and opacity, never ${prop}`);
      }
    }
  }
}

// ---- 7. the scrim ----------------------------------------------------------
// A dialog dim is theme-flipped; an overlay on ARTWORK is not. The rule is
// narrow on purpose: only elements whose class says they are a scrim.
for (const [rel, raw] of FILES) {
  // Class ends in "scrim", or is the dialog's bare .overlay. Deliberately not
  // every *-overlay: a .cam-overlay is a caption chip painted on a photograph,
  // and black is simply what it is, on every theme.
  for (const m of strip(raw).matchAll(/\.(?:[\w-]*scrim|overlay)\s*\{[^}]*?background:\s*(rgba?\(\s*0\s*,\s*0\s*,\s*0[^)]*\))/g)) {
    fail(`${rel}: a scrim painted ${m[1]} — use var(--scrim), which flips with the theme`);
  }
}

// ---- 8. the quiet button's ink ---------------------------------------------
//
// `button { background: var(--accent); color: var(--accent-fg) }` is a trap, and
// it sprung. --accent-fg is the ink chosen to survive ON the accent, and since
// it stopped being white (white only made 3.14:1 on the brand teal) it has been
// near-black — so every button that repainted its background to a SURFACE and
// left `color` alone has been writing near-black on charcoal. In the dark theme
// that is not poor contrast, it is 1.1:1, and the inbox shipped four filter
// chips and two actions like it with the moderation log's chips copied from the
// same block.
//
// `button.quiet` is the answer: one class that declares BOTH halves. This holds
// it to that, and measures the pair it declares — plus the two surface inks a
// quiet control is allowed to use — against the real backgrounds in both
// themes. 4.5:1 because these are labels, not decoration.

// Token values per theme, falling back to :root for anything a theme does not
// override. Read out of the stylesheet so retuning a token retunes the rules
// that police it. Shared by rules 8 and 10.
const themeBlock = (sel) => {
  const at = APP.indexOf(sel);
  if (at < 0) fail(`tokens gate: no "${sel}" block in app.css — the theme rules would measure nothing`);
  return at < 0 ? "" : APP.slice(at, APP.indexOf("\n}", at));
};
// The light palette is scoped (no pack, or a Daylight pack), so its selector
// opens with this and continues with the :where() list.
const LIGHT_SEL = ':root[data-theme="light"]:where(';
const ROOT = themeBlock(":root {");
const LIGHT = themeBlock(LIGHT_SEL);
const THEMES = [["dark", ROOT], ["light", LIGHT]];
const value = (name, block) => {
  const re = new RegExp(`^\\s*${name}:\\s*([^;]+);`, "m");
  const m = re.exec(block) || re.exec(ROOT);
  return m ? colorsIn(m[1])[0] : null;
};

{
  const quiet = /button\.quiet\s*\{([^}]*)\}/.exec(APP);
  if (!quiet) fail("app.css: button.quiet is gone — it is the pattern that keeps a surface button legible");
  else {
    const bg = /background(?:-color)?:\s*var\((--[\w-]+)\)/.exec(quiet[1]);
    const fg = /(?:^|[^-])color:\s*var\((--[\w-]+)\)/.exec(quiet[1]);
    if (!bg || !fg) {
      fail("app.css: button.quiet must declare BOTH a background and a color — half of it is the bug");
    } else {
      // fg, bg, why. The quiet pair comes from the stylesheet; the other two
      // are the inks a chip may use for its unselected and selected states.
      const PAIRS = [
        [fg[1], bg[1], "button.quiet"],
        ["--text-muted", bg[1], "a quiet chip's unselected label"],
        ["--accent-fg", "--accent", "an accent-filled chip's label"],
        // The faintest ink on the hover/selected/field fill: timestamps in a
        // hovered row, meta in a selected one.
        ["--text-faint", "--bg-3", "faint ink on a hover or selected fill"],
      ];
      for (const [theme, b] of THEMES) {
        for (const [fgTok, bgTok, why] of PAIRS) {
          const f = value(fgTok, b);
          const g = value(bgTok, b);
          if (!f || !g) {
            fail(`tokens gate: cannot read ${fgTok} / ${bgTok} in the ${theme} theme`);
            continue;
          }
          const r = contrast(f.slice(0, 3), g.slice(0, 3));
          if (r < 4.5) {
            fail(`${why}: ${fgTok} on ${bgTok} is ${r.toFixed(2)}:1 in the ${theme} theme, below 4.5`);
          }
        }
      }
    }
  }
}

// ---- 8b. the same inks, in every theme pack --------------------------------
//
// Rule 8 measured the two default themes, and the inbox's chips went out to
// the phone reading dark-on-grey in a pack it never measured. A pack retints
// --bg-3 and --text-muted together, and thirty-one of them give --bg-3 an
// alpha so the backdrop shows through — so the chip's real ground is --bg-3
// composited over the surface the dialog sits on (--bg-elevated, opaque in
// every pack by rule 9). The selected chip's ink is not in any stylesheet:
// applyAppearance picks accentForeground(--accent) at runtime, the better of
// near-black and white, so the gate asks the same question of the pack's
// accent. 4.5:1, labels again.
{
  const PACKS = fs.readFileSync(path.join(SRC, "themepacks.css"), "utf8");
  const FG = [
    [0x14, 0x14, 0x19, 1],
    [255, 255, 255, 1],
  ]; // FG_DARK / FG_LIGHT in lib/state.svelte.js
  // The LAST declaration wins, as it does in the cascade: a pack may have a
  // second block further down that re-tints its grounds and says nothing
  // about its inks, and the two are one rule to the browser.
  const rawTok = (name, block) => {
    const re = new RegExp(`^\\s*${name}:\\s*([^;]+);`, "gm");
    const all = [...block.matchAll(re)];
    const m = all.length ? all[all.length - 1] : re.exec(ROOT);
    return m ? m[1].trim() : null;
  };
  // A token's colour, following `var(--other)` through the pack and then :root.
  const resolve = (name, block, depth = 0) => {
    const raw = rawTok(name, block);
    if (!raw || depth > 6) return null;
    const ref = /^var\((--[\w-]+)\)$/.exec(raw);
    if (ref) return resolve(ref[1], block, depth + 1);
    return colorsIn(raw)[0] || null;
  };
  const merged = new Map();
  for (const m of strip(PACKS).matchAll(/:root\[data-theme-pack="([^"]+)"\]\s*\{([^}]*)\}/g)) {
    merged.set(m[1], (merged.get(m[1]) || "") + "\n" + m[2]);
  }
  let packs = 0;
  for (const [pack, block] of merged) {
    packs++;
    const elevated = resolve("--bg-elevated", block);
    const bg3 = resolve("--bg-3", block);
    const muted = resolve("--text-muted", block);
    const faint = resolve("--text-faint", block);
    const accent = resolve("--accent", block);
    if (!elevated || !bg3 || !muted || !accent || !faint) {
      fail(`tokens gate: cannot read the chip tokens of the "${pack}" pack`);
      continue;
    }
    const ground = over(bg3, over(elevated, [0, 0, 0]));
    const ink = over(muted, ground);
    const r = contrast(ink, ground);
    if (r < 4.5) {
      fail(`a quiet chip's unselected label: --text-muted on --bg-3 is ${r.toFixed(2)}:1 in the "${pack}" pack, below 4.5`);
    }
    // The faint tier is text too (timestamps, meta), on the same fill.
    const fr = contrast(over(faint, ground), ground);
    if (fr < 4.5) {
      fail(`faint ink on a hover or selected fill: --text-faint on --bg-3 is ${fr.toFixed(2)}:1 in the "${pack}" pack, below 4.5`);
    }
    const acc = over(accent, ground);
    const best = Math.max(...FG.map((f) => contrast(f.slice(0, 3), acc)));
    if (best < 4.5) {
      fail(`an accent-filled chip's label: neither ink reaches 4.5 on --accent in the "${pack}" pack (${best.toFixed(2)}:1)`);
    }
  }
  if (packs < 40) fail(`tokens gate: only ${packs} theme packs were measured — the pack selector has changed`);
}

// ---- 8c. a surface that clears the status bar keeps clearing it ------------
//
// The phone's top bar sat under the clock on a Galaxy S24+ (384 CSS px wide)
// and nowhere else, through three rounds of native-inset work that each
// measured a correct 52px on a 412px emulator. The bar's rule said
// `padding-top: max(var(--safe-top), …)`; a narrow-viewport rule forty lines
// later said `padding: 0 2px` — and the shorthand resets padding-top to 0 on
// every phone 400px wide or narrower. Nothing native could have fixed it.
//
// So: a selector that pads itself by --safe-top in one rule may not be given
// a `padding:` shorthand in another. Longhands (padding-inline, padding-left)
// say what they mean and leave the inset alone.
{
  // Every (selector, body) pair in a stylesheet, including rules nested in
  // @media blocks; the selector list is kept verbatim.
  const allRules = (css) => {
    const out = [];
    const walk = (text) => {
      let i = 0;
      while (i < text.length) {
        const open = text.indexOf("{", i);
        if (open < 0) break;
        let depth = 1;
        let j = open + 1;
        for (; j < text.length && depth; j++) {
          if (text[j] === "{") depth++;
          else if (text[j] === "}") depth--;
        }
        const sel = text.slice(i, open).trim().split(/[;}]/).pop().trim();
        const body = text.slice(open + 1, j - 1);
        if (sel.startsWith("@")) walk(body);
        else out.push([sel, body]);
        i = j;
      }
    };
    walk(text(css));
    return out;
  };
  const text = (css) => strip(css);
  for (const [rel, raw] of FILES) {
    const rules = allRules(raw);
    const inset = new Set();
    const INSET = /padding-top:\s*max\([^;]*--safe-top/;
    for (const [sel, body] of rules) {
      if (INSET.test(body)) {
        for (const one of sel.split(",")) inset.add(one.trim());
      }
    }
    if (!inset.size) continue;
    for (const [sel, body] of rules) {
      // A shorthand that the SAME block then follows with the inset longhand
      // is the common "padding: 0 6px; padding-top: max(...)" idiom and is
      // fine: later wins within one block too. Only a shorthand that comes
      // after the longhand, or in another block, zeroes it.
      const at = body.search(/(^|[\s;])padding:\s/);
      if (at < 0) continue;
      const insetAt = body.search(INSET);
      if (insetAt >= 0 && insetAt > at) continue;
      for (const one of sel.split(",")) {
        if (inset.has(one.trim())) {
          fail(
            `${rel}: \`${one.trim()}\` pads itself by --safe-top and is later given a \`padding:\` shorthand, ` +
              "which zeroes that top inset — use padding-inline / padding-left / padding-right",
          );
        }
      }
    }
  }
}

// ---- 9. floating chrome is opaque -----------------------------------------
//
// Thirty-one of the theme packs give --bg-0/1/2/3 an alpha, because those are
// the grounds an animated backdrop is meant to show through. --bg-elevated is
// the deliberate exception: it names the surface that is LIFTED off the page —
// a dialog, a menu, a popover, a toast — and it is opaque in all fifty packs
// and both default themes. See the note above --bg-elevated in themepacks.css.
//
// The dialogs were moved onto it and the popovers were not, so the profile
// card, the status sheet, every dropdown, the tooltip, the ⓘ bubble and the
// toasts were printing over the message they were covering. This rule is what
// stops that happening again.
//
// "Floats" is read as --shadow-pop (the app's only shadow token, and it exists
// to say exactly that) AND taken out of the flow. Both halves are needed: a
// preview tile with a drop shadow is laid out inside a parent whose ground it
// already knows, and it is allowed to be a lighter or darker step of it. Only
// a box that is positioned over something it did not lay out has to be opaque.
{
  // A rule that says "lifted" and also paints a ground must paint an opaque one.
  const GROUND = /background(?:-color)?:\s*([^;]*var\(--bg-[0123]\)[^;]*);/;
  for (const [rel, raw] of FILES) {
    for (const m of strip(raw).matchAll(/\{([^{}]*box-shadow:\s*var\(--shadow-pop\)[^{}]*)\}/g)) {
      if (!/position:\s*(?:fixed|absolute|sticky)\b/.test(m[1])) continue;
      const g = GROUND.exec(m[1]);
      if (g && !g[1].includes("--bg-elevated")) {
        fail(
          `${rel}: a --shadow-pop surface is grounded on \`${g[1].trim()}\` — ` +
            "use var(--bg-elevated), which is opaque in every pack",
        );
      }
    }
  }
}

// ---- 10. a button that repaints its ground declares its ink ----------------
//
// Rule 8 holds the ONE pattern (`button.quiet`) legible. This one stops the
// trap being reachable at all, because the pattern only helps the people who
// reach for it: `.sfx` and `.bubble` in VoicePanel were the fourth and fifth
// surfaces to set `background` on a <button> and leave `color` to the global
// `button { background: var(--accent); color: var(--accent-fg) }` — which is
// near-black, so every sound's name on the in-call soundboard and every name
// chip under a focused tile rendered at 1.12:1 in the dark theme.
//
// Neither of those classes is spelt "btn", so a rule that read selectors could
// not have caught them. Button-ness is therefore read off the MARKUP: collect
// every class this file puts on a <button> (or a role="button"), then hold the
// rules that style one of them to declaring both halves.
//
// Only BASE rules are asked. A `:hover` that deepens a wash inherits the ink
// its base rule set, and demanding `color` there would be noise. A background
// of `transparent`/`none` is not a repaint, so it is not asked either.
{
  // The ink a silent button gets, from the global rule in app.css.
  // `color: inherit` (the bare button is unstyled) inherits the page's ink.
  const g = /\nbutton\s*\{([^}]*)\}/.exec(APP)?.[1] || "";
  const INHERITED = /(?:^|[^-])color:\s*inherit/.test(g)
    ? "--text"
    : /(?:^|[^-])color:\s*var\((--[\w-]+)\)/.exec(g)?.[1] || "--accent-fg";
  // The last compound of a selector — the thing the rule is actually about.
  const subject = (sel) => sel.trim().split(/\s*[>+~]\s*|\s+/).pop() || "";
  for (const [rel, raw] of FILES) {
    if (isAppCss(rel)) continue;
    // Every class this file hands to a button, from `class="a b"` and `class:c`.
    const btnClasses = new Set();
    for (const tag of raw.matchAll(/<[A-Za-z][^>]*>/g)) {
      const t = tag[0];
      if (!/^<button\b/.test(t) && !/role=["']button["']/.test(t)) continue;
      for (const m of t.matchAll(/\bclass=["']([^"']*)["']/g)) {
        for (const c of m[1].split(/\s+/)) if (c && !c.includes("{")) btnClasses.add(c);
      }
      for (const m of t.matchAll(/\bclass:([\w-]+)/g)) btnClasses.add(m[1]);
    }
    const style = /<style[^>]*>([\s\S]*)<\/style>/.exec(raw);
    if (!style) continue;
    const css = strip(style[1]);
    // Two passes: what declares ink, then what repaints without it.
    //
    // Which rules cover which is a SUBSET question, and it has to be, in both
    // directions. `.row { color }` covers `.row.sel { background }`, because
    // every element the variant paints is also a `.row`. `.sfx.add { color }`
    // does NOT cover `.sfx { background }` — that was the first version of this
    // rule, and it declared the soundboard fine because one chip out of
    // fourteen ("Make one") happened to name its own colour.
    const inked = [];
    const painted = [];
    for (const m of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
      const body = m[2];
      const hasInk = /(?:^|[^-\w])color:\s*[^;]+/.test(body);
      // The FIRST token in the value is the ground: `color-mix(in srgb,
      // var(--bg-1) 82%, transparent)` is a wash of --bg-1 over whatever is
      // behind it, and a literal (#000, a swatch variable) is a picture rather
      // than a surface — those carry their own ink or no text at all.
      const bg = /background(?:-color)?:\s*[^;]*?var\((--[\w-]+)\)/.exec(body);
      for (const sel of m[1].split(",")) {
        const sub = subject(sel);
        if (/[:[]/.test(sub)) continue; // a state or an attribute variant, not the base
        // A scrim is a full-screen button with nothing inside it; rule 7 is
        // what governs the one colour it paints.
        if (/scrim/.test(sub)) continue;
        const classes = [...sub.matchAll(/\.([\w-]+)/g)].map((x) => x[1]);
        // EVERY class, not any: `active`, `on` and `sel` are worn by buttons
        // all over a file, and `.channel-row.active` is a row, not a button.
        const isButton = classes.length
          ? classes.every((c) => btnClasses.has(c))
          : /(^|\.)button$/.test(sub);
        if (!isButton) continue;
        if (hasInk) inked.push(classes);
        if (bg) painted.push([classes, bg[1], sel.trim()]);
      }
    }
    for (const [classes, ground, sel] of painted) {
      if (inked.some((ink) => ink.every((c) => classes.includes(c)))) continue;
      // Measured, not assumed: only a ground the inherited ink cannot survive
      // is a bug. A button filled with --danger or --accent is already the
      // pair those tokens were chosen as.
      for (const [theme, block] of THEMES) {
        const f = value(INHERITED, block);
        const g = value(ground, block);
        if (!f || !g) continue;
        const r = contrast(f.slice(0, 3), g.slice(0, 3));
        if (r >= 4.5) continue;
        fail(
          `${rel}: \`${sel}\` is a button, repaints its ground to var(${ground}) and never says ` +
            `\`color\` — it inherits var(${INHERITED}), which is ${r.toFixed(2)}:1 there in the ${theme} theme`,
        );
        break;
      }
    }
  }
}

// ---- rule 11: a state change names a duration token -----------------------
//
// The motion tokens were adopted well — hundreds of uses — and thirty-six
// literals survived, including exact duplicates of the tokens spelled by hand
// and the same number spelled two ways in one codebase (60ms and 0.06s; 220ms
// and 0.22s). The token block's own comment says why that matters: "the drift
// is visible when two elements animate side by side", and 0.18s next to
// var(--dur-standard) is 30ms of visible drift on nine surfaces.
//
// The rule is bounded to the band the tokens cover. A transition of a second is
// a progress bar or a slide across a pane — travel, or data — and those are
// deliberately off the scale; every one of them carries a comment saying so.
// Anything from 100ms to 260ms is a state change, and a state change has a name.
{
  const DUR = /(\b\d*\.?\d+)(ms|s)\b/g;
  const ms = (n, unit) => (unit === "s" ? Number(n) * 1000 : Number(n));
  for (const [rel, src] of FILES) {
    if (rel === "app.css") continue; // where the tokens are declared
    for (const decl of src.match(/transition:[^;{}]*;/g) || []) {
      for (const m of decl.matchAll(DUR)) {
        const v = ms(m[1], m[2]);
        if (v < 100 || v > 260) continue;
        fail(
          `${rel}: \`${decl.trim()}\` — ${m[0]} is a state change in the band the motion ` +
            `tokens cover. Name one (--dur-quick 120ms, --dur-standard 150ms, --dur-calm 200ms).`,
        );
      }
    }
  }
}

// ---- 8d. nothing scrolls under the status bar --------------------------
//
// Padding a SCROLLER by --safe-top keeps it clear only while it is still: the
// padding scrolls with the content, so the content rides up through it and
// under the clock. On an S24-shaped emulator (45dp status bar, punch hole) the
// onboarding scroller carried the logo and the recovery phrase behind the
// status icons that way. A scroller takes the inset as a border (content clips
// at the padding edge) or sits below a non-scrolling bar that takes it.
{
  for (const [rel, raw] of FILES) {
    if (!/\.(svelte|css)$/.test(rel)) continue;
    const by = new Map();
    for (const m of strip(raw).matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
      const sel = m[1].trim().split("\n").pop().trim();
      for (const one of sel.split(",")) {
        const k = one.trim();
        by.set(k, (by.get(k) || "") + ";" + m[2]);
      }
    }
    for (const [sel, body] of by) {
      if (/overflow(-y)?:\s*(auto|scroll)/.test(body) && /padding(-top)?:[^;]*--safe-top/.test(body))
        fail(`${rel}: ${sel} scrolls and pads itself by --safe-top, so its content scrolls under the status bar; take the inset as border-top instead`);
    }
  }
}

// ---- the faintest ink still reads -----------------------------------------
// --text-faint carries timestamps, captions and labels at 11–12.5px, where
// WCAG asks 4.5:1. The two base themes hold it there against every surface,
// the hover/field fill (--bg-3) included; rule 8b holds every pack to the same
// on its own --bg-3. Placeholders still use --text-muted.
{
  const block = (sel) => {
    const at = APP.indexOf(sel);
    return at < 0 ? "" : APP.slice(at, APP.indexOf("}", at));
  };
  const tok = (body, name) => {
    const m = body.match(new RegExp(`--${name}:\\s*(#[0-9a-fA-F]{6})`));
    return m ? colorsIn(m[1])[0] : null;
  };
  const dark = block(":root {");
  const light = block(LIGHT_SEL);
  const themes = [
    ["dark", dark, ["bg-0", "bg-1", "bg-2", "bg-3"]],
    ["light", light, ["bg-0", "bg-1", "bg-2", "bg-3", "bg-elevated"]],
  ];
  for (const [name, body, grounds] of themes) {
    const faint = tok(body, "text-faint");
    if (!faint) {
      fail(`${name}: no --text-faint hex to check`);
      continue;
    }
    for (const g of grounds) {
      const bg = tok(body, g) || tok(dark, g);
      if (!bg) continue;
      const c = contrast(faint, bg);
      if (c < 4.5) fail(`${name}: --text-faint on --${g} is ${c.toFixed(2)}:1, under 4.5`);
    }
  }
  const ph = APP.match(/input::placeholder,\s*textarea::placeholder\s*\{([^}]*)\}/);
  if (!ph || !/var\(--text-muted\)/.test(ph[1])) fail("placeholders must use --text-muted (--text-faint is 3.4:1 on a field's fill)");
}

// ---- 12. no glows, and no overshoot in chrome ------------------------------
// The accent means "this is about you", and a halo of it around a button, a
// badge or a tile spent that meaning on decoration. --accent-glow and
// --float-shadow survive as names (packs set them, and the cascade must still
// resolve), but nothing outside the token sheets may paint with them.
// Likewise the overshoot curve: chrome does not bounce, a celebration may.
{
  const OVERSHOOT_OK = /^(lib\/burst\.js|lib\/fx[\w-]*\.js|FxLayer\.svelte|FxOverlay\.svelte|JoinVeil\.svelte|app\.css)$/;
  const walkAll = (dir, out = []) => {
    for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
      const p = path.join(dir, e.name);
      if (e.isDirectory()) walkAll(p, out);
      else if (/\.(svelte|css|js)$/.test(e.name) && !/\.test\.mjs$/.test(e.name)) out.push(p);
    }
    return out;
  };
  for (const p of walkAll(SRC)) {
    const rel = path.relative(SRC, p);
    const src = strip(fs.readFileSync(p, "utf8"));
    if (!isAppCss(rel)) {
      for (const m of src.matchAll(/var\(--(accent-glow|float-shadow)\b/g)) {
        fail(`${rel}: var(--${m[1]}) — glows are retired; a float uses var(--shadow-pop), and nothing else glows`);
      }
    }
    // --ease-lift is NOT this: it settles by 2% and is allowed by name in
    // chrome; rule 4 forbids its literal, so the bound lives in one place.
    if (!OVERSHOOT_OK.test(rel) && /var\(--ease-overshoot\)/.test(src)) {
      fail(`${rel}: var(--ease-overshoot) is for celebrations (burst.js, fx*.js, FxLayer, FxOverlay, JoinVeil); chrome uses var(--ease-spring)`);
    }
  }
}

// ---- 13. a count is never red ----------------------------------------------
// A number means "this needs you": mentions, DMs, replies. It wears the user's
// accent. Red says "something is wrong", and a guild with three new messages
// is not wrong. Any rule whose selector names a badge or a count may not fill
// itself with --danger.
for (const [rel, raw] of FILES) {
  for (const m of strip(raw).matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const sel = m[1].trim().split("\n").pop();
    if (!/badge|count/i.test(sel)) continue;
    const bg = /background(?:-color)?:\s*([^;]*)/.exec(m[2]);
    if (bg && /var\(--danger\)/.test(bg[1]) && !/color-mix/.test(bg[1])) {
      fail(`${rel}: \`${sel.trim()}\` fills a count with var(--danger) — counts use var(--accent); red is for errors`);
    }
  }
}

// ---- 14. labels are sentence case -----------------------------------------
// Tracked-out capitals ("PICK ONE", "TUESDAY · 18:30", "MEMBERS & ROLES")
// were the old system's labels, and they survived in corners of the feed long
// after the rest went to sentence case. A label is --fs-small/600 in muted
// ink. Initials in an avatar tile are not a label. The two art studios are
// still waiting for their pass and are listed so the rule can hold everywhere
// else; take them off the list when they get it.
const CAPS_OK = new Set(["Avatar.svelte", "GroupAvatar.svelte", "BannerStudio.svelte", "DecorStudio.svelte"]);
for (const [rel, raw] of FILES) {
  if (CAPS_OK.has(rel)) continue;
  if (/text-transform:\s*uppercase/.test(strip(raw))) fail(`${rel}: text-transform: uppercase — labels are sentence case (--fs-small, 600, --text-muted)`);
}

console.log(failures === 0 ? "tokens.test.mjs: OK" : `tokens.test.mjs: ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
