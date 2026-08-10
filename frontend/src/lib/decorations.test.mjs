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
import { readFileSync } from "node:fs";
import {
  DECORATIONS,
  DECORATION_BY_ID,
  DECORATION_GROUPS,
  decoration,
  wornRing,
} from "./decorations.js";

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
const TOKENS = new Set(["c1", "c2", "ink", "light", "none"]);
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

  let animated = 0;
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
      ok(TOKENS.has(v) || hex.test(v), `${at}: "${v}" is neither a colour token nor a hex colour`);
    }

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

if (failures) {
  console.error(`decorations.js: ${failures} failure(s)`);
  process.exit(1);
}
console.log(
  `decorations.js: all passed (${DECORATIONS.length} decorations, ${DECORATIONS.filter((d) => d.ring).length} of them rings, ${DECORATIONS.reduce((n, d) => n + d.parts.length, 0)} parts, ${animClasses.size} animations)`,
);
