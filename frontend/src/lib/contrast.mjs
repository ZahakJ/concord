// contrast.mjs — the colour maths the cosmetics tests are built on.
//
// Lifted out of guildbanners.test.mjs, which had it to itself. It was written
// there to answer one question — "can you read a guild's name over this
// banner?" — but the question generalises to every decorative library in the
// app, and the answer had been guessed by eye for all of them.
//
// Nothing here is imported by the app; it is test infrastructure that happens
// to live beside the data it measures.

// --- WCAG relative luminance and contrast --------------------------------

const lin = (c) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
export const lum = ([r, g, b]) =>
  0.2126 * lin(r / 255) + 0.7152 * lin(g / 255) + 0.0722 * lin(b / 255);

export function contrast(a, b) {
  const [hi, lo] = lum(a) >= lum(b) ? [lum(a), lum(b)] : [lum(b), lum(a)];
  return (hi + 0.05) / (lo + 0.05);
}

// --- parsing --------------------------------------------------------------

// Every colour literal in a CSS fragment, as [r,g,b,a]. Understands #rgb,
// #rrggbb, rgb()/rgba() in both comma and space syntax, and the handful of
// bare keywords the cosmetics libraries actually use.
const KEYWORD = {
  white: [255, 255, 255, 1],
  black: [0, 0, 0, 1],
  transparent: [0, 0, 0, 0],
};

export function colorsIn(s) {
  const out = [];
  for (const m of String(s).matchAll(/#([0-9a-f]{3}|[0-9a-f]{6})\b/gi)) {
    const h = m[1].length === 3 ? [...m[1]].map((c) => c + c).join("") : m[1];
    out.push([
      parseInt(h.slice(0, 2), 16),
      parseInt(h.slice(2, 4), 16),
      parseInt(h.slice(4, 6), 16),
      1,
    ]);
  }
  for (const m of String(s).matchAll(
    /rgba?\(\s*([\d.]+)[\s,]+([\d.]+)[\s,]+([\d.]+)(?:[\s,/]+([\d.]+))?\s*\)/gi,
  )) {
    out.push([+m[1], +m[2], +m[3], m[4] === undefined ? 1 : +m[4]]);
  }
  for (const [word, rgba] of Object.entries(KEYWORD)) {
    for (let i = (String(s).match(new RegExp(`\\b${word}\\b`, "g")) || []).length; i > 0; i--) {
      out.push([...rgba]);
    }
  }
  return out;
}

// Split a `background` shorthand into its layers — commas at paren depth 0.
export function layersOf(base) {
  const out = [];
  let depth = 0;
  let start = 0;
  for (let i = 0; i < base.length; i++) {
    const c = base[i];
    if (c === "(") depth++;
    else if (c === ")") depth--;
    else if (c === "," && depth === 0) {
      out.push(base.slice(start, i));
      start = i + 1;
    }
  }
  out.push(base.slice(start));
  return out;
}

// The worst backdrop a painted surface can put behind something, as an [r,g,b]
// triple. `dir` is +1 when we want the LIGHTEST possible result and -1 for the
// darkest.
//
// Why this is a true bound and not a guess: relative luminance is monotone in
// each channel, so a per-channel extreme bounds the luminance of anything the
// surface can produce. Within one layer, a gradient only ever blends its own
// stops, and (lin being convex and increasing) a blend's luminance never
// exceeds the max of its endpoints. Across layers, each is composited exactly
// ONCE over what's below, so walking bottom-to-top — CSS lists the bottom layer
// last — gives the extreme without pretending a 4%-alpha wash is opaque.
export function worstBackdrop(base, dir) {
  const better = (x, y) => (dir > 0 ? Math.max(x, y) : Math.min(x, y));
  let m = null;
  for (const layer of layersOf(base).reverse()) {
    const cols = colorsIn(layer);
    if (!cols.length) continue;
    if (!m) {
      // The bottom layer is the floor: nothing shows through it.
      m = [0, 1, 2].map((i) => cols.reduce((acc, c) => better(acc, c[i]), cols[0][i]));
      continue;
    }
    // Transparent regions keep what's underneath, so `m` is always a candidate.
    m = [0, 1, 2].map((i) =>
      cols.reduce((acc, c) => better(acc, c[3] * c[i] + (1 - c[3]) * m[i]), m[i]),
    );
  }
  return m || [0, 0, 0];
}

// Composite one [r,g,b,a] over an opaque [r,g,b].
export const over = ([r, g, b, a = 1], bg) =>
  [r, g, b].map((c, i) => a * c + (1 - a) * bg[i]);

// --- the daylight grounds -------------------------------------------------

// The packs a cosmetic has to survive: the six bright theme packs plus the
// plain light theme. Their --bg-1 is read out of app.css rather than copied
// here, so retuning a pack retunes the gate with it.
//
// Three of them declare --bg-1 as translucent white over a pastel mesh. The
// brightest that composite can be is plain white, and brighter is strictly
// harder for the pale colours these libraries are full of — so white is the
// honest worst case and the one used.
export const DAYLIGHT = [
  { name: "light", sel: ':root[data-theme="light"]' },
  { name: "paper", sel: ':root[data-theme-pack="paper"]' },
  { name: "solarized", sel: ':root[data-theme-pack="solarized"]' },
  { name: "meadow", sel: ':root[data-theme-pack="meadow"]' },
  { name: "porcelain", sel: ':root[data-theme-pack="porcelain"]' },
  { name: "daybreak", sel: ':root[data-theme-pack="daybreak"]' },
  { name: "atrium", sel: ':root[data-theme-pack="atrium"]' },
];

// daylightGrounds reads each pack's --bg-1 out of the stylesheet and returns
// [{ name, rgb }]. Throws rather than silently skipping: a gate that quietly
// measures nothing is worse than no gate.
export function daylightGrounds(css) {
  return DAYLIGHT.map(({ name, sel }) => {
    // A pack's selector appears more than once — the daylight packs share a
    // grouped block for their status colours before each declares its own
    // palette — so take every occurrence and keep the one that actually
    // declares the ground.
    let m = null;
    let seen = false;
    for (let at = css.indexOf(sel); at >= 0; at = css.indexOf(sel, at + 1)) {
      seen = true;
      const block = css.slice(at, css.indexOf("\n}", at));
      const hit = block.match(/--bg-1:\s*([^;]+);/);
      if (hit) m = hit;
    }
    if (!seen) throw new Error(`contrast harness: ${sel} is no longer in app.css`);
    if (!m) throw new Error(`contrast harness: ${sel} declares no --bg-1`);
    const [c] = colorsIn(m[1]);
    if (!c) throw new Error(`contrast harness: cannot read ${sel} --bg-1 (${m[1].trim()})`);
    // Translucent surfaces sit on a near-white mesh; white is the bound.
    return { name, rgb: over(c, [255, 255, 255]) };
  });
}

// worstDaylight returns the daylight ground a colour reads WORST against.
export function worstDaylight(rgb, grounds) {
  let worst = null;
  for (const g of grounds) {
    const ratio = contrast(rgb, g.rgb);
    if (!worst || ratio < worst.ratio) worst = { ratio, pack: g.name };
  }
  return worst;
}
