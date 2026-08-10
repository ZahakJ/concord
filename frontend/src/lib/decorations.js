// decorations.js — everything you WEAR on an avatar.
//
// A gradient ring (lib/rings.js) is built from gradients: a spinning disc, an
// orbiting dot, a halo, a weather layer. That vocabulary can only ever produce
// rings, which is why every option looked like a variation of the same option.
// A decoration is DRAWN, and drawn covers both of the things worn here:
//
//   a FIGURE — ears, horns, a crown, a jaw closing over your face — which is
//   what makes an avatar look like a character instead of a circle with a
//   gradient behind it; and
//
//   a RING (`ring: true`) — a band of runes, chainmail, laurel, filigree — art
//   that encircles the face rather than perching on it.
//
// They are one library and one picker because they are one question: what are
// you wearing. Nothing stops you wearing both, and the two compose.
//
// Drawn, never downloaded. Nothing in Concord is fetched at runtime, so these
// are paths, not images. That constraint turns out to be the advantage: a path
// recolours to the wearer's own palette, stays sharp from a 20px member row to
// a 96px profile card, animates procedurally rather than as a baked loop, and
// costs a couple of hundred bytes.
//
// ── THE ARCH ────────────────────────────────────────────────────────────────
//
// Every FIGURE here is built on a BAND worn over the head, with everything
// else attached to that band. (The rings at the bottom of the file are the one
// exception, and only because a full circle is already centred by
// construction.) That is not a style choice, it is the fix for the two things
// wrong with the first library: nothing was centred convincingly and nothing
// moved.
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
//   "c1" / "c2"                   the wearer's two profile colours
//   "c1-glint" / "c1-lit"         two steps toward the light
//   "c1-shade" / "c1-deep"        two steps away from it
//   "ink"                         a near-black outline, reads on any backdrop
//   "light"                       a soft highlight
//   "@name"                       a gradient from this decoration's own `defs`
// or a literal hex ONLY where the object's identity IS its colour — the black
// on the tip of a fox's ear, bone on an antler.
//
// The four ramp steps are derived from the base with color-mix in oklab, in
// AvatarDecoration.svelte, and they are the reason a wearer's colour can make
// an OBJECT rather than a silhouette. Two flat tokens can only ever produce a
// wash; five steps give the same shape a lit face, a body and a crease.
//
// `own: [c1, c2]` gives a piece its own colourway — gold for a crown, orange
// for a fox — which the wearer can ask for by name ("As designed"). By default
// a wearer who has set a profile colour gets the piece in their own colour,
// still shaded, and `own` only shows through when there is no profile colour
// at all. Never bake a colourway into the fills instead: that is exactly what
// made half the library unchangeable and the other half the same teal.
//
// What the wearer is actually painted in is decided by COLORWAYS at the bottom
// of this file — a bounded table of named bases (gold, obsidian, azure…) that
// the same ramp expands. The base is the wearer's choice; everything above is
// the piece's.
//
// Material. `defs` carries gradients and filters, built by the helpers below
// and referenced by name. `filter: "name"` puts a part through one.
//   lg / rg / rgb   linear, radial, and object-fitted radial gradients
//   blur            a gaussian, for anything that is more glow than edge
//   turb            noise into a displacement map — fur, flame, frost, smoke.
//                   A `flick` seed list re-seeds the noise in hard steps, which
//                   is the only way to draw something that is genuinely a
//                   different shape from frame to frame rather than a loop.
// `op` sets a part's opacity outright, for the few marks that are pure light.
//
// TIERS. Filters cost an offscreen buffer whatever the element's size, so the
// painter drops them below 40px and a decoration must still read without them
// — everything structural has to be in the gradients. `hi: true` marks fine
// detail (a stray ember, a hard glint) that is dropped at the same threshold.
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
//
// That rule is why the rings below are still reachable from the profile's
// `frame` field as well as its `dec` field. They were their own library once
// and `frame` was where they travelled, so Avatar.svelte resolves a `frame`
// against the RINGS in this table before it falls through to lib/rings.js. No
// migration was needed and none should be written: the ids never moved, only
// the file did.
//
// Against the RINGS, precisely — a `ring: true` entry and nothing else. Only
// those twenty-one ever travelled in `frame`, and matching the whole table
// there meant any figure sharing a name with a gradient ring silently ate it.
// "comet" is a name both libraries chose, and for one commit a wearer of the
// gradient comet got a drawn one.

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

// tongue: a lick of flame, as distinct from `leaf`, which is a petal and looks
// like one. Three things separate fire from foliage and this has all of them: a
// base WIDER than the gap to its neighbour, so a row of them merges into one
// sheet instead of standing apart like a wreath; a bulge that dips below the
// root, so the join is never a straight cut; and a tip that curls to one side,
// because a symmetric point is a leaf however you colour it.
function tongue(a, r, len, w, curl = 0) {
  const f = frame(a, r);
  return (
    `M${f(-w / 2, 0)}` +
    `C${f(-w * 0.6, len * 0.36)} ${f(-w * 0.3 + curl * 0.5, len * 0.7)} ${f(curl, len)}` +
    `C${f(w * 0.24 + curl * 0.3, len * 0.62)} ${f(w * 0.58, len * 0.3)} ${f(w / 2, 0)}` +
    `Q${f(0, -w * 0.26)} ${f(-w / 2, 0)}Z`
  );
}

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

// ── light and material ──────────────────────────────────────────────────────
//
// Flat fills can draw a SHAPE but not an OBJECT, and sixty flat shapes in one
// wardrobe read as sixty stickers. A decoration may therefore carry `defs` —
// gradients and filters, exactly as a card scene does — and reference them from
// a part's `fill` as "@name" or from its `filter`. The builders are the scene
// painter's, deliberately: same names, same argument order, same file style.
//
// ONE LIGHT FOR THE WHOLE LIBRARY, from the UPPER LEFT and slightly in front.
// This is a rule, not a preference. Sixty objects each lit from wherever suited
// them read as sixty objects; the same sixty lit from one place read as a
// wardrobe, and the consistency does more for the set than any single piece
// does. Concretely: highlights go on the upper-left face of a form, shadows
// fall away to the lower right, and a contact shadow on the face sits under and
// slightly right of whatever casts it.
const LIGHT = [-0.6, -0.8];

const LG = (id, x1, y1, x2, y2, stops) => ({ t: "lg", id, x1, y1, x2, y2, stops });
const RG = (id, cx, cy, r, stops, o = {}) => ({ t: "rg", id, cx, cy, r, stops, ...o });
// A radial that fits whatever shape it is painted on rather than a fixed place
// in the box: one definition serves every pearl on a crown at every size. `fx`
// and `fy` move the hot spot off centre — which is the entire difference
// between a sphere and a disc.
const RGB = (id, stops, o = {}) => ({ t: "rgb", id, stops, ...o });
// feTurbulence into feDisplacementMap: material rather than shape. It is the
// one thing SVG has that stops an outline reading as vector — fur that frays,
// flame with a body. `flick` is a list of seeds; giving it one makes the noise
// field JUMP between frames, which is a flame rather than a shape on a loop.
const TURB = (id, freq, scale, o = {}) => ({ t: "turb", id, freq, scale, ...o });

// section: a radial gradient in the AVATAR's coordinates, authored 0..1 across
// a radius range. Because everything in this library is polar, that one
// primitive shades both the thickness of a band and the length of a spoke
// rising off it. It is the cheap way to round a flat shape off: a metal band
// that is dark at both edges and bright a third of the way out reads as a bar
// with a top on it, for the price of a gradient rather than a filter.
const section = (id, r0, r1, stops) =>
  RG(
    id,
    50,
    50,
    r1,
    stops.map(([t, c, a]) => [n((r0 + (r1 - r0) * t) / r1), c, a]),
  );

// axis: a linear gradient laid along the light direction and spanning a circle
// of radius r about the head. Stop 0 is the lit side. Authoring every
// directional gradient through this rather than by picking two endpoints is
// what makes the one-light rule hold without anyone having to remember it.
const axis = (id, r, stops) =>
  LG(
    id,
    n(50 + LIGHT[0] * r),
    n(50 + LIGHT[1] * r),
    n(50 - LIGHT[0] * r),
    n(50 - LIGHT[1] * r),
    stops,
  );

// sheen: the light direction itself, as a translucent black-to-white wash laid
// OVER a form that has already been shaded by `section`. Cross-section gives an
// object its roundness; this gives the whole piece one sun. Two cheap passes
// beat one expensive light filter, and unlike feSpecularLighting they survive
// into the tile tier, where the form is needed most.
const sheen = (id, r = 46, k = 1) =>
  axis(id, r, [
    [0, "#ffffff", n(0.42 * k)],
    [0.26, "#ffffff", n(0.1 * k)],
    [0.48, "#ffffff", 0],
    [0.6, "#000000", 0],
    [1, "#000000", n(0.4 * k)],
  ]);

// cast: the darkening a worn thing throws on the face under it. The first
// library had none, anywhere, and its absence is why every crown floated — a
// contact shadow is the strongest single cue that an object is SITTING on
// something rather than pasted over it.
//
// The gradient is offset down and right of centre, which is where the light
// rule puts it, and the wedge that carries it stays inside radius 36 so the
// shadow can never spill past the avatar's silhouette — themes are allowed to
// square the avatar off, and a shadow escaping the corner would be a bug the
// theme could not fix.
// The wedge the shadow is painted on closes to NOTHING at both ends. A constant
// -thickness band would finish on a straight chord across the wearer's cheek,
// and a hard edge in the middle of a face is worse than no shadow at all —
// that is what the first attempt at this looked like.
function crescent(a1, a2, rOut, depth, steps = 26) {
  const out = [];
  const inn = [];
  for (let i = 0; i <= steps; i++) {
    const t = i / steps;
    const a = a1 + (a2 - a1) * t;
    out.push(xy(a, rOut));
    inn.push(xy(a, rOut - depth * Math.sin(Math.PI * t) ** 0.6));
  }
  return `M${out.join("L")}L${inn.reverse().join("L")}Z`;
}
const castG = (id = "cast", k = 1) =>
  RG(id, 51.6, 51.6, 36, [
    [0, "#04060a", 0],
    [0.74, "#04060a", 0],
    [0.9, "#04060a", n(0.22 * k)],
    [1, "#04060a", n(0.52 * k)],
  ]);
const cast = (a1, a2, depth = 11, ref = "@cast") =>
  P(crescent(a1, a2, 36, depth), { fill: ref });

// The flame crown's tongues, shared by its three layers so they stack in
// register. Every layer is the SAME thirteen roots at different heights, which
// is what makes it read as one fire seen through itself rather than three rows
// of shapes. The heights are irregular on purpose — an even row is a fence —
// and they are bounded by the box: straight up there are only about 14 units
// clear of the viewBox, while a diagonal has twice that.
const FLAME_A = [-86, -72, -58, -44, -30, -16, -2, 12, 26, 40, 54, 68, 82];
const FLAME_L = [5.5, 8, 11.5, 15, 17, 13, 9.5, 12, 16.5, 14, 10.5, 7.5, 5];
const FLAME_C = [2.2, -2.6, 1.8, -3, 2.4, -2, 2.8, -2.4, 3, -1.8, 2.2, -2.6, 1.6];

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
    own: ["#d9762e", "#ffd7b5"],
    defs: [
      castG("cast", 0.6),
      section("band", 35.6, 38.4, [
        [0, "c1-deep"],
        [0.24, "c1-shade"],
        [0.58, "c1"],
        [0.86, "c1-lit"],
        [1, "c1-shade"],
      ]),
      // A fox's ear is orange at the root and BLACK at the tip, and the whole
      // animal is legible from that one fact. Shading along the radius is what
      // lets the ear carry the wearer's colour and still end in a real tip.
      section("ear", 36.5, 58, [
        [0, "c1-deep"],
        [0.12, "c1-shade"],
        [0.32, "c1"],
        [0.56, "c1"],
        [0.8, "c1-shade"],
        [1, "#231610"],
      ]),
      // The hot spot sits at the BOTTOM of the shape (fy 0.95), so the inner
      // ear is dark where it folds into the head and opens toward the tip. A
      // centred radial gave a flat pink lozenge.
      RGB(
        "inner",
        [
          [0, "c2-deep"],
          [0.26, "c2-shade"],
          [0.6, "c2"],
          [1, "c2-lit"],
        ],
        { fx: 0.5, fy: 0.95 },
      ),
      sheen("sun", 54, 0.5),
      // Fur. HIGH frequency and a SMALL displacement: the noise has to be finer
      // than the shape it roughens, or the map stops fraying the outline and
      // starts smearing whole limbs — the first pass at 0.09 turned both ears
      // into thumbprints. No blur either; a blurred edge is felt, not fur.
      // Static, too: fur that crawls is a caterpillar.
      TURB("fur", "0.42 0.62", 1.1, { oct: 2, seed: 7 }),
    ],
    parts: [
      // The tail is gone, deliberately. It hung off the wearer's jaw at four
      // o'clock — over the presence dot — and every attempt to draw it well
      // (spiral, brush, curl) read as a hook or a claw, because a tail seen
      // from the front of a face has nowhere to be. The piece is called Fox
      // ears and the ears are what carries it.
      cast(-70, 70, 10, "@cast"),
      ...mirror(33, (a, s) => spoke(a, 35.5, 22, 22, 7 * s), {
        fill: "@ear",
        filter: "fur",
        a: true,
        pv: 36,
      }),
      ...mirror(33, (a, s) => spoke(a, 35.5, 22, 22, 7 * s), { fill: "@sun", a: true, pv: 36 }),
      ...mirror(33, (a, s) => spoke(a, 37.6, 14.5, 11.5, 7 * s), {
        fill: "@inner",
        a: true,
        pv: 36,
      }),
      // `crescent` again, and not `arcBand`: a constant-width arch ends on two
      // blunt stumps at the wearer's temples. This one thins away to nothing,
      // so the fur runs out instead of being cut off.
      P(crescent(-76, 76, 38.4, 2.8), { fill: "@band" }),
      P(crescent(-76, 76, 38.4, 2.8), { fill: "@sun" }),
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
    // Gold is the DEFAULT here, not the object. A wearer who has chosen a
    // profile colour wears the crown in it, shaded through the same ramp — and
    // that is the answer to the two complaints at once, because the pieces that
    // used to look like metal were exactly the pieces nobody could recolour.
    own: ["#e0a629", "#5ad2e6"],
    defs: [
      // Deeper than the house default. When a wearer takes the crown in their
      // own colour it sits on an avatar of that colour, and the contact shadow
      // is then the only thing separating the two — a red crown on a red face
      // with no shadow is one red shape.
      castG("cast", 1.25),
      // The band in cross-section: dark where it seats on the head, a bright
      // line a third of the way out, dark again over the outer lip. This single
      // gradient is most of what makes it read as metal rather than a yellow
      // arc, and it costs nothing that a filter would.
      section("band", 34.5, 40, [
        [0, "c1-deep"],
        [0.16, "c1-shade"],
        [0.34, "c1-lit"],
        [0.44, "c1-glint"],
        [0.6, "c1"],
        [0.86, "c1-shade"],
        [1, "c1-deep"],
      ]),
      // The points take the same treatment along their LENGTH, so light runs
      // up them to the tips instead of stopping at the band.
      section("point", 38.4, 48.6, [
        [0, "c1-deep"],
        [0.14, "c1-shade"],
        [0.34, "c1"],
        [0.72, "c1-lit"],
        [1, "c1-glint"],
      ]),
      sheen("sun", 50, 0.7),
      // The rim: one unit of the outer lip turned to face the sun. It carries
      // only light, never shade — a dark wire laid over the far side of a band
      // reads as a scratch, not as form.
      axis("rim", 46, [
        [0, "c1-glint", 0.95],
        [0.34, "c1-lit", 0.45],
        [0.62, "c1-lit", 0.08],
        [1, "c1-lit", 0],
      ]),
      // An off-centre hot spot is the whole difference between a sphere and a
      // disc, and a stone is a sphere with a dark rim.
      RGB(
        "gem",
        [
          [0, "c2-glint"],
          [0.3, "c2"],
          [0.72, "c2-shade"],
          [1, "c2-deep"],
        ],
        { fx: 0.34, fy: 0.26 },
      ),
      RGB(
        "pearl",
        [
          [0, "#ffffff"],
          [0.34, "#eef3f9"],
          [0.78, "#aebbca"],
          [1, "#63718a"],
        ],
        { fx: 0.32, fy: 0.26 },
      ),
    ],
    parts: [
      cast(-86, 86),
      P(arcBand(34.7, -84, 84, 1), { fill: "c1-deep" }),
      P(
        one([-70, -46, -22, 0, 22, 46, 70], (a, i) =>
          spoke(a, 38.4, [5.6, 8.2, 9.2, 6.6, 9.2, 8.2, 5.6][i] + 1.2, 16),
        ),
        { fill: "@point" },
      ),
      P(
        one([-70, -46, -22, 0, 22, 46, 70], (a, i) =>
          spoke(a, 38.4, [5.6, 8.2, 9.2, 6.6, 9.2, 8.2, 5.6][i] + 1.2, 16),
        ),
        { fill: "@sun" },
      ),
      P(arcBand(37.2, -84, 84, 5.4), { fill: "@band" }),
      P(arcBand(37.2, -84, 84, 5.4), { fill: "@sun" }),
      P(arcBand(39.5, -84, 84, 0.7), { fill: "@rim" }),
      ...row(
        [-70, -46, -22, 0, 22, 46, 70],
        (a, i) => blob(a, 38.9 + [5.6, 8.2, 9.2, 6.6, 9.2, 8.2, 5.6][i], 1.9),
        { fill: "@gem", a: true },
      ),
      ...row([-58, -34, -11, 11, 34, 58], (a) => blob(a, 37.3, 1.35), {
        fill: "@pearl",
        op: 0.9,
        a: true,
      }),
      P(gem(0, 36.6, 5.8, 7), { fill: "@gem", anim: "shimmer", a: true }),
      // The one mark that is not shaded but LIT: a hard glint on the stone's
      // upper-left facet, where the library's sun is.
      P(blob(-9, 37.6, 1.1), { fill: "light", op: 0.85, anim: "shimmer", a: true, hi: true }),
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
    own: ["#ff6a18", "#ffd23f"],
    defs: [
      // Fire is the one thing worn here that LIGHTS the face instead of
      // shading it, so this is the one piece with no contact SHADOW at all —
      // it has a contact HIGHLIGHT. Same wedge, same rule about where the light
      // comes from, opposite sign.
      //
      // The gradient reaches zero WELL inside the wedge it is painted on. A
      // glow that still has alpha where its shape stops draws a ruled line
      // across the wearer's face, which is what the first two attempts did.
      RG("hearth", 50, 50, 36, [
        [0, "c1-lit", 0],
        [0.82, "c1-lit", 0],
        [0.94, "c1-lit", 0.1],
        [1, "c1-glint", 0.3],
      ]),
      section("ember", 35.4, 38.4, [
        [0, "c1-deep"],
        [0.4, "c1-shade"],
        [0.72, "c1-deep"],
        [1, "c1-deep"],
      ]),
      // Three layers, each fading OUT at its own height. A flame is not a
      // silhouette with a colour, it is a stack of transparencies that run out
      // at different points, and stopping every layer at a hard edge is what
      // made the first version read as a row of petals.
      // Brightest LOW, dissolving at the tip. Fire is hottest where it is fed
      // and it runs out of itself on the way up; a flame that ends in a white
      // point is a highlight on a leaf, which is what the first version was.
      section("outer", 36, 56, [
        [0, "c1-shade", 0.92],
        [0.16, "c1", 1],
        [0.5, "c1", 0.88],
        [0.8, "c1-shade", 0.42],
        [1, "c1-deep", 0],
      ]),
      section("mid", 36, 51, [
        [0, "c1-lit", 0.85],
        [0.18, "c1", 0.95],
        [0.55, "c1", 0.85],
        [0.85, "c1-shade", 0.28],
        [1, "c1-shade", 0],
      ]),
      section("core", 36, 46, [
        [0, "c1-glint", 0.5],
        [0.32, "c1-glint", 0.95],
        [0.72, "c1-lit", 0.65],
        [1, "c1-lit", 0],
      ]),
      // The one animated filter in the app. The noise field is re-seeded in
      // hard steps rather than tweened, because a flame is a DIFFERENT flame
      // from one instant to the next — it does not ease between two shapes.
      // Stepping is also what keeps it affordable: the filter re-rasterises
      // seven times in a second and a half, not sixty times a second.
      TURB("lick", "0.035 0.07", 6, {
        oct: 2,
        seed: 3,
        blur: 0.14,
        flick: "1;5;2;9;4;7;3",
        dur: 1.5,
      }),
    ],
    parts: [
      P(crescent(-96, 96, 36, 11), { fill: "@hearth" }),
      // ONE path for the outer wall of fire, not thirteen. A filter costs an
      // offscreen buffer per ELEMENT it is applied to, and thirteen buffers to
      // say what one says is how a decoration turns into a fan. The turbulence
      // is what varies the tongues; it does not need them to be separate.
      P(
        one(FLAME_A, (a, i) => tongue(a, 36.4, FLAME_L[i], 9.6, FLAME_C[i])),
        { fill: "@outer", filter: "lick" },
      ),
      P(arcBand(36.8, -94, 94, 2.4), { fill: "@ember" }),
      ...row(FLAME_A, (a, i) => tongue(a, 36.6, FLAME_L[i] * 0.7, 6.8, -FLAME_C[i] * 0.7), {
        fill: "@mid",
        a: true,
      }),
      ...row(
        [-58, -44, -30, -16, -2, 12, 26, 40],
        (a, i) => tongue(a, 36.8, [5, 7.5, 8.5, 6.5, 5, 6, 8, 6][i], 2.8, [1, -1, 1, -1, 1, -1, 1, -1][i]),
        { fill: "@core", a: true },
      ),
      ...row([-70, -20, 20, 70], (a) => blob(a, 48, 1.5), {
        fill: "c1-lit",
        anim: "drift",
        a: true,
        hi: true,
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

  // ══ RINGS ═══════════════════════════════════════════════════════════════
  // The other way to wear something: not perched on the head but ENCIRCLING
  // it, in the annulus around r=34..50. Same authoring contract, same painter,
  // same box — a ring simply reaches all the way round. They carry
  // `ring: true` so the avatar can keep its circular silhouette under a theme
  // that squares avatars off, since a disc drawn around a squircle reads as a
  // rendering bug.
  //
  // These are a separate vocabulary from lib/rings.js, not a replacement. A
  // gradient ring can only ever look like a gradient; these are drawn, so one
  // can be a ring of runes, a chainmail band or a laurel.

  // ---------- arcane ----------
  {
    id: "runic-ring",
    name: "Runic ring",
    group: "Arcane",
    ring: true,
    anim: "spin",
    parts: [
      P(
        "M97.6 50A47.6 47.6 0 1 0 2.4 50A47.6 47.6 0 1 0 97.6 50ZM86 50A36 36 0 1 1 14 50A36 36 0 1 1 86 50Z",
        { z: "back", fill: "ink" },
      ),
      P(
        "M96.4 50A46.4 46.4 0 1 0 3.6 50A46.4 46.4 0 1 0 96.4 50ZM87.2 50A37.2 37.2 0 1 1 12.8 50A37.2 37.2 0 1 1 87.2 50Z",
        { fill: "c1" },
      ),
      P(
        "M60.28 5.78A45.4 45.4 0 0 1 63.21 6.56L61.17 13.26A38.4 38.4 0 0 0 58.69 12.6ZM61.62 6.63A44.9 44.9 0 0 1 67.9 8.82L66.87 11.21A42.3 42.3 0 0 0 60.95 9.14ZM81.01 16.84A45.4 45.4 0 0 1 83.16 18.99L78.04 23.77A38.4 38.4 0 0 0 76.23 21.96ZM76.3 15.73A43.2 43.2 0 0 1 84.27 23.7L82.21 25.28A40.6 40.6 0 0 0 74.72 17.79ZM93.44 36.79A45.4 45.4 0 0 1 94.22 39.72L87.4 41.31A38.4 38.4 0 0 0 86.74 38.83ZM91.73 31.86A45.5 45.5 0 0 1 93.95 38.22L91.44 38.9A42.9 42.9 0 0 0 89.34 32.89ZM89.7 39.36A41.1 41.1 0 0 1 90.84 45.35L88.25 45.64A38.5 38.5 0 0 0 87.19 40.04ZM94.22 60.28A45.4 45.4 0 0 1 93.44 63.21L86.74 61.17A38.4 38.4 0 0 0 87.4 58.69ZM93.37 61.62A44.9 44.9 0 0 1 91.18 67.9L88.79 66.87A42.3 42.3 0 0 0 90.86 60.95ZM83.16 81.01A45.4 45.4 0 0 1 81.01 83.16L76.23 78.04A38.4 38.4 0 0 0 78.04 76.23ZM84.27 76.3A43.2 43.2 0 0 1 76.3 84.27L74.72 82.21A40.6 40.6 0 0 0 82.21 74.72ZM63.21 93.44A45.4 45.4 0 0 1 60.28 94.22L58.69 87.4A38.4 38.4 0 0 0 61.17 86.74ZM68.14 91.73A45.5 45.5 0 0 1 61.78 93.95L61.1 91.44A42.9 42.9 0 0 0 67.11 89.34ZM60.64 89.7A41.1 41.1 0 0 1 54.65 90.84L54.36 88.25A38.5 38.5 0 0 0 59.96 87.19ZM39.72 94.22A45.4 45.4 0 0 1 36.79 93.44L38.83 86.74A38.4 38.4 0 0 0 41.31 87.4ZM38.38 93.37A44.9 44.9 0 0 1 32.1 91.18L33.13 88.79A42.3 42.3 0 0 0 39.05 90.86ZM18.99 83.16A45.4 45.4 0 0 1 16.84 81.01L21.96 76.23A38.4 38.4 0 0 0 23.77 78.04ZM23.7 84.27A43.2 43.2 0 0 1 15.73 76.3L17.79 74.72A40.6 40.6 0 0 0 25.28 82.21ZM6.56 63.21A45.4 45.4 0 0 1 5.78 60.28L12.6 58.69A38.4 38.4 0 0 0 13.26 61.17ZM8.27 68.14A45.5 45.5 0 0 1 6.05 61.78L8.56 61.1A42.9 42.9 0 0 0 10.66 67.11ZM10.3 60.64A41.1 41.1 0 0 1 9.16 54.65L11.75 54.36A38.5 38.5 0 0 0 12.81 59.96ZM5.78 39.72A45.4 45.4 0 0 1 6.56 36.79L13.26 38.83A38.4 38.4 0 0 0 12.6 41.31ZM6.63 38.38A44.9 44.9 0 0 1 8.82 32.1L11.21 33.13A42.3 42.3 0 0 0 9.14 39.05ZM16.84 18.99A45.4 45.4 0 0 1 18.99 16.84L23.77 21.96A38.4 38.4 0 0 0 21.96 23.77ZM15.73 23.7A43.2 43.2 0 0 1 23.7 15.73L25.28 17.79A40.6 40.6 0 0 0 17.79 25.28ZM36.79 6.56A45.4 45.4 0 0 1 39.72 5.78L41.31 12.6A38.4 38.4 0 0 0 38.83 13.26ZM31.86 8.27A45.5 45.5 0 0 1 38.22 6.05L38.9 8.56A42.9 42.9 0 0 0 32.89 10.66ZM39.36 10.3A41.1 41.1 0 0 1 45.35 9.16L45.64 11.75A38.5 38.5 0 0 0 40.04 12.81Z",
        { fill: "ink", a: true },
      ),
      P(
        "M50 0.5L54.19 5.2L50 9.5L45.81 5.2ZM99.5 50L94.8 54.19L90.5 50L94.8 45.81ZM50 99.5L45.81 94.8L50 90.5L54.19 94.8ZM0.5 50L5.2 45.81L9.5 50L5.2 54.19Z",
        { fill: "c2" },
      ),
      P(
        "M50 2.8L51.8 5.04L50 7.2L48.2 5.04ZM97.2 50L94.96 51.8L92.8 50L94.96 48.2ZM50 97.2L48.2 94.96L50 92.8L51.8 94.96ZM2.8 50L5.04 48.2L7.2 50L5.04 51.8Z",
        { fill: "light" },
      ),
    ],
  },
  {
    id: "orbit-sigils",
    name: "Orbiting sigils",
    group: "Arcane",
    ring: true,
    anim: "spin",
    parts: [
      P(
        "M91.6 50A41.6 41.6 0 1 0 8.4 50A41.6 41.6 0 1 0 91.6 50ZM86.6 50A36.6 36.6 0 1 1 13.4 50A36.6 36.6 0 1 1 86.6 50Z",
        { fill: "ink" },
      ),
      P(
        "M90.6 50A40.6 40.6 0 1 0 9.4 50A40.6 40.6 0 1 0 90.6 50ZM87.6 50A37.6 37.6 0 1 1 12.4 50A37.6 37.6 0 1 1 87.6 50Z",
        { fill: "c1" },
      ),
      P(
        "M58.09 4.11A46.6 46.6 0 0 1 86.72 21.31L84.51 23.03A43.8 43.8 0 0 0 57.61 6.87ZM93.79 34.06A46.6 46.6 0 0 1 93.21 67.46L90.61 66.41A43.8 43.8 0 0 0 91.16 35.02ZM85.7 79.95A46.6 46.6 0 0 1 56.49 96.15L56.1 93.37A43.8 43.8 0 0 0 83.55 78.15ZM41.91 95.89A46.6 46.6 0 0 1 13.28 78.69L15.49 76.97A43.8 43.8 0 0 0 42.39 93.13ZM6.21 65.94A46.6 46.6 0 0 1 6.79 32.54L9.39 33.59A43.8 43.8 0 0 0 8.84 64.98ZM14.3 20.05A46.6 46.6 0 0 1 43.51 3.85L43.9 6.63A43.8 43.8 0 0 0 16.45 21.85Z",
        { fill: "c2", a: true, o: "r" },
      ),
      P(
        "M50 0.6L54.39 6.02L50 11L45.61 6.02ZM92.78 25.3L90.29 31.81L83.77 30.5L85.89 24.21ZM92.78 74.7L85.89 75.79L83.77 69.5L90.29 68.19ZM50 99.4L45.61 93.98L50 89L54.39 93.98ZM7.22 74.7L9.71 68.19L16.23 69.5L14.11 75.79ZM7.22 25.3L14.11 24.21L16.23 30.5L9.71 31.81Z",
        { fill: "light", a: true, o: "r" },
      ),
      P(
        "M68.94 13.64A41 41 0 0 1 72.02 15.42L70.09 18.45A37.4 37.4 0 0 0 67.28 16.83ZM90.96 48.22A41 41 0 0 1 90.96 51.78L87.36 51.62A37.4 37.4 0 0 0 87.36 48.38ZM72.02 84.58A41 41 0 0 1 68.94 86.36L67.28 83.17A37.4 37.4 0 0 0 70.09 81.55ZM31.06 86.36A41 41 0 0 1 27.98 84.58L29.91 81.55A37.4 37.4 0 0 0 32.72 83.17ZM9.04 51.78A41 41 0 0 1 9.04 48.22L12.64 48.38A37.4 37.4 0 0 0 12.64 51.62ZM27.98 15.42A41 41 0 0 1 31.06 13.64L32.72 16.83A37.4 37.4 0 0 0 29.91 18.45Z",
        { fill: "c2", a: true, o: "l" },
      ),
    ],
  },
  {
    id: "eldritch-iris",
    name: "Eldritch iris",
    group: "Arcane",
    ring: true,
    anim: "flicker",
    parts: [
      P(
        "M50 0.5L62.31 16.17L37.69 16.17ZM85 15L82.63 34.79L65.21 17.37ZM99.5 50L83.83 62.31L83.83 37.69ZM85 85L65.21 82.63L82.63 65.21ZM50 99.5L37.69 83.83L62.31 83.83ZM15 85L17.37 65.21L34.79 82.63ZM0.5 50L16.17 37.69L16.17 62.31ZM15 15L34.79 17.37L17.37 34.79Z",
        { z: "back", fill: "c1" },
      ),
      P(
        "M93.4 50A43.4 43.4 0 1 0 6.6 50A43.4 43.4 0 1 0 93.4 50ZM85.8 50A35.8 35.8 0 1 1 14.2 50A35.8 35.8 0 1 1 85.8 50Z",
        { fill: "c1" },
      ),
      P(
        "M56.96 7.16A43.4 43.4 0 0 1 59.96 7.76L58.31 14.77A36.2 36.2 0 0 0 55.81 14.27ZM72.83 13.09A43.4 43.4 0 0 1 75.37 14.78L71.16 20.63A36.2 36.2 0 0 0 69.04 19.21ZM85.22 24.63A43.4 43.4 0 0 1 86.91 27.17L80.79 30.96A36.2 36.2 0 0 0 79.37 28.84ZM92.24 40.04A43.4 43.4 0 0 1 92.84 43.04L85.73 44.19A36.2 36.2 0 0 0 85.23 41.69ZM92.84 56.96A43.4 43.4 0 0 1 92.24 59.96L85.23 58.31A36.2 36.2 0 0 0 85.73 55.81ZM86.91 72.83A43.4 43.4 0 0 1 85.22 75.37L79.37 71.16A36.2 36.2 0 0 0 80.79 69.04ZM75.37 85.22A43.4 43.4 0 0 1 72.83 86.91L69.04 80.79A36.2 36.2 0 0 0 71.16 79.37ZM59.96 92.24A43.4 43.4 0 0 1 56.96 92.84L55.81 85.73A36.2 36.2 0 0 0 58.31 85.23ZM43.04 92.84A43.4 43.4 0 0 1 40.04 92.24L41.69 85.23A36.2 36.2 0 0 0 44.19 85.73ZM27.17 86.91A43.4 43.4 0 0 1 24.63 85.22L28.84 79.37A36.2 36.2 0 0 0 30.96 80.79ZM14.78 75.37A43.4 43.4 0 0 1 13.09 72.83L19.21 69.04A36.2 36.2 0 0 0 20.63 71.16ZM7.76 59.96A43.4 43.4 0 0 1 7.16 56.96L14.27 55.81A36.2 36.2 0 0 0 14.77 58.31ZM7.16 43.04A43.4 43.4 0 0 1 7.76 40.04L14.77 41.69A36.2 36.2 0 0 0 14.27 44.19ZM13.09 27.17A43.4 43.4 0 0 1 14.78 24.63L20.63 28.84A36.2 36.2 0 0 0 19.21 30.96ZM24.63 14.78A43.4 43.4 0 0 1 27.17 13.09L30.96 19.21A36.2 36.2 0 0 0 28.84 20.63ZM40.04 7.76A43.4 43.4 0 0 1 43.04 7.16L44.19 14.27A36.2 36.2 0 0 0 41.69 14.77Z",
        { fill: "ink", a: true },
      ),
      P(
        "M50 3.4L57.14 13.29L42.86 13.29ZM82.95 17.05L81.01 29.09L70.91 18.99ZM96.6 50L86.71 57.14L86.71 42.86ZM82.95 82.95L70.91 81.01L81.01 70.91ZM50 96.6L42.86 86.71L57.14 86.71ZM17.05 82.95L18.99 70.91L29.09 81.01ZM3.4 50L13.29 42.86L13.29 57.14ZM17.05 17.05L29.09 18.99L18.99 29.09Z",
        { fill: "c2", a: true },
      ),
      P(
        "M50 1.6L52.2 4.65L50 7.6L47.8 4.65ZM84.22 15.78L83.62 19.49L79.98 20.02L80.51 16.38ZM98.4 50L95.35 52.2L92.4 50L95.35 47.8ZM84.22 84.22L80.51 83.62L79.98 79.98L83.62 80.51ZM50 98.4L47.8 95.35L50 92.4L52.2 95.35ZM15.78 84.22L16.38 80.51L20.02 79.98L19.49 83.62ZM1.6 50L4.65 47.8L7.6 50L4.65 52.2ZM15.78 15.78L19.49 16.38L20.02 20.02L16.38 19.49Z",
        { fill: "light", a: true },
      ),
    ],
  },
  {
    id: "spellbound-chain",
    name: "Spellbound chain",
    group: "Arcane",
    ring: true,
    anim: "spin",
    parts: [
      P(
        "M88.6 50A38.6 38.6 0 1 0 11.4 50A38.6 38.6 0 1 0 88.6 50ZM86 50A36 36 0 1 1 14 50A36 36 0 1 1 86 50Z",
        { fill: "ink" },
      ),
      P(
        "M59.6 7.8A9.6 5.4 0 1 0 40.4 7.8A9.6 5.4 0 1 0 59.6 7.8ZM55.8 7.8A5.8 1.6 0 1 1 44.2 7.8A5.8 1.6 0 1 1 55.8 7.8ZM75.02 14.68A9.6 5.4 22.5 1 0 57.28 7.34A9.6 5.4 22.5 1 0 75.02 14.68ZM71.51 13.23A5.8 1.6 22.5 1 1 60.79 8.79A5.8 1.6 22.5 1 1 71.51 13.23ZM86.63 26.95A9.6 5.4 45 1 0 73.05 13.37A9.6 5.4 45 1 0 86.63 26.95ZM83.94 24.26A5.8 1.6 45 1 1 75.74 16.06A5.8 1.6 45 1 1 83.94 24.26ZM92.66 42.72A9.6 5.4 67.5 1 0 85.32 24.98A9.6 5.4 67.5 1 0 92.66 42.72ZM91.21 39.21A5.8 1.6 67.5 1 1 86.77 28.49A5.8 1.6 67.5 1 1 91.21 39.21ZM92.2 59.6A9.6 5.4 90 1 0 92.2 40.4A9.6 5.4 90 1 0 92.2 59.6ZM92.2 55.8A5.8 1.6 90 1 1 92.2 44.2A5.8 1.6 90 1 1 92.2 55.8ZM85.32 75.02A9.6 5.4 112.5 1 0 92.66 57.28A9.6 5.4 112.5 1 0 85.32 75.02ZM86.77 71.51A5.8 1.6 112.5 1 1 91.21 60.79A5.8 1.6 112.5 1 1 86.77 71.51ZM73.05 86.63A9.6 5.4 135 1 0 86.63 73.05A9.6 5.4 135 1 0 73.05 86.63ZM75.74 83.94A5.8 1.6 135 1 1 83.94 75.74A5.8 1.6 135 1 1 75.74 83.94ZM57.28 92.66A9.6 5.4 157.5 1 0 75.02 85.32A9.6 5.4 157.5 1 0 57.28 92.66ZM60.79 91.21A5.8 1.6 157.5 1 1 71.51 86.77A5.8 1.6 157.5 1 1 60.79 91.21ZM40.4 92.2A9.6 5.4 180 1 0 59.6 92.2A9.6 5.4 180 1 0 40.4 92.2ZM44.2 92.2A5.8 1.6 180 1 1 55.8 92.2A5.8 1.6 180 1 1 44.2 92.2ZM24.98 85.32A9.6 5.4 202.5 1 0 42.72 92.66A9.6 5.4 202.5 1 0 24.98 85.32ZM28.49 86.77A5.8 1.6 202.5 1 1 39.21 91.21A5.8 1.6 202.5 1 1 28.49 86.77ZM13.37 73.05A9.6 5.4 225 1 0 26.95 86.63A9.6 5.4 225 1 0 13.37 73.05ZM16.06 75.74A5.8 1.6 225 1 1 24.26 83.94A5.8 1.6 225 1 1 16.06 75.74ZM7.34 57.28A9.6 5.4 247.5 1 0 14.68 75.02A9.6 5.4 247.5 1 0 7.34 57.28ZM8.79 60.79A5.8 1.6 247.5 1 1 13.23 71.51A5.8 1.6 247.5 1 1 8.79 60.79ZM7.8 40.4A9.6 5.4 270 1 0 7.8 59.6A9.6 5.4 270 1 0 7.8 40.4ZM7.8 44.2A5.8 1.6 270 1 1 7.8 55.8A5.8 1.6 270 1 1 7.8 44.2ZM14.68 24.98A9.6 5.4 292.5 1 0 7.34 42.72A9.6 5.4 292.5 1 0 14.68 24.98ZM13.23 28.49A5.8 1.6 292.5 1 1 8.79 39.21A5.8 1.6 292.5 1 1 13.23 28.49ZM26.95 13.37A9.6 5.4 315 1 0 13.37 26.95A9.6 5.4 315 1 0 26.95 13.37ZM24.26 16.06A5.8 1.6 315 1 1 16.06 24.26A5.8 1.6 315 1 1 24.26 16.06ZM42.72 7.34A9.6 5.4 337.5 1 0 24.98 14.68A9.6 5.4 337.5 1 0 42.72 7.34ZM39.21 8.79A5.8 1.6 337.5 1 1 28.49 13.23A5.8 1.6 337.5 1 1 39.21 8.79Z",
        { fill: "ink", a: true },
      ),
      P(
        "M58.6 7.8A8.6 4.6 0 1 0 41.4 7.8A8.6 4.6 0 1 0 58.6 7.8ZM56 7.8A6 2 0 1 1 44 7.8A6 2 0 1 1 56 7.8ZM85.92 26.24A8.6 4.6 45 1 0 73.76 14.08A8.6 4.6 45 1 0 85.92 26.24ZM84.08 24.4A6 2 45 1 1 75.6 15.92A6 2 45 1 1 84.08 24.4ZM92.2 58.6A8.6 4.6 90 1 0 92.2 41.4A8.6 4.6 90 1 0 92.2 58.6ZM92.2 56A6 2 90 1 1 92.2 44A6 2 90 1 1 92.2 56ZM73.76 85.92A8.6 4.6 135 1 0 85.92 73.76A8.6 4.6 135 1 0 73.76 85.92ZM75.6 84.08A6 2 135 1 1 84.08 75.6A6 2 135 1 1 75.6 84.08ZM41.4 92.2A8.6 4.6 180 1 0 58.6 92.2A8.6 4.6 180 1 0 41.4 92.2ZM44 92.2A6 2 180 1 1 56 92.2A6 2 180 1 1 44 92.2ZM14.08 73.76A8.6 4.6 225 1 0 26.24 85.92A8.6 4.6 225 1 0 14.08 73.76ZM15.92 75.6A6 2 225 1 1 24.4 84.08A6 2 225 1 1 15.92 75.6ZM7.8 41.4A8.6 4.6 270 1 0 7.8 58.6A8.6 4.6 270 1 0 7.8 41.4ZM7.8 44A6 2 270 1 1 7.8 56A6 2 270 1 1 7.8 44ZM26.24 14.08A8.6 4.6 315 1 0 14.08 26.24A8.6 4.6 315 1 0 26.24 14.08ZM24.4 15.92A6 2 315 1 1 15.92 24.4A6 2 315 1 1 24.4 15.92Z",
        { fill: "c1", a: true },
      ),
      P(
        "M74.1 14.3A8.6 4.6 22.5 1 0 58.2 7.72A8.6 4.6 22.5 1 0 74.1 14.3ZM71.69 13.31A6 2 22.5 1 1 60.61 8.71A6 2 22.5 1 1 71.69 13.31ZM92.28 41.8A8.6 4.6 67.5 1 0 85.7 25.9A8.6 4.6 67.5 1 0 92.28 41.8ZM91.29 39.39A6 2 67.5 1 1 86.69 28.31A6 2 67.5 1 1 91.29 39.39ZM85.7 74.1A8.6 4.6 112.5 1 0 92.28 58.2A8.6 4.6 112.5 1 0 85.7 74.1ZM86.69 71.69A6 2 112.5 1 1 91.29 60.61A6 2 112.5 1 1 86.69 71.69ZM58.2 92.28A8.6 4.6 157.5 1 0 74.1 85.7A8.6 4.6 157.5 1 0 58.2 92.28ZM60.61 91.29A6 2 157.5 1 1 71.69 86.69A6 2 157.5 1 1 60.61 91.29ZM25.9 85.7A8.6 4.6 202.5 1 0 41.8 92.28A8.6 4.6 202.5 1 0 25.9 85.7ZM28.31 86.69A6 2 202.5 1 1 39.39 91.29A6 2 202.5 1 1 28.31 86.69ZM7.72 58.2A8.6 4.6 247.5 1 0 14.3 74.1A8.6 4.6 247.5 1 0 7.72 58.2ZM8.71 60.61A6 2 247.5 1 1 13.31 71.69A6 2 247.5 1 1 8.71 60.61ZM14.3 25.9A8.6 4.6 292.5 1 0 7.72 41.8A8.6 4.6 292.5 1 0 14.3 25.9ZM13.31 28.31A6 2 292.5 1 1 8.71 39.39A6 2 292.5 1 1 13.31 28.31ZM41.8 7.72A8.6 4.6 337.5 1 0 25.9 14.3A8.6 4.6 337.5 1 0 41.8 7.72ZM39.39 8.71A6 2 337.5 1 1 28.31 13.31A6 2 337.5 1 1 39.39 8.71Z",
        { fill: "c2", a: true },
      ),
      P(
        "M65.38 12.86L66.32 16.35L63.24 18.03L62.26 14.67ZM87.14 34.62L85.33 37.74L81.97 36.76L83.65 33.68ZM87.14 65.38L83.65 66.32L81.97 63.24L85.33 62.26ZM65.38 87.14L62.26 85.33L63.24 81.97L66.32 83.65ZM34.62 87.14L33.68 83.65L36.76 81.97L37.74 85.33ZM12.86 65.38L14.67 62.26L18.03 63.24L16.35 66.32ZM12.86 34.62L16.35 33.68L18.03 36.76L14.67 37.74ZM34.62 12.86L37.74 14.67L36.76 18.03L33.68 16.35Z",
        { fill: "light" },
      ),
    ],
  },
  {
    id: "warding-hex",
    name: "Warding hex",
    group: "Arcane",
    ring: true,
    anim: "spin",
    parts: [
      P(
        "M50 1L92.44 25.5L92.44 74.5L50 99L7.56 74.5L7.56 25.5ZM14.93 29.75L14.93 70.25L50 90.5L85.07 70.25L85.07 29.75L50 9.5Z",
        { z: "back", fill: "ink" },
      ),
      P(
        "M50 2.6L91.05 26.3L91.05 73.7L50 97.4L8.95 73.7L8.95 26.3ZM13.28 28.8L13.28 71.2L50 92.4L86.72 71.2L86.72 28.8L50 7.6Z",
        { fill: "c2" },
      ),
      P(
        "M90 50A40 40 0 1 0 10 50A40 40 0 1 0 90 50ZM86.2 50A36.2 36.2 0 1 1 13.8 50A36.2 36.2 0 1 1 86.2 50Z",
        { fill: "ink" },
      ),
      P(
        "M89 50A39 39 0 1 0 11 50A39 39 0 1 0 89 50ZM87 50A37 37 0 1 1 13 50A37 37 0 1 1 87 50Z",
        { fill: "c1" },
      ),
      P(
        "M50 0.5L54.39 4.81L50 8.7L45.61 4.81ZM92.87 25.25L91.33 31.21L85.77 29.35L86.94 23.6ZM92.87 74.75L86.94 76.4L85.77 70.65L91.33 68.79ZM50 99.5L45.61 95.19L50 91.3L54.39 95.19ZM7.13 74.75L8.67 68.79L14.23 70.65L13.06 76.4ZM7.13 25.25L13.06 23.6L14.23 29.35L8.67 31.21Z",
        { fill: "light" },
      ),
      P(
        "M69.69 12.23A42.6 42.6 0 0 1 72.87 14.06L69.54 19.29A36.4 36.4 0 0 0 66.83 17.72ZM92.56 48.17A42.6 42.6 0 0 1 92.56 51.83L86.37 51.57A36.4 36.4 0 0 0 86.37 48.43ZM72.87 85.94A42.6 42.6 0 0 1 69.69 87.77L66.83 82.28A36.4 36.4 0 0 0 69.54 80.71ZM30.31 87.77A42.6 42.6 0 0 1 27.13 85.94L30.46 80.71A36.4 36.4 0 0 0 33.17 82.28ZM7.44 51.83A42.6 42.6 0 0 1 7.44 48.17L13.63 48.43A36.4 36.4 0 0 0 13.63 51.57ZM27.13 14.06A42.6 42.6 0 0 1 30.31 12.23L33.17 17.72A36.4 36.4 0 0 0 30.46 19.29Z",
        { fill: "c2", a: true },
      ),
    ],
  },
  // ---------- nature ----------
  {
    // Named for the ring it is, not the wreath: the Regalia group already
    // wears a "Laurel wreath", and two identical labels in one library is a
    // picker you cannot choose from.
    id: "laurel-ring",
    name: "Laurel ring",
    group: "Nature",
    ring: true,
    anim: "sway",
    parts: [
      P(
        "M58.7 90.9A41.8 41.8 0 0 0 60.1 9.4L59.7 11A40.2 40.2 0 0 1 58.4 89.3ZM41.3 90.9A41.8 41.8 0 0 1 39.9 9.4L40.3 11A40.2 40.2 0 0 0 41.6 89.3Z",
        { fill: "ink" },
      ),
      P(
        "M66.4 13.1C67 8 63.4 2.5 58.6 1.2C57.9 6.1 61.5 11.6 66.4 13.1ZM76.7 19.6C78.7 14.9 76.8 8.7 72.6 6.2C70.5 10.6 72.4 16.8 76.7 19.6ZM84.5 29C87.8 25.1 87.7 18.7 84.4 15.1C81.1 18.7 81.2 25.1 84.5 29ZM89.2 40.2C93.4 37.5 95.2 31.4 93 27.1C88.8 29.6 87.1 35.6 89.2 40.2ZM90.3 52.3C95.1 50.9 98.4 45.6 97.5 40.9C92.8 42.1 89.5 47.3 90.3 52.3ZM87.8 64.1C92.8 64.2 97.4 60.2 97.8 55.4C93 55.2 88.4 59.2 87.8 64.1ZM81.9 74.8C86.6 76.3 92 73.7 93.7 69.3C89.2 67.7 83.8 70.3 81.9 74.8ZM73.1 83.1C77.1 85.8 83 85 85.8 81.2C82 78.4 76.2 79.3 73.1 83.1ZM62.2 88.5C65.3 92.2 71 93 74.7 90.2C71.9 86.5 66.1 85.7 62.2 88.5Z",
        { fill: "c1", a: true, o: "r" },
      ),
      P(
        "M33.6 13.1C38.5 11.6 42.1 6.1 41.4 1.2C36.6 2.5 33 8 33.6 13.1ZM23.3 19.6C27.6 16.8 29.5 10.6 27.4 6.2C23.2 8.7 21.3 14.9 23.3 19.6ZM15.5 29C18.8 25.1 18.9 18.7 15.6 15.1C12.3 18.7 12.2 25.1 15.5 29ZM10.8 40.2C12.9 35.6 11.2 29.6 7 27.1C4.8 31.4 6.6 37.5 10.8 40.2ZM9.7 52.3C10.5 47.3 7.2 42.1 2.5 40.9C1.6 45.6 4.9 50.9 9.7 52.3ZM12.2 64.1C11.6 59.2 7 55.2 2.2 55.4C2.6 60.2 7.2 64.2 12.2 64.1ZM18.1 74.8C16.2 70.3 10.8 67.7 6.3 69.3C8 73.7 13.4 76.3 18.1 74.8ZM26.9 83.1C23.8 79.3 18 78.4 14.2 81.2C17 85 22.9 85.8 26.9 83.1ZM37.8 88.5C33.9 85.7 28.1 86.5 25.3 90.2C29 93 34.7 92.2 37.8 88.5Z",
        { fill: "c1", a: true, o: "l" },
      ),
      P(
        "M70.7 14.1C68.4 17.2 69 21.6 72 23.8C74.3 20.9 73.7 16.5 70.7 14.1ZM29.3 14.1C26.3 16.5 25.7 20.9 28 23.8C31 21.6 31.6 17.2 29.3 14.1ZM87.4 32.2C83.8 33.4 81.7 37.4 83 40.9C86.6 39.9 88.6 35.9 87.4 32.2ZM12.6 32.2C11.4 35.9 13.4 39.9 17 40.9C18.3 37.4 16.2 33.4 12.6 32.2ZM90.9 56.6C87.2 55.5 83.3 57.6 82.3 61.2C85.8 62.4 89.8 60.3 90.9 56.6ZM9.1 56.6C10.2 60.3 14.2 62.4 17.7 61.2C16.7 57.6 12.8 55.5 9.1 56.6ZM79.9 78.7C77.5 75.7 73 75.2 70.2 77.6C72.4 80.6 76.9 81.1 79.9 78.7ZM20.1 78.7C23.1 81.1 27.6 80.6 29.8 77.6C27 75.2 22.5 75.7 20.1 78.7ZM58.3 90.6C58 86.8 54.7 83.8 51 84.2C51.1 87.9 54.5 90.8 58.3 90.6ZM41.7 90.6C45.5 90.8 48.9 87.9 49 84.2C45.3 83.8 42 86.8 41.7 90.6Z",
        { fill: "c2" },
      ),
      P(
        "M49.1 87.4L44.4 91.5L42.8 96.1L44 96.7L46 92.9L50.9 89.8ZM42.7 96.4A0.7 0.7 0 1 0 44.1 96.4A0.7 0.7 0 1 0 42.7 96.4ZM48.5 88.6A1.5 1.5 0 1 0 51.5 88.6A1.5 1.5 0 1 0 48.5 88.6ZM49.1 89.8L54 92.9L56 96.7L57.2 96.1L55.6 91.5L50.9 87.4ZM55.9 96.4A0.7 0.7 0 1 0 57.3 96.4A0.7 0.7 0 1 0 55.9 96.4ZM46.8 88.6A3.2 3.2 0 1 0 53.2 88.6A3.2 3.2 0 1 0 46.8 88.6Z",
        { fill: "c2" },
      ),
      P(
        "M84 20.3A1.5 1.5 0 1 0 86.9 20.3A1.5 1.5 0 1 0 84 20.3ZM13.2 20.3A1.5 1.5 0 1 0 16.1 20.3A1.5 1.5 0 1 0 13.2 20.3ZM80.6 20.5A1.5 1.5 0 1 0 83.6 20.5A1.5 1.5 0 1 0 80.6 20.5ZM16.5 20.5A1.5 1.5 0 1 0 19.3 20.5A1.5 1.5 0 1 0 16.5 20.5ZM83.1 23.5A1.5 1.5 0 1 0 86.1 23.5A1.5 1.5 0 1 0 83.1 23.5ZM14 23.5A1.5 1.5 0 1 0 16.9 23.5A1.5 1.5 0 1 0 14 23.5ZM90.8 68.8A1.5 1.5 0 1 0 93.7 68.8A1.5 1.5 0 1 0 90.8 68.8ZM6.4 68.8A1.5 1.5 0 1 0 9.3 68.8A1.5 1.5 0 1 0 6.4 68.8ZM89.1 65.9A1.5 1.5 0 1 0 92.1 65.9A1.5 1.5 0 1 0 89.1 65.9ZM8 65.9A1.5 1.5 0 1 0 10.9 65.9A1.5 1.5 0 1 0 8 65.9ZM87.6 69.5A1.5 1.5 0 1 0 90.5 69.5A1.5 1.5 0 1 0 87.6 69.5ZM9.6 69.5A1.5 1.5 0 1 0 12.5 69.5A1.5 1.5 0 1 0 9.6 69.5ZM48.8 88.6A1.2 1.2 0 1 0 51.2 88.6A1.2 1.2 0 1 0 48.8 88.6Z",
        { fill: "light" },
      ),
    ],
  },
  {
    id: "blossom-crown",
    name: "Blossom crown",
    group: "Nature",
    ring: true,
    anim: "spin",
    parts: [
      P(
        "M57.1 14.2C62.9 14.7 68.2 10.2 68.6 4.6C63 3.9 57.7 8.4 57.1 14.2ZM70.3 19.7C75.5 22.3 82 20.3 84.5 15.2C79.6 12.5 73 14.5 70.3 19.7ZM80.3 29.7C84.1 34.2 90.9 34.8 95.2 31.1C91.7 26.7 84.9 26 80.3 29.7ZM85.8 42.9C87.6 48.5 93.7 51.6 99 49.8C97.5 44.4 91.4 41.2 85.8 42.9ZM85.8 57.1C85.3 62.9 89.8 68.2 95.4 68.6C96.1 63 91.6 57.7 85.8 57.1ZM80.3 70.3C77.7 75.5 79.7 82 84.8 84.5C87.5 79.6 85.5 73 80.3 70.3ZM70.3 80.3C65.8 84.1 65.2 90.9 68.9 95.2C73.3 91.7 74 84.9 70.3 80.3ZM57.1 85.8C51.5 87.6 48.4 93.7 50.2 99C55.6 97.5 58.8 91.4 57.1 85.8ZM42.9 85.8C37.1 85.3 31.8 89.8 31.4 95.4C37 96.1 42.3 91.6 42.9 85.8ZM29.7 80.3C24.5 77.7 18 79.7 15.5 84.8C20.4 87.5 27 85.5 29.7 80.3ZM19.7 70.3C15.9 65.8 9.1 65.2 4.8 68.9C8.3 73.3 15.1 74 19.7 70.3ZM14.2 57.1C12.4 51.5 6.3 48.4 1 50.2C2.5 55.6 8.6 58.8 14.2 57.1ZM14.2 42.9C14.7 37.1 10.2 31.8 4.6 31.4C3.9 37 8.4 42.3 14.2 42.9ZM19.7 29.7C22.3 24.5 20.3 18 15.2 15.5C12.5 20.4 14.5 27 19.7 29.7ZM29.7 19.7C34.2 15.9 34.8 9.1 31.1 4.8C26.7 8.3 26 15.1 29.7 19.7ZM42.9 14.2C48.5 12.4 51.6 6.3 49.8 1C44.4 2.5 41.2 8.6 42.9 14.2Z",
        { z: "back", fill: "c1", a: true },
      ),
      P(
        "M50 8.5C54.5 6.5 54.5 3.2 50 1.3C45.5 3.2 45.5 6.5 50 8.5ZM50 8.5C53.3 12.1 56.4 11.1 56.8 6.3C53.7 2.6 50.5 3.6 50 8.5ZM50 8.5C47.6 12.8 49.5 15.4 54.2 14.3C56.7 10.2 54.8 7.5 50 8.5ZM50 8.5C45.2 7.5 43.3 10.2 45.8 14.3C50.5 15.4 52.4 12.8 50 8.5ZM50 8.5C49.5 3.6 46.3 2.6 43.2 6.3C43.6 11.1 46.7 12.1 50 8.5ZM91.5 50C93.5 54.5 96.8 54.5 98.7 50C96.8 45.5 93.5 45.5 91.5 50ZM91.5 50C87.9 53.3 88.9 56.4 93.7 56.8C97.4 53.7 96.4 50.5 91.5 50ZM91.5 50C87.2 47.6 84.6 49.5 85.7 54.2C89.8 56.7 92.5 54.8 91.5 50ZM91.5 50C92.5 45.2 89.8 43.3 85.7 45.8C84.6 50.5 87.2 52.4 91.5 50ZM91.5 50C96.4 49.5 97.4 46.3 93.7 43.2C88.9 43.6 87.9 46.7 91.5 50ZM50 91.5C45.5 93.5 45.5 96.8 50 98.7C54.5 96.8 54.5 93.5 50 91.5ZM50 91.5C46.7 87.9 43.6 88.9 43.2 93.7C46.3 97.4 49.5 96.4 50 91.5ZM50 91.5C52.4 87.2 50.5 84.6 45.8 85.7C43.3 89.8 45.2 92.5 50 91.5ZM50 91.5C54.8 92.5 56.7 89.8 54.2 85.7C49.5 84.6 47.6 87.2 50 91.5ZM50 91.5C50.5 96.4 53.7 97.4 56.8 93.7C56.4 88.9 53.3 87.9 50 91.5ZM8.5 50C6.5 45.5 3.2 45.5 1.3 50C3.2 54.5 6.5 54.5 8.5 50ZM8.5 50C12.1 46.7 11.1 43.6 6.3 43.2C2.6 46.3 3.6 49.5 8.5 50ZM8.5 50C12.8 52.4 15.4 50.5 14.3 45.8C10.2 43.3 7.5 45.2 8.5 50ZM8.5 50C7.5 54.8 10.2 56.7 14.3 54.2C15.4 49.5 12.8 47.6 8.5 50ZM8.5 50C3.6 50.5 2.6 53.7 6.3 56.8C11.1 56.4 12.1 53.3 8.5 50Z",
        { fill: "light", a: true },
      ),
      P(
        "M79.3 20.7C82 24.8 85.3 24.3 86.4 19.6C83.9 15.5 80.6 16 79.3 20.7ZM79.3 20.7C76.2 24.5 77.7 27.5 82.6 27.1C85.7 23.4 84.2 20.5 79.3 20.7ZM79.3 20.7C74.7 19 72.4 21.3 74.2 25.8C78.7 27.6 81 25.3 79.3 20.7ZM79.3 20.7C79.5 15.8 76.6 14.3 72.9 17.4C72.5 22.3 75.5 23.8 79.3 20.7ZM79.3 20.7C84 19.4 84.5 16.1 80.4 13.6C75.7 14.7 75.2 18 79.3 20.7ZM79.3 79.3C75.2 82 75.7 85.3 80.4 86.4C84.5 83.9 84 80.6 79.3 79.3ZM79.3 79.3C75.5 76.2 72.5 77.7 72.9 82.6C76.6 85.7 79.5 84.2 79.3 79.3ZM79.3 79.3C81 74.7 78.7 72.4 74.2 74.2C72.4 78.7 74.7 81 79.3 79.3ZM79.3 79.3C84.2 79.5 85.7 76.6 82.6 72.9C77.7 72.5 76.2 75.5 79.3 79.3ZM79.3 79.3C80.6 84 83.9 84.5 86.4 80.4C85.3 75.7 82 75.2 79.3 79.3ZM20.7 79.3C18 75.2 14.7 75.7 13.6 80.4C16.1 84.5 19.4 84 20.7 79.3ZM20.7 79.3C23.8 75.5 22.3 72.5 17.4 72.9C14.3 76.6 15.8 79.5 20.7 79.3ZM20.7 79.3C25.3 81 27.6 78.7 25.8 74.2C21.3 72.4 19 74.7 20.7 79.3ZM20.7 79.3C20.5 84.2 23.4 85.7 27.1 82.6C27.5 77.7 24.5 76.2 20.7 79.3ZM20.7 79.3C16 80.6 15.5 83.9 19.6 86.4C24.3 85.3 24.8 82 20.7 79.3ZM20.7 20.7C24.8 18 24.3 14.7 19.6 13.6C15.5 16.1 16 19.4 20.7 20.7ZM20.7 20.7C24.5 23.8 27.5 22.3 27.1 17.4C23.4 14.3 20.5 15.8 20.7 20.7ZM20.7 20.7C19 25.3 21.3 27.6 25.8 25.8C27.6 21.3 25.3 19 20.7 20.7ZM20.7 20.7C15.8 20.5 14.3 23.4 17.4 27.1C22.3 27.5 23.8 24.5 20.7 20.7ZM20.7 20.7C19.4 16 16.1 15.5 13.6 19.6C14.7 24.3 18 24.8 20.7 20.7Z",
        { fill: "c2", a: true },
      ),
      P(
        "M47.7 8.5A2.3 2.3 0 1 0 52.3 8.5A2.3 2.3 0 1 0 47.7 8.5ZM89.2 50A2.3 2.3 0 1 0 93.8 50A2.3 2.3 0 1 0 89.2 50ZM47.7 91.5A2.3 2.3 0 1 0 52.3 91.5A2.3 2.3 0 1 0 47.7 91.5ZM6.2 50A2.3 2.3 0 1 0 10.8 50A2.3 2.3 0 1 0 6.2 50Z",
        { fill: "c2", a: true },
      ),
      P(
        "M77 20.7A2.3 2.3 0 1 0 81.6 20.7A2.3 2.3 0 1 0 77 20.7ZM77 79.3A2.3 2.3 0 1 0 81.6 79.3A2.3 2.3 0 1 0 77 79.3ZM18.4 79.3A2.3 2.3 0 1 0 23 79.3A2.3 2.3 0 1 0 18.4 79.3ZM18.4 20.7A2.3 2.3 0 1 0 23 20.7A2.3 2.3 0 1 0 18.4 20.7Z",
        { fill: "light", a: true },
      ),
    ],
  },
  // ---------- forged ----------
  {
    id: "riveted-band",
    name: "Riveted band",
    group: "Forged",
    ring: true,
    anim: "spin",
    parts: [
      P(
        "M1,50A49,49 0 0 1 99,50A49,49 0 0 1 1,50ZM14,50A36,36 0 0 0 86,50A36,36 0 0 0 14,50Z",
        { z: "back", fill: "ink" },
      ),
      P(
        "M84.45,51.81L96.94,52.46A47,47 0 0 1 75.6,89.42L68.79,78.93A34.5,34.5 0 0 0 84.45,51.81ZM65.66,80.74L71.34,91.88A47,47 0 0 1 28.66,91.88L34.34,80.74A34.5,34.5 0 0 0 65.66,80.74ZM31.21,78.93L24.4,89.42A47,47 0 0 1 3.06,52.46L15.55,51.81A34.5,34.5 0 0 0 31.21,78.93ZM15.55,48.19L3.06,47.54A47,47 0 0 1 24.4,10.58L31.21,21.07A34.5,34.5 0 0 0 15.55,48.19ZM34.34,19.26L28.66,8.12A47,47 0 0 1 71.34,8.12L65.66,19.26A34.5,34.5 0 0 0 34.34,19.26ZM68.79,21.07L75.6,10.58A47,47 0 0 1 96.94,47.54L84.45,48.19A34.5,34.5 0 0 0 68.79,21.07Z",
        { fill: "c1" },
      ),
      P(
        "M95.14,52.37L96.94,52.46A47,47 0 0 1 75.6,89.42L74.62,87.91A45.2,45.2 0 0 0 95.14,52.37ZM70.52,90.27L71.34,91.88A47,47 0 0 1 28.66,91.88L29.48,90.27A45.2,45.2 0 0 0 70.52,90.27ZM25.38,87.91L24.4,89.42A47,47 0 0 1 3.06,52.46L4.86,52.37A45.2,45.2 0 0 0 25.38,87.91ZM4.86,47.63L3.06,47.54A47,47 0 0 1 24.4,10.58L25.38,12.09A45.2,45.2 0 0 0 4.86,47.63ZM29.48,9.73L28.66,8.12A47,47 0 0 1 71.34,8.12L70.52,9.73A45.2,45.2 0 0 0 29.48,9.73ZM74.62,12.09L75.6,10.58A47,47 0 0 1 96.94,47.54L95.14,47.63A45.2,45.2 0 0 0 74.62,12.09Z",
        { fill: "c2" },
      ),
      P(
        "M84.45,51.81L85.85,51.88A35.9,35.9 0 0 1 69.55,80.11L68.79,78.93A34.5,34.5 0 0 0 84.45,51.81ZM65.66,80.74L66.3,81.99A35.9,35.9 0 0 1 33.7,81.99L34.34,80.74A34.5,34.5 0 0 0 65.66,80.74ZM31.21,78.93L30.45,80.11A35.9,35.9 0 0 1 14.15,51.88L15.55,51.81A34.5,34.5 0 0 0 31.21,78.93ZM15.55,48.19L14.15,48.12A35.9,35.9 0 0 1 30.45,19.89L31.21,21.07A34.5,34.5 0 0 0 15.55,48.19ZM34.34,19.26L33.7,18.01A35.9,35.9 0 0 1 66.3,18.01L65.66,19.26A34.5,34.5 0 0 0 34.34,19.26ZM68.79,21.07L69.55,19.89A35.9,35.9 0 0 1 85.85,48.12L84.45,48.19A34.5,34.5 0 0 0 68.79,21.07Z",
        { fill: "ink" },
      ),
      P(
        "M86.99,59.82A2.4,2.4 0 0 1 91.79,59.82A2.4,2.4 0 0 1 86.99,59.82ZM75.8,79.21A2.4,2.4 0 0 1 80.6,79.21A2.4,2.4 0 0 1 75.8,79.21ZM58.79,89.03A2.4,2.4 0 0 1 63.59,89.03A2.4,2.4 0 0 1 58.79,89.03ZM36.41,89.03A2.4,2.4 0 0 1 41.21,89.03A2.4,2.4 0 0 1 36.41,89.03ZM19.4,79.21A2.4,2.4 0 0 1 24.2,79.21A2.4,2.4 0 0 1 19.4,79.21ZM8.21,59.82A2.4,2.4 0 0 1 13.01,59.82A2.4,2.4 0 0 1 8.21,59.82ZM8.21,40.18A2.4,2.4 0 0 1 13.01,40.18A2.4,2.4 0 0 1 8.21,40.18ZM19.4,20.79A2.4,2.4 0 0 1 24.2,20.79A2.4,2.4 0 0 1 19.4,20.79ZM36.41,10.97A2.4,2.4 0 0 1 41.21,10.97A2.4,2.4 0 0 1 36.41,10.97ZM58.79,10.97A2.4,2.4 0 0 1 63.59,10.97A2.4,2.4 0 0 1 58.79,10.97ZM75.8,20.79A2.4,2.4 0 0 1 80.6,20.79A2.4,2.4 0 0 1 75.8,20.79ZM86.99,40.18A2.4,2.4 0 0 1 91.79,40.18A2.4,2.4 0 0 1 86.99,40.18Z",
        { fill: "light" },
      ),
      P(
        "M88.29,59.82A1.1,1.1 0 0 1 90.49,59.82A1.1,1.1 0 0 1 88.29,59.82ZM77.1,79.21A1.1,1.1 0 0 1 79.3,79.21A1.1,1.1 0 0 1 77.1,79.21ZM60.09,89.03A1.1,1.1 0 0 1 62.29,89.03A1.1,1.1 0 0 1 60.09,89.03ZM37.71,89.03A1.1,1.1 0 0 1 39.91,89.03A1.1,1.1 0 0 1 37.71,89.03ZM20.7,79.21A1.1,1.1 0 0 1 22.9,79.21A1.1,1.1 0 0 1 20.7,79.21ZM9.51,59.82A1.1,1.1 0 0 1 11.71,59.82A1.1,1.1 0 0 1 9.51,59.82ZM9.51,40.18A1.1,1.1 0 0 1 11.71,40.18A1.1,1.1 0 0 1 9.51,40.18ZM20.7,20.79A1.1,1.1 0 0 1 22.9,20.79A1.1,1.1 0 0 1 20.7,20.79ZM37.71,10.97A1.1,1.1 0 0 1 39.91,10.97A1.1,1.1 0 0 1 37.71,10.97ZM60.09,10.97A1.1,1.1 0 0 1 62.29,10.97A1.1,1.1 0 0 1 60.09,10.97ZM77.1,20.79A1.1,1.1 0 0 1 79.3,20.79A1.1,1.1 0 0 1 77.1,20.79ZM88.29,40.18A1.1,1.1 0 0 1 90.49,40.18A1.1,1.1 0 0 1 88.29,40.18Z",
        { fill: "ink" },
      ),
      P("M10.51,27.2Q21.37,5.91 45.23,4.65Q25.94,12.96 10.51,27.2Z", {
        fill: "light",
        a: true,
      }),
    ],
  },
  {
    id: "gold-filigree",
    name: "Gold filigree",
    group: "Forged",
    ring: true,
    anim: "spin",
    parts: [
      P(
        "M2,50A48,48 0 0 1 98,50A48,48 0 0 1 2,50ZM3.8,50A46.2,46.2 0 0 0 96.2,50A46.2,46.2 0 0 0 3.8,50ZM13,50A37,37 0 0 1 87,50A37,37 0 0 1 13,50ZM14.8,50A35.2,35.2 0 0 0 85.2,50A35.2,35.2 0 0 0 14.8,50Z",
        { fill: "#d8a12a" },
      ),
      P(
        "M58.69,9.11Q91.11,8.89 90.89,41.31Q76.69,23.31 58.69,9.11ZM90.89,58.69Q91.11,91.11 58.69,90.89Q76.69,76.69 90.89,58.69ZM41.31,90.89Q8.89,91.11 9.11,58.69Q23.31,76.69 41.31,90.89ZM9.11,41.31Q8.89,8.89 41.31,9.11Q23.31,23.31 9.11,41.31Z",
        { fill: "#d8a12a", a: true },
      ),
      P(
        "M64.23,10.91Q86.13,13.87 89.09,35.77Q77.93,22.07 64.23,10.91ZM89.09,64.23Q86.13,86.13 64.23,89.09Q77.93,77.93 89.09,64.23ZM35.77,89.09Q13.87,86.13 10.91,64.23Q22.07,77.93 35.77,89.09ZM10.91,35.77Q13.87,13.87 35.77,10.91Q22.07,22.07 10.91,35.77Z",
        { fill: "#7d5410", a: true },
      ),
      P(
        "M66.92,12Q83.92,16.08 88,33.08Q78.82,21.18 66.92,12ZM88,66.92Q83.92,83.92 66.92,88Q78.82,78.82 88,66.92ZM33.08,88Q16.08,83.92 12,66.92Q21.18,78.82 33.08,88ZM12,33.08Q16.08,16.08 33.08,12Q21.18,21.18 12,33.08Z",
        { fill: "#f8e2a2", a: true },
      ),
      P(
        "M50,15L46.23,8.57L50,1.8L53.77,8.57ZM85,50L91.43,46.23L98.2,50L91.43,53.77ZM50,85L53.77,91.43L50,98.2L46.23,91.43ZM15,50L8.57,53.77L1.8,50L8.57,46.23Z",
        { fill: "#d8a12a", a: true },
      ),
      P(
        "M47.7,8.4A2.3,2.3 0 0 1 52.3,8.4A2.3,2.3 0 0 1 47.7,8.4ZM89.3,50A2.3,2.3 0 0 1 93.9,50A2.3,2.3 0 0 1 89.3,50ZM47.7,91.6A2.3,2.3 0 0 1 52.3,91.6A2.3,2.3 0 0 1 47.7,91.6ZM6.1,50A2.3,2.3 0 0 1 10.7,50A2.3,2.3 0 0 1 6.1,50Z",
        { fill: "#f8e2a2", a: true },
      ),
    ],
  },
  {
    id: "gear-ring",
    name: "Gear ring",
    group: "Forged",
    ring: true,
    anim: "spin",
    parts: [
      P(
        "M1,50A49,49 0 0 1 99,50A49,49 0 0 1 1,50ZM14,50A36,36 0 0 0 86,50A36,36 0 0 0 14,50Z",
        { z: "back", fill: "ink" },
      ),
      P(
        "M90.69,45L98.44,46.1L98.44,53.9L90.69,55ZM87.74,66.02L93.9,70.85L90,77.6L82.74,74.67ZM74.67,82.74L77.6,90L70.85,93.9L66.02,87.74ZM55,90.69L53.9,98.44L46.1,98.44L45,90.69ZM33.98,87.74L29.15,93.9L22.4,90L25.33,82.74ZM17.26,74.67L10,77.6L6.1,70.85L12.26,66.02ZM9.31,55L1.56,53.9L1.56,46.1L9.31,45ZM12.26,33.98L6.1,29.15L10,22.4L17.26,25.33ZM25.33,17.26L22.4,10L29.15,6.1L33.98,12.26ZM45,9.31L46.1,1.56L53.9,1.56L55,9.31ZM66.02,12.26L70.85,6.1L77.6,10L74.67,17.26ZM82.74,25.33L90,22.4L93.9,29.15L87.74,33.98Z",
        { z: "back", fill: "c2", a: true },
      ),
      P(
        "M7,50A43,43 0 0 1 93,50A43,43 0 0 1 7,50ZM15.5,50A34.5,34.5 0 0 0 84.5,50A34.5,34.5 0 0 0 15.5,50Z",
        { fill: "c1", a: true },
      ),
      P(
        "M7,50A43,43 0 0 1 93,50A43,43 0 0 1 7,50ZM8.6,50A41.4,41.4 0 0 0 91.4,50A41.4,41.4 0 0 0 8.6,50Z",
        { fill: "ink", a: true },
      ),
      P(
        "M84.3,59.89A2.6,2.6 0 0 1 89.5,59.89A2.6,2.6 0 0 1 84.3,59.89ZM57.29,86.9A2.6,2.6 0 0 1 62.49,86.9A2.6,2.6 0 0 1 57.29,86.9ZM20.39,77.01A2.6,2.6 0 0 1 25.59,77.01A2.6,2.6 0 0 1 20.39,77.01ZM10.5,40.11A2.6,2.6 0 0 1 15.7,40.11A2.6,2.6 0 0 1 10.5,40.11ZM37.51,13.1A2.6,2.6 0 0 1 42.71,13.1A2.6,2.6 0 0 1 37.51,13.1ZM74.41,22.99A2.6,2.6 0 0 1 79.61,22.99A2.6,2.6 0 0 1 74.41,22.99Z",
        { fill: "ink", a: true },
      ),
      P(
        "M85.6,59.89A1.3,1.3 0 0 1 88.2,59.89A1.3,1.3 0 0 1 85.6,59.89ZM58.59,86.9A1.3,1.3 0 0 1 61.19,86.9A1.3,1.3 0 0 1 58.59,86.9ZM21.69,77.01A1.3,1.3 0 0 1 24.29,77.01A1.3,1.3 0 0 1 21.69,77.01ZM11.8,40.11A1.3,1.3 0 0 1 14.4,40.11A1.3,1.3 0 0 1 11.8,40.11ZM38.81,13.1A1.3,1.3 0 0 1 41.41,13.1A1.3,1.3 0 0 1 38.81,13.1ZM75.71,22.99A1.3,1.3 0 0 1 78.31,22.99A1.3,1.3 0 0 1 75.71,22.99Z",
        { fill: "light", a: true },
      ),
    ],
  },
  {
    id: "chainmail",
    name: "Chainmail",
    group: "Forged",
    ring: true,
    anim: "flicker",
    parts: [
      P(
        "M1,50A49,49 0 0 1 99,50A49,49 0 0 1 1,50ZM14,50A36,36 0 0 0 86,50A36,36 0 0 0 14,50Z",
        { z: "back", fill: "ink" },
      ),
      P(
        "M83,50A8.2,8.2 0 0 1 99.4,50A8.2,8.2 0 0 1 83,50ZM85.6,50A5.6,5.6 0 0 0 96.8,50A5.6,5.6 0 0 0 85.6,50ZM75.13,74.22A8.2,8.2 0 0 1 91.53,74.22A8.2,8.2 0 0 1 75.13,74.22ZM77.73,74.22A5.6,5.6 0 0 0 88.93,74.22A5.6,5.6 0 0 0 77.73,74.22ZM54.53,89.18A8.2,8.2 0 0 1 70.93,89.18A8.2,8.2 0 0 1 54.53,89.18ZM57.13,89.18A5.6,5.6 0 0 0 68.33,89.18A5.6,5.6 0 0 0 57.13,89.18ZM29.07,89.18A8.2,8.2 0 0 1 45.47,89.18A8.2,8.2 0 0 1 29.07,89.18ZM31.67,89.18A5.6,5.6 0 0 0 42.87,89.18A5.6,5.6 0 0 0 31.67,89.18ZM8.47,74.22A8.2,8.2 0 0 1 24.87,74.22A8.2,8.2 0 0 1 8.47,74.22ZM11.07,74.22A5.6,5.6 0 0 0 22.27,74.22A5.6,5.6 0 0 0 11.07,74.22ZM0.6,50A8.2,8.2 0 0 1 17,50A8.2,8.2 0 0 1 0.6,50ZM3.2,50A5.6,5.6 0 0 0 14.4,50A5.6,5.6 0 0 0 3.2,50ZM8.47,25.78A8.2,8.2 0 0 1 24.87,25.78A8.2,8.2 0 0 1 8.47,25.78ZM11.07,25.78A5.6,5.6 0 0 0 22.27,25.78A5.6,5.6 0 0 0 11.07,25.78ZM29.07,10.82A8.2,8.2 0 0 1 45.47,10.82A8.2,8.2 0 0 1 29.07,10.82ZM31.67,10.82A5.6,5.6 0 0 0 42.87,10.82A5.6,5.6 0 0 0 31.67,10.82ZM54.53,10.82A8.2,8.2 0 0 1 70.93,10.82A8.2,8.2 0 0 1 54.53,10.82ZM57.13,10.82A5.6,5.6 0 0 0 68.33,10.82A5.6,5.6 0 0 0 57.13,10.82ZM75.13,25.78A8.2,8.2 0 0 1 91.53,25.78A8.2,8.2 0 0 1 75.13,25.78ZM77.73,25.78A5.6,5.6 0 0 0 88.93,25.78A5.6,5.6 0 0 0 77.73,25.78Z",
        { fill: "c2" },
      ),
      P(
        "M80.98,62.73A8.2,8.2 0 0 1 97.38,62.73A8.2,8.2 0 0 1 80.98,62.73ZM83.58,62.73A5.6,5.6 0 0 0 94.78,62.73A5.6,5.6 0 0 0 83.58,62.73ZM66.02,83.33A8.2,8.2 0 0 1 82.42,83.33A8.2,8.2 0 0 1 66.02,83.33ZM68.62,83.33A5.6,5.6 0 0 0 79.82,83.33A5.6,5.6 0 0 0 68.62,83.33ZM41.8,91.2A8.2,8.2 0 0 1 58.2,91.2A8.2,8.2 0 0 1 41.8,91.2ZM44.4,91.2A5.6,5.6 0 0 0 55.6,91.2A5.6,5.6 0 0 0 44.4,91.2ZM17.58,83.33A8.2,8.2 0 0 1 33.98,83.33A8.2,8.2 0 0 1 17.58,83.33ZM20.18,83.33A5.6,5.6 0 0 0 31.38,83.33A5.6,5.6 0 0 0 20.18,83.33ZM2.62,62.73A8.2,8.2 0 0 1 19.02,62.73A8.2,8.2 0 0 1 2.62,62.73ZM5.22,62.73A5.6,5.6 0 0 0 16.42,62.73A5.6,5.6 0 0 0 5.22,62.73ZM2.62,37.27A8.2,8.2 0 0 1 19.02,37.27A8.2,8.2 0 0 1 2.62,37.27ZM5.22,37.27A5.6,5.6 0 0 0 16.42,37.27A5.6,5.6 0 0 0 5.22,37.27ZM17.58,16.67A8.2,8.2 0 0 1 33.98,16.67A8.2,8.2 0 0 1 17.58,16.67ZM20.18,16.67A5.6,5.6 0 0 0 31.38,16.67A5.6,5.6 0 0 0 20.18,16.67ZM41.8,8.8A8.2,8.2 0 0 1 58.2,8.8A8.2,8.2 0 0 1 41.8,8.8ZM44.4,8.8A5.6,5.6 0 0 0 55.6,8.8A5.6,5.6 0 0 0 44.4,8.8ZM66.02,16.67A8.2,8.2 0 0 1 82.42,16.67A8.2,8.2 0 0 1 66.02,16.67ZM68.62,16.67A5.6,5.6 0 0 0 79.82,16.67A5.6,5.6 0 0 0 68.62,16.67ZM80.98,37.27A8.2,8.2 0 0 1 97.38,37.27A8.2,8.2 0 0 1 80.98,37.27ZM83.58,37.27A5.6,5.6 0 0 0 94.78,37.27A5.6,5.6 0 0 0 83.58,37.27Z",
        { fill: "c1" },
      ),
      P(
        "M82.18,62.73A7,7 0 0 1 96.18,62.73A7,7 0 0 1 82.18,62.73ZM83.18,62.73A6,6 0 0 0 95.18,62.73A6,6 0 0 0 83.18,62.73ZM67.22,83.33A7,7 0 0 1 81.22,83.33A7,7 0 0 1 67.22,83.33ZM68.22,83.33A6,6 0 0 0 80.22,83.33A6,6 0 0 0 68.22,83.33ZM43,91.2A7,7 0 0 1 57,91.2A7,7 0 0 1 43,91.2ZM44,91.2A6,6 0 0 0 56,91.2A6,6 0 0 0 44,91.2ZM18.78,83.33A7,7 0 0 1 32.78,83.33A7,7 0 0 1 18.78,83.33ZM19.78,83.33A6,6 0 0 0 31.78,83.33A6,6 0 0 0 19.78,83.33ZM3.82,62.73A7,7 0 0 1 17.82,62.73A7,7 0 0 1 3.82,62.73ZM4.82,62.73A6,6 0 0 0 16.82,62.73A6,6 0 0 0 4.82,62.73ZM3.82,37.27A7,7 0 0 1 17.82,37.27A7,7 0 0 1 3.82,37.27ZM4.82,37.27A6,6 0 0 0 16.82,37.27A6,6 0 0 0 4.82,37.27ZM18.78,16.67A7,7 0 0 1 32.78,16.67A7,7 0 0 1 18.78,16.67ZM19.78,16.67A6,6 0 0 0 31.78,16.67A6,6 0 0 0 19.78,16.67ZM43,8.8A7,7 0 0 1 57,8.8A7,7 0 0 1 43,8.8ZM44,8.8A6,6 0 0 0 56,8.8A6,6 0 0 0 44,8.8ZM67.22,16.67A7,7 0 0 1 81.22,16.67A7,7 0 0 1 67.22,16.67ZM68.22,16.67A6,6 0 0 0 80.22,16.67A6,6 0 0 0 68.22,16.67ZM82.18,37.27A7,7 0 0 1 96.18,37.27A7,7 0 0 1 82.18,37.27ZM83.18,37.27A6,6 0 0 0 95.18,37.27A6,6 0 0 0 83.18,37.27Z",
        { fill: "light", a: true },
      ),
    ],
  },
  {
    id: "hammered-bronze",
    name: "Hammered bronze",
    group: "Forged",
    ring: true,
    anim: "spin",
    parts: [
      P(
        "M1.4,50A48.6,48.6 0 0 1 98.6,50A48.6,48.6 0 0 1 1.4,50ZM14,50A36,36 0 0 0 86,50A36,36 0 0 0 14,50Z",
        { z: "back", fill: "#63380f" },
      ),
      P(
        "M3.4,50A46.6,46.6 0 0 1 96.6,50A46.6,46.6 0 0 1 3.4,50ZM15.5,50A34.5,34.5 0 0 0 84.5,50A34.5,34.5 0 0 0 15.5,50Z",
        { fill: "#bb7a3c" },
      ),
      P(
        "M96.54,45.11Q102.66,50 96.54,54.89Q90.26,50 96.54,45.11ZM94.06,65.79Q97.44,72.85 89.81,74.6Q86.27,67.47 94.06,65.79ZM82.84,83.34Q82.83,91.17 75.19,89.44Q75.1,81.47 82.84,83.34ZM65.13,94.29Q61.72,101.34 55.59,96.47Q58.96,89.25 65.13,94.29ZM44.41,96.47Q38.28,101.34 34.87,94.29Q41.04,89.25 44.41,96.47ZM24.81,89.44Q17.17,91.17 17.16,83.34Q24.9,81.47 24.81,89.44ZM10.19,74.6Q2.56,72.85 5.94,65.79Q13.73,67.47 10.19,74.6ZM3.46,54.89Q-2.66,50 3.46,45.11Q9.74,50 3.46,54.89ZM5.94,34.21Q2.56,27.15 10.19,25.4Q13.73,32.53 5.94,34.21ZM17.16,16.66Q17.17,8.83 24.81,10.56Q24.9,18.53 17.16,16.66ZM34.87,5.71Q38.28,-1.34 44.41,3.53Q41.04,10.75 34.87,5.71ZM55.59,3.53Q61.72,-1.34 65.13,5.71Q58.96,10.75 55.59,3.53ZM75.19,10.56Q82.83,8.83 82.84,16.66Q75.1,18.53 75.19,10.56ZM89.81,25.4Q97.44,27.15 94.06,34.21Q86.27,32.53 89.81,25.4Z",
        { fill: "#63380f", a: true },
      ),
      P(
        "M89.56,59.13Q94.04,67.79 84.8,70.91Q82.17,63 89.56,59.13ZM71.51,84.43Q68.56,93.72 59.82,89.39Q63.56,81.94 71.51,84.43ZM40.87,89.56Q32.21,94.04 29.09,84.8Q37,82.17 40.87,89.56ZM15.57,71.51Q6.28,68.56 10.61,59.82Q18.06,63.56 15.57,71.51ZM10.44,40.87Q5.96,32.21 15.2,29.09Q17.83,37 10.44,40.87ZM28.49,15.57Q31.44,6.28 40.18,10.61Q36.44,18.06 28.49,15.57ZM59.13,10.44Q67.79,5.96 70.91,15.2Q63,17.83 59.13,10.44ZM84.43,28.49Q93.72,31.44 89.39,40.18Q81.94,36.44 84.43,28.49Z",
        { fill: "#63380f", a: true },
      ),
      P(
        "M89.59,59.87Q93.02,67.38 85.33,70.4Q83.38,63.48 89.59,59.87ZM71.01,84.97Q68.13,92.71 60.56,89.41Q64.07,83.14 71.01,84.97ZM40.13,89.59Q32.62,93.02 29.6,85.33Q36.52,83.38 40.13,89.59ZM15.03,71.01Q7.29,68.13 10.59,60.56Q16.86,64.07 15.03,71.01ZM10.41,40.13Q6.98,32.62 14.67,29.6Q16.62,36.52 10.41,40.13ZM28.99,15.03Q31.87,7.29 39.44,10.59Q35.93,16.86 28.99,15.03ZM59.87,10.41Q67.38,6.98 70.4,14.67Q63.48,16.62 59.87,10.41ZM84.97,28.99Q92.71,31.87 89.41,39.44Q83.14,35.93 84.97,28.99Z",
        { fill: "#f0cb96", a: true },
      ),
      P(
        "M13.8,50A36.2,36.2 0 0 1 86.2,50A36.2,36.2 0 0 1 13.8,50ZM15.5,50A34.5,34.5 0 0 0 84.5,50A34.5,34.5 0 0 0 15.5,50Z",
        { fill: "#63380f" },
      ),
    ],
  },
  // ---------- crystal ----------
  {
    id: "gem-circlet",
    name: "Gem circlet",
    group: "Crystal",
    ring: true,
    anim: "spin",
    parts: [
      P(
        "M 50 1.5 A 48.5 48.5 0 1 1 50 98.5 A 48.5 48.5 0 1 1 50 1.5 Z M 50 10 A 40 40 0 1 0 50 90 A 40 40 0 1 0 50 10 Z",
        { z: "back", fill: "ink" },
      ),
      P(
        "M 50 8.4 A 41.6 41.6 0 1 1 50 91.6 A 41.6 41.6 0 1 1 50 8.4 Z M 50 13.6 A 36.4 36.4 0 1 0 50 86.4 A 36.4 36.4 0 1 0 50 13.6 Z",
        { fill: "c1" },
      ),
      P(
        "M 50 11.6 A 38.4 38.4 0 1 1 50 88.4 A 38.4 38.4 0 1 1 50 11.6 Z M 50 13.6 A 36.4 36.4 0 1 0 50 86.4 A 36.4 36.4 0 1 0 50 13.6 Z",
        { fill: "light" },
      ),
      P(
        "M 50 0.4 L 56.07 6.82 L 50 12.4 L 43.93 6.82 Z M 85.07 14.93 L 84.82 23.76 L 76.59 23.41 L 76.24 15.18 Z M 99.6 50 L 93.18 56.07 L 87.6 50 L 93.18 43.93 Z M 85.07 85.07 L 76.24 84.82 L 76.59 76.59 L 84.82 76.24 Z M 50 99.6 L 43.93 93.18 L 50 87.6 L 56.07 93.18 Z M 14.93 85.07 L 15.18 76.24 L 23.41 76.59 L 23.76 84.82 Z M 0.4 50 L 6.82 43.93 L 12.4 50 L 6.82 56.07 Z M 14.93 14.93 L 23.76 15.18 L 23.41 23.41 L 15.18 23.76 Z",
        { z: "back", fill: "ink", a: true },
      ),
      P(
        "M 50 1.2 L 55.31 6.72 L 50 11.6 L 44.69 6.72 Z M 84.51 15.49 L 84.36 23.16 L 77.15 22.85 L 76.84 15.64 Z M 98.8 50 L 93.28 55.31 L 88.4 50 L 93.28 44.69 Z M 84.51 84.51 L 76.84 84.36 L 77.15 77.15 L 84.36 76.84 Z M 50 98.8 L 44.69 93.28 L 50 88.4 L 55.31 93.28 Z M 15.49 84.51 L 15.64 76.84 L 22.85 77.15 L 23.16 84.36 Z M 1.2 50 L 6.72 44.69 L 11.6 50 L 6.72 55.31 Z M 15.49 15.49 L 23.16 15.64 L 22.85 22.85 L 15.64 23.16 Z",
        { fill: "c2", a: true },
      ),
      P(
        "M 50 1.2 L 44.69 6.72 L 50 11.6 Z M 84.51 15.49 L 76.84 15.64 L 77.15 22.85 Z M 98.8 50 L 93.28 44.69 L 88.4 50 Z M 84.51 84.51 L 84.36 76.84 L 77.15 77.15 Z M 50 98.8 L 55.31 93.28 L 50 88.4 Z M 15.49 84.51 L 23.16 84.36 L 22.85 77.15 Z M 1.2 50 L 6.72 55.31 L 11.6 50 Z M 15.49 15.49 L 15.64 23.16 L 22.85 22.85 Z",
        { fill: "light", a: true },
      ),
      P(
        "M 67.07 8.79 L 68.3 11.97 L 65.23 13.23 L 63.95 10.17 Z M 91.21 32.93 L 89.83 36.05 L 86.77 34.77 L 88.03 31.7 Z M 91.21 67.07 L 88.03 68.3 L 86.77 65.23 L 89.83 63.95 Z M 67.07 91.21 L 63.95 89.83 L 65.23 86.77 L 68.3 88.03 Z M 32.93 91.21 L 31.7 88.03 L 34.77 86.77 L 36.05 89.83 Z M 8.79 67.07 L 10.17 63.95 L 13.23 65.23 L 11.97 68.3 Z M 8.79 32.93 L 11.97 31.7 L 13.23 34.77 L 10.17 36.05 Z M 32.93 8.79 L 36.05 10.17 L 34.77 13.23 L 31.7 11.97 Z",
        { fill: "light", a: true },
      ),
    ],
  },
  {
    id: "frost-shards",
    name: "Frost shards",
    group: "Crystal",
    ring: true,
    anim: "spin",
    parts: [
      P(
        "M 44.77 15.39 L 50 0.4 L 55.23 15.39 Z M 70.77 21.83 L 85.07 14.93 L 78.17 29.23 Z M 84.61 44.77 L 99.6 50 L 84.61 55.23 Z M 78.17 70.77 L 85.07 85.07 L 70.77 78.17 Z M 55.23 84.61 L 50 99.6 L 44.77 84.61 Z M 29.23 78.17 L 14.93 85.07 L 21.83 70.77 Z M 15.39 55.23 L 0.4 50 L 15.39 44.77 Z M 21.83 29.23 L 14.93 14.93 L 29.23 21.83 Z",
        { z: "back", fill: "c1", a: true },
      ),
      P(
        "M 44.77 15.39 L 50 0.4 L 47.92 15.06 Z M 70.77 21.83 L 85.07 14.93 L 73.24 23.83 Z M 84.61 44.77 L 99.6 50 L 84.94 47.92 Z M 78.17 70.77 L 85.07 85.07 L 76.17 73.24 Z M 55.23 84.61 L 50 99.6 L 52.08 84.94 Z M 29.23 78.17 L 14.93 85.07 L 26.76 76.17 Z M 15.39 55.23 L 0.4 50 L 15.06 52.08 Z M 21.83 29.23 L 14.93 14.93 L 23.83 26.76 Z",
        { z: "back", fill: "light", a: true },
      ),
      P(
        "M 60.29 16.55 L 67.03 8.89 L 66.38 19.07 Z M 80.93 33.62 L 91.11 32.97 L 83.45 39.71 Z M 83.45 60.29 L 91.11 67.03 L 80.93 66.38 Z M 66.38 80.93 L 67.03 91.11 L 60.29 83.45 Z M 39.71 83.45 L 32.97 91.11 L 33.62 80.93 Z M 19.07 66.38 L 8.89 67.03 L 16.55 60.29 Z M 16.55 39.71 L 8.89 32.97 L 19.07 33.62 Z M 33.62 19.07 L 32.97 8.89 L 39.71 16.55 Z",
        { z: "back", fill: "c2", a: true },
      ),
      P(
        "M 64.67 11.18 L 67.03 8.89 L 67.08 12.18 Z M 87.82 32.92 L 91.11 32.97 L 88.82 35.33 Z M 88.82 64.67 L 91.11 67.03 L 87.82 67.08 Z M 67.08 87.82 L 67.03 91.11 L 64.67 88.82 Z M 35.33 88.82 L 32.97 91.11 L 32.92 87.82 Z M 12.18 67.08 L 8.89 67.03 L 11.18 64.67 Z M 11.18 35.33 L 8.89 32.97 L 12.18 32.92 Z M 32.92 12.18 L 32.97 8.89 L 35.33 11.18 Z",
        { z: "back", fill: "light", a: true },
      ),
      P(
        "M 50 12.2 A 37.8 37.8 0 1 1 50 87.8 A 37.8 37.8 0 1 1 50 12.2 Z M 50 16 A 34 34 0 1 0 50 84 A 34 34 0 1 0 50 16 Z",
        { fill: "light" },
      ),
      P(
        "M 57.41 12.73 L 58.84 15.2 L 56.59 16.85 L 55.15 14.47 Z M 71.11 18.4 L 71.48 21.23 L 68.78 21.9 L 68.36 19.15 Z M 81.6 28.89 L 80.85 31.64 L 78.1 31.22 L 78.77 28.52 Z M 87.27 42.59 L 85.53 44.85 L 83.15 43.41 L 84.8 41.16 Z M 87.27 57.41 L 84.8 58.84 L 83.15 56.59 L 85.53 55.15 Z M 81.6 71.11 L 78.77 71.48 L 78.1 68.78 L 80.85 68.36 Z M 71.11 81.6 L 68.36 80.85 L 68.78 78.1 L 71.48 78.77 Z M 57.41 87.27 L 55.15 85.53 L 56.59 83.15 L 58.84 84.8 Z M 42.59 87.27 L 41.16 84.8 L 43.41 83.15 L 44.85 85.53 Z M 28.89 81.6 L 28.52 78.77 L 31.22 78.1 L 31.64 80.85 Z M 18.4 71.11 L 19.15 68.36 L 21.9 68.78 L 21.23 71.48 Z M 12.73 57.41 L 14.47 55.15 L 16.85 56.59 L 15.2 58.84 Z M 12.73 42.59 L 15.2 41.16 L 16.85 43.41 L 14.47 44.85 Z M 18.4 28.89 L 21.23 28.52 L 21.9 31.22 L 19.15 31.64 Z M 28.89 18.4 L 31.64 19.15 L 31.22 21.9 L 28.52 21.23 Z M 42.59 12.73 L 44.85 14.47 L 43.41 16.85 L 41.16 15.2 Z",
        { fill: "c1" },
      ),
    ],
  },
  {
    id: "prism-halo",
    name: "Prismatic halo",
    group: "Crystal",
    ring: true,
    anim: "spin",
    parts: [
      P(
        "M 42.13 15.9 L 43.11 0.98 L 56.89 0.98 L 57.87 15.9 Z M 75.6 26.13 L 89.01 19.52 L 95.9 31.46 L 83.47 39.77 Z M 83.47 60.23 L 95.9 68.54 L 89.01 80.48 L 75.6 73.87 Z M 57.87 84.1 L 56.89 99.02 L 43.11 99.02 L 42.13 84.1 Z M 24.4 73.87 L 10.99 80.48 L 4.1 68.54 L 16.53 60.23 Z M 16.53 39.77 L 4.1 31.46 L 10.99 19.52 L 24.4 26.13 Z",
        { z: "back", fill: "c1", a: true },
      ),
      P(
        "M 60.23 16.53 L 67.78 8.12 L 77.38 13.66 L 73.87 24.4 Z M 84.1 42.13 L 95.16 44.45 L 95.16 55.55 L 84.1 57.87 Z M 73.87 75.6 L 77.38 86.34 L 67.78 91.88 L 60.23 83.47 Z M 39.77 83.47 L 32.22 91.88 L 22.62 86.34 L 26.13 75.6 Z M 15.9 57.87 L 4.84 55.55 L 4.84 44.45 L 15.9 42.13 Z M 26.13 24.4 L 22.62 13.66 L 32.22 8.12 L 39.77 16.53 Z",
        { z: "back", fill: "c2", a: true },
      ),
      P(
        "M 47.92 15.06 L 50 0.2 L 52.08 15.06 Z M 79.22 30.73 L 93.13 25.1 L 81.3 34.33 Z M 81.3 65.67 L 93.13 74.9 L 79.22 69.27 Z M 52.08 84.94 L 50 99.8 L 47.92 84.94 Z M 20.78 69.27 L 6.87 74.9 L 18.7 65.67 Z M 18.7 34.33 L 6.87 25.1 L 20.78 30.73 Z",
        { z: "back", fill: "light", a: true },
      ),
      P(
        "M 50 11.4 A 38.6 38.6 0 1 1 50 88.6 A 38.6 38.6 0 1 1 50 11.4 Z M 50 15.8 A 34.2 34.2 0 1 0 50 84.2 A 34.2 34.2 0 1 0 50 15.8 Z",
        { fill: "ink" },
      ),
      P(
        "M 50 13.8 A 36.2 36.2 0 1 1 50 86.2 A 36.2 36.2 0 1 1 50 13.8 Z M 50 15.8 A 34.2 34.2 0 1 0 50 84.2 A 34.2 34.2 0 1 0 50 15.8 Z",
        { fill: "light" },
      ),
      P(
        "M 60.25 11.75 L 61.65 14.57 L 59.06 16.19 L 57.63 13.49 Z M 78 22 L 77.81 25.14 L 74.75 25.25 L 74.86 22.19 Z M 88.25 39.75 L 86.51 42.37 L 83.81 40.94 L 85.43 38.35 Z M 88.25 60.25 L 85.43 61.65 L 83.81 59.06 L 86.51 57.63 Z M 78 78 L 74.86 77.81 L 74.75 74.75 L 77.81 74.86 Z M 60.25 88.25 L 57.63 86.51 L 59.06 83.81 L 61.65 85.43 Z M 39.75 88.25 L 38.35 85.43 L 40.94 83.81 L 42.37 86.51 Z M 22 78 L 22.19 74.86 L 25.25 74.75 L 25.14 77.81 Z M 11.75 60.25 L 13.49 57.63 L 16.19 59.06 L 14.57 61.65 Z M 11.75 39.75 L 14.57 38.35 L 16.19 40.94 L 13.49 42.37 Z M 22 22 L 25.14 22.19 L 25.25 25.25 L 22.19 25.14 Z M 39.75 11.75 L 42.37 13.49 L 40.94 16.19 L 38.35 14.57 Z",
        { fill: "light" },
      ),
    ],
  },
  {
    id: "diamond-lattice",
    name: "Diamond lattice",
    group: "Crystal",
    ring: true,
    anim: "spin",
    parts: [
      P(
        "M 50 2.2 A 47.8 47.8 0 1 1 50 97.8 A 47.8 47.8 0 1 1 50 2.2 Z M 50 4.8 A 45.2 45.2 0 1 0 50 95.2 A 45.2 45.2 0 1 0 50 4.8 Z",
        { z: "back", fill: "c1", a: true },
      ),
      P(
        "M 50 11.2 A 38.8 38.8 0 1 1 50 88.8 A 38.8 38.8 0 1 1 50 11.2 Z M 50 14 A 36 36 0 1 0 50 86 A 36 36 0 1 0 50 14 Z",
        { fill: "c1" },
      ),
      P(
        "M 50 2.3 L 55.28 8.23 L 50 13.5 L 44.72 8.23 Z M 73.85 8.69 L 75.45 16.47 L 68.25 18.39 L 66.31 11.19 Z M 91.31 26.15 L 88.81 33.69 L 81.61 31.75 L 83.53 24.55 Z M 97.7 50 L 91.77 55.28 L 86.5 50 L 91.77 44.72 Z M 91.31 73.85 L 83.53 75.45 L 81.61 68.25 L 88.81 66.31 Z M 73.85 91.31 L 66.31 88.81 L 68.25 81.61 L 75.45 83.53 Z M 50 97.7 L 44.72 91.77 L 50 86.5 L 55.28 91.77 Z M 26.15 91.31 L 24.55 83.53 L 31.75 81.61 L 33.69 88.81 Z M 8.69 73.85 L 11.19 66.31 L 18.39 68.25 L 16.47 75.45 Z M 2.3 50 L 8.23 44.72 L 13.5 50 L 8.23 55.28 Z M 8.69 26.15 L 16.47 24.55 L 18.39 31.75 L 11.19 33.69 Z M 26.15 8.69 L 33.69 11.19 L 31.75 18.39 L 24.55 16.47 Z",
        { fill: "c2", a: true },
      ),
      P(
        "M 50 5.2 L 52.57 7.98 L 50 10.6 L 47.43 7.98 Z M 72.4 11.2 L 73.24 14.89 L 69.7 15.88 L 68.78 12.32 Z M 88.8 27.6 L 87.68 31.22 L 84.12 30.3 L 85.11 26.76 Z M 94.8 50 L 92.02 52.57 L 89.4 50 L 92.02 47.43 Z M 88.8 72.4 L 85.11 73.24 L 84.12 69.7 L 87.68 68.78 Z M 72.4 88.8 L 68.78 87.68 L 69.7 84.12 L 73.24 85.11 Z M 50 94.8 L 47.43 92.02 L 50 89.4 L 52.57 92.02 Z M 27.6 88.8 L 26.76 85.11 L 30.3 84.12 L 31.22 87.68 Z M 11.2 72.4 L 12.32 68.78 L 15.88 69.7 L 14.89 73.24 Z M 5.2 50 L 7.98 47.43 L 10.6 50 L 7.98 52.57 Z M 11.2 27.6 L 14.89 26.76 L 15.88 30.3 L 12.32 31.22 Z M 27.6 11.2 L 31.22 12.32 L 30.3 15.88 L 26.76 14.89 Z",
        { fill: "light", a: true },
      ),
      P(
        "M 61.78 6.05 L 63.98 10.29 L 60.02 12.62 L 57.74 8.62 Z M 82.17 17.83 L 81.97 22.6 L 77.37 22.63 L 77.4 18.03 Z M 93.95 38.22 L 91.38 42.26 L 87.38 39.98 L 89.71 36.02 Z M 93.95 61.78 L 89.71 63.98 L 87.38 60.02 L 91.38 57.74 Z M 82.17 82.17 L 77.4 81.97 L 77.37 77.37 L 81.97 77.4 Z M 61.78 93.95 L 57.74 91.38 L 60.02 87.38 L 63.98 89.71 Z M 38.22 93.95 L 36.02 89.71 L 39.98 87.38 L 42.26 91.38 Z M 17.83 82.17 L 18.03 77.4 L 22.63 77.37 L 22.6 81.97 Z M 6.05 61.78 L 8.62 57.74 L 12.62 60.02 L 10.29 63.98 Z M 6.05 38.22 L 10.29 36.02 L 12.62 39.98 L 8.62 42.26 Z M 17.83 17.83 L 22.6 18.03 L 22.63 22.63 L 18.03 22.6 Z M 38.22 6.05 L 42.26 8.62 L 39.98 12.62 L 36.02 10.29 Z",
        { z: "back", fill: "c1", a: true },
      ),
    ],
  },
  // ---------- neon ----------
  {
    id: "neon-circuit-ring",
    name: "Circuit ring",
    group: "Neon",
    ring: true,
    anim: "spin",
    parts: [
      P(
        "M65.41 9.21A43.6 43.6 0 0 1 67.94 10.26L65.64 15.37A38 38 0 0 0 63.43 14.45ZM72.51 5.85L65.3 2.86L62.93 8.59L70.14 11.58ZM89.74 32.06A43.6 43.6 0 0 1 90.79 34.59L85.55 36.57A38 38 0 0 0 84.63 34.36ZM97.14 34.7L94.15 27.49L88.42 29.86L91.41 37.07ZM90.79 65.41A43.6 43.6 0 0 1 89.74 67.94L84.63 65.64A38 38 0 0 0 85.55 63.43ZM94.15 72.51L97.14 65.3L91.41 62.93L88.42 70.14ZM67.94 89.74A43.6 43.6 0 0 1 65.41 90.79L63.43 85.55A38 38 0 0 0 65.64 84.63ZM65.3 97.14L72.51 94.15L70.14 88.42L62.93 91.41ZM34.59 90.79A43.6 43.6 0 0 1 32.06 89.74L34.36 84.63A38 38 0 0 0 36.57 85.55ZM27.49 94.15L34.7 97.14L37.07 91.41L29.86 88.42ZM10.26 67.94A43.6 43.6 0 0 1 9.21 65.41L14.45 63.43A38 38 0 0 0 15.37 65.64ZM2.86 65.3L5.85 72.51L11.58 70.14L8.59 62.93ZM9.21 34.59A43.6 43.6 0 0 1 10.26 32.06L15.37 34.36A38 38 0 0 0 14.45 36.57ZM5.85 27.49L2.86 34.7L8.59 37.07L11.58 29.86ZM32.06 10.26A43.6 43.6 0 0 1 34.59 9.21L36.57 14.45A38 38 0 0 0 34.36 15.37ZM34.7 2.86L27.49 5.85L29.86 11.58L37.07 8.59Z",
        { z: "back", fill: "c2", a: true },
      ),
      P(
        "M70.28 7.31L65.85 5.47L65.16 7.13L69.59 8.97ZM94.53 34.15L92.69 29.72L91.03 30.41L92.87 34.84ZM92.69 70.28L94.53 65.85L92.87 65.16L91.03 69.59ZM65.85 94.53L70.28 92.69L69.59 91.03L65.16 92.87ZM29.72 92.69L34.15 94.53L34.84 92.87L30.41 91.03ZM5.47 65.85L7.31 70.28L8.97 69.59L7.13 65.16ZM7.31 29.72L5.47 34.15L7.13 34.84L8.97 30.41ZM34.15 5.47L29.72 7.31L30.41 8.97L34.84 7.13Z",
        { z: "back", fill: "light", a: true },
      ),
      P(
        "M48.98 8.41A41.6 41.6 0 0 1 51.02 8.41L50.93 12.01A38 38 0 0 0 49.07 12.01ZM47.6 7a2.4 2.4 0 1 0 4.8 0a2.4 2.4 0 1 0 -4.8 0ZM78.69 19.87A41.6 41.6 0 0 1 80.13 21.31L77.52 23.79A38 38 0 0 0 76.21 22.48ZM78.01 19.59a2.4 2.4 0 1 0 4.8 0a2.4 2.4 0 1 0 -4.8 0ZM91.59 48.98A41.6 41.6 0 0 1 91.59 51.02L87.99 50.93A38 38 0 0 0 87.99 49.07ZM90.6 50a2.4 2.4 0 1 0 4.8 0a2.4 2.4 0 1 0 -4.8 0ZM80.13 78.69A41.6 41.6 0 0 1 78.69 80.13L76.21 77.52A38 38 0 0 0 77.52 76.21ZM78.01 80.41a2.4 2.4 0 1 0 4.8 0a2.4 2.4 0 1 0 -4.8 0ZM51.02 91.59A41.6 41.6 0 0 1 48.98 91.59L49.07 87.99A38 38 0 0 0 50.93 87.99ZM47.6 93a2.4 2.4 0 1 0 4.8 0a2.4 2.4 0 1 0 -4.8 0ZM21.31 80.13A41.6 41.6 0 0 1 19.87 78.69L22.48 76.21A38 38 0 0 0 23.79 77.52ZM17.19 80.41a2.4 2.4 0 1 0 4.8 0a2.4 2.4 0 1 0 -4.8 0ZM8.41 51.02A41.6 41.6 0 0 1 8.41 48.98L12.01 49.07A38 38 0 0 0 12.01 50.93ZM4.6 50a2.4 2.4 0 1 0 4.8 0a2.4 2.4 0 1 0 -4.8 0ZM19.87 21.31A41.6 41.6 0 0 1 21.31 19.87L23.79 22.48A38 38 0 0 0 22.48 23.79ZM17.19 19.59a2.4 2.4 0 1 0 4.8 0a2.4 2.4 0 1 0 -4.8 0Z",
        { z: "back", fill: "c1", a: true },
      ),
      P(
        "M50 10.2A39.8 39.8 0 1 1 50 89.8A39.8 39.8 0 1 1 50 10.2ZM50 14.8A35.2 35.2 0 1 0 50 85.2A35.2 35.2 0 1 0 50 14.8Z",
        { fill: "c1" },
      ),
      P(
        "M50 12A38 38 0 1 1 50 88A38 38 0 1 1 50 12ZM50 12.8A37.2 37.2 0 1 0 50 87.2A37.2 37.2 0 1 0 50 12.8Z",
        { fill: "ink" },
      ),
      P(
        "M61.55 15.35a2.8 2.8 0 1 0 5.6 0a2.8 2.8 0 1 0 -5.6 0ZM81.85 35.65a2.8 2.8 0 1 0 5.6 0a2.8 2.8 0 1 0 -5.6 0ZM81.85 64.35a2.8 2.8 0 1 0 5.6 0a2.8 2.8 0 1 0 -5.6 0ZM61.55 84.65a2.8 2.8 0 1 0 5.6 0a2.8 2.8 0 1 0 -5.6 0ZM32.85 84.65a2.8 2.8 0 1 0 5.6 0a2.8 2.8 0 1 0 -5.6 0ZM12.55 64.35a2.8 2.8 0 1 0 5.6 0a2.8 2.8 0 1 0 -5.6 0ZM12.55 35.65a2.8 2.8 0 1 0 5.6 0a2.8 2.8 0 1 0 -5.6 0ZM32.85 15.35a2.8 2.8 0 1 0 5.6 0a2.8 2.8 0 1 0 -5.6 0Z",
        { fill: "light", a: true },
      ),
      P(
        "M63.15 15.35a1.2 1.2 0 1 0 2.4 0a1.2 1.2 0 1 0 -2.4 0ZM83.45 35.65a1.2 1.2 0 1 0 2.4 0a1.2 1.2 0 1 0 -2.4 0ZM83.45 64.35a1.2 1.2 0 1 0 2.4 0a1.2 1.2 0 1 0 -2.4 0ZM63.15 84.65a1.2 1.2 0 1 0 2.4 0a1.2 1.2 0 1 0 -2.4 0ZM34.45 84.65a1.2 1.2 0 1 0 2.4 0a1.2 1.2 0 1 0 -2.4 0ZM14.15 64.35a1.2 1.2 0 1 0 2.4 0a1.2 1.2 0 1 0 -2.4 0ZM14.15 35.65a1.2 1.2 0 1 0 2.4 0a1.2 1.2 0 1 0 -2.4 0ZM34.45 15.35a1.2 1.2 0 1 0 2.4 0a1.2 1.2 0 1 0 -2.4 0Z",
        { fill: "ink", a: true },
      ),
    ],
  },
  {
    id: "neon-holo-segments",
    name: "Holo segments",
    group: "Neon",
    ring: true,
    anim: "flicker",
    parts: [
      P(
        "M37.38 5.97A45.8 45.8 0 0 1 62.62 5.97L61.47 10.01A41.6 41.6 0 0 0 38.53 10.01ZM81.82 17.05A45.8 45.8 0 0 1 94.44 38.92L90.36 39.94A41.6 41.6 0 0 0 78.9 20.08ZM94.44 61.08A45.8 45.8 0 0 1 81.82 82.95L78.9 79.92A41.6 41.6 0 0 0 90.36 60.06ZM62.62 94.03A45.8 45.8 0 0 1 37.38 94.03L38.53 89.99A41.6 41.6 0 0 0 61.47 89.99ZM18.18 82.95A45.8 45.8 0 0 1 5.56 61.08L9.64 60.06A41.6 41.6 0 0 0 21.1 79.92ZM5.56 38.92A45.8 45.8 0 0 1 18.18 17.05L21.1 20.08A41.6 41.6 0 0 0 9.64 39.94Z",
        { z: "back", fill: "c2", a: true },
      ),
      P(
        "M74.05 8.34L69.08 10.55L69.65 15.96L74.62 13.75ZM98.1 50L93.7 46.8L89.3 50L93.7 53.2ZM74.05 91.66L74.62 86.25L69.65 84.04L69.08 89.45ZM25.95 91.66L30.92 89.45L30.35 84.04L25.38 86.25ZM1.9 50L6.3 53.2L10.7 50L6.3 46.8ZM25.95 8.34L25.38 13.75L30.35 15.96L30.92 10.55Z",
        { z: "back", fill: "c1", a: true },
      ),
      P(
        "M72.95 10.24L70.46 11.35L70.75 14.06L73.24 12.95ZM95.9 50L93.7 48.4L91.5 50L93.7 51.6ZM72.95 89.76L73.24 87.05L70.75 85.94L70.46 88.65ZM27.05 89.76L29.54 88.65L29.25 85.94L26.76 87.05ZM4.1 50L6.3 51.6L8.5 50L6.3 48.4ZM27.05 10.24L26.76 12.95L29.25 14.06L29.54 11.35Z",
        { z: "back", fill: "light", a: true },
      ),
      P(
        "M53.83 10.18A40 40 0 0 1 66.59 13.6L65.01 17.06A36.2 36.2 0 0 0 53.47 13.97ZM73.23 17.44A40 40 0 0 1 82.56 26.77L79.47 28.98A36.2 36.2 0 0 0 71.02 20.53ZM86.4 33.41A40 40 0 0 1 89.82 46.17L86.03 46.53A36.2 36.2 0 0 0 82.94 34.99ZM89.82 53.83A40 40 0 0 1 86.4 66.59L82.94 65.01A36.2 36.2 0 0 0 86.03 53.47ZM82.56 73.23A40 40 0 0 1 73.23 82.56L71.02 79.47A36.2 36.2 0 0 0 79.47 71.02ZM66.59 86.4A40 40 0 0 1 53.83 89.82L53.47 86.03A36.2 36.2 0 0 0 65.01 82.94ZM46.17 89.82A40 40 0 0 1 33.41 86.4L34.99 82.94A36.2 36.2 0 0 0 46.53 86.03ZM26.77 82.56A40 40 0 0 1 17.44 73.23L20.53 71.02A36.2 36.2 0 0 0 28.98 79.47ZM13.6 66.59A40 40 0 0 1 10.18 53.83L13.97 53.47A36.2 36.2 0 0 0 17.06 65.01ZM10.18 46.17A40 40 0 0 1 13.6 33.41L17.06 34.99A36.2 36.2 0 0 0 13.97 46.53ZM17.44 26.77A40 40 0 0 1 26.77 17.44L28.98 20.53A36.2 36.2 0 0 0 20.53 28.98ZM33.41 13.6A40 40 0 0 1 46.17 10.18L46.53 13.97A36.2 36.2 0 0 0 34.99 17.06Z",
        { fill: "c1" },
      ),
      P(
        "M53.58 12.77A37.4 37.4 0 0 1 65.51 15.97L65.01 17.06A36.2 36.2 0 0 0 53.47 13.97ZM71.72 19.55A37.4 37.4 0 0 1 80.45 28.28L79.47 28.98A36.2 36.2 0 0 0 71.02 20.53ZM84.03 34.49A37.4 37.4 0 0 1 87.23 46.42L86.03 46.53A36.2 36.2 0 0 0 82.94 34.99ZM87.23 53.58A37.4 37.4 0 0 1 84.03 65.51L82.94 65.01A36.2 36.2 0 0 0 86.03 53.47ZM80.45 71.72A37.4 37.4 0 0 1 71.72 80.45L71.02 79.47A36.2 36.2 0 0 0 79.47 71.02ZM65.51 84.03A37.4 37.4 0 0 1 53.58 87.23L53.47 86.03A36.2 36.2 0 0 0 65.01 82.94ZM46.42 87.23A37.4 37.4 0 0 1 34.49 84.03L34.99 82.94A36.2 36.2 0 0 0 46.53 86.03ZM28.28 80.45A37.4 37.4 0 0 1 19.55 71.72L20.53 71.02A36.2 36.2 0 0 0 28.98 79.47ZM15.97 65.51A37.4 37.4 0 0 1 12.77 53.58L13.97 53.47A36.2 36.2 0 0 0 17.06 65.01ZM12.77 46.42A37.4 37.4 0 0 1 15.97 34.49L17.06 34.99A36.2 36.2 0 0 0 13.97 46.53ZM19.55 28.28A37.4 37.4 0 0 1 28.28 19.55L28.98 20.53A36.2 36.2 0 0 0 20.53 28.98ZM34.49 15.97A37.4 37.4 0 0 1 46.42 12.77L46.53 13.97A36.2 36.2 0 0 0 34.99 17.06Z",
        { fill: "light" },
      ),
    ],
  },
  {
    id: "neon-target-lock",
    name: "Target lock",
    group: "Neon",
    ring: true,
    anim: "twitch",
    parts: [
      P(
        "M83.38 14.95A48.4 48.4 0 0 1 85.05 16.62L74.62 26.55A34 34 0 0 0 73.45 25.38ZM85.05 83.38A48.4 48.4 0 0 1 83.38 85.05L73.45 74.62A34 34 0 0 0 74.62 73.45ZM16.62 85.05A48.4 48.4 0 0 1 14.95 83.38L25.38 73.45A34 34 0 0 0 26.55 74.62ZM14.95 16.62A48.4 48.4 0 0 1 16.62 14.95L26.55 25.38A34 34 0 0 0 25.38 26.55Z",
        { z: "back", fill: "c2" },
      ),
      P(
        "M85.21 14.79L80.69 15.07L80.41 19.59L84.93 19.31ZM85.21 85.21L84.93 80.69L80.41 80.41L80.69 84.93ZM14.79 85.21L19.31 84.93L19.59 80.41L15.07 80.69ZM14.79 14.79L15.07 19.31L19.59 19.59L19.31 15.07Z",
        { z: "back", fill: "c2", a: true },
      ),
      P(
        "M27.56 15.45A41.2 41.2 0 0 1 72.44 15.45L70.37 18.63A37.4 37.4 0 0 0 29.63 18.63ZM84.55 27.56A41.2 41.2 0 0 1 84.55 72.44L81.37 70.37A37.4 37.4 0 0 0 81.37 29.63ZM72.44 84.55A41.2 41.2 0 0 1 27.56 84.55L29.63 81.37A37.4 37.4 0 0 0 70.37 81.37ZM15.45 72.44A41.2 41.2 0 0 1 15.45 27.56L18.63 29.63A37.4 37.4 0 0 0 18.63 70.37Z",
        { fill: "c1" },
      ),
      P(
        "M29.09 9.48A45.6 45.6 0 0 1 30.95 8.57L32.78 12.57A41.2 41.2 0 0 0 31.1 13.39ZM69.05 8.57A45.6 45.6 0 0 1 70.91 9.48L68.9 13.39A41.2 41.2 0 0 0 67.22 12.57ZM48.6 2.82A47.2 47.2 0 0 1 51.4 2.82L51.22 8.82A41.2 41.2 0 0 0 48.78 8.82ZM90.52 29.09A45.6 45.6 0 0 1 91.43 30.95L87.43 32.78A41.2 41.2 0 0 0 86.61 31.1ZM91.43 69.05A45.6 45.6 0 0 1 90.52 70.91L86.61 68.9A41.2 41.2 0 0 0 87.43 67.22ZM97.18 48.6A47.2 47.2 0 0 1 97.18 51.4L91.18 51.22A41.2 41.2 0 0 0 91.18 48.78ZM70.91 90.52A45.6 45.6 0 0 1 69.05 91.43L67.22 87.43A41.2 41.2 0 0 0 68.9 86.61ZM30.95 91.43A45.6 45.6 0 0 1 29.09 90.52L31.1 86.61A41.2 41.2 0 0 0 32.78 87.43ZM51.4 97.18A47.2 47.2 0 0 1 48.6 97.18L48.78 91.18A41.2 41.2 0 0 0 51.22 91.18ZM9.48 70.91A45.6 45.6 0 0 1 8.57 69.05L12.57 67.22A41.2 41.2 0 0 0 13.39 68.9ZM8.57 30.95A45.6 45.6 0 0 1 9.48 29.09L13.39 31.1A41.2 41.2 0 0 0 12.57 32.78ZM2.82 51.4A47.2 47.2 0 0 1 2.82 48.6L8.82 48.78A41.2 41.2 0 0 0 8.82 51.22Z",
        { fill: "c1" },
      ),
      P(
        "M29.52 18.47A37.6 37.6 0 0 1 70.48 18.47L69.72 19.64A36.2 36.2 0 0 0 30.28 19.64ZM81.53 29.52A37.6 37.6 0 0 1 81.53 70.48L80.36 69.72A36.2 36.2 0 0 0 80.36 30.28ZM70.48 81.53A37.6 37.6 0 0 1 29.52 81.53L30.28 80.36A36.2 36.2 0 0 0 69.72 80.36ZM18.47 70.48A37.6 37.6 0 0 1 18.47 29.52L19.64 30.28A36.2 36.2 0 0 0 19.64 69.72Z",
        { fill: "light" },
      ),
    ],
  },
  {
    id: "neon-hex-lattice",
    name: "Hex lattice",
    group: "Neon",
    ring: true,
    anim: "sway",
    parts: [
      P(
        "M50 1.4L92.09 25.7L92.09 74.3L50 98.6L7.91 74.3L7.91 25.7ZM11.55 27.8L11.55 72.2L50 94.4L88.45 72.2L88.45 27.8L50 5.6Z",
        { z: "back", fill: "c1" },
      ),
      P(
        "M50 0.4L46.4 4.4L50 8.4L53.6 4.4ZM92.95 25.2L87.69 24.08L86.03 29.2L91.29 30.32ZM92.95 74.8L91.29 69.68L86.03 70.8L87.69 75.92ZM50 99.6L53.6 95.6L50 91.6L46.4 95.6ZM7.05 74.8L12.31 75.92L13.97 70.8L8.71 69.68ZM7.05 25.2L8.71 30.32L13.97 29.2L12.31 24.08Z",
        { z: "back", fill: "c2", a: true },
      ),
      P(
        "M69.31 13.38A41.4 41.4 0 0 1 72.06 14.97L69.18 19.54A36 36 0 0 0 66.79 18.16ZM91.37 48.41A41.4 41.4 0 0 1 91.37 51.59L85.97 51.38A36 36 0 0 0 85.97 48.62ZM72.06 85.03A41.4 41.4 0 0 1 69.31 86.62L66.79 81.84A36 36 0 0 0 69.18 80.46ZM30.69 86.62A41.4 41.4 0 0 1 27.94 85.03L30.82 80.46A36 36 0 0 0 33.21 81.84ZM8.63 51.59A41.4 41.4 0 0 1 8.63 48.41L14.03 48.62A36 36 0 0 0 14.03 51.38ZM27.94 14.97A41.4 41.4 0 0 1 30.69 13.38L33.21 18.16A36 36 0 0 0 30.82 19.54Z",
        { z: "back", fill: "c1" },
      ),
      P(
        "M60.92 11.93A39.6 39.6 0 0 1 77.51 21.51L75.15 23.96A36.2 36.2 0 0 0 59.98 15.2ZM88.42 40.42A39.6 39.6 0 0 1 88.42 59.58L85.12 58.76A36.2 36.2 0 0 0 85.12 41.24ZM77.51 78.49A39.6 39.6 0 0 1 60.92 88.07L59.98 84.8A36.2 36.2 0 0 0 75.15 76.04ZM39.08 88.07A39.6 39.6 0 0 1 22.49 78.49L24.85 76.04A36.2 36.2 0 0 0 40.02 84.8ZM11.58 59.58A39.6 39.6 0 0 1 11.58 40.42L14.88 41.24A36.2 36.2 0 0 0 14.88 58.76ZM22.49 21.51A39.6 39.6 0 0 1 39.08 11.93L40.02 15.2A36.2 36.2 0 0 0 24.85 23.96Z",
        { fill: "c1" },
      ),
      P(
        "M60.31 14.05A37.4 37.4 0 0 1 75.98 23.1L75.15 23.96A36.2 36.2 0 0 0 59.98 15.2ZM86.29 40.95A37.4 37.4 0 0 1 86.29 59.05L85.12 58.76A36.2 36.2 0 0 0 85.12 41.24ZM75.98 76.9A37.4 37.4 0 0 1 60.31 85.95L59.98 84.8A36.2 36.2 0 0 0 75.15 76.04ZM39.69 85.95A37.4 37.4 0 0 1 24.02 76.9L24.85 76.04A36.2 36.2 0 0 0 40.02 84.8ZM13.71 59.05A37.4 37.4 0 0 1 13.71 40.95L14.88 41.24A36.2 36.2 0 0 0 14.88 58.76ZM24.02 23.1A37.4 37.4 0 0 1 39.69 14.05L40.02 15.2A36.2 36.2 0 0 0 24.85 23.96Z",
        { fill: "light" },
      ),
    ],
  },
  // ---------- celestial ----------
  {
    id: "sunburst-crown",
    name: "Sunburst",
    group: "Celestial",
    ring: true,
    anim: "spin",
    parts: [
      P(
        "M 82.82 53.45 L 91.05 61 L 80.15 63.42 Z M 76.7 69.4 L 80.05 80.05 L 69.4 76.7 Z M 63.42 80.15 L 61 91.05 L 53.45 82.82 Z M 46.55 82.82 L 39 91.05 L 36.58 80.15 Z M 30.6 76.7 L 19.95 80.05 L 23.3 69.4 Z M 19.85 63.42 L 8.95 61 L 17.18 53.45 Z M 17.18 46.55 L 8.95 39 L 19.85 36.58 Z M 23.3 30.6 L 19.95 19.95 L 30.6 23.3 Z M 36.58 19.85 L 39 8.95 L 46.55 17.18 Z M 53.45 17.18 L 61 8.95 L 63.42 19.85 Z M 69.4 23.3 L 80.05 19.95 L 76.7 30.6 Z M 80.15 36.58 L 91.05 39 L 82.82 46.55 Z",
        { z: "back", fill: "c2", a: true },
      ),
      P(
        "M 82.72 45.69 L 99 50 L 82.72 54.31 Z M 80.49 62.63 L 92.44 74.5 L 76.18 70.09 Z M 70.09 76.18 L 74.5 92.44 L 62.63 80.49 Z M 54.31 82.72 L 50 99 L 45.69 82.72 Z M 37.37 80.49 L 25.5 92.44 L 29.91 76.18 Z M 23.82 70.09 L 7.56 74.5 L 19.51 62.63 Z M 17.28 54.31 L 1 50 L 17.28 45.69 Z M 19.51 37.37 L 7.56 25.5 L 23.82 29.91 Z M 29.91 23.82 L 25.5 7.56 L 37.37 19.51 Z M 45.69 17.28 L 50 1 L 54.31 17.28 Z M 62.63 19.51 L 74.5 7.56 L 70.09 23.82 Z M 76.18 29.91 L 92.44 25.5 L 80.49 37.37 Z",
        { z: "back", fill: "c1", a: true },
      ),
      P(
        "M 89.4 50 A 39.4 39.4 0 1 1 10.6 50 A 39.4 39.4 0 1 1 89.4 50 Z M 84.2 50 A 34.2 34.2 0 1 0 15.8 50 A 34.2 34.2 0 1 0 84.2 50 Z",
        { fill: "c1" },
      ),
      P(
        "M 87.2 50 A 37.2 37.2 0 1 1 12.8 50 A 37.2 37.2 0 1 1 87.2 50 Z M 85.6 50 A 35.6 35.6 0 1 0 14.4 50 A 35.6 35.6 0 1 0 85.6 50 Z",
        { fill: "ink" },
      ),
      P(
        "M 86.99 58.01 A 1.9 1.9 0 1 1 86.98 58.01 Z M 77.08 75.18 A 1.9 1.9 0 1 1 77.07 75.18 Z M 59.91 85.09 A 1.9 1.9 0 1 1 59.9 85.09 Z M 40.09 85.09 A 1.9 1.9 0 1 1 40.08 85.09 Z M 22.92 75.18 A 1.9 1.9 0 1 1 22.91 75.18 Z M 13.01 58.01 A 1.9 1.9 0 1 1 13 58.01 Z M 13.01 38.19 A 1.9 1.9 0 1 1 13 38.19 Z M 22.92 21.02 A 1.9 1.9 0 1 1 22.91 21.02 Z M 40.09 11.11 A 1.9 1.9 0 1 1 40.08 11.11 Z M 59.91 11.11 A 1.9 1.9 0 1 1 59.9 11.11 Z M 77.08 21.02 A 1.9 1.9 0 1 1 77.07 21.02 Z M 86.99 38.19 A 1.9 1.9 0 1 1 86.98 38.19 Z",
        { fill: "light" },
      ),
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

// wornRing answers the one question the rest of the app asks about the `ring`
// flag: does this decoration draw a disc around the face? A theme pack may set
// --avatar-radius to a squircle, and app.css keeps the circle only for avatars
// marked `.ringed` — a round band around a rounded square reads as a bug.
//
// It is also the rule Avatar.svelte uses to decide whether a `frame` names a
// decoration at all. Exactly the twenty-one drawn rings ever travelled in that
// field, and they all carry this flag; a decoration WITHOUT it never did, so
// resolving one out of `frame` can only shadow a gradient ring of the same
// name — which is what happened to the comet, a name both libraries had.
export function wornRing(id) {
  return !!DECORATION_BY_ID[id]?.ring;
}

// ── COLOURWAYS ──────────────────────────────────────────────────────────────
//
// The wearer's choice of what colour to have their decoration IN. A piece is
// drawn from five steps derived from one base (AvatarDecoration.svelte mixes
// them in oklab), so a colourway only has to name that base and the ramp does
// the rest — which is exactly why this is a table of curated presets and not a
// hex field. A preset id is bounded, so it costs a handful of bytes on the
// wire and can be validated by validID like every other cosmetic id; an
// arbitrary hex is neither, and it also loses more often than it wins, because
// a base picked without regard for where the ramp will take it produces mud.
//
// Twelve, spanning the axes a wearer actually reaches for: three metals, black
// and white, two blues, a green, and the warm end from orange through red to
// pink, plus a purple. Each pairs a mid-lightness base with a lighter partner
// for `c2`, because a piece that uses both wants contrast between them, not a
// second copy of the first.
//
// Two choices are NOT colourways and so are not in this table:
//
//   ""     match my profile colour — today's behaviour, and the DEFAULT. An
//          absent field has to render exactly as it did before this existed,
//          or every profile in the world quietly changes appearance.
//   "own"  as designed — the piece's own `own: [c1, c2]` colourway, so a gold
//          crown stays gold. Only meaningful on a piece that declares one;
//          asked of a piece that does not, it falls back to the default.
export const CW_OWN = "own";

export const COLORWAYS = [
  { id: "gold", name: "Gold", c: ["#d4a12e", "#f3d98d"] },
  { id: "silver", name: "Silver", c: ["#a9b3c1", "#e4ebf4"] },
  { id: "copper", name: "Copper", c: ["#b8672f", "#e39a5e"] },
  { id: "obsidian", name: "Obsidian", c: ["#2e3340", "#5b6478"] },
  { id: "bone", name: "Bone", c: ["#e2dccb", "#fbf7ec"] },
  { id: "azure", name: "Azure", c: ["#3a7fd5", "#8ec5f7"] },
  { id: "frost", name: "Frost", c: ["#79c6e2", "#d5f0fb"] },
  { id: "jade", name: "Jade", c: ["#2ea27c", "#7fd9b6"] },
  { id: "ember", name: "Ember", c: ["#e0561f", "#f79a3a"] },
  { id: "crimson", name: "Crimson", c: ["#c22f43", "#ea7381"] },
  { id: "rose", name: "Rose", c: ["#d9648f", "#f5a8c2"] },
  { id: "amethyst", name: "Amethyst", c: ["#8a5cd8", "#c4a5f3"] },
];

export const COLORWAY_BY_ID = Object.fromEntries(COLORWAYS.map((c) => [c.id, c]));

// decorColors resolves what a decoration is actually painted in: the pair of
// base colours the ramp expands. Fails CLOSED in the only direction that is
// safe — an id this build does not know falls back to the wearer's profile
// colour, which is what every profile saved before this field existed already
// gets. It can never return nothing, and it never returns a colour a peer sent
// us, only one out of this file.
export function decorColors(id, cw, c1 = "", c2 = "") {
  const w = COLORWAY_BY_ID[cw];
  if (w) return [w.c[0], w.c[1] || w.c[0]];
  if (cw === CW_OWN) {
    const own = DECORATION_BY_ID[id]?.own;
    if (own) return [own[0], own[1] || own[0]];
  }
  return [c1, c2];
}
