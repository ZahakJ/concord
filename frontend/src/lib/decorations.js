// decorations.js — things you WEAR, as opposed to rings you sit inside.
//
// A ring (lib/rings.js) is built from gradients: a spinning disc, an orbiting
// dot, a halo, a weather layer. That vocabulary can only ever produce rings,
// which is why every option looked like a variation of the same option. A
// decoration is a drawn FIGURE — ears, horns, a crown, a jaw closing over your
// face — and it is what makes an avatar look like a character instead of a
// circle with a gradient behind it.
//
// Drawn, never downloaded. Nothing in Concord is fetched at runtime, so these
// are paths, not images. That constraint turns out to be the advantage: a path
// recolours to the wearer's own palette, stays sharp from a 20px member row to
// a 96px profile card, animates procedurally rather than as a baked loop, and
// costs a couple of hundred bytes.
//
// ── THE ARCH ────────────────────────────────────────────────────────────────
//
// Every decoration here is built on a BAND worn over the head, with everything
// else attached to that band. That is not a style choice, it is the fix for the
// two things wrong with the first library: nothing was centred convincingly and
// nothing moved.
//
// The centring was structural. Objects placed at absolute coordinates each
// invented their own idea of where the head was, so a crown floated, ears sat
// too high, and horns read as stuck ON the avatar rather than WORN by it. A
// band is a constant-radius arc around the avatar's own centre, and an ornament
// rooted on that band at an angle cannot be off-centre — the geometry will not
// let it. Everything below is therefore placed in POLAR terms: an angle in
// degrees clockwise from the top of the head, and a radius.
//
// Motion is the other half. A decoration that does not move is a sticker, so
// every entry here moves: the piece rocks on the head, or its ornaments swing
// from where they are attached, or light travels along it.
//
// ── THE AUTHORING CONTRACT ──────────────────────────────────────────────────
//
// Geometry. Authored in a `0 0 100 100` viewBox. The avatar is the circle at
// (50,50) with r=36, so radius 36 is the wearer's edge and radius 48 is about
// as far as anything can reach straight up before it leaves the box. Diagonals
// have far more room than the vertical does — an ornament that wants to be tall
// should lean out rather than climb.
//
// Layering. Each part declares `z`:
//   "back"  — behind the avatar (wings, a mantle, a halo's far side)
//   "front" — over the avatar (a band crossing the crown, teeth, a jaw)
// Parts render in array order within their layer. A "back" part is only visible
// OUTSIDE radius 36; one drawn entirely inside is invisible by construction,
// and that is the single most common way to author nothing at all.
//
// Colour. Never hardcode a colour that ought to be the wearer's. Use:
//   "c1" / "c2"  the wearer's two profile colours
//   "ink"        a near-black outline that reads on any background
//   "light"      a soft highlight
// or a literal hex ONLY where the object's identity IS its colour — gold on a
// crown, red on a devil horn, bone on an antler.
//
// Motion. `anim` names a class defined in AvatarDecoration.svelte. It may sit
// on the decoration (the default for its parts) or on a single part, which is
// how one piece rocks while the gems set into it pulse on their own clock:
//   twitch  a flick, at rest most of the time — ears
//   wag     a continuous swing from the root — ears, horns, leaves
//   dangle  a swing from the top — pendants, drops
//   float   slow vertical drift
//   flap    a wing beat
//   flicker opacity and scale jitter — flame
//   pulse   a breath in scale and opacity — gems, beads, stars
//   shimmer opacity alone — anything more light than object
//   drift   a spark rising and fading out
//   zap     hard electrical blink
//   wave    a swell passing through — water, cloth
//   chomp   a jaw closing (`o: "r"` gives the opposing jaw)
//   spin    slow rotation about the head's centre
//   whirl   the same, at orbit speed
//   sway    lazy side-to-side about a part's own middle
// Parts opt in with `a: true`. `o` places a part in the queue: "l"/"r" for the
// two halves of a pair, or 1..8 to offset it into a cycle already running, so a
// row moves as a wave. `pv: [x, y]` overrides the pivot with an explicit point,
// which is what keeps an ear and its inner shell swinging as one object rather
// than coming apart. `tilt: true` on the decoration rocks the whole piece on
// the head, and composes with whatever the parts are doing.
//
// Motion must degrade: prefers-reduced-motion stops all of it, and small
// avatars drop it too — forty animated decorations in a member list is forty
// animation timers for something 20 pixels tall.
//
// Ids are on the wire. They are validated as [a-z0-9-]{1,32} (validID,
// service.go) and looked up here; an unknown id renders NOTHING rather than
// failing, so a peer inventing one gets an ordinary avatar. Never make an id
// carry data, and never delete one: a peer who saved it would silently lose
// their decoration, since lookup fails closed.

const P = (d, o = {}) => ({ d, z: "front", fill: "ink", ...o });

// ── polar geometry ──────────────────────────────────────────────────────────
//
// Angles are degrees clockwise from the TOP of the head, so -60 is over the
// left brow and +60 over the right. Radius 36 is the avatar's own edge.

const RAD = Math.PI / 180;
// One decimal is a twentieth of a pixel on a 96px card and half that on a
// member row, and the sampled bands below are long runs of coordinates: the
// second decimal is pure payload.
const n = (v) => +v.toFixed(1);
const pt = (a, r) => [n(50 + r * Math.sin(a * RAD)), n(50 - r * Math.cos(a * RAD))];
const xy = (a, r) => pt(a, r).join(" ");

// frame: a local coordinate system anchored at a polar point, with +x running
// clockwise along the band and +y running straight out from the head. Every
// ornament below is drawn in these terms, which is why none of them needs to
// know where it is — the frame already leans it the right way.
function frame(a, r) {
  const s = Math.sin(a * RAD);
  const c = Math.cos(a * RAD);
  const [ox, oy] = pt(a, r);
  return (lx, ly) => `${n(ox + lx * c + ly * s)} ${n(oy + lx * s - ly * c)}`;
}

// poly: a closed shape given in the local frame of a point on the band.
const poly = (a, r, pts) => `M${pts.map(([x, y]) => frame(a, r)(x, y)).join("L")}Z`;

// arcBand: the ARCH itself — a band of thickness w centred on radius r,
// sweeping from a1 to a2. Everything else hangs off one of these.
function arcBand(r, a1, a2, w) {
  const ro = r + w / 2;
  const ri = r - w / 2;
  const [x1, y1] = pt(a1, ro);
  const [x2, y2] = pt(a2, ro);
  const [x3, y3] = pt(a2, ri);
  const [x4, y4] = pt(a1, ri);
  const big = Math.abs(a2 - a1) > 180 ? 1 : 0;
  return `M${x1} ${y1}A${ro} ${ro} 0 ${big} 1 ${x2} ${y2}L${x3} ${y3}A${ri} ${ri} 0 ${big} 0 ${x4} ${y4}Z`;
}

// taperBand: an arch whose thickness changes along its sweep — a comet tail, a
// cresting wave, a wisp. Give it a second radius and it spirals instead, which
// is the difference between a gust of wind and a broken ring. Sampled rather
// than arced, so it also handles sweeps past 180 degrees without the large-arc
// bookkeeping.
function taperBand(r, a1, a2, w1, w2, r2 = r, steps = 22) {
  const out = [];
  const inn = [];
  for (let i = 0; i <= steps; i++) {
    const t = i / steps;
    const a = a1 + (a2 - a1) * t;
    const w = w1 + (w2 - w1) * t;
    const rr = r + (r2 - r) * t;
    out.push(xy(a, rr + w / 2));
    inn.push(xy(a, rr - w / 2));
  }
  return `M${out.join("L")}L${inn.reverse().join("L")}Z`;
}

// ovalBand: a ring seen in perspective, for anything that encircles the head
// rather than crowning it. Split it into two calls — the far half behind the
// avatar, the near half in front — and it reads as a ring the head is inside.
function ovalBand(cy, rx, ry, t1, t2, w, rot = 0, steps = 24) {
  const cr = Math.cos(rot * RAD);
  const sr = Math.sin(rot * RAD);
  const at = (t, k) => {
    const x = (rx + k) * Math.cos(t * RAD);
    const y = (ry + k) * Math.sin(t * RAD);
    return `${n(50 + x * cr - y * sr)} ${n(cy + x * sr + y * cr)}`;
  };
  const out = [];
  const inn = [];
  for (let i = 0; i <= steps; i++) {
    const t = t1 + (t2 - t1) * (i / steps);
    out.push(at(t, w / 2));
    inn.push(at(t, -w / 2));
  }
  return `M${out.join("L")}L${inn.reverse().join("L")}Z`;
}

// annulus / ringAt: a full ring, as one path with two subpaths wound in
// opposite directions so the middle cancels out and stays hollow.
const donut = (x, y, r, w) => {
  const ro = r + w / 2;
  const ri = r - w / 2;
  return (
    `M${n(x - ro)} ${y}a${ro} ${ro} 0 1 0 ${n(ro * 2)} 0a${ro} ${ro} 0 1 0 ${n(-ro * 2)} 0Z` +
    `M${n(x - ri)} ${y}a${ri} ${ri} 0 1 1 ${n(ri * 2)} 0a${ri} ${ri} 0 1 1 ${n(-ri * 2)} 0Z`
  );
};
const annulus = (r, w) => donut(50, 50, r, w);
const ringAt = (a, r, R, w) => {
  const [x, y] = pt(a, r);
  return donut(x, y, R, w);
};

// spoke: a tapered shape rising outward from the band — an ear, a spike, a
// tooth. `spread` is its angular width at the base, `len` how far past the band
// it reaches (negative points it inward, at the face), `lean` tips it away from
// vertical so a pair can splay.
function spoke(a, rBase, len, spread, lean = 0) {
  const [bx1, by1] = pt(a - spread / 2, rBase);
  const [bx2, by2] = pt(a + spread / 2, rBase);
  const [tx, ty] = pt(a + lean, rBase + len);
  const [c1x, c1y] = pt(a - spread / 3, rBase + len * 0.6);
  const [c2x, c2y] = pt(a + spread / 3, rBase + len * 0.6);
  return `M${bx1} ${by1}Q${c1x} ${c1y} ${tx} ${ty}Q${c2x} ${c2y} ${bx2} ${by2}Z`;
}

// leaf: a petal, a feather, a tongue of flame — rooted on the band, pointed at
// the tip, and leaning by `lean` units sideways so a row of them can fan.
const leaf = (a, r, len, w, lean = 0) => {
  const f = frame(a, r);
  return `M${f(0, 0)}Q${f(w, len * 0.45)} ${f(lean, len)}Q${f(-w, len * 0.45)} ${f(0, 0)}Z`;
};

// ray: a straight tapered spike along the radius, from r0 out to r1.
const ray = (a, r0, r1, w0, w1 = 0) =>
  `M${frame(a, r0)(-w0 / 2, 0)}L${frame(a, r1)(-w1 / 2, 0)}L${frame(a, r1)(w1 / 2, 0)}L${frame(a, r0)(w0 / 2, 0)}Z`;

// blob: a round ornament — a bead, a berry, a bell, a pearl.
const blobXY = (x, y, s) =>
  `M${n(x)} ${n(y)}m${-s} 0a${s} ${s} 0 1 0 ${s * 2} 0a${s} ${s} 0 1 0 ${-s * 2} 0Z`;
const blob = (a, r, s) => blobXY(...pt(a, r), s);

// star / gem: the two shapes a jewel ever needs to be.
function star(a, r, R, ri, points = 5, rot = 0) {
  const p = [];
  for (let k = 0; k < points * 2; k++) {
    const th = (k * 180) / points + rot;
    const rr = k % 2 ? ri : R;
    p.push([rr * Math.sin(th * RAD), rr * Math.cos(th * RAD)]);
  }
  return poly(a, r, p);
}
const gem = (a, r, w, h) =>
  poly(a, r, [
    [0, h / 2],
    [w / 2, h * 0.14],
    [w * 0.3, -h / 2],
    [-w * 0.3, -h / 2],
    [-w / 2, h * 0.14],
  ]);

// flower: five petals and room for a centre, placed on the band.
function flower(a, r, R, petals = 5) {
  let d = "";
  const f = frame(a, r);
  for (let k = 0; k < petals; k++) {
    const th = (k * 360) / petals;
    const px = Math.cos(th * RAD) * R * 0.58;
    const py = Math.sin(th * RAD) * R * 0.58;
    const s = R * 0.5;
    const [cx, cy] = f(px, py).split(" ").map(Number);
    d += blobXY(cx, cy, s);
  }
  return d;
}

// horn: a tapered beam whose centreline is a circular arc, so it grows out of
// the band and then curves. `R` sets how tight the curve is and `sweep` how far
// round it goes; `dir` picks which way it bends.
function horn(a, rBase, R, sweep, w0, dir = 1, steps = 13) {
  const f = frame(a, rBase);
  const out = [];
  const inn = [];
  for (let i = 0; i <= steps; i++) {
    const t = i / steps;
    const th = t * sweep * RAD;
    const cx = dir * R * (1 - Math.cos(th));
    const cy = R * Math.sin(th);
    const nx = Math.cos(th);
    const ny = -dir * Math.sin(th);
    const w = (w0 * (1 - t) ** 1.1) / 2 + 0.2;
    out.push(f(cx + nx * w, cy + ny * w));
    inn.push(f(cx - nx * w, cy - ny * w));
  }
  return `M${out.join("L")}L${inn.reverse().join("L")}Z`;
}

// coil: a horn that keeps going — a spiral rooted on the band and winding in
// on itself beside the head. A ram's horn, a nautilus, a fiddlehead.
function coil(a, rBase, R, turns, w0, dir = 1, steps = 34) {
  const f = frame(a, rBase);
  const cx = dir * R;
  const phi0 = dir > 0 ? 180 : 0;
  const out = [];
  const inn = [];
  for (let i = 0; i <= steps; i++) {
    const t = i / steps;
    const phi = (phi0 - dir * turns * 360 * t) * RAD;
    const rad = R * (1 - 0.6 * t);
    const w = (w0 * (1 - 0.72 * t)) / 2;
    out.push(f(cx + (rad + w) * Math.cos(phi), (rad + w) * Math.sin(phi)));
    inn.push(f(cx + (rad - w) * Math.cos(phi), (rad - w) * Math.sin(phi)));
  }
  return `M${out.join("L")}L${inn.reverse().join("L")}Z`;
}

// bolt: lightning. Fixed silhouette, scaled into the local frame; a negative
// length turns it round to strike inward, at the face.
const BOLT = [
  [0, 1],
  [-0.5, 0.46],
  [-0.13, 0.46],
  [-0.38, 0],
  [0.5, 0.62],
  [0.13, 0.62],
  [0.5, 1],
];
const bolt = (a, r, len, w) => poly(a, r, BOLT.map(([x, y]) => [x * w, y * len]));

// chip: a rounded slab, for anything manufactured — an ear cup, a lens bezel,
// a solder pad. An octagon is enough at this size.
const chip = (a, r, w, h, k = 2) => {
  const x = w / 2;
  const y = h / 2;
  return poly(a, r, [
    [-x + k, -y],
    [x - k, -y],
    [x, -y + k],
    [x, y - k],
    [x - k, y],
    [-x + k, y],
    [-x, y - k],
    [-x, -y + k],
  ]);
};

// rubble: an irregular lump, deterministic from its seed so the same stone is
// the same stone on every screen.
function rubble(a, r, size, seed = 1) {
  const p = [];
  for (let k = 0; k < 7; k++) {
    const th = (k * 360) / 7 + seed * 17;
    const h = Math.sin(seed * 12.9898 + k * 78.233) * 43758.5453;
    const j = 0.68 + 0.55 * (h - Math.floor(h));
    p.push([size * j * Math.sin(th * RAD), size * j * Math.cos(th * RAD)]);
  }
  return poly(a, r, p);
}

// link: a straight strut between two polar points, for constellation lines and
// circuit traces.
function link(a1, r1, a2, r2, w) {
  const [x1, y1] = pt(a1, r1);
  const [x2, y2] = pt(a2, r2);
  const L = Math.hypot(x2 - x1, y2 - y1) || 1;
  const ox = ((y1 - y2) / L) * (w / 2);
  const oy = ((x2 - x1) / L) * (w / 2);
  return `M${n(x1 + ox)} ${n(y1 + oy)}L${n(x2 + ox)} ${n(y2 + oy)}L${n(x2 - ox)} ${n(y2 - oy)}L${n(x1 - ox)} ${n(y1 - oy)}Z`;
}

// drop: a pendant on a short stem, hanging straight down from a point on the
// band. Stem and bead are one path so the whole thing swings together.
const drop = (a, r, len, w, s) => {
  const [x, y] = pt(a, r);
  return (
    `M${n(x - w / 2)} ${y}L${n(x + w / 2)} ${y}L${n(x + w / 2)} ${n(y + len)}L${n(x - w / 2)} ${n(y + len)}Z` +
    blobXY(x, y + len + s * 0.6, s)
  );
};

// mirror: the same ornament on both sides of the head, opposed rather than in
// lockstep, which is what stops a pair reading as a machine. `make(angle, side)`
// gets -1 on the left and +1 on the right, so a lean written as `k * side`
// splays outward on both. A numeric `pv` means "pivot where this meets the
// band" and is resolved per side.
const mirror = (a, make, opts = {}) => {
  const one = (s) => ({
    ...opts,
    o: s < 0 ? "l" : "r",
    ...(typeof opts.pv === "number" ? { pv: pt(a * s, opts.pv) } : {}),
  });
  return [P(make(-a, -1), one(-1)), P(make(a, 1), one(1))];
};

// row: a run of ornaments along the band, each offset one step further into the
// animation cycle so the row moves as a wave.
const row = (angles, make, opts = {}) =>
  angles.map((a, i) => P(make(a, i), { ...opts, o: (i % 8) + 1 }));

// one: a run that does NOT need to move independently, merged into a single
// path with many subpaths. A staggered row has to stay separate — the stagger
// is the whole point — but a static row of twelve is twelve elements pretending
// to be one shape, on every avatar in a member list.
const one = (angles, make) => angles.map(make).join("");

export const DECORATIONS = [
  // ---------- arch ----------
  // The plainest statement of the idea: a band worn over the head, ornaments
  // hanging from it, the whole piece rocking as one. The band sits at a radius
  // that straddles the avatar's own edge, so it rests ON the head rather than
  // hovering above it — the difference between worn and stuck on.
  {
    id: "flower-circlet",
    name: "Flower circlet",
    group: "Arch",
    anim: "pulse",
    tilt: true,
    parts: [
      P(arcBand(37.6, -88, 88, 2.4), { fill: "c1" }),
      ...row([-78, -52, -26, 26, 52, 78], (a, i) => leaf(a, 37.6, 6.5, 2.4, i < 3 ? -2 : 2), {
        fill: "c2",
        anim: "wag",
        a: true,
      }),
      ...row([-62, -34, 0, 34, 62], (a, i) => flower(a, 39.6, i === 2 ? 6 : 5), {
        fill: "light",
        a: true,
      }),
      ...row([-62, -34, 0, 34, 62], (a, i) => blob(a, 39.6, i === 2 ? 2.2 : 1.8), {
        fill: "c2",
        a: true,
      }),
    ],
  },
  {
    id: "cat-circlet",
    name: "Cat circlet",
    group: "Arch",
    anim: "twitch",
    tilt: true,
    parts: [
      P(arcBand(37.4, -76, 76, 2.6), { fill: "c2" }),
      ...mirror(34, (a, s) => spoke(a, 37, 18, 26, 8 * s), { fill: "c1", a: true, pv: 37 }),
      ...mirror(34, (a, s) => spoke(a, 38, 11.5, 15, 8 * s), { fill: "light", a: true, pv: 37 }),
      P(blob(0, 38.8, 3), { fill: "#e6b23c", anim: "pulse", a: true }),
      P(blob(0, 39.7, 1), { fill: "#7a5a12" }),
    ],
  },
  {
    id: "antler-circlet",
    name: "Antler circlet",
    group: "Arch",
    anim: "pulse",
    tilt: true,
    parts: [
      P(arcBand(37.4, -80, 80, 2.4), { fill: "#7a5a3a" }),
      ...mirror(26, (a, s) => horn(a, 36.6, 40, 26, 6.2, s), { fill: "#c9a875" }),
      ...mirror(14, (a, s) => horn(a, 37.4, 26, 26, 3.4, s), { fill: "#b08e5c" }),
      ...mirror(40, (a, s) => horn(a, 37.2, 24, 40, 3.8, s), { fill: "#b08e5c" }),
      ...mirror(54, (a, s) => horn(a, 37.2, 18, 44, 3.2, s), { fill: "#9c7f52" }),
      ...row([-68, -4, 4, 68], (a) => blob(a, 37.4, 2.2), { fill: "c2", a: true }),
      ...row([-33, 33], (a) => blob(a, 37.4, 1.5), { fill: "light", a: true }),
    ],
  },
  {
    id: "star-circlet",
    name: "Star circlet",
    group: "Arch",
    anim: "pulse",
    tilt: true,
    parts: [
      P(arcBand(38.2, -86, 86, 1.6), { fill: "light" }),
      ...row([-74, -50, -26, 26, 50, 74], (a, i) =>
        star(a, 38.2 + (i % 2 ? 5.4 : 3.4), i % 2 ? 4.4 : 3.2, i % 2 ? 1.8 : 1.3, 5), {
        fill: "c2",
        a: true,
      }),
      P(star(0, 42.6, 5.2, 2.1, 5), { fill: "c1", a: true }),
      P(star(0, 42.6, 2.6, 1, 5), { fill: "light", a: true, o: 4 }),
    ],
  },
  // ---------- creature ----------
  {
    id: "cat-ears",
    name: "Cat ears",
    group: "Creature",
    anim: "twitch",
    tilt: true,
    parts: [
      P(arcBand(37.2, -80, 80, 4.6), { fill: "c1" }),
      P(arcBand(39, -80, 80, 1), { fill: "light" }),
      ...mirror(37, (a, s) => spoke(a, 36.8, 19, 30, 9 * s), { fill: "c1", a: true, pv: 37 }),
      ...mirror(37, (a, s) => spoke(a, 37.8, 12, 18, 9 * s), { fill: "c2", a: true, pv: 37 }),
      ...row([-58, -20, 20, 58], (a, i) => leaf(a, 39.2, 4.6, 1.7, i < 2 ? -1.6 : 1.6), {
        fill: "light",
        anim: "wag",
        a: true,
      }),
    ],
  },
  {
    id: "fox-ears",
    name: "Fox ears",
    group: "Creature",
    anim: "twitch",
    parts: [
      P(coil(122, 37, 11, 0.62, 10.5, 1), {
        z: "back",
        fill: "#d97a34",
        anim: "sway",
        a: true,
      }),
      P(blobXY(71.3, 80.1, 2.8), { z: "back", fill: "light", anim: "sway", a: true }),
      P(arcBand(37.4, -74, 74, 2.8), { fill: "#d97a34" }),
      ...mirror(35, (a, s) => spoke(a, 37, 20, 24, 9 * s), {
        fill: "#d97a34",
        a: true,
        pv: 37,
      }),
      ...mirror(35, (a, s) => spoke(a, 38, 13, 13, 9 * s), { fill: "light", a: true, pv: 37 }),
      ...mirror(43, (a, s) => spoke(a, 50.5, 6.5, 8, 3 * s), { fill: "ink", a: true, pv: 37 }),
      ...row([-58, 58], (a) => blob(a, 37.4, 1.6), { fill: "light", anim: "pulse", a: true }),
    ],
  },
  {
    id: "bunny-ears",
    name: "Bunny ears",
    group: "Creature",
    anim: "wag",
    parts: [
      P(arcBand(37.2, -72, 72, 3), { fill: "c1" }),
      ...mirror(18, (a, s) => leaf(a, 34.5, 17.5, 4.2, 6 * s), { fill: "c1", a: true, pv: 36 }),
      ...mirror(18, (a, s) => leaf(a, 35.6, 13.5, 2.3, 5 * s), { fill: "c2", a: true, pv: 36 }),
      P(poly(-46, 39, [[-0.9, 0], [-4.6, 2.6], [-4.6, -2.6]]), {
        fill: "c2",
        anim: "pulse",
        a: true,
      }),
      P(poly(-46, 39, [[0.9, 0], [4.6, 2.6], [4.6, -2.6]]), {
        fill: "c2",
        anim: "pulse",
        a: true,
        o: 2,
      }),
      P(blob(-46, 39, 1.5), { fill: "light", anim: "pulse", a: true, o: 1 }),
      ...row([-52, 52], (a) => blob(a, 37.2, 1.5), { fill: "light", anim: "pulse", a: true }),
    ],
  },
  {
    id: "ram-horns",
    name: "Ram horns",
    group: "Creature",
    anim: "pulse",
    tilt: true,
    parts: [
      P(arcBand(37.2, -86, 86, 3.6), { fill: "#5c4a33" }),
      P(arcBand(38.8, -86, 86, 0.9), { fill: "#8a7050" }),
      ...mirror(56, (a, s) => coil(a, 36.5, 9, 1.1, 8, s), { fill: "#cbb18a" }),
      ...mirror(56, (a, s) => coil(a, 36.9, 8.6, 1, 2.6, s), { fill: "#9c8158" }),
      ...row([-30, 0, 30], (a, i) => gem(a, 39, i === 1 ? 5 : 3.6, i === 1 ? 7 : 5), {
        fill: "c2",
        a: true,
      }),
    ],
  },
  {
    id: "devil-horns",
    name: "Devil horns",
    group: "Creature",
    anim: "flicker",
    tilt: true,
    parts: [
      P(arcBand(37.2, -74, 74, 3.4), { fill: "#3a1214" }),
      ...mirror(32, (a, s) => horn(a, 36.4, 24, 54, 9.6, s), { fill: "#c1362f" }),
      ...mirror(32, (a, s) => horn(a, 37.6, 23, 52, 3.8, s), { fill: "#e8674f" }),
      ...row([-58, -44, 44, 58], (a) => blob(a, 37.2, 2), { fill: "#ff8a3d", a: true }),
      P(gem(0, 39, 4.6, 6.2), { fill: "#ff8a3d", anim: "pulse", a: true }),
    ],
  },
  {
    id: "shark",
    name: "Shark bite",
    group: "Creature",
    anim: "chomp",
    parts: [
      P(arcBand(42, 112, 248, 9), { z: "back", fill: "#3f6379", a: true }),
      P(arcBand(38.2, 112, 248, 3.4), { fill: "#e8f0f6", a: true }),
      P(one([124, 138, 152, 166, 180, 194, 208, 222, 236], (a) => spoke(a, 37.2, -6.5, 8)), {
        fill: "#ffffff",
        a: true,
      }),
      P(arcBand(42, -68, 68, 8), { z: "back", fill: "#4d7995", a: true, o: "r" }),
      P(arcBand(38.2, -68, 68, 3.2), { fill: "#e8f0f6", a: true, o: "r" }),
      P(one([-60, -45, -30, -15, 0, 15, 30, 45, 60], (a) => spoke(a, 37.2, -6, 8)), {
        fill: "#ffffff",
        a: true,
        o: "r",
      }),
    ],
  },
  // ---------- regalia ----------
  {
    id: "royal-crown",
    name: "Royal crown",
    group: "Regalia",
    anim: "pulse",
    tilt: true,
    parts: [
      P(arcBand(37.2, -84, 84, 5.4), { fill: "#e6b23c" }),
      P(arcBand(39.3, -84, 84, 1.1), { fill: "#ffe6a6" }),
      P(arcBand(35, -84, 84, 1.1), { fill: "#a9761b" }),
      P(one([-70, -46, -22, 0, 22, 46, 70], (a, i) => spoke(a, 39.6, [5, 7.5, 8.5, 6, 8.5, 7.5, 5][i], 16)), {
        fill: "#e6b23c",
      }),
      P(one([-70, -46, -22, 0, 22, 46, 70], (a, i) => spoke(a, 39.6, [3.4, 5.4, 6.2, 4, 6.2, 5.4, 3.4][i], 7)), {
        fill: "#ffe6a6",
      }),
      ...row(
        [-70, -46, -22, 0, 22, 46, 70],
        (a, i) => blob(a, 39.6 + [5, 7.5, 8.5, 6, 8.5, 7.5, 5][i], 2.1),
        { fill: "c1", a: true },
      ),
      ...row([-58, -34, -11, 11, 34, 58], (a) => blob(a, 37.2, 1.6), { fill: "light", a: true }),
      P(gem(0, 36.6, 5.4, 6.6), { fill: "c2", anim: "shimmer", a: true }),
    ],
  },
  {
    id: "laurel-wreath",
    name: "Laurel wreath",
    group: "Regalia",
    anim: "wag",
    parts: [
      P(arcBand(37.4, -152, -26, 1.6), { fill: "#5b7f3a" }),
      P(arcBand(37.4, 26, 152, 1.6), { fill: "#5b7f3a" }),
      ...row([-140, -124, -108, -92, -76, -60, -44], (a) => leaf(a, 37.6, 9, 4.6, 5), {
        fill: "c1",
        a: true,
      }),
      ...row([140, 124, 108, 92, 76, 60, 44], (a) => leaf(a, 37.6, 9, 4.6, -5), {
        fill: "c1",
        a: true,
      }),
      ...row([-132, -116, -100, -84, -68, -52], (a) => leaf(a, 36.4, 6.5, 3.4, -4), {
        fill: "c2",
        a: true,
      }),
      ...row([132, 116, 100, 84, 68, 52], (a) => leaf(a, 36.4, 6.5, 3.4, 4), {
        fill: "c2",
        a: true,
      }),
      ...row([-150, -34, 34, 150], (a) => blob(a, 38.4, 1.9), {
        fill: "light",
        anim: "pulse",
        a: true,
      }),
      P(gem(180, 38.6, 5, 7), { fill: "c2", anim: "pulse", a: true }),
    ],
  },
  {
    id: "tiara",
    name: "Tiara",
    group: "Regalia",
    anim: "dangle",
    tilt: true,
    parts: [
      P(arcBand(37.1, -76, 76, 2.2), { fill: "#dfe6ef" }),
      P(arcBand(38.6, -76, 76, 0.8), { fill: "light" }),
      P(ringAt(0, 42.4, 4.8, 1.5), { fill: "#dfe6ef" }),
      ...mirror(28, (a) => ringAt(a, 41.4, 3.6, 1.3), { fill: "#dfe6ef" }),
      ...mirror(52, (a) => ringAt(a, 40.4, 2.6, 1.1), { fill: "#dfe6ef" }),
      P(gem(0, 42.4, 4.4, 6), { fill: "c1", anim: "pulse", a: true }),
      ...mirror(28, (a) => blob(a, 41.4, 2), { fill: "c2", anim: "pulse", a: true }),
      ...mirror(52, (a) => blob(a, 40.4, 1.4), { fill: "c2", anim: "pulse", a: true }),
      ...row([-66, -42, 42, 66], (a, i) => drop(a, 36.4, 3.5 + (i % 2) * 2.4, 0.9, 1.8), {
        fill: "light",
        a: true,
      }),
    ],
  },
  {
    id: "diadem",
    name: "Jewelled diadem",
    group: "Regalia",
    anim: "shimmer",
    tilt: true,
    parts: [
      P(arcBand(37.3, -90, 90, 3.4), { fill: "c1" }),
      P(arcBand(38.8, -90, 90, 0.9), { fill: "light", a: true }),
      P(gem(0, 42.4, 8, 10.4), { fill: "c2", anim: "pulse", a: true }),
      P(gem(0, 42.4, 3.6, 5), { fill: "light", anim: "pulse", a: true, o: 2 }),
      ...row([-58, -30, 30, 58], (a, i) => gem(a, 40.4, 4.6 - (i % 2), 6.6 - (i % 2)), {
        fill: "c2",
        anim: "pulse",
        a: true,
      }),
      ...row([-76, -44, -14, 14, 44, 76], (a) => blob(a, 37.3, 1.4), { fill: "light", a: true }),
    ],
  },
  {
    id: "royal-collar",
    name: "Royal collar",
    group: "Regalia",
    anim: "wave",
    parts: [
      P(arcBand(41.4, 116, 244, 9.6), { z: "back", fill: "c1", a: true }),
      P(arcBand(45.4, 116, 244, 1.6), { z: "back", fill: "light", a: true }),
      P(one([122, 136, 150, 164, 178, 192, 206, 220, 234], (a) => blob(a, 45.4, 2)), {
        z: "back",
        fill: "light",
        a: true,
      }),
      ...mirror(118, (a, s) => spoke(a, 41.4, 8, 13, -5 * s), { z: "back", fill: "c1", a: true }),
      P(arcBand(37.4, 116, 244, 1.4), { z: "back", fill: "c2", a: true }),
      P(gem(180, 40, 5.6, 7.6), { fill: "c2", anim: "pulse", a: true }),
      P(blob(180, 40, 1.6), { fill: "light", anim: "pulse", a: true, o: 2 }),
    ],
  },
  {
    id: "velvet-mantle",
    name: "Velvet mantle",
    group: "Regalia",
    anim: "sway",
    parts: [
      P(taperBand(40.4, 100, 180, 6, 11), { z: "back", fill: "#7d1439", a: true }),
      P(taperBand(40.4, 260, 180, 6, 11), { z: "back", fill: "#7d1439", a: true, o: "r" }),
      P(taperBand(37.6, 108, 180, 1.4, 2.4), { z: "back", fill: "#4c0a22", a: true }),
      P(taperBand(37.6, 252, 180, 1.4, 2.4), { z: "back", fill: "#4c0a22", a: true, o: "r" }),
      P(arcBand(44.4, 104, 256, 2.4), { z: "back", fill: "#f4efe2", a: true }),
      P(one([120, 144, 168, 192, 216, 240], (a) => blob(a, 44.4, 1.2)), {
        z: "back",
        fill: "#33373f",
        a: true,
      }),
      ...mirror(102, (a, s) => spoke(a, 40.4, 9, 14, -6 * s), {
        z: "back",
        fill: "#7d1439",
        a: true,
      }),
      P(gem(180, 39.4, 5.4, 7), { fill: "#e6b23c", anim: "pulse", a: true }),
    ],
  },
  // ---------- ethereal ----------
  {
    id: "halo-gold",
    name: "Halo",
    group: "Ethereal",
    anim: "float",
    parts: [
      P(ovalBand(13.5, 23, 6.2, 0, 360, 4.4), { fill: "#b7761c", a: true }),
      P(ovalBand(13.1, 23, 6.2, 0, 360, 2.4), { fill: "#ffd24a", a: true }),
      P(ovalBand(12.4, 22.4, 5.8, 190, 350, 1), { fill: "#fff3bd", a: true }),
      ...row([-62, -38, 38, 62], (a, i) => star(a, 42 + (i % 2) * 3, 3.2, 1.2, 4), {
        fill: "#ffe9a8",
        anim: "pulse",
        a: true,
      }),
    ],
  },
  {
    id: "orbit-ring",
    name: "Orbit ring",
    group: "Ethereal",
    anim: "pulse",
    parts: [
      P(ovalBand(53, 42.6, 14.5, 180, 360, 4.6), { z: "back", fill: "c2" }),
      P(ovalBand(53, 42.6, 14.5, 0, 180, 4.6), { fill: "c1" }),
      P(ovalBand(53, 42.6, 14.5, 14, 166, 1.4), { fill: "light" }),
      P(ovalBand(53, 42.6, 14.5, 194, 346, 1.4), { z: "back", fill: "light" }),
      P(blobXY(7.4, 53, 4), { z: "back", fill: "c2", a: true }),
      P(blobXY(92.6, 53, 4), { fill: "c1", a: true, o: 4 }),
      P(blobXY(91.8, 51.6, 1.4), { fill: "light", a: true, o: 4 }),
    ],
  },
  {
    id: "aura-crown",
    name: "Aura crown",
    group: "Ethereal",
    anim: "flicker",
    parts: [
      P(arcBand(36.8, -98, 98, 1.8), { fill: "c1", a: true }),
      ...row(
        [-90, -74, -58, -42, -26, -10, 10, 26, 42, 58, 74, 90],
        (a, i) => leaf(a, 36.8, [5, 8, 12, 15, 11, 8, 8, 11, 15, 12, 8, 5][i], 3, 0),
        { fill: "c1", a: true },
      ),
      ...row(
        [-74, -42, -26, 26, 42, 74],
        (a, i) => leaf(a, 36.8, [4, 9, 6, 6, 9, 4][i], 1.8, 0),
        { fill: "light", a: true },
      ),
      ...row([-58, 0, 58], (a) => blob(a, 45, 1.7), { fill: "light", anim: "drift", a: true }),
    ],
  },
  {
    id: "spectral-shroud",
    name: "Spectral shroud",
    group: "Ethereal",
    anim: "shimmer",
    parts: [
      P(taperBand(39.6, 0, -152, 5, 13, 44.6), { z: "back", fill: "c1", a: true }),
      P(taperBand(39.6, 0, 152, 5, 13, 44.6), { z: "back", fill: "c1", a: true, o: "r" }),
      P(taperBand(37.2, 0, -152, 1.2, 2.6, 38.6), { z: "back", fill: "light", a: true }),
      P(taperBand(37.2, 0, 152, 1.2, 2.6, 38.6), { z: "back", fill: "light", a: true, o: "r" }),
      ...mirror(150, (a, s) => leaf(a, 44.6, 9.5, 5, 4 * s), {
        z: "back",
        fill: "c1",
        anim: "dangle",
        a: true,
      }),
      ...mirror(134, (a, s) => leaf(a, 44.4, 8, 4, 3 * s), {
        z: "back",
        fill: "c1",
        anim: "dangle",
        a: true,
        o: 3,
      }),
      ...mirror(118, (a, s) => leaf(a, 42.8, 5, 3, 2 * s), {
        z: "back",
        fill: "c1",
        anim: "dangle",
        a: true,
        o: 5,
      }),
      ...row([-104, -86, 86, 104], (a) => blob(a, 47, 1.8), {
        z: "back",
        fill: "light",
        anim: "drift",
        a: true,
      }),
    ],
  },
  {
    id: "radiant-nimbus",
    name: "Radiant nimbus",
    group: "Ethereal",
    anim: "shimmer",
    parts: [
      ...Array.from({ length: 16 }, (_, i) =>
        P(ray(i * 22.5, 38.5, i % 2 ? 46.8 : 43, 3.2, 0.6), {
          z: "back",
          fill: "light",
          anim: "whirl",
          a: true,
        }),
      ),
      P(annulus(39.8, 2.2), { z: "back", fill: "c1", a: true }),
      P(annulus(43.6, 0.9), { z: "back", fill: "light", a: true, o: 3 }),
      ...row([-40, 0, 40], (a) => star(a, 45.4, 3, 1.1, 4), {
        z: "back",
        fill: "light",
        anim: "pulse",
        a: true,
      }),
    ],
  },
  // ---------- elemental ----------
  {
    id: "flame-crown",
    name: "Flame crown",
    group: "Elemental",
    anim: "flicker",
    parts: [
      P(arcBand(36.8, -94, 94, 2.6), { fill: "#7a2a12" }),
      ...row(
        [-84, -66, -48, -30, -12, 12, 30, 48, 66, 84],
        (a, i) => leaf(a, 36.8, [6, 9, 13, 16, 11, 11, 16, 13, 9, 6][i], 4, 0),
        { fill: "#e8431f", a: true },
      ),
      ...row(
        [-84, -66, -48, -30, -12, 12, 30, 48, 66, 84],
        (a, i) => leaf(a, 36.8, [4, 6, 9, 11.5, 7.5, 7.5, 11.5, 9, 6, 4][i], 2.6, 0),
        { fill: "#ff922b", a: true },
      ),
      ...row(
        [-48, -30, -12, 12, 30, 48],
        (a, i) => leaf(a, 36.8, [4.5, 6, 4, 4, 6, 4.5][i], 1.4, 0),
        { fill: "#ffe066", a: true },
      ),
      ...row([-70, -20, 20, 70], (a) => blob(a, 48, 1.6), {
        fill: "#ff922b",
        anim: "drift",
        a: true,
      }),
    ],
  },
  {
    id: "frost-crown",
    name: "Frost crown",
    group: "Elemental",
    anim: "shimmer",
    parts: [
      P(arcBand(36.8, -90, 90, 2.4), { fill: "#3d7ba8" }),
      P(
        one([-76, -58, -40, -22, 0, 22, 40, 58, 76], (a, i) =>
          ray(a, 36.8, 36.8 + [6, 9, 14, 11, 8.5, 11, 14, 9, 6][i], 5.4, 0),
        ),
        { fill: "#3d7ba8" },
      ),
      ...row(
        [-76, -58, -40, -22, 0, 22, 40, 58, 76],
        (a, i) => ray(a, 36.8, 36.8 + [4.8, 7.4, 12, 9, 7, 9, 12, 7.4, 4.8][i], 3.4, 0),
        { fill: "#b9e7ff", a: true },
      ),
      ...row(
        [-40, 0, 40],
        (a, i) => ray(a, 36.8, 36.8 + [8, 4.5, 8][i], 1.4, 0),
        { fill: "#f0fbff", a: true },
      ),
      ...mirror(49, (a, s) => ray(a + 4 * s, 40, 45.5, 2.6, 0), { fill: "#b9e7ff", a: true }),
      ...row([-66, -12, 12, 66], (a) => star(a, 44, 2.6, 1, 6), {
        fill: "#f0fbff",
        anim: "pulse",
        a: true,
      }),
    ],
  },
  {
    id: "storm-bolts",
    name: "Storm bolts",
    group: "Elemental",
    anim: "zap",
    parts: [
      P(arcBand(40, -104, 104, 6), { z: "back", fill: "#414c5e" }),
      P(
        one([-98, -80, -62, -44, -26, -8, 8, 26, 44, 62, 80, 98], (a, i) =>
          blob(a, 42.8, 3.6 - (i % 3) * 0.7),
        ),
        { z: "back", fill: "#54607a" },
      ),
      P(arcBand(37.6, -104, 104, 1.6), { z: "back", fill: "#242a36" }),
      ...row([-62, -21, 21, 62], (a, i) => bolt(a, 37, i % 3 ? -19 : -14, i % 3 ? 9 : 7), {
        fill: "#ffe066",
        a: true,
      }),
      ...row([-62, -21, 21, 62], (a, i) => bolt(a, 37, i % 3 ? -12 : -9, i % 3 ? 4.6 : 3.6), {
        fill: "#fff6c2",
        a: true,
      }),
    ],
  },
  {
    id: "tide-crest",
    name: "Tide crest",
    group: "Elemental",
    anim: "wave",
    parts: [
      P(taperBand(41, 208, 378, 11, 2.4), { z: "back", fill: "c1", a: true }),
      P(taperBand(43.6, 230, 372, 2.2, 1), { z: "back", fill: "light", a: true }),
      P(coil(22, 39, 5.4, 0.9, 5.4, 1), { fill: "c1", a: true }),
      P(coil(22, 39.5, 5, 0.8, 2, 1), { fill: "light", a: true, o: 2 }),
      ...row([250, 285, 320, 350], (a) => blob(a, 45.6, 2.2), {
        z: "back",
        fill: "light",
        a: true,
      }),
      ...row([196, 214, 232], (a, i) => blob(a, 43 + i * 2, 1.6), {
        z: "back",
        fill: "c2",
        anim: "drift",
        a: true,
      }),
      P(gem(180, 39.4, 4.4, 6), { z: "back", fill: "c2", anim: "pulse", a: true }),
    ],
  },
  {
    id: "stone-mantle",
    name: "Stone mantle",
    group: "Elemental",
    anim: "float",
    parts: [
      ...row(
        [-148, -118, -88, -58, 58, 88, 118, 148],
        (a, i) => rubble(a, 42.4, 8 - (i % 2) * 1.6, i + 1),
        { z: "back", fill: "#1d2126", a: true },
      ),
      ...row(
        [-148, -118, -88, -58, 58, 88, 118, 148],
        (a, i) => rubble(a, 42.8, 6.8 - (i % 2) * 1.4, i + 1),
        { z: "back", fill: "#576069", a: true },
      ),
      ...row(
        [-148, -118, -88, -58, 58, 88, 118, 148],
        (a, i) => rubble(a, 44.6, 3.2 - (i % 2) * 0.8, i + 1),
        { z: "back", fill: "#98a2ae", a: true },
      ),
      ...row([-170, 170, 180], (a, i) => rubble(a, 39.5 + i, 2.4 - i * 0.4, i + 4), {
        z: "back",
        fill: "#576069",
        anim: "drift",
        a: true,
      }),
      ...row([-32, 32], (a) => blob(a, 38.6, 2.4), { fill: "c2", anim: "pulse", a: true }),
    ],
  },
  {
    id: "ember-rise",
    name: "Ember rise",
    group: "Elemental",
    anim: "drift",
    parts: [
      P(arcBand(39.4, 96, 264, 5.6), { z: "back", fill: "#5e2a12" }),
      P(arcBand(37.2, 96, 264, 1.4), { z: "back", fill: "#8c4a1e" }),
      ...row(
        [104, 122, 140, 158, 180, 202, 220, 238, 256],
        (a, i) => blob(a, 40.4, 2.6 - (i % 3) * 0.5),
        { z: "back", fill: "#e8431f", anim: "flicker", a: true },
      ),
      ...row(
        [112, 132, 152, 172, 190, 210, 230, 250],
        (a, i) => blob(a, 40.4, 1.4 - (i % 3) * 0.3),
        { z: "back", fill: "#ffe066", anim: "flicker", a: true },
      ),
      ...row(
        [104, 120, 136, 224, 240, 256],
        (a, i) => blob(a, 44.5 + (i % 3) * 2, 1.8 - (i % 3) * 0.4),
        { z: "back", fill: "#ff922b", a: true },
      ),
      ...row([112, 134, 226, 248], (a, i) => blob(a, 48 + (i % 2) * 2.5, 1.3), {
        z: "back",
        fill: "#ffe066",
        a: true,
      }),
    ],
  },
  {
    id: "gale-swirl",
    name: "Gale swirl",
    group: "Elemental",
    anim: "whirl",
    parts: [
      P(taperBand(46.4, -50, 170, 5, 0.6, 37.4), { z: "back", fill: "c1", a: true }),
      P(taperBand(38.4, 150, 380, 4.4, 0.5, 46.8), { z: "back", fill: "c1", a: true }),
      P(taperBand(43.4, 70, 260, 2.6, 0.4, 37), { z: "back", fill: "light", a: true, o: "r" }),
      P(taperBand(37.2, 250, 430, 2.4, 0.4, 44.4), { z: "back", fill: "light", a: true, o: "r" }),
      ...row([-100, 20, 140, 260], (a) => leaf(a, 45, 5, 2, 2), {
        z: "back",
        fill: "c2",
        anim: "drift",
        a: true,
      }),
    ],
  },
  // ---------- cosmic ----------
  {
    id: "ringed-world",
    name: "Ringed world",
    group: "Cosmic",
    anim: "pulse",
    parts: [
      P(ovalBand(51, 43, 13, 178, 362, 6.4, -14), { z: "back", fill: "c1" }),
      P(ovalBand(51, 43, 13, 178, 362, 2, -14), { z: "back", fill: "light" }),
      P(ovalBand(51, 43, 13, -2, 182, 6.4, -14), { fill: "c1" }),
      P(ovalBand(51, 43, 13, 6, 174, 1.8, -14), { fill: "light" }),
      P(blob(64, 44, 5.6), { fill: "c2", a: true }),
      P(blob(61, 45.4, 1.9), { fill: "light", a: true, o: 2 }),
      ...row([-46, -20], (a) => star(a, 44, 2.8, 1, 4), { z: "back", fill: "light", a: true }),
    ],
  },
  {
    id: "comet",
    name: "Comet",
    group: "Cosmic",
    anim: "whirl",
    parts: [
      P(taperBand(43, -130, 14, 0.8, 7.6), { z: "back", fill: "c1", a: true }),
      P(taperBand(43, -92, 12, 0.6, 4), { z: "back", fill: "light", a: true }),
      P(blob(18, 42, 5.2), { fill: "c2", a: true }),
      P(star(18, 42, 8.6, 2, 4, 0), { fill: "light", a: true }),
      P(blob(17, 43.4, 1.7), { fill: "light", a: true }),
      ...row([-150, -112], (a, i) => star(a, 43 + i * 2, 2.6, 0.9, 4), {
        z: "back",
        fill: "light",
        a: true,
      }),
    ],
  },
  {
    id: "constellation",
    name: "Constellation",
    group: "Cosmic",
    anim: "pulse",
    parts: [
      P(
        [
          link(-80, 39, -58, 43.5, 0.9),
          link(-58, 43.5, -34, 38.5, 0.9),
          link(-34, 38.5, -8, 44.5, 0.9),
          link(-8, 44.5, 18, 39, 0.9),
          link(18, 39, 44, 43.5, 0.9),
          link(44, 43.5, 70, 38.5, 0.9),
          link(-8, 44.5, 12, 47.5, 0.9),
        ].join(""),
        { fill: "c1", anim: "shimmer", a: true },
      ),
      ...row(
        [-80, -58, -34, -8, 18, 44, 70],
        (a, i) => star(a, [39, 43.5, 38.5, 44.5, 39, 43.5, 38.5][i], i === 3 ? 5.4 : 3.8, 1.4, 4),
        { fill: "light", a: true },
      ),
      P(star(12, 47.5, 3.2, 1.1, 4), { fill: "c2", anim: "pulse", a: true, o: 5 }),
      ...row([-94, -20, 32, 88], (a, i) => blob(a, 42 + (i % 2) * 3, 1.2), {
        fill: "c2",
        a: true,
      }),
    ],
  },
  {
    id: "orbits",
    name: "Orbits",
    group: "Cosmic",
    anim: "shimmer",
    parts: [
      P(ovalBand(50, 43, 15, 180, 360, 2.4, 26), { z: "back", fill: "c1", a: true }),
      P(ovalBand(50, 43, 15, 0, 180, 2.4, 26), { fill: "c1", a: true }),
      P(ovalBand(50, 43, 15, 180, 360, 2.4, -26), { z: "back", fill: "c2", a: true, o: 3 }),
      P(ovalBand(50, 43, 15, 0, 180, 2.4, -26), { fill: "c2", a: true, o: 3 }),
      P(blob(72, 44, 3.6), { fill: "c1", anim: "whirl", a: true }),
      P(blob(-108, 44, 3), { fill: "c2", anim: "whirl", a: true, o: "r" }),
      P(blob(71, 45.2, 1.2), { fill: "light", anim: "whirl", a: true }),
    ],
  },
  {
    id: "solar-corona",
    name: "Solar corona",
    group: "Cosmic",
    anim: "flicker",
    parts: [
      ...row(
        Array.from({ length: 18 }, (_, i) => i * 20),
        (a, i) => leaf(a, 38, [8, 5, 6.5][i % 3], 4.5, [2, -2, 0][i % 3]),
        { z: "back", fill: "c1", a: true },
      ),
      ...row(
        Array.from({ length: 9 }, (_, i) => i * 40 + 10),
        (a, i) => leaf(a, 38, [5, 3.5][i % 2], 2.6, 0),
        { z: "back", fill: "c2", a: true },
      ),
      P(annulus(38.4, 2.6), { z: "back", fill: "c1" }),
      P(annulus(39.6, 0.8), { z: "back", fill: "light", anim: "shimmer", a: true }),
      ...row([-40, 30], (a, i) => leaf(a, 40, 7 + i, 3, 3), {
        z: "back",
        fill: "light",
        anim: "drift",
        a: true,
      }),
    ],
  },
  // ---------- tech ----------
  {
    id: "headphones",
    name: "Headphones",
    group: "Tech",
    anim: "pulse",
    parts: [
      P(arcBand(41.6, -96, 96, 5.6), { fill: "ink" }),
      P(arcBand(41.6, -96, 96, 3), { fill: "c1" }),
      P(arcBand(43.2, -62, 62, 0.9), { fill: "light" }),
      ...mirror(96, (a) => chip(a, 39.6, 10, 16, 3.2), { fill: "ink" }),
      ...mirror(96, (a) => chip(a, 39.6, 7.4, 13, 2.6), { fill: "c1" }),
      ...mirror(96, (a) => chip(a, 39.6, 4.6, 9, 1.8), { fill: "c2", anim: "shimmer", a: true }),
      P(taperBand(36.4, 102, 146, 2.6, 2), { fill: "ink" }),
      P(taperBand(36.4, 104, 144, 1, 0.8), { fill: "c1" }),
      P(blob(148, 36.4, 3), { fill: "ink" }),
      P(blob(148, 36.4, 1.7), { fill: "c2", anim: "pulse", a: true }),
    ],
  },
  {
    id: "antennae",
    name: "Antennae",
    group: "Tech",
    anim: "twitch",
    parts: [
      P(arcBand(37.2, -64, 64, 3.2), { fill: "c1" }),
      P(arcBand(38.8, -64, 64, 0.9), { fill: "light" }),
      ...mirror(27, (a, s) => ray(a, 36.8, 48, 2.6, 1.6) + blob(a + 1.6 * s, 50.4, 4), {
        fill: "c1",
        a: true,
        pv: 37,
      }),
      ...mirror(28.6, (a) => blob(a, 50.4, 2.2), { fill: "c2", anim: "pulse", a: true }),
      ...row([-48, 0, 48], (a) => blob(a, 37.2, 1.5), { fill: "c2", anim: "shimmer", a: true }),
    ],
  },
  {
    id: "circuit-traces",
    name: "Circuit traces",
    group: "Tech",
    anim: "shimmer",
    parts: [
      P(arcBand(38.6, -108, 108, 3.6), { fill: "#1c2733" }),
      P(arcBand(38.6, -108, 108, 1.2), { fill: "c1" }),
      P(
        one([-96, -72, -48, -24, 0, 24, 48, 72, 96], (a, i) =>
          ray(a, 38.6, 38.6 + (i % 2 ? 5.4 : -4.6), 1.1, 1.1),
        ),
        { fill: "c1" },
      ),
      ...row(
        [-96, -72, -48, -24, 0, 24, 48, 72, 96],
        (a, i) => chip(a, 38.6 + (i % 2 ? 7 : -6.2), 3.4, 3.4, 1),
        { fill: "c2", a: true },
      ),
      ...row([-84, -60, -36, -12, 12, 36, 60, 84], (a) => blob(a, 38.6, 1.5), {
        fill: "light",
        a: true,
      }),
      P(arcBand(41.4, -108, -60, 0.8), { fill: "c2", anim: "pulse", a: true }),
      P(arcBand(41.4, 60, 108, 0.8), { fill: "c2", anim: "pulse", a: true, o: 4 }),
    ],
  },
  {
    id: "holo-rim",
    name: "Holo rim",
    group: "Tech",
    anim: "shimmer",
    parts: [
      ...Array.from({ length: 12 }, (_, i) =>
        P(arcBand(39.6, i * 30 - 10, i * 30 + 10, 2.2), {
          z: "back",
          fill: "c1",
          anim: "whirl",
          a: true,
          o: (i % 8) + 1,
        }),
      ),
      ...Array.from({ length: 6 }, (_, i) =>
        P(arcBand(44.4, i * 60 + 14, i * 60 + 46, 1.2), {
          z: "back",
          fill: "c2",
          anim: "whirl",
          a: true,
          o: "r",
        }),
      ),
      ...row([-90, 0, 90, 180], (a) => chip(a, 39.6, 3, 3, 0.8), {
        z: "back",
        fill: "c2",
        a: true,
      }),
    ],
  },
  {
    id: "goggles-up",
    name: "Goggles up",
    group: "Tech",
    anim: "shimmer",
    parts: [
      P(arcBand(36.2, -126, 126, 5), { fill: "#2a2f38" }),
      P(arcBand(38, -126, 126, 1.8), { fill: "c1" }),
      P(chip(0, 38.6, 9, 3.4, 1.2), { fill: "#2a2f38" }),
      ...mirror(28, (a) => blob(a, 37.4, 10.4), { fill: "ink" }),
      ...mirror(28, (a) => blob(a, 37.4, 8.8), { fill: "c1" }),
      ...mirror(28, (a) => blob(a, 37.4, 7.2), { fill: "c2" }),
      ...mirror(28, (a, s) => leaf(a - 6 * s, 34.6, 6.5, 2.2, 2.4 * s), { fill: "light", a: true }),
      ...mirror(76, (a) => chip(a, 36.6, 4.4, 5.6, 1.4), { fill: "c2", anim: "pulse", a: true }),
    ],
  },
  {
    id: "signal",
    name: "Signal",
    group: "Tech",
    anim: "pulse",
    parts: [
      P(arcBand(37.2, -104, 104, 3.4), { fill: "#2a2f38" }),
      P(arcBand(38.8, -104, 104, 0.9), { fill: "c1" }),
      P(ray(0, 37.2, 44, 3.4, 1.8), { fill: "c1" }),
      P(blob(0, 45.4, 2.4), { fill: "c2", a: true }),
      ...row([1, 2, 3], (_a, i) => arcBand(40.4 + i * 2.4, -100, -48, 1.1 + i * 0.2), {
        fill: "c2",
        a: true,
      }),
      ...row([1, 2, 3], (_a, i) => arcBand(40.4 + i * 2.4, 48, 100, 1.1 + i * 0.2), {
        fill: "c2",
        a: true,
      }),
      ...row([-88, 88], (a) => chip(a, 37.2, 3.4, 3.4, 1), { fill: "ink" }),
      ...row([-24, 24], (a) => blob(a, 37.2, 1.4), { fill: "light", anim: "shimmer", a: true }),
    ],
  },
  {
    id: "circuit-crown",
    name: "Circuit crown",
    group: "Tech",
    anim: "pulse",
    tilt: true,
    parts: [
      P(arcBand(37.4, -92, 92, 4.6), { fill: "ink" }),
      P(arcBand(37.4, -92, 92, 2.4), { fill: "c1" }),
      P(one([-70, -42, -14, 14, 42, 70], (a, i) => ray(a, 37.4, 37.4 + [6, 10, 7, 7, 10, 6][i], 4.4, 3)), {
        fill: "ink",
      }),
      P(
        one([-70, -42, -14, 14, 42, 70], (a, i) =>
          ray(a, 37.4, 37.4 + [4.6, 8.4, 5.6, 5.6, 8.4, 4.6][i], 2.4, 1.6),
        ),
        { fill: "c1" },
      ),
      ...row(
        [-70, -42, -14, 14, 42, 70],
        (a, i) => chip(a, 37.4 + [7.4, 11.4, 8.4, 8.4, 11.4, 7.4][i], 3.6, 3, 1),
        { fill: "c2", a: true },
      ),
      ...row([-84, -56, -28, 0, 28, 56, 84], (a) => blob(a, 37.4, 1.4), {
        fill: "light",
        a: true,
      }),
    ],
  },
];

export const DECORATION_BY_ID = Object.fromEntries(
  DECORATIONS.map((d) => [d.id, d]),
);

export const DECORATION_GROUPS = [
  ...new Set(DECORATIONS.map((d) => d.group)),
].map((title) => ({
  title,
  ids: DECORATIONS.filter((d) => d.group === title).map((d) => d.id),
}));

// decoration resolves an id to its definition, or null. Fails CLOSED: an id
// this build does not know renders nothing at all, which is what makes it safe
// to take the value straight off a peer's profile.
export function decoration(id) {
  return DECORATION_BY_ID[id] || null;
}
