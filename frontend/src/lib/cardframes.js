// cardframes.js — scenic frames drawn AROUND the profile card.
//
// A card frame is not a border. It is a small stage set: battlements standing
// above the card's top edge, gnarled branches reaching in over the corners,
// palm trunks holding the sides up, waves lapping along the foot. The card sits
// inside it the way a photograph sits inside an ornate frame.
//
// AUTHORING CONTRACT
//
//   viewBox     0 0 272 400 — the card's own box. x maps 1:1 onto the 272px
//               card; y is stretched to the card's real height, because a card
//               grows with its content. Vertical detail therefore belongs in
//               the TOP and BOTTOM bands where a few percent of stretch does
//               not read, and the long sides want things that do not care how
//               tall they are: trunks, columns, curtains, kelp.
//   overflow    The painter does NOT clip. Art may run outside the box —
//               negative y above the card, x < 0 or x > 272 at the sides — and
//               that overhang is most of the point.
//   keep clear  The card's CONTENT has to stay legible. Side art stays within
//               ~16 units of each edge below y=56; the masses live in the top
//               band (y < 56), the bottom band (y > 366) and the four corners.
//   z           "back" draws BEHIND the card, "front" over it. A back part is
//               only visible where it leaves the box — that is what makes a
//               tower read as standing behind the card instead of painted on
//               it. A back part drawn entirely inside 0..272 × 0..400 is
//               invisible; that is the single most common authoring mistake.
//   colour      Tokens "c1"/"c2" (the wearer's two profile colours), "light",
//               "ink", "@name" for one of the frame's own gradients, or a
//               literal hex where the colour IS the identity — stone grey,
//               pumpkin orange, velvet red.
//   motion      `a` names an animation class defined in CardFrame.svelte, `or`
//               gives its transform-origin in viewBox units, `dl` a delay in
//               seconds, `op` a flat opacity. Everything stops under
//               prefers-reduced-motion with the drawing intact.
//   offline     Paths and gradients only. Nothing is ever fetched — no images,
//               no fonts, no CDN.
//
// Ids are short and lowercase because they ride a peer's profile through
// validID in internal/app/service.go, and are resolved here failing CLOSED.

// ── geometry helpers ──────────────────────────────────────────────────────
// Path data written by hand is unverifiable and, in this repository's
// experience, usually wrong. Everything below is computed instead.

const r2 = (n) => Math.round(n * 100) / 100;
const rad = (deg) => (deg * Math.PI) / 180;

/** poly: a closed (or open) polyline through points. */
function poly(pts, close = true) {
  return (
    pts.map(([x, y], i) => `${i ? "L" : "M"}${r2(x)} ${r2(y)}`).join("") +
    (close ? "Z" : "")
  );
}

/** smooth: an open curve through points, quadratics via midpoints. */
function smooth(pts, close = false) {
  if (pts.length < 3) return poly(pts, close);
  let d = `M${r2(pts[0][0])} ${r2(pts[0][1])}`;
  for (let i = 1; i < pts.length - 1; i++) {
    const [x, y] = pts[i];
    const [nx, ny] = pts[i + 1];
    d += `Q${r2(x)} ${r2(y)} ${r2((x + nx) / 2)} ${r2((y + ny) / 2)}`;
  }
  const last = pts[pts.length - 1];
  const prev = pts[pts.length - 2];
  d += `Q${r2(prev[0])} ${r2(prev[1])} ${r2(last[0])} ${r2(last[1])}`;
  return d + (close ? "Z" : "");
}

const rect = (x, y, w, h) =>
  poly([
    [x, y],
    [x + w, y],
    [x + w, y + h],
    [x, y + h],
  ]);

const tri = (a, b, c) => poly([a, b, c]);

/** dot: a circle, as path data (the painter only draws paths). */
const dot = (cx, cy, r) =>
  `M${r2(cx - r)} ${r2(cy)}a${r2(r)} ${r2(r)} 0 1 0 ${r2(2 * r)} 0a${r2(r)} ${r2(r)} 0 1 0 ${r2(-2 * r)} 0Z`;

/** oval: an ellipse, optionally rotated, sampled so rotation is trivial. */
function oval(cx, cy, rx, ry, rot = 0, n = 28) {
  const c = Math.cos(rad(rot));
  const s = Math.sin(rad(rot));
  const pts = [];
  for (let i = 0; i < n; i++) {
    const t = (i / n) * Math.PI * 2;
    const x = Math.cos(t) * rx;
    const y = Math.sin(t) * ry;
    pts.push([cx + x * c - y * s, cy + x * s + y * c]);
  }
  return poly(pts);
}

function arcPts(cx, cy, r, a0, a1, n = 16) {
  const pts = [];
  for (let i = 0; i <= n; i++) {
    const a = rad(a0 + ((a1 - a0) * i) / n);
    pts.push([cx + Math.cos(a) * r, cy + Math.sin(a) * r]);
  }
  return pts;
}

/**
 * limb: a tapered, optionally bending limb — trunk, branch, kelp, root, tail.
 * Angles are degrees with 0 = right and -90 = straight up. `curve` is the
 * total bend over the whole length. Returns the outline plus the tip, so the
 * caller can grow the next thing from where this one ended.
 */
function limb(x, y, ang, len, w0, w1, curve = 0, segs = 9) {
  const spine = [];
  let cx = x;
  let cy = y;
  let a = ang;
  for (let i = 0; i <= segs; i++) {
    spine.push([cx, cy]);
    const step = len / segs;
    cx += Math.cos(rad(a)) * step;
    cy += Math.sin(rad(a)) * step;
    a += curve / segs;
  }
  const left = [];
  const right = [];
  for (let i = 0; i < spine.length; i++) {
    const j = Math.min(i + 1, spine.length - 1);
    const k = Math.max(i - 1, 0);
    const dx = spine[j][0] - spine[k][0];
    const dy = spine[j][1] - spine[k][1];
    const m = Math.hypot(dx, dy) || 1;
    const w = w0 + ((w1 - w0) * i) / segs;
    left.push([spine[i][0] - (dy / m) * w, spine[i][1] + (dx / m) * w]);
    right.push([spine[i][0] + (dy / m) * w, spine[i][1] - (dx / m) * w]);
  }
  return {
    d: poly([...left, ...right.reverse()]),
    spine,
    tip: spine[spine.length - 1],
    ang: a,
  };
}

/** rnd: a tiny deterministic generator, so a frame draws the same every time. */
function rnd(seed) {
  let s = seed >>> 0;
  return () => {
    s = (s + 0x6d2b79f5) >>> 0;
    let t = Math.imul(s ^ (s >>> 15), 1 | s);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/** blob: an organic lump — a leaf mass, a boulder, a cloud, a moss mound. */
function blob(cx, cy, r, { squash = 1, wob = 0.16, n = 14, seed = 1 } = {}) {
  const g = rnd(seed);
  const pts = [];
  for (let i = 0; i < n; i++) {
    const t = (i / n) * Math.PI * 2;
    const rr = r * (1 - wob + g() * wob * 2);
    pts.push([cx + Math.cos(t) * rr, cy + Math.sin(t) * rr * squash]);
  }
  return smooth([...pts, pts[0], pts[1]], true);
}

/** waveTop: a sine crest from x0 to x1, closed down to `bottom`. */
function waveTop(x0, x1, y, amp, wl, phase, bottom) {
  const pts = [];
  const n = Math.max(12, Math.round((x1 - x0) / 6));
  for (let i = 0; i <= n; i++) {
    const x = x0 + ((x1 - x0) * i) / n;
    pts.push([x, y + Math.sin((x / wl) * Math.PI * 2 + phase) * amp]);
  }
  return (
    smooth(pts, false) +
    `L${r2(x1)} ${r2(bottom)}L${r2(x0)} ${r2(bottom)}Z`
  );
}

/** crenel: a battlemented wall top — merlons up, embrasures between. */
function crenel(x0, x1, yTop, yMerlon, yBase, merlon, gap) {
  const pts = [[x0, yBase]];
  let x = x0;
  let up = true;
  pts.push([x0, yTop]);
  while (x < x1) {
    const w = up ? merlon : gap;
    const y = up ? yMerlon : yTop;
    pts.push([x, y], [Math.min(x + w, x1), y]);
    x += w;
    up = !up;
  }
  pts.push([x1, yTop], [x1, yBase]);
  return poly(pts);
}

/**
 * spray: a conifer bough — paired needle blades swept BACK along a spine, which
 * is what makes it read as a fir rather than a comb. Filled, not stroked: an
 * even-width stroke has no taper and a bough is all taper.
 */
function spray(x, y, ang, len, n, size, sweep = 34) {
  let d = "";
  for (let i = 1; i <= n; i++) {
    const t = i / (n + 0.6);
    const px = x + Math.cos(rad(ang)) * len * t;
    const py = y + Math.sin(rad(ang)) * len * t;
    const s = size * (1 - t * 0.62);
    for (const side of [-1, 1]) {
      const a = ang + side * (90 - sweep) + 180;
      const bx = px + Math.cos(rad(a)) * s;
      const by = py + Math.sin(rad(a)) * s;
      const wx = Math.cos(rad(ang)) * s * 0.22;
      const wy = Math.sin(rad(ang)) * s * 0.22;
      d += poly([
        [px + wx, py + wy],
        [bx, by],
        [px - wx, py - wy],
      ]);
    }
  }
  return d;
}

/** web: a corner cobweb — radial anchors plus sagging cross-strands. */
function web(cx, cy, r, a0, a1, rings = 4, spokes = 6) {
  let d = "";
  const angs = [];
  for (let i = 0; i <= spokes; i++) angs.push(a0 + ((a1 - a0) * i) / spokes);
  for (const a of angs) {
    d += `M${r2(cx)} ${r2(cy)}L${r2(cx + Math.cos(rad(a)) * r)} ${r2(cy + Math.sin(rad(a)) * r)}`;
  }
  for (let k = 1; k <= rings; k++) {
    const rr = (r * k) / rings;
    const pts = [];
    for (const a of angs) {
      const sag = rr * 0.1;
      pts.push([cx + Math.cos(rad(a)) * (rr - sag), cy + Math.sin(rad(a)) * (rr - sag)]);
    }
    // Sag each span by pulling its midpoint toward the centre.
    for (let i = 0; i < pts.length - 1; i++) {
      const [ax, ay] = pts[i];
      const [bx, by] = pts[i + 1];
      const mx = (ax + bx) / 2;
      const my = (ay + by) / 2;
      const px = cx + (mx - cx) * 0.86;
      const py = cy + (my - cy) * 0.86;
      d += `M${r2(ax)} ${r2(ay)}Q${r2(px)} ${r2(py)} ${r2(bx)} ${r2(by)}`;
    }
  }
  return d;
}

// ── weathering ────────────────────────────────────────────────────────────
//
// The stone frames were built from clean rectangles, and clean is the tell. A
// wall that has stood outside for four hundred years is mostly a record of
// water: joints that do not line up, corners knocked off, dirt bled down from
// every ledge, a crack that wandered and forked. None of these marks is
// interesting on its own — the density of them is the whole effect, so each of
// these returns ONE path holding dozens of marks, cheap enough to spend
// freely.

/**
 * coursedWall: masonry as courses of BLOCKS, not a striped panel. Returns the
 * joints and the chipped corners separately so a caller can lay them over an
 * already-shaded body and keep the body's own light intact. The vertical joint
 * in each course starts at a different place, which is the point: a wall whose
 * joints line up between courses is a grid, and a grid reads as printed.
 */
function coursedWall(x0, y0, w, h, ch, seed) {
  const g = rnd(seed);
  const mortar = [];
  const chips = [];
  for (let y = y0; y < y0 + h; y += ch) {
    const hh = Math.min(ch - 1.2, y0 + h - y);
    let x = x0 + 2 + g() * w * 0.45;
    while (x < x0 + w - 2) {
      mortar.push(rect(x, y, 0.9, hh));
      if (g() > 0.72) {
        const s = 1.4 + g() * 2.4;
        chips.push(g() > 0.5 ? tri([x, y], [x + s, y], [x, y + s]) : tri([x, y + hh], [x - s, y + hh], [x, y + hh - s]));
      }
      x += 6 + g() * (w > 40 ? 18 : 8);
    }
    mortar.push(rect(x0, y + hh, w, 1.2));
  }
  return { mortar: mortar.join(""), chips: chips.join("") };
}

/** streaks: rain-borne dirt bleeding down from under a ledge. */
function streaks(x0, x1, y, seed, { n = 12, len = 26, w = 1.6 } = {}) {
  const g = rnd(seed);
  const d = [];
  for (let i = 0; i < n; i++) {
    const x = x0 + (x1 - x0) * g();
    const L = len * (0.3 + g());
    const ww = w * (0.4 + g());
    d.push(poly([[x, y], [x + ww, y], [x + ww * 0.3, y + L], [x, y + L]]));
  }
  return d.join("");
}

/** crack: a fracture wandering down a face, with one fork off the middle. */
function crack(x, y, len, seed, { ang = 90, spread = 30, segs = 9 } = {}) {
  const g = rnd(seed);
  let d = `M${r2(x)} ${r2(y)}`;
  let cx = x;
  let cy = y;
  let fork = null;
  for (let i = 0; i < segs; i++) {
    const a = ang + (g() - 0.5) * spread;
    cx += Math.cos(rad(a)) * (len / segs);
    cy += Math.sin(rad(a)) * (len / segs);
    d += `L${r2(cx)} ${r2(cy)}`;
    if (i === Math.floor(segs / 2)) fork = [cx, cy, a];
  }
  if (fork) {
    let [px, py] = fork;
    d += `M${r2(px)} ${r2(py)}`;
    for (let i = 0; i < 4; i++) {
      const a = fork[2] - 36 + (g() - 0.5) * 22;
      px += Math.cos(rad(a)) * (len / segs) * 0.7;
      py += Math.sin(rad(a)) * (len / segs) * 0.7;
      d += `L${r2(px)} ${r2(py)}`;
    }
  }
  return d;
}

/** corbelRow: the brackets a parapet or a machicolation is carried on. */
function corbelRow(x0, x1, y, h, step) {
  let d = "";
  for (let x = x0; x < x1; x += step) {
    const w = Math.min(step - 3, x1 - x);
    if (w < 2) break;
    d += poly([[x, y], [x + w, y], [x + w - 1.7, y + h], [x + 1.7, y + h]]);
  }
  return d;
}

/** skyline: a hazy ridge across the top band, closed down to `bottom`. */
function skyline(x0, x1, y, amp, seed, n = 22, bottom = 90) {
  const g = rnd(seed);
  const pts = [];
  for (let i = 0; i <= n; i++) {
    const x = x0 + ((x1 - x0) * i) / n;
    pts.push([x, y + Math.sin(i * 1.31) * amp * 0.6 + (g() - 0.5) * amp * 1.4]);
  }
  return smooth(pts, false) + `L${r2(x1)} ${r2(bottom)}L${r2(x0)} ${r2(bottom)}Z`;
}

/**
 * gothicArch: a real two-centred arch, left springing to right springing.
 *
 * Hand-placed control points were how the cathedral's arch was built, and they
 * cannot be offset: every concentric moulding had to be guessed again, so the
 * arch had exactly one order and read as a cardboard cut-out. Two arcs struck
 * from centres inside the span give the pointed head for nothing, and — the
 * part that matters — calling it again with a smaller span and rise produces a
 * curve that is genuinely parallel to the first.
 */
function gothicArch(cx, ys, half, rise, n = 14) {
  const d = (rise * rise - half * half) / (2 * half);
  const R = half + d;
  const aL = ((Math.atan2(-rise, -d) * 180) / Math.PI + 360) % 360;
  const aR = ((Math.atan2(-rise, d) * 180) / Math.PI + 360) % 360;
  return [...arcPts(cx + d, ys, R, 180, aL, n), ...arcPts(cx - d, ys, R, aR, 360, n)];
}

/** archBand: the masonry between two such arches, carried down to `foot`. */
function archBand(cx, ys, h1, r1, h2, r2, foot, n = 14) {
  const o = gothicArch(cx, ys, h1, r1, n);
  const i = gothicArch(cx, ys, h2, r2, n).reverse();
  return poly([[cx - h1, foot], ...o, [cx + h1, foot], [cx + h2, foot], ...i, [cx - h2, foot]]);
}

/** spiral: a blacksmith's scroll. Stroked; the one shape wrought iron is for. */
function spiral(cx, cy, r0, r1, turns, a0, dir = 1) {
  const pts = [];
  const n = Math.round(turns * 18);
  for (let i = 0; i <= n; i++) {
    const t = i / n;
    const a = a0 + dir * turns * 360 * t;
    const r = r0 + (r1 - r0) * t;
    pts.push([cx + Math.cos(rad(a)) * r, cy + Math.sin(rad(a)) * r]);
  }
  return smooth(pts);
}

/** tuft: grass or weeds sprouting out of a joint. Stroked. */
function tuft(x, y, h, n, seed, spread = 16) {
  const g = rnd(seed);
  let d = "";
  for (let i = 0; i < n; i++) {
    const lean = (g() - 0.5) * spread;
    const hh = h * (0.45 + g());
    d += `M${r2(x + (g() - 0.5) * 5)} ${r2(y)}Q${r2(x + lean * 0.4)} ${r2(y - hh * 0.6)} ${r2(x + lean)} ${r2(y - hh)}`;
  }
  return d;
}

/**
 * flame: a teardrop with a wobble in it. A zigzag polygon reads as a comic
 * flame; fire has one silhouette that tapers and one that flickers, and the
 * difference is whether the outline is smoothed.
 */
function flame(x, y, h, w, seed) {
  const g = rnd(seed);
  const pts = [[x - w, y]];
  for (let i = 1; i <= 4; i++) {
    const t = i / 5;
    pts.push([x - w * (1 - t) * (0.65 + g() * 0.6) + (g() - 0.5) * 2.4, y - h * t]);
  }
  pts.push([x + (g() - 0.5) * 2.6, y - h]);
  for (let i = 4; i >= 1; i--) {
    const t = i / 5;
    pts.push([x + w * (1 - t) * (0.65 + g() * 0.6), y - h * t]);
  }
  pts.push([x + w, y]);
  return smooth(pts, true);
}

/** birdMark: an open-winged silhouette, sized so it can be sent travelling. */
const birdMark = (s) => `M${r2(-7 * s)} 0q${r2(3.5 * s)} ${r2(-4 * s)} ${r2(7 * s)} 0q${r2(3.5 * s)} ${r2(-4 * s)} ${r2(7 * s)} 0`;

// ── part constructors ─────────────────────────────────────────────────────
const P = (d, o = {}) => ({ d, z: "front", fill: "ink", ...o });
const BK = (d, o = {}) => P(d, { z: "back", ...o });
const SK = (d, o = {}) => ({ d, z: "front", stroke: "ink", sw: 2, ...o });

// A gradient definition. `lin`/`radial` are userSpaceOnUse, so their
// coordinates are viewBox units and read like everything else in the file —
// right for a thing that exists once, at one place: a moon, a sun, a sky.
//
// `glow` is the other kind, and the distinction matters: it is relative to the
// shape it fills, so ONE definition serves every torch, candle, firefly and
// spore in a frame. Written in user space at the origin, as they first were,
// each of those paints from a gradient centred at (0,0) — which is to say it
// paints nothing at all where the flame actually is.
const lin = (id, x1, y1, x2, y2, stops) => ({ id, x1, y1, x2, y2, stops });
const radial = (id, cx, cy, r, stops) => ({ id, cx, cy, r, stops });
const glow = (id, stops) => ({ id, bb: true, cx: 0.5, cy: 0.5, r: 0.5, stops });

// ── MATERIALS ───────────────────────────────────────────────────────────────
//
// These frames were built as SHAPES: a tower was one grey, a trunk one brown,
// a column one tan. The silhouettes were right and the whole set still read as
// flat cartoon vector, for the same reason the worn decorations did — a shape
// with no light in it is a sticker whatever its outline says.
//
// The fix is the same fix, and it starts with one rule: ONE LIGHT FOR EVERY
// FRAME, from the upper left. Twelve frames each lit from wherever suited them
// are twelve unrelated pictures; the same twelve lit from one place are a set.
//
// `tube` is where most of the work happens. It fits the gradient to the SHAPE
// instead of the box, so a single definition rounds off every column, trunk,
// pillar and curtain fold in a frame no matter where each one stands — dark on
// the shaded flank, bright a third of the way across, dark again at the far
// edge. That cross-section is the whole difference between a brown rectangle
// and a tree. A user-space gradient cannot do it: it lights one position, so
// the second column of a pair inherits the first one's shading and both go
// flat, which is exactly how these frames were failing.
//
// `plane` is the other half — a face turned toward or away from the light, for
// walls, floors, canopies and water, where there is no roundness to express,
// only orientation.
const tube = (id, stops) => ({ id, bb: true, x1: 0, y1: 0, x2: 1, y2: 0, stops });
const plane = (id, stops) => ({ id, bb: true, x1: 0, y1: 0, x2: 1, y2: 1, stops });
// `wash` is the vertical one, also fitted to the shape: a stain that fades as
// it runs, a glaze that dies out at the foot of a curtain. Fitted rather than
// user-space because the same definition has to serve every ledge in a frame,
// and those are at a dozen different heights.
const wash = (id, stops) => ({ id, bb: true, x1: 0, y1: 0, x2: 0, y2: 1, stops });

// The ramps. Each takes the object's own three tones — deep, body, lit — and
// spends them the way that material spends light:
//
//   rock    two hard planes and a crease, because stone is faceted, not round
//   bark    a wide dark flank and a narrow highlight, set off-centre so a
//           trunk reads as turning away rather than as a striped cylinder
//   drape   a slow wide falloff with a second dimmer highlight behind the
//           first — the thing that makes a curtain read as heavy cloth
//   leafy   lit from above-left, dark underneath, because a leaf mass is a
//           canopy and the light lands on its top
//   forged  a hard narrow specular, the tell that separates iron from stone
//   frozen  bright at both edges and dim between, so it looks see-through
const rock = (id, deep, body, lit) =>
  tube(id, [
    [0, lit],
    [0.3, body],
    [0.34, body],
    [0.37, deep],
    [1, deep],
  ]);
const bark = (id, deep, body, lit) =>
  tube(id, [
    [0, deep],
    [0.22, body],
    [0.38, lit],
    [0.54, body],
    [1, deep],
  ]);
const drape = (id, deep, body, lit) =>
  tube(id, [
    [0, deep],
    [0.16, body],
    [0.3, lit],
    [0.46, body],
    [0.62, lit],
    [0.8, body],
    [1, deep],
  ]);
const leafy = (id, deep, body, lit) =>
  plane(id, [
    [0, lit],
    [0.34, body],
    [0.7, body],
    [1, deep],
  ]);
const forged = (id, deep, body, lit) =>
  tube(id, [
    [0, deep],
    [0.2, body],
    [0.3, lit],
    [0.42, body],
    [0.74, body],
    [1, deep],
  ]);
const frozen = (id, deep, body, lit) =>
  tube(id, [
    [0, lit],
    [0.26, body],
    [0.5, deep],
    [0.74, body],
    [1, lit],
  ]);

// ── surface and depth ───────────────────────────────────────────────────────
//
// Materials round a shape off. These three are what stop it looking DRAWN, and
// the frames had none of them:
//
//   grain      noise multiplied into a surface. A gradient can say "this is
//              curved"; only texture says "this is made of something". Stone
//              gets a coarse, low-frequency grain and wood a fine one running
//              along it, and the difference between the two is most of the
//              difference between a wall and a plank.
//   occlusion  the shadow a frame casts ON the card it surrounds. This is the
//              single strongest cue that the frame is an object in front of
//              something rather than a picture printed at the same depth, and
//              its absence is why every one of these read as a sticker border.
//              Drawn last, over the card's own edge, at both sides and the top.
//   rim        the thin bright edge where light grazes a silhouette. Cheap,
//              and it does more for "solid" than another hundred marks would.
const grain = (id, freq, o = {}) => ({ t: "grain", id, freq, ...o });
const soft = (id, std) => ({ t: "soft", id, std });

// The contact shadow, as parts rather than a gradient, because it has to sit
// at a known place on the card regardless of what the frame above it is doing.
// Left, right and top: the bottom of a card is usually clear of its frame and
// a shadow there reads as dirt.
const occlusion = (k = 1) => [
  P(rect(0, 0, 26, 400), { fill: "@occl", op: r2(0.9 * k) }),
  P(rect(246, 0, 26, 400), { fill: "@occr", op: r2(0.9 * k) }),
  P(rect(0, 0, 272, 30), { fill: "@occt", op: r2(0.85 * k) }),
];
const occlusionGrads = (tint = "#05070c") => [
  lin("occl", 0, 0, 26, 0, [
    [0, tint, 0.55],
    [0.5, tint, 0.18],
    [1, tint, 0],
  ]),
  lin("occr", 272, 0, 246, 0, [
    [0, tint, 0.55],
    [0.5, tint, 0.18],
    [1, tint, 0],
  ]),
  lin("occt", 0, 0, 0, 30, [
    [0, tint, 0.5],
    [0.6, tint, 0.14],
    [1, tint, 0],
  ]),
];

// ── the frames ────────────────────────────────────────────────────────────

const castleKeep = (() => {
  // A keep at last light, seen from the foot of its wall. The palette is the
  // whole design: every face the low sun still reaches goes warm ochre, every
  // face it has left goes blue-violet, and the further a plane stands the paler
  // and the bluer it gets. Neutral grey — which is what this frame used to be
  // built from — is the one colour that cannot say which of those a surface is,
  // and a picture made of it reads as unfinished no matter how much is in it.
  const parts = [];
  const g = rnd(7);
  const MORTAR = "#2b2f3e";
  const IRON_L = "#58607a";
  // A slit, splayed on its lit side. Used on both towers and the wall.
  const loop = (x, y, h) => rect(x - 1.7, y, 3.4, h);

  // ── plane 4: the sky itself ────────────────────────────────────────────
  // A soft ellipse rather than a panel: the frame has no outer boundary, so a
  // rectangle of sky would draw its own edges and read as a poster. Fitted
  // radials fade to nothing and leave only the haze.
  parts.push(BK(oval(136, -76, 234, 166), { fill: "@sky" }));
  parts.push(BK(oval(110, -14, 218, 82), { fill: "@dusk" }));

  // Two ridges on the horizon, the further one paler, bluer and lower in
  // contrast. That pair is the cheapest depth in the frame. Both fills die out
  // before the card's top edge, because a solid ridge stops at a straight line
  // wherever it is told to and a straight line is what gives the trick away.
  parts.push(BK(skyline(-70, 342, -54, 15, 3, 26, 14), { fill: "@ridge-far" }));
  parts.push(BK(skyline(-70, 342, -32, 11, 8, 24, 14), { fill: "@ridge-near" }));

  // ── plane 3: a further keep along the same ridge ───────────────────────
  const far = [
    rect(150, -74, 44, 60),
    crenel(147, 197, -74, -83, -64, 7, 5),
    rect(196, -94, 17, 80),
    crenel(194, 215, -94, -103, -84, 5, 4),
    rect(128, -56, 20, 42),
    tri([124, -58], [152, -58], [138, -84]),
    rect(213, -40, 34, 26),
  ];
  parts.push(BK(far.join(""), { fill: "@far" }));
  parts.push(BK(rect(150, -74, 11, 60) + rect(196, -94, 5, 80) + rect(128, -56, 6, 42), { fill: "@far-lit", op: 0.55 }));
  // Two windows lit somewhere over there. Small warm marks at distance do more
  // for scale than the silhouette they sit in.
  parts.push(BK(rect(160, -60, 2.4, 4) + rect(172, -48, 2.4, 4), { fill: "#ffc169", op: 0.7, a: "glow", dl: -1.7 }));

  // Birds, high and small, crossing the whole width on a long clock.
  for (const [s, cls, dl, op] of [
    [1, "cross-high", 0, 0.45],
    [0.72, "cross", -6.5, 0.38],
    [0.55, "cross-high", -13.5, 0.3],
  ]) {
    parts.push(BK(birdMark(s), { fill: "none", stroke: "#8f9bba", sw: 1.5 * s, a: cls, or: "0px 0px", op, dl }));
  }

  // ── plane 2: the towers ────────────────────────────────────────────────
  // Deliberately unlike each other. The old frame mirrored one tower across the
  // card, and a mirrored building is the loudest possible signal that nobody
  // drew it: this one is roofed, intact and flying a pennant, the other has
  // lost its head and carries a timber hoarding bolted to the outside.

  // LEFT — tall, capped, and the first thing the light finds.
  const LX = -2;
  const LW = 31;
  const lBody = poly([[LX - LW + 3, -126], [LX + LW - 3, -126], [LX + LW, 392], [LX - LW, 392]]);
  parts.push(BK(lBody, { fill: "@tower" }));
  parts.push(BK(lBody, { fill: "@tower", f: "stone", op: 0.5 }));
  // The warm sliver down the lit edge. One thin line does more for "this is a
  // solid thing standing in light" than any amount of interior modelling.
  parts.push(BK(poly([[LX - LW + 3, -126], [LX - LW + 5.4, -126], [LX - LW + 2.4, 392], [LX - LW, 392]]), { fill: "@rim", op: 0.5 }));
  const lw = coursedWall(LX - LW, -126, LW * 2, 518, 15, 5);
  parts.push(BK(lw.mortar, { fill: MORTAR, op: 0.6 }));
  parts.push(BK(lw.chips, { fill: "@chip", op: 0.4 }));
  // A string course, its lit top edge, the shadow it throws, and the dirt that
  // has run out from under it ever since.
  for (const sy of [-58, 96]) {
    parts.push(BK(rect(LX - LW - 3, sy, LW * 2 + 6, 7), { fill: "@ledge" }));
    parts.push(BK(rect(LX - LW - 3, sy, LW * 2 + 6, 1.8), { fill: "@rim", op: 0.65 }));
    parts.push(BK(rect(LX - LW - 3, sy + 7, LW * 2 + 6, 3.6), { fill: "#0f1324", op: 0.6 }));
    parts.push(BK(streaks(LX - LW, LX + LW, sy + 10, 12 + sy, { n: 11, len: 30, w: 1.7 }), { fill: "@stain", op: 0.6 }));
  }
  parts.push(BK(loop(-19, -112, 18) + loop(-19, -18, 18) + loop(-19, 130, 18), { fill: "#0b0e18" }));
  parts.push(BK(rect(-21.6, -112, 1.4, 18) + rect(-21.6, -18, 1.4, 18) + rect(-21.6, 130, 1.4, 18), { fill: "@rim", op: 0.45 }));
  parts.push(BK(rect(-20.4, -110, 2.8, 8), { fill: "#ffc36b", a: "glow", op: 0.85 }));

  // Machicolation: brackets, then the parapet they carry, standing proud.
  parts.push(BK(corbelRow(-38, 36, -136, 10, 8), { fill: "@tower-dk" }));
  parts.push(BK(rect(-40, -150, 78, 15), { fill: "@parapet" }));
  parts.push(BK(rect(-40, -150, 78, 2.2), { fill: "@rim", op: 0.7 }));
  parts.push(BK(rect(-40, -137, 78, 3), { fill: "#0f1324", op: 0.6 }));
  // The roof goes on before the merlons, so the merlons stand in front of it.
  parts.push(BK(tri([-45, -170], [39, -170], [-3, -238]), { fill: "@roof" }));
  parts.push(BK(poly([[-3, -238], [39, -170], [-3, -170]]), { fill: "#141827", op: 0.55 }));
  const slates = [];
  for (let i = 1; i < 10; i++) {
    const t = i / 10;
    const y = -238 + 68 * t;
    const hw = 42 * t;
    slates.push(`M${r2(-3 - hw)} ${r2(y)}L${r2(-3 + hw)} ${r2(y - 1.6)}`);
  }
  parts.push({ d: slates.join(""), z: "back", stroke: "#0d111c", sw: 1, op: 0.55 });
  parts.push(BK(poly([[-3, -238], [-45, -170], [-40, -170], [-3, -233]]), { fill: "@rim", op: 0.6 }));
  parts.push(BK(crenel(-40, 38, -158, -173, -146, 11, 8), { fill: "@parapet" }));
  parts.push(BK(crenel(-40, 38, -171, -173, -173, 11, 8), { fill: "@rim", op: 0.85 }));
  // Finial, staff, pennant.
  parts.push(BK(rect(-4.3, -256, 2.6, 22), { fill: "@iron" }));
  parts.push(BK(dot(-3, -241, 3.6), { fill: "@rim", op: 0.9 }));
  parts.push(BK(poly([[-3, -255], [31, -246], [-3, -237]]), { fill: "c1", a: "wave-flag", or: "-3px -246px" }));
  parts.push(BK(poly([[-3, -251], [17, -247], [-3, -242]]), { fill: "ink", op: 0.26, a: "wave-flag", or: "-3px -246px" }));

  // RIGHT — roofless, and losing its crown one stone at a time.
  const RX = 274;
  const RW = 27;
  const rBody = poly([[RX - RW + 2, -104], [RX + RW - 2, -104], [RX + RW, 392], [RX - RW, 392]]);
  parts.push(BK(rBody, { fill: "@tower-r" }));
  parts.push(BK(rBody, { fill: "@tower-r", f: "stone", op: 0.5 }));
  parts.push(BK(poly([[RX - RW + 2, -104], [RX - RW + 4.4, -104], [RX - RW + 2.4, 392], [RX - RW, 392]]), { fill: "@rim", op: 0.4 }));
  const rw = coursedWall(RX - RW, -104, RW * 2, 496, 13, 23);
  parts.push(BK(rw.mortar, { fill: MORTAR, op: 0.55 }));
  parts.push(BK(rw.chips, { fill: "@chip", op: 0.35 }));
  // The broken crown, drawn as one wandering profile rather than a tidy row.
  parts.push(
    BK(
      poly([
        [246, -86], [246, -124], [254, -124], [254, -112], [262, -112], [262, -130],
        [271, -130], [271, -108], [278, -100], [286, -114], [293, -114], [293, -128],
        [302, -128], [302, -86],
      ]),
      { fill: "@parapet-r" },
    ),
  );
  parts.push(
    BK(rect(246, -124, 8, 2) + rect(262, -130, 9, 2) + rect(286, -114, 7, 2) + rect(293, -128, 9, 2), {
      fill: "@rim",
      op: 0.7,
    }),
  );
  parts.push(BK(corbelRow(244, 304, -86, 9, 8), { fill: "@tower-dk" }));
  // Rubble where the crown came down.
  const rubble = [];
  for (let i = 0; i < 7; i++) rubble.push(blob(272 + g() * 28, -102 + g() * 8, 2 + g() * 2.6, { squash: 0.7, seed: 60 + i }));
  parts.push(BK(rubble.join(""), { fill: "@tower-dk" }));
  // A timber hoarding bolted to the outside — the wooden gallery a garrison
  // hangs off a wall so it can drop things down the face of it.
  parts.push(BK(poly([[299, -70], [320, -70], [318, -52], [299, -52]]), { fill: "@timber" }));
  parts.push(BK(rect(297, -74, 25, 5), { fill: "@timber-lit" }));
  parts.push({ d: "M300 -65L319 -65M300 -59L318.4 -59", z: "back", stroke: "#221a12", sw: 0.9, op: 0.75 });
  parts.push(BK(poly([[299, -52], [308, -52], [299, -38]]) + poly([[301, -74], [301, -84], [297, -84]]), { fill: "@timber" }));
  parts.push(BK(loop(297, -98, 15), { fill: "#0b0e18" }));
  // A raven up there, watching.
  parts.push(
    BK(
      oval(288, -135, 6.4, 4.2, -14) + oval(293.5, -140, 2.8, 2.6) + poly([[296, -140.6], [301, -139.4], [296, -138.6]]) + poly([[283, -135.6], [275, -130], [283, -132.4]]),
      { fill: "#0b0e17" },
    ),
  );
  // Ivy taking the shaded flank, hugging the stone rather than floating off it.
  const ivy = [];
  const leaf = [];
  for (let k = 0; k < 3; k++) {
    const x0 = 289 + k * 4;
    const pts = [];
    for (let y = 150; y > -58; y -= 16) pts.push([x0 + Math.sin(y / 23 + k * 1.4) * 3.6, y]);
    ivy.push(smooth(pts));
    pts.forEach((p, i) => {
      leaf.push(oval(p[0] + (i % 2 ? 4.6 : -4.6), p[1], 4.4, 3.1, (i % 2 ? 1 : -1) * 26));
    });
  }
  parts.push({ d: ivy.join(""), z: "back", stroke: "#1f321c", sw: 1.4, op: 0.9 });
  parts.push(BK(leaf.join(""), { fill: "@ivy" }));

  // ── plane 1: the wall across the brow ──────────────────────────────────
  // Nearest, so the darkest darks and the warmest lights in the frame. Merlons
  // of varying width and height, one of them half knocked away.
  parts.push(P(rect(-12, -18, 296, 58), { fill: "@wall" }));
  const mer = [];
  const merRim = [];
  const merShade = [];
  let mx = -12;
  let mi = 0;
  while (mx < 286) {
    const w = 20 + Math.round(g() * 8);
    const top = -42 - Math.round(g() * 7);
    if (mi === 5) {
      mer.push(poly([[mx, -18], [mx, top + 8], [mx + 5, top + 13], [mx + 9, top + 4], [mx + w * 0.55, top + 17], [mx + w, top + 10], [mx + w, -18]]));
      merRim.push(poly([[mx, top + 8], [mx + 5, top + 13], [mx + 9, top + 4], [mx + 9, top + 6.4], [mx + 5, top + 15.4], [mx, top + 10.4]]));
    } else {
      mer.push(rect(mx, top, w, -18 - top));
      merRim.push(rect(mx, top, w, 2.4));
      merShade.push(rect(mx + w - 4, top, 4, -18 - top));
    }
    mx += w + 11 + Math.round(g() * 4);
    mi++;
  }
  parts.push(P(mer.join(""), { fill: "@wall" }));
  parts.push(P(rect(-12, -18, 296, 2.4), { fill: "@rim", op: 0.8 }));
  parts.push(P(merRim.join(""), { fill: "@rim", op: 0.9 }));
  parts.push(P(merShade.join(""), { fill: "#1a1f36", op: 0.5 }));
  // Putlog holes: the sockets the scaffold poles sat in while it was built,
  // never filled, a couple still with the stub of a beam rotting in them. A
  // long band of wall needs a rhythm across it that is not the coursing, and
  // this is the one such a wall would actually have.
  const putlog = [];
  const stubs = [];
  for (let i = 0; i < 11; i++) {
    const px = 4 + i * 25 + Math.round(g() * 7);
    putlog.push(rect(px, -6 + Math.round(g() * 3), 6, 5.4));
    if (i === 2 || i === 7) stubs.push(rect(px + 0.8, -4 + Math.round(g() * 3), 4.4, 3.4));
  }
  parts.push(P(putlog.join(""), { fill: "#0d1122", op: 0.85 }));
  parts.push(P(stubs.join(""), { fill: "@timber" }));
  // Quoins: the big dressed corner stones, alternating long and short.
  const quoin = [];
  for (let i = 0; i < 5; i++) {
    const y = -14 + i * 11;
    quoin.push(rect(-12, y, i % 2 ? 9 : 15, 9.6), rect(i % 2 ? 275 : 269, y, i % 2 ? 9 : 15, 9.6));
  }
  parts.push(P(quoin.join(""), { fill: "@quoin", op: 0.5 }));
  const ww = coursedWall(-12, -50, 296, 90, 14, 17);
  parts.push(P(ww.mortar, { fill: MORTAR, op: 0.6 }));
  parts.push(P(ww.chips, { fill: "@chip", op: 0.5 }));
  parts.push(P(rect(-12, -18, 296, 58) + mer.join(""), { fill: "@wall", f: "stone", op: 0.5 }));
  // The string course near the foot of the wall, and the dirt under it.
  parts.push(P(rect(-14, 14, 300, 7), { fill: "@ledge" }));
  parts.push(P(rect(-14, 14, 300, 2), { fill: "@rim", op: 0.9 }));
  parts.push(P(rect(-14, 21, 300, 4), { fill: "#0d1122", op: 0.65 }));
  parts.push(P(streaks(-12, 284, 24, 41, { n: 28, len: 15, w: 1.8 }), { fill: "@stain", op: 0.55 }));
  parts.push(P(loop(52, -12, 22) + loop(198, -12, 22) + loop(244, -8, 16), { fill: "#0b0e18" }));
  parts.push(P(rect(48.6, -12, 1.4, 22) + rect(194.6, -12, 1.4, 22) + rect(240.6, -8, 1.4, 16), { fill: "@rim", op: 0.5 }));
  // Lichen, and only where the rain drives: the upper left.
  const lichen = [];
  for (let i = 0; i < 24; i++) lichen.push(blob(-8 + g() * 150, -44 + g() * 56, 2 + g() * 4.5, { squash: 0.6, wob: 0.45, seed: 90 + i }));
  parts.push(P(lichen.join(""), { fill: "@lichen", op: 0.28 }));
  // Machicolation under the wall too, so its lower edge is toothed rather than
  // ruled — a straight line across a card is the thing that says "border".
  parts.push(P(corbelRow(-12, 284, 39, 12, 12), { fill: "@wall-dk" }));
  parts.push(P(rect(-12, 35, 296, 4), { fill: "#0d1122", op: 0.55 }));
  // A raven on the wall, and a rope hitched to an iron ring beside it.
  parts.push(
    P(
      oval(214, -50, 5.6, 3.8, -12) + oval(218.6, -54, 2.5, 2.3) + poly([[221, -54.4], [225, -53.4], [221, -52.6]]) + poly([[210, -50.6], [203, -46], [210, -48]]),
      { fill: "#0b0e17" },
    ),
  );
  parts.push({ d: dot(258, -6, 3.2), z: "front", stroke: IRON_L, sw: 1.5 });
  parts.push({ d: "M258 -3Q262 10 259 24Q256 34 260 39", z: "front", stroke: "#42341f", sw: 1.6, op: 0.9, a: "sway-slow", or: "258px -3px" });

  // ── plane 1: the piers ─────────────────────────────────────────────────
  // Weathered differently on purpose. The left has taken the water and gone
  // green at the foot; the right has split from top to bottom and been patched
  // once, in stone that has not had time to darken.
  for (const [x0, w, ch, seed] of [
    [0, 17, 17, 9],
    [255, 17, 15, 33],
  ]) {
    parts.push(P(rect(x0, 36, w, 356), { fill: "@rail" }));
    parts.push(P(rect(x0, 36, w, 356), { fill: "@rail", f: "grit", op: 0.5 }));
    const c = coursedWall(x0, 40, w, 352, ch, seed);
    parts.push(P(c.mortar, { fill: MORTAR, op: 0.6 }));
    parts.push(P(c.chips, { fill: "@chip", op: 0.5 }));
  }
  parts.push(P(rect(0, 36, 2, 356), { fill: "@rim", op: 0.45 }));
  parts.push(P(rect(255, 36, 1.6, 356), { fill: "@rim", op: 0.35 }));
  // The battered foot: both piers flare outward before they meet the ground.
  parts.push(P(poly([[0, 322], [17, 322], [17, 396], [-14, 396]]), { fill: "@rail" }));
  parts.push(P(poly([[0, 322], [17, 322], [17, 324.4], [-1.4, 324.8]]), { fill: "@rim", op: 0.6 }));
  parts.push(P(poly([[272, 338], [255, 338], [255, 396], [286, 396]]), { fill: "@rail" }));
  parts.push(P(poly([[272, 338], [255, 338], [255, 340.4], [272.6, 340.8]]), { fill: "@rim", op: 0.45 }));
  parts.push(P(coursedWall(-13, 326, 30, 68, 15, 91).mortar + coursedWall(255, 342, 30, 54, 14, 93).mortar, { fill: MORTAR, op: 0.45 }));
  // Left: moss climbing out of the joints, heaviest at the bottom.
  const moss = [];
  for (let i = 0; i < 18; i++) {
    const t = i / 18;
    moss.push(blob(1 + g() * 14, 200 + t * 190 + g() * 12, 2.5 + t * 5 + g() * 2.5, { squash: 0.55, wob: 0.45, seed: 120 + i }));
  }
  parts.push(P(moss.join(""), { fill: "@moss", op: 0.6 }));
  parts.push({ d: tuft(9, 388, 13, 7, 55), z: "front", stroke: "#3a5430", sw: 1.2, op: 0.85, a: "sway", or: "9px 388px" });
  // Right: the split, and the patch.
  parts.push({ d: crack(263, 60, 190, 77, { spread: 34, segs: 13 }), z: "front", stroke: "#0f1424", sw: 1.5, op: 0.9 });
  parts.push({ d: crack(259, 300, 62, 71, { spread: 26, segs: 6 }), z: "front", stroke: "#0f1424", sw: 1.2, op: 0.6 });
  parts.push(P(rect(255, 262, 17, 46), { fill: "@patch", op: 0.85 }));
  parts.push(P(coursedWall(255, 262, 17, 46, 11, 44).mortar, { fill: MORTAR, op: 0.55 }));
  // Iron tie plates, three on one side and one on the other.
  const ties = [[8, 122], [8, 224], [8, 326], [263, 196]];
  parts.push(P(ties.map(([tx, ty]) => poly([[tx, ty - 5], [tx + 5, ty], [tx, ty + 5], [tx - 5, ty]])).join(""), { fill: "@iron" }));
  parts.push(P(ties.map(([tx, ty]) => dot(tx, ty, 1.4)).join(""), { fill: IRON_L, op: 0.85 }));

  // Torches, at two different heights because a matched pair reads as pattern.
  for (const [x, y, dl] of [
    [11, 96, 0],
    [263, 168, -0.62],
  ]) {
    // The wash of light on the stone, then the soot the flame has left above.
    parts.push(P(oval(x, y - 14, 34, 54), { fill: "@torch", a: "glow", dl, op: 0.95 }));
    parts.push(P(oval(x, y - 18, 15, 26), { fill: "@torch", a: "glow", dl: dl - 0.4, op: 0.8 }));
    parts.push(P(poly([[x - 7, y - 24], [x + 7, y - 24], [x + 4, y - 76], [x - 4, y - 76]]), { fill: "@soot", op: 0.5 }));
    // Bracket: an arm off the wall with a scrolled stay under it.
    parts.push(P(rect(x - 8, y - 4, 16, 4.4), { fill: "@iron" }));
    parts.push({ d: `M${x - 6} ${y + 13}Q${x - 1} ${y + 9} ${x + 1} ${y}`, z: "front", stroke: "#171b25", sw: 2 });
    parts.push(P(poly([[x - 6.5, y - 22], [x + 6.5, y - 22], [x + 4.5, y - 3], [x - 4.5, y - 3]]), { fill: "@iron" }));
    parts.push({ d: `M${x - 6} ${y - 17}L${x + 6} ${y - 17}M${x - 5.4} ${y - 11}L${x + 5.4} ${y - 11}`, z: "front", stroke: IRON_L, sw: 1.1, op: 0.7 });
    parts.push(P(flame(x, y - 20, 30, 8, 5), { fill: "#e8571a", a: "flick", or: `${x}px ${y - 20}px`, dl }));
    parts.push(P(flame(x, y - 21, 22, 5.4, 9), { fill: "#ffa22c", a: "flick", or: `${x}px ${y - 21}px`, dl: dl - 0.18 }));
    parts.push(P(flame(x, y - 22, 12, 3, 13), { fill: "#ffeab0", a: "flick", or: `${x}px ${y - 22}px`, dl: dl - 0.34 }));
    for (let k = 0; k < 3; k++) {
      parts.push(P(dot(x + (k - 1) * 3, y - 26, 1.1 + k * 0.3), { fill: "#ffc25e", a: "float-up", dl: dl - k * 2.6, op: 0.9 }));
    }
  }

  // ── the foot ───────────────────────────────────────────────────────────
  parts.push(P(rect(-38, 372, 348, 9), { fill: "@ledge" }));
  parts.push(P(rect(-38, 372, 348, 2.2), { fill: "@rim", op: 0.9 }));
  parts.push(P(rect(-38, 381, 348, 4), { fill: "#0d1122", op: 0.65 }));
  parts.push(P(poly([[-36, 383], [308, 383], [318, 424], [-46, 424]]), { fill: "@plinth" }));
  parts.push(P(poly([[-36, 383], [308, 383], [318, 424], [-46, 424]]), { fill: "@plinth", f: "stone", op: 0.5 }));
  const pj = [];
  for (let px = -40; px < 310; px += 24 + g() * 10) pj.push(rect(px, 385, 1.3, 39));
  parts.push(P(pj.join(""), { fill: MORTAR, op: 0.7 }));
  parts.push(P(streaks(-36, 308, 385, 63, { n: 15, len: 17, w: 2.2 }), { fill: "@stain", op: 0.32 }));
  // Fallen stones and weeds, more on the left than the right.
  const fallen = [];
  for (let i = 0; i < 9; i++) fallen.push(blob(-14 + g() * 130, 396 + g() * 18, 3 + g() * 5, { squash: 0.62, wob: 0.35, seed: 150 + i }));
  for (let i = 0; i < 4; i++) fallen.push(blob(230 + g() * 70, 404 + g() * 12, 3 + g() * 3.4, { squash: 0.6, wob: 0.35, seed: 170 + i }));
  parts.push(P(fallen.join(""), { fill: "@rubble" }));
  parts.push({
    d: tuft(26, 396, 15, 8, 61) + tuft(62, 398, 12, 6, 67) + tuft(120, 394, 10, 5, 73) + tuft(248, 400, 11, 5, 79),
    z: "front",
    stroke: "#37502d",
    sw: 1.2,
    op: 0.85,
    a: "sway",
    or: "136px 400px",
  });

  parts.push(...occlusion());

  return {
    id: "castle-keep",
    name: "Castle keep",
    group: "Stone & stage",
    // A roofed tower and a ruined one behind the card, a battlemented wall
    // across its brow, torches burning on the piers and the whole thing lit by
    // a sun already under the horizon.
    grads: [
      // Weak on purpose. It is a medium for the pale far ridges to sit in, not
      // a backdrop: any stronger and it reads as a grey halo painted round the
      // card, which is exactly what it looked like at 0.9.
      glow("sky", [
        [0, "#334066", 0.62],
        [0.5, "#252c49", 0.34],
        [1, "#191e31", 0],
      ]),
      glow("dusk", [
        [0, "#e79753", 0.66],
        [0.42, "#a4653c", 0.32],
        [1, "#6b3f2c", 0],
      ]),
      lin("ridge-far", 0, -76, 0, 14, [
        [0, "#7b87a6", 0.55],
        [0.72, "#5f6a8d", 0.5],
        [1, "#5f6a8d", 0],
      ]),
      lin("ridge-near", 0, -50, 0, 14, [
        [0, "#4d5779", 0.92],
        [0.76, "#3a4368", 0.88],
        [1, "#3a4368", 0],
      ]),
      lin("far", 0, -104, 0, -6, [
        [0, "#4e5779", 0.95],
        [0.86, "#3f4869", 0.9],
        [1, "#3f4869", 0],
      ]),
      lin("far-lit", 0, -104, 0, -10, [
        [0, "#6e779a"],
        [1, "#5a6386"],
      ]),
      // Plane 2. Bluer, paler and lower in contrast than the wall in front of
      // it — the towers are twenty metres further off and the air says so.
      rock("tower", "#252b44", "#4a506a", "#a08f76"),
      rock("tower-r", "#222840", "#454b63", "#93836f"),
      lin("tower-lit", 0, -60, 0, 110, [
        [0, "#8f8879"],
        [1, "#6f7183"],
      ]),
      lin("tower-dk", 0, -150, 0, -80, [
        [0, "#2f3550"],
        [1, "#262b44"],
      ]),
      rock("parapet", "#2b3149", "#565c76", "#b6a184"),
      rock("parapet-r", "#272d45", "#4e5470", "#a4917a"),
      plane("roof", [
        [0, "#8089a6"],
        [0.35, "#3d445e"],
        [1, "#1d2233"],
      ]),
      // Plane 1. The warmest lights and the coldest darks in the frame.
      plane("wall", [
        [0, "#d2ae7c"],
        [0.26, "#9c8a70"],
        [0.58, "#5a5a6c"],
        [1, "#2b3048"],
      ]),
      lin("wall-dk", 0, 30, 0, 56, [
        [0, "#3f4356"],
        [1, "#232842"],
      ]),
      lin("quoin", 0, -20, 0, 40, [
        [0, "#c2a67d"],
        [1, "#8b8070"],
      ]),
      // A ledge is a horizontal face: it catches the sky, so its top edge is
      // the brightest mark on the frame and its underside the darkest.
      // Horizontal, not vertical: a ledge runs the whole width of the frame, so
      // what has to change along it is how much light is left by the time the
      // sun has crossed it. A single flat tone over 300 units is the mark that
      // made every band in this frame read as a plank.
      lin("ledge", -16, 0, 288, 0, [
        [0, "#b1966e"],
        [0.34, "#7d735f"],
        [0.72, "#4e4d5c"],
        [1, "#343850"],
      ]),
      rock("rail", "#1f2438", "#57534f", "#bb9a70"),
      plane("plinth", [
        [0, "#a68a64"],
        [0.3, "#736c5f"],
        [0.66, "#4a4a5b"],
        [1, "#252a41"],
      ]),
      lin("patch", 0, 262, 0, 308, [
        [0, "#9c9484"],
        [1, "#7a7469"],
      ]),
      lin("rubble", 0, 390, 0, 424, [
        [0, "#6b6660"],
        [1, "#3a3e52"],
      ]),
      forged("iron", "#0f121a", "#22262f", "#5b6479"),
      bark("timber", "#241a11", "#463523", "#7d6242"),
      lin("timber-lit", 0, -80, 0, -60, [
        [0, "#8a6d4a"],
        [1, "#5e4830"],
      ]),
      lin("mortar", 0, 0, 0, 400, [
        [0, "#2b3040", 0.9],
        [1, "#1e222d", 0.9],
      ]),
      // A fresh break shows the stone's inside, which is paler and warmer than
      // any weathered face on it.
      lin("chip", 0, -60, 0, 424, [
        [0, "#dcc79f", 0.9],
        [1, "#9d8f78", 0.9],
      ]),
      lin("rim", 0, -260, 0, 424, [
        [0, "#f6e2b8", 0.95],
        [0.5, "#e0c89c", 0.9],
        [1, "#bfa984", 0.8],
      ]),
      wash("stain", [
        [0, "#12172a", 0.8],
        [1, "#12172a", 0],
      ]),
      lin("moss", 0, 190, 0, 400, [
        [0, "#496a39"],
        [1, "#2c4124"],
      ]),
      lin("lichen", 0, -50, 0, 40, [
        [0, "#c6ccaa"],
        [1, "#909c7a"],
      ]),
      lin("ivy", 0, -60, 0, 160, [
        [0, "#2f4a29"],
        [1, "#1a2716"],
      ]),
      glow("torch", [
        [0, "#ffb24d", 0.5],
        [0.45, "#e07a2c", 0.2],
        [1, "#c2521c", 0],
      ]),
      wash("soot", [
        [0, "#0a0c14", 0],
        [1, "#0a0c14", 0.85],
      ]),
      ...occlusionGrads("#070a12"),
    ],
    filters: [
      grain("stone", "0.85 1.05", { oct: 4, seed: 11, k: 0.9 }),
      grain("grit", "1.5 1.7", { oct: 3, seed: 5, k: 0.85 }),
    ],
    parts,
  };
})();

const hallowsEve = (() => {
  // A churchyard at moonrise. The old frame was purple-black everywhere, and
  // flat black is what makes a night scene read as a sticker: real moonlight is
  // a cold blue-green, it rims every silhouette on the side facing it, and the
  // only warm thing in the whole picture is the one candle somebody left.
  const parts = [];
  const g = rnd(43);

  // ── plane 4: the moon, and the cloud going past it ─────────────────────
  parts.push(BK(oval(202, -62, 150, 130), { fill: "@moonhaze" }));
  parts.push(BK(dot(202, -58, 44), { fill: "@moon" }));
  parts.push(
    BK(
      blob(190, -72, 12, { squash: 0.9, wob: 0.4, seed: 2 }) +
        blob(216, -44, 9, { squash: 0.85, wob: 0.45, seed: 5 }) +
        blob(212, -76, 6.4, { squash: 0.9, wob: 0.4, seed: 8 }) +
        blob(186, -44, 7, { squash: 0.8, wob: 0.5, seed: 11 }),
      { fill: "@maria", op: 0.5 },
    ),
  );
  parts.push(BK(dot(202, -58, 44), { fill: "@moonrim", op: 0.55 }));
  // Cloud, as masses rather than as ribbons: a sine band across the whole
  // width reads as water, and it read as water here for exactly that reason.
  for (const [cx, cy, r, op, cls, dl] of [
    [188, -104, 30, 0.5, "lap-far", 0],
    [232, -98, 22, 0.44, "lap-far", 0],
    [150, -46, 26, 0.34, "lap", -2.6],
    [252, -34, 20, 0.3, "lap", -2.6],
    [66, -78, 24, 0.26, "lap-far", -5.2],
  ]) {
    parts.push(
      BK(blob(cx, cy, r, { squash: 0.38, wob: 0.3, n: 16, seed: cx }) + blob(cx + r * 0.9, cy + 4, r * 0.7, { squash: 0.4, wob: 0.35, n: 14, seed: cx + 7 }), {
        fill: "@cloud",
        op,
        a: cls,
        dl,
      }),
    );
  }

  // ── plane 3: the village on the far side of the wall ───────────────────
  const village = [
    rect(46, -46, 26, 40),
    tri([42, -46], [76, -46], [59, -74]),
    rect(57, -92, 4, 20),
    rect(76, -32, 34, 26),
    tri([72, -32], [114, -32], [93, -48]),
    rect(-14, -26, 30, 20),
    tri([-18, -26], [20, -26], [1, -42]),
  ];
  parts.push(BK(village.join(""), { fill: "@far" }));
  parts.push(BK(poly([[42, -46], [59, -74], [61, -71], [46, -46]]) + poly([[72, -32], [93, -48], [95, -45], [77, -32]]) + poly([[-18, -26], [1, -42], [3, -39], [-14, -26]]), { fill: "@moonrim", op: 0.3 }));
  parts.push(BK(rect(52, -36, 3.4, 5) + rect(86, -24, 3.4, 5), { fill: "#ffb347", op: 0.8, a: "glow", dl: -1.9 }));
  // The far rank of headstones, hazed almost to nothing.
  const farStones = [];
  for (let i = 0; i < 22; i++) {
    const x = -20 + i * 14 + g() * 6;
    const h = 6 + g() * 8;
    farStones.push(poly([[x, -4], [x, -4 - h], [x + 5, -8 - h], [x + 10, -4 - h], [x + 10, -4]]));
  }
  parts.push(BK(farStones.join(""), { fill: "@far-stone", op: 0.55 }));

  // ── plane 2: the trees ─────────────────────────────────────────────────
  // Two on the left grown into each other, one thin one on the right. Every
  // limb forks twice, because a branch that forks once is a candlestick.
  const bough = [];
  const rim = [];
  const grow = (x, y, a, len, w0, curve, depth) => {
    const b = limb(x, y, a, len, w0, w0 * 0.42, curve, 7);
    bough.push(b.d);
    // The moon is up and to the right, so every limb takes a thread of light on
    // that side and nothing on the other.
    rim.push(limb(x + 0.9, y - 0.9, a, len, w0 * 0.32, w0 * 0.14, curve, 7).d);
    if (depth <= 0) return;
    const [tx, ty] = b.tip;
    grow(tx, ty, b.ang - 24 - g() * 16, len * 0.62, w0 * 0.5, curve * 0.7, depth - 1);
    grow(tx, ty, b.ang + 20 + g() * 18, len * 0.54, w0 * 0.44, -curve * 0.6, depth - 1);
    const mid = b.spine[4];
    if (depth > 1) grow(mid[0], mid[1], b.ang + (g() > 0.5 ? 46 : -46), len * 0.4, w0 * 0.32, curve * 0.5, depth - 1);
  };
  // Rooted off the bottom corners of the top band and reaching up out of it:
  // grown from halfway down the card, every limb was hidden behind it and all
  // that showed were stubs poking past the edges.
  grow(-56, 52, -66, 104, 5.6, 26, 3);
  grow(-38, 40, -84, 78, 4, 40, 2);
  grow(330, 40, -114, 98, 5, -24, 3);
  grow(310, 30, -98, 68, 3.6, -38, 2);
  parts.push(BK(bough.join(""), { fill: "@bark" }));
  parts.push(BK(rim.join(""), { fill: "@moonrim", op: 0.35 }));

  // Bats, in three sizes on three clocks.
  const bat = (s) =>
    poly([
      [-9 * s, -1 * s], [-5.4 * s, -4.4 * s], [-2.4 * s, -1.4 * s], [0, -4.4 * s],
      [2.4 * s, -1.4 * s], [5.4 * s, -4.4 * s], [9 * s, -1 * s], [6 * s, 0.4 * s],
      [4.4 * s, 2 * s], [1.6 * s, 0.6 * s], [0, 1.6 * s], [-1.6 * s, 0.6 * s],
      [-4.4 * s, 2 * s], [-6 * s, 0.4 * s],
    ]);
  parts.push(BK(bat(1.6), { fill: "@bat", a: "cross", or: "0px 0px", dl: 0, op: 0.95 }));
  parts.push(BK(bat(1.05), { fill: "@bat", a: "cross-high", or: "0px 0px", dl: -5.5, op: 0.8 }));
  parts.push(BK(bat(0.7), { fill: "@bat", a: "cross", or: "0px 0px", dl: -11, op: 0.6 }));

  // ── plane 1: the churchyard railing across the brow ────────────────────
  const bars = [];
  const spears = [];
  const rust = [];
  for (let i = 0; i < 26; i++) {
    const x = -10 + i * 12;
    if (i === 9) continue; // one gone entirely
    const lean = i === 10 ? 5 : i === 17 ? -3 : 0;
    bars.push(poly([[x, -18 + lean * 0.4], [x + 3, -18 + lean * 0.4], [x + 3 - lean, 34], [x - lean, 34]]));
    spears.push(poly([[x - 2.4, -18], [x + 5.4, -18], [x + 1.5, -30]]));
    if (g() > 0.55) rust.push(blob(x + 1.5, 8 + g() * 22, 1.8 + g() * 2.4, { squash: 0.8, wob: 0.5, seed: 30 + i }));
  }
  parts.push(P(bars.join(""), { fill: "@iron" }));
  parts.push(P(spears.join(""), { fill: "@iron" }));
  parts.push(P(rect(-12, -14, 296, 5) + rect(-12, 22, 296, 5), { fill: "@iron" }));
  parts.push(P(rect(-12, -14, 296, 1.6) + rect(-12, 22, 296, 1.6), { fill: "@moonrim", op: 0.5 }));
  parts.push(P(rust.join(""), { fill: "@rust", op: 0.45 }));
  // Two gate posts, one leaning, and a crow on the rail.
  parts.push(P(rect(-16, -30, 12, 70) + poly([[-18, -30], [-2, -30], [-10, -44]]), { fill: "@stone" }));
  parts.push(P(rect(276, -26, 12, 66) + poly([[274, -26], [290, -26], [282, -40]]), { fill: "@stone" }));
  parts.push(P(rect(-16, -30, 3, 70) + rect(276, -26, 3, 66), { fill: "@moonrim", op: 0.35 }));
  parts.push(
    P(
      oval(150, -26, 7.4, 5, -10) + oval(156, -32, 3.2, 3) + poly([[159, -32.6], [165, -31.2], [159, -30]]) + poly([[143, -26.6], [133, -20], [143, -23]]),
      { fill: "@bat" },
    ),
  );
  parts.push(P(dot(157, -32.8, 1) , { fill: "#ffc46a", a: "twinkle", op: 0.9 }));

  // Webs, and a spider coming down out of the corner.
  parts.push({ d: web(-6, -34, 86, 0, 90), z: "front", stroke: "@web", sw: 0.8, op: 0.45 });
  parts.push({ d: web(278, -34, 74, 90, 180), z: "front", stroke: "@web", sw: 0.8, op: 0.35 });
  parts.push({ d: web(-6, 410, 58, -90, 0), z: "front", stroke: "@web", sw: 0.8, op: 0.28 });
  parts.push({ d: "M244 -14L244 44", z: "front", stroke: "@web", sw: 0.8, op: 0.5, a: "abseil-line", or: "244px -14px" });
  parts.push(P(oval(244, 51, 5, 6) + dot(244, 44, 3.4), { fill: "@bat", a: "abseil" }));
  parts.push({
    d: "M238 48L230 42M238 52L228 53M250 48L258 42M250 52L260 53",
    z: "front",
    stroke: "@bat",
    sw: 1.4,
    a: "abseil",
  });

  // ── the sides: dead vine on the rails ──────────────────────────────────
  for (const [x, dir] of [
    [5, 1],
    [267, -1],
  ]) {
    const v = limb(x, 34, 90, 372, 3.4, 2.4, dir * 3, 12);
    parts.push(P(v.d, { fill: "@bark" }));
    parts.push(P(limb(x + dir * 0.9, 34, 90, 372, 1.1, 0.8, dir * 3, 12).d, { fill: "@moonrim", op: 0.28 }));
    const thorns = [];
    for (let i = 0; i < 22; i++) {
      const y = 44 + i * 16 + g() * 6;
      const s = dir * (i % 3 ? 1 : -1);
      thorns.push(limb(x, y, s > 0 ? 26 : 154, 5 + g() * 4, 1.3, 0.2, s * -30, 3).d);
    }
    parts.push(P(thorns.join(""), { fill: "@bark" }));
  }

  // ── the foot: stones, fog, litter and the lantern ──────────────────────
  const stones = [
    [-8, 398, 24, 44, 0, 0],
    [26, 404, 17, 32, -7, 1],
    [232, 400, 21, 38, 4, 2],
    [262, 406, 15, 28, 0, 1],
    [120, 412, 13, 22, -3, 0],
  ];
  for (const [x, y, w, h, tilt, kind] of stones) {
    const dx = (tilt * h) / 40;
    const top =
      kind === 0
        ? poly([[x, y], [x + dx, y - h], ...arcPts(x + dx + w / 2, y - h, w / 2, 180, 360, 8), [x + w + dx, y - h], [x + w, y]])
        : kind === 1
          ? poly([[x, y], [x + dx, y - h], [x + dx + w / 2, y - h - 5], [x + w + dx, y - h], [x + w, y]])
          : poly([[x, y], [x + dx, y - h * 0.8], [x + dx + w * 0.3, y - h], [x + w + dx, y - h * 0.7], [x + w, y]]);
    parts.push(P(top, { fill: "@stone" }));
    parts.push(P(top, { fill: "@stone", f: "grit", op: 0.5 }));
    parts.push(P(poly([[x, y], [x + dx, y - h * 0.9], [x + dx + 3.4, y - h * 0.9], [x + 3.4, y]]), { fill: "@moonrim", op: 0.4 }));
    parts.push({
      d: `M${r2(x + w * 0.28)} ${r2(y - h * 0.62)}L${r2(x + w * 0.72)} ${r2(y - h * 0.62)}M${r2(x + w * 0.3)} ${r2(y - h * 0.44)}L${r2(x + w * 0.68)} ${r2(y - h * 0.44)}M${r2(x + w * 0.34)} ${r2(y - h * 0.26)}L${r2(x + w * 0.6)} ${r2(y - h * 0.26)}`,
      z: "front",
      stroke: "#0d1018",
      sw: 1.1,
      op: 0.55,
    });
    if (kind === 2) parts.push({ d: crack(x + w * 0.5, y - h * 0.7, h * 0.7, 90 + x, { spread: 40, segs: 5 }), z: "front", stroke: "#0b0e15", sw: 1.2, op: 0.8 });
  }
  // Leaf litter, then the fog laid over the foot of everything — fog behind a
  // headstone is what puts the headstone in a place rather than on a card.
  const litter = [];
  for (let i = 0; i < 30; i++) {
    const x = -20 + g() * 312;
    const y = 396 + g() * 26;
    litter.push(oval(x, y, 3.4 + g() * 2.6, 2 + g() * 1.4, g() * 180));
  }
  parts.push(P(litter.join(""), { fill: "@leaf", op: 0.75 }));
  parts.push(P(waveTop(-40, 312, 396, 6, 128, 0.4, 428), { fill: "@fog", op: 0.4, a: "lap-far" }));
  parts.push(P(waveTop(-40, 312, 406, 5, 92, 2.3, 428), { fill: "@fog", op: 0.5, a: "lap" }));
  parts.push(P(waveTop(-40, 312, 416, 4, 66, 4.1, 428), { fill: "@fog", op: 0.45, a: "lap-far", dl: -3 }));

  // The lantern: the one warm thing in the frame, so it gets the most work.
  const LX = 62;
  const LY = 410;
  parts.push(P(oval(LX, LY - 4, 46, 34), { fill: "@lampglow", a: "glow", op: 0.9 }));
  parts.push(P(oval(LX, LY, 21, 18), { fill: "@pumpkin" }));
  parts.push(P(oval(LX - 11, LY, 9, 17.4), { fill: "@pumpkin-d", op: 0.5 }));
  parts.push(P(oval(LX + 11, LY, 9, 17.4), { fill: "@pumpkin-d", op: 0.5 }));
  parts.push(P(oval(LX - 20, LY, 4, 15), { fill: "@pumpkin-d", op: 0.7 }));
  parts.push(P(oval(LX + 20, LY, 4, 15), { fill: "@pumpkin-d", op: 0.7 }));
  parts.push(P(poly([[LX - 3, LY - 20], [LX + 2, LY - 20], [LX + 3, LY - 30], [LX - 1, LY - 30]]) + oval(LX + 1, LY - 30, 5, 3, -20), { fill: "@stalk" }));
  parts.push(
    P(
      poly([[LX - 12, LY - 5], [LX - 4, LY - 5], [LX - 8, LY - 12]]) +
        poly([[LX + 4, LY - 5], [LX + 12, LY - 5], [LX + 8, LY - 12]]) +
        poly([[LX - 3, LY + 1], [LX + 3, LY + 1], [LX, LY - 4]]) +
        poly([[LX - 15, LY + 8], [LX + 15, LY + 8], [LX + 11, LY + 15], [LX + 7, LY + 10], [LX + 2, LY + 15], [LX - 3, LY + 10], [LX - 8, LY + 15], [LX - 12, LY + 10]]),
      { fill: "@lampcut", a: "flick", or: `${LX}px ${LY}px` },
    ),
  );
  // A candle stub burning on the leaning stone, and one on the ground.
  for (const [cx, cy, h, dl] of [
    [34, 372, 11, 0],
    [246, 400, 8, -0.7],
  ]) {
    parts.push(P(dot(cx, cy - h, 17), { fill: "@candleglow", a: "glow", dl, op: 0.75 }));
    parts.push(P(rect(cx - 2.4, cy - h, 4.8, h), { fill: "@wax" }));
    parts.push(P(flame(cx, cy - h, 9, 2.2, 3), { fill: "#ffae3a", a: "flick", or: `${cx}px ${cy - h}px`, dl }));
    parts.push(P(flame(cx, cy - h, 5, 1.2, 7), { fill: "#fff0c6", a: "flick", or: `${cx}px ${cy - h}px`, dl: dl - 0.2 }));
  }
  // Will-o'-the-wisps drifting up out of the fog.
  for (const [x, dl, r] of [
    [12, 0, 2.2],
    [258, -2.8, 1.8],
    [96, -5.4, 1.5],
    [200, -7.9, 1.3],
  ]) {
    parts.push(P(dot(x, 396, r * 3.4), { fill: "@wisp", a: "float-up", dl, op: 0.55 }));
    parts.push(P(dot(x, 396, r), { fill: "#c8f0d8", a: "float-up", dl, op: 0.9 }));
  }

  return {
    id: "hallows-eve",
    name: "Hallow's eve",
    group: "After dark",
    // A moon coming up over a churchyard, bats crossing it, a railing along the
    // brow and a lantern burning in the fog at the foot.
    grads: [
      radial("moonhaze", 202, -62, 75, [
        [0, "#cfe0dd", 0.45],
        [0.45, "#8fa8ae", 0.16],
        [1, "#5d7381", 0],
      ]),
      lin("moon", 0, -102, 0, -14, [
        [0, "#f4f2e2"],
        [1, "#cfd6cf"],
      ]),
      lin("maria", 0, -102, 0, -14, [
        [0, "#cdd3c9"],
        [1, "#a9b3ad"],
      ]),
      // Moonlight itself: cold, slightly green, and used for every rim in the
      // frame so the whole picture agrees about where the light is.
      lin("moonrim", 0, -110, 0, 424, [
        [0, "#e6f0e8", 0.95],
        [0.5, "#a9c0be", 0.8],
        [1, "#7c9698", 0.7],
      ]),
      wash("cloud", [
        [0, "#7b8ea0", 0.85],
        [1, "#7b8ea0", 0],
      ]),
      lin("far", 0, -96, 0, -4, [
        [0, "#44516f"],
        [1, "#313c58"],
      ]),
      lin("far-stone", 0, -20, 0, -2, [
        [0, "#54627c"],
        [1, "#3c485e"],
      ]),
      // Bark, near-black but not black: it still turns, and the turn is what
      // keeps a limb from reading as a paper cut-out.
      bark("bark", "#0c0b14", "#1e1b2b", "#3a3550"),
      lin("bat", 0, -60, 0, 60, [
        [0, "#0a0912"],
        [1, "#15131f"],
      ]),
      rock("stone", "#1a1e2c", "#414a5e", "#8b9aa4"),
      forged("iron", "#0a0c13", "#1d222d", "#5f6d78"),
      lin("rust", 0, -30, 0, 40, [
        [0, "#7a4423"],
        [1, "#4e2b16"],
      ]),
      lin("web", 0, -40, 0, 420, [
        [0, "#cfe0e2", 0.9],
        [1, "#9fb0b6", 0.7],
      ]),
      lin("leaf", 0, 390, 0, 424, [
        [0, "#7a4a1e"],
        [0.5, "#5c3418"],
        [1, "#3a2313"],
      ]),
      wash("fog", [
        [0, "#8fa8b4", 0],
        [0.45, "#8fa8b4", 0.32],
        [1, "#a8bcc4", 0.55],
      ]),
      radial("lampglow", 62, 406, 46, [
        [0, "#ffa832", 0.6],
        [0.5, "#d9701c", 0.24],
        [1, "#8e3f10", 0],
      ]),
      tube("pumpkin", [
        [0, "#7a2c08"],
        [0.22, "#c9530f"],
        [0.34, "#ec7a1c"],
        [0.6, "#b8460d"],
        [1, "#6d2607"],
      ]),
      lin("pumpkin-d", 0, 392, 0, 428, [
        [0, "#a83c0a"],
        [1, "#782906"],
      ]),
      lin("stalk", 0, 376, 0, 396, [
        [0, "#6a7a34"],
        [1, "#3f4a1e"],
      ]),
      lin("lampcut", 0, 396, 0, 428, [
        [0, "#ffe9a8"],
        [1, "#ffb03c"],
      ]),
      glow("candleglow", [
        [0, "#ffcf8a", 0.6],
        [1, "#ffcf8a", 0],
      ]),
      lin("wax", 0, 356, 0, 404, [
        [0, "#e8dcc0"],
        [1, "#a89c84"],
      ]),
      glow("wisp", [
        [0, "#a8ecc4", 0.7],
        [1, "#a8ecc4", 0],
      ]),
    ],
    filters: [grain("grit", "1.4 1.6", { oct: 3, seed: 17, k: 0.85 })],
    parts,
  };
})();

const deepWoods = (() => {
  const BARK = "#4b3a2a";
  const BARK2 = "#33261b";
  const LEAF = ["#20502f", "#2d6b39", "#3f8a44"];
  const parts = [];

  // Canopy: three depths of leaf mass overhanging the top edge, behind the
  // card so the lowest lobes are cut by it. The lobes are deliberately uneven
  // — a row of equal blobs at one height is a green slab, not a canopy.
  const gc = rnd(21);
  for (let layer = 0; layer < 3; layer++) {
    const d = [];
    for (let x = -40; x <= 312; x += 26) {
      const lift = gc() * 26;
      d.push(
        blob(x + layer * 7 + gc() * 8, -46 + layer * 20 + lift, 17 + gc() * 13 - layer * 1.5, {
          squash: 0.78,
          wob: 0.3,
          seed: Math.round(x + layer * 97),
        }),
      );
    }
    parts.push(
      BK(d.join(""), {
        fill: LEAF[layer],
        a: layer ? "breeze" : "breeze-slow",
        or: "136px -60px",
        dl: -layer * 1.7,
      }),
    );
  }

  // Two trunks holding the sides up, in front of the card.
  for (const [x, dir] of [
    [7, 1],
    [265, -1],
  ]) {
    // Straight, and the bark is placed FROM THE SPINE. The first version gave
    // the trunk a 5° bend and then drew its bark at fixed x — over 430 units
    // that walked the trunk 19 units sideways, off its own texture and onto the
    // card's text.
    const t = limb(x, 406, -90, 434, 7.4, 7, 0, 8);
    parts.push(P(t.d, { fill: "@trunk" }));
    const bark = [];
    t.spine.forEach((p, i) => {
      bark.push(`M${r2(p[0] - 3.4 * dir)} ${r2(p[1])}q${r2(2.6 * dir)} 14 0 28`);
      if (i % 2) bark.push(`M${r2(p[0] + 2.6 * dir)} ${r2(p[1] + 14)}q${r2(-2 * dir)} 11 0 22`);
    });
    parts.push(SK(bark.join(""), { stroke: BARK2, sw: 1.3, op: 0.8 }));
    // One low bough per side, reaching in over the banner with a leaf cluster.
    const b = limb(x, 40, dir > 0 ? -22 : -158, 46, 3.6, 1.6, dir * 34, 6);
    parts.push(P(b.d, { fill: "@bough" }));
    parts.push(
      P(
        blob(b.tip[0], b.tip[1], 15, { squash: 0.68, seed: 40 + x }) +
          blob(b.tip[0] + 11 * dir, b.tip[1] + 7, 10, { squash: 0.7, seed: 60 + x }),
        { fill: LEAF[2], a: "sway", or: `${x}px 40px`, dl: dir > 0 ? 0 : -2.4 },
      ),
    );
  }

  // Roots and moss along the foot, with a couple of toadstools.
  const roots = [];
  for (let i = 0; i < 7; i++) {
    const x = -10 + i * 48;
    roots.push(limb(x, 414, i % 2 ? -18 : -162, 30, 5, 1.6, i % 2 ? 30 : -30, 5).d);
  }
  parts.push(P(roots.join(""), { fill: BARK2 }));
  parts.push(P(waveTop(-14, 286, 396, 5, 96, 0.6, 418), { fill: "#2b4a2a" }));
  parts.push(P(waveTop(-14, 286, 403, 4, 74, 2.1, 418), { fill: "#3c6335" }));
  for (const [x, r] of [
    [40, 8],
    [52, 5],
    [228, 7],
  ]) {
    parts.push(P(rect(x - r * 0.28, 400 - r, r * 0.56, r + 8), { fill: "#efe6d0" }));
    parts.push(P(oval(x, 400 - r, r, r * 0.66), { fill: "#c8534d" }));
    parts.push(P(dot(x - r * 0.4, 400 - r * 1.2, r * 0.17) + dot(x + r * 0.45, 400 - r * 0.95, r * 0.14), { fill: "#f6efe0" }));
  }

  // Fireflies: slow glowing drifts along the rails.
  for (const [x, y, dl] of [
    [22, 250, 0],
    [250, 190, -3.4],
    [30, 330, -6.1],
    [244, 300, -8.8],
  ]) {
    parts.push(P(dot(x, y, 6), { fill: "@spark", a: "hover", or: `${x}px ${y}px`, dl, op: 0.85 }));
    parts.push(P(dot(x, y, 1.8), { fill: "#fff6c2", a: "hover", or: `${x}px ${y}px`, dl }));
  }

  return {
    id: "deep-woods",
    name: "Deep woods",
    group: "Woodland",
    // Trunks either side, a canopy breathing over the top edge, roots and
    // toadstools underfoot, fireflies wandering the margins.
    grads: [
      glow("spark", [
        [0, "#ffe98a", 0.85],
        [1, "#ffe98a", 0],
      ]),
      bark("trunk", "#1f1710", "#4b3a2a", "#7a6144"),
      bark("bough", "#241b13", "#3d2f22", "#63503a"),
    ],
    parts,
  };
})();

const palmShore = (() => {
  const TRUNK = "#8a6a44";
  const TRUNK2 = "#6d5133";
  const FROND = ["#1f6f45", "#2d8d55", "#46ab68"];
  const parts = [];

  // A low sun behind the card's brow.
  parts.push(BK(dot(136, -18, 56), { fill: "@sun", op: 0.85 }));
  parts.push(BK(dot(136, -18, 32), { fill: "#ffd98a" }));

  // Two palms rising from the bottom corners, hugging the edges, opening into
  // fronds that arch over the banner.
  for (const [x0, dir] of [
    [9, 1],
    [263, -1],
  ]) {
    const t = limb(x0 + 3 * dir, 404, -90, 412, 6.5, 4, -dir * 9, 10);
    parts.push(P(t.d, { fill: "@trunk" }));
    const rings = [];
    for (let i = 0; i < 20; i++) {
      const p = t.spine[Math.min(t.spine.length - 1, Math.floor((i / 20) * t.spine.length))];
      rings.push(`M${r2(p[0] - 6)} ${r2(p[1])}q6 3 12 0`);
    }
    parts.push(SK(rings.join(""), { stroke: TRUNK2, sw: 1.1, op: 0.75 }));

    // Fronds: long, and bending back down under their own weight — a frond
    // that only points upward reads as a fern. The bend is what makes the pair
    // of palms into an arch over the card's brow.
    const crown = t.tip;
    const fronds =
      dir > 0
        ? [
            [-128, 96, 74],
            [-100, 116, 96],
            [-66, 122, 104],
            [-30, 108, 92],
            [4, 88, 74],
          ]
        : [
            [-52, 96, -74],
            [-80, 116, -96],
            [-114, 122, -104],
            [-150, 108, -92],
            [-184, 88, -74],
          ];
    fronds.forEach(([a, len, curve], i) => {
      const f = limb(crown[0], crown[1], a, len, 2.6, 0.7, curve, 9);
      const leaflets = [];
      f.spine.forEach((p, k) => {
        if (!k) return;
        const j = Math.min(k + 1, f.spine.length - 1);
        const dirAng = (Math.atan2(f.spine[j][1] - f.spine[k - 1][1], f.spine[j][0] - f.spine[k - 1][0]) * 180) / Math.PI;
        const s = 21 * (1 - k / (f.spine.length + 1)) + 6;
        for (const side of [-1, 1]) {
          leaflets.push(
            poly([
              [p[0], p[1]],
              [p[0] + Math.cos(rad(dirAng + side * 74)) * s, p[1] + Math.sin(rad(dirAng + side * 74)) * s],
              [p[0] + Math.cos(rad(dirAng + side * 30)) * s * 0.85, p[1] + Math.sin(rad(dirAng + side * 30)) * s * 0.85],
            ]),
          );
        }
      });
      parts.push(
        P(f.d + leaflets.join(""), {
          fill: FROND[i % 3],
          a: i % 2 ? "sway" : "sway-slow",
          or: `${r2(crown[0])}px ${r2(crown[1])}px`,
          dl: -i * 0.7,
        }),
      );
    });
    // Coconuts under the crown.
    parts.push(P(dot(crown[0] + 5 * dir, crown[1] + 7, 4) + dot(crown[0] - 3 * dir, crown[1] + 9, 3.4), { fill: "#5d4327" }));
  }

  // Sea and sand along the foot: three crests, each lapping at its own pace.
  parts.push(P(rect(-30, 396, 332, 22), { fill: "#e6d2a4" }));
  parts.push(P(waveTop(-40, 312, 384, 4.5, 108, 0, 416), { fill: "#16708c" }));
  parts.push(P(waveTop(-40, 312, 391, 4, 78, 1.9, 416), { fill: "#2196ab", a: "lap", dl: -1.4 }));
  parts.push(P(waveTop(-40, 312, 398, 3.2, 58, 3.4, 416), { fill: "#5cc2cd", a: "lap-far" }));
  parts.push(
    SK(waveTopStroke(-40, 312, 402, 2.6, 46, 1.2), {
      stroke: "#f2f7f8",
      sw: 1.6,
      op: 0.8,
      a: "lap",
    }),
  );
  parts.push(P(oval(196, 407, 7, 4.6, -14) + oval(60, 409, 5, 3.4, 10), { fill: "#f4e6cd" }));

  // A gull crossing high above the card.
  parts.push(
    BK("M-10 0q7-7 12 0q5-7 12 0", {
      fill: "none",
      stroke: "#f2f7f8",
      sw: 2,
      a: "cross-high",
      or: "0px 0px",
      op: 0.85,
    }),
  );

  return {
    id: "palm-shore",
    name: "Palm shore",
    group: "Water",
    // Two palms holding the card up, fronds arching over the banner, three
    // crests lapping along the foot.
    grads: [
      radial("sun", 136, -18, 56, [
        [0, "#ffe9a8", 0.7],
        [1, "#ffe9a8", 0],
      ]),
      bark("trunk", "#4a3720", "#8a6a44", "#c2a071"),
    ],
    parts,
  };
})();

/** waveTopStroke: the open crest only, for a foam line over a filled band. */
function waveTopStroke(x0, x1, y, amp, wl, phase) {
  const pts = [];
  const n = Math.max(12, Math.round((x1 - x0) / 6));
  for (let i = 0; i <= n; i++) {
    const x = x0 + ((x1 - x0) * i) / n;
    pts.push([x, y + Math.sin((x / wl) * Math.PI * 2 + phase) * amp]);
  }
  return smooth(pts, false);
}

const frostwood = (() => {
  // Snow at the blue hour. The old frame painted every flake of it #eef5fb,
  // and white snow is the single most common way to make a winter picture look
  // like clip art: snow is a mirror, so it is blue-violet wherever it only sees
  // the sky and warm cream where the last of the sun still reaches it. The two
  // together are the whole subject. Nothing here is pure white except the
  // sunlit lip of a drift and the core of an icicle.
  const parts = [];
  const g = rnd(53);

  // ── plane 4: the sky, and the ranks going back into it ─────────────────
  parts.push(BK(oval(136, -40, 240, 150), { fill: "@sky" }));
  parts.push(BK(oval(178, 8, 168, 74), { fill: "@lastlight" }));
  const rank = (y, h, step, fill, op, seed) => {
    const gg = rnd(seed);
    const d = [];
    for (let x = -70; x < 346; x += step) {
      const hh = h * (0.6 + gg() * 0.8);
      const w = hh * 0.36;
      d.push(poly([[x - w, y], [x, y - hh], [x + w, y]]));
    }
    parts.push(BK(d.join(""), { fill, op }));
  };
  rank(-30, 46, 17, "@rank-far", 0.5, 3);
  rank(-16, 58, 21, "@rank-mid", 0.75, 9);
  rank(-2, 74, 27, "@rank-near", 0.95, 17);

  // Snow coming down in front of the ranks and behind the card, in three sizes
  // so it has depth of its own.
  for (let i = 0; i < 26; i++) {
    const x = -50 + g() * 372;
    const r = 0.9 + g() * 1.9;
    parts.push(BK(dot(x, -90 + g() * 90, r), { fill: "@flake", a: "petal", or: `${r2(x)}px 0px`, dl: -g() * 11, op: 0.5 + g() * 0.45 }));
  }

  // ── plane 2: the boughs over the brow ──────────────────────────────────
  // Weighted differently on each side. The left is loaded and sagging; the
  // right has shed its load and one limb has snapped off short.
  const boughs = [
    [-40, -40, -4, 132, 0, 1],
    [-32, 6, 14, 108, 1, 1],
    [-24, 48, 24, 78, 2, 1],
    [308, -34, -174, 122, 0, 1],
    [300, 12, 164, 92, 1, 0],
    [294, 52, 154, 58, 2, 0],
  ];
  boughs.forEach(([x, y, a, len, tone, load], i) => {
    const cls = i % 2 ? "bough" : "bough-slow";
    parts.push(P(limb(x, y, a, len, 3.6, 1, 0, 5).d, { fill: "@limb", a: cls, or: `${x}px ${y}px`, dl: -i * 0.7 }));
    parts.push(P(spray(x, y, a, len, 9, 31), { fill: `@needle-${tone}`, a: cls, or: `${x}px ${y}px`, dl: -i * 0.7 }));
    // A second, darker spray offset down: the underside of a bough is in its
    // own shadow, and without it a conifer is a green comb.
    parts.push(P(spray(x, y + 4, a, len * 0.9, 8, 22), { fill: "@needle-dk", op: 0.75, a: cls, or: `${x}px ${y}px`, dl: -i * 0.7 }));
    if (load) {
      // Snow LYING on a bough, not a second bough made of snow: a spray of
      // white blades reads as a row of arrowheads, which is what the old frame
      // did. A run of squashed lumps along the axis reads as a load.
      const lo = [];
      const hi = [];
      for (let k = 1; k <= 9; k++) {
        const t = k / 9.5;
        const px = x + Math.cos(rad(a)) * len * t;
        const py = y + Math.sin(rad(a)) * len * t;
        const r = 15 * (1 - t * 0.62);
        lo.push(blob(px, py - r * 0.42, r, { squash: 0.46, wob: 0.32, seed: k * 5 + i }));
        hi.push(blob(px - 1, py - r * 0.7, r * 0.66, { squash: 0.42, wob: 0.3, seed: k * 7 + i * 3 }));
      }
      parts.push(P(lo.join(""), { fill: "@snow-shade", a: cls, or: `${x}px ${y}px`, dl: -i * 0.7 }));
      parts.push(P(hi.join(""), { fill: "@snow-lit", a: cls, or: `${x}px ${y}px`, dl: -i * 0.7 }));
    }
  });
  // The snapped limb, hanging by a splinter.
  parts.push(P(limb(288, 96, 150, 34, 3, 1.4, 0, 4).d, { fill: "@limb" }));
  parts.push(P(limb(263, 112, 96, 26, 2, 0.6, 12, 4).d, { fill: "@limb", a: "swing", or: "263px 112px" }));

  // ── plane 1: the rime along the brow, and what hangs off it ────────────
  // A ridge, not a bar. waveTop can only wave one of its two edges, and the
  // straight one was sitting above the card in plain sight looking like a
  // painted plank; a band between two different sine curves has no flat side.
  const ridge = (yT, aT, wT, pT, yB, aB, wB, pB) => {
    const top = [];
    const bot = [];
    const n = 52;
    for (let i = 0; i <= n; i++) {
      const x = -16 + (304 * i) / n;
      top.push([x, yT + Math.sin((x / wT) * Math.PI * 2 + pT) * aT]);
      bot.push([x, yB + Math.sin((x / wB) * Math.PI * 2 + pB) * aB]);
    }
    return smooth(top) + smooth(bot.reverse()).replace(/^M/, "L") + "Z";
  };
  parts.push(P(ridge(-11, 3.4, 84, 0.3, 6, 4.5, 62, 1.1), { fill: "@snow-shade" }));
  parts.push(P(ridge(-14, 3.8, 76, 0.7, 0, 5, 58, 1.4), { fill: "@snow-lit" }));
  const ice = [];
  const iceCore = [];
  for (let i = 0; i < 30; i++) {
    const x = -10 + i * 9.6 + g() * 3;
    const len = 6 + g() * 30;
    const w = 1.8 + g() * 1.8;
    ice.push(poly([[x - w, 3], [x + w, 3], [x, 3 + len]]));
    iceCore.push(poly([[x - w * 0.34, 3], [x + w * 0.3, 3], [x, 3 + len * 0.82]]));
  }
  parts.push(P(ice.join(""), { fill: "@ice" }));
  parts.push(P(iceCore.join(""), { fill: "@ice-core", op: 0.8, a: "twinkle" }));

  // ── the rails: birches ─────────────────────────────────────────────────
  // Birch rather than more conifer, because the sides of this frame want a
  // material and not more of the same silhouette: white bark takes the blue of
  // the sky on one flank and the warm of the low sun on the other, which is the
  // same argument the snow is making, said by a different object.
  for (const [x0, dir, seed] of [
    [8, 1, 5],
    [264, -1, 31],
  ]) {
    const t = limb(x0, 412, -90, 452, 8.4, 7, 0, 8);
    parts.push(P(t.d, { fill: "@birch" }));
    parts.push(P(t.d, { fill: "@birch", f: "papery", op: 0.4 }));
    // Lenticels: the black dashes that ARE birch. Uneven, in bands, and never
    // the same length twice.
    const marks = [];
    const peel = [];
    for (let i = 0; i < 46; i++) {
      const y = -30 + i * 10 + g() * 6;
      const w = 2 + g() * 9;
      const px = x0 - 6 + g() * 11;
      marks.push(poly([[px, y], [px + w, y - 0.6], [px + w, y + 1.9], [px, y + 2.4]]));
      if (g() > 0.86) peel.push(poly([[px, y], [px + w * 0.8, y - 1], [px + w * 0.8, y + 4], [px, y + 5.4]]));
    }
    parts.push(P(marks.join(""), { fill: "@lenticel", op: 0.85 }));
    parts.push(P(peel.join(""), { fill: "@peel", op: 0.7 }));
    // A knot, and the snow lying along the windward side of the trunk.
    parts.push(P(oval(x0 + dir * 3, 208, 4.4, 6, 8), { fill: "@lenticel", op: 0.8 }));
    parts.push(P(oval(x0 + dir * 3, 208, 2.4, 3.4, 8), { fill: "@birch" }));
    parts.push(P(poly([[x0 - 8.4 * dir, -30], [x0 - 5.4 * dir, -30], [x0 - 5.8 * dir, 412], [x0 - 8.4 * dir, 412]]), { fill: "@snow-lit", op: 0.5 }));
  }

  // ── the drift along the foot ───────────────────────────────────────────
  // Wind-sculpted: a shaded body, then a lit lip that does NOT follow the same
  // line, which is what a cornice actually looks like.
  parts.push(P(waveTop(-40, 312, 382, 9, 150, 0.4, 430), { fill: "@drift-far" }));
  parts.push(P(waveTop(-40, 312, 394, 7, 106, 2.6, 430), { fill: "@drift" }));
  parts.push(P(waveTop(-40, 312, 392, 6, 98, 2.9, 402), { fill: "@snow-lit" }));
  parts.push(P(waveTop(-40, 312, 408, 5, 74, 4.4, 430), { fill: "@drift-near" }));
  parts.push(P(waveTop(-40, 312, 406, 4, 70, 4.7, 414), { fill: "@snow-lit", op: 0.9 }));
  // Saplings and dead stems standing out of it, and a set of tracks.
  const saplings = [];
  for (let i = 0; i < 16; i++) {
    const x = -24 + i * 20 + g() * 9;
    const h = 8 + g() * 20;
    saplings.push(`M${r2(x)} 406Q${r2(x + (g() - 0.5) * 5)} ${r2(406 - h * 0.6)} ${r2(x + (g() - 0.5) * 11)} ${r2(406 - h)}`);
  }
  parts.push({ d: saplings.join(""), z: "front", stroke: "@stem", sw: 1.2, op: 0.75, a: "sway", or: "136px 406px" });
  const tracks = [];
  for (let i = 0; i < 9; i++) {
    const x = 96 + i * 17;
    tracks.push(oval(x, 400 + (i % 2 ? 4 : 0), 3.4, 2.2, -12));
  }
  parts.push(P(tracks.join(""), { fill: "@track", op: 0.55 }));
  // Three small firs on the drift, in front of everything, and darkest.
  for (const [x, h] of [
    [22, 46],
    [252, 34],
    [44, 26],
  ]) {
    parts.push(P(tri([x - h * 0.36, 404], [x + h * 0.36, 404], [x, 404 - h]), { fill: "@needle-2" }));
    parts.push(P(tri([x - h * 0.3, 398], [x + h * 0.3, 398], [x, 402 - h]), { fill: "@snow-shade", op: 0.9 }));
    parts.push(P(poly([[x, 402 - h], [x - h * 0.3, 398], [x - h * 0.18, 398]]), { fill: "@snow-lit" }));
  }

  // The lantern half-buried in the drift: the one warm light, and the reason
  // the blue everywhere else reads as cold rather than as a colour cast.
  parts.push(P(oval(196, 396, 44, 30), { fill: "@lampglow", a: "glow", op: 0.85 }));
  parts.push(P(rect(190, 384, 12, 16), { fill: "@lampglass" }));
  parts.push(P(rect(188, 380, 16, 4) + rect(188, 398, 16, 4), { fill: "@lampiron" }));
  parts.push({ d: "M191 380Q196 368 201 380", z: "front", stroke: "@lampiron", sw: 1.6 });
  parts.push(P(flame(196, 396, 10, 2.6, 5), { fill: "#ffbe4e", a: "flick", or: "196px 396px" }));
  parts.push(P(flame(196, 396, 5.4, 1.3, 9), { fill: "#fff3d2", a: "flick", or: "196px 396px", dl: -0.2 }));
  // Snow drifting up off the drift in the light.
  for (const [x, dl] of [
    [176, 0],
    [212, -2.9],
    [30, -5.1],
    [250, -7.4],
  ]) {
    parts.push(P(dot(x, 392, 1.5), { fill: "@flake", a: "float-up", dl, op: 0.8 }));
  }

  return {
    id: "frostwood",
    name: "Frostwood",
    group: "Woodland",
    // Snow-laden boughs dipping over the brow, birches holding the sides,
    // icicles along the rime and a lantern burning in the drift.
    grads: [
      glow("sky", [
        [0, "#43597e", 0.55],
        [0.5, "#2f4260", 0.28],
        [1, "#22304a", 0],
      ]),
      glow("lastlight", [
        [0, "#f0b478", 0.4],
        [1, "#c07c4a", 0],
      ]),
      // Each rank dies out before its own baseline. A solid triangle stops at a
      // ruled horizontal line wherever it is told to, and that line ran the
      // whole width of the frame.
      lin("rank-far", 0, -80, 0, -28, [
        [0, "#7d93b4", 1],
        [0.72, "#68809f", 0.85],
        [1, "#68809f", 0],
      ]),
      lin("rank-mid", 0, -76, 0, -14, [
        [0, "#526a90", 1],
        [0.74, "#43597e", 0.9],
        [1, "#43597e", 0],
      ]),
      lin("rank-near", 0, -78, 0, 0, [
        [0, "#33486c", 1],
        [0.8, "#2a3c5e", 0.92],
        [1, "#2a3c5e", 0],
      ]),
      lin("flake", 0, -100, 0, 400, [
        [0, "#eaf2fb", 0.95],
        [1, "#cddcee", 0.8],
      ]),
      // Snow twice: what the sky lights, and what the sun still reaches.
      plane("snow-lit", [
        [0, "#fff3e2"],
        [0.4, "#f0e2dc"],
        [1, "#c9cfe2"],
      ]),
      plane("snow-shade", [
        [0, "#b9c6de"],
        [0.5, "#93a5c6"],
        [1, "#6e83ab"],
      ]),
      leafy("needle-0", "#0f2a2c", "#1c4442", "#356257"),
      leafy("needle-1", "#0d2528", "#183c3b", "#2d564d"),
      leafy("needle-2", "#0a1f22", "#143331", "#254941"),
      leafy("needle-dk", "#07171a", "#0d2426", "#132f2e"),
      bark("limb", "#160f10", "#2b2320", "#4a3d34"),
      // Birch: cool on the flank that only sees sky, warm on the flank the low
      // sun still finds, and never a flat white.
      tube("birch", [
        [0, "#8f9dbd"],
        [0.2, "#d6dcea"],
        [0.4, "#f6efe6"],
        [0.62, "#ded2c8"],
        [0.84, "#a3a3b4"],
        [1, "#77798f"],
      ]),
      lin("lenticel", 0, -40, 0, 412, [
        [0, "#3d3a44"],
        [1, "#25242c"],
      ]),
      lin("peel", 0, -40, 0, 412, [
        [0, "#c9a67e"],
        [1, "#8d7355"],
      ]),
      plane("drift-far", [
        [0, "#a6b6d2"],
        [0.5, "#8698bd"],
        [1, "#6b7ea6"],
      ]),
      plane("drift", [
        [0, "#b6c4dc"],
        [0.5, "#93a4c6"],
        [1, "#7183ab"],
      ]),
      plane("drift-near", [
        [0, "#c6d1e4"],
        [0.5, "#a2b1cf"],
        [1, "#7f90b6"],
      ]),
      lin("track", 0, 396, 0, 410, [
        [0, "#5f739b"],
        [1, "#48597e"],
      ]),
      lin("stem", 0, 380, 0, 410, [
        [0, "#8a7a5e"],
        [1, "#5e523c"],
      ]),
      // Ice: bright at both edges and dim between, which is how a transparent
      // thing shows itself.
      frozen("ice", "#6f92b8", "#a8cbe2", "#e6f4fb"),
      lin("ice-core", 0, 0, 0, 40, [
        [0, "#ffffff", 0.9],
        [1, "#d2ecf8", 0.5],
      ]),
      radial("lampglow", 196, 394, 44, [
        [0, "#ffc96e", 0.6],
        [0.5, "#d98a34", 0.22],
        [1, "#8a4f18", 0],
      ]),
      lin("lampglass", 0, 382, 0, 402, [
        [0, "#ffe6ab"],
        [1, "#e09b3c"],
      ]),
      forged("lampiron", "#171a22", "#2c313c", "#6b7488"),
    ],
    filters: [grain("papery", "0.7 1.9", { oct: 3, seed: 29, k: 0.8 })],
    parts,
  };
})();

const coralReef = (() => {
  const KELP = ["#1f7a58", "#2b9a6a", "#3fb47d"];
  const parts = [];

  // Light shafts coming down through the water. They stop at the banner's
  // lower edge and fade out before they get there — a shaft with a hard end is
  // a grey rectangle sitting on someone's face.
  for (const [x, w, dl] of [
    [34, 16, 0],
    [148, 22, -3.1],
    [226, 13, -5.4],
  ]) {
    parts.push(
      P(poly([[x, -60], [x + w, -60], [x + w + 18, 108], [x + 15, 108]]), {
        fill: "@shaft",
        op: 0.3,
        a: "shimmer",
        or: `${x}px -60px`,
        dl,
      }),
    );
  }

  // Kelp climbing both rails, each strand swaying on its own clock.
  for (const [x, dir] of [
    [6, 1],
    [266, -1],
  ]) {
    [0, 1, 2].forEach((k) => {
      const s = limb(x + k * 3 * dir, 406, -90 - dir * 3, 300 + k * 44, 3.4 - k * 0.6, 1.2, dir * (2 + k * 2), 9);
      parts.push(
        P(s.d, {
          fill: KELP[k],
          a: k % 2 ? "kelp" : "kelp-slow",
          or: `${r2(x + k * 3 * dir)}px 406px`,
          dl: -k * 1.6,
        }),
      );
      const blades = [];
      s.spine.forEach((p, i) => {
        if (i % 2) return;
        blades.push(oval(p[0] + 6 * dir, p[1], 7, 2.6, dir * 18));
      });
      parts.push(
        P(blades.join(""), {
          fill: KELP[k],
          op: 0.85,
          a: k % 2 ? "kelp" : "kelp-slow",
          or: `${r2(x + k * 3 * dir)}px 406px`,
          dl: -k * 1.6,
        }),
      );
    });
  }

  // Sea fans rooted in the top corners and opening upward, out of the card.
  for (const [x, dir] of [
    [-2, 1],
    [274, -1],
  ]) {
    // A fan is a solid blade with ribs on it, not a lattice: the first version
    // drew radial strands crossed by arcs and came out looking like a cobweb.
    const rim = arcPts(x, 30, 52, dir > 0 ? -98 : -82, dir > 0 ? -6 : -174, 12);
    parts.push(
      P(poly([[x, 30], ...rim]), { fill: "#c65f66", op: 0.85, a: "kelp-slow", or: `${x}px 30px` }),
    );
    const ribs = [];
    for (let i = 0; i < 7; i++) {
      const a = (dir > 0 ? -96 : -84) + i * 15 * dir;
      ribs.push(
        `M${x} 30L${r2(x + Math.cos(rad(a)) * 50)} ${r2(30 + Math.sin(rad(a)) * 50)}`,
      );
    }
    parts.push(SK(ribs.join(""), { stroke: "#8f3b46", sw: 1.4, op: 0.7, a: "kelp-slow", or: `${x}px 30px` }));
  }

  // Coral heads, anemones and sand along the foot.
  parts.push(P(waveTop(-30, 302, 398, 5, 120, 0.8, 418), { fill: "#e6dcbc" }));
  const heads = [
    [18, 386, 20, "#e0685f"],
    [44, 396, 12, "#f0916a"],
    [240, 388, 17, "#d95f7c"],
    [216, 398, 11, "#e8a05f"],
    [136, 404, 13, "#c8607a"],
  ];
  for (const [x, y, r, c] of heads) {
    parts.push(P(blob(x, y, r, { squash: 0.75, wob: 0.28, seed: x }), { fill: c }));
    const pores = [];
    const gg = rnd(x);
    for (let i = 0; i < 8; i++) pores.push(dot(x - r * 0.7 + gg() * r * 1.4, y - r * 0.5 + gg() * r, 1.2));
    parts.push(P(pores.join(""), { fill: "#ffffff", op: 0.35 }));
  }
  for (const [x, dl] of [
    [72, 0],
    [190, -1.6],
  ]) {
    const arms = [];
    for (let i = 0; i < 9; i++) {
      const a = -160 + i * 20;
      arms.push(limb(x, 410, a, 20, 1.8, 0.6, a < -90 ? -22 : 22, 4).d);
    }
    parts.push(P(arms.join(""), { fill: "c2", a: "anemone", or: `${x}px 410px`, dl }));
  }

  // Bubbles rising along both rails, and one fish crossing the top.
  for (const [x, r, dl] of [
    [16, 3.4, 0],
    [22, 2.2, -2.6],
    [254, 3, -1.3],
    [260, 2, -4.2],
    [14, 2.6, -5.5],
  ]) {
    parts.push(P(dot(x, 340, r), { fill: "#dff4fb", op: 0.5, a: "bubble", dl }));
  }
  parts.push(
    P(
      poly([[0, 0], [16, -7], [26, 0], [16, 7]]) + poly([[0, 0], [-9, -7], [-7, 0], [-9, 7]]),
      { fill: "c1", a: "cross-low", or: "0px 0px", op: 0.9 },
    ),
  );

  return {
    id: "coral-reef",
    name: "Coral reef",
    group: "Water",
    // Kelp climbing both rails, fans in the corners, coral and anemones along
    // the foot, bubbles going up and a fish going across.
    grads: [
      lin("shaft", 0, -60, 0, 108, [
        [0, "#cdf3ff", 0],
        [0.25, "#cdf3ff", 0.55],
        [1, "#cdf3ff", 0],
      ]),
    ],
    parts,
  };
})();

const cathedral = (() => {
  // A west front at blue hour, and the one frame in the set lit from INSIDE:
  // the stone is cold, the sky is colder, and every warm thing in the picture
  // is light coming out through glass. That inversion is what keeps it from
  // being the castle again in a different silhouette.
  const parts = [];
  const g = rnd(19);
  const CX = 136;
  const YS = 76; // the springing — where the arch leaves the piers
  const H1 = 146; // outer face
  const H2 = 119; // inner face; the piers are 27 wide, 17 of them on the card
  const R1 = 210;
  const R2 = 186;

  // ── the sky, and the spires standing out beyond the frame ──────────────
  parts.push(BK(oval(136, -84, 226, 158), { fill: "@sky" }));
  parts.push(BK(dot(226, -150, 13), { fill: "#e8eefb", op: 0.9 }));
  parts.push(BK(dot(231, -154, 12), { fill: "@moonbite" }));
  parts.push(BK(dot(226, -150, 30), { fill: "@moonhalo", op: 0.7 }));

  // Two spires, and only one of them finished — a cathedral that took two
  // hundred years to build is very rarely symmetrical, and the scaffold is the
  // cheapest way to say so.
  for (const [sx, done] of [
    [-24, 1],
    [296, 0],
  ]) {
    parts.push(BK(rect(sx - 20, -120, 40, 512), { fill: "@spire" }));
    parts.push(BK(coursedWall(sx - 20, -120, 40, 512, 13, 41).mortar, { fill: "@mortar", op: 0.5 }));
    parts.push(BK(rect(sx - 24, -132, 48, 12), { fill: "@stone-lit", op: 0.75 }));
    parts.push(BK(rect(sx - 24, -132, 48, 2), { fill: "@rim", op: 0.6 }));
    for (const ly of [-96, -52, -8]) {
      parts.push(BK(poly([[sx - 7, ly + 22], ...arcPts(sx, ly + 22, 7, 180, 270, 5), [sx, ly - 4], ...arcPts(sx, ly + 22, 7, 270, 360, 5), [sx + 7, ly + 22]]), { fill: "#141a30" }));
      parts.push(BK(rect(sx - 0.8, ly - 2, 1.6, 24) + rect(sx - 6, ly + 8, 12, 1.4), { fill: "@stone-lit", op: 0.5 }));
    }
    if (done) {
      parts.push(BK(tri([sx - 25, -132], [sx + 25, -132], [sx, -246]), { fill: "@spire-roof" }));
      parts.push(BK(poly([[sx, -246], [sx + 25, -132], [sx, -132]]), { fill: "#12172a", op: 0.5 }));
      parts.push(BK(poly([[sx, -246], [sx - 25, -132], [sx - 21, -132], [sx, -240]]), { fill: "@rim", op: 0.5 }));
      // Crockets climbing the spire — the leafy hooks that make a gothic
      // silhouette read as carved rather than as a cone.
      const ck = [];
      for (let i = 1; i < 8; i++) {
        const t = i / 8;
        const y = -246 + 114 * t;
        const hw = 25 * t;
        for (const s of [-1, 1]) ck.push(blob(sx + s * (hw + 2.4), y, 3.4, { squash: 0.8, wob: 0.35, seed: i * 7 + (s + 2) }));
      }
      parts.push(BK(ck.join(""), { fill: "@spire-roof" }));
      parts.push(BK(rect(sx - 1.3, -266, 2.6, 22) + dot(sx, -262, 3.4), { fill: "@stone-lit" }));
    } else {
      // Unfinished: a truncated stump, a hoist, and a lashed timber cage.
      parts.push(BK(rect(sx - 26, -146, 52, 16), { fill: "@spire" }));
      parts.push(BK(rect(sx - 26, -146, 52, 2.2), { fill: "@rim", op: 0.55 }));
      const sc = [];
      for (let i = 0; i < 5; i++) sc.push(rect(sx - 30 + i * 15, -186, 2.6, 60));
      for (let i = 0; i < 4; i++) sc.push(rect(sx - 32, -180 + i * 16, 64, 2.2));
      parts.push(BK(sc.join(""), { fill: "@timber" }));
      parts.push(BK(rect(sx - 34, -196, 76, 3.4), { fill: "@timber-lit" }));
      parts.push({ d: `M${sx + 22} -194L${sx + 22} -156`, z: "back", stroke: "#2a3145", sw: 1.2, op: 0.9, a: "abseil-line", or: `${sx + 22}px -194px` });
      parts.push(BK(rect(sx + 17, -160, 10, 8), { fill: "@timber", a: "abseil", or: `${sx + 22}px -194px` }));
    }
  }

  // Rooks going home, high over both spires.
  for (const [s, cls, dl, op] of [
    [0.9, "cross-high", 0, 0.45],
    [0.66, "cross", -7.5, 0.36],
    [0.5, "cross-high", -14, 0.28],
  ]) {
    parts.push(BK(birdMark(s), { fill: "none", stroke: "#7f8bab", sw: 1.4 * s, a: cls, or: "0px 0px", op, dl }));
  }

  // ── the west front ─────────────────────────────────────────────────────
  const oPts = gothicArch(CX, YS, H1, R1, 13);
  const iPts = gothicArch(CX, YS, H2, R2, 13);
  parts.push(P(archBand(CX, YS, H1, R1, H2, R2, 400, 13), { fill: "@arch" }));
  parts.push(P(archBand(CX, YS, H1, R1, H2, R2, 400, 13), { fill: "@arch", f: "ashlar", op: 0.45 }));
  // Voussoirs: one joint per pair of sampled points, struck between the two
  // curves, so the head is coursed radially the way an arch actually is.
  const vj = [];
  for (let i = 0; i < oPts.length; i += 2) {
    vj.push(`M${r2(oPts[i][0])} ${r2(oPts[i][1])}L${r2(iPts[i][0])} ${r2(iPts[i][1])}`);
  }
  parts.push({ d: vj.join(""), z: "front", stroke: "@mortar", sw: 1.1, op: 0.75 });
  // Two mouldings following the same curve: a roll catching the light on the
  // outside, a hollow in shadow inside it.
  parts.push(P(archBand(CX, YS, H1 - 1, R1 - 2, H1 - 6, R1 - 8, 400, 13), { fill: "@roll" }));
  parts.push(P(archBand(CX, YS, H1 - 6, R1 - 8, H1 - 10, R1 - 13, 400, 13), { fill: "#161c30", op: 0.5 }));
  parts.push(P(archBand(CX, YS, H2 + 5, R2 + 6, H2, R2, 400, 13), { fill: "@roll", op: 0.8 }));
  // Crockets up the outer curve, and a finial at the apex.
  const crock = [];
  for (let i = 2; i < oPts.length - 2; i += 2) {
    const [px, py] = oPts[i];
    const dx = px - CX;
    const dy = py - (YS - R1 * 0.55);
    const m = Math.hypot(dx, dy) || 1;
    crock.push(blob(px + (dx / m) * 3.6, py + (dy / m) * 3.6, 3.6, { squash: 0.8, wob: 0.4, seed: 40 + i }));
  }
  parts.push(P(crock.join(""), { fill: "@arch-lit" }));
  parts.push(P(poly([[CX - 6, -128], [CX + 6, -128], [CX + 2.4, -150], [CX, -160], [CX - 2.4, -150]]), { fill: "@arch-lit" }));
  parts.push(P(rect(CX - 8, -130, 16, 4), { fill: "@rim", op: 0.7 }));

  // ── the tympanum and its window ────────────────────────────────────────
  // The head was left open and the lancets hung in it with nothing round them,
  // which is the same mistake as a back part drawn inside the card: a window is
  // a hole in something, and with nothing to be a hole in it reads as a sticker
  // floating in the doorway. So the upper head is filled and the glass is set
  // into it, and only the lower head — where the card's own art has to be seen
  // — is left open.
  const TY = 20;
  const tymp = iPts.filter((pt) => pt[1] <= TY);
  const tx0 = tymp[0][0];
  const tx1 = tymp[tymp.length - 1][0];
  parts.push(P(poly([[tx0, TY], ...tymp, [tx1, TY]]), { fill: "@tympanum" }));
  parts.push(P(poly([[tx0, TY], ...tymp, [tx1, TY]]), { fill: "@tympanum", f: "ashlar", op: 0.4 }));
  // The shadow the arch throws onto the recessed panel behind it.
  parts.push(P(poly([[tx0, TY], ...tymp, [tx1, TY], [tx1 - 7, TY], ...gothicArch(CX, YS, H2 - 7, R2 - 8, 13).filter((pt) => pt[1] <= TY).reverse(), [tx0 + 7, TY]]), { fill: "#0c1121", op: 0.55 }));

  const RY = -46;
  const RR = 32;
  parts.push(P(dot(CX, RY, RR + 34), { fill: "@rose-glow", a: "glow", op: 0.85 }));
  parts.push(P(dot(CX, RY, RR + 6), { fill: "@arch-lit" }));
  parts.push(P(dot(CX, RY, RR + 6.5), { fill: "@rim", op: 0.35 }));
  parts.push(P(dot(CX, RY, RR), { fill: "#0e1326" }));
  const petA = [];
  const petB = [];
  const petC = [];
  for (let i = 0; i < 10; i++) {
    const a = i * 36 - 90;
    const px = CX + Math.cos(rad(a)) * RR * 0.58;
    const py = RY + Math.sin(rad(a)) * RR * 0.58;
    const o = oval(px, py, RR * 0.28, RR * 0.2, a);
    if (i % 3 === 0) petA.push(o);
    else if (i % 3 === 1) petB.push(o);
    else petC.push(o);
  }
  parts.push(P(petA.join(""), { fill: "c1", a: "glow", op: 0.95 }));
  parts.push(P(petB.join(""), { fill: "c2", a: "glow", dl: -1.3, op: 0.95 }));
  parts.push(P(petC.join(""), { fill: "@glass-gold", a: "glow", dl: -2.4, op: 0.95 }));
  parts.push(P(dot(CX, RY, RR * 0.23), { fill: "@glass-ruby", a: "glow", dl: -0.7 }));
  const spokes = [];
  for (let i = 0; i < 10; i++) {
    const a = i * 36 - 72;
    spokes.push(`M${r2(CX + Math.cos(rad(a)) * RR * 0.2)} ${r2(RY + Math.sin(rad(a)) * RR * 0.2)}L${r2(CX + Math.cos(rad(a)) * RR)} ${r2(RY + Math.sin(rad(a)) * RR)}`);
  }
  parts.push({ d: spokes.join("") + dot(CX, RY, RR * 0.62), z: "front", stroke: "@arch-lit", sw: 1.5, op: 0.95 });

  // Quatrefoils in the spandrels either side of the arcade — the tympanum was
  // a blank panel and a blank panel is where the eye stops.
  const quatre = (qx, qy, qr) =>
    dot(qx, qy - qr * 0.6, qr * 0.6) + dot(qx, qy + qr * 0.6, qr * 0.6) + dot(qx - qr * 0.6, qy, qr * 0.6) + dot(qx + qr * 0.6, qy, qr * 0.6);
  for (const [qx, qc, qdl] of [
    [70, "@glass-gold", -0.9],
    [202, "c2", -2.2],
  ]) {
    parts.push(P(quatre(qx, -12, 15), { fill: "@arch-lit" }));
    parts.push(P(quatre(qx, -12, 12.4), { fill: "#0d1224" }));
    parts.push(P(quatre(qx, -12, 10.4), { fill: qc, a: "glow", dl: qdl, op: 0.9 }));
  }

  // The arcade under it: five lights, tallest in the middle, each with its own
  // colour of glass and its own clock.
  const lights = [
    [-2, 34, "c1", 0],
    [-1, 42, "@glass-gold", -1.1],
    [0, 50, "c2", -2.1],
    [1, 42, "@glass-ruby", -3.2],
    [2, 34, "c1", -4.1],
  ];
  for (const [k, h, c, dl] of lights) {
    const lx = CX + k * 23;
    const head = (w, hh, y0) => poly([[lx - w, y0], ...arcPts(lx, y0 - hh + w, w, 180, 360, 8), [lx + w, y0]]);
    parts.push(P(head(10, h + 5, 16), { fill: "@arch-lit" }));
    parts.push(P(head(8, h + 2, 15), { fill: "#0d1224" }));
    parts.push(P(head(6.6, h, 14), { fill: c, a: "glow", dl, op: 0.94 }));
    parts.push({
      d: `M${lx} 14L${lx} ${r2(14 - h + 3)}M${r2(lx - 5.6)} ${r2(14 - h * 0.42)}L${r2(lx + 5.6)} ${r2(14 - h * 0.42)}`,
      z: "front",
      stroke: "@arch-lit",
      sw: 1.1,
      op: 0.9,
    });
  }
  // The transom the arcade stands on, and the light spilling past it.
  parts.push(P(poly([[tx0 - 3, 14], [tx1 + 3, 14], [tx1 + 3, 22], [tx0 - 3, 22]]), { fill: "@arch-lit" }));
  parts.push(P(poly([[tx0 - 3, 14], [tx1 + 3, 14], [tx1 + 3, 16], [tx0 - 3, 16]]), { fill: "@rim", op: 0.85 }));
  parts.push(P(poly([[tx0 - 3, 22], [tx1 + 3, 22], [tx1 + 3, 26], [tx0 - 3, 26]]), { fill: "#0b1020", op: 0.6 }));
  parts.push(P(streaks(tx0, tx1, 26, 33, { n: 14, len: 22, w: 1.8 }), { fill: "@stain", op: 0.45 }));
  // The light the window throws down into the room. Blurred, because a light
  // shaft with a ruled edge is a grey triangle sitting on somebody's face.
  parts.push(P(poly([[CX - 46, 26], [CX + 46, 26], [CX + 96, 260], [CX - 96, 260]]), { fill: "@spill", op: 0.16, f: "beam", a: "shimmer" }));

  // Gargoyles at the springing, leaning out over the card. Different beasts on
  // purpose: a mirrored pair is a pattern, two beasts is a building.
  const gargL = [
    oval(4, 68, 17, 8, 12),
    poly([[-10, 66], [-32, 58], [-30, 72], [-8, 76]]),
    poly([[-32, 58], [-42, 55], [-34, 68], [-30, 66]]),
    poly([[-30, 66], [-42, 68], [-33, 72]]),
    oval(10, 56, 8, 9.6, -18),
    poly([[0, 58], [8, 40], [14, 60]]),
    poly([[14, 52], [24, 40], [24, 58]]),
    oval(14, 80, 5.4, 9, 14),
    poly([[16, 74], [30, 84], [22, 86]]),
  ];
  parts.push(P(gargL.join(""), { fill: "@garg" }));
  parts.push(P(poly([[-42, 55], [-32, 58], [-33, 62], [-42, 60]]) + poly([[8, 40], [11, 48], [5, 50]]), { fill: "@rim", op: 0.5 }));
  parts.push(P(poly([[-40, 60], [-30, 62], [-33, 66]]), { fill: "#0a0e1c" }));
  const gargR = [
    oval(266, 66, 14, 10, -10),
    poly([[276, 58], [298, 46], [292, 64]]),
    poly([[280, 56], [286, 34], [296, 52]]),
    poly([[262, 56], [266, 40], [272, 58]]),
    oval(258, 80, 6, 10, -12),
    poly([[254, 72], [238, 80], [254, 86]]),
    poly([[266, 74], [280, 86], [272, 88]]),
  ];
  parts.push(P(gargR.join(""), { fill: "@garg" }));
  parts.push(P(poly([[238, 80], [252, 76], [252, 80]]) + poly([[286, 34], [292, 44], [284, 46]]), { fill: "@rim", op: 0.4 }));
  parts.push(P(dot(252, 78, 1.6) + dot(-38, 58, 1.5), { fill: "#ffcf87", a: "twinkle", op: 0.85 }));

  // ── the piers ──────────────────────────────────────────────────────────
  // Clustered shafts, each a separate path so the fitted gradient rounds each
  // one off in its own place. One rectangle with lines ruled on it is a pilaster
  // in a diagram; three rolls with a hollow between them is a gothic pier.
  for (const [x0, dir] of [
    [0, 1],
    [255, -1],
  ]) {
    parts.push(P(coursedWall(x0 - 10 * (dir > 0 ? 1 : 0), 90, 27, 300, 15, dir > 0 ? 63 : 71).mortar, { fill: "@mortar", op: 0.55 }));
    const shafts = [];
    for (let k = 0; k < 3; k++) shafts.push(rect(x0 + 1 + k * 5.6, 92, 4.6, 268));
    parts.push(P(shafts.join(""), { fill: "@shaft" }));
    parts.push(P(rect(x0 + 16, 92, 1.6, 268), { fill: "#141a2e", op: 0.5 }));
    // Capitals: a bell, an abacus, and a band of stiff-leaf under it.
    parts.push(P(rect(x0 - 2, 78, 21, 7), { fill: "@arch-lit" }));
    parts.push(P(rect(x0 - 2, 78, 21, 2), { fill: "@rim", op: 0.85 }));
    parts.push(P(poly([[x0 - 1, 85], [x0 + 18, 85], [x0 + 15, 93], [x0 + 2, 93]]), { fill: "@cap" }));
    const leaves = [];
    for (let k = 0; k < 4; k++) leaves.push(blob(x0 + 2 + k * 4.6, 89, 3.2, { squash: 0.9, wob: 0.4, seed: 80 + k }));
    parts.push(P(leaves.join(""), { fill: "@cap-lit", op: 0.8 }));
    // Bases, and the plinth they stand on.
    parts.push(P(poly([[x0 + 1, 360], [x0 + 16, 360], [x0 + 18, 370], [x0 - 1, 370]]), { fill: "@cap" }));
    parts.push(P(rect(x0 - 3, 370, 23, 8), { fill: "@arch-lit" }));
    parts.push(P(rect(x0 - 3, 370, 23, 2), { fill: "@rim", op: 0.8 }));
    // A niche halfway down, with somebody standing in it.
    const ny = 176;
    parts.push(P(poly([[x0 + 2, ny + 34], ...arcPts(x0 + 8.5, ny + 8, 6.5, 180, 360, 8), [x0 + 15, ny + 34]]), { fill: "#0c1122" }));
    parts.push(P(poly([[x0 + 8.5, ny + 30], [x0 + 12, ny + 30], [x0 + 11.4, ny + 12], [x0 + 8.5, ny + 8], [x0 + 5.6, ny + 12], [x0 + 5, ny + 30]]), { fill: "@figure" }));
    parts.push(P(dot(x0 + 8.5, ny + 6.4, 2.4), { fill: "@figure" }));
    parts.push(P(poly([[x0 + 1, ny + 34], [x0 + 16, ny + 34], [x0 + 17.5, ny + 39], [x0 - 0.5, ny + 39]]), { fill: "@arch-lit" }));
    parts.push(P(streaks(x0, x0 + 17, ny + 39, 200 + x0, { n: 7, len: 26, w: 1.5 }), { fill: "@stain", op: 0.5 }));
  }

  // Weathering: the two long ledges bleed, and the west face has gone green in
  // the lee of the north pier only.
  parts.push(P(streaks(-4, 20, 86, 21, { n: 8, len: 40, w: 1.7 }) + streaks(252, 276, 86, 27, { n: 6, len: 30, w: 1.5 }), { fill: "@stain", op: 0.5 }));
  const algae = [];
  for (let i = 0; i < 14; i++) algae.push(blob(1 + g() * 15, 250 + g() * 120, 2.6 + g() * 4, { squash: 0.55, wob: 0.45, seed: 210 + i }));
  parts.push(P(algae.join(""), { fill: "@algae", op: 0.45 }));

  // ── the step, and the candles on it ────────────────────────────────────
  parts.push(P(rect(-14, 378, 300, 8), { fill: "@arch-lit" }));
  parts.push(P(rect(-14, 378, 300, 2.2), { fill: "@rim", op: 0.85 }));
  parts.push(P(rect(-14, 386, 300, 4), { fill: "#0b1020", op: 0.6 }));
  parts.push(P(poly([[-20, 388], [292, 388], [300, 424], [-28, 424]]), { fill: "@step" }));
  const sj = [];
  for (let px = -22; px < 296; px += 26 + g() * 12) sj.push(rect(px, 390, 1.3, 34));
  parts.push(P(sj.join(""), { fill: "@mortar", op: 0.6 }));
  for (const [x, h, dl] of [
    [22, 30, 0],
    [34, 20, -0.55],
    [250, 26, -1.1],
    [239, 16, -1.7],
  ]) {
    parts.push(P(dot(x, 386 - h, 22), { fill: "@warm-glow", a: "glow", dl, op: 0.8 }));
    parts.push(P(poly([[x - 3.4, 388], [x + 3.4, 388], [x + 2.8, 388 - h], [x - 2.8, 388 - h]]), { fill: "@wax" }));
    // Wax that has run down one side and set there.
    parts.push(P(poly([[x - 2.8, 388 - h + 2], [x - 1.2, 388 - h + 2], [x - 1.6, 388 - h * 0.4], [x - 3.2, 388 - h * 0.45]]), { fill: "@wax-lit", op: 0.8 }));
    parts.push(P(flame(x, 388 - h, 11, 2.6, 3 + h), { fill: "#ffb03c", a: "flick", or: `${x}px ${388 - h}px`, dl }));
    parts.push(P(flame(x, 388 - h, 6, 1.4, 7 + h), { fill: "#fff2cd", a: "flick", or: `${x}px ${388 - h}px`, dl: dl - 0.2 }));
  }
  // Dust in the light, drifting up through the candle glow.
  for (const [x, dl, r] of [
    [26, 0, 1.4],
    [16, -3.1, 1],
    [246, -1.8, 1.2],
    [256, -5.4, 0.9],
  ]) {
    parts.push(P(dot(x, 370, r), { fill: "#ffe6b4", a: "float-up", dl, op: 0.75 }));
  }

  parts.push(...occlusion(0.9));

  return {
    id: "cathedral",
    name: "Cathedral",
    group: "Stone & stage",
    // A two-centred arch springing from clustered piers, a rose burning in its
    // head, gargoyles leaning off the springing and candles at the foot.
    grads: [
      glow("sky", [
        [0, "#2b3557", 0.6],
        [0.5, "#1f2742", 0.32],
        [1, "#161b2e", 0],
      ]),
      radial("moonhalo", 226, -150, 30, [
        [0, "#dbe6fb", 0.4],
        [1, "#dbe6fb", 0],
      ]),
      lin("moonbite", 0, -170, 0, -130, [
        [0, "#2d3757"],
        [1, "#232c48"],
      ]),
      // Plane 2 — hazier and bluer than the front, which is what puts twelve
      // metres of evening air between them.
      lin("stone-lit", 0, -270, 0, 90, [
        [0, "#9aa0b4"],
        [1, "#6b7288"],
      ]),
      rock("spire", "#232941", "#434a64", "#767c8d"),
      plane("spire-roof", [
        [0, "#6d7694"],
        [0.4, "#333b56"],
        [1, "#1c2135"],
      ]),
      // Plane 1. Limestone: warm where the last light still lands on it, and
      // decisively blue-violet everywhere it does not.
      lin("tympanum", -20, 0, 292, 0, [
        [0, "#5d5a53"],
        [0.45, "#3a3d51"],
        [1, "#1e2337"],
      ]),
      plane("arch", [
        [0, "#cfc3a4"],
        [0.24, "#948d84"],
        [0.56, "#5b5c6d"],
        [1, "#2e3349"],
      ]),
      lin("arch-lit", -20, 0, 292, 0, [
        [0, "#c9bb9a"],
        [0.4, "#8c8779"],
        [0.78, "#565a6c"],
        [1, "#383d54"],
      ]),
      lin("roll", -20, 0, 292, 0, [
        [0, "#e0d3af"],
        [0.42, "#9a927f"],
        [1, "#41465e"],
      ]),
      bark("shaft", "#242940", "#6b6a6e", "#c4b696"),
      rock("cap", "#242940", "#666570", "#b3a68b"),
      lin("cap-lit", 0, 84, 0, 96, [
        [0, "#d3c5a2"],
        [1, "#9a927f"],
      ]),
      lin("garg", 0, 44, 0, 84, [
        [0, "#8b8474"],
        [0.55, "#5c5c6b"],
        [1, "#333850"],
      ]),
      lin("figure", 0, 176, 0, 214, [
        [0, "#cfc2a4"],
        [1, "#7f7c81"],
      ]),
      plane("step", [
        [0, "#a89b7f"],
        [0.32, "#6f6d69"],
        [0.7, "#464a5e"],
        [1, "#252a41"],
      ]),
      lin("mortar", 0, -140, 0, 424, [
        [0, "#2a3046", 0.85],
        [1, "#1d2130", 0.9],
      ]),
      lin("rim", 0, -270, 0, 424, [
        [0, "#f2e6c4", 0.95],
        [0.5, "#ddd0ab", 0.85],
        [1, "#b9ae90", 0.75],
      ]),
      wash("stain", [
        [0, "#131829", 0.7],
        [1, "#131829", 0],
      ]),
      lin("algae", 0, 250, 0, 380, [
        [0, "#4d6444"],
        [1, "#31432c"],
      ]),
      bark("timber", "#241a11", "#4a3826", "#7d6242"),
      lin("timber-lit", 0, -200, 0, -190, [
        [0, "#8a6d4a"],
        [1, "#5e4830"],
      ]),
      // The glass. Jewel colours, because a cathedral window is the most
      // saturated thing in any street it stands on and washing it out to pastel
      // is the surest way to make the whole front look printed.
      lin("glass-gold", 0, -80, 0, -14, [
        [0, "#ffd884"],
        [1, "#d8912c"],
      ]),
      lin("glass-ruby", 0, -60, 0, -32, [
        [0, "#e04a5c"],
        [1, "#9c1730"],
      ]),
      radial("rose-glow", 136, -46, 62, [
        [0, "#ffd9a0", 0.5],
        [0.5, "#c98b52", 0.2],
        [1, "#8a4f36", 0],
      ]),
      glow("warm-glow", [
        [0, "#ffd190", 0.6],
        [1, "#ffd190", 0],
      ]),
      lin("spill", 136, 20, 136, 260, [
        [0, "#ffdca4", 0.85],
        [0.4, "#e0b07a", 0.35],
        [1, "#c98b52", 0],
      ]),
      lin("wax", 0, 340, 0, 390, [
        [0, "#efe4cb"],
        [1, "#b9ac92"],
      ]),
      lin("wax-lit", 0, 340, 0, 390, [
        [0, "#fff6e2"],
        [1, "#d9cdb4"],
      ]),
      ...occlusionGrads("#080c18"),
    ],
    filters: [grain("ashlar", "1.1 1.3", { oct: 4, seed: 7, k: 0.85 }), soft("beam", 9)],
    parts,
  };
})();

const sakura = (() => {
  const BARK = "#5a4231";
  const PETAL = ["#ffd6e4", "#f7b4cd", "#e58fb2"];
  const parts = [];

  // A pale moon behind the card's brow.
  parts.push(BK(dot(58, -24, 42), { fill: "@moonglow", op: 0.8 }));
  parts.push(BK(dot(58, -24, 26), { fill: "#fdf6e4" }));

  const bloom = (x, y, r, tone, seed) => {
    const g = rnd(seed);
    let d = "";
    for (let i = 0; i < 5; i++) {
      const a = i * 72 + g() * 20;
      d += oval(x + Math.cos(rad(a)) * r * 0.62, y + Math.sin(rad(a)) * r * 0.62, r * 0.5, r * 0.42, a);
    }
    return { d, tone };
  };

  // Branches entering from both top corners and arching in over the banner.
  const clusters = [];
  const specks = [];
  [
    [-20, 18, -18, 128, 1, 0],
    [292, 16, -162, 132, -1, 1],
    [-16, 62, 10, 78, 1, 2],
    [288, 66, 170, 78, -1, 3],
  ].forEach(([x, y, a, len, dir, i]) => {
    const b = limb(x, y, a, len, 4.4, 1.4, dir * 30, 9);
    parts.push(P(b.d, { fill: "@bough", a: "sway-slow", or: `${x}px ${y}px`, dl: -i * 1.1 }));
    const twigs = [];
    b.spine.forEach((p, k) => {
      if (k < 2 || k % 2) return;
      const t = limb(p[0], p[1], a + dir * (36 + k * 3), 20 - k, 1.4, 0.5, -dir * 20, 4);
      twigs.push(t.d);
      const bl = bloom(t.tip[0], t.tip[1], 7 - k * 0.4, k % 3, k);
      clusters.push(bl);
      specks.push(dot(t.tip[0], t.tip[1], 1.7));
    });
    parts.push(P(twigs.join(""), { fill: BARK, a: "sway-slow", or: `${x}px ${y}px`, dl: -i * 1.1 }));
    const byTone = [0, 1, 2].map((t) => clusters.filter((c) => c.tone === t).map((c) => c.d).join(""));
    byTone.forEach((d, t) => {
      if (d) parts.push(P(d, { fill: PETAL[t], a: "sway-slow", or: `${x}px ${y}px`, dl: -i * 1.1 }));
    });
    parts.push(P(specks.join(""), { fill: "#c9705a", a: "sway-slow", or: `${x}px ${y}px`, dl: -i * 1.1 }));
    clusters.length = 0;
    specks.length = 0;
  });

  // A paper lantern hung from the right-hand branch.
  parts.push(SK("M232 40L232 62", { stroke: BARK, sw: 1.6, a: "swing", or: "232px 40px" }));
  parts.push(P(oval(232, 78, 15, 17), { fill: "#d94f4c", a: "swing", or: "232px 40px" }));
  parts.push(P(rect(224, 60, 16, 4) + rect(224, 92, 16, 4), { fill: "#6b3a2a", a: "swing", or: "232px 40px" }));
  parts.push(P(oval(232, 78, 15, 17), { fill: "@lantern", a: "swing", or: "232px 40px", op: 0.7 }));

  // A low branch across the foot, and petals coming down the margins.
  const low = limb(-16, 404, -8, 150, 4, 1.6, 16, 8);
  parts.push(P(low.d, { fill: "@bough" }));
  const lowBlooms = [];
  low.spine.forEach((p, k) => {
    if (k % 2) return;
    lowBlooms.push(bloom(p[0], p[1] - 8, 7, 0, k + 30).d);
  });
  parts.push(P(lowBlooms.join(""), { fill: PETAL[0] }));
  const low2 = limb(288, 408, -172, 120, 3.6, 1.4, -16, 8);
  parts.push(P(low2.d, { fill: "@bough" }));
  const lowBlooms2 = [];
  low2.spine.forEach((p, k) => {
    if (k % 2) return;
    lowBlooms2.push(bloom(p[0], p[1] - 7, 6, 1, k + 60).d);
  });
  parts.push(P(lowBlooms2.join(""), { fill: PETAL[1] }));

  for (const [x, dl, tone] of [
    [16, 0, 0],
    [30, -2.9, 1],
    [246, -1.5, 0],
    [258, -4.4, 2],
    [8, -6.2, 1],
  ]) {
    parts.push(P(oval(x, 60, 4.6, 2.8, 24), { fill: PETAL[tone], a: "petal", or: `${x}px 60px`, dl }));
  }

  return {
    id: "sakura",
    name: "Sakura",
    group: "Woodland",
    // Blossom branches leaning in from both corners, a paper lantern swinging
    // under one of them and petals coming down the margins.
    grads: [
      radial("moonglow", 58, -24, 42, [
        [0, "#fdf6e4", 0.45],
        [1, "#fdf6e4", 0],
      ]),
      radial("lantern", 232, 74, 17, [
        [0, "#ffe6a8", 0.85],
        [1, "#ffe6a8", 0],
      ]),
      bark("bough", "#2c2018", "#5a4231", "#8b6b51"),
    ],
    parts,
  };
})();

const velvetStage = (() => {
  // A proscenium from the third row. Velvet is the hardest material in this set
  // to draw, because what identifies it is not its colour but its FALLOFF: a
  // narrow bright roll along each fold and a shadow that goes almost black two
  // centimetres away. The old frame drew alternating flat ribbons of light and
  // dark red, which is the one thing velvet never looks like. Here every fold
  // is its own path with its own fitted ramp, so each one rounds off where it
  // actually stands.
  const parts = [];
  const g = rnd(29);
  const clamp = (t) => Math.min(1, Math.max(0, t));

  // ── behind: the stage itself, lit in the wearer's colour ───────────────
  parts.push(BK(oval(136, 10, 136, 78), { fill: "@cyc" }));
  // Dust hanging in the beam, which is most of what tells you there IS a beam.
  for (let i = 0; i < 14; i++) {
    const x = 40 + g() * 192;
    parts.push(BK(dot(x, -70 + g() * 60, 0.7 + g() * 1.4), { fill: "#ffeec4", a: "twinkle", dl: -g() * 4, op: 0.55 }));
  }

  // The follow spot. The wearer's colour belongs somewhere the audience can
  // see it, and a gelled beam is the one thing on a stage allowed to be any
  // colour at all. Drawn before the velvet, so the drapes and the valance hang
  // in front of it instead of being washed teal by it.
  parts.push(P(poly([[118, -34], [154, -34], [202, 300], [70, 300]]), { fill: "@spot", op: 0.13, f: "beam", a: "shimmer" }));
  parts.push(P(oval(136, 296, 64, 20), { fill: "@spot", op: 0.1, f: "beam" }));

  // ── the drapes ─────────────────────────────────────────────────────────
  // Wide over the brow, drawn back to a narrow band below it so nothing the
  // card has to say ends up behind velvet.
  const K = 8;
  for (const [side, dir, seed, pull] of [
    [0, 1, 3, 0],
    [272, -1, 11, 6],
  ]) {
    const inner = (y) => side + dir * (54 - (37 - pull) * clamp((y - 4) / 84));
    const outer = side - dir * 30;
    const sil = [];
    for (let y = -40; y <= 416; y += 12) sil.push([inner(y), y]);
    parts.push(P(poly([[outer, -40], ...sil, [outer, 416]]), { fill: "@velvet-dk" }));
    for (let k = 0; k < K; k++) {
      const a = [];
      const b = [];
      for (let y = -40; y <= 416; y += 12) {
        const w = inner(y) - outer;
        const t0 = k / K + Math.sin(y / 47 + k * 1.9 + seed) * 0.016;
        const t1 = (k + 1) / K + Math.sin(y / 47 + (k + 1) * 1.9 + seed) * 0.016;
        a.push([outer + w * clamp(t0), y]);
        b.push([outer + w * clamp(t1), y]);
      }
      parts.push(
        P(poly([...a, ...b.reverse()]), {
          fill: k % 2 ? "@velvet" : "@velvet-b",
          a: "breathe",
          or: `${side}px 200px`,
          dl: dir > 0 ? -k * 0.3 : -2.2 - k * 0.3,
        }),
      );
    }
    // The footlights are under the drape, not over it, so the bottom two
    // hands' width of every fold is warm and the top is not.
    parts.push(P(poly([[outer, -40], ...sil, [outer, 416]]), { fill: "@footwash", op: 0.5 }));
    parts.push(P(poly([[outer, -40], ...sil, [outer, 416]]), { fill: "@nap", f: "pile", op: 0.35 }));
    // The tie-back: a gilt cord round the drape with a tassel on it, at a
    // different height each side because a theatre dresses by eye.
    const ty = dir > 0 ? 128 : 152;
    parts.push(P(poly([[outer, ty - 5], [inner(ty), ty - 8], [inner(ty), ty - 1], [outer, ty + 4]]), { fill: "@cord" }));
    parts.push(P(poly([[outer, ty - 5], [inner(ty), ty - 8], [inner(ty), ty - 6.6], [outer, ty - 3.4]]), { fill: "@gold-lit", op: 0.7 }));
    parts.push({ d: `M${r2(inner(ty))} ${ty}Q${r2(inner(ty) - dir * 7)} ${ty + 14} ${r2(inner(ty) - dir * 3)} ${ty + 26}`, z: "front", stroke: "@cord", sw: 2, a: "swing", or: `${r2(inner(ty))}px ${ty}px`, dl: -dir * 0.6 });
    parts.push(
      P(oval(inner(ty) - dir * 3, ty + 31, 4.4, 5.4) + poly([[inner(ty) - dir * 3 - 4, ty + 35], [inner(ty) - dir * 3 + 4, ty + 35], [inner(ty) - dir * 3 + 2.6, ty + 48], [inner(ty) - dir * 3 - 2.6, ty + 48]]), {
        fill: "@gold",
        a: "swing",
        or: `${r2(inner(ty))}px ${ty}px`,
        dl: -dir * 0.6,
      }),
    );
  }

  // ── the valance ────────────────────────────────────────────────────────
  // Three swags of nested crescents. A swag is gathered cloth: the bands are
  // brightest along their upper edge and go dark where the next one laps over
  // them, and that stack is the whole reason it reads as heavy.
  const swagAt = (x0, x1, dip, bands, ph) => {
    for (let k = 0; k < bands; k++) {
      const d0 = -16 + ((dip + 16) * k) / bands;
      const d1 = -16 + ((dip + 16) * (k + 1)) / bands;
      const top = [];
      const bot = [];
      const n = 14;
      for (let i = 0; i <= n; i++) {
        const t = i / n;
        const x = x0 + (x1 - x0) * t;
        const s = Math.sin(Math.PI * t);
        top.push([x, -18 + (d0 + 18) * s]);
        bot.push([x, -18 + (d1 + 18) * s]);
      }
      parts.push(
        P(smooth(top) + smooth(bot.reverse()).replace(/^M/, "L") + "Z", {
          fill: k % 2 ? "@swag" : "@swag-b",
          a: "breathe",
          or: `${r2((x0 + x1) / 2)}px -18px`,
          dl: ph,
        }),
      );
    }
    // Bullion fringe along the lowest crescent.
    const fr = [];
    const n = 26;
    for (let i = 1; i < n; i++) {
      const t = i / n;
      const x = x0 + (x1 - x0) * t;
      const y = -18 + (dip + 18) * Math.sin(Math.PI * t);
      const L = 6 + Math.abs(Math.sin(t * 9)) * 5;
      fr.push(poly([[x - 1.3, y - 2], [x + 1.3, y - 2], [x + 0.9, y + L], [x - 0.9, y + L]]));
    }
    parts.push(P(fr.join(""), { fill: "@gold", a: "breathe", or: `${r2((x0 + x1) / 2)}px -18px`, dl: ph }));
  };
  swagAt(-30, 96, 40, 5, 0);
  swagAt(82, 190, 50, 5, -1.4);
  swagAt(176, 302, 36, 5, -2.6);

  // The pelmet board the swags hang from, and the gilt on it.
  parts.push(P(rect(-32, -54, 336, 22), { fill: "@pelmet" }));
  parts.push(P(rect(-32, -54, 336, 3.4), { fill: "@gold-lit", op: 0.85 }));
  parts.push(P(rect(-32, -34, 336, 4), { fill: "@gold" }));
  const egg = [];
  const dart = [];
  for (let x = -28; x < 302; x += 17) {
    egg.push(oval(x, -43, 3.6, 5));
    dart.push(poly([[x + 8.5, -48], [x + 10.2, -39], [x + 6.8, -39]]));
  }
  parts.push(P(egg.join(""), { fill: "@gold", op: 0.7 }));
  parts.push(P(dart.join(""), { fill: "@gold-lit", op: 0.45 }));
  // Two long tassels at the swag joins.
  for (const [x, dl] of [
    [88, 0],
    [184, -1.1],
  ]) {
    parts.push({ d: `M${x} -30L${x} 30`, z: "front", stroke: "@cord", sw: 2, a: "swing", or: `${x}px -30px`, dl });
    parts.push(
      P(oval(x, 36, 6, 7) + poly([[x - 5, 41], [x + 5, 41], [x + 3.2, 60], [x - 3.2, 60]]), {
        fill: "@gold",
        a: "swing",
        or: `${x}px -30px`,
        dl,
      }),
    );
    parts.push(P(poly([[x - 5, 41], [x + 5, 41], [x + 4.4, 45], [x - 4.4, 45]]), { fill: "@gold-lit", op: 0.7, a: "swing", or: `${x}px -30px`, dl }));
  }

  // ── the boards, and the lights along the front of them ─────────────────
  parts.push(P(rect(-34, 380, 340, 46), { fill: "@boards" }));
  const planks = [];
  for (let x = -32; x < 306; x += 21 + g() * 7) planks.push(rect(x, 380, 1.4, 46));
  parts.push(P(planks.join(""), { fill: "#2a1c10", op: 0.8 }));
  parts.push(P(rect(-34, 380, 340, 46), { fill: "@boards", f: "wood", op: 0.5 }));
  parts.push(P(rect(-34, 378, 340, 3), { fill: "@gold", op: 0.5 }));
  // The trough, its brass reflectors, and the wash they throw back up.
  parts.push(P(rect(-34, 396, 340, 30), { fill: "@brass-dk" }));
  parts.push(P(rect(-34, 394, 340, 4), { fill: "@gold-lit", op: 0.8 }));
  for (let i = 0; i < 11; i++) {
    const x = -18 + i * 30;
    parts.push(P(oval(x, 392, 24, 34), { fill: "@foot", a: "glow", dl: -i * 0.42, op: 0.85 }));
    parts.push(P(poly([[x - 11, 400], [x + 11, 400], [x + 7, 386], [x - 7, 386]]), { fill: "@brass" }));
    parts.push(P(dot(x, 390, 6), { fill: "#ffeec0", a: "glow", dl: -i * 0.42 }));
    parts.push(P(dot(x - 1.6, 388, 2.2), { fill: "#ffffff", op: 0.85 }));
  }
  // Motes riding the up-draught off the lamps.
  for (const [x, dl] of [
    [30, 0],
    [96, -2.6],
    [176, -4.4],
    [244, -6.8],
  ]) {
    parts.push(P(dot(x, 384, 1.6), { fill: "#ffe3ac", a: "float-up", dl, op: 0.8 }));
  }

  // ── the proscenium itself, in front of everything ──────────────────────
  // Gilt plaster over the curtain edges: the arch is what the drapes hang
  // BEHIND, and without it they were two red shapes stuck to the card's sides.
  for (const [x0, dir] of [
    [-20, 1],
    [272, -1],
  ]) {
    parts.push(P(rect(x0, -56, 20, 482), { fill: "@pros" }));
    const flute = [];
    for (let k = 0; k < 3; k++) flute.push(rect(x0 + 4 + k * 4.6, -30, 2, 440));
    parts.push(P(flute.join(""), { fill: "#170e19", op: 0.5 }));
    parts.push(P(rect(x0 + (dir > 0 ? 18 : 0), -56, 2.2, 482), { fill: "@gold", op: 0.7 }));
    const rosette = [];
    for (let k = 0; k < 3; k++) rosette.push(dot(x0 + 10, 66 + k * 130, 7));
    parts.push(P(rosette.join(""), { fill: "@gold" }));
    parts.push(P(rosette.map((_, k) => dot(x0 + 10, 66 + k * 130, 3.4)).join(""), { fill: "@pros" }));
    parts.push(P(rosette.map((_, k) => dot(x0 + 8.6, 64 + k * 130, 1.6)).join(""), { fill: "@gold-lit", op: 0.8 }));
  }
  parts.push(P(rect(-24, -76, 324, 22), { fill: "@pros" }));
  const dent = [];
  for (let x = -22; x < 300; x += 12) dent.push(rect(x, -60, 7, 6));
  parts.push(P(dent.join(""), { fill: "@gold", op: 0.4 }));
  parts.push(P(rect(-24, -76, 324, 3), { fill: "@gold-lit", op: 0.55 }));
  // The cartouche goes on last, over the entablature — put on before it, the
  // arch simply covered it up.
  parts.push(P(oval(136, -66, 30, 19), { fill: "@gold" }));
  parts.push(P(oval(136, -66, 24, 14), { fill: "@pros" }));
  parts.push(P(oval(136, -66, 24, 14), { fill: "c2", op: 0.55 }));
  parts.push(P(oval(136, -70, 19, 7), { fill: "@gold-lit", op: 0.4 }));
  const scroll2 = [];
  for (const sgn of [-1, 1]) {
    scroll2.push(poly([[136 + sgn * 29, -66], [136 + sgn * 44, -74], [136 + sgn * 49, -63], [136 + sgn * 37, -59]]));
    scroll2.push(dot(136 + sgn * 45, -66, 4.4));
  }
  parts.push(P(scroll2.join(""), { fill: "@gold" }));
  parts.push(P(dot(136 - 45, -66, 2.2) + dot(136 + 45, -66, 2.2), { fill: "@pros" }));

  parts.push(...occlusion(0.8));

  return {
    id: "velvet-stage",
    name: "Velvet stage",
    group: "Stone & stage",
    // A gilt proscenium, swagged valance and drawn velvet, with the footlights
    // burning along the boards and throwing the whole thing upward.
    grads: [
      // Behind: the stage, washed in the wearer's colours.
      lin("spot", 136, -30, 136, 300, [
        [0, "c1", 0.75],
        [0.45, "c1", 0.3],
        [1, "c1", 0],
      ]),
      glow("cyc", [
        [0, "#ffd79a", 0.5],
        [0.5, "#c9803c", 0.18],
        [1, "#8a4a26", 0],
      ]),
      // Velvet. Almost black at the edge of every fold, a narrow bright roll a
      // third of the way across, and nothing like a linear ramp between them.
      tube("velvet", [
        [0, "#1b0810"],
        [0.14, "#4c0f1d"],
        [0.3, "#9c2233"],
        [0.4, "#c04452"],
        [0.52, "#7c1928"],
        [0.78, "#360b15"],
        [1, "#1b0810"],
      ]),
      tube("velvet-b", [
        [0, "#170710"],
        [0.16, "#420d1a"],
        [0.34, "#871c2c"],
        [0.44, "#a63340"],
        [0.58, "#640f1f"],
        [0.82, "#2c0912"],
        [1, "#170710"],
      ]),
      lin("velvet-dk", 0, -40, 0, 416, [
        [0, "#200a12"],
        [1, "#12060c"],
      ]),
      tube("swag", [
        [0, "#3c0c17"],
        [0.5, "#a3273a"],
        [1, "#3c0c17"],
      ]),
      tube("swag-b", [
        [0, "#2c0913"],
        [0.5, "#821c2c"],
        [1, "#2c0913"],
      ]),
      lin("pelmet", 0, -56, 0, -30, [
        [0, "#5a1020"],
        [1, "#2c0913"],
      ]),
      // The footlights are BELOW everything, so the wash runs the other way
      // from every other light in the set.
      wash("footwash", [
        [0, "#2b1030", 0.22],
        [0.55, "#a03a2a", 0],
        [0.86, "#e8873c", 0.3],
        [1, "#ffc266", 0.55],
      ]),
      wash("nap", [
        [0, "#f0c8b0", 0.5],
        [1, "#f0c8b0", 0.2],
      ]),
      forged("gold", "#4a3008", "#a8781f", "#f2dc96"),
      lin("gold-lit", 0, -80, 0, 424, [
        [0, "#ffeaa8"],
        [1, "#c99a37"],
      ]),
      lin("cord", 0, 100, 0, 200, [
        [0, "#c9971f"],
        [1, "#7a5410"],
      ]),
      // The plaster of the arch: cool violet, so the velvet in front of it does
      // not have to compete with another red.
      plane("pros", [
        [0, "#4f3d56"],
        [0.35, "#2e2338"],
        [1, "#130e1b"],
      ]),
      plane("boards", [
        [0, "#7d5a34"],
        [0.4, "#553b22"],
        [1, "#2c1d10"],
      ]),
      lin("brass", 0, 386, 0, 402, [
        [0, "#e8c46a"],
        [0.5, "#9a7422"],
        [1, "#4e380d"],
      ]),
      lin("brass-dk", 0, 396, 0, 426, [
        [0, "#4b350e"],
        [1, "#1d1406"],
      ]),
      glow("foot", [
        [0, "#ffdf9b", 0.7],
        [0.5, "#e8933a", 0.28],
        [1, "#c05a1e", 0],
      ]),
      ...occlusionGrads("#170610"),
    ],
    filters: [
      // Velvet nap: fine and vertical, so it reads as pile rather than as dust.
      grain("pile", "0.05 2.4", { oct: 2, seed: 13, k: 0.8 }),
      grain("wood", "0.04 1.8", { oct: 2, seed: 3, k: 0.85 }),
      soft("beam", 11),
    ],
    parts,
  };
})();

const auroraRidge = (() => {
  const parts = [];

  // Ribbons of light over the brow. Each is filled with its own top-to-bottom
  // fade so the lower edge dissolves instead of ending in a straight line
  // across somebody's banner.
  [
    ["@rib1", -78, 26, 0],
    ["@rib2", -54, 34, -4.2],
    ["@rib3", -30, 22, -8.6],
  ].forEach(([c, y, amp, dl], i) => {
    const top = [];
    const bot = [];
    for (let x = -60; x <= 336; x += 22) {
      const w = Math.sin(x / 46 + i * 1.7) * amp;
      top.push([x, y + w]);
      bot.push([x, y + w + 62 + Math.cos(x / 60 + i) * 18]);
    }
    parts.push(
      P(smooth(top) + smooth(bot.reverse()).replace(/^M/, "L") + "Z", {
        fill: c,
        a: i % 2 ? "aurora" : "aurora-slow",
        or: "136px -60px",
        dl,
      }),
    );
  });

  // Stars, twinkling above the card.
  const g = rnd(17);
  for (let i = 0; i < 26; i++) {
    const x = -40 + g() * 356;
    const y = -76 + g() * 66;
    parts.push(BK(dot(x, y, 0.7 + g() * 1.5), { fill: "#eaf3ff", a: "twinkle", dl: -g() * 4, op: 0.9 }));
  }

  // Ridge lines: two layers of peaks that only rise at the corners, so the
  // middle of the foot stays clear.
  const ridge = (pts, fill, op) => parts.push(P(poly([[-30, 424], ...pts, [302, 424]]), { fill, op }));
  // Ridge lines, two layers deep. They are lit, not black: a silhouette in
  // #16233f on a dark card is a silhouette nobody can see.
  ridge(
    [
      [-30, 380],
      [-4, 346],
      [24, 380],
      [56, 364],
      [92, 396],
      [180, 398],
      [214, 368],
      [248, 342],
      [278, 380],
      [302, 366],
    ],
    "#33507f",
    1,
  );
  parts.push(
    P(
      poly([[-11, 352], [-4, 346], [10, 366], [3, 362], [-3, 367]]) +
        poly([[241, 350], [248, 342], [262, 366], [254, 361], [248, 366]]),
      { fill: "#eef5fd", op: 0.95 },
    ),
  );
  ridge(
    [
      [-30, 402],
      [10, 380],
      [44, 402],
      [120, 410],
      [206, 402],
      [238, 380],
      [272, 402],
      [302, 394],
    ],
    "#1c2b4d",
    1,
  );
  // Conifers picked out on the near ridge.
  for (const [x, h] of [
    [14, 24],
    [27, 16],
    [248, 22],
    [235, 14],
    [60, 12],
    [222, 11],
  ]) {
    parts.push(P(tri([x - h * 0.34, 404], [x + h * 0.34, 404], [x, 404 - h]), { fill: "#0d1526" }));
  }

  // Cold rails: a thin fade down each edge keeps the frame continuous.
  parts.push(P(rect(0, 30, 7, 360), { fill: "@edge", op: 0.75 }));
  parts.push(P(rect(265, 30, 7, 360), { fill: "@edge", op: 0.75 }));

  return {
    id: "aurora-ridge",
    name: "Aurora ridge",
    group: "After dark",
    // Ribbons of light shifting above the brow, stars going in and out and a
    // snow-capped ridge along the foot.
    grads: [
      lin("rib1", 0, -78, 0, 20, [
        [0, "c1", 0],
        [0.3, "c1", 0.7],
        [1, "c1", 0],
      ]),
      lin("rib2", 0, -54, 0, 44, [
        [0, "c2", 0],
        [0.3, "c2", 0.6],
        [1, "c2", 0],
      ]),
      lin("rib3", 0, -30, 0, 68, [
        [0, "#6ef2c0", 0],
        [0.3, "#6ef2c0", 0.5],
        [1, "#6ef2c0", 0],
      ]),
      lin("edge", 0, 30, 0, 378, [
        [0, "#6ef2c0", 0.55],
        [0.5, "#2a3f6b", 0.35],
        [1, "#1c2b4d", 0.7],
      ]),
    ],
    parts,
  };
})();

const mushroomHollow = (() => {
  const CAP = ["#c94f5a", "#e0737c", "#a63a49"];
  const STEM = "#f0e6d2";
  const VINE = "#3f6b32";
  const parts = [];

  // A big cap arching over one corner from behind the card.
  parts.push(BK(rect(212, -40, 12, 90), { fill: STEM }));
  parts.push(BK(poly([...arcPts(218, -34, 56, 180, 360, 18)]), { fill: CAP[0] }));
  parts.push(BK(poly([...arcPts(218, -34, 56, 180, 270, 10), [218, -34]]), { fill: CAP[1], op: 0.5 }));
  const spots = [];
  const g = rnd(5);
  for (let i = 0; i < 9; i++) {
    const a = 188 + g() * 164;
    const r = 20 + g() * 30;
    spots.push(dot(218 + Math.cos(rad(a)) * r, -34 + Math.sin(rad(a)) * r * 0.9, 3 + g() * 4));
  }
  parts.push(BK(spots.join(""), { fill: "#f6efe0", op: 0.9 }));
  parts.push(BK(rect(106, -44, 8, 60) + poly([...arcPts(110, -40, 30, 180, 360, 12)]), { fill: CAP[2] }));
  parts.push(BK(dot(96, -52, 4) + dot(118, -58, 3.4) + dot(110, -44, 3), { fill: "#f6efe0", op: 0.8 }));

  // Vines climbing both rails, with leaves turning inward.
  for (const [x, dir, dl] of [
    [7, 1, 0],
    [265, -1, -2.6],
  ]) {
    const pts = [];
    for (let y = -10; y <= 410; y += 16) pts.push([x + Math.sin(y / 34) * 5 * dir, y]);
    parts.push(SK(smooth(pts), { stroke: VINE, sw: 2.6, a: "sway-slow", or: `${x}px 200px`, dl }));
    const leaves = [];
    pts.forEach((p, i) => {
      if (i % 2) return;
      leaves.push(oval(p[0] + 8 * dir, p[1], 8, 4.2, dir * 26));
      leaves.push(oval(p[0] - 3 * dir, p[1] + 8, 5.5, 3, -dir * 30));
    });
    parts.push(P(leaves.join(""), { fill: "#4f8a3c", a: "sway-slow", or: `${x}px 200px`, dl }));
  }

  // A ring of toadstools along the foot, largest in the corners.
  parts.push(P(waveTop(-30, 302, 398, 4, 96, 1.4, 420), { fill: "#3f5c33" }));
  const shrooms = [
    [18, 404, 17, 0],
    [40, 408, 10, 1],
    [58, 410, 7, 2],
    [252, 402, 15, 0],
    [232, 408, 9, 2],
    [214, 411, 6, 1],
    [136, 412, 8, 1],
  ];
  for (const [x, y, r, tone] of shrooms) {
    parts.push(P(rect(x - r * 0.24, y - r * 0.7, r * 0.48, r + 8), { fill: "@stem" }));
    parts.push(P(poly([...arcPts(x, y - r * 0.5, r, 180, 360, 12)]), { fill: CAP[tone] }));
    const sp = [];
    const gg = rnd(x + r);
    for (let i = 0; i < 5; i++) sp.push(dot(x - r * 0.7 + gg() * r * 1.4, y - r * 0.5 - gg() * r * 0.7, 1.2 + gg() * 1.6));
    parts.push(P(sp.join(""), { fill: "#f6efe0", op: 0.9 }));
  }

  // Spores drifting up the margins.
  for (const [x, dl, r] of [
    [26, 0, 2.4],
    [16, -3.3, 1.6],
    [248, -1.7, 2.2],
    [258, -5.1, 1.4],
    [34, -6.8, 1.8],
  ]) {
    parts.push(P(dot(x, 350, r * 3), { fill: "@spore", a: "float-up", dl, op: 0.6 }));
    parts.push(P(dot(x, 350, r), { fill: "#fff3c4", a: "float-up", dl, op: 0.9 }));
  }

  return {
    id: "mushroom-hollow",
    name: "Mushroom hollow",
    group: "Woodland",
    // A giant cap leaning over one corner, vines up both rails and a fairy ring
    // of toadstools along the foot, with spores drifting up through it.
    grads: [
      glow("spore", [
        [0, "#fff3c4", 0.8],
        [1, "#fff3c4", 0],
      ]),
      bark("stem", "#b9ad93", STEM, "#fffaf0"),
    ],
    parts,
  };
})();

const emberForge = (() => {
  // A forge at night, and the only frame in the set with TWO light sources
  // pulling in opposite directions: cold moonlight down the top of every bar
  // and the fire coming up under it. Iron is the material that shows that
  // split most plainly — a blue-white edge along the top of a bar and an
  // orange one along the bottom, with almost nothing in between — and the old
  // frame, which was flat grey rectangles with rivets on, had neither.
  const parts = [];
  const g = rnd(37);

  // ── the hood, and what is going up it ──────────────────────────────────
  parts.push(BK(oval(136, -110, 120, 96), { fill: "@updraught" }));
  // Sparks riding the draught, well above the card.
  for (let i = 0; i < 16; i++) {
    parts.push(BK(dot(94 + g() * 84, -104 - g() * 26, 0.8 + g() * 1.7), { fill: "#ffc255", a: "float-up", dl: -g() * 8, op: 0.85 }));
  }
  // The hood: plates, seams and a cowl. Shallow on purpose — a tall one fills
  // the whole top band with a black pyramid and stops being a hood at all.
  parts.push(P(poly([[-28, 24], [300, 24], [176, -54], [96, -54]]), { fill: "@plate" }));
  parts.push(P(poly([[-28, 24], [300, 24], [176, -54], [96, -54]]), { fill: "@plate", f: "scale", op: 0.4 }));
  parts.push(P(poly([[-28, 24], [136, 24], [136, -54], [96, -54]]), { fill: "@sheen", op: 0.3 }));
  const seams = [];
  for (let k = 1; k < 4; k++) {
    const t = k / 4;
    const y = 24 - 78 * t;
    const x0 = -28 + (96 + 28) * t;
    const x1 = 300 - (300 - 176) * t;
    seams.push(rect(x0, y, x1 - x0, 2.2));
  }
  parts.push(P(seams.join(""), { fill: "#070910", op: 0.8 }));
  parts.push(P(rect(-30, 20, 332, 8), { fill: "@plate-lit" }));
  parts.push(P(rect(-30, 18, 332, 2.4), { fill: "@steel", op: 0.7 }));
  // The flange under it takes the fire from below, which is the one surface in
  // the top half of this frame that is allowed to be warm.
  parts.push(P(rect(-30, 24, 332, 4), { fill: "@underlight", op: 0.75 }));
  // Stays from the posts up into the hood, so it is carried rather than
  // hovering.
  parts.push(P(poly([[6, 30], [12, 30], [52, 6], [46, 4]]) + poly([[266, 30], [260, 30], [220, 6], [226, 4]]), { fill: "@iron-l" }));
  parts.push(P(dot(49, 5, 2.2) + dot(223, 5, 2.2) + dot(9, 30, 2.2) + dot(263, 30, 2.2), { fill: "@rivet" }));
  parts.push(P(rect(100, -86, 72, 34), { fill: "@plate" }));
  parts.push(P(rect(96, -94, 80, 9), { fill: "@plate-lit" }));
  parts.push(P(rect(96, -94, 80, 2.2), { fill: "@steel", op: 0.9 }));
  parts.push(P(poly([[92, -94], [180, -94], [172, -106], [100, -106]]), { fill: "@plate-lit", op: 0.85 }));
  parts.push(P(rect(92, -108, 88, 3), { fill: "@steel", op: 0.6 }));
  // Rivets: down every seam, because that is where a plate is fastened.
  const riv = [];
  for (let k = 0; k <= 4; k++) {
    const t = k / 4;
    const y = 24 - 78 * t + 4;
    const x0 = -28 + (96 + 28) * t + 6;
    const x1 = 300 - (300 - 176) * t - 6;
    for (let x = x0; x < x1; x += 24) riv.push(dot(x, y, 2.3));
  }
  parts.push(P(riv.join(""), { fill: "@rivet" }));
  parts.push(P(streaks(-24, 120, 28, 51, { n: 8, len: 9, w: 1.6 }), { fill: "@rust", op: 0.2 }));

  // ── the posts ──────────────────────────────────────────────────────────
  // Forged bars, not extrusions: hammer facets down the length, bolt plates
  // where they are actually fixed, and a twisted section on one side only.
  for (const [x0, side] of [
    [0, 1],
    [255, -1],
  ]) {
    parts.push(P(rect(x0, 18, 17, 380), { fill: "@iron" }));
    parts.push(P(rect(x0, 18, 17, 380), { fill: "@iron", f: "scale", op: 0.45 }));
    const facets = [];
    for (let y = 22; y < 396; y += 13) {
      const w = 3 + g() * 5;
      facets.push(poly([[x0 + 2, y], [x0 + 2 + w, y + 2], [x0 + 2 + w, y + 9], [x0 + 2, y + 7]]));
    }
    parts.push(P(facets.join(""), { fill: "@steel", op: 0.16 }));
    parts.push(P(rect(x0 + (side > 0 ? 0 : 15), 18, 2, 380), { fill: "@steel", op: 0.5 }));
    parts.push(P(rect(x0 + (side > 0 ? 15 : 0), 18, 2, 380), { fill: "#05070c", op: 0.7 }));
    // Bolt plates.
    for (const by of side > 0 ? [72, 196, 322] : [110, 268]) {
      parts.push(P(rect(x0 - 3, by, 23, 22), { fill: "@plate" }));
      parts.push(P(rect(x0 - 3, by, 23, 2), { fill: "@steel", op: 0.7 }));
      parts.push(P(dot(x0 + 2, by + 5, 2.4) + dot(x0 + 15, by + 5, 2.4) + dot(x0 + 2, by + 17, 2.4) + dot(x0 + 15, by + 17, 2.4), { fill: "@rivet" }));
    }
    // Rust and scale, heavier on the left where the water tub stands.
    const rust = [];
    for (let i = 0; i < (side > 0 ? 10 : 5); i++) {
      const t = g();
      rust.push(blob(x0 + 2 + g() * 13, 120 + t * t * 270, 2 + g() * 3.4, { squash: 0.7, wob: 0.5, seed: 60 + i + x0 }));
    }
    parts.push(P(rust.join(""), { fill: "@rust", op: 0.3 }));
  }
  // A twist worked into the right-hand post, which is the sort of thing a
  // smith does to a bar when nobody is paying by the hour.
  const twist = [];
  for (let i = 0; i < 7; i++) {
    const y = 150 + i * 15;
    twist.push(poly([[256, y], [271, y + 5], [271, y + 12], [256, y + 7]]));
  }
  parts.push(P(twist.join(""), { fill: "@steel", op: 0.22 }));
  parts.push(P(rect(254, 146, 20, 5) + rect(254, 258, 20, 5), { fill: "@plate" }));

  // Wrought scrolls in the top corners, and a hook and chain off the lintel.
  for (const [sx, dir] of [
    [17, 1],
    [255, -1],
  ]) {
    const s1 = spiral(sx + dir * 15, 42, 1.6, 10, 1.1, dir > 0 ? 200 : -20, dir);
    const s2 = spiral(sx + dir * 10, 66, 1.4, 7, 0.95, dir > 0 ? 20 : 160, -dir);
    const arm = `M${sx} 26Q${r2(sx + dir * 16)} 28 ${r2(sx + dir * 19)} 42`;
    parts.push({ d: s1 + s2 + arm, z: "front", stroke: "#0a0d14", sw: 4.4, op: 0.9 });
    parts.push({ d: s1 + s2 + arm, z: "front", stroke: "@iron-l", sw: 2.8 });
    parts.push({ d: s1 + arm, z: "front", stroke: "@steel", sw: 1, op: 0.55 });
  }
  const chain = [];
  for (let i = 0; i < 6; i++) chain.push(oval(232, 26 + i * 9, 3.2, 5));
  parts.push(P(chain.join(""), { fill: "@link", a: "swing", or: "232px 24px" }));
  parts.push({ d: "M232 76Q232 92 224 94Q216 96 218 86", z: "front", stroke: "@link", sw: 3, a: "swing", or: "232px 24px" });

  // ── braziers ───────────────────────────────────────────────────────────
  for (const [x, y, dl] of [
    [16, 128, 0],
    [256, 214, -0.7],
  ]) {
    parts.push(P(oval(x, y - 16, 40, 56), { fill: "@ember", a: "glow", dl, op: 0.9 }));
    parts.push({ d: `M${x} ${y - 74}L${x} ${y - 22}`, z: "front", stroke: "@iron-l", sw: 1.8, op: 0.9 });
    parts.push(P(poly([[x - 14, y - 22], [x + 14, y - 22], [x + 9, y + 2], [x - 9, y + 2]]), { fill: "@iron" }));
    parts.push(P(poly([[x - 14, y - 22], [x + 14, y - 22], [x + 13, y - 18], [x - 13, y - 18]]), { fill: "@steel", op: 0.5 }));
    const bars = [];
    for (let k = 0; k < 5; k++) bars.push(poly([[x - 12 + k * 6, y - 20], [x - 10.6 + k * 6, y - 20], [x - 7.4 + k * 4.2, y + 1], [x - 8.6 + k * 4.2, y + 1]]));
    parts.push(P(bars.join(""), { fill: "#05070c", op: 0.75 }));
    parts.push(P(flame(x, y - 22, 34, 11, 5), { fill: "#d8410f", a: "flick", or: `${x}px ${y - 22}px`, dl }));
    parts.push(P(flame(x, y - 23, 25, 7, 9), { fill: "#ff8d1e", a: "flick", or: `${x}px ${y - 23}px`, dl: dl - 0.2 }));
    parts.push(P(flame(x, y - 24, 14, 3.6, 13), { fill: "#ffe6a2", a: "flick", or: `${x}px ${y - 24}px`, dl: dl - 0.36 }));
    for (let k = 0; k < 3; k++) parts.push(P(dot(x + (k - 1) * 4, y - 30, 1.2 + k * 0.3), { fill: "#ffc255", a: "float-up", dl: dl - k * 2.4, op: 0.9 }));
  }

  // ── the fire ───────────────────────────────────────────────────────────
  // Built the way a coal bed actually reads: the incandescence goes down
  // FIRST, and the black crust is laid over it with gaps, so what glows is the
  // space between the lumps rather than the lumps themselves.
  parts.push(P(rect(-40, 374, 352, 14), { fill: "@hearth" }));
  parts.push(P(rect(-40, 374, 352, 2.4), { fill: "@steel", op: 0.5 }));
  parts.push(P(rect(-40, 387, 352, 39), { fill: "@fire" }));
  const crust = [];
  for (let i = 0; i < 54; i++) {
    crust.push(blob(-38 + g() * 350, 393 + g() * 28, 4 + g() * 6, { squash: 0.72, wob: 0.36, seed: 200 + i }));
  }
  parts.push(P(crust.join(""), { fill: "@coal" }));
  const crustLit = [];
  for (let i = 0; i < 26; i++) {
    crustLit.push(blob(-34 + g() * 344, 391 + g() * 11, 3 + g() * 4, { squash: 0.6, wob: 0.4, seed: 260 + i }));
  }
  parts.push(P(crustLit.join(""), { fill: "@coal-lit", op: 0.85 }));
  for (let k = 0; k < 3; k++) {
    const hot = [];
    for (let i = 0; i < 12; i++) hot.push(blob(-30 + k * 9 + i * 27, 402 + g() * 9, 3.4 + g() * 3, { squash: 0.7, seed: 300 + i + k * 17 }));
    parts.push(P(hot.join(""), { fill: k === 0 ? "#ffd873" : k === 1 ? "#ff8f22" : "#e2500f", a: "coals", dl: -k * 1.3, op: 0.92 }));
  }
  // Heat coming off it, and the light it throws back up the posts.
  parts.push(P(waveTop(-40, 312, 380, 7, 76, 0.5, 408), { fill: "@heat", op: 0.5, a: "heat" }));
  parts.push(P(waveTop(-40, 312, 372, 9, 104, 2.4, 402), { fill: "@heat", op: 0.35, a: "heat", dl: -2.4 }));
  parts.push(P(rect(0, 150, 17, 246) + rect(255, 150, 17, 246), { fill: "@underlight", op: 0.75 }));

  // The anvil, hung off the left of the frame on its stump; the slack tub,
  // steaming, off the right. Two different objects, on purpose.
  parts.push(P(poly([[-46, 402], [4, 402], [4, 424], [-46, 424]]), { fill: "@stump" }));
  parts.push(P(poly([[-46, 402], [4, 402], [4, 405], [-46, 405]]), { fill: "@stump-lit", op: 0.8 }));
  parts.push(
    P(
      poly([[-42, 396], [2, 396], [2, 390], [-8, 386], [-8, 378], [-2, 372], [-14, 368], [-34, 370], [-40, 378], [-38, 386], [-42, 390]]),
      { fill: "@iron" },
    ),
  );
  parts.push(P(poly([[-14, 368], [-2, 372], [-8, 378], [-34, 376], [-34, 370]]), { fill: "@steel", op: 0.45 }));
  parts.push(P(oval(-20, 372, 9, 4), { fill: "#ffb03c", a: "glow", op: 0.9 }));
  parts.push({ d: "M-24 372L-24 356M-24 356L-14 348", z: "front", stroke: "@iron-l", sw: 3, a: "swing", or: "-24px 372px" });
  parts.push(P(poly([[276, 424], [316, 424], [312, 388], [280, 388]]), { fill: "@tub" }));
  parts.push(P(rect(278, 386, 36, 5), { fill: "@stump-lit", op: 0.7 }));
  parts.push(P(rect(278, 398, 36, 3) + rect(280, 412, 32, 3), { fill: "@iron-l", op: 0.8 }));
  for (const [sx, dl] of [
    [288, 0],
    [302, -3.4],
  ]) {
    parts.push(P(blob(sx, 380, 7, { squash: 0.8, wob: 0.4, seed: sx }), { fill: "@steam", a: "float-up", dl, op: 0.4 }));
  }
  // Sparks off the bed, along both margins.
  for (const [x, dl, r] of [
    [10, 0, 1.8],
    [22, -2.2, 1.3],
    [4, -4.6, 1.5],
    [262, -1.1, 1.6],
    [250, -3.6, 1.2],
    [270, -6, 1.4],
    [136, -5.2, 1.2],
  ]) {
    parts.push(P(dot(x, 390, r), { fill: "#ffce6b", a: "float-up", dl, op: 0.95 }));
  }

  parts.push(...occlusion(0.85));

  return {
    id: "ember-forge",
    name: "Ember forge",
    group: "Stone & stage",
    // A riveted hood drawing the sparks up, forged posts down both sides with
    // scrollwork in the corners, and a coal bed burning along the foot.
    grads: [
      glow("updraught", [
        [0, "#ff9a35", 0.3],
        [0.5, "#c2551a", 0.1],
        [1, "#7a2f10", 0],
      ]),
      // Iron. Cold blue-white where the night sky reaches it, near-black in
      // between, and warm again at the bottom from the fire.
      tube("iron", [
        [0, "#0d111a"],
        [0.1, "#2b3342"],
        [0.26, "#75839b"],
        [0.42, "#394153"],
        [0.72, "#1b212c"],
        [1, "#0c0f16"],
      ]),
      tube("link", [
        [0, "#11151d"],
        [0.3, "#5d6a80"],
        [0.55, "#242b39"],
        [1, "#0e121a"],
      ]),
      lin("iron-l", 0, 20, 0, 400, [
        [0, "#414a5c"],
        [1, "#1a1f2b"],
      ]),
      tube("plate", [
        [0, "#0e131c"],
        [0.18, "#2a3140"],
        [0.3, "#5d6a80"],
        [0.46, "#262d3b"],
        [1, "#0d111a"],
      ]),
      lin("plate-lit", 0, -140, 0, -100, [
        [0, "#5c667a"],
        [1, "#2d3444"],
      ]),
      lin("steel", 0, -140, 0, 424, [
        [0, "#aebbd2", 0.95],
        [0.5, "#7d8aa2", 0.85],
        [1, "#4e5768", 0.8],
      ]),
      lin("sheen", 0, -100, 0, 24, [
        [0, "#8e9ab4", 0.5],
        [1, "#8e9ab4", 0],
      ]),
      lin("rivet", 0, -140, 0, 400, [
        [0, "#6d7893"],
        [1, "#39414f"],
      ]),
      lin("rust", 0, 0, 0, 400, [
        [0, "#8a4a22"],
        [1, "#5c2f16"],
      ]),
      // The fire, as one field the crust is laid over.
      lin("fire", 0, 378, 0, 424, [
        [0, "#ffe9a8"],
        [0.28, "#ff9c2a"],
        [0.62, "#c8380c"],
        [1, "#5c1405"],
      ]),
      lin("coal", 0, 380, 0, 424, [
        [0, "#2a1d18"],
        [0.5, "#17100e"],
        [1, "#0c0807"],
      ]),
      lin("coal-lit", 0, 378, 0, 400, [
        [0, "#6b3a1c"],
        [1, "#38200f"],
      ]),
      lin("hearth", 0, 360, 0, 384, [
        [0, "#4a4640"],
        [1, "#221f1e"],
      ]),
      wash("heat", [
        [0, "#ffb257", 0],
        [1, "#ffb257", 0.55],
      ]),
      // The bounce back up the posts: this is why the bottom of every bar in
      // the frame is warm and the top of it is not.
      wash("underlight", [
        [0, "#ff8a2a", 0],
        [0.55, "#ff8a2a", 0.16],
        [1, "#ffbc5e", 0.6],
      ]),
      glow("ember", [
        [0, "#ff8c2a", 0.55],
        [0.45, "#d8541a", 0.22],
        [1, "#8a2f0c", 0],
      ]),
      bark("stump", "#1b120b", "#3d2a18", "#6d4c2a"),
      lin("stump-lit", 0, 386, 0, 406, [
        [0, "#8a6437"],
        [1, "#4e361c"],
      ]),
      tube("tub", [
        [0, "#150f0a"],
        [0.24, "#3a2a1a"],
        [0.36, "#5e442a"],
        [0.6, "#2c2013"],
        [1, "#120d08"],
      ]),
      glow("steam", [
        [0, "#d8dee8", 0.55],
        [1, "#d8dee8", 0],
      ]),
      ...occlusionGrads("#0a0509"),
    ],
    // Mill scale: coarse and blotchy, which is what separates a forged surface
    // from a rolled one.
    filters: [grain("scale", "1.3 1.1", { oct: 4, seed: 23, k: 0.85 })],
    parts,
  };
})();

export const CARD_FRAMES = [
  castleKeep,
  cathedral,
  velvetStage,
  emberForge,
  deepWoods,
  sakura,
  frostwood,
  mushroomHollow,
  palmShore,
  coralReef,
  hallowsEve,
  auroraRidge,
];

export const CARD_FRAME_BY_ID = Object.fromEntries(
  CARD_FRAMES.map((f) => [f.id, f]),
);

export const CARD_FRAME_GROUPS = [
  ...new Set(CARD_FRAMES.map((f) => f.group)),
].map((title) => ({
  title,
  ids: CARD_FRAMES.filter((f) => f.group === title).map((f) => f.id),
}));

// cardFrame resolves an id to its definition, or null. Fails CLOSED: an id this
// build has never heard of draws nothing, which is what makes it safe to take
// the value straight off a peer's broadcast profile.
export function cardFrame(id) {
  return CARD_FRAME_BY_ID[id] || null;
}
