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
  const STONE = "#8a9099";
  const MID = "#666d78";
  const DARK = "#454b55";
  const parts = [];

  // Two towers standing BEHIND the card: only the strip outside each edge and
  // everything above the top edge is visible, which is exactly what sells the
  // depth. Drawn first so the wall in front of them reads as nearer.
  // BACK stone is deliberately darker than the rails in front of it. Two greys
  // at the same value read as one flat wall, which is exactly how the first
  // draft of this frame failed.
  const BACK_STONE = "#5b626c";
  const BACK_DARK = "#3a4049";
  for (const [cx, flip] of [
    [4, 1],
    [268, -1],
  ]) {
    parts.push(BK(rect(cx - 26, -46, 52, 210), { fill: "@tower" }));
    parts.push(BK(rect(cx - 26, -46, 52, 7), { fill: BACK_DARK }));
    parts.push(BK(crenel(cx - 29, cx + 29, -62, -78, -44, 10, 9), { fill: "@crown" }));
    parts.push(BK(tri([cx - 34, -78], [cx + 34, -78], [cx, -132]), { fill: "c1" }));
    parts.push(BK(tri([cx, -78], [cx + 34 * flip, -78], [cx, -132]), { fill: "ink", op: 0.28 }));
    parts.push(BK(rect(cx - 1.5, -158, 3, 28), { fill: BACK_DARK }));
    parts.push(
      BK(poly([[cx, -156], [cx + 34 * flip, -145], [cx, -134]]), {
        fill: "c2",
        a: "wave-flag",
        or: `${cx}px -145px`,
        dl: flip > 0 ? 0 : -1.3,
      }),
    );
    // Arrow slits, on the visible outer strip only.
    for (const y of [-26, 8, 42]) {
      parts.push(BK(rect(cx - (flip > 0 ? 22 : 19) * flip, y, 3, 13), { fill: BACK_DARK }));
    }
  }

  // The wall across the top edge, in front of the card.
  parts.push(P(crenel(-6, 278, -6, -22, 26, 16, 12), { fill: "@wall" }));
  parts.push(P(crenel(-6, 278, -6, -22, 26, 16, 12), { fill: "@wall", f: "stone", op: 0.6 }));
  // The lit top edge of the merlons — a rim light, which does more for "solid"
  // than another hundred marks would.
  parts.push(P(crenel(-6, 278, -6, -20, -18, 16, 12), { fill: "@rim", op: 0.85 }));
  parts.push(P(rect(-6, 20, 284, 6), { fill: DARK }));
  // The wall's own shadow on the course below it.
  parts.push(P(rect(-6, 20, 284, 3), { fill: "#05070c", op: 0.4 }));
  const courses = [];
  for (let x = -6; x < 278; x += 22) courses.push(rect(x, -6, 1.4, 26));
  parts.push(P(courses.join(""), { fill: "@mortar" }));

  // Masonry rails, as individual BLOCKS rather than a striped panel. A wall
  // built from one rectangle with lines ruled across it is the thing that most
  // says "drawn": real coursing has blocks of different lengths, joints that
  // do not line up between courses, and corners that have lost a chip.
  const g = rnd(7);
  const blocks = [];
  const mortar = [];
  const chips = [];
  for (let i = 0; i < 17; i++) {
    const y = 26 + i * 20;
    // Each course is split into two or three blocks at a different place, so
    // no two courses share a vertical joint.
    for (const side of [0, 257]) {
      const cut = 5 + Math.round(g() * 6);
      blocks.push(rect(side, y, cut, 18.6));
      blocks.push(rect(side + cut + 0.9, y, 15 - cut - 0.9, 18.6));
      mortar.push(rect(side + cut, y, 0.9, 18.6));
      mortar.push(rect(side, y + 18.6, 15, 1.4));
      // A chipped corner every few blocks, alternating which corner.
      if (g() > 0.62) {
        const cx = side + (g() > 0.5 ? 0 : 11);
        chips.push(tri([cx, y], [cx + 4, y], [cx, y + 4]));
      }
    }
  }
  parts.push(P(blocks.join(""), { fill: "@rail" }));
  parts.push(P(blocks.join(""), { fill: "@rail", f: "stone", op: 0.55 }));
  parts.push(P(mortar.join(""), { fill: "@mortar" }));
  parts.push(P(chips.join(""), { fill: "@chip", op: 0.75 }));
  // Moss gathering in the lower courses, where water sits. Only on one side —
  // a wall weathered identically on both is a wall nobody stood next to.
  const moss = [];
  for (let i = 0; i < 9; i++) {
    const y = 236 + i * 20 + g() * 8;
    moss.push(blob(2 + g() * 11, y, 3 + g() * 4, { squash: 0.55, wob: 0.4, seed: i + 3 }));
  }
  for (let i = 0; i < 4; i++) {
    const y = 300 + i * 26 + g() * 8;
    moss.push(blob(259 + g() * 10, y, 2.5 + g() * 3, { squash: 0.5, wob: 0.4, seed: i + 21 }));
  }
  parts.push(P(moss.join(""), { fill: "@moss", op: 0.5 }));
  parts.push(
    P(
      Array.from({ length: 17 }, (_, i) => rect(0, 26 + i * 20 + 18, 15, 2) + rect(257, 26 + i * 20 + 18, 15, 2)).join(""),
      { fill: DARK, op: 0.7 },
    ),
  );

  // Torches on the rails: a bracket, a flame, a warm wash on the stone.
  for (const [x, dl] of [
    [21, 0],
    [251, -0.55],
  ]) {
    parts.push(P(dot(x, 52, 24), { fill: "@torch", a: "glow", dl, op: 0.9 }));
    parts.push(P(poly([[x - 5, 74], [x + 5, 74], [x + 4, 50], [x - 4, 50]]), { fill: DARK }));
    parts.push(P(rect(x - 7, 46, 14, 6), { fill: MID }));
    parts.push(
      P(poly([[x - 8, 48], [x - 3, 20], [x + 1, 32], [x + 4, 16], [x + 8, 48]]), {
        fill: "#ff9a2e",
        a: "flick",
        or: `${x}px 48px`,
        dl,
      }),
    );
    parts.push(
      P(poly([[x - 4, 47], [x - 1, 30], [x + 1.5, 36], [x + 3, 26], [x + 4.5, 47]]), {
        fill: "#ffe08a",
        a: "flick",
        or: `${x}px 47px`,
        dl: dl - 0.2,
      }),
    );
  }

  // Plinth along the foot, with a sloped batter at each corner.
  parts.push(P(rect(-6, 392, 284, 26), { fill: MID }));
  parts.push(P(rect(-6, 392, 284, 26), { fill: MID, f: "stone", op: 0.5 }));
  parts.push(P(rect(-6, 390, 284, 4), { fill: "@rim", op: 0.8 }));
  const joints = [];
  for (let x = -6; x < 278; x += 26) joints.push(rect(x, 394, 1.6, 16));
  parts.push(P(joints.join(""), { fill: DARK, op: 0.6 }));
  parts.push(P(poly([[0, 416], [0, 358], [15, 358], [30, 394], [30, 416]]), { fill: MID }));
  parts.push(P(poly([[272, 416], [272, 358], [257, 358], [242, 394], [242, 416]]), { fill: MID }));
  parts.push(P(poly([[0, 360], [15, 360], [17, 366], [0, 366]]) + poly([[272, 360], [257, 360], [255, 366], [272, 366]]), { fill: STONE }));

  // The shadow the keep throws onto the card it stands around. Last, so it
  // falls across everything, and the reason the frame reads as an object in
  // front of something rather than a border printed at the same depth.
  parts.push(...occlusion());

  return {
    id: "castle-keep",
    name: "Castle keep",
    group: "Stone & stage",
    // Two towers behind the card, a battlemented wall along its brow, torches
    // guttering on the masonry rails and pennants snapping over the top.
    grads: [
      glow("torch", [
        [0, "#ffb347", 0.7],
        [1, "#ffb347", 0],
      ]),
      // The towers stand BEHIND the card, so only a strip of each is ever
      // seen — and a strip of flat grey is the least convincing masonry there
      // is. `rock` gives it a lit face and a hard crease instead.
      rock("tower", "#3f454e", "#5b626c", "#79808b"),
      rock("crown", "#4a515b", "#6d747f", "#8b929d"),
      // The wall across the brow catches the most light: it faces the viewer
      // and stands above everything else.
      rock("wall", "#5a6069", STONE, "#a7adb6"),
      rock("rail", "#41474f", MID, "#848b95"),
      lin("mortar", 0, 0, 0, 400, [
        [0, "#2b3038", 0.85],
        [1, "#22262d", 0.9],
      ]),
      lin("chip", 0, 0, 0, 400, [
        [0, "#aeb5bf", 0.9],
        [1, "#8c939d", 0.9],
      ]),
      lin("rim", 0, 0, 0, 400, [
        [0, "#d6dce4", 0.9],
        [1, "#b9c0c9", 0.7],
      ]),
      lin("moss", 0, 200, 0, 400, [
        [0, "#4a6b3a", 0.8],
        [1, "#38512c", 0.9],
      ]),
      ...occlusionGrads(),
    ],
    // Coarse and low-frequency: masonry is a rough surface at arm's length,
    // not sandpaper. A fine grain here reads as noise on a screen rather than
    // as stone.
    filters: [grain("stone", "0.9 1.1", { oct: 4, seed: 11, k: 0.9 })],
    parts,
  };
})();

const hallowsEve = (() => {
  const BARK = "#241b2c";
  const BARK2 = "#3a2d45";
  const WEB = "#c9d2e2";
  const parts = [];

  // The moon, mostly behind the card so it rises out of the top edge.
  parts.push(BK(dot(214, -16, 48), { fill: "@halo", op: 0.8 }));
  parts.push(BK(dot(214, -16, 30), { fill: "#f6ecc9" }));
  parts.push(BK(dot(226, -26, 6), { fill: "#e2d6ad", op: 0.7 }));
  parts.push(BK(dot(203, -8, 4), { fill: "#e2d6ad", op: 0.6 }));

  // Bare branches reaching in over the top corners, behind the card.
  const twigs = [];
  const boughs = [
    [-24, 8, -34, 128, 7.5, 1.8, 44],
    [-20, 40, -10, 96, 5, 1.3, 34],
    [296, 6, -146, 136, 7.5, 1.8, -46],
    [292, 38, -170, 102, 5, 1.3, -36],
  ];
  for (const [x, y, a, len, w0, w1, curve] of boughs) {
    const b = limb(x, y, a, len, w0, w1, curve, 10);
    parts.push(BK(b.d, { fill: "@bough" }));
    let t = b.spine[Math.round(b.spine.length * 0.55)];
    twigs.push(limb(t[0], t[1], a + curve * 0.4 + (a < -90 ? 40 : -40), 26, 1.7, 0.5, a < -90 ? -30 : 30, 5).d);
    t = b.tip;
    twigs.push(limb(t[0], t[1], b.ang - 34, 22, 1.6, 0.4, 26, 5).d);
    twigs.push(limb(t[0], t[1], b.ang + 30, 18, 1.4, 0.4, -24, 5).d);
  }
  parts.push(BK(twigs.join(""), { fill: BARK2 }));

  // Bats crossing above the card. Each is one path, moved by a long linear
  // translate — a silhouette leaving one side and arriving at the other.
  const bat = (s) =>
    poly([
      [-9 * s, -1 * s],
      [-5 * s, -4 * s],
      [-2.4 * s, -1.4 * s],
      [0, -4 * s],
      [2.4 * s, -1.4 * s],
      [5 * s, -4 * s],
      [9 * s, -1 * s],
      [4.6 * s, 1.4 * s],
      [0, 0.6 * s],
      [-4.6 * s, 1.4 * s],
    ]);
  parts.push(BK(bat(1.5), { fill: BARK, a: "cross", or: "0px 0px", dl: 0, op: 0.95 }));
  parts.push(BK(bat(1), { fill: BARK, a: "cross-high", or: "0px 0px", dl: -5.5, op: 0.8 }));
  parts.push(BK(bat(0.7), { fill: BARK, a: "cross", or: "0px 0px", dl: -11, op: 0.65 }));

  // Thorny vines down the rails, in front.
  for (const [x, dir] of [
    [4, 1],
    [268, -1],
  ]) {
    const v = limb(x, 20, 90, 356, 3, 2.2, 0, 12);
    parts.push(P(v.d, { fill: BARK2 }));
    const thorns = [];
    for (let i = 0; i < 11; i++) {
      const y = 40 + i * 30;
      thorns.push(tri([x, y], [x + 9 * dir, y - 5], [x, y + 5]));
    }
    parts.push(P(thorns.join(""), { fill: BARK2 }));
  }

  // Cobwebs in every corner, and a spider abseiling out of the top right.
  parts.push(SK(web(-4, -4, 78, 0, 90), { stroke: WEB, sw: 0.8, op: 0.45 }));
  parts.push(SK(web(276, -4, 78, 90, 180), { stroke: WEB, sw: 0.8, op: 0.45 }));
  parts.push(SK(web(-4, 404, 54, -90, 0), { stroke: WEB, sw: 0.8, op: 0.32 }));
  parts.push(SK(web(276, 404, 54, 180, 270), { stroke: WEB, sw: 0.8, op: 0.32 }));
  parts.push(SK("M232 -6L232 50", { stroke: WEB, sw: 0.8, op: 0.5, a: "abseil-line", or: "232px -6px" }));
  parts.push(
    P(oval(232, 57, 5, 6) + dot(232, 50, 3.4), {
      fill: "#1a1420",
      a: "abseil",
    }),
  );
  parts.push(
    SK("M226 54L218 48M226 58L216 59M238 54L246 48M238 58L248 59", {
      stroke: "#1a1420",
      sw: 1.4,
      a: "abseil",
    }),
  );

  // A jack-o'-lantern in one corner, headstones in the other.
  parts.push(P(dot(24, 396, 30), { fill: "@lampglow", a: "glow", op: 0.9 }));
  parts.push(P(oval(24, 398, 20, 17), { fill: "#e0701f" }));
  parts.push(P(oval(14, 398, 8, 16.4), { fill: "#c85f14", op: 0.55 }));
  parts.push(P(oval(34, 398, 8, 16.4), { fill: "#c85f14", op: 0.55 }));
  parts.push(P(rect(22, 376, 5, 8) + oval(24.5, 378, 4, 3), { fill: "#4a6b2c" }));
  parts.push(
    P(
      tri([14, 394], [20, 394], [17, 389]) +
        tri([28, 394], [34, 394], [31, 389]) +
        poly([[13, 404], [35, 404], [31, 410], [27, 406], [21, 406], [17, 410]]),
      { fill: "#ffdc7a", a: "flick", or: "24px 400px" },
    ),
  );
  parts.push(P(poly([[246, 416], [246, 386], [255, 376], [264, 386], [264, 416]]), { fill: "#6b6f78" }));
  parts.push(SK("M255 390L255 406M249 396L261 396", { stroke: "#4c5058", sw: 2 }));
  parts.push(P(poly([[266, 416], [266, 394], [280, 394], [280, 416]]), { fill: "#5a5e66" }));
  const grass = [];
  const gg = rnd(31);
  for (let i = 0; i < 34; i++) {
    const x = -6 + i * 9 + gg() * 5;
    const h = 5 + gg() * 13;
    const lean = (gg() - 0.5) * 12;
    grass.push(`M${r2(x)} 412Q${r2(x + lean * 0.4)} ${r2(412 - h * 0.6)} ${r2(x + lean)} ${r2(412 - h)}`);
  }
  parts.push(SK(grass.join(""), { stroke: "#403c2d", sw: 1.5, a: "sway", or: "136px 412px" }));

  return {
    id: "hallows-eve",
    name: "Hallow's eve",
    group: "After dark",
    // A moon rising behind the card, bats crossing it, webs in all four corners
    // and a lantern guttering at the foot.
    grads: [
      radial("halo", 214, -16, 48, [
        [0, "#f6ecc9", 0.5],
        [1, "#f6ecc9", 0],
      ]),
      radial("lampglow", 24, 398, 30, [
        [0, "#ff9f2e", 0.55],
        [1, "#ff9f2e", 0],
      ]),
      bark("bough", "#140f1a", "#2c2235", "#4a3c58"),
    ],
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
  const PINE = ["#1c3b32", "#26543f", "#33684b"];
  const SNOW = "#eef5fb";
  const ICE = "#bfe1f0";
  const parts = [];

  // Boughs leaning in over the brow from both sides, each with its own snow
  // load drawn a little above it so the load reads as sitting ON the needles.
  const boughs = [
    [-30, -34, -2, 128, 0],
    [302, -34, -178, 128, 0],
    [-26, 4, 12, 104, 1],
    [298, 4, 168, 104, 1],
    [-20, 44, 22, 74, 2],
    [292, 44, 158, 74, 2],
  ];
  boughs.forEach(([x, y, a, len, tone], i) => {
    parts.push(P(limb(x, y, a, len, 3.4, 1, 0, 5).d, { fill: "#3a2f28", a: i % 2 ? "bough" : "bough-slow", or: `${x}px ${y}px`, dl: -i * 0.7 }));
    parts.push(
      P(spray(x, y, a, len, 8, 30), {
        fill: PINE[tone],
        a: i % 2 ? "bough" : "bough-slow",
        or: `${x}px ${y}px`,
        dl: -i * 0.7,
      }),
    );
    parts.push(
      P(spray(x, y - 5, a, len, 8, 19), {
        fill: SNOW,
        op: 0.92,
        a: i % 2 ? "bough" : "bough-slow",
        or: `${x}px ${y}px`,
        dl: -i * 0.7,
      }),
    );
  });

  // A rime ledge along the brow, with icicles hanging off it.
  parts.push(P(waveTop(-14, 286, 0, 3.5, 54, 1.1, -14), { fill: SNOW }));
  const icicles = [];
  const g = rnd(9);
  for (let i = 0; i < 22; i++) {
    const x = -6 + i * 13 + g() * 4;
    const len = 7 + g() * 24;
    icicles.push(poly([[x - 3, 0], [x + 3, 0], [x, len]]));
  }
  parts.push(P(icicles.join(""), { fill: ICE, op: 0.92 }));
  parts.push(P(icicles.join(""), { fill: SNOW, a: "twinkle", op: 0.5 }));

  // Frosted rails: sprigs pointing inward off a thin bare stem.
  for (const [x, dir] of [
    [4, 1],
    [268, -1],
  ]) {
    parts.push(P(limb(x, 14, 90, 372, 3, 2.6, 0, 6).d, { fill: "#3a2f28" }));
    // Sprigs alternate up and down the stem. A column of identical sprays all
    // pointing the same way reads as a row of chevrons, not as a branch.
    for (let i = 0; i < 12; i++) {
      const y = 44 + i * 29;
      const a = (dir > 0 ? 1 : -1) * (i % 2 ? 26 : -22) + (dir > 0 ? 0 : 180);
      parts.push(P(spray(x, y, a, 20, 3, 11), { fill: PINE[i % 3] }));
      parts.push(P(spray(x, y - 2.5, a, 20, 3, 7), { fill: SNOW, op: 0.85 }));
    }
  }
  parts.push(P(waveTop(-30, 302, 392, 6, 130, 0.4, 418), { fill: "#cfe0ee" }));
  parts.push(P(waveTop(-30, 302, 400, 4.5, 92, 2.6, 418), { fill: SNOW }));
  for (const [x, h] of [
    [14, 40],
    [254, 32],
    [32, 24],
  ]) {
    parts.push(P(tri([x - h * 0.4, 406], [x + h * 0.4, 406], [x, 406 - h]), { fill: PINE[0] }));
    parts.push(P(tri([x - h * 0.26, 396], [x + h * 0.26, 396], [x, 406 - h]), { fill: SNOW, op: 0.85 }));
  }

  return {
    id: "frostwood",
    name: "Frostwood",
    group: "Woodland",
    // Snow-laden boughs dipping over the brow, icicles catching the light and a
    // drift banked along the foot.
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
  const STONE = "#918876";
  const MID = "#6e6658";
  const DARK = "#4c463b";
  const parts = [];

  // Twin spires standing behind the card, set outside the arch.
  for (const cx of [-8, 280]) {
    parts.push(BK(rect(cx - 17, -78, 34, 150), { fill: "@tower" }));
    parts.push(BK(rect(cx - 21, -86, 42, 10), { fill: STONE }));
    parts.push(BK(tri([cx - 21, -86], [cx + 21, -86], [cx, -178]), { fill: MID }));
    parts.push(BK(tri([cx - 21, -86], [cx, -86], [cx, -178]), { fill: DARK }));
    parts.push(BK(poly([[cx - 3, -178], [cx + 3, -178], [cx, -196]]), { fill: "c1" }));
    parts.push(BK(rect(cx - 6, -60, 12, 26) + poly([...arcPts(cx, -60, 6, 180, 360, 8)]), { fill: DARK }));
  }

  // The pointed arch. Hand-placed control points, in front of the card: an
  // arch that springs from below the brow and closes above it is the whole
  // reason this frame is not just two columns.
  const outer = [
    [0, 200],
    [0, 62],
    [6, 24],
    [24, -14],
    [58, -48],
    [98, -74],
    [136, -98],
    [174, -74],
    [214, -48],
    [248, -14],
    [266, 24],
    [272, 62],
    [272, 200],
  ];
  const inner = [
    [254, 200],
    [254, 66],
    [248, 34],
    [230, 4],
    [200, -20],
    [168, -40],
    [136, -60],
    [104, -40],
    [72, -20],
    [42, 4],
    [24, 34],
    [18, 66],
    [18, 200],
  ];
  parts.push(P(smooth(outer) + smooth(inner).replace(/^M/, "L") + "Z", { fill: "@arch" }));
  // The lit half. `inner` already runs apex-to-base on this side, so it is NOT
  // reversed — reversing it closes the path across the opening and floods the
  // whole arch head with a translucent slab.
  parts.push(
    P(smooth(outer.slice(0, 7)) + smooth(inner.slice(6)).replace(/^M/, "L") + "Z", {
      fill: MID,
      op: 0.5,
    }),
  );
  // Tracery in the arch head, above the card: a rose window and two lancets.
  parts.push(P(dot(136, -46, 19), { fill: MID }));
  parts.push(P(dot(136, -46, 14), { fill: "@rose", a: "glow" }));
  const petals = [];
  for (let i = 0; i < 6; i++) {
    const a = i * 60;
    petals.push(dot(136 + Math.cos(rad(a)) * 8, -46 + Math.sin(rad(a)) * 8, 3.6));
  }
  parts.push(P(petals.join(""), { fill: MID, op: 0.8 }));
  for (const [cx, c] of [
    [104, "c1"],
    [168, "c2"],
  ]) {
    parts.push(
      P(poly([[cx - 10, -6], ...arcPts(cx, -6, 10, 180, 270, 6), [cx, -30], ...arcPts(cx, -6, 10, 270, 360, 6), [cx + 10, -6]]), {
        fill: MID,
      }),
    );
    parts.push(
      P(poly([[cx - 6, -8], ...arcPts(cx, -8, 6, 180, 270, 5), [cx, -24], ...arcPts(cx, -8, 6, 270, 360, 5), [cx + 6, -8]]), {
        fill: c,
        a: "glow",
      }),
    );
  }

  // Columns down the rails, with capitals under the arch's springing and bases
  // on the altar step.
  for (const x of [0, 256]) {
    parts.push(P(rect(x, 96, 16, 290), { fill: "@column" }));
    parts.push(P(rect(x + (x ? 0 : 12), 96, 4, 290), { fill: MID, op: 0.55 }));
    const flutes = [];
    for (let i = 0; i < 3; i++) flutes.push(rect(x + 4 + i * 3.5, 118, 1.4, 248));
    parts.push(P(flutes.join(""), { fill: DARK, op: 0.35 }));
    parts.push(P(rect(x - 5, 100, 26, 12), { fill: "@cap" }));
    parts.push(P(rect(x - 5, 96, 26, 5), { fill: STONE }));
    parts.push(P(rect(x - 5, 374, 26, 14), { fill: "@cap" }));
    parts.push(P(rect(x - 3, 368, 22, 7), { fill: STONE }));
  }

  // Stained glass slivers on the columns, clear of anything that has to read.
  for (const [x, c] of [
    [4, "c1"],
    [260, "c2"],
  ]) {
    parts.push(P(poly([[x, 158], [x + 8, 158], [x + 8, 136], [x + 4, 128], [x, 136]]), { fill: c, a: "glow" }));
    parts.push(P(poly([[x, 200], [x + 8, 200], [x + 8, 178], [x + 4, 170], [x, 178]]), { fill: c, op: 0.7, a: "glow", dl: -1.5 }));
    parts.push(P(poly([[x, 242], [x + 8, 242], [x + 8, 220], [x + 4, 212], [x, 220]]), { fill: c, op: 0.85, a: "glow", dl: -2.6 }));
  }

  // Altar step and candles at the foot.
  parts.push(P(rect(-8, 394, 288, 22), { fill: MID }));
  parts.push(P(rect(-8, 390, 288, 5), { fill: STONE }));
  for (const [x, dl] of [
    [20, 0],
    [252, -0.5],
  ]) {
    parts.push(P(dot(x, 372, 16), { fill: "@candle", a: "glow", dl, op: 0.9 }));
    parts.push(P(rect(x - 4, 372, 8, 24), { fill: "#f0e6cf" }));
    parts.push(P(poly([[x - 3.4, 372], [x, 358], [x + 3.4, 372]]), { fill: "#ffbf5c", a: "flick", or: `${x}px 372px`, dl }));
    parts.push(P(poly([[x - 1.6, 371], [x, 363], [x + 1.6, 371]]), { fill: "#fff0c0", a: "flick", or: `${x}px 371px`, dl: dl - 0.25 }));
  }

  return {
    id: "cathedral",
    name: "Cathedral",
    group: "Stone & stage",
    // A pointed arch breaking the top edge with a rose window in its head,
    // fluted columns down the rails and candles burning at the foot.
    grads: [
      radial("rose", 136, -18, 15, [
        [0, "#ffe9a8", 1],
        [0.6, "#e0a63c", 1],
        [1, "#8d4f2a", 1],
      ]),
      glow("candle", [
        [0, "#ffcf7a", 0.6],
        [1, "#ffcf7a", 0],
      ]),
      // A column is a cylinder and was drawn as a tan rectangle. `bark` rather
      // than `rock` here on purpose: a fluted column is turned, not faceted,
      // so it wants a rolled highlight instead of two hard planes.
      bark("column", "#4c463b", STONE, "#c0b7a2"),
      rock("cap", "#443f35", MID, "#a9a08d"),
      rock("tower", "#3f3a31", MID, "#9b9280"),
      rock("arch", "#59533f", STONE, "#bdb49f"),
    ],
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
  const VEL = "#8d1b2c";
  const VEL_D = "#59101c";
  const VEL_L = "#b3283d";
  const GOLD = "#d9a441";
  const parts = [];

  // Curtains: wide over the banner, narrowing to a rail below it so nothing
  // the card has to say ends up behind velvet.
  for (const [side, dir] of [
    [0, 1],
    [272, -1],
  ]) {
    const edge = (t) => side + dir * (48 - 34 * Math.min(1, Math.max(0, (t - 8) / 70)));
    const inner = [];
    for (let y = -14; y <= 406; y += 14) inner.push([edge(y), y]);
    parts.push(
      P(poly([[side - dir * 20, -14], ...inner, [side - dir * 20, 406]]), {
        fill: VEL,
        a: "breathe",
        or: `${side}px 200px`,
        dl: dir > 0 ? 0 : -2.2,
      }),
    );
    // Folds: alternating light and dark ribbons following the same taper.
    for (let k = 1; k <= 4; k++) {
      const f = [];
      for (let y = -14; y <= 406; y += 14) {
        const w = (edge(y) - (side - dir * 20)) * (k / 5);
        f.push([side - dir * 20 + w, y]);
      }
      const g = [];
      for (let y = 406; y >= -14; y -= 14) {
        const w = (edge(y) - (side - dir * 20)) * (k / 5 + 0.085);
        g.push([side - dir * 20 + w, y]);
      }
      parts.push(
        P(poly([...f, ...g]), {
          fill: k % 2 ? VEL_D : VEL_L,
          op: 0.75,
          a: "breathe",
          or: `${side}px 200px`,
          dl: dir > 0 ? 0 : -2.2,
        }),
      );
    }
  }

  // Valance: three swags with a gold band and tassels, breaking the top edge.
  const swag = (x0, x1, dip) =>
    `M${x0} -18Q${r2((x0 + x1) / 2)} ${r2(dip)} ${x1} -18L${x1} -30L${x0} -30Z`;
  parts.push(P(rect(-24, -34, 320, 20), { fill: "@pelmet" }));
  parts.push(P(swag(-24, 92, 52) + swag(84, 188, 60) + swag(180, 296, 52), { fill: "@swag" }));
  parts.push(P(swag(-24, 92, 40) + swag(84, 188, 48) + swag(180, 296, 40), { fill: VEL_L, op: 0.55 }));
  parts.push(SK("M-24 -14Q34 54 92 -14M84 -14Q136 62 188 -14M180 -14Q238 54 296 -14", { stroke: GOLD, sw: 2.2 }));
  for (const [x, y, dl] of [
    [88, 44, 0],
    [184, 44, -1.1],
  ]) {
    parts.push(SK(`M${x} ${y - 30}L${x} ${y}`, { stroke: GOLD, sw: 1.6, a: "swing", or: `${x}px ${y - 30}px`, dl }));
    parts.push(P(oval(x, y + 5, 5, 6) + poly([[x - 4, y + 9], [x + 4, y + 9], [x + 2.6, y + 22], [x - 2.6, y + 22]]), {
      fill: GOLD,
      a: "swing",
      or: `${x}px ${y - 30}px`,
      dl,
    }));
  }

  // Boards and footlights along the foot.
  parts.push(P(rect(-24, 390, 320, 28), { fill: "#4a3524" }));
  const boards = [];
  for (let x = -20; x < 300; x += 26) boards.push(rect(x, 390, 1.6, 28));
  parts.push(P(boards.join(""), { fill: "#33240f", op: 0.7 }));
  for (let i = 0; i < 9; i++) {
    const x = 8 + i * 32;
    parts.push(P(dot(x, 394, 15), { fill: "@foot", a: "glow", dl: -i * 0.45, op: 0.85 }));
    parts.push(P(poly([...arcPts(x, 394, 7, 180, 360, 8)]), { fill: "#ffe9b0" }));
    parts.push(P(rect(x - 8, 394, 16, 5), { fill: GOLD }));
  }

  return {
    id: "velvet-stage",
    name: "Velvet stage",
    group: "Stone & stage",
    // A proscenium: swagged valance over the brow, curtains drawn back down the
    // sides and footlights burning along the boards.
    grads: [
      glow("foot", [
        [0, "#ffdf9b", 0.75],
        [1, "#ffdf9b", 0],
      ]),
      drape("swag", VEL_D, VEL, VEL_L),
      drape("pelmet", "#3d0a13", VEL_D, VEL),
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
  const IRON = "#3b3f47";
  const IRON2 = "#22252b";
  const parts = [];

  // A riveted lintel over the brow, standing proud of the card, on two rails.
  parts.push(P(rect(-16, -40, 304, 56), { fill: "@lintel" }));
  parts.push(P(rect(-16, -46, 304, 8), { fill: "#4d525b" }));
  parts.push(P(rect(-16, 8, 304, 8), { fill: IRON2 }));
  parts.push(P(rect(0, 16, 18, 378) + rect(254, 16, 18, 378), { fill: "@post" }));
  parts.push(P(rect(0, 16, 5, 378) + rect(267, 16, 5, 378), { fill: IRON2, op: 0.7 }));
  // Corner brackets, where a real frame would carry the load.
  parts.push(
    P(
      poly([[18, 16], [58, 16], [18, 62]]) +
        poly([[254, 16], [214, 16], [254, 62]]) +
        poly([[18, 394], [50, 394], [18, 356]]) +
        poly([[254, 394], [222, 394], [254, 356]]),
      { fill: IRON },
    ),
  );
  const rivets = [];
  for (let i = 0; i < 19; i++) {
    const y = 26 + i * 19;
    rivets.push(dot(9, y, 2.6), dot(263, y, 2.6));
  }
  for (let i = 0; i < 16; i++) rivets.push(dot(-10 + i * 20, -30, 2.6), dot(-10 + i * 20, -2, 2.6));
  for (const [x, y] of [
    [30, 26],
    [44, 26],
    [30, 40],
    [242, 26],
    [228, 26],
    [242, 40],
    [30, 384],
    [242, 384],
  ])
    rivets.push(dot(x, y, 2.6));
  parts.push(P(rivets.join(""), { fill: "#5f6673" }));

  // Two braziers hanging off the lintel, chains and all.
  for (const [x, dl] of [
    [34, 0],
    [238, -0.7],
  ]) {
    parts.push(SK(`M${x} 12L${x} 44`, { stroke: "#5f6673", sw: 2.4, a: "swing", or: `${x}px 12px`, dl }));
    parts.push(P(dot(x, 44, 26), { fill: "@ember", a: "glow", dl, op: 0.85 }));
    parts.push(P(poly([[x - 15, 44], [x + 15, 44], [x + 10, 62], [x - 10, 62]]), { fill: IRON2, a: "swing", or: `${x}px 16px`, dl }));
    parts.push(
      P(poly([[x - 11, 44], [x - 5, 26], [x, 36], [x + 5, 22], [x + 11, 44]]), {
        fill: "#ff7a1e",
        a: "flick",
        or: `${x}px 44px`,
        dl,
      }),
    );
    parts.push(
      P(poly([[x - 6, 43], [x - 2, 32], [x + 1, 38], [x + 5, 30], [x + 7, 43]]), {
        fill: "#ffd66a",
        a: "flick",
        or: `${x}px 43px`,
        dl: dl - 0.3,
      }),
    );
  }

  // A bed of coals along the foot, breathing.
  parts.push(P(rect(-10, 392, 292, 26), { fill: IRON2 }));
  const g = rnd(11);
  const coals = [];
  for (let i = 0; i < 30; i++) coals.push(blob(-8 + i * 10, 398 + g() * 8, 5 + g() * 4, { squash: 0.7, seed: i }));
  parts.push(P(coals.join(""), { fill: "#7a2a12" }));
  for (let k = 0; k < 3; k++) {
    const hot = [];
    for (let i = 0; i < 10; i++) hot.push(blob(-4 + k * 8 + i * 28, 400 + g() * 6, 4 + g() * 3, { squash: 0.7, seed: 40 + i + k }));
    parts.push(P(hot.join(""), { fill: k ? "#ff8c2a" : "#ffd166", a: "coals", dl: -k * 1.3, op: 0.9 }));
  }
  parts.push(P(rect(-10, 384, 292, 22), { fill: "@coalglow", op: 0.7, a: "coals", dl: -0.6 }));

  // Sparks rising off the coals along the margins.
  for (const [x, dl, r] of [
    [22, 0, 2],
    [12, -2.4, 1.4],
    [252, -1.2, 1.8],
    [262, -3.8, 1.2],
    [32, -5.2, 1.6],
  ]) {
    parts.push(P(dot(x, 386, r), { fill: "#ffcf6a", a: "float-up", dl, op: 0.95 }));
  }

  return {
    id: "ember-forge",
    name: "Ember forge",
    group: "Stone & stage",
    // Riveted iron down the sides, braziers swinging off the lintel and a bed
    // of coals breathing along the foot.
    grads: [
      glow("ember", [
        [0, "#ff8c2a", 0.6],
        [1, "#ff8c2a", 0],
      ]),
      lin("coalglow", 0, 384, 0, 406, [
        [0, "#ff7a1e", 0],
        [1, "#ff7a1e", 0.85],
      ]),
      forged("post", IRON2, IRON, "#6d7382"),
      forged("lintel", "#191c21", IRON, "#5f6573"),
    ],
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
