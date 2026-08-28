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
import { contrast, colorsIn } from "./contrast.mjs";

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
  "--focus-ring", "--focus-ring-offset", "--scrim",
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
// simply illegible"). Two exemptions, both text drawn inside a fixed box small
// enough that the phone step would not fit: see the comments at each site.
const FS_FLOOR = 11;
const FS_EXEMPT = new Set(["PollView.svelte:8px", "modals/InfoDot.svelte:9.5px"]);
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
// The app means two things by a curve: an overshoot (a thing ARRIVES) and a
// deceleration (a thing MOVES). Anything genuinely else — an ease-in for
// something leaving, the shake, the editorial --ease-calm — stays a literal.
const norm = (s) => s.replace(/\s+/g, "");
const SPRING = new Set(["cubic-bezier(0.34,1.56,0.64,1)", "cubic-bezier(0.34,1.4,0.5,1)", "cubic-bezier(0.34,1.3,0.5,1)", "cubic-bezier(0.34,1.4,0.64,1)", "cubic-bezier(0.34,1.5,0.5,1)", "cubic-bezier(0.22,1.1,0.36,1)"]);
const DECEL = new Set(["cubic-bezier(0.2,0.9,0.3,1)", "cubic-bezier(0.2,0.8,0.2,1)", "cubic-bezier(0.22,1,0.36,1)", "cubic-bezier(0.2,0.8,0.25,1)", "cubic-bezier(0.16,0.84,0.44,1)", "cubic-bezier(0.2,0.7,0.45,1)", "cubic-bezier(0.2,0.7,0.3,1)"]);
for (const [rel, raw] of FILES) {
  for (const line of strip(raw).split("\n")) {
    if (/--ease-(spring|out|calm):/.test(line)) continue; // the definitions
    for (const m of line.matchAll(/cubic-bezier\([^)]*\)/g)) {
      const n = norm(m[0]);
      if (SPRING.has(n)) fail(`${rel}: ${m[0]} is the overshoot family — use var(--ease-spring)`);
      if (DECEL.has(n)) fail(`${rel}: ${m[0]} is the decelerate family — use var(--ease-out)`);
    }
  }
}

const DUR = new Map();
for (const n of ["--dur-quick", "--dur-standard"]) {
  const v = tokenValue(n);
  if (!v) continue;
  const ms = parseFloat(v);
  DUR.set(`${ms}ms`, n);
  DUR.set(`${ms / 1000}s`, n); // 120ms and 0.12s are the same duration
}
for (const [rel, raw] of FILES) {
  for (const line of strip(raw).split("\n")) {
    if (/--dur-(quick|standard|calm):/.test(line)) continue;
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

// ---- 6. the reduced-motion stand-down --------------------------------------
// The global block zeroed duration and left delay alone for a long time, which
// on a staggered `animation: … both` entrance means the element is held at its
// FROM frame — invisible — for the length of the delay. Both lines, or neither
// is doing its job.
const nuke = /@media \(prefers-reduced-motion: reduce\) \{\s*\*,\s*\*::before,\s*\*::after \{([\s\S]*?)\}/.exec(APP);
if (!nuke) fail("app.css has no global prefers-reduced-motion stand-down");
else {
  for (const prop of ["animation-duration", "animation-delay", "transition-duration", "transition-delay"]) {
    if (!new RegExp(`${prop}:[^;]*!important`).test(nuke[1])) {
      fail(`app.css reduced-motion block does not neutralise ${prop}`);
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
{
  const quiet = /button\.quiet\s*\{([^}]*)\}/.exec(APP);
  if (!quiet) fail("app.css: button.quiet is gone — it is the pattern that keeps a surface button legible");
  else {
    const bg = /background(?:-color)?:\s*var\((--[\w-]+)\)/.exec(quiet[1]);
    const fg = /(?:^|[^-])color:\s*var\((--[\w-]+)\)/.exec(quiet[1]);
    if (!bg || !fg) {
      fail("app.css: button.quiet must declare BOTH a background and a color — half of it is the bug");
    } else {
      // Token values per theme, falling back to :root for anything a theme
      // does not override. Read out of the stylesheet so retuning a token
      // retunes the gate with it.
      const block = (sel) => {
        const at = APP.indexOf(sel + " {");
        return at < 0 ? "" : APP.slice(at, APP.indexOf("\n}", at));
      };
      const ROOT = block(":root");
      const LIGHT = block(':root[data-theme="light"]');
      const value = (name, themeBlock) => {
        const re = new RegExp(`^\\s*${name}:\\s*([^;]+);`, "m");
        const m = re.exec(themeBlock) || re.exec(ROOT);
        return m ? colorsIn(m[1])[0] : null;
      };
      // fg, bg, why. The quiet pair comes from the stylesheet; the other two
      // are the inks a chip may use for its unselected and selected states.
      const PAIRS = [
        [fg[1], bg[1], "button.quiet"],
        ["--text-muted", bg[1], "a quiet chip's unselected label"],
        ["--accent-fg", "--accent", "an accent-filled chip's label"],
      ];
      for (const [theme, b] of [["dark", ROOT], ["light", LIGHT]]) {
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

console.log(failures === 0 ? "tokens.test.mjs: OK" : `tokens.test.mjs: ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
