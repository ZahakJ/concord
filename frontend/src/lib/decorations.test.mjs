// Zero-dependency test for the worn-decoration library (`npm test` runs it).
//
// A decoration is data that becomes SVG on every viewer's screen, so the ways
// it can be wrong are all silent — nothing throws, the art just is not there.
// Every check below is a mistake that was actually made while building it:
//
//   1. The WIRE. The id travels inside a peer's profile style blob and
//      internal/app/service.go bounds it with validID: 1–32 characters of
//      a-z/0-9/'-'. An id outside that is dropped on save, and the wearer is
//      the one who finds out. Ids are also permanent — deleting one blanks the
//      decoration of everyone who saved it, because lookup fails closed.
//   2. The PAINTER. `anim` names an animation class, `fill` names a colour
//      token, `o` names a queue position; all are strings, and a typo produces
//      a part that is never animated, never painted, or never offset. Every one
//      is checked against what AvatarDecoration.svelte actually defines.
//   3. MOTION. The premise of the library is that a still decoration is a
//      sticker, so a decoration with nothing animated is a bug in itself.
//   4. VISIBILITY. A `z: "back"` part draws behind the avatar, so it is only
//      visible outside radius 36. Wings authored without ever clearing that
//      radius shipped once, and only a sliver of them was ever on screen.
//   5. THE BOX. There are only about 12 units of headroom straight up before
//      art leaves the 100-unit viewBox. Every crown drawn to look right on its
//      own turned out to be clipped at the top until this was checked.
//   6. THE RING FLAG. `ring: true` is what tells Avatar.svelte to keep the
//      circular silhouette (app.css hands a non-.ringed avatar the theme's
//      --avatar-radius, which some packs square off). An entry that encircles
//      the face without the flag gets a disc drawn around a squircle; one that
//      claims the flag without reaching past the avatar's own edge takes the
//      circle away from a theme for nothing.
//   7. THE DEFS. A gradient or filter is reached by name — `fill: "@band"`,
//      `filter: "fur"` — and a name that is not in the decoration's own `defs`
//      resolves to `url(#…)` against nothing at all. SVG's answer to a dangling
//      paint reference is to draw the shape in BLACK, silently, so a typo in a
//      gradient name is a black crown rather than an error.
//   8. THE COLOURWAY. What the wearer chose to have the piece IN is another
//      bounded id on the wire (Style.Dc), resolved against COLORWAYS. The two
//      choices that are NOT colourways — "" for the wearer's profile colour and
//      "own" for the piece's own — are the ones a table entry could quietly
//      eat by taking one of those names, and the fallback for an id this build
//      does not know has to be the DEFAULT, not nothing: a decoration painted
//      in no colour at all is a black silhouette.
import { readFileSync } from "node:fs";
import {
  DECORATIONS,
  DECORATION_BY_ID,
  DECORATION_GROUPS,
  COLORWAYS,
  COLORWAY_BY_ID,
  CW_OWN,
  decoration,
  decorColors,
  wornRing,
} from "./decorations.js";
import { WORN_RINGS } from "./wornrings.js";
import { RINGS } from "./rings.js";

let failures = 0;
const fail = (msg) => {
  console.error("FAIL:", msg);
  failures++;
};
const ok = (cond, msg) => {
  if (!cond) fail(msg);
};

// The painter is the authority on which classes exist.
const painter = readFileSync(new URL("../AvatarDecoration.svelte", import.meta.url), "utf8");
// The trailing guard keeps the queue-position classes (.anim.o-3) out of the
// set of animation names; without it "o" reads as a legal animation.
const animClasses = new Set(
  [...painter.matchAll(/\.anim\.([a-z][a-z0-9]*)(?![a-z0-9-])/g)].map((m) => m[1]),
);
// Queue positions must have a rule to mean anything. The two side markers are
// the exception: "l" is always the unshifted side, and "r" is opposed only for
// the animations that define it, so both are legal on any part.
const offsets = new Set([...painter.matchAll(/\.o-([a-z0-9]+)\b/g)].map((m) => m[1]));
offsets.add("l").add("r");
// The ramp. The painter derives four shades per wearer colour with color-mix;
// a token naming a step it never declared resolves to an unset custom property,
// which paints the shape BLACK rather than failing.
const SHADES = ["glint", "lit", "shade", "deep"];
const TOKENS = new Set(["c1", "c2", "ink", "light", "none"]);
for (const c of ["c1", "c2"])
  for (const sh of SHADES) {
    TOKENS.add(`${c}-${sh}`);
    ok(
      painter.includes(`--d-${c}-${sh}:`),
      `the painter no longer declares --d-${c}-${sh}, so that ramp step paints black`,
    );
  }
// The def kinds the painter knows how to build, read off its own branches.
const DEFKINDS = new Set([...painter.matchAll(/g\.t === "([a-z]+)"/g)].map((m) => m[1]));
// …and of those, the ones that produce a <filter> rather than a paint server.
const FILTERKINDS = new Set(["blur", "turb"]);
ok(DEFKINDS.size >= 4, `only ${DEFKINDS.size} def kinds found — did the painter's <defs> move?`);
const hex = /^#[0-9a-f]{3,8}$/i;

ok(animClasses.size >= 8, `only ${animClasses.size} animation classes found — did the CSS move?`);
ok(painter.includes(".dec.tilt"), "the painter no longer defines the whole-piece tilt");
ok(DECORATIONS.length >= 61, `expected at least 61 decorations, got ${DECORATIONS.length}`);
// The drawn rings folded in from the old frames library. They are counted
// rather than named so adding one is free, but losing the lot is not: every id
// among them is still reachable from a profile's `frame` field, saved before
// the two libraries became one.
ok(
  DECORATIONS.filter((d) => d.ring).length >= 21,
  `expected at least 21 worn rings, got ${DECORATIONS.filter((d) => d.ring).length}`,
);
for (const id of ["runic-ring", "laurel-ring", "chainmail", "sunburst-crown"])
  ok(!!decoration(id) && wornRing(id), `${id}: a legacy \`frame\` id must still resolve, as a ring`);

const seen = new Set();
for (const d of DECORATIONS) {
  const where = d.id || "(no id)";

  // 1. the wire
  ok(/^[a-z0-9-]{1,32}$/.test(d.id || ""), `${where}: id must match validID in service.go`);
  ok(!seen.has(d.id), `${where}: duplicate id`);
  seen.add(d.id);
  ok(!!d.name && !!d.group, `${where}: needs a name and a group`);
  ok(Array.isArray(d.parts) && d.parts.length > 0, `${where}: no parts`);
  if (d.anim != null) ok(animClasses.has(d.anim), `${where}: anim "${d.anim}" is not defined`);
  if (d.tilt != null) ok(d.tilt === true, `${where}: tilt is a flag, not ${d.tilt}`);

  if (d.ring != null) ok(d.ring === true, `${where}: ring is a flag, not ${d.ring}`);

  // A piece may carry its own colourway. It is a DEFAULT — the wearer's
  // profile colour overrides it — so it may only ever be literal colour.
  if (d.own != null) {
    ok(
      Array.isArray(d.own) && d.own.length >= 1 && d.own.length <= 2,
      `${where}: own must be one or two colours`,
    );
    for (const c of d.own || []) ok(hex.test(c), `${where}: own colour "${c}" is not a hex colour`);
  }

  // 7. the defs
  const paints = new Set();
  const filters = new Set();
  for (const [i, g] of (d.defs || []).entries()) {
    const at = `${where}.defs[${i}]`;
    ok(!!g.id && !paints.has(g.id) && !filters.has(g.id), `${at}: missing or duplicate def id`);
    ok(DEFKINDS.has(g.t), `${at}: def kind "${g.t}" is not one the painter can build`);
    (FILTERKINDS.has(g.t) ? filters : paints).add(g.id);
    if (g.t === "lg" || g.t === "rg" || g.t === "rgb") {
      ok(Array.isArray(g.stops) && g.stops.length >= 2, `${at}: a gradient needs two stops`);
      let last = -1;
      for (const [o, c, a] of g.stops || []) {
        ok(o >= 0 && o <= 1 && o >= last, `${at}: stop offset ${o} is out of range or out of order`);
        last = o;
        ok(TOKENS.has(c) || hex.test(c), `${at}: stop colour "${c}" is neither a token nor hex`);
        ok(a == null || (a >= 0 && a <= 1), `${at}: stop alpha ${a} is out of range`);
      }
    }
    // A turbulence with a `flick` list is the only SMIL in the app, and the
    // painter drops it under prefers-reduced-motion. Seeds are integers: a
    // fractional seed is a different noise field on every browser.
    if (g.flick != null)
      ok(
        /^\d+(;\d+)+$/.test(g.flick),
        `${at}: flick must be a semicolon-separated list of integer seeds`,
      );
    if (g.t === "turb") ok(g.scale > 0, `${at}: a displacement map with no scale does nothing`);
  }

  let animated = 0;
  let animatedCoarse = 0;
  let backTotal = 0;
  let backOutside = 0;
  let lo = [Infinity, Infinity];
  let hi = [-Infinity, -Infinity];
  // Which quarters of the annulus outside the avatar this decoration reaches.
  const around = new Set();

  for (const [i, p] of d.parts.entries()) {
    const at = `${where}[${i}]`;
    ok(p.z === "back" || p.z === "front", `${at}: z must be "back" or "front"`);

    // 2. the painter
    if (p.a) {
      const name = p.anim || d.anim;
      ok(!!name, `${at}: animated but neither the part nor the decoration names an animation`);
      if (name) ok(animClasses.has(name), `${at}: animation "${name}" is not defined`);
      animated++;
      if (!p.hi) animatedCoarse++;
    } else {
      ok(p.anim == null, `${at}: names an animation but never opts in with a: true`);
    }
    if (p.o != null) ok(offsets.has(String(p.o)), `${at}: offset "o: ${p.o}" has no rule`);
    if (p.pv != null)
      ok(
        Array.isArray(p.pv) && p.pv.length === 2 && p.pv.every(Number.isFinite),
        `${at}: pv must be an [x, y] pivot`,
      );
    for (const v of [p.fill, p.stroke]) {
      if (v == null) continue;
      if (v[0] === "@")
        ok(paints.has(v.slice(1)), `${at}: "${v}" names no gradient in this decoration's defs`);
      else ok(TOKENS.has(v) || hex.test(v), `${at}: "${v}" is neither a colour token nor hex`);
    }
    if (p.filter != null)
      ok(filters.has(p.filter), `${at}: filter "${p.filter}" names no filter in this decoration`);
    if (p.op != null)
      ok(p.op > 0 && p.op <= 1, `${at}: op ${p.op} is out of range (0 draws nothing)`);
    if (p.hi != null) ok(p.hi === true, `${at}: hi is a flag, not ${p.hi}`);

    // geometry
    let pts;
    if (p.el === "circle" || p.el === "ellipse") {
      const { cx, cy, r, rx = r, ry = r } = p.attrs || {};
      ok(Number.isFinite(cx) && Number.isFinite(cy) && Number.isFinite(rx), `${at}: bad ${p.el}`);
      pts = [
        [cx - rx, cy - ry],
        [cx + rx, cy + ry],
        [cx - rx, cy + ry],
        [cx + rx, cy - ry],
      ];
    } else {
      ok(typeof p.d === "string" && p.d.length > 0, `${at}: empty path data`);
      ok(!/NaN|undefined|Infinity/.test(p.d || ""), `${at}: path data contains NaN/undefined/Infinity`);
      pts = pathPoints(p.d || "");
      ok(pts.length > 0, `${at}: no drawable points parsed out of the path`);
    }
    for (const [x, y] of pts) {
      lo = [Math.min(lo[0], x), Math.min(lo[1], y)];
      hi = [Math.max(hi[0], x), Math.max(hi[1], y)];
      if (Math.hypot(x - 50, y - 50) > 36.5)
        around.add((x < 50 ? 0 : 1) + (y < 50 ? 0 : 2));
    }

    // 4. visibility
    if (p.z === "back") {
      backTotal++;
      if (pts.some(([x, y]) => Math.hypot(x - 50, y - 50) > 36.5)) backOutside++;
    }
  }

  // 3. motion
  ok(animated > 0, `${where}: nothing moves — the library's whole premise is motion`);
  // …and it has to survive the tile tier, where `hi` parts are not drawn at
  // all. A piece whose only moving parts are fine detail is a still picture in
  // the picker, which is the one place the motion is the information.
  ok(
    animatedCoarse > 0,
    `${where}: everything that moves is flagged \`hi\`, so nothing moves below 40px`,
  );

  if (backTotal)
    ok(
      backOutside > 0,
      `${where}: all ${backTotal} back parts are inside r=36, so the avatar hides every one`,
    );

  // 5. the box
  //
  // A figure gets half a unit of slack, because the failure it guards against
  // is a crown drawn to look right on its own and then cut off at the top.
  // A ring gets three, and the difference is not laziness: a ring is authored
  // OUT to the edge of the box on every side, so a link whose short axis is
  // 5 units wide sits its long axis across the boundary by a unit or two, and
  // that has always painted — the painter's SVG is overflow: visible, and the
  // overlay is 138% of the avatar. Past three units it would start reaching
  // into the tile beside it in a picker grid, which is the real limit.
  const pad = d.ring ? 3 : 0.5;
  ok(
    lo[0] >= -pad && lo[1] >= -pad && hi[0] <= 100 + pad && hi[1] <= 100 + pad,
    `${where}: art leaves the viewBox — x ${lo[0].toFixed(1)}..${hi[0].toFixed(1)}, y ${lo[1].toFixed(1)}..${hi[1].toFixed(1)}`,
  );

  // 6. the ring flag
  if (d.ring)
    ok(
      around.size === 4,
      `${where}: claims ring: true but only reaches ${around.size} of the 4 quarters outside the avatar — it does not encircle, and the flag costs the wearer's theme its avatar shape`,
    );
}

// Lookup fails closed — the property that makes it safe to render an id that
// arrived on someone else's broadcast profile.
ok(decoration("") === null, "decoration('') should be null");
ok(decoration("no-such-decoration") === null, "an unknown id must resolve to null");
ok(wornRing("") === false, "wornRing('') should be false");
ok(wornRing("no-such-decoration") === false, "wornRing must fail closed too");
ok(wornRing("cat-ears") === false, "a figure worn on the head is not a ring");
ok(decoration(DECORATIONS[0].id) === DECORATIONS[0], "a known id must resolve to its decoration");
ok(Object.keys(DECORATION_BY_ID).length === DECORATIONS.length, "index size mismatch");
ok(
  DECORATION_GROUPS.reduce((n, g) => n + g.ids.length, 0) === DECORATIONS.length,
  "every decoration must appear in exactly one group",
);

// A drawn RING is the only kind of decoration ever reachable from a profile's
// `frame` field, and Avatar.svelte resolves it there before falling through to
// lib/rings.js. So a ring-flagged id that also names a gradient ring makes that
// gradient ring unwearable — which is not hypothetical: both libraries picked
// "comet", and the merge made the drawn one shadow the gradient one until the
// resolution was narrowed to the ring flag. A figure may share a name freely,
// because a figure travels in `dec` and is never looked for in `frame`.
{
  const gradient = new Set(RINGS.map((r) => r.id).filter(Boolean));
  for (const d of DECORATIONS)
    if (d.ring)
      ok(
        !gradient.has(d.id),
        `${d.id}: a drawn ring may not share an id with a gradient ring — it would shadow it in \`frame\``,
      );
}

// ── 8. the colourways ───────────────────────────────────────────────────────
ok(
  COLORWAYS.length >= 10 && COLORWAYS.length <= 14,
  `the colourway table is meant to be curated and bounded, not ${COLORWAYS.length} long`,
);
ok(Object.keys(COLORWAY_BY_ID).length === COLORWAYS.length, "colourway index size mismatch");
{
  const cseen = new Set();
  for (const c of COLORWAYS) {
    const where = c.id || "(no id)";
    ok(/^[a-z0-9-]{1,32}$/.test(c.id || ""), `${where}: colourway id must match validID`);
    ok(!cseen.has(c.id), `${where}: duplicate colourway id`);
    cseen.add(c.id);
    ok(!!c.name, `${where}: a colourway needs a name to appear in the picker`);
    ok(c.id !== CW_OWN, `${where}: "${CW_OWN}" is the as-designed choice, not a colourway`);
    ok(
      Array.isArray(c.c) && c.c.length >= 1 && c.c.length <= 2,
      `${where}: a colourway is one or two base colours — the ramp derives the rest`,
    );
    for (const v of c.c || []) ok(hex.test(v), `${where}: base "${v}" is not a hex colour`);
    // Every entry has to actually resolve, or a swatch in the picker paints
    // something the wearer can never get.
    const [r1, r2] = decorColors("cat-ears", c.id, "#ff0000", "#00ff00");
    ok(r1 === c.c[0] && !!r2, `${where}: does not resolve to its own base colours`);
  }
}
// The default, and the fail-closed direction. "" is what every profile saved
// before this field existed carries, and an id from a peer's newer build has to
// land on the same thing rather than on nothing at all.
ok(
  decorColors("cat-ears", "", "#123456", "#654321").join() === "#123456,#654321",
  "no colourway must resolve to the wearer's own profile colours",
);
ok(
  decorColors("cat-ears", "no-such-colourway", "#123456", "#654321").join() === "#123456,#654321",
  "an unknown colourway must fail closed to the wearer's profile colours, not to nothing",
);
// "As designed" only means something on a piece that declares one; anywhere
// else it is another unknown id and falls back the same way.
{
  const own = DECORATIONS.find((d) => d.own);
  ok(!!own, "no decoration declares `own` any more — the as-designed choice has nothing to pick");
  ok(
    decorColors(own.id, CW_OWN, "#123456", "#654321")[0] === own.own[0],
    `${own.id}: "${CW_OWN}" must paint the piece in its own colourway`,
  );
  // Every piece in the library now declares a colourway, so the fallback is
  // only reachable through an id this build does not know — which is the case
  // that actually matters, since the id arrives on someone else's profile.
  ok(
    decorColors("no-such-decoration", CW_OWN, "#123456", "#654321").join() === "#123456,#654321",
    `"${CW_OWN}" on an id with no colourway of its own must fall back to the wearer's colours`,
  );
}

// ── the standard every piece is held to ─────────────────────────────────────
//
// Both of these were true of three decorations out of sixty-one once. The
// library read as sixty-one flat stickers in the same accent colour, and the
// reason was not that anyone chose flatness — it was that nothing stopped a
// new piece shipping without either, so nothing ever had them.
//
// `defs` is the material: the gradients that give a shape a lit face and a
// shaded crease. Without one a piece is a silhouette, whatever it is drawn as.
// `own` is the colourway it was DESIGNED in, and it is a default rather than a
// lock — a wearer who has set a profile colour still overrides it. Without one
// a piece can only ever be the viewer's accent, which is what made a laurel
// wreath and a circuit ring the same object in two shapes.
for (const d of DECORATIONS) {
  ok(
    (d.defs || []).length > 0,
    `${d.id}: no defs, so it has no material — every piece is shaded, see the MATERIALS block`,
  );
  ok(
    Array.isArray(d.own) && d.own.length > 0,
    `${d.id}: no \`own\` colourway, so it can only ever be painted in the viewer's accent`,
  );
}

/**
 * pathPoints: every point a path actually reaches. Curve control points are
 * included (they bound the curve) and arcs are sampled, because the shape that
 * matters most here — a band swept round the head — is all arc and none of its
 * extremes are endpoints. Reading numbers off the path in pairs, the cheap way,
 * mistakes an arc's flags and radii for coordinates and then cheerfully reports
 * that a hidden part is visible.
 */
function pathPoints(d) {
  const ARITY = { M: 2, L: 2, H: 1, V: 1, C: 6, S: 4, Q: 4, T: 2, A: 7, Z: 0 };
  const out = [];
  const toks = d.match(/[a-zA-Z]|-?\d*\.?\d+(?:e[-+]?\d+)?/g) || [];
  let i = 0;
  let cmd = "M";
  let cx = 0;
  let cy = 0;
  let sx = 0;
  let sy = 0;
  while (i < toks.length) {
    if (/[a-zA-Z]/.test(toks[i])) {
      cmd = toks[i++];
      if (cmd.toUpperCase() === "Z") {
        cx = sx;
        cy = sy;
        continue;
      }
    }
    const up = cmd.toUpperCase();
    const k = ARITY[up];
    if (k == null) break;
    const args = toks.slice(i, i + k).map(Number);
    i += k;
    if (args.length < k || args.some(Number.isNaN)) break;
    const rel = cmd !== up;
    let ex = cx;
    let ey = cy;
    if (up === "H") ex = rel ? cx + args[0] : args[0];
    else if (up === "V") ey = rel ? cy + args[0] : args[0];
    else {
      ex = rel ? cx + args[k - 2] : args[k - 2];
      ey = rel ? cy + args[k - 1] : args[k - 1];
      if (up === "C" || up === "S" || up === "Q" || up === "T")
        for (let j = 0; j + 1 < k; j += 2)
          out.push([rel ? cx + args[j] : args[j], rel ? cy + args[j + 1] : args[j + 1]]);
      if (up === "A") out.push(...arcPoints(cx, cy, ex, ey, args[0], args[3], args[4]));
    }
    out.push([ex, ey]);
    if (up === "M") {
      sx = ex;
      sy = ey;
    }
    cx = ex;
    cy = ey;
  }
  return out;
}

// Circular arcs only: every arc this library emits has rx === ry.
function arcPoints(x1, y1, x2, y2, r, large, sweep) {
  const dx = (x2 - x1) / 2;
  const dy = (y2 - y1) / 2;
  const half = Math.hypot(dx, dy);
  if (!half || !r) return [];
  const h = Math.sqrt(Math.max(0, r * r - half * half));
  const s = large === sweep ? -1 : 1;
  const cx = (x1 + x2) / 2 + (s * h * -dy) / half;
  const cy = (y1 + y2) / 2 + (s * h * dx) / half;
  const a1 = Math.atan2(y1 - cy, x1 - cx);
  let span = Math.atan2(y2 - cy, x2 - cx) - a1;
  if (sweep && span < 0) span += 2 * Math.PI;
  if (!sweep && span > 0) span -= 2 * Math.PI;
  const out = [];
  for (let j = 0; j <= 20; j++) {
    const t = a1 + (span * j) / 20;
    out.push([cx + r * Math.cos(t), cy + r * Math.sin(t)]);
  }
  return out;
}

// 8. The RING LIST. wornrings.js duplicates which decorations are rings,
//    because every avatar in the app needs that answer before it can pick its
//    silhouette and cannot wait for a 125KB table to arrive over the network to
//    get it. A duplicate is only safe while something enforces it.
{
  const inTable = DECORATIONS.filter((d) => d.ring).map((d) => d.id).sort();
  const inList = [...WORN_RINGS].sort();
  const missing = inTable.filter((id) => !WORN_RINGS.has(id));
  const extra = inList.filter((id) => !inTable.includes(id));
  if (missing.length) fail(`wornrings.js is missing rings that decorations.js has: ${missing.join(", ")}`);
  if (extra.length) fail(`wornrings.js names rings decorations.js does not have: ${extra.join(", ")}`);
}

if (failures) {
  console.error(`decorations.js: ${failures} failure(s)`);
  process.exit(1);
}
console.log(
  `decorations.js: all passed (${DECORATIONS.length} decorations, ${DECORATIONS.filter((d) => d.ring).length} of them rings, ${DECORATIONS.reduce((n, d) => n + d.parts.length, 0)} parts, ${animClasses.size} animations, ${COLORWAYS.length} colourways)`,
);
