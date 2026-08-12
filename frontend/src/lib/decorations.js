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
// `t0`/`t1` take a SLICE of the spiral rather than all of it, which is what
// lets one coil be drawn twice at two depths: the stretch that has swung
// behind the head in the back layer, the stretch still in front of it over the
// top. Without that a horn is painted flat onto the face — the shape says it
// wraps and the compositing says it does not, and the eye believes the
// compositing every time.
function coil(a, rBase, R, turns, w0, dir = 1, steps = 34, t0 = 0, t1 = 1) {
  const f = frame(a, rBase);
  const cx = dir * R;
  const phi0 = dir > 0 ? 180 : 0;
  const out = [];
  const inn = [];
  for (let i = 0; i <= steps; i++) {
    const t = t0 + ((t1 - t0) * i) / steps;
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
// A gaussian, for the things that are more glow than edge. Costly enough that
// the painter drops it below 40px, so nothing structural may depend on it.
const BLUR = (id, std) => ({ t: "blur", id, std });

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

// ── MATERIALS ───────────────────────────────────────────────────────────────
//
// The helpers above can shade anything; these decide what it is MADE of. They
// exist because the first pass at lighting produced three beautifully lit
// pieces and fifty-eight flat ones, and the reason was economics: hand-authoring
// a stop list per piece is enough work that it only ever got done for the
// pieces someone sat down with. A material is one call.
//
// Every one is a cross-section along the RADIUS — dark at the inner edge,
// something happening in the middle, dark again at the outer edge — because
// that is the profile of a real object seen from the front, and because the
// whole library is polar so the same primitive shades a band's thickness and a
// spoke's length without knowing which it is on.
//
// What separates them is where the light goes and how fast it falls away, and
// that is genuinely all metal, bone and stone are to the eye at 36 pixels:
//
//   metal   a HARD specular line about a third across, and a second dimmer
//           one near the outer edge. Two highlights is the tell — one is
//           plastic. Gold, steel, bronze, chrome.
//   bone    dark where it roots, pale where it ends, no specular at all.
//           Antler, horn, tooth, claw, driftwood.
//   stone   low contrast throughout and a dark crease. Anything matte reads as
//           stone if you refuse it a highlight.
//   gem     an object-fitted radial with the hot spot up and left, so it is a
//           SPHERE rather than a disc, and a deep core so it has an inside.
//   glass   inverted: dim in the middle, bright at BOTH rims. That is what
//           makes a shard look empty rather than solid, and it is why crystal
//           drawn with the metal ramp looked like painted tin.
//   glow    centre-bright and falling to fully transparent, for the things
//           that are light rather than objects — haloes, auras, signal.
//   cloth   shaded along the LIGHT axis instead of the radius, because fabric
//           has no cross-section worth speaking of; what it has is one lit
//           face and one in shadow.
//   leaf    dark root, lit tip, and deliberately no glint — foliage that
//           specular-highlights reads as plastic fruit.
//   ember   the one that crosses the two wearer colours: a c2 core running out
//           into c1 and then into nothing, for fire and anything molten.
//
// All of them are written in ramp tokens, never hex, so the wearer's colour —
// or their chosen colourway — flows through every material in the library.
const metal = (id, r0, r1) =>
  section(id, r0, r1, [
    [0, "c1-deep"],
    [0.12, "c1-shade"],
    [0.32, "c1-glint"],
    [0.44, "c1-lit"],
    [0.66, "c1"],
    [0.82, "c1-lit"],
    [0.93, "c1-shade"],
    [1, "c1-deep"],
  ]);
const bone = (id, r0, r1) =>
  section(id, r0, r1, [
    [0, "c1-deep"],
    [0.16, "c1-shade"],
    [0.46, "c1"],
    [0.76, "c1-lit"],
    [1, "c1-glint"],
  ]);
const stone = (id, r0, r1) =>
  section(id, r0, r1, [
    [0, "c1-deep"],
    [0.24, "c1-shade"],
    [0.52, "c1"],
    [0.78, "c1-shade"],
    [1, "c1-deep"],
  ]);
// fx/fy put the hot spot up and to the left, which is the one light the whole
// library agrees on. A centred hot spot is a disc however you colour it.
const gemG = (id, c = "c2") =>
  RGB(
    id,
    [
      [0, `${c}-glint`],
      [0.2, `${c}-lit`],
      [0.5, c],
      [0.8, `${c}-shade`],
      [1, `${c}-deep`],
    ],
    { fx: 0.34, fy: 0.3 },
  );
// Bright at BOTH rims and dimmer between them, which is what makes a shard
// look empty rather than solid. The alphas stay high on purpose: the first
// pass ran them down to 0.24 in the middle and every crystal piece in the
// library disappeared against a dark backdrop. Glass is transparent to what is
// BEHIND it, not to the eye — the transparency has to be a lightness gradient,
// not an opacity one, or there is nothing left to see.
const glass = (id, r0, r1) =>
  section(id, r0, r1, [
    [0, "c1-glint"],
    [0.2, "c1-lit", 0.86],
    [0.5, "c1", 0.72],
    [0.8, "c1-lit", 0.9],
    [1, "c1-glint"],
  ]);
const glow = (id, c = "c1") =>
  RGB(id, [
    [0, `${c}-glint`, 0.95],
    [0.35, `${c}-lit`, 0.6],
    [0.7, c, 0.22],
    [1, c, 0],
  ]);
const cloth = (id, r = 46) =>
  axis(id, r, [
    [0, "c1-lit"],
    [0.3, "c1"],
    [0.68, "c1-shade"],
    [1, "c1-deep"],
  ]);
const leafG = (id, r0, r1) =>
  section(id, r0, r1, [
    [0, "c1-deep"],
    [0.2, "c1-shade"],
    [0.55, "c1"],
    [0.85, "c1-lit"],
    [1, "c1-lit"],
  ]);
const ember = (id, r0, r1) =>
  section(id, r0, r1, [
    [0, "c2-glint"],
    [0.24, "c2-lit"],
    [0.5, "c1"],
    [0.78, "c1-shade"],
    [1, "c1-deep", 0.82],
  ]);

// A worn piece needs three things to stop being a sticker, and only the middle
// one is the material: a shadow where it touches, the material itself, and one
// sun over the top of it. `dressed` is those three as a single call, so the
// pieces below say what they are made of rather than restating the recipe.
//
// Order matters and is the order light works in — the cast shadow is under
// everything, the sheen is over everything.
const dressed = (mat, { castK = 1, sheenR = 46, sheenK = 0.85 } = {}) => [
  castG("cast", castK),
  ...mat,
  sheen("sun", sheenR, sheenK),
];

// ── rings ───────────────────────────────────────────────────────────────────
//
// A drawn ring's whole problem is that it is a circle, and twenty-one circles
// are one circle. Sprinkling different ornaments around the same flat stroke
// does not fix it — at a member row's twenty pixels the ornaments are two
// pixels each and every ring in the library is the same blue hoop.
//
// What actually separates them is PROFILE: how the band is shaped across its
// thickness. A stroke has no profile at all, which is why the whole set read as
// wireframe. `banded` gives one — an annulus painted through a material, so it
// is dark at both edges and lit across the middle, and a second pass laying the
// library's one sun over the top.
//
// The sun pass deliberately does NOT take the animation. A rotating band under
// a FIXED highlight is what metal turning in a room looks like; carry the
// highlight round with the band and the whole thing reads as a decal on a
// spinning plate, which is exactly what the first version looked like.
const banded = (r, w, opts = {}) => [
  P(annulus(r, w), { fill: "@band", ...opts }),
  P(annulus(r, w), { fill: "@sun" }),
];

// The dark seam just inside a band, where it turns away from the viewer. One
// hairline of shadow is the cheapest possible way to stop a ring reading as a
// sticker laid on the wallpaper, and it works at every size.
const seat = (r, w = 1) => P(annulus(r, w), { fill: "#04060a", op: 0.34 });

// teeth: a run of tapered spikes standing off a band, as ONE path. A gear's
// teeth, a crown's points, a sun's rays. They never move independently, so
// twenty-four of them have no business being twenty-four elements.
const teeth = (count, r, len, w, from = 0) =>
  one(
    Array.from({ length: count }, (_, i) => from + (i * 360) / count),
    (a) => ray(a, r, r + len, w, w * 0.34),
  );

// ovalAt: an elliptical annulus centred anywhere, at any rotation — which is
// the shape a chain link actually is, and the one shape the helpers above
// could not make (`ovalBand` can only ever centre on the vertical axis).
// Sampled rather than arced, because the offset curve of an ellipse is not
// another ellipse: stroking one would not thicken it evenly.
function ovalAt(cx, cy, rx, ry, w, rot = 0, steps = 26) {
  const cr = Math.cos(rot * RAD);
  const sr = Math.sin(rot * RAD);
  const at = (t, k) => {
    const x = (rx + k) * Math.cos(t * RAD);
    const y = (ry + k) * Math.sin(t * RAD);
    return `${n(cx + x * cr - y * sr)} ${n(cy + x * sr + y * cr)}`;
  };
  const out = [];
  const inn = [];
  for (let i = 0; i <= steps; i++) {
    const t = (360 * i) / steps;
    out.push(at(t, w / 2));
    inn.push(at(t, -w / 2));
  }
  // Outer loop forward, inner loop reversed: opposite winding, so the middle
  // of the link cancels out and stays a hole. A link you cannot see through is
  // a bead.
  return `M${out.join("L")}L${inn.reverse().join("L")}Z`;
}

// links: a chain, as interlocking ovals that ALTERNATE between lying along the
// band and standing across it. That alternation is the entire difference
// between a chain and a string of beads — the chainmail this replaces was a
// ring of identical circles, and everyone read it as pearls.
const links = (count, r, R, w) =>
  Array.from({ length: count }, (_, i) => {
    const a = (i * 360) / count;
    const [x, y] = pt(a, r);
    const flat = i % 2 === 0;
    return P(ovalAt(x, y, R, R * 0.5, w, a + (flat ? 0 : 90)), {
      fill: flat ? "@band" : "@band2",
    });
  });

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
    own: ["#5e9152", "#f0a9c8"],
    defs: dressed([
      leafG("band", 36.4, 38.8),
      leafG("leaf", 37.6, 44.2),
      gemG("petal", "c2"),
    ]),
    parts: [
      cast(-92, 92, 9),
      P(arcBand(37.6, -88, 88, 2.4), { fill: "@band" }),
      ...row([-78, -52, -26, 26, 52, 78], (a, i) => leaf(a, 37.6, 6.5, 2.4, i < 3 ? -2 : 2), {
        fill: "@leaf",
        anim: "wag",
        a: true,
      }),
      ...row([-62, -34, 0, 34, 62], (a, i) => flower(a, 39.6, i === 2 ? 6 : 5), {
        fill: "@petal",
        a: true,
      }),
      ...row([-62, -34, 0, 34, 62], (a, i) => blob(a, 39.6, i === 2 ? 2.2 : 1.8), {
        fill: "c1-lit",
        a: true,
      }),
      P(arcBand(37.6, -88, 88, 2.4), { fill: "@sun" }),
    ],
  },
  {
    id: "cat-circlet",
    name: "Cat circlet",
    group: "Arch",
    anim: "twitch",
    tilt: true,
    own: ["#6f5f8a", "#efe6fb"],
    defs: dressed([
      metal("band", 36.1, 38.7),
      bone("ear", 36.5, 56),
      RGB(
        "inner",
        [
          [0, "c2-deep"],
          [0.3, "c2-shade"],
          [0.62, "c2"],
          [1, "c2-lit"],
        ],
        { fx: 0.5, fy: 0.92 },
      ),
      gemG("bell", "c2"),
    ]),
    parts: [
      cast(-80, 80, 9),
      P(arcBand(37.4, -76, 76, 2.6), { fill: "@band" }),
      ...mirror(34, (a, s) => spoke(a, 37, 18, 26, 8 * s), { fill: "@ear", a: true, pv: 37 }),
      ...mirror(34, (a, s) => spoke(a, 38, 11.5, 15, 8 * s), { fill: "@inner", a: true, pv: 37 }),
      P(blob(0, 38.8, 3), { fill: "@bell", anim: "pulse", a: true }),
      P(blob(0, 39.7, 1), { fill: "c2-deep" }),
      P(arcBand(37.4, -76, 76, 2.6), { fill: "@sun" }),
    ],
  },
  {
    id: "antler-circlet",
    name: "Antler circlet",
    group: "Arch",
    anim: "pulse",
    tilt: true,
    own: ["#c2a476", "#6e8f5a"],
    // Antler was four hardcoded browns, which is why it was the one piece in
    // the library nobody could recolour. The tines are one material now, shaded
    // along their own length, so a wearer's colour arrives as bone rather than
    // as four flat swatches.
    defs: dressed([bone("antler", 36, 62), stone("band", 36.2, 38.6), gemG("bead", "c2")]),
    parts: [
      cast(-84, 84, 8),
      P(arcBand(37.4, -80, 80, 2.4), { fill: "@band" }),
      ...mirror(26, (a, s) => horn(a, 36.6, 40, 26, 6.2, s), { fill: "@antler" }),
      ...mirror(14, (a, s) => horn(a, 37.4, 26, 26, 3.4, s), { fill: "@antler" }),
      ...mirror(40, (a, s) => horn(a, 37.2, 24, 40, 3.8, s), { fill: "@antler" }),
      ...mirror(54, (a, s) => horn(a, 37.2, 18, 44, 3.2, s), { fill: "@antler" }),
      ...row([-68, -4, 4, 68], (a) => blob(a, 37.4, 2.2), { fill: "@bead", a: true }),
      ...row([-33, 33], (a) => blob(a, 37.4, 1.5), { fill: "c2-lit", a: true }),
      P(arcBand(37.4, -80, 80, 2.4), { fill: "@sun" }),
    ],
  },
  {
    id: "star-circlet",
    name: "Star circlet",
    group: "Arch",
    anim: "pulse",
    tilt: true,
    own: ["#d8c069", "#fff4c8"],
    defs: dressed([
      metal("band", 37.4, 39),
      gemG("star", "c2"),
      glow("halo", "c2"),
      BLUR("haze", 2.4),
    ]),
    parts: [
      // The glow goes behind the whole arch, so the stars sit in light rather
      // than each carrying a halo of its own — six small glows read as six
      // smudges, one big one reads as a night sky.
      P(crescent(-92, 92, 44, 10), { z: "back", fill: "@halo", filter: "haze", op: 0.7, a: true }),
      cast(-90, 90, 8),
      P(arcBand(38.2, -86, 86, 1.6), { fill: "@band" }),
      ...row([-74, -50, -26, 26, 50, 74], (a, i) =>
        star(a, 38.2 + (i % 2 ? 5.4 : 3.4), i % 2 ? 4.4 : 3.2, i % 2 ? 1.8 : 1.3, 5), {
        fill: "@star",
        a: true,
      }),
      P(star(0, 42.6, 5.2, 2.1, 5), { fill: "@star", a: true }),
      P(star(0, 42.6, 2.6, 1, 5), { fill: "c2-glint", a: true, o: 4 }),
    ],
  },
  // ---------- creature ----------
  {
    id: "cat-ears",
    name: "Cat ears",
    group: "Creature",
    anim: "twitch",
    tilt: true,
    own: ["#5d6b8a", "#f2c4d4"],
    defs: [
      castG("cast", 1),
      section("band", 34.9, 39.5, [
        [0, "c1-deep"],
        [0.22, "c1-shade"],
        [0.56, "c1"],
        [0.84, "c1-lit"],
        [1, "c1-shade"],
      ]),
      // The ear darkens toward its tip the way a cat's does, but stops short
      // of the black a fox gets — that black is the fox's whole identity and
      // borrowing it made the two pieces the same animal.
      section("ear", 36.5, 57, [
        [0, "c1-deep"],
        [0.14, "c1-shade"],
        [0.4, "c1"],
        [0.72, "c1"],
        [1, "c1-deep"],
      ]),
      RGB(
        "inner",
        [
          [0, "c2-deep"],
          [0.28, "c2-shade"],
          [0.6, "c2"],
          [1, "c2-lit"],
        ],
        { fx: 0.5, fy: 0.94 },
      ),
      TURB("fur", "0.44 0.64", 1, { oct: 2, seed: 3 }),
      sheen("sun", 52, 0.6),
    ],
    parts: [
      cast(-84, 84, 10),
      P(arcBand(37.2, -80, 80, 4.6), { fill: "@band" }),
      P(arcBand(39, -80, 80, 1), { fill: "c1-glint", op: 0.7 }),
      ...mirror(37, (a, s) => spoke(a, 36.8, 19, 30, 9 * s), {
        fill: "@ear",
        filter: "fur",
        a: true,
        pv: 37,
      }),
      ...mirror(37, (a, s) => spoke(a, 36.8, 19, 30, 9 * s), { fill: "@sun", a: true, pv: 37 }),
      ...mirror(37, (a, s) => spoke(a, 37.8, 12, 18, 9 * s), { fill: "@inner", a: true, pv: 37 }),
      ...row([-58, -20, 20, 58], (a, i) => leaf(a, 39.2, 4.6, 1.7, i < 2 ? -1.6 : 1.6), {
        fill: "c2-lit",
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
    own: ["#e7e0d6", "#f3b3c1"],
    defs: [
      castG("cast", 1),
      section("band", 35.7, 38.7, [
        [0, "c1-deep"],
        [0.24, "c1-shade"],
        [0.58, "c1"],
        [0.86, "c1-lit"],
        [1, "c1-shade"],
      ]),
      section("ear", 34.5, 54, [
        [0, "c1-deep"],
        [0.16, "c1-shade"],
        [0.44, "c1"],
        [0.78, "c1-lit"],
        [1, "c1-glint"],
      ]),
      RGB(
        "inner",
        [
          [0, "c2-deep"],
          [0.3, "c2-shade"],
          [0.64, "c2"],
          [1, "c2-lit"],
        ],
        { fx: 0.5, fy: 0.9 },
      ),
      TURB("fuzz", "0.5 0.7", 0.8, { oct: 2, seed: 5 }),
      sheen("sun", 50, 0.6),
    ],
    parts: [
      cast(-76, 76, 9),
      P(arcBand(37.2, -72, 72, 3), { fill: "@band" }),
      ...mirror(18, (a, s) => leaf(a, 34.5, 17, 5.6, 6 * s), {
        fill: "@ear",
        filter: "fuzz",
        a: true,
        pv: 36,
      }),
      ...mirror(18, (a, s) => leaf(a, 34.5, 17, 5.6, 6 * s), { fill: "@sun", a: true, pv: 36 }),
      ...mirror(18, (a, s) => leaf(a, 35.9, 12.8, 3.2, 5 * s), { fill: "@inner", a: true, pv: 36 }),
      P(poly(-46, 39, [[-0.9, 0], [-4.6, 2.6], [-4.6, -2.6]]), {
        fill: "@inner",
        anim: "pulse",
        a: true,
      }),
      P(poly(-46, 39, [[0.9, 0], [4.6, 2.6], [4.6, -2.6]]), {
        fill: "@inner",
        anim: "pulse",
        a: true,
        o: 2,
      }),
      P(blob(-46, 39, 1.5), { fill: "c2-glint", anim: "pulse", a: true, o: 1 }),
      ...row([-52, 52], (a) => blob(a, 37.2, 1.5), { fill: "c1-glint", anim: "pulse", a: true }),
    ],
  },
  {
    id: "ram-horns",
    name: "Ram horns",
    group: "Creature",
    anim: "pulse",
    tilt: true,
    own: ["#c8ad82", "#8a6f4a"],
    // Redrawn. The horns were a full turn and an eighth of `coil`, which winds
    // the tip all the way back into the middle of its own spiral — from the
    // front that is not a horn, it is a brass door-knocker, and two of them
    // framed the face like hardware. A ram's horn seen from the front makes
    // rather less than one turn: it leaves the brow, sweeps out and DOWN past
    // the ear, and stops with the tip pointing forward. Three quarters of a
    // turn, and the silhouette is right.
    //
    // Three nested passes give it a cross-section, since a coil cannot take a
    // radial `section` the way a spoke can — the shape doubles back on itself,
    // so distance-from-the-head stops meaning distance-along-the-horn. Widest
    // and darkest underneath, the material over it, a light edge on top.
    defs: [
      castG("cast", 1),
      metal("band", 35.4, 39),
      section("horn", 36, 52, [
        [0, "c1-deep"],
        [0.2, "c1-shade"],
        [0.55, "c1"],
        [0.85, "c1-lit"],
        [1, "c1-glint"],
      ]),
      gemG("stone", "c2"),
      sheen("sun", 48, 0.7),
    ],
    parts: [
      cast(-90, 90, 10),
      // The horn is drawn TWICE at two depths, and the second half of the
      // spiral is the half that goes behind. A ram's horn sweeps out from the
      // brow, past the ear, and curls back IN toward the jaw — and that last
      // stretch is on the far side of the head. Drawn in one pass at one
      // depth it lies flat across the face instead, which reads as a sticker
      // of a horn rather than a horn being worn, however well it is shaded.
      //
      // The two slices overlap by a tenth of a turn so the seam between them
      // falls inside the avatar's silhouette, where nothing can show through.
      ...mirror(66, (a, s) => coil(a, 35.8, 10, 0.52, 11, s, 34, 0.45, 1), {
        z: "back",
        fill: "c1-deep",
      }),
      ...mirror(66, (a, s) => coil(a, 36.2, 9.7, 0.52, 9, s, 34, 0.45, 1), {
        z: "back",
        fill: "@horn",
      }),
      ...mirror(66, (a, s) => coil(a, 35.8, 10, 0.52, 11, s, 34, 0, 0.55), { fill: "c1-deep" }),
      ...mirror(66, (a, s) => coil(a, 36.2, 9.7, 0.52, 9, s, 34, 0, 0.55), { fill: "@horn" }),
      // The ridges a ram's horn is banded with, as a narrow pass riding the
      // upper edge of the main coil. Without them the horn is a smooth tusk.
      ...mirror(66, (a, s) => coil(a, 38.4, 9, 0.5, 2.4, s, 34, 0, 0.55), {
        fill: "c1-glint",
        op: 0.5,
      }),
      P(arcBand(37.2, -86, 86, 3.6), { fill: "@band" }),
      P(arcBand(38.8, -86, 86, 0.9), { fill: "c1-glint", op: 0.6 }),
      ...row([-30, 0, 30], (a, i) => gem(a, 39.4, i === 1 ? 5 : 3.6, i === 1 ? 7 : 5), {
        fill: "@stone",
        a: true,
      }),
      P(arcBand(37.2, -86, 86, 3.6), { fill: "@sun" }),
    ],
  },
  {
    id: "devil-horns",
    name: "Devil horns",
    group: "Creature",
    anim: "flicker",
    tilt: true,
    own: ["#c1362f", "#ff9a3d"],
    defs: [
      castG("cast", 1.15),
      stone("band", 35.5, 38.9),
      section("horn", 36, 58, [
        [0, "c1-deep"],
        [0.18, "c1-shade"],
        [0.5, "c1"],
        [0.82, "c1-lit"],
        [1, "c1-glint"],
      ]),
      gemG("coal", "c2"),
      glow("heat", "c2"),
      BLUR("haze", 2.2),
      sheen("sun", 50, 0.8),
    ],
    parts: [
      // Heat coming off the horns, behind everything. The piece flickers, so
      // the glow flickering with it is what sells it as hot rather than red.
      P(crescent(-80, 80, 44, 11), { z: "back", fill: "@heat", filter: "haze", op: 0.55, a: true }),
      cast(-78, 78, 11),
      P(arcBand(37.2, -74, 74, 3.4), { fill: "@band" }),
      ...mirror(32, (a, s) => horn(a, 36.4, 24, 54, 9.6, s), { fill: "@horn" }),
      ...mirror(32, (a, s) => horn(a, 37.6, 23, 52, 3.8, s), { fill: "c1-lit", op: 0.75 }),
      ...row([-58, -44, 44, 58], (a) => blob(a, 37.2, 2), { fill: "@coal", a: true }),
      P(gem(0, 39, 4.6, 6.2), { fill: "@coal", anim: "pulse", a: true }),
      P(arcBand(37.2, -74, 74, 3.4), { fill: "@sun" }),
    ],
  },
  {
    id: "shark",
    name: "Shark bite",
    group: "Creature",
    anim: "chomp",
    own: ["#4d7995", "#eef5fa"],
    defs: [
      castG("cast", 1.2),
      // The jaw, shaded across its depth: dark where it disappears behind the
      // head, lit along the ridge. A shark drawn in two flat blues is a pair
      // of brackets.
      section("jaw", 36, 47, [
        [0, "c1-deep"],
        [0.22, "c1-shade"],
        [0.52, "c1"],
        [0.8, "c1-lit"],
        [1, "c1-shade"],
      ]),
      // The gum line is its own material, warmer and paler than the hide.
      section("gum", 36, 41, [
        [0, "c2-shade"],
        [0.4, "c2"],
        [1, "c2-lit"],
      ]),
      sheen("sun", 48, 0.7),
    ],
    parts: [
      cast(-90, 90, 12, "@cast"),
      P(arcBand(42, 112, 248, 9), { z: "back", fill: "@jaw", a: true }),
      P(arcBand(38.2, 112, 248, 3.4), { fill: "@gum", a: true }),
      P(one([124, 138, 152, 166, 180, 194, 208, 222, 236], (a) => spoke(a, 37.2, -6.5, 8)), {
        fill: "#ffffff",
        a: true,
      }),
      P(arcBand(42, -68, 68, 8), { z: "back", fill: "@jaw", a: true, o: "r" }),
      P(arcBand(38.2, -68, 68, 3.2), { fill: "@gum", a: true, o: "r" }),
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
    own: ["#4e7c3a", "#c9a441"],
    defs: dressed([
      leafG("leaf", 37, 47),
      section("under", 36, 43.5, [
        [0, "c1-deep"],
        [0.4, "c1-shade"],
        [1, "c1"],
      ]),
      section("stem", 36.6, 38.2, [
        [0, "c1-deep"],
        [0.5, "c1-shade"],
        [1, "c1-deep"],
      ]),
      gemG("berry", "c2"),
    ]),
    parts: [
      cast(28, 152, 8),
      cast(-152, -28, 8),
      P(arcBand(37.4, -152, -26, 1.6), { fill: "@stem" }),
      P(arcBand(37.4, 26, 152, 1.6), { fill: "@stem" }),
      ...row([-140, -124, -108, -92, -76, -60, -44], (a) => leaf(a, 37.6, 9, 4.6, 5), {
        fill: "@leaf",
        a: true,
      }),
      ...row([140, 124, 108, 92, 76, 60, 44], (a) => leaf(a, 37.6, 9, 4.6, -5), {
        fill: "@leaf",
        a: true,
      }),
      // The under-layer is the SAME green a step darker rather than the second
      // colour: a laurel is one plant. The old version painted it in c2, so
      // every wreath came out two-tone and read as plastic.
      ...row([-132, -116, -100, -84, -68, -52], (a) => leaf(a, 36.4, 6.5, 3.4, -4), {
        fill: "@under",
        a: true,
      }),
      ...row([132, 116, 100, 84, 68, 52], (a) => leaf(a, 36.4, 6.5, 3.4, 4), {
        fill: "@under",
        a: true,
      }),
      ...row([-150, -34, 34, 150], (a) => blob(a, 38.4, 1.9), {
        fill: "@berry",
        anim: "pulse",
        a: true,
      }),
      P(gem(180, 38.6, 5, 7), { fill: "@berry", anim: "pulse", a: true }),
    ],
  },
  {
    id: "tiara",
    name: "Tiara",
    group: "Regalia",
    anim: "dangle",
    tilt: true,
    own: ["#dfe6ef", "#7fd8f5"],
    // Redrawn. Every setting was a `ringAt` — a hollow circle floating at its
    // own radius above the band, touching nothing. That is why it read as
    // wireframe hovering over the head rather than as a tiara: a tiara's
    // stones are held UP by something, and the something was missing.
    //
    // Now each stone sits on a tapered riser rooted in the band, and the
    // risers are drawn before the band so the band closes over their feet.
    // The whole thing is one connected object, which is the only reason the
    // dangling drops below it make sense.
    defs: dressed([metal("band", 36, 39.6), metal("riser", 36.5, 44), gemG("stone", "c2")]),
    parts: [
      cast(-80, 80, 9),
      // Risers first, tallest in the middle, so the arch reads as a crown line
      // rather than a row of equal spikes.
      P(ray(0, 36.8, 43.4, 3.4, 1.8), { fill: "@riser" }),
      ...mirror(28, (a) => ray(a, 36.8, 41.8, 2.8, 1.5), { fill: "@riser" }),
      ...mirror(52, (a) => ray(a, 36.8, 40.4, 2.2, 1.2), { fill: "@riser" }),
      P(arcBand(37.1, -76, 76, 2.6), { fill: "@band" }),
      P(arcBand(38.7, -76, 76, 0.8), { fill: "c1-glint", op: 0.8 }),
      P(gem(0, 44.4, 4.4, 6), { fill: "@stone", anim: "pulse", a: true }),
      ...mirror(28, (a) => blob(a, 42.4, 2.1), { fill: "@stone", anim: "pulse", a: true }),
      ...mirror(52, (a) => blob(a, 40.9, 1.5), { fill: "@stone", anim: "pulse", a: true }),
      ...row([-66, -42, 42, 66], (a, i) => drop(a, 36.4, 3.5 + (i % 2) * 2.4, 0.9, 1.8), {
        fill: "c1-glint",
        a: true,
      }),
      P(arcBand(37.1, -76, 76, 2.6), { fill: "@sun" }),
    ],
  },
  {
    id: "diadem",
    name: "Jewelled diadem",
    group: "Regalia",
    anim: "shimmer",
    tilt: true,
    own: ["#d8c169", "#6fd8f5"],
    defs: dressed([metal("band", 35.6, 39), gemG("stone", "c2"), glow("fire", "c2"), BLUR("haze", 2)]),
    parts: [
      // The centre stone throws light onto the band around it. One glow behind
      // the whole crown line, not one per stone — six small hazes is fog.
      P(crescent(-70, 70, 45, 10), { z: "back", fill: "@fire", filter: "haze", op: 0.6, a: true }),
      cast(-94, 94, 9),
      P(arcBand(37.3, -90, 90, 3.4), { fill: "@band" }),
      P(arcBand(38.8, -90, 90, 0.9), { fill: "c1-glint", a: true }),
      P(gem(0, 42.4, 8, 10.4), { fill: "@stone", anim: "pulse", a: true }),
      P(gem(0, 42.4, 3.6, 5), { fill: "c2-glint", anim: "pulse", a: true, o: 2 }),
      ...row([-58, -30, 30, 58], (a, i) => gem(a, 40.4, 4.6 - (i % 2), 6.6 - (i % 2)), {
        fill: "@stone",
        anim: "pulse",
        a: true,
      }),
      ...row([-76, -44, -14, 14, 44, 76], (a) => blob(a, 37.3, 1.4), { fill: "c1-glint", a: true }),
      P(arcBand(37.3, -90, 90, 3.4), { fill: "@sun" }),
    ],
  },
  {
    id: "royal-collar",
    name: "Royal collar",
    group: "Regalia",
    anim: "wave",
    own: ["#8d2547", "#f0e6d2"],
    defs: [
      castG("cast", 1),
      // Cloth is shaded along the LIGHT, not along the radius: a collar has no
      // cross-section to speak of, it has a lit side and a shadowed one.
      cloth("velvet", 47),
      section("trim", 43.4, 47, [
        [0, "c2-shade"],
        [0.35, "c2"],
        [0.7, "c2-lit"],
        [1, "c2-glint"],
      ]),
      gemG("stone", "c2"),
      sheen("sun", 48, 0.5),
    ],
    parts: [
      P(arcBand(41.4, 116, 244, 9.6), { z: "back", fill: "@velvet", a: true }),
      P(arcBand(45.4, 116, 244, 1.6), { z: "back", fill: "@trim", a: true }),
      P(one([122, 136, 150, 164, 178, 192, 206, 220, 234], (a) => blob(a, 45.4, 2)), {
        z: "back",
        fill: "@trim",
        a: true,
      }),
      ...mirror(118, (a, s) => spoke(a, 41.4, 8, 13, -5 * s), {
        z: "back",
        fill: "@velvet",
        a: true,
      }),
      P(arcBand(37.4, 116, 244, 1.4), { z: "back", fill: "c1-deep", a: true }),
      cast(120, 240, 8),
      P(gem(180, 40, 5.6, 7.6), { fill: "@stone", anim: "pulse", a: true }),
      P(blob(180, 40, 1.6), { fill: "c2-glint", anim: "pulse", a: true, o: 2 }),
    ],
  },
  {
    id: "velvet-mantle",
    name: "Velvet mantle",
    group: "Regalia",
    anim: "sway",
    own: ["#7d1439", "#f4efe2"],
    defs: [
      castG("cast", 1),
      cloth("velvet", 48),
      // Ermine: the pale trim, and the black flecks that are the only reason
      // anyone reads it as ermine rather than as a white hem.
      section("ermine", 42.6, 46.2, [
        [0, "c2-shade"],
        [0.4, "c2"],
        [1, "c2-glint"],
      ]),
      gemG("clasp", "c2"),
      sheen("sun", 48, 0.45),
    ],
    parts: [
      P(taperBand(40.4, 100, 180, 6, 11), { z: "back", fill: "@velvet", a: true }),
      P(taperBand(40.4, 260, 180, 6, 11), { z: "back", fill: "@velvet", a: true, o: "r" }),
      P(taperBand(37.6, 108, 180, 1.4, 2.4), { z: "back", fill: "c1-deep", a: true }),
      P(taperBand(37.6, 252, 180, 1.4, 2.4), { z: "back", fill: "c1-deep", a: true, o: "r" }),
      P(arcBand(44.4, 104, 256, 2.4), { z: "back", fill: "@ermine", a: true }),
      P(one([120, 144, 168, 192, 216, 240], (a) => blob(a, 44.4, 1.2)), {
        z: "back",
        fill: "#33373f",
        a: true,
      }),
      ...mirror(102, (a, s) => spoke(a, 40.4, 9, 14, -6 * s), {
        z: "back",
        fill: "@velvet",
        a: true,
      }),
      cast(110, 250, 9),
      P(gem(180, 39.4, 5.4, 7), { fill: "@clasp", anim: "pulse", a: true }),
    ],
  },
  // ---------- ethereal ----------
  {
    id: "halo-gold",
    name: "Halo",
    group: "Ethereal",
    anim: "float",
    own: ["#d99b1f", "#ffe9a8"],
    defs: [
      // A halo is seen edge-on, so its gradient runs across the SHORT axis of
      // the ellipse — a radial about the head would shade it by how far round
      // the ring you were looking, which is not how a disc of light works.
      LG("gold", 50, 7, 50, 20, [
        [0, "c1-lit"],
        [0.34, "c1-glint"],
        [0.62, "c1"],
        [1, "c1-shade"],
      ]),
      glow("shine", "c2"),
      BLUR("haze", 2.6),
    ],
    parts: [
      P(ovalBand(13.2, 25, 7.4, 0, 360, 7), { fill: "@shine", filter: "haze", op: 0.7, a: true }),
      P(ovalBand(13.5, 23, 6.2, 0, 360, 4.4), { fill: "@gold", a: true }),
      P(ovalBand(13.1, 23, 6.2, 0, 360, 2.4), { fill: "c1-glint", a: true }),
      P(ovalBand(12.4, 22.4, 5.8, 190, 350, 1), { fill: "c2-glint", a: true }),
      ...row([-62, -38, 38, 62], (a, i) => star(a, 42 + (i % 2) * 3, 3.2, 1.2, 4), {
        fill: "c2-lit",
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
    own: ["#c8a96a", "#7fb2e8"],
    defs: [
      // The near half of the ring is lit and the far half is not — that
      // difference is the only thing that says the head is INSIDE it rather
      // than in front of a painted oval.
      LG("near", 50, 38, 50, 68, [
        [0, "c1-lit"],
        [0.4, "c1"],
        [1, "c1-shade"],
      ]),
      LG("far", 50, 38, 50, 68, [
        [0, "c2-deep"],
        [0.5, "c2-shade"],
        [1, "c2"],
      ]),
      gemG("moon", "c2"),
    ],
    parts: [
      P(ovalBand(53, 42.6, 14.5, 180, 360, 4.6), { z: "back", fill: "@far" }),
      P(ovalBand(53, 42.6, 14.5, 0, 180, 4.6), { fill: "@near" }),
      P(ovalBand(53, 42.6, 14.5, 14, 166, 1.4), { fill: "c1-glint", op: 0.8 }),
      P(ovalBand(53, 42.6, 14.5, 194, 346, 1.4), { z: "back", fill: "c2-shade" }),
      P(blobXY(7.4, 53, 4), { z: "back", fill: "@moon", a: true }),
      P(blobXY(92.6, 53, 4), { fill: "@moon", a: true, o: 4 }),
      P(blobXY(91.8, 51.6, 1.4), { fill: "c2-glint", a: true, o: 4 }),
    ],
  },
  {
    id: "aura-crown",
    name: "Aura crown",
    group: "Ethereal",
    anim: "flicker",
    own: ["#4fd6e8", "#eafcff"],
    defs: [
      // An aura is light, so it gets `glow` rather than a cross-section, and
      // the tongues are drawn OVER a blurred copy of themselves — the blur is
      // what stops a row of pointed shapes reading as a paper crown.
      leafG("tongue", 36.8, 52),
      glow("aura", "c1"),
      BLUR("haze", 2.4),
    ],
    parts: [
      P(crescent(-100, 100, 46, 12), { z: "back", fill: "@aura", filter: "haze", op: 0.65, a: true }),
      P(arcBand(36.8, -98, 98, 1.8), { fill: "c1-glint", a: true }),
      ...row(
        [-90, -74, -58, -42, -26, -10, 10, 26, 42, 58, 74, 90],
        (a, i) => leaf(a, 36.8, [5, 8, 12, 15, 11, 8, 8, 11, 15, 12, 8, 5][i], 3, 0),
        { fill: "@tongue", a: true },
      ),
      ...row(
        [-74, -42, -26, 26, 42, 74],
        (a, i) => leaf(a, 36.8, [4, 9, 6, 6, 9, 4][i], 1.8, 0),
        { fill: "c2-glint", a: true },
      ),
      ...row([-58, 0, 58], (a) => blob(a, 45, 1.7), { fill: "c2-glint", anim: "drift", a: true }),
    ],
  },
  {
    id: "spectral-shroud",
    name: "Spectral shroud",
    group: "Ethereal",
    anim: "shimmer",
    own: ["#6f7fa8", "#cfe4ff"],
    // A ghost, not a hooded cloak. The difference is entirely in the EDGE: a
    // shroud drawn with clean vector outlines is a dressing gown, however
    // pale you paint it. The turbulence below re-seeds itself in hard steps,
    // so the hem is a different shape from one instant to the next rather
    // than a loop — which is the one thing that reads as not-solid.
    defs: [
      section("veil", 36, 50, [
        [0, "c1", 0.14],
        [0.3, "c1", 0.42],
        [0.66, "c1-lit", 0.6],
        [1, "c1-lit", 0.05],
      ]),
      glow("wisp", "c2"),
      TURB("fray", "0.03 0.06", 5.5, { oct: 3, seed: 2, flick: "2;9;4;13;7", dur: 2.6, blur: 0.4 }),
      BLUR("haze", 2.2),
    ],
    parts: [
      P(taperBand(39.6, 0, -152, 5, 13, 44.6), {
        z: "back",
        fill: "@veil",
        filter: "fray",
        a: true,
      }),
      P(taperBand(39.6, 0, 152, 5, 13, 44.6), {
        z: "back",
        fill: "@veil",
        filter: "fray",
        a: true,
        o: "r",
      }),
      P(taperBand(37.2, 0, -152, 1.2, 2.6, 38.6), { z: "back", fill: "c2-glint", op: 0.5, a: true }),
      P(taperBand(37.2, 0, 152, 1.2, 2.6, 38.6), {
        z: "back",
        fill: "c2-glint",
        op: 0.5,
        a: true,
        o: "r",
      }),
      ...mirror(150, (a, s) => leaf(a, 44.6, 9.5, 5, 4 * s), {
        z: "back",
        fill: "@veil",
        filter: "fray",
        anim: "dangle",
        a: true,
      }),
      ...mirror(134, (a, s) => leaf(a, 44.4, 8, 4, 3 * s), {
        z: "back",
        fill: "@veil",
        filter: "fray",
        anim: "dangle",
        a: true,
        o: 3,
      }),
      ...mirror(118, (a, s) => leaf(a, 42.8, 5, 3, 2 * s), {
        z: "back",
        fill: "@veil",
        anim: "dangle",
        a: true,
        o: 5,
      }),
      ...row([-104, -86, 86, 104], (a) => blob(a, 47, 1.8), {
        z: "back",
        fill: "@wisp",
        filter: "haze",
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
    own: ["#f0c96a", "#fff6d8"],
    defs: [
      // Rays fade out along their own length rather than ending on a hard
      // point. A ray with a cut end is a spike; a ray that runs out of light
      // is a ray.
      section("ray", 38.5, 47, [
        [0, "c1-glint", 0.9],
        [0.4, "c1-lit", 0.6],
        [1, "c1", 0],
      ]),
      glow("core", "c1"),
      BLUR("haze", 3),
    ],
    parts: [
      P(annulus(42, 12), { z: "back", fill: "@core", filter: "haze", op: 0.55, a: true }),
      ...Array.from({ length: 16 }, (_, i) =>
        P(ray(i * 22.5, 38.5, i % 2 ? 46.8 : 43, 3.2, 0.6), {
          z: "back",
          fill: "@ray",
          anim: "whirl",
          a: true,
        }),
      ),
      P(annulus(39.8, 2.2), { z: "back", fill: "c1-lit", a: true }),
      P(annulus(43.6, 0.9), { z: "back", fill: "c2-glint", a: true, o: 3 }),
      ...row([-40, 0, 40], (a) => star(a, 45.4, 3, 1.1, 4), {
        z: "back",
        fill: "c2-glint",
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
    own: ["#54a8d8", "#e8faff"],
    defs: [
      castG("cast", 0.9),
      metal("band", 35.6, 38),
      // Ice, with the glass ramp: bright at both edges of a shard and dimmer
      // through the middle, which is what makes it look like something you
      // could see through rather than painted tin.
      glass("ice", 36.8, 51),
      section("spike", 36.8, 51, [
        [0, "c1-shade"],
        [0.35, "c1-lit"],
        [0.72, "c2-glint"],
        [1, "c2-glint"],
      ]),
      glow("chill", "c2"),
      BLUR("haze", 2.4),
      sheen("sun", 48, 0.5),
    ],
    parts: [
      P(crescent(-96, 96, 46, 11), { z: "back", fill: "@chill", filter: "haze", op: 0.5, a: true }),
      cast(-94, 94, 9),
      P(arcBand(36.8, -90, 90, 2.4), { fill: "@band" }),
      P(
        one([-76, -58, -40, -22, 0, 22, 40, 58, 76], (a, i) =>
          ray(a, 36.8, 36.8 + [6, 9, 14, 11, 8.5, 11, 14, 9, 6][i], 5.4, 0),
        ),
        { fill: "c1-deep" },
      ),
      ...row(
        [-76, -58, -40, -22, 0, 22, 40, 58, 76],
        (a, i) => ray(a, 36.8, 36.8 + [4.8, 7.4, 12, 9, 7, 9, 12, 7.4, 4.8][i], 3.4, 0),
        { fill: "@spike", a: true },
      ),
      ...row(
        [-40, 0, 40],
        (a, i) => ray(a, 36.8, 36.8 + [8, 4.5, 8][i], 1.4, 0),
        { fill: "c2-glint", a: true },
      ),
      ...mirror(49, (a, s) => ray(a + 4 * s, 40, 45.5, 2.6, 0), { fill: "@ice", a: true }),
      ...row([-66, -12, 12, 66], (a) => star(a, 44, 2.6, 1, 6), {
        fill: "c2-glint",
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
    own: ["#414c5e", "#ffe066"],
    // Redrawn. The bolts struck INWARD, at the wearer's face, from a ring of
    // evenly spaced grey lumps — which read as a cog with cartoon lightning
    // glued on, and put the brightest thing in the piece over the eyes. A
    // storm is a dark cloud with light coming OUT of it. The cloud is now a
    // ragged mass rather than a row of circles, and the bolts fall away from
    // it, outward and down, where lightning goes.
    defs: [
      stone("cloud", 36.8, 47),
      glow("flash", "c2"),
      BLUR("haze", 2.6),
      TURB("churn", "0.05 0.08", 3.4, { oct: 3, seed: 4 }),
    ],
    parts: [
      P(crescent(-112, 112, 45.6, 12), { z: "back", fill: "@flash", filter: "haze", op: 0.45, a: true }),
      // One ragged mass, displaced by noise, rather than twelve blobs on a
      // band: a cloud has no repeating unit.
      P(crescent(-108, 108, 44.2, 9.2), { z: "back", fill: "@cloud", filter: "churn" }),
      P(crescent(-104, 104, 42.2, 5.2), { z: "back", fill: "c1-lit", op: 0.45, filter: "churn" }),
      P(arcBand(37.6, -104, 104, 1.6), { z: "back", fill: "c1-deep" }),
      // Bolts falling OUT of the cloud, down the sides of the head. Positive
      // lengths, so they reach away from the face rather than across it.
      ...row([-84, -52, 52, 84], (a, i) => bolt(a, 40.6, i % 3 ? 8.4 : 6.6, i % 3 ? 9 : 7), {
        z: "back",
        fill: "@flash",
        filter: "haze",
        a: true,
      }),
      ...row([-84, -52, 52, 84], (a, i) => bolt(a, 40.6, i % 3 ? 7.8 : 6, i % 3 ? 5 : 4), {
        z: "back",
        fill: "c2-glint",
        a: true,
      }),
      ...row([-30, 30], (a) => bolt(a, 40.2, 6, 4), { z: "back", fill: "c2-lit", a: true, hi: true }),
    ],
  },
  {
    id: "tide-crest",
    name: "Tide crest",
    group: "Elemental",
    anim: "wave",
    own: ["#2f7fa8", "#d8f4ff"],
    defs: [
      // Water is deep where it is thick and pale where it thins to foam, so
      // the ramp runs the OTHER way from a metal: darkest at the inner edge,
      // brightest at the crest.
      section("water", 36, 48, [
        [0, "c1-deep"],
        [0.28, "c1-shade"],
        [0.62, "c1"],
        [0.88, "c1-lit"],
        [1, "c2-glint"],
      ]),
      gemG("foam", "c2"),
      TURB("churn", "0.07 0.1", 2.2, { oct: 2, seed: 6 }),
    ],
    parts: [
      P(taperBand(41, 208, 378, 11, 2.4), { z: "back", fill: "@water", filter: "churn", a: true }),
      P(taperBand(43.6, 230, 372, 2.2, 1), { z: "back", fill: "c2-glint", a: true }),
      P(coil(22, 39, 5.4, 0.9, 5.4, 1), { fill: "@water", a: true }),
      P(coil(22, 39.5, 5, 0.8, 2, 1), { fill: "c2-glint", a: true, o: 2 }),
      ...row([250, 285, 320, 350], (a) => blob(a, 45.6, 2.2), {
        z: "back",
        fill: "@foam",
        a: true,
      }),
      ...row([196, 214, 232], (a, i) => blob(a, 43 + i * 2, 1.6), {
        z: "back",
        fill: "@foam",
        anim: "drift",
        a: true,
      }),
      P(gem(180, 39.4, 4.4, 6), { z: "back", fill: "@foam", anim: "pulse", a: true }),
    ],
  },
  {
    id: "stone-mantle",
    name: "Stone mantle",
    group: "Elemental",
    anim: "float",
    own: ["#7b8590", "#e07a2f"],
    // Redrawn. Three passes of `rubble` at three flat greys, each one smaller
    // and paler than the last, drawn concentrically — which produced a ring of
    // grey blobs with lighter grey blobs on top, and no rock anywhere. Stone
    // does not read from its outline; it reads from a FACE catching the light
    // and a face turned away. So each boulder is now two shapes, not three: a
    // dark body and a lit cap offset up and to the left, where the library's
    // one sun is. The molten seams between them are what make it volcanic
    // rather than gravel.
    defs: [
      stone("rock", 36, 50),
      glow("magma", "c2"),
      BLUR("haze", 2),
      TURB("grit", "0.14 0.18", 1.6, { oct: 3, seed: 9 }),
    ],
    parts: [
      P(crescent(56, 304, 46, 12), { z: "back", fill: "@magma", filter: "haze", op: 0.32, a: true }),
      ...row(
        [-148, -118, -88, -58, 58, 88, 118, 148],
        (a, i) => rubble(a, 42.4, 8 - (i % 2) * 1.6, i + 1),
        { z: "back", fill: "c1-deep", filter: "grit", a: true },
      ),
      ...row(
        [-148, -118, -88, -58, 58, 88, 118, 148],
        (a, i) => rubble(a, 43.6, 5.4 - (i % 2) * 1.1, i + 1),
        { z: "back", fill: "@rock", filter: "grit", a: true },
      ),
      // The molten seam, between the body and its lit cap.
      ...row(
        [-148, -118, -88, -58, 58, 88, 118, 148],
        (a, i) => rubble(a, 44.4, 3 - (i % 2) * 0.6, i + 1),
        { z: "back", fill: "@magma", anim: "flicker", a: true },
      ),
      ...row([-170, 170, 180], (a, i) => rubble(a, 39.5 + i, 2.4 - i * 0.4, i + 4), {
        z: "back",
        fill: "@rock",
        anim: "drift",
        a: true,
      }),
      ...row([-32, 32], (a) => blob(a, 38.6, 2.4), { fill: "@magma", anim: "pulse", a: true }),
    ],
  },
  {
    id: "ember-rise",
    name: "Ember rise",
    group: "Elemental",
    anim: "drift",
    own: ["#c9401a", "#ffd24a"],
    defs: [
      // The bed the embers sit in: charred at the bottom, still hot at the
      // top. `ember` is the one material that crosses both wearer colours,
      // which is what a coal bed actually looks like.
      ember("bed", 36.6, 43),
      glow("heat", "c2"),
      BLUR("haze", 2.4),
      TURB("char", "0.1 0.14", 1.8, { oct: 2, seed: 11 }),
    ],
    parts: [
      P(crescent(92, 268, 46, 12), { z: "back", fill: "@heat", filter: "haze", op: 0.45, a: true }),
      P(arcBand(39.4, 96, 264, 5.6), { z: "back", fill: "@bed", filter: "char" }),
      P(arcBand(37.2, 96, 264, 1.4), { z: "back", fill: "c1-lit", op: 0.7 }),
      ...row(
        [104, 122, 140, 158, 180, 202, 220, 238, 256],
        (a, i) => blob(a, 40.4, 2.6 - (i % 3) * 0.5),
        { z: "back", fill: "c1-lit", anim: "flicker", a: true },
      ),
      ...row(
        [112, 132, 152, 172, 190, 210, 230, 250],
        (a, i) => blob(a, 40.4, 1.4 - (i % 3) * 0.3),
        { z: "back", fill: "c2-glint", anim: "flicker", a: true },
      ),
      ...row(
        [104, 120, 136, 224, 240, 256],
        (a, i) => blob(a, 44.5 + (i % 3) * 2, 1.8 - (i % 3) * 0.4),
        { z: "back", fill: "c2-lit", a: true },
      ),
      ...row([112, 134, 226, 248], (a, i) => blob(a, 48 + (i % 2) * 2.5, 1.3), {
        z: "back",
        fill: "c2-glint",
        a: true,
      }),
    ],
  },
  {
    id: "gale-swirl",
    name: "Gale swirl",
    group: "Elemental",
    anim: "whirl",
    own: ["#8fb8c8", "#e8f6fb"],
    defs: [
      // Wind is only visible where it is dense. Each gust fades out along its
      // own length rather than ending, which is the difference between moving
      // air and a broken hoop.
      section("gust", 36.8, 48, [
        [0, "c1", 0.1],
        [0.3, "c1-lit", 0.8],
        [0.66, "c1-glint"],
        [1, "c1-lit", 0.25],
      ]),
      glow("mote", "c2"),
      BLUR("haze", 1.8),
    ],
    parts: [
      P(taperBand(46.4, -50, 170, 5, 0.6, 37.4), { z: "back", fill: "@gust", a: true }),
      P(taperBand(38.4, 150, 380, 4.4, 0.5, 46.8), { z: "back", fill: "@gust", a: true }),
      P(taperBand(43.4, 70, 260, 2.6, 0.4, 37), { z: "back", fill: "c1-glint", op: 0.6, a: true, o: "r" }),
      P(taperBand(37.2, 250, 430, 2.4, 0.4, 44.4), { z: "back", fill: "c1-glint", op: 0.6, a: true, o: "r" }),
      ...row([-100, 20, 140, 260], (a) => leaf(a, 45, 5, 2, 2), {
        z: "back",
        fill: "@mote",
        filter: "haze",
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
    own: ["#c9a86a", "#7fc8e8"],
    defs: [
      // The far half of the ring passes BEHIND the planet and is in its
      // shadow; the near half catches the sun. Painting both halves the same
      // is what made this read as a flat decal of Saturn rather than a body
      // with a ring around it.
      LG("near", 50, 34, 50, 66, [
        [0, "c1-glint"],
        [0.35, "c1-lit"],
        [1, "c1-shade"],
      ]),
      LG("far", 50, 34, 50, 66, [
        [0, "c1-shade"],
        [0.6, "c1-deep"],
        [1, "c1-deep"],
      ]),
      gemG("moon", "c2"),
    ],
    parts: [
      P(ovalBand(51, 43, 13, 178, 362, 6.4, -14), { z: "back", fill: "@far" }),
      P(ovalBand(51, 43, 13, 178, 362, 2, -14), { z: "back", fill: "c1-shade" }),
      P(ovalBand(51, 43, 13, -2, 182, 6.4, -14), { fill: "@near" }),
      P(ovalBand(51, 43, 13, 6, 174, 1.8, -14), { fill: "c1-glint" }),
      P(blob(64, 44, 5.6), { fill: "@moon", a: true }),
      P(blob(61, 45.4, 1.9), { fill: "c2-glint", a: true, o: 2 }),
      ...row([-46, -20], (a) => star(a, 44, 2.8, 1, 4), { z: "back", fill: "c2-glint", a: true }),
    ],
  },
  {
    id: "comet",
    name: "Comet",
    group: "Cosmic",
    anim: "whirl",
    own: ["#6f9ee0", "#fff2c0"],
    defs: [
      // A tail is dense at the head and gone at the end. Shading it along the
      // radius cannot express that — the tail runs AROUND the head at a
      // constant radius — so this is a linear ramp across the box instead,
      // and the taper does the rest.
      LG("tail", 18, 22, 74, 62, [
        [0, "c1", 0],
        [0.45, "c1", 0.5],
        [0.8, "c1-lit", 0.85],
        [1, "c2-glint"],
      ]),
      glow("core", "c2"),
      BLUR("haze", 2.4),
    ],
    parts: [
      P(taperBand(43, -130, 14, 0.8, 7.6), { z: "back", fill: "@tail", a: true }),
      P(taperBand(43, -92, 12, 0.6, 4), { z: "back", fill: "c2-glint", op: 0.6, a: true }),
      P(blob(18, 42, 7.4), { fill: "@core", filter: "haze", a: true }),
      P(star(18, 42, 8.6, 2, 4, 0), { fill: "c2-glint", a: true }),
      P(blob(18, 42, 3.4), { fill: "@core", a: true }),
      P(blob(17, 43.4, 1.7), { fill: "light", a: true }),
      ...row([-150, -112], (a, i) => star(a, 43 + i * 2, 2.6, 0.9, 4), {
        z: "back",
        fill: "c2-glint",
        a: true,
      }),
    ],
  },
  {
    id: "constellation",
    name: "Constellation",
    group: "Cosmic",
    anim: "pulse",
    own: ["#6f8ad8", "#fff4d0"],
    defs: [glow("halo", "c2"), BLUR("haze", 2.2)],
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
        { fill: "c1-lit", op: 0.55, anim: "shimmer", a: true },
      ),
      // Each star gets a soft halo under a hard point. A star drawn as one
      // flat polygon is a snowflake; the halo is what makes it burn.
      ...row(
        [-80, -58, -34, -8, 18, 44, 70],
        (a, i) => blob(a, [39, 43.5, 38.5, 44.5, 39, 43.5, 38.5][i], i === 3 ? 5 : 3.4),
        { fill: "@halo", filter: "haze", a: true },
      ),
      ...row(
        [-80, -58, -34, -8, 18, 44, 70],
        (a, i) => star(a, [39, 43.5, 38.5, 44.5, 39, 43.5, 38.5][i], i === 3 ? 5.4 : 3.8, 1.4, 4),
        { fill: "c2-glint", a: true },
      ),
      P(star(12, 47.5, 3.2, 1.1, 4), { fill: "c2-lit", anim: "pulse", a: true, o: 5 }),
      ...row([-94, -20, 32, 88], (a, i) => blob(a, 42 + (i % 2) * 3, 1.2), {
        fill: "c2-glint",
        a: true,
      }),
    ],
  },
  {
    id: "orbits",
    name: "Orbits",
    group: "Cosmic",
    anim: "shimmer",
    own: ["#7f9ce8", "#f0c86a"],
    defs: [
      LG("t1", 20, 30, 80, 70, [
        [0, "c1-glint"],
        [0.5, "c1"],
        [1, "c1-deep"],
      ]),
      LG("t2", 80, 30, 20, 70, [
        [0, "c2-glint"],
        [0.5, "c2"],
        [1, "c2-deep"],
      ]),
      gemG("body", "c2"),
    ],
    parts: [
      P(ovalBand(50, 43, 15, 180, 360, 2.4, 26), { z: "back", fill: "@t1", a: true }),
      P(ovalBand(50, 43, 15, 0, 180, 2.4, 26), { fill: "@t1", a: true }),
      P(ovalBand(50, 43, 15, 180, 360, 2.4, -26), { z: "back", fill: "@t2", a: true, o: 3 }),
      P(ovalBand(50, 43, 15, 0, 180, 2.4, -26), { fill: "@t2", a: true, o: 3 }),
      P(blob(72, 44, 3.6), { fill: "@body", anim: "whirl", a: true }),
      P(blob(-108, 44, 3), { fill: "@body", anim: "whirl", a: true, o: "r" }),
      P(blob(71, 45.2, 1.2), { fill: "c2-glint", anim: "whirl", a: true }),
    ],
  },
  {
    id: "solar-corona",
    name: "Solar corona",
    group: "Cosmic",
    anim: "flicker",
    own: ["#f07a1f", "#ffe89a"],
    defs: [
      // Prominences are hottest at the surface and cool as they arc away, so
      // the ramp runs from the second colour out into nothing.
      section("flare", 38, 48, [
        [0, "c2-glint"],
        [0.24, "c2-lit"],
        [0.58, "c1"],
        [1, "c1", 0.05],
      ]),
      glow("corona", "c1"),
      BLUR("haze", 3.2),
    ],
    parts: [
      P(annulus(41, 11), { z: "back", fill: "@corona", filter: "haze", op: 0.6, a: true }),
      ...row(
        Array.from({ length: 18 }, (_, i) => i * 20),
        (a, i) => leaf(a, 38, [11, 5.5, 8][i % 3], 4.5, [3, -3, 0][i % 3]),
        { z: "back", fill: "@flare", a: true },
      ),
      ...row(
        Array.from({ length: 9 }, (_, i) => i * 40 + 10),
        (a, i) => leaf(a, 38, [5, 3.5][i % 2], 2.6, 0),
        { z: "back", fill: "c2-glint", a: true },
      ),
      P(annulus(38.4, 2.6), { z: "back", fill: "c1-lit" }),
      P(annulus(39.6, 0.8), { z: "back", fill: "c2-glint", anim: "shimmer", a: true }),
      ...row([-40, 30], (a, i) => leaf(a, 40, 7 + i, 3, 3), {
        z: "back",
        fill: "c2-glint",
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
    own: ["#3f4654", "#4fd6e8"],
    defs: [
      castG("cast", 1),
      metal("head", 38.8, 44.4),
      // The ear cup is a moulded shell, not a band, so it takes an
      // object-fitted radial: one definition lights both cups identically
      // wherever they sit.
      RGB(
        "cup",
        [
          [0, "c1-lit"],
          [0.3, "c1"],
          [0.72, "c1-shade"],
          [1, "c1-deep"],
        ],
        { fx: 0.34, fy: 0.28 },
      ),
      glow("led", "c2"),
      sheen("sun", 48, 0.9),
    ],
    parts: [
      cast(-100, 100, 9),
      P(arcBand(41.6, -96, 96, 5.6), { fill: "c1-deep" }),
      P(arcBand(41.6, -96, 96, 3), { fill: "@head" }),
      P(arcBand(43.2, -62, 62, 0.9), { fill: "c1-glint" }),
      ...mirror(96, (a) => chip(a, 39.6, 10, 16, 3.2), { fill: "c1-deep" }),
      ...mirror(96, (a) => chip(a, 39.6, 7.4, 13, 2.6), { fill: "@cup" }),
      ...mirror(96, (a) => chip(a, 39.6, 4.6, 9, 1.8), { fill: "@led", anim: "shimmer", a: true }),
      P(taperBand(36.4, 102, 146, 2.6, 2), { fill: "c1-deep" }),
      P(taperBand(36.4, 104, 144, 1, 0.8), { fill: "@head" }),
      P(blob(148, 36.4, 3), { fill: "c1-deep" }),
      P(blob(148, 36.4, 1.7), { fill: "c2-glint", anim: "pulse", a: true }),
      P(arcBand(41.6, -96, 96, 3), { fill: "@sun" }),
    ],
  },
  {
    id: "antennae",
    name: "Antennae",
    group: "Tech",
    anim: "twitch",
    own: ["#5a6474", "#7df9c8"],
    defs: [
      castG("cast", 0.9),
      metal("band", 35.6, 38.8),
      section("stalk", 36.8, 52, [
        [0, "c1-deep"],
        [0.24, "c1-shade"],
        [0.55, "c1-lit"],
        [1, "c1-shade"],
      ]),
      gemG("bulb", "c2"),
      glow("halo", "c2"),
      BLUR("haze", 2),
      sheen("sun", 46, 0.8),
    ],
    parts: [
      ...mirror(28.6, (a) => blob(a, 50.4, 5), {
        z: "back",
        fill: "@halo",
        filter: "haze",
        anim: "pulse",
        a: true,
      }),
      cast(-68, 68, 8),
      P(arcBand(37.2, -64, 64, 3.2), { fill: "@band" }),
      P(arcBand(38.8, -64, 64, 0.9), { fill: "c1-glint" }),
      ...mirror(27, (a, s) => ray(a, 36.8, 48, 2.6, 1.6) + blob(a + 1.6 * s, 50.4, 4), {
        fill: "@stalk",
        a: true,
        pv: 37,
      }),
      ...mirror(28.6, (a) => blob(a, 50.4, 2.2), { fill: "@bulb", anim: "pulse", a: true }),
      ...row([-48, 0, 48], (a) => blob(a, 37.2, 1.5), { fill: "@bulb", anim: "shimmer", a: true }),
    ],
  },
  {
    id: "circuit-traces",
    name: "Circuit traces",
    group: "Tech",
    anim: "shimmer",
    own: ["#2f8fb8", "#7df9ff"],
    defs: [
      castG("cast", 0.9),
      stone("board", 36.8, 40.4),
      glow("trace", "c2"),
      BLUR("haze", 1.6),
      gemG("pad", "c2"),
    ],
    parts: [
      P(arcBand(38.6, -108, 108, 4.6), { z: "back", fill: "@trace", filter: "haze", op: 0.5, a: true }),
      cast(-112, 112, 8),
      P(arcBand(38.6, -108, 108, 3.6), { fill: "@board" }),
      P(arcBand(38.6, -108, 108, 1.2), { fill: "c2-glint" }),
      P(
        one([-96, -72, -48, -24, 0, 24, 48, 72, 96], (a, i) =>
          ray(a, 38.6, 38.6 + (i % 2 ? 5.4 : -4.6), 1.1, 1.1),
        ),
        { fill: "c2-lit" },
      ),
      ...row(
        [-96, -72, -48, -24, 0, 24, 48, 72, 96],
        (a, i) => chip(a, 38.6 + (i % 2 ? 7 : -6.2), 3.4, 3.4, 1),
        { fill: "@pad", a: true },
      ),
      ...row([-84, -60, -36, -12, 12, 36, 60, 84], (a) => blob(a, 38.6, 1.5), {
        fill: "c2-glint",
        a: true,
      }),
      P(arcBand(41.4, -108, -60, 0.8), { fill: "c2-glint", anim: "pulse", a: true }),
      P(arcBand(41.4, 60, 108, 0.8), { fill: "c2-glint", anim: "pulse", a: true, o: 4 }),
    ],
  },
  {
    id: "holo-rim",
    name: "Holo rim",
    group: "Tech",
    anim: "shimmer",
    own: ["#3f9fd8", "#9be8ff"],
    defs: [glow("beam", "c1"), BLUR("haze", 2), gemG("node", "c2")],
    parts: [
      P(annulus(40.8, 7), { z: "back", fill: "@beam", filter: "haze", op: 0.5, a: true }),
      ...Array.from({ length: 12 }, (_, i) =>
        P(arcBand(39.6, i * 30 - 10, i * 30 + 10, 2.2), {
          z: "back",
          fill: "c1-glint",
          anim: "whirl",
          a: true,
          o: (i % 8) + 1,
        }),
      ),
      ...Array.from({ length: 6 }, (_, i) =>
        P(arcBand(44.4, i * 60 + 14, i * 60 + 46, 1.2), {
          z: "back",
          fill: "c2-lit",
          anim: "whirl",
          a: true,
          o: "r",
        }),
      ),
      ...row([-90, 0, 90, 180], (a) => chip(a, 39.6, 3, 3, 0.8), {
        z: "back",
        fill: "@node",
        a: true,
      }),
    ],
  },
  {
    id: "goggles-up",
    name: "Goggles up",
    group: "Tech",
    anim: "shimmer",
    own: ["#4a5260", "#e0a63f"],
    defs: [
      castG("cast", 1.1),
      stone("strap", 33.7, 38.7),
      metal("rim", 34, 41),
      // The lens: a dome, so its hot spot is up and left like every other
      // rounded thing here, and it darkens hard at the bottom edge where the
      // glass turns away.
      RGB(
        "lens",
        [
          [0, "c2-glint"],
          [0.24, "c2-lit"],
          [0.58, "c2"],
          [0.85, "c2-shade"],
          [1, "c2-deep"],
        ],
        { fx: 0.32, fy: 0.26 },
      ),
      sheen("sun", 46, 0.9),
    ],
    parts: [
      cast(-130, 130, 10),
      P(arcBand(36.2, -126, 126, 5), { fill: "@strap" }),
      P(arcBand(38, -126, 126, 1.8), { fill: "c1-lit" }),
      P(chip(0, 38.6, 9, 3.4, 1.2), { fill: "c1-deep" }),
      ...mirror(28, (a) => blob(a, 37.4, 10.4), { fill: "c1-deep" }),
      ...mirror(28, (a) => blob(a, 37.4, 8.8), { fill: "@rim" }),
      ...mirror(28, (a) => blob(a, 37.4, 7.2), { fill: "@lens" }),
      ...mirror(28, (a, s) => leaf(a - 6 * s, 34.6, 6.5, 2.2, 2.4 * s), {
        fill: "light",
        op: 0.55,
        a: true,
      }),
      ...mirror(76, (a) => chip(a, 36.6, 4.4, 5.6, 1.4), { fill: "@rim", anim: "pulse", a: true }),
    ],
  },
  {
    id: "signal",
    name: "Signal",
    group: "Tech",
    anim: "pulse",
    own: ["#4a5260", "#5fe08a"],
    defs: [
      castG("cast", 0.9),
      metal("band", 35.5, 38.9),
      section("mast", 37.2, 46, [
        [0, "c1-deep"],
        [0.3, "c1-shade"],
        [0.6, "c1-lit"],
        [1, "c1-shade"],
      ]),
      glow("wave", "c2"),
      BLUR("haze", 1.8),
      gemG("lamp", "c2"),
      sheen("sun", 46, 0.8),
    ],
    parts: [
      // The broadcast, behind the head — arcs of light leaving the mast. It
      // was three flat strokes before, which read as a wireframe rainbow.
      P(blob(0, 44.8, 4.8), { z: "back", fill: "@wave", filter: "haze", anim: "pulse", a: true }),
      cast(-108, 108, 8),
      P(arcBand(37.2, -104, 104, 3.4), { fill: "@band" }),
      P(arcBand(38.8, -104, 104, 0.9), { fill: "c1-glint" }),
      P(ray(0, 37.2, 44, 3.4, 1.8), { fill: "@mast" }),
      P(blob(0, 45.4, 2.4), { fill: "@lamp", a: true }),
      ...row([1, 2, 3], (_a, i) => arcBand(40.4 + i * 2.4, -100, -48, 1.1 + i * 0.2), {
        fill: "c2-glint",
        a: true,
      }),
      ...row([1, 2, 3], (_a, i) => arcBand(40.4 + i * 2.4, 48, 100, 1.1 + i * 0.2), {
        fill: "c2-glint",
        a: true,
      }),
      ...row([-88, 88], (a) => chip(a, 37.2, 3.4, 3.4, 1), { fill: "c1-deep" }),
      ...row([-24, 24], (a) => blob(a, 37.2, 1.4), { fill: "c2-glint", anim: "shimmer", a: true }),
    ],
  },
  {
    id: "circuit-crown",
    name: "Circuit crown",
    group: "Tech",
    anim: "pulse",
    tilt: true,
    own: ["#3f4a5e", "#7df9ff"],
    defs: [
      castG("cast", 1),
      stone("board", 35.1, 39.7),
      metal("post", 37.4, 49),
      glow("trace", "c2"),
      BLUR("haze", 1.8),
      gemG("chip", "c2"),
      sheen("sun", 46, 0.85),
    ],
    parts: [
      P(crescent(-94, 94, 46, 10), { z: "back", fill: "@trace", filter: "haze", op: 0.5, a: true }),
      cast(-96, 96, 9),
      P(arcBand(37.4, -92, 92, 4.6), { fill: "c1-deep" }),
      P(arcBand(37.4, -92, 92, 2.4), { fill: "@board" }),
      P(one([-70, -42, -14, 14, 42, 70], (a, i) => ray(a, 37.4, 37.4 + [6, 10, 7, 7, 10, 6][i], 4.4, 3)), {
        fill: "c1-deep",
      }),
      P(
        one([-70, -42, -14, 14, 42, 70], (a, i) =>
          ray(a, 37.4, 37.4 + [4.6, 8.4, 5.6, 5.6, 8.4, 4.6][i], 2.4, 1.6),
        ),
        { fill: "@post" },
      ),
      ...row(
        [-70, -42, -14, 14, 42, 70],
        (a, i) => chip(a, 37.4 + [7.4, 11.4, 8.4, 8.4, 11.4, 7.4][i], 3.6, 3, 1),
        { fill: "@chip", a: true },
      ),
      ...row([-84, -56, -28, 0, 28, 56, 84], (a) => blob(a, 37.4, 1.4), {
        fill: "c2-glint",
        a: true,
      }),
      P(arcBand(37.4, -92, 92, 2.4), { fill: "@sun" }),
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
  //
  // Everything from here down is a RING, and every one of them used to be the
  // same ring: a flat stroked circle with a different sprinkle of ornaments
  // around it. They are rebuilt on `banded`, so each now has a cross-section —
  // a dark inner edge, a lit face, a dark outer edge — and a material that
  // says what it is made of before any ornament is read. At a member row's
  // twenty pixels the ornament is two pixels and the material is the whole
  // decoration, which is the argument for doing it in that order.
  {
    id: "runic-ring",
    name: "Runic ring",
    group: "Arcane",
    ring: true,
    anim: "spin",
    own: ["#5b4a7a", "#b98cf5"],
    defs: [
      stone("band", 38.2, 44),
      glow("rune", "c2"),
      BLUR("haze", 1.8),
      sheen("sun", 46, 0.7),
    ],
    parts: [
      // The glow lives BEHIND the stone and is blurred, so the runes read as
      // light coming through the band rather than paint sitting on it.
      P(annulus(41.1, 4.4), { z: "back", fill: "@rune", filter: "haze", op: 0.75, a: true }),
      ...banded(41.1, 5.8),
      seat(38.5, 0.9),
      // Runes: bars cut across the band, alternating long and short, as a
      // single path — twelve glyphs have no business being twelve elements on
      // every avatar in a member list. Painted in a flat ramp step rather than
      // the glow gradient: `glow` is object-fitted, so on a bar 1.5 units wide
      // it fades out at both ends and the rune reads as a smudge.
      P(
        one([0, 30, 60, 90, 120, 150, 180, 210, 240, 270, 300, 330], (a, i) =>
          i % 2 ? ray(a, 39.2, 43, 1.7, 1.7) : ray(a, 39.4, 42.8, 1, 1),
        ),
        { fill: "c2-glint", a: true },
      ),
      P(one([15, 75, 135, 195, 255, 315], (a) => blob(a, 41.1, 1.1)), {
        fill: "c2-glint",
        a: true,
        hi: true,
      }),
    ],
  },
  {
    id: "orbit-sigils",
    name: "Orbiting sigils",
    group: "Arcane",
    ring: true,
    anim: "spin",
    own: ["#4a3f6b", "#9d7bf0"],
    defs: [metal("band", 38.6, 42), gemG("sigil", "c2"), glow("aura", "c2"), BLUR("haze", 2.2)],
    parts: [
      P(annulus(40.3, 6), { z: "back", fill: "@aura", filter: "haze", op: 0.55, a: true }),
      P(annulus(40.3, 2.4), { fill: "@band", a: true }),
      // Three sigils on their own clock, at their own radius, turning the
      // other way. One ring of evenly spaced ornaments is a gear; two rings
      // running against each other is an orrery.
      ...row([0, 120, 240], (a) => star(a, 44.6, 5.6, 2.2, 4, 45), {
        fill: "@sigil",
        anim: "whirl",
        a: true,
      }),
      ...row([60, 180, 300], (a) => star(a, 36.8, 4.2, 1.7, 4, 45), {
        fill: "@sigil",
        anim: "whirl",
        a: true,
        o: "r",
      }),
      P(one([30, 90, 150, 210, 270, 330], (a) => blob(a, 40.3, 0.9)), {
        fill: "c2-glint",
        a: true,
        hi: true,
      }),
    ],
  },
  {
    id: "eldritch-iris",
    name: "Eldritch iris",
    group: "Arcane",
    ring: true,
    anim: "pulse",
    own: ["#3d2d4f", "#c65ce0"],
    defs: [
      stone("band", 39, 45.4),
      glow("pupil", "c2"),
      BLUR("haze", 2.6),
      sheen("sun", 47, 0.75),
    ],
    parts: [
      P(annulus(41.4, 8), { z: "back", fill: "@pupil", filter: "haze", op: 0.5, a: true }),
      ...banded(42.2, 6.4),
      // The lids. Two crescents closing on the band from above and below, on
      // the chomp clock, so the whole ring reads as an eye that blinks rather
      // than a circle that pulses. They sit OUTSIDE the band rather than over
      // it: drawn at the band's own radius they covered it exactly and the
      // whole decoration went back to being one flat hoop.
      P(crescent(-104, 104, 47.6, 6), { fill: "@band", anim: "chomp", a: true }),
      P(crescent(76, 284, 47.6, 6), { fill: "@band", anim: "chomp", a: true, o: "r" }),
      P(annulus(38.6, 1.6), { fill: "@pupil", a: true }),
      seat(39.2, 0.8),
    ],
  },
  {
    id: "spellbound-chain",
    name: "Spellbound chain",
    group: "Arcane",
    ring: true,
    anim: "spin",
    own: ["#6d6b8c", "#cfd3ea"],
    defs: [
      metal("band", 37, 45),
      section("band2", 37, 45, [
        [0, "c2-deep"],
        [0.3, "c2-shade"],
        [0.55, "c2-glint"],
        [0.8, "c2"],
        [1, "c2-deep"],
      ]),
      glow("aura", "c2"),
      BLUR("haze", 2),
    ],
    parts: [
      P(annulus(41, 5), { z: "back", fill: "@aura", filter: "haze", op: 0.45, a: true }),
      // A real chain: links lying along the band and standing across it in
      // turn, so it interlocks instead of reading as a bracelet of beads.
      ...links(14, 41, 3.5, 1.5).map((p) => ({ ...p, a: true })),
    ],
  },
  {
    id: "warding-hex",
    name: "Warding hex",
    group: "Arcane",
    ring: true,
    anim: "sway",
    own: ["#4b4468", "#8fe0ff"],
    defs: [
      stone("band", 38.4, 44),
      glow("seam", "c2"),
      BLUR("haze", 1.6),
      sheen("sun", 46, 0.6),
    ],
    parts: [
      P(annulus(41.2, 5.6), { z: "back", fill: "@seam", filter: "haze", op: 0.6, a: true }),
      ...banded(41.2, 5.2),
      // Six ward plates, seamed with light. The seams are what carry the
      // decoration at size; the plates only have to be there to be seamed.
      P(one([0, 60, 120, 180, 240, 300], (a) => ray(a, 38, 44.4, 1.8, 1.8)), {
        fill: "c2-glint",
        a: true,
      }),
      ...row([30, 90, 150, 210, 270, 330], (a) => star(a, 41.2, 3.2, 1.2, 6), {
        fill: "c2-lit",
        anim: "shimmer",
        a: true,
      }),
      seat(38.7, 0.9),
    ],
  },
  // ---------- nature ----------
  {
    id: "laurel-ring",
    name: "Laurel ring",
    group: "Nature",
    ring: true,
    anim: "sway",
    own: ["#3f7a43", "#c9a441"],
    defs: [
      leafG("leaf", 38, 47),
      section("stem", 38.6, 41, [
        [0, "c1-deep"],
        [0.5, "c1-shade"],
        [1, "c1-deep"],
      ]),
      sheen("sun", 47, 0.7),
    ],
    parts: [
      P(annulus(39.8, 1.5), { fill: "@stem" }),
      // Leaves on both sides of the stem, each a step further into the sway,
      // so the wreath ripples round instead of rocking as one plank.
      ...row(
        [0, 24, 48, 72, 96, 120, 144, 168, 192, 216, 240, 264, 288, 312, 336],
        (a, i) => leaf(a, 39.8, 6.6, 2.6, i % 2 ? 2.6 : -2.6),
        { fill: "@leaf", a: true },
      ),
      P(one([12, 84, 156, 228, 300], (a) => blob(a, 39.8, 1.5)), { fill: "c2", a: true }),
      P(annulus(39.8, 1.5), { fill: "@sun" }),
    ],
  },
  {
    id: "blossom-crown",
    name: "Blossom crown",
    group: "Nature",
    ring: true,
    anim: "pulse",
    own: ["#4a8050", "#f2a8c8"],
    defs: [
      leafG("leaf", 38, 46),
      gemG("petal", "c2"),
      section("stem", 38.6, 41, [
        [0, "c1-deep"],
        [0.5, "c1-shade"],
        [1, "c1-deep"],
      ]),
    ],
    parts: [
      P(annulus(39.6, 1.4), { fill: "@stem" }),
      ...row([18, 54, 90, 126, 162, 198, 234, 270, 306, 342], (a) => leaf(a, 39.6, 5.2, 2.2, 2.2), {
        fill: "@leaf",
        anim: "sway",
        a: true,
      }),
      // Blossoms opening in a ripple round the ring rather than all at once —
      // `row` steps each one into the cycle already running.
      ...row([0, 72, 144, 216, 288], (a) => flower(a, 41.4, 5.4), { fill: "@petal", a: true }),
      ...row([0, 72, 144, 216, 288], (a) => blob(a, 41.4, 1.5), { fill: "c1-lit", a: true }),
    ],
  },
  // ---------- forged ----------
  {
    id: "riveted-band",
    name: "Riveted band",
    group: "Forged",
    ring: true,
    anim: "spin",
    own: ["#7a828e", "#d7dee7"],
    defs: [metal("band", 37.6, 44.4), gemG("rivet", "c2"), sheen("sun", 46, 0.95)],
    parts: [
      ...banded(41, 6.8),
      seat(37.9, 1),
      // Rivets are spheres, not dots: the object-fitted radial puts the same
      // hot spot up and left on every one of them at every size.
      P(one([0, 40, 80, 120, 160, 200, 240, 280, 320], (a) => blob(a, 41, 2.5)), {
        fill: "@rivet",
        a: true,
      }),
      P(one([20, 60, 100, 140, 180, 220, 260, 300, 340], (a) => ray(a, 38.2, 43.8, 0.7, 0.7)), {
        fill: "c1-deep",
        op: 0.5,
      }),
    ],
  },
  {
    id: "gold-filigree",
    name: "Gold filigree",
    group: "Forged",
    ring: true,
    anim: "spin",
    own: ["#c79020", "#f6e2a0"],
    defs: [metal("band", 37.4, 45.2), gemG("stone", "c2"), sheen("sun", 47, 1)],
    parts: [
      P(annulus(41.2, 2.4), { fill: "@band" }),
      // Scrollwork: short paired arcs curling off the band and back onto it.
      // The first pass swept each one 80 degrees and ran it out to r=45.6,
      // which crossed its neighbours and read as tangled wire rather than
      // filigree. A scroll has to close before the next one starts.
      ...row([0, 45, 90, 135, 180, 225, 270, 315], (a) => taperBand(41.2, a - 20, a + 2, 0.5, 1.9, 44.6, 14), {
        fill: "@band",
        a: true,
      }),
      ...row([0, 45, 90, 135, 180, 225, 270, 315], (a) => taperBand(41.2, a + 20, a - 2, 0.5, 1.9, 37.8, 14), {
        fill: "@band",
        a: true,
        o: "r",
      }),
      ...row([0, 90, 180, 270], (a) => gem(a, 45.2, 4.2, 5.6), { fill: "@stone", a: true }),
      P(annulus(41.2, 2.4), { fill: "@sun" }),
    ],
  },
  {
    id: "gear-ring",
    name: "Gear ring",
    group: "Forged",
    ring: true,
    anim: "spin",
    own: ["#6f7683", "#b9c3d0"],
    defs: [metal("band", 37, 46), gemG("bolt", "c2"), sheen("sun", 47, 0.9)],
    parts: [
      // Teeth first and BEHIND the band, so the band's own lit face closes
      // over their roots. Teeth drawn on top of a ring are a saw blade.
      //
      // They root INSIDE the band and reach well past its outer edge. The
      // first pass had a 7.4-wide band and 4.6 of tooth, which left nine
      // tenths of a unit showing: a cog with no teeth is a washer.
      P(teeth(18, 40.4, 7.2, 3.6), { z: "back", fill: "@band", a: true }),
      ...banded(40.4, 6),
      seat(37.7, 1.1),
      P(one([0, 60, 120, 180, 240, 300], (a) => blob(a, 40.4, 1.9)), { fill: "@bolt", a: true }),
      P(annulus(37.9, 1.4), { fill: "c1-deep", op: 0.55 }),
    ],
  },
  {
    id: "chainmail",
    name: "Chainmail",
    group: "Forged",
    ring: true,
    anim: "sway",
    own: ["#767f8d", "#aeb8c6"],
    defs: [
      metal("band", 36, 46),
      section("band2", 36, 46, [
        [0, "c2-deep"],
        [0.28, "c2-shade"],
        [0.52, "c2-glint"],
        [0.78, "c2"],
        [1, "c2-deep"],
      ]),
    ],
    parts: [
      // A backing course, dark and behind, so the light links have something
      // to overlap. Mail is depth before it is pattern.
      P(annulus(40.6, 5.2), { z: "back", fill: "c1-deep" }),
      // Then one course of interlocking links, big enough to actually read as
      // linked. Sixteen small ones at two radii looked like scattered zeroes;
      // eleven large ones alternating flat and edge-on interlock.
      ...links(11, 40.6, 4.6, 1.7).map((p) => ({ ...p, a: true })),
    ],
  },
  {
    id: "hammered-bronze",
    name: "Hammered bronze",
    group: "Forged",
    ring: true,
    anim: "spin",
    own: ["#a8622a", "#e8a865"],
    defs: [
      metal("band", 37, 45.6),
      RGB(
        "dimple",
        [
          [0, "c1-deep"],
          [0.36, "c1-shade"],
          [0.74, "c1-lit"],
          [1, "c1-glint"],
        ],
        // The hot spot goes DOWN AND RIGHT here, the opposite of every other
        // rounded thing in the library, because a hammer dimple is concave:
        // it catches the light on the far wall of the dent, not the near one.
        { fx: 0.66, fy: 0.72 },
      ),
      sheen("sun", 46, 0.95),
    ],
    parts: [
      ...banded(41.3, 8),
      seat(37.6, 1.2),
      P(one([0, 45, 90, 135, 180, 225, 270, 315], (a) => blob(a, 42.6, 2.4)), {
        fill: "@dimple",
        a: true,
      }),
      P(one([22, 67, 112, 157, 202, 247, 292, 337], (a) => blob(a, 39.6, 2)), {
        fill: "@dimple",
        a: true,
        o: "r",
      }),
    ],
  },
  // ---------- crystal ----------
  {
    id: "gem-circlet",
    name: "Gem circlet",
    group: "Crystal",
    ring: true,
    anim: "pulse",
    own: ["#8894ad", "#6fd6f5"],
    defs: [
      metal("band", 38.4, 42.6),
      gemG("stone", "c2"),
      glow("aura", "c2"),
      BLUR("haze", 2),
      sheen("sun", 46, 0.8),
    ],
    parts: [
      P(annulus(41.6, 7), { z: "back", fill: "@aura", filter: "haze", op: 0.5, a: true }),
      P(annulus(40.5, 3), { fill: "@band" }),
      ...row([0, 45, 90, 135, 180, 225, 270, 315], (a, i) => gem(a, 44, i % 2 ? 4 : 5.4, i % 2 ? 5.4 : 7.4), {
        fill: "@stone",
        a: true,
      }),
      P(annulus(40.5, 3), { fill: "@sun" }),
    ],
  },
  {
    id: "frost-shards",
    name: "Frost shards",
    group: "Crystal",
    ring: true,
    anim: "shimmer",
    own: ["#6fb6d8", "#d8f3ff"],
    defs: [glass("shard", 37, 48), glow("aura", "c2"), BLUR("haze", 2.4), sheen("sun", 47, 0.5)],
    parts: [
      P(annulus(41.5, 8), { z: "back", fill: "@aura", filter: "haze", op: 0.42, a: true }),
      // Shards of two lengths, alternating. Even spikes are a cog; uneven ones
      // are ice, and the irregularity is the entire read.
      P(one([0, 40, 80, 120, 160, 200, 240, 280, 320], (a) => ray(a, 37.4, 47.4, 3.6, 0.4)), {
        fill: "@shard",
      }),
      P(one([20, 60, 100, 140, 180, 220, 260, 300, 340], (a) => ray(a, 37.4, 43.2, 2.6, 0.4)), {
        fill: "@shard",
        a: true,
      }),
      P(annulus(38.4, 1.8), { fill: "@shard" }),
      P(one([0, 80, 160, 240, 320], (a) => blob(a, 45, 1)), {
        fill: "c2-glint",
        a: true,
        hi: true,
      }),
    ],
  },
  {
    id: "prism-halo",
    name: "Prism halo",
    group: "Crystal",
    ring: true,
    anim: "spin",
    own: ["#9d7bd8", "#8ff0e0"],
    defs: [
      glass("ring", 38, 45),
      // A prism's whole identity is that it splits light, so this is the one
      // gradient in the library allowed to leave the wearer's colour: the
      // spectrum IS the object. It still rides over a band the wearer colours.
      axis("spectrum", 45, [
        [0, "#ff5f6d", 0.85],
        [0.24, "#ffc371", 0.8],
        [0.46, "#7ee8a2", 0.78],
        [0.68, "#5fc9f8", 0.8],
        [1, "#b57bf5", 0.85],
      ]),
      BLUR("haze", 2),
    ],
    parts: [
      P(annulus(41.6, 7.2), { z: "back", fill: "@spectrum", filter: "haze", op: 0.6, a: true }),
      P(annulus(41.6, 5.4), { fill: "@ring" }),
      P(one([0, 30, 60, 90, 120, 150, 180, 210, 240, 270, 300, 330], (a) => ray(a, 38.9, 44.3, 1.8, 0.6)), {
        fill: "@spectrum",
        a: true,
      }),
      P(annulus(38.9, 1), { fill: "c1-glint", op: 0.7 }),
    ],
  },
  {
    id: "diamond-lattice",
    name: "Diamond lattice",
    group: "Crystal",
    ring: true,
    anim: "shimmer",
    own: ["#7f8cb0", "#e6f0ff"],
    defs: [glass("facet", 37.6, 46), gemG("stone", "c2"), sheen("sun", 46, 0.6)],
    parts: [
      // The lattice: two rows of diamonds offset by half a step, joined by the
      // band between them. Offsetting is what makes it a weave rather than a
      // row of lozenges.
      P(one([0, 36, 72, 108, 144, 180, 216, 252, 288, 324], (a) => gem(a, 43.8, 4.4, 6.2)), {
        fill: "@facet",
        a: true,
      }),
      P(one([18, 54, 90, 126, 162, 198, 234, 270, 306, 342], (a) => gem(a, 39, 3.6, 5.2)), {
        fill: "@facet",
        a: true,
        o: "r",
      }),
      P(annulus(41.4, 1.4), { fill: "c1-glint", op: 0.85 }),
      ...row([0, 90, 180, 270], (a) => star(a, 43.4, 2.6, 0.9, 4), {
        fill: "@stone",
        anim: "pulse",
        a: true,
      }),
    ],
  },
  // ---------- neon ----------
  //
  // The one family here that is NOT an object: these are light, so they get
  // `glow` rather than a cross-section, and their motion is electrical —
  // hard blinks and jitter, never the slow spin the metals get.
  {
    id: "neon-circuit-ring",
    name: "Circuit ring",
    group: "Neon",
    ring: true,
    anim: "zap",
    own: ["#1d9bd1", "#7df9ff"],
    defs: [glow("trace", "c2"), BLUR("haze", 1.6), gemG("pad", "c2")],
    parts: [
      P(annulus(41.2, 5.4), { z: "back", fill: "@trace", filter: "haze", op: 0.7, a: true }),
      P(annulus(41.2, 1.6), { fill: "c2-glint", a: true }),
      // Traces breaking off the ring at right angles, the way a trace leaves a
      // bus — stubs and pads, not decoration evenly spaced round a circle.
      P(
        one([0, 45, 90, 135, 180, 225, 270, 315], (a) => ray(a, 41.2, 46.2, 1.1, 1.1)) +
          one([22, 112, 202, 292], (a) => ray(a, 41.2, 36.6, 1.1, 1.1)),
        { fill: "c2", a: true },
      ),
      P(one([0, 45, 90, 135, 180, 225, 270, 315], (a) => chip(a, 46.8, 3, 2.2, 0.6)), {
        fill: "@pad",
        a: true,
      }),
      P(one([22, 112, 202, 292], (a) => blob(a, 36.6, 1.3)), { fill: "@pad", a: true, hi: true }),
    ],
  },
  {
    id: "neon-holo-segments",
    name: "Holo segments",
    group: "Neon",
    ring: true,
    anim: "flicker",
    own: ["#2f7fd8", "#9be8ff"],
    defs: [glow("seg", "c2"), BLUR("haze", 2.2)],
    parts: [
      P(annulus(41.4, 6.4), { z: "back", fill: "@seg", filter: "haze", op: 0.55, a: true }),
      // Six arcs with gaps, each on its own step of the flicker, so the ring
      // stutters round rather than blinking as one lamp.
      ...row([0, 60, 120, 180, 240, 300], (a) => arcBand(41.4, a + 4, a + 52, 2.6), {
        fill: "c2-glint",
        a: true,
      }),
      ...row([30, 90, 150, 210, 270, 330], (a) => arcBand(45.2, a + 8, a + 46, 1.2), {
        fill: "c2",
        a: true,
        o: "r",
      }),
      P(one([0, 60, 120, 180, 240, 300], (a) => blob(a, 41.4, 1.4)), { fill: "light", a: true }),
    ],
  },
  {
    id: "neon-target-lock",
    name: "Target lock",
    group: "Neon",
    ring: true,
    anim: "twitch",
    own: ["#d84a3c", "#ffd166"],
    defs: [glow("lock", "c1"), BLUR("haze", 1.8), gemG("tick", "c2")],
    parts: [
      P(annulus(41, 5), { z: "back", fill: "@lock", filter: "haze", op: 0.6, a: true }),
      P(annulus(41, 1.2), { fill: "c1-glint", op: 0.7 }),
      // Four corner brackets, which is the whole idiom — a reticle is corners
      // and a gap, not a complete circle.
      ...row([45, 135, 225, 315], (a) => arcBand(44.4, a - 20, a + 20, 3.6), {
        fill: "c1-glint",
        a: true,
      }),
      ...row([45, 135, 225, 315], (a) => ray(a, 42.6, 47.8, 2, 2), {
        fill: "c1-lit",
        a: true,
        o: "r",
      }),
      // The crosshair ticks, on the cardinals, reaching in past the band.
      P(one([0, 90, 180, 270], (a) => ray(a, 36.8, 45.4, 0.9, 0.9)), { fill: "@tick", a: true }),
    ],
  },
  {
    id: "neon-hex-lattice",
    name: "Hex lattice",
    group: "Neon",
    ring: true,
    anim: "sway",
    own: ["#3ad1a6", "#c6ffe9"],
    defs: [glow("cell", "c2"), BLUR("haze", 2)],
    parts: [
      P(annulus(41.6, 6), { z: "back", fill: "@cell", filter: "haze", op: 0.5, a: true }),
      // The hexagon itself, as six straight chords rather than an arc — a hex
      // lattice whose outline curves is a circle with corners drawn on.
      P(
        one([0, 60, 120, 180, 240, 300], (a) => link(a, 44.8, a + 60, 44.8, 1.6)),
        { fill: "c2-glint", a: true },
      ),
      P(one([30, 90, 150, 210, 270, 330], (a) => link(a, 38.6, a + 60, 38.6, 1)), {
        fill: "c2",
        a: true,
        o: "r",
      }),
      P(one([0, 60, 120, 180, 240, 300], (a) => link(a, 44.8, a + 30, 38.6, 0.8)), {
        fill: "c2",
        op: 0.7,
      }),
      P(one([0, 60, 120, 180, 240, 300], (a) => blob(a, 44.8, 1.5)), { fill: "light", a: true }),
    ],
  },
  // ---------- celestial ----------
  {
    id: "sunburst-crown",
    name: "Sunburst",
    group: "Celestial",
    ring: true,
    anim: "spin",
    own: ["#e0a12a", "#fff0b8"],
    defs: [metal("band", 38, 44.4), glow("corona", "c2"), BLUR("haze", 2.8), sheen("sun", 46, 1)],
    parts: [
      P(annulus(42, 11), { z: "back", fill: "@corona", filter: "haze", op: 0.6, a: true }),
      // Long rays and short ones alternating, both behind the band so the ring
      // closes over their roots and they read as coming OUT of it.
      P(teeth(12, 41.2, 6.6, 3.6), { z: "back", fill: "@band", a: true }),
      P(teeth(12, 41.2, 3.4, 2.4, 15), { z: "back", fill: "@band", a: true, o: "r" }),
      ...banded(41.2, 6.2),
      seat(38.3, 0.9),
      P(one([0, 30, 60, 90, 120, 150, 180, 210, 240, 270, 300, 330], (a) => blob(a, 41.2, 1.1)), {
        fill: "c2-glint",
        a: true,
        hi: true,
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
