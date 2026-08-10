// cardscenes.js — drawn profile-card scenes.
//
// The other card library, lib/cardfx.js, is a particle engine: fields of
// specks that fall, rise, streak or twinkle. It is cheap, it composes, and it
// tops out where every particle field tops out — a field of dots cannot be a
// ghost, or a tree in the wind, or a planet with moons around it. This file is
// the other half of the choice: SCENES, drawn as SVG, with layered depth and
// motion that is authored rather than randomised.
//
// A scene is DATA — nodes and gradients — painted by CardScene.svelte, which
// owns the motion vocabulary. Nothing here is fetched at runtime; every mark
// is a path, a gradient or a filter. An id travels in the profile's `effect`
// field exactly as a particle effect's does, and resolves against whichever of
// the two libraries knows it, so **ids must be unique across both files**.
//
// ── The 272×400 box ─────────────────────────────────────────────────────────
// Authored in viewBox `0 0 272 400`, painted with `xMidYMin slice`. The card is
// 272 wide and 380–430 tall depending on how much profile there is, so the box
// is scaled to cover and cropped from the BOTTOM — the top is the part that is
// always on screen, and the part every scene composes around.
//
// The layer sits above the banner and IN FRONT of the card's text: it is
// positioned, the name and bio are not, so it paints over them. That is the
// whole design constraint and it is not negotiable:
//
//   y   0–115   over the banner. Full strength, fully opaque. The subject
//               goes here — this is the only band nothing else competes for.
//   y 115–150   the avatar straddles this, and it is where the sky has to run
//               out. Horizons, treelines, rooflines, the waterline.
//   y 150–400   NAME, bio, roles, safety number. Everything here is at ≤ 0.16
//               alpha, or it is a handful of pinprick lights, or it is absent.
//
// Every scene therefore opens with a `veil()` backdrop: opaque across the
// banner, faded to a whisper before the name. Anything that reaches down into
// the text does so through a vertical fade gradient rather than at its own
// opacity — a shape that simply stops at y=150 leaves a ruled line across the
// card, and one that carries its own alpha down there competes with the words.
//
// ── Motion ──────────────────────────────────────────────────────────────────
// Class names on a node select a keyframe from CardSceneNode.svelte; `--dur`,
// `--dl`, `--amp`, `--tx`, `--ty`, `--a` and `--sc` dial it per element. Only
// transform and opacity are ever animated. `prefers-reduced-motion` stops all
// of it, so nothing may be authored off-canvas or at zero opacity — the
// resting frame has to be the picture.

import { rng } from "./fx.js";

const r2 = (n) => Math.round(n * 100) / 100;
const rad = (deg) => (deg * Math.PI) / 180;

// ── node builders ───────────────────────────────────────────────────────────
const G = (children, o = {}) => ({ el: "g", children, ...o });
const RECT = (x, y, w, h, fill, o = {}) => ({ el: "rect", x, y, w, h, fill, ...o });
const PATH = (d, fill, o = {}) => ({ el: "path", d, fill, ...o });
const CIRC = (cx, cy, r, fill, o = {}) => ({ el: "circle", cx, cy, r, fill, ...o });
const ELL = (cx, cy, rx, ry, fill, o = {}) => ({ el: "ellipse", cx, cy, rx, ry, fill, ...o });
const LINE = (d, stroke, sw, o = {}) => ({ el: "path", d, stroke, sw, cap: "round", ...o });

// ── def builders ────────────────────────────────────────────────────────────
const LG = (id, x1, y1, x2, y2, stops) => ({ t: "lg", id, x1, y1, x2, y2, stops });
const RG = (id, cx, cy, r, stops) => ({ t: "rg", id, cx, cy, r, stops });
// A radial that fits whatever it is painted on, rather than a fixed place on
// the card: one definition serves twenty bokeh discs at twenty sizes. (The
// alternative, a user-space radial of radius 1, silently paints the shape in
// its LAST stop — which is how a field of glowing orbs became a field of dots.)
const RGB = (id, stops) => ({ t: "rgb", id, stops });
const BLUR = (id, std) => ({ t: "blur", id, std });

// The house backdrop. Solid across the banner, gone by the name, a breath of
// vignette at the very bottom so the card still feels enclosed. Every scene
// starts with one of these; it is what makes the library safe to wear.
// `k` scales the whole lower half down, for the pale scenes: a light veil over
// a dark card lifts the background toward the text instead of away from it, so
// a bright sky has to give up more of its tail than a black one does.
const veil = (id, top, low, k = 1) =>
  LG(id, 0, 0, 0, 400, [
    // OPAQUE down to y≈120. The banner underneath is 112 CSS px tall whatever
    // the card's height, so anything less than full cover leaves the banner's
    // bottom edge showing through the sky as a ruled line across the art.
    [0, top, 1],
    [0.24, top, 1],
    [0.3, top, 0.92],
    [0.33, low, 0.5],
    [0.365, low, r2(0.16 * k)],
    [0.42, low, r2(0.085 * k)],
    [0.6, low, r2(0.05 * k)],
    [0.85, low, r2(0.05 * k)],
    [1, low, r2(0.13 * k)],
  ]);

// A vertical fade, for anything that has to reach down past y=150: kelp, tree
// trunks, reflections. The shape keeps its detail up top and dissolves into
// the text band instead of stopping at a hard edge.
const fade = (id, color, y0, y1, a0, a1) =>
  LG(id, 0, y0, 0, y1, [
    [0, color, a0],
    [1, color, a1],
  ]);

// ── geometry ────────────────────────────────────────────────────────────────

// Many small marks as ONE path — window lights, distant stars, gravel. Fifty
// separate <rect>s cost fifty nodes to say the same thing.
const dots = (list) => list.map(([x, y, w, h]) => `M${r2(x)} ${r2(y)}h${w}v${h}h${-w}Z`).join("");

// A closed silhouette: ridgeline across the top, straight down to `floor`.
// The hump keeps a mountain from reading as noise, the jitter keeps it from
// reading as a bell curve.
function ridge(seed, { y, amp, steps = 10, floor = 400, w = 272, jag = 0.9, skew = 0.5 }) {
  const r = rng(seed);
  const pts = [];
  for (let i = 0; i <= steps; i++) {
    const t = i / steps;
    const hump = Math.sin(Math.pow(t, skew < 0.5 ? 0.7 : 1.3) * Math.PI) * amp;
    pts.push([r2(t * w), r2(y - hump * 0.62 - r() * amp * jag)]);
  }
  return (
    `M0 ${floor}L${pts.map(([x, yy]) => `${x} ${yy}`).join("L")}L${w} ${floor}Z`
  );
}

// One period-locked sine, drawn twice the card's width so a `shift` can slide
// it without a seam ever entering the frame.
function sine(y, amp, wave, phase = 0, w = 560, step = 10) {
  let d = `M0 ${r2(y + amp * Math.sin(phase))}`;
  for (let x = step; x <= w; x += step) {
    d += `L${r2(x)} ${r2(y + amp * Math.sin(phase + (x / wave) * Math.PI * 2))}`;
  }
  return d;
}
const swell = (y, amp, wave, phase, h) => `${sine(y, amp, wave, phase)}L560 ${y + h}L-8 ${y + h}Z`;

// A branch, and everything growing out of it, as one group pivoting on its own
// base. Nesting is the point: a twig inherits the bough's swing and the
// bough inherits the trunk's, which is what a tree in wind actually does.
// `animAt` picks ONE depth to animate — a whole tree of live groups is a lot
// of repaint for motion the eye reads from the tips anyway.
function bough(r, o) {
  const { x, y, len, ang, w, depth } = o;
  const a = rad(ang);
  const tx = x + Math.sin(a) * len;
  const ty = y - Math.cos(a) * len;
  const px = Math.cos(a);
  const py = Math.sin(a);
  const bend = (r() - 0.5) * len * 0.3;
  const mx = (x + tx) / 2 + px * bend;
  const my = (y + ty) / 2 + py * bend;
  const w2 = Math.max(0.45, w * 0.56);
  const d =
    `M${r2(x - px * w * 0.5)} ${r2(y - py * w * 0.5)}` +
    `Q${r2(mx - px * w * 0.42)} ${r2(my - py * w * 0.42)} ${r2(tx - px * w2 * 0.5)} ${r2(ty - py * w2 * 0.5)}` +
    `L${r2(tx + px * w2 * 0.5)} ${r2(ty + py * w2 * 0.5)}` +
    `Q${r2(mx + px * w * 0.42)} ${r2(my + py * w * 0.42)} ${r2(x + px * w * 0.5)} ${r2(y + py * w * 0.5)}Z`;

  const kids = [PATH(d, o.fill)];
  if (depth > 0) {
    const n = r() < 0.35 ? 3 : 2;
    for (let i = 0; i < n; i++) {
      const na = ang + (i - (n - 1) / 2) * (o.spread ?? 34) + (r() - 0.5) * 12;
      const t = 0.76 + r() * 0.24;
      kids.push(
        bough(r, {
          ...o,
          x: x + (tx - x) * t,
          y: y + (ty - y) * t,
          len: len * (0.6 + r() * 0.18),
          ang: na,
          w: w2,
          depth: depth - 1,
        }),
      );
    }
  } else if (o.leaf) {
    for (let i = 0; i < (o.leafN ?? 1); i++) {
      kids.push(
        ELL(
          r2(tx + (r() - 0.5) * o.leaf * 1.6),
          r2(ty + (r() - 0.5) * o.leaf * 1.2),
          r2(o.leaf * (0.7 + r() * 0.6)),
          r2(o.leaf * (0.5 + r() * 0.4)),
          o.leafFill,
          { o: o.leafO ?? 0.55 },
        ),
      );
    }
  }

  const live = depth === (o.animAt ?? -1) || (o.animAll && depth < o.maxDepth);
  // `hi` has to ride out on the group, not just sit in the options: a tree
  // flagged as fine detail that never carries the flag to its own node is a
  // tree the picker still pays for.
  const tag = o.hi ? { hi: true } : {};
  return G(
    kids,
    live
      ? {
          ...tag,
          cls: o.cls || "gust",
          origin: [r2(x), r2(y)],
          amp: r2((o.amp ?? 1.4) * (1 + (o.maxDepth - depth) * 0.6)),
          dur: r2((o.dur ?? 9) - (o.maxDepth - depth) * 0.9),
          dl: r2(-r() * 9),
        }
      : tag,
  );
}

// ── scenes ──────────────────────────────────────────────────────────────────

// A wraith: hood, hollow sockets, a hem torn into rags. Drawn once here so the
// haunting can afford two of them at different distances.
function wraith(cx, cy, s, fill, halo, eyeGlow) {
  const p = (dx, dy) => `${r2(cx + dx * s)} ${r2(cy + dy * s)}`;
  // Long, narrow and torn to nothing at the bottom: a hooded thing has to be
  // TALLER than it is wide or it reads as a bedsheet with eyes. The hem is
  // fourteen rags of uneven length, and the gradient it is painted with runs
  // out before they end, so it dissolves rather than stopping.
  const shroud =
    `M${p(0, -2)}` +
    `C${p(11, -2)} ${p(19, 10)} ${p(19, 26)}` +
    `C${p(19, 40)} ${p(24, 52)} ${p(27, 68)}` +
    `C${p(30, 84)} ${p(25, 98)} ${p(27, 116)}` +
    `L${p(23, 96)}L${p(20, 126)}L${p(16, 100)}L${p(12, 138)}L${p(8, 104)}` +
    `L${p(4, 144)}L${p(0, 108)}L${p(-4, 146)}L${p(-8, 106)}L${p(-12, 140)}` +
    `L${p(-16, 101)}L${p(-20, 128)}L${p(-23, 97)}L${p(-27, 118)}` +
    `C${p(-25, 98)} ${p(-30, 84)} ${p(-27, 68)}` +
    `C${p(-24, 52)} ${p(-19, 40)} ${p(-19, 26)}` +
    `C${p(-19, 10)} ${p(-11, -2)} ${p(0, -2)}Z`;
  // The hood is EMPTY. Drawing eyes on a dome gives you a bedsheet with a
  // face; cutting a dark cowl into the fabric and setting two slits back
  // inside it gives you something that is not there. The difference is the
  // whole scene.
  const cowl =
    `M${p(0, 9)}C${p(8, 9)} ${p(11.5, 19)} ${p(11, 30)}` +
    `C${p(10, 41)} ${p(5, 48)} ${p(0, 49)}` +
    `C${p(-5, 48)} ${p(-10, 41)} ${p(-11, 30)}` +
    `C${p(-11.5, 19)} ${p(-8, 9)} ${p(0, 9)}Z`;
  // Folds. Three hairlines down the drape is the whole difference between
  // cloth and a balloon.
  const folds =
    `M${p(-13, 40)}C${p(-15, 58)} ${p(-16, 76)} ${p(-14, 94)}` +
    `M${p(0, 56)}C${p(-1, 76)} ${p(-1, 92)} ${p(0, 106)}` +
    `M${p(13, 42)}C${p(16, 60)} ${p(17, 78)} ${p(15, 96)}`;
  const arm = (k) =>
    `M${p(k * 19, 48)}C${p(k * 36, 54)} ${p(k * 43, 70)} ${p(k * 37, 86)}` +
    `C${p(k * 35, 70)} ${p(k * 29, 60)} ${p(k * 17, 58)}Z`;
  const ex = r2(5.6 * s);
  const ey = r2(cy + 29 * s);
  return [
    CIRC(cx, r2(cy + 38 * s), r2(58 * s), halo, { o: 0.34 }),
    PATH(shroud, fill),
    G([PATH(arm(-1), fill, { o: 0.6 })], { cls: "sway", origin: [r2(cx - 19 * s), r2(cy + 48 * s)], amp: 7, dur: 5.4, dl: -1.3 }),
    G([PATH(arm(1), fill, { o: 0.6 })], { cls: "sway", origin: [r2(cx + 19 * s), r2(cy + 48 * s)], amp: 7, dur: 6.2, dl: -3.4 }),
    LINE(folds, "#0a1220", r2(0.9 * s), { o: 0.35 }),
    PATH(cowl, "#04060c", { o: 0.9 }),
    ELL(r2(cx - ex), ey, r2(1.5 * s), r2(4.2 * s), eyeGlow, { cls: "twinkle", a: 1, dur: 3.6, dl: -1.1, o: 0.66 }),
    ELL(r2(cx + ex), ey, r2(1.5 * s), r2(4.2 * s), eyeGlow, { cls: "twinkle", a: 1, dur: 4.3, dl: -2.7, o: 0.66 }),
  ];
}

function hollowScene() {
  const parts = [
    RECT(0, 0, 272, 400, "@sky"),
    CIRC(206, 42, 72, "@halo", { cls: "breathe", a: 0.95, dur: 12, o: 0.8 }),
    CIRC(206, 42, 19, "@moon", { o: 0.95 }),
    CIRC(199, 36, 4.2, "rgba(150,168,192,.4)"),
    CIRC(213, 48, 3, "rgba(150,168,192,.34)"),
    CIRC(204, 51, 2.1, "rgba(150,168,192,.3)"),
  ];
  // Bare winter woods. Trunks are rooted below the name and fade out on the
  // way down, so what you actually see is a canopy of black branches.
  for (const [x, len, w, tilt] of [
    [14, 62, 5.6, -7],
    [62, 48, 4.4, 5],
    [124, 70, 6.2, -2],
    [186, 46, 4.2, 8],
    [250, 58, 5.2, -5],
  ]) {
    parts.push(
      bough(rng(`hollow-${x}`), {
        x,
        y: 182,
        len,
        ang: tilt,
        w,
        depth: 2,
        maxDepth: 2,
        animAt: 1,
        spread: 44,
        fill: "@bark",
        amp: 1.1,
        dur: 10,
        hi: x === 62 || x === 186,
      }),
    );
  }
  parts.push(
    ELL(120, 140, 200, 15, "@mist", { cls: "shift", tx: 26, dur: 34, o: 1 }),
    ELL(170, 126, 150, 10, "@mist", { cls: "shift", tx: -20, dur: 27, dl: -9, o: 0.75, hi: true }),
    // The far one is smaller, dimmer and slower, and it walks the other way —
    // depth and unease, from one drawing used twice.
    G([G(wraith(196, 18, 0.62, "@wraith2", "@wraithhalo2", "#a7e6ff"), { cls: "bob", ty: 3, dur: 7.5 })], {
      cls: "cross",
      x0: 90,
      x1: -250,
      a: 0.32,
      dur: 41,
      o: 0.26,
      hi: true,
    }),
    G([G(wraith(74, 30, 1.14, "@wraith", "@wraithhalo", "#c9f2ff"), { cls: "bob", ty: 5, dur: 6.4, dl: -2 })], {
      cls: "cross",
      x0: -150,
      x1: 250,
      a: 0.5,
      dur: 29,
      o: 0.44,
    }),
    RECT(0, 300, 272, 100, "@floor"),
  );
  // Branch tips clawing up from the bottom edge: the card is enclosed, and the
  // eye gets something at the foot without anything bright landing on text.
  for (const [x, len, tilt] of [
    [26, 42, 12],
    [96, 30, -9],
    [176, 36, 7],
    [246, 34, -14],
  ]) {
    parts.push(
      bough(rng(`hollow-low-${x}`), {
        x,
        y: 412,
        len,
        ang: tilt,
        w: 3.4,
        depth: 1,
        maxDepth: 1,
        animAt: 0,
        spread: 40,
        fill: "@claw",
        amp: 1.6,
        dur: 11,
        hi: true,
      }),
    );
  }
  return parts;
}

function witchlightScene() {
  const r = rng("witchlight");
  const parts = [
    RECT(0, 0, 272, 400, "@sky"),
    // Swamp gas, low and enormous, drifting the long way.
    ELL(90, 120, 150, 44, "@gas", { cls: "shift", tx: 30, dur: 41, o: 1 }),
    ELL(210, 96, 130, 36, "@gas2", { cls: "shift", tx: -24, dur: 33, dl: -12, o: 1 }),
  ];
  // Drowned trees: no leaves, leaning, doubled by their own reflection.
  for (const [x, len, w, tilt] of [
    [34, 74, 5, 11],
    [104, 56, 4, -8],
    [166, 82, 5.6, 6],
    [236, 60, 4.4, -12],
  ]) {
    parts.push(
      bough(rng(`wl-${x}`), {
        x,
        y: 150,
        len,
        ang: tilt,
        w,
        depth: 2,
        maxDepth: 2,
        animAt: 1,
        spread: 46,
        fill: "@trunk",
        amp: 1,
        dur: 12,
        hi: x === 104 || x === 236,
      }),
    );
    // The reflection is the same tree upside down and nearly gone — enough to
    // say "water" and far too faint to fight the name below it.
    parts.push(
      G(
        [
          bough(rng(`wl-${x}`), {
            x,
            y: 150,
            len,
            ang: tilt,
            w,
            depth: 1,
            maxDepth: 1,
            spread: 46,
            fill: "@ripple",
          }),
        ],
        { tr: `translate(0 ${r2(300)}) scale(1 -1)`, cls: "", hi: true },
      ),
    );
  }
  // Will-o'-the-wisps. Each is a halo, a core and a blink, hovering rather
  // than travelling — a light that crosses the card reads as a firefly, and a
  // light that hangs and pulses reads as something waiting for you.
  const wisps = [
    [44, 100, 1.15],
    [92, 66, 0.9],
    [140, 114, 1.3],
    [190, 80, 1],
    [232, 106, 0.8],
    [258, 52, 0.62],
  ];
  for (const [x, y, s] of wisps) {
    const dur = r2(4 + r() * 4);
    parts.push(
      G(
        [
          CIRC(x, y, r2(30 * s), "@wispglow", { cls: "breathe", a: 0.75, dur, dl: r2(-r() * 6), o: 0.5, hi: true }),
          CIRC(x, y, r2(11 * s), "@wispcore", { o: 0.7 }),
          CIRC(x, y, r2(2.8 * s), "#ecfff4", { cls: "breathe", a: 1, dur: r2(dur * 0.7), dl: r2(-r() * 4), o: 0.9 }),
          // What the water underneath makes of it: directly below, stretched
          // flat, and a fifth as bright.
          ELL(x, r2(y + 44), r2(15 * s), r2(3 * s), "@wispglow", { cls: "shimmer", tx: 4, a: 0.36, dur: r2(7 + r() * 5), o: 0.15, hi: true }),
        ],
        { cls: "bob", ty: r2(4 + r() * 5), dur: r2(6 + r() * 5), dl: r2(-r() * 8) },
      ),
    );
  }
  // Reeds along the bottom edge — thin, dark, and the only thing down there.
  const reeds = [];
  for (let i = 0; i < 12; i++) {
    const x = r2(4 + r() * 264);
    const h = r2(26 + r() * 40);
    reeds.push(
      G([LINE(`M${x} 402C${r2(x + 3)} ${r2(402 - h * 0.6)} ${r2(x + 5)} ${r2(402 - h * 0.85)} ${r2(x + 2)} ${r2(402 - h)}`, "@reed", 1.5)], {
        cls: "sway",
        origin: [x, 402],
        amp: r2(2 + r() * 3),
        dur: r2(5 + r() * 5),
        dl: r2(-r() * 8),
        hi: i > 5,
      }),
    );
  }
  parts.push(RECT(0, 310, 272, 90, "@bottom"), ...reeds);
  return parts;
}

function windbreakScene() {
  // A canopy seen from underneath: two trunks entering at the top corners and
  // reaching in over the card. It puts every branch in the banner band and
  // leaves the whole lower card to a few drifting leaves.
  const parts = [
    RECT(0, 0, 272, 400, "@sky"),
    CIRC(140, 34, 96, "@sun", { cls: "breathe", a: 0.7, dur: 14, o: 0.55 }),
    ridgeBand("@hill", "windbreak-hill", { y: 146, amp: 22, floor: 260, steps: 7 }),
  ];
  // A back layer first, in a colder green and thinner: the canopy needs
  // something behind it or the front branches read as stickers on a wall.
  for (const [x, y, ang, len, w, seed] of [
    [40, -22, 152, 50, 7, "wb-b1"],
    [222, -20, -150, 46, 7, "wb-b2"],
  ]) {
    parts.push(
      bough(rng(seed), {
        x, y, len, ang, w,
        depth: 2, maxDepth: 2, animAt: 1, spread: 40,
        fill: "@barkfar", leaf: 4, leafN: 5, leafFill: "@leaffar", leafO: 0.5,
        amp: 1.2, dur: 11,
        hi: true,
      }),
    );
  }
  for (const [x, y, ang, len, w, seed] of [
    [-14, -18, 128, 62, 13, "wb-l"],
    [286, -12, -126, 58, 12, "wb-r"],
    [126, -26, 172, 44, 9, "wb-m"],
  ]) {
    const fine = seed === "wb-m";
    parts.push(
      bough(rng(seed), {
        x,
        y,
        len,
        ang,
        w,
        depth: 2,
        maxDepth: 2,
        animAll: true,
        spread: 38,
        fill: "@bark",
        leaf: 4.2,
        leafN: 7,
        leafFill: "@leaf",
        leafO: 0.55,
        amp: 1.5,
        dur: 8.5,
        hi: fine,
      }),
    );
  }
  // Leaves that let go. Six of them, small, warm, and gone before they reach
  // anything you have to read.
  const r = rng("wb-leaves");
  for (let i = 0; i < 6; i++) {
    const x = r2(24 + r() * 224);
    const y = r2(48 + r() * 50);
    parts.push(
      ELL(x, y, r2(2.6 + r() * 1.6), r2(1.7 + r() * 1), "@leaf", {
        cls: "fall",
        tx: r2(-26 + r() * 52),
        ty: r2(70 + r() * 60),
        a: 0.5,
        dur: r2(9 + r() * 7),
        dl: r2(-r() * 14),
        o: 0.35,
        hi: i > 2,
      }),
    );
  }
  return parts;
}

// A ridgeline as one filled silhouette — used by half the landscape scenes.
const ridgeBand = (fill, seed, opts, extra = {}) => PATH(ridge(seed, opts), fill, extra);

function willowbankScene() {
  const r = rng("willow");
  // The limb the whole scene hangs from, as one quadratic. Fronds are anchored
  // ON it rather than on a flat line, which is the difference between a willow
  // and a bead curtain.
  const limbAt = (t) => [
    r2((1 - t) * (1 - t) * -14 + 2 * (1 - t) * t * 136 + t * t * 286),
    r2((1 - t) * (1 - t) * 48 + 2 * (1 - t) * t * 16 + t * t * 54),
  ];
  const parts = [
    RECT(0, 0, 272, 400, "@sky"),
    CIRC(48, 74, 62, "@sunglow", { o: 0.6 }),
    CIRC(48, 74, 24, "@sun", { cls: "breathe", a: 0.95, dur: 13, o: 0.75 }),
    ridgeBand("@far", "willow-far", { y: 134, amp: 18, floor: 200, steps: 9, jag: 0.5 }),
    PATH(swell(144, 2.4, 150, 0, 56), "@water", { cls: "shift", tx: 14, dur: 30, o: 1 }),
    PATH(swell(151, 1.8, 96, 2.1, 44), "@water2", { cls: "shift", tx: -11, dur: 23, dl: -7, o: 1, hi: true }),
    PATH("M-14 42Q136 12 286 50L286 60Q136 24 -14 52Z", "@bark"),
    PATH("M96 26Q128 6 176 12L176 17Q130 13 98 31Z", "@bark", { hi: true }),
  ];
  // The curtain. Anchors are RANDOM along the limb, not evenly spaced, and the
  // lengths run from 30 to 130 — evenly spaced strands of equal length are a
  // fringe on a lampshade, and that is exactly what the first attempt looked
  // like. Two depths: a dark thin layer behind, the lit one in front.
  const frond = (x, y, len, lean, layer, i) => {
    const tip = r2(x + lean);
    const d = `M${x} ${y}C${r2(x + lean * 0.1)} ${r2(y + len * 0.42)} ${r2(x + lean * 0.58)} ${r2(y + len * 0.76)} ${tip} ${r2(y + len)}`;
    const ticks = [];
    const n = Math.max(3, Math.round(len / 9));
    for (let k = 1; k <= n; k++) {
      const u = k / (n + 1);
      const px = r2(x + lean * u * u * 1.1);
      const py = r2(y + len * u);
      const sgn = k % 2 ? 1 : -1;
      const sz = r2(1.7 + r() * 1.3);
      ticks.push(
        ELL(r2(px + sgn * sz * 0.8), py, sz, r2(sz * 0.36), layer, {
          tr: `rotate(${sgn * (26 + r() * 20)} ${r2(px + sgn * sz * 0.8)} ${py})`,
          o: 0.95,
        }),
      );
    }
    return G([LINE(d, layer, r2(0.6 + r() * 0.4)), ...ticks], {
      cls: "sway",
      origin: [x, r2(y - 2)],
      amp: r2(1.2 + r() * 2.4),
      dur: r2(6 + r() * 6),
      dl: r2(-r() * 11),
      hi: i % 3 !== 0,
    });
  };
  const back = [];
  for (let i = 0; i < 15; i++) {
    const t = r();
    const [x, y] = limbAt(t);
    back.push(frond(r2(x + (r() - 0.5) * 10), r2(y + 4), r2(26 + r() * 74), r2((r() - 0.5) * 20), "@frondback", i));
  }
  parts.splice(parts.length - 2, 0, ...back);
  for (let i = 0; i < 27; i++) {
    const t = r();
    const [x, y] = limbAt(t);
    const env = 0.45 + 0.55 * Math.sin(Math.pow(t, 0.9) * Math.PI);
    parts.push(frond(r2(x + (r() - 0.5) * 7), y, r2((40 + r() * 66) * env + 26), r2((r() - 0.5) * 18), "@frond", i));
  }
  parts.push(
    // One glint travelling the water — the only bright thing below the name.
    ELL(120, 138, 46, 1.5, "#fff3d0", { cls: "shimmer", tx: 70, a: 0.24, dur: 15, o: 0.11 }),
    RECT(0, 320, 272, 80, "@bottom"),
  );
  return parts;
}

function orreryScene() {
  const r = rng("orrery");
  const parts = [
    RECT(0, 0, 272, 400, "@sky"),
    // Nebula: three still, heavily softened clouds. They are the only blurred
    // things in the library and they do not move, because a moving blur is a
    // full re-raster every frame.
    ELL(66, 62, 78, 54, "@neb1", { filter: "soft", o: 0.55 }),
    ELL(186, 108, 96, 46, "@neb2", { filter: "soft", o: 0.42 }),
    ELL(126, 30, 70, 34, "@neb3", { filter: "soft", o: 0.4 }),
  ];
  // Star field: one path for the many, live elements for the fourteen that
  // actually twinkle.
  const still = [];
  for (let i = 0; i < 70; i++) {
    const x = r2(r() * 272);
    const y = r2(r() * 400);
    // Below the name the field thins out to almost nothing.
    if (y > 150 && r() > 0.22) continue;
    const s = y > 150 ? 0.8 : 1 + (r() < 0.2 ? 0.7 : 0);
    still.push([x, y, s, s]);
  }
  parts.push(PATH(dots(still), "#dce8ff", { o: 0.34 }));
  for (let i = 0; i < 14; i++) {
    parts.push(
      CIRC(r2(r() * 272), r2(6 + r() * 130), r2(0.8 + r() * 1.1), i % 3 ? "#eaf1ff" : "#bcd4ff", {
        cls: "twinkle",
        a: r2(0.5 + r() * 0.45),
        dur: r2(3 + r() * 5),
        dl: r2(-r() * 8),
        o: 0.5,
        hi: i > 4,
      }),
    );
  }
  // The gas giant, tilted, with a ring that passes behind it and in front.
  const PX = 190;
  const PY = 72;
  parts.push(
    // The far moon goes BEHIND the planet, so it disappears for part of every
    // orbit. That occlusion is the whole reason the scene reads as 3D.
    G([CIRC(r2(PX + 74), PY, 3.4, "#c9d6e8", { o: 0.85 })], {
      cls: "spin",
      origin: [PX, PY],
      dur: 34,
      dl: -12,
    }),
    G(
      [
        LINE(`M${PX - 66} ${PY}A66 17 0 0 1 ${PX + 66} ${PY}`, "@ring", 7, { o: 0.55 }),
        CIRC(PX, PY, 42, "@planet"),
        // A terminator, so the planet is lit from the nebula side rather than
        // being a flat disc.
        CIRC(PX, PY, 42, "@shade"),
        LINE(`M${PX - 42} ${PY - 14}A44 30 0 0 0 ${PX + 40} ${PY - 20}`, "rgba(255,236,200,.18)", 5),
        LINE(`M${PX - 41} ${PY + 6}A46 34 0 0 0 ${PX + 41} ${PY - 2}`, "rgba(120,80,50,.22)", 7),
        LINE(`M${PX - 66} ${PY}A66 17 0 0 0 ${PX + 66} ${PY}`, "@ring", 7, { o: 0.8 }),
        LINE(`M${PX - 74} ${PY}A74 21 0 0 0 ${PX + 74} ${PY}`, "@ring", 2.6, { o: 0.5 }),
      ],
      { tr: `rotate(-17 ${PX} ${PY})` },
    ),
    G([CIRC(r2(PX - 58), PY, 5, "#e6ecf6", { o: 0.9 }), CIRC(r2(PX - 60), r2(PY - 2), 1.8, "rgba(120,134,158,.5)")], {
      cls: "spin",
      origin: [PX, PY],
      dur: 21,
      dl: -5,
    }),
    // A far, faint galaxy low down, at the alpha the text band allows.
    ELL(70, 236, 62, 9, "@far", { tr: "rotate(-24 70 236)", o: 0.13, hi: true }),
  );
  return parts;
}

function eclipseScene() {
  const r = rng("eclipse");
  const CX = 136;
  const CY = 74;
  // The corona: two counter-rotating fans of hairline rays. One alone reads as
  // a cog turning; two at different speeds read as plasma.
  const fan = (seed, n, minL, maxL, hw) => {
    const rr = rng(seed);
    let d = "";
    for (let i = 0; i < n; i++) {
      const a = (i / n) * 360 + (rr() - 0.5) * 4;
      const w = hw * (0.5 + rr());
      const L = minL + rr() * (maxL - minL);
      const p = (ang, rad2) => `${r2(CX + Math.cos(rad(ang)) * rad2)} ${r2(CY + Math.sin(rad(ang)) * rad2)}`;
      d += `M${p(a - w, 35)}L${p(a, 35 + L)}L${p(a + w, 35)}Z`;
    }
    return d;
  };
  const parts = [
    RECT(0, 0, 272, 400, "@sky"),
    CIRC(CX, CY, 118, "@aureole", { cls: "breathe", a: 0.8, dur: 15, o: 0.6 }),
    G([PATH(fan("ec-a", 54, 10, 44, 1.1), "@corona")], { cls: "spin", origin: [CX, CY], dur: 120, o: 0.75 }),
    G([PATH(fan("ec-b", 34, 18, 68, 0.7), "@corona2")], { cls: "spin", origin: [CX, CY], dur: 86, dl: -20, o: 0.6 }),
    CIRC(CX, CY, 35, "#05070d", { o: 0.97 }),
    CIRC(CX, CY, 35.6, "@rim", { cls: "breathe", a: 1, dur: 9, o: 0.8 }),
    // Baily's bead: one bright point creeping round the rim.
    G([CIRC(r2(CX + 35.4), CY, 2.6, "#fff8e2", { o: 0.95 })], { cls: "spin", origin: [CX, CY], dur: 46 }),
  ];
  const still = [];
  for (let i = 0; i < 44; i++) {
    const x = r2(r() * 272);
    const y = r2(r() * 150);
    still.push([x, y, 1, 1]);
  }
  parts.push(
    PATH(dots(still), "#e8eeff", { o: 0.28 }),
    ridgeBand("@land", "ec-land", { y: 152, amp: 12, floor: 250, steps: 11, jag: 1.1 }),
  );
  // Birds, because an eclipse is the one time they go quiet and come home.
  for (const [x, y, s, dur] of [
    [62, 118, 1, 30],
    [88, 106, 0.8, 37],
    [102, 126, 0.7, 25],
  ]) {
    parts.push(
      PATH(`M${x} ${y}q${r2(4 * s)} ${r2(-3 * s)} ${r2(8 * s)} 0q${r2(4 * s)} ${r2(-3 * s)} ${r2(8 * s)} 0`, "none", {
        stroke: "#0a0e16",
        sw: 1.2,
        cap: "round",
        cls: "cross",
        x0: -30,
        x1: 200,
        a: 0.5,
        dur,
        dl: r2(-r() * 20),
        o: 0.4,
        hi: true,
      }),
    );
  }
  return parts;
}

function rainglassScene() {
  const r = rng("rainglass");
  const parts = [RECT(0, 0, 272, 400, "@sky")];
  // The city out there, entirely out of focus: bokeh discs, no edges.
  for (let i = 0; i < 22; i++) {
    const x = r2(r() * 272);
    const y = r2(6 + r() * 128);
    const rr = r2(4 + r() * 13);
    parts.push(
      CIRC(x, y, rr, i % 3 === 0 ? "@bokeh2" : "@bokeh", {
        cls: "breathe",
        a: r2(0.3 + r() * 0.35),
        dur: r2(6 + r() * 8),
        dl: r2(-r() * 12),
        o: 0.3,
        hi: i > 6,
      }),
    );
  }
  parts.push(RECT(0, 0, 272, 400, "@glass"));
  // Droplets running down. A drop is a head plus the trail it left, and it
  // fades out well before the name — the run ENDS at y≈180, it does not
  // simply become invisible there.
  // A running drop is a bead with a hairline scar behind it, not a tadpole:
  // the trail is the water it FAILED to carry, so it is a fraction of the
  // bead's width and several times its length.
  for (let i = 0; i < 17; i++) {
    const x = r2(8 + r() * 256);
    const y = r2(-6 + r() * 56);
    const len = r2(16 + r() * 42);
    const w = r2(1.3 + r() * 1.3);
    parts.push(
      G(
        [
          LINE(`M${x} ${y}L${r2(x + (r() - 0.5) * 2)} ${r2(y + len)}`, "@trail", r2(w * 0.55)),
          // Beads left stranded along the scar — the reason a real pane looks
          // dotted a second after a drop has gone.
          CIRC(r2(x + 0.4), r2(y + len * 0.32), r2(w * 0.42), "@drop", { o: 0.5 }),
          CIRC(r2(x - 0.4), r2(y + len * 0.68), r2(w * 0.34), "@drop", { o: 0.4 }),
          ELL(x, r2(y + len), r2(w * 1.05), r2(w * 1.55), "@drop"),
          ELL(r2(x - w * 0.42), r2(y + len - w * 0.5), r2(w * 0.3), r2(w * 0.45), "rgba(255,255,255,.55)"),
        ],
        {
          cls: "fall",
          ty: r2(80 + r() * 56),
          tx: r2((r() - 0.5) * 5),
          a: r2(0.45 + r() * 0.4),
          dur: r2(3.5 + r() * 6),
          dl: r2(-r() * 10),
          o: 0.4,
          hi: i > 5,
        },
      ),
    );
  }
  // Condensation: still, and dense enough to say the glass is cold.
  const specks = [];
  for (let i = 0; i < 200; i++) {
    const y = r2(r() * 190);
    if (y > 140 && r() > 0.22) continue;
    const s = r() < 0.18 ? 2 : 1;
    specks.push([r2(r() * 272), y, s, s]);
  }
  parts.push(
    PATH(dots(specks), "#cfe2f2", { o: 0.22 }),
    // The sill: the card's own bottom edge, in shadow.
    RECT(0, 316, 272, 84, "@sill"),
  );
  return parts;
}

function snowlineScene() {
  const r = rng("snowline");
  const parts = [
    RECT(0, 0, 272, 400, "@sky"),
    CIRC(214, 34, 15, "@sun", { o: 0.55 }),
    // Three ridges, each paler and softer than the one in front — the oldest
    // trick for depth and still the best one.
    ridgeBand("@far", "sl-far", { y: 96, amp: 40, floor: 220, steps: 9, jag: 1.3, skew: 0.3 }, { o: 0.5 }),
    ridgeBand("@mid", "sl-mid", { y: 120, amp: 34, floor: 240, steps: 8, jag: 1.5 }, { o: 0.7 }),
    ridgeBand("@near", "sl-near", { y: 146, amp: 26, floor: 300, steps: 7, jag: 1.7, skew: 0.7 }),
    // Cloud banks crossing the peaks, slow enough to be weather.
    ELL(80, 78, 96, 14, "@cloud", { cls: "shift", tx: 34, dur: 44, o: 1 }),
    ELL(200, 104, 110, 12, "@cloud", { cls: "shift", tx: -28, dur: 36, dl: -14, o: 0.8 }),
  ];
  // Drawn snow, not a particle field: bigger, softer, and it stops falling
  // before it gets anywhere near the text.
  for (let i = 0; i < 20; i++) {
    const x = r2(r() * 272);
    const y = r2(-8 + r() * 70);
    parts.push(
      CIRC(x, y, r2(0.9 + r() * 1.8), "#ffffff", {
        cls: "fall",
        tx: r2(-22 + r() * 44),
        ty: r2(80 + r() * 70),
        a: r2(0.35 + r() * 0.45),
        dur: r2(9 + r() * 10),
        dl: r2(-r() * 18),
        o: 0.4,
        hi: i > 5,
      }),
    );
  }
  parts.push(RECT(0, 320, 272, 80, "@drift"));
  return parts;
}

function kelplineScene() {
  const r = rng("kelp");
  const parts = [RECT(0, 0, 272, 400, "@sky")];
  // Light shafts from the surface. Each is a long wedge whose opacity swings
  // on its own clock, so the water looks like it is moving above you.
  for (const [x, w, lean, dur, a] of [
    [40, 22, 26, 11, 0.32],
    [104, 34, 14, 15, 0.26],
    [178, 26, -18, 13, 0.3],
    [238, 18, -30, 17, 0.22],
  ]) {
    parts.push(
      PATH(`M${x} -10L${r2(x + w)} -10L${r2(x + w + lean)} 250L${r2(x + lean - 6)} 250Z`, "@shaft", {
        cls: "shimmer",
        tx: 5,
        a,
        dur,
        dl: r2(-r() * 10),
        o: 0.14,
      }),
    );
  }
  // Kelp: three hinged segments per stalk, rooted at the bottom edge and
  // painted with a gradient that is barely there where the text is and full
  // strength up in the banner where the fronds fan out.
  for (let i = 0; i < 7; i++) {
    const x = r2(10 + i * 40 + (r() - 0.5) * 18);
    const seg = (y0, y1, w, kids) =>
      G([LINE(`M${x} ${y0}C${r2(x + 8)} ${r2(y0 - (y0 - y1) * 0.4)} ${r2(x - 6)} ${r2(y0 - (y0 - y1) * 0.75)} ${x} ${y1}`, "@kelp", w), ...kids], {
        cls: "sway",
        origin: [x, y0],
        amp: r2(1.4 + r() * 1.6),
        dur: r2(7 + r() * 5),
        dl: r2(-r() * 10),
      });
    const blades = [];
    for (let k = 0; k < 5; k++) {
      const by = r2(96 + k * 22);
      const s = k % 2 ? 1 : -1;
      blades.push(ELL(r2(x + s * 7), by, 8, 2.6, "@kelp", { tr: `rotate(${s * 22} ${r2(x + s * 7)} ${by})` }));
    }
    const stalk = seg(404, 262, 3.4, [seg(262, 176, 2.6, [seg(176, r2(74 + r() * 34), 1.9, blades)])]);
    parts.push(i % 3 === 1 ? { ...stalk, hi: true } : stalk);
  }
  // Bubbles, sized by how far down they start.
  for (let i = 0; i < 10; i++) {
    const x = r2(r() * 272);
    parts.push(
      CIRC(x, r2(150 + r() * 90), r2(1.1 + r() * 2), "@bubble", {
        cls: "rise",
        ty: r2(-(90 + r() * 90)),
        tx: r2((r() - 0.5) * 16),
        a: r2(0.3 + r() * 0.3),
        dur: r2(7 + r() * 7),
        dl: r2(-r() * 12),
        o: 0.2,
        hi: i > 4,
      }),
    );
  }
  // Two silhouettes crossing high up, where a moving shape costs nothing.
  for (const [y, s, dur, dl] of [
    [58, 1, 26, 0],
    [92, 0.7, 34, -14],
  ]) {
    parts.push(
      G(
        [
          PATH(`M0 0c${r2(9 * s)} ${r2(-5 * s)} ${r2(20 * s)} ${r2(-4 * s)} ${r2(26 * s)} 0c${r2(-6 * s)} ${r2(4 * s)} ${r2(-17 * s)} ${r2(5 * s)} ${r2(-26 * s)} 0Z`, "@fish"),
          PATH(`M0 0l${r2(-8 * s)} ${r2(-5 * s)}l0 ${r2(10 * s)}Z`, "@fish"),
        ],
        { cls: "cross", x0: -60, x1: 320, a: 0.3, dur, dl, o: 0.22, tr: `translate(30 ${y})`, hi: true },
      ),
    );
  }
  parts.push(RECT(0, 300, 272, 100, "@floor"));
  return parts;
}

function koipondScene() {
  const r = rng("koi");
  const parts = [RECT(0, 0, 272, 400, "@sky")];
  // Ripple rings, opening and dying at three different places.
  for (const [x, y, dur, dl, sc] of [
    [72, 62, 9, 0, 3.4],
    [186, 96, 11, -4, 4.2],
    [126, 34, 13, -8, 2.8],
  ]) {
    parts.push(
      LINE(`M${x - 10} ${y}a10 4 0 1 0 20 0a10 4 0 1 0 -20 0`, "@ring", 1.2, {
        cls: "pulse",
        origin: [x, y],
        sc,
        a: 0.34,
        dur,
        dl,
        o: 0.3,
      }),
    );
  }
  // Lily pads: a disc with a wedge cut out, each rocking very slightly.
  for (const [x, y, rr] of [
    [40, 40, 20],
    [96, 96, 15],
    [204, 46, 17],
    [240, 116, 13],
    [148, 132, 12],
  ]) {
    parts.push(
      G(
        [
          PATH(`M${x} ${y}m${rr} 0a${rr} ${r2(rr * 0.82)} 0 1 1 ${r2(-rr * 0.5)} ${r2(-rr * 0.72)}Z`, "@pad"),
          LINE(`M${x} ${y}l${r2(rr * 0.7)} ${r2(rr * 0.3)}M${x} ${y}l${r2(rr * 0.2)} ${r2(rr * 0.75)}M${x} ${y}l${r2(-rr * 0.7)} ${r2(rr * 0.4)}`, "@vein", 0.8, { o: 0.5 }),
        ],
        { cls: "sway", origin: [x, y], amp: r2(1.4 + r() * 1.6), dur: r2(8 + r() * 6), dl: r2(-r() * 10) },
      ),
    );
  }
  // The koi. Body, tail that actually flutters, and a couple of blotches —
  // drawn nose-right and then carried across on a translation.
  const koi = (s, body, blotch) =>
    G(
      [
        PATH(`M0 0c${r2(-6 * s)} ${r2(-7 * s)} ${r2(-26 * s)} ${r2(-7 * s)} ${r2(-34 * s)} 0c${r2(8 * s)} ${r2(7 * s)} ${r2(28 * s)} ${r2(7 * s)} ${r2(34 * s)} 0Z`, body),
        ELL(r2(-12 * s), r2(-1.6 * s), r2(5 * s), r2(2.6 * s), blotch, { o: 0.85 }),
        ELL(r2(-24 * s), r2(1.2 * s), r2(3.4 * s), r2(1.8 * s), blotch, { o: 0.7 }),
        G([PATH(`M${r2(-33 * s)} 0l${r2(-13 * s)} ${r2(-7 * s)}l${r2(3 * s)} ${r2(7 * s)}l${r2(-3 * s)} ${r2(7 * s)}Z`, body, { o: 0.8 })], {
          cls: "sway",
          origin: [r2(-33 * s), 0],
          amp: 13,
          dur: r2(1.4 + r() * 0.8),
        }),
      ],
      { cls: "bob", ty: r2(2 + r() * 3), dur: r2(4 + r() * 3), dl: r2(-r() * 5) },
    );
  for (const [y, s, dur, dl, body, blotch, x0, x1] of [
    [70, 1, 26, -3, "@koi1", "#fff6ea", -80, 340],
    [112, 0.78, 34, -18, "@koi2", "#ffd9a8", -70, 330],
    [40, 0.62, 42, -30, "@koi3", "#ffffff", -60, 320],
  ]) {
    parts.push(G([G([koi(s, body, blotch)], { cls: "cross", x0, x1, a: 0.72, dur, dl, o: 0.6 })], { tr: `translate(40 ${y})` }));
  }
  // Fallen petals, drifting rather than falling — the water carries them.
  for (let i = 0; i < 8; i++) {
    const x = r2(r() * 272);
    const y = r2(20 + r() * 130);
    parts.push(
      ELL(x, y, 3.2, 1.8, "@petal", {
        cls: "shift",
        tx: r2(10 + r() * 22),
        dur: r2(14 + r() * 12),
        dl: r2(-r() * 16),
        o: r2(0.3 + r() * 0.3),
        hi: i > 3,
      }),
    );
  }
  parts.push(RECT(0, 300, 272, 100, "@deep"));
  return parts;
}

function skylineScene() {
  const r = rng("skyline");
  const parts = [RECT(0, 0, 272, 400, "@sky")];
  const stars = [];
  for (let i = 0; i < 30; i++) stars.push([r2(r() * 272), r2(r() * 70), 1, 1]);
  parts.push(PATH(dots(stars), "#dbe6ff", { o: 0.3 }));

  // Two depths of buildings. The far row is pale and hazy; the near row is
  // near-black and taller, and only the near row gets lit windows.
  const row = (seed, { y0, hMin, hMax, wMin, wMax, fill, lit, litFill, o }) => {
    const rr = rng(seed);
    const boxes = [];
    const wins = [];
    const live = [];
    let x = -8;
    while (x < 280) {
      const w = r2(wMin + rr() * (wMax - wMin));
      const h = r2(hMin + rr() * (hMax - hMin));
      const top = r2(y0 - h);
      boxes.push(`M${r2(x)} ${top}h${w}v${h}h${-w}Z`);
      // A water tank or an aerial on some of them, so the skyline has a profile.
      if (rr() < 0.35) boxes.push(`M${r2(x + w * 0.3)} ${r2(top - 7)}h${r2(w * 0.3)}v7h${r2(-w * 0.3)}Z`);
      if (lit) {
        for (let cy = top + 6; cy < y0 - 4; cy += 8) {
          for (let cx = x + 3; cx < x + w - 3; cx += 6) {
            if (rr() > 0.42) continue;
            if (rr() < 0.08 && live.length < 10) live.push([r2(cx), r2(cy), rr()]);
            else wins.push([r2(cx), r2(cy), 2.4, 3.2]);
          }
        }
      }
      x += w + r2(1 + rr() * 4);
    }
    const out = [PATH(boxes.join(""), fill, o != null ? { o } : {})];
    if (wins.length) out.push(PATH(dots(wins), litFill, { o: 0.62 }));
    for (const [cx, cy, k] of live) {
      out.push(
        RECT(cx, cy, 2.4, 3.2, litFill, {
          cls: "flicker",
          a: 0.8,
          dur: r2(5 + k * 9),
          dl: r2(-k * 12),
          o: 0.55,
        }),
      );
    }
    return out;
  };
  parts.push(
    ...row("sk-far", { y0: 150, hMin: 26, hMax: 62, wMin: 12, wMax: 26, fill: "@far", o: 0.55 }),
    ...row("sk-near", { y0: 152, hMin: 40, hMax: 104, wMin: 16, wMax: 34, fill: "@near", lit: true, litFill: "@win" }),
  );
  // An aircraft, blinking, crossing above the roofline.
  parts.push(
    G([CIRC(0, 0, 1.5, "#ff8a8a", { cls: "twinkle", a: 1, dur: 1.8, o: 0.7 })], {
      cls: "cross",
      x0: -30,
      x1: 300,
      a: 0.9,
      dur: 40,
      o: 0.5,
      tr: "translate(0 26)",
      hi: true,
    }),
    // The river: the skyline again, upside down, smeared, at the alpha the
    // text band allows.
    G([...row("sk-near", { y0: 152, hMin: 40, hMax: 104, wMin: 16, wMax: 34, fill: "@mirror" })], {
      tr: "translate(0 306) scale(1 -0.7)",
      hi: true,
    }),
    // Two glints on the river. They sit just under the horizon rather than
    // out in the middle of the bio, and they are the dimmest marks in the
    // scene — a bright band across a paragraph is a failed effect.
    ELL(136, 158, 180, 2.2, "#ffd9a0", { cls: "shimmer", tx: 26, a: 0.12, dur: 13, o: 0.06 }),
    ELL(136, 178, 150, 1.8, "#a8c8ff", { cls: "shimmer", tx: -20, a: 0.09, dur: 17, dl: -6, o: 0.045, hi: true }),
    RECT(0, 320, 272, 80, "@bottom"),
  );
  return parts;
}

function lighthouseScene() {
  const r = rng("lighthouse");
  const LX = 216;
  const LY = 46;
  const parts = [
    RECT(0, 0, 272, 400, "@sky"),
    CIRC(60, 40, 46, "@moonglow", { cls: "breathe", a: 0.5, dur: 13, o: 0.35 }),
    CIRC(60, 40, 11, "#e9f1ff", { o: 0.65 }),
  ];
  const stars = [];
  for (let i = 0; i < 34; i++) stars.push([r2(r() * 272), r2(r() * 96), 1, 1]);
  parts.push(PATH(dots(stars), "#dfe9ff", { o: 0.26 }));
  // The beam sweeps under everything else, so the tower stays crisp on top of
  // its own light. Two lobes, opposite, one revolution every nine seconds.
  parts.push(
    G(
      [
        PATH(`M${LX} ${LY}L${LX - 260} ${LY - 54}L${LX - 260} ${LY + 54}Z`, "@beam"),
        PATH(`M${LX} ${LY}L${LX + 260} ${LY - 40}L${LX + 260} ${LY + 40}Z`, "@beam2"),
      ],
      { cls: "sweep", origin: [LX, LY], dur: 9 },
    ),
    CIRC(LX, LY, 26, "@lamp", { cls: "breathe", a: 0.8, dur: 9, o: 0.5 }),
  );
  // Tower: tapered, banded, with a lantern room and a gallery rail.
  parts.push(
    PATH(`M${LX - 9} ${LY + 6}L${LX + 9} ${LY + 6}L${LX + 15} 150L${LX - 15} 150Z`, "@tower"),
    PATH(
      `M${LX - 10.2} ${LY + 26}L${LX + 10.2} ${LY + 26}L${LX + 11.6} ${LY + 48}L${LX - 11.6} ${LY + 48}Z` +
        `M${LX - 12.9} ${LY + 70}L${LX + 12.9} ${LY + 70}L${LX + 14.3} ${LY + 92}L${LX - 14.3} ${LY + 92}Z`,
      "@band",
    ),
    PATH(`M${LX - 12} ${LY + 6}L${LX + 12} ${LY + 6}L${LX + 12} ${LY + 10}L${LX - 12} ${LY + 10}Z`, "@tower"),
    PATH(`M${LX - 7} ${LY - 9}L${LX + 7} ${LY - 9}L${LX + 7} ${LY + 5}L${LX - 7} ${LY + 5}Z`, "@glass"),
    PATH(`M${LX - 9} ${LY - 9}L${LX + 9} ${LY - 9}L${LX} ${LY - 20}Z`, "@tower"),
    CIRC(LX, LY - 22, 1.6, "#ffe9b0", { cls: "twinkle", a: 0.9, dur: 3, o: 0.6 }),
    // Rocks at the base, dissolving before they reach the name.
    PATH(`M${LX - 34} 152c8-9 18-11 26-10c9 1 16 5 24 10Z`, "@rock"),
  );
  // The sea: four sine bands sliding at different rates. Wide enough that the
  // wrap never shows.
  for (const [y, amp, wave, phase, h, fill, tx, dur, o] of [
    [136, 2.2, 120, 0, 22, "@sea1", 16, 21, 0.85],
    [148, 2.8, 92, 1.4, 26, "@sea2", -13, 27, 0.7],
    [164, 3.2, 74, 2.6, 30, "@sea3", 10, 33, 0.45],
    [186, 3.6, 60, 0.7, 40, "@sea4", -8, 39, 0.28],
  ]) {
    parts.push(PATH(swell(y, amp, wave, phase, h), fill, { cls: "shift", tx, dur, o }));
  }
  parts.push(
    ELL(120, 136, 70, 1.6, "#ffe6b4", { cls: "shimmer", tx: 40, a: 0.28, dur: 12, o: 0.13 }),
    RECT(0, 300, 272, 100, "@deep"),
  );
  return parts;
}

export const CARD_SCENES = [
  // ---------- haunting ----------
  {
    id: "the-hollow",
    name: "The Hollow",
    group: "Haunting",
    // A wraith crossing bare moonlit woods, twice — once close and half seen,
    // once far and nearly not there at all. Its face is two sockets and
    // nothing else, which is the difference between eerie and cute.
    defs: [
      veil("sky", "#04060b", "#0a1120"),
      RG("moon", 206, 42, 20, [
        [0, "#f4f8ff", 1],
        [0.55, "#d2dfef", 1],
        [1, "#a9bbd3", 1],
      ]),
      RG("halo", 206, 42, 72, [
        [0, "#93aed1", 0.32],
        [0.42, "#7f97ba", 0.12],
        [1, "#7f97ba", 0],
      ]),
      fade("bark", "#02040a", 46, 190, 1, 0.05),
      fade("claw", "#04070e", 340, 412, 0.02, 0.32),
      LG("wraith", 0, 26, 0, 200, [
        [0, "#dceaf8", 0.66],
        [0.26, "#c2d6ec", 0.44],
        [0.58, "#aec6e0", 0.17],
        [1, "#aec6e0", 0],
      ]),
      LG("wraith2", 0, 14, 0, 128, [
        [0, "#d3e4f6", 0.48],
        [0.4, "#bcd2ea", 0.24],
        [1, "#aec6e0", 0],
      ]),
      RG("wraithhalo", 74, 69, 60, [
        [0, "#b9d6f2", 0.22],
        [0.5, "#9dbcdd", 0.08],
        [1, "#9dbcdd", 0],
      ]),
      RG("wraithhalo2", 196, 39, 34, [
        [0, "#b9d6f2", 0.14],
        [1, "#9dbcdd", 0],
      ]),
      fade("mist", "#a9bcd4", 112, 168, 0.15, 0),
      fade("floor", "#04070e", 300, 400, 0, 0.26),
    ],
    parts: hollowScene(),
  },
  {
    id: "witchlight",
    name: "Witchlight",
    group: "Haunting",
    // Six cold green lights hanging over black water, breathing but never
    // moving on. Drowned trees, their reflections, and reeds at your feet.
    defs: [
      veil("sky", "#03080a", "#06131a"),
      fade("trunk", "#02070a", 56, 160, 0.95, 0.35),
      fade("ripple", "#0a1c22", 150, 250, 0.3, 0),
      fade("gas", "#2f6b58", 76, 176, 0.2, 0),
      fade("gas2", "#1f5f6b", 56, 148, 0.16, 0),
      RGB("wispglow", [
        [0, "#8dffc0", 0.5],
        [0.3, "#4fe0a0", 0.2],
        [1, "#2fc98a", 0],
      ]),
      RGB("wispcore", [
        [0, "#e6fff0", 0.85],
        [0.45, "#8effc4", 0.4],
        [1, "#4fe0a0", 0],
      ]),
      fade("reed", "#031014", 330, 402, 0.05, 0.4),
      fade("bottom", "#02090c", 310, 400, 0, 0.22),
    ],
    parts: witchlightScene(),
  },
  {
    id: "eclipse",
    name: "Totality",
    group: "Haunting",
    // The two minutes when the birds go quiet. A black disc, a corona made of
    // two counter-turning fans of hairline rays, and one bead of light
    // creeping round the rim.
    defs: [
      veil("sky", "#070a18", "#0d1226"),
      RG("aureole", 136, 74, 118, [
        [0, "#b9c8ee", 0.3],
        [0.32, "#8e9fd0", 0.13],
        [1, "#6d7cb4", 0],
      ]),
      RG("corona", 136, 74, 82, [
        [0.4, "#ffffff", 0.6],
        [0.72, "#dbe6ff", 0.22],
        [1, "#b9caf5", 0],
      ]),
      RG("corona2", 136, 74, 106, [
        [0.32, "#fff4dd", 0.45],
        [0.7, "#ffd9a6", 0.14],
        [1, "#ffc98a", 0],
      ]),
      RG("rim", 136, 74, 38, [
        [0.9, "#ffffff", 0],
        [0.96, "#fff6e0", 0.9],
        [1, "#ffd9a0", 0],
      ]),
      fade("land", "#05070d", 130, 240, 0.9, 0),
    ],
    parts: eclipseScene(),
  },
  // ---------- woodland ----------
  {
    id: "windbreak",
    name: "Windbreak",
    group: "Woodland",
    // Looking up into a canopy while a wind works through it. The gust cycle
    // is mostly stillness, then a shove that overshoots and settles twice —
    // branches at the tips swing furthest, because they are carried by every
    // rotation above them as well as their own.
    defs: [
      veil("sky", "#1d2b1e", "#2a3a26"),
      RG("sun", 140, 34, 96, [
        [0, "#ffe9b0", 0.5],
        [0.4, "#e8c98a", 0.2],
        [1, "#c9ae74", 0],
      ]),
      fade("bark", "#241a12", 0, 150, 0.95, 0.35),
      fade("barkfar", "#1c2617", 0, 150, 0.7, 0.25),
      fade("leaf", "#7fae52", 0, 150, 0.85, 0.3),
      fade("leaffar", "#41682f", 0, 150, 0.7, 0.22),
      fade("hill", "#1a2a17", 110, 210, 0.55, 0),
    ],
    parts: windbreakScene(),
  },
  {
    id: "willowbank",
    name: "Willowbank",
    group: "Woodland",
    // A willow leaning over still water at the end of the day. Fifteen fronds,
    // each on its own clock, hanging from one heavy bough.
    defs: [
      veil("sky", "#2a2436", "#3b3145"),
      RG("sun", 58, 52, 26, [
        [0, "#ffe7bd", 0.95],
        [1, "#ffcf94", 0.7],
      ]),
      RG("sunglow", 58, 52, 62, [
        [0, "#ffbf85", 0.35],
        [1, "#ff9d6a", 0],
      ]),
      fade("far", "#20202f", 118, 210, 0.6, 0),
      fade("bark", "#2e2620", 14, 90, 0.92, 0.68),
      LG("frond", 0, 20, 0, 200, [
        [0, "#93a468", 0.88],
        [0.32, "#7d8f56", 0.56],
        [0.64, "#7d8f56", 0.18],
        [1, "#7d8f56", 0],
      ]),
      LG("frondback", 0, 20, 0, 190, [
        [0, "#3f4a2c", 0.85],
        [0.34, "#3f4a2c", 0.48],
        [0.68, "#3f4a2c", 0.14],
        [1, "#3f4a2c", 0],
      ]),
      fade("water", "#4a3f52", 146, 216, 0.34, 0),
      fade("water2", "#5d5064", 152, 200, 0.22, 0),
      fade("bottom", "#241f2c", 320, 400, 0, 0.2),
    ],
    parts: willowbankScene(),
  },
  {
    id: "snowline",
    name: "Snowline",
    group: "Woodland",
    // Three ridges at three distances, each paler than the one in front, with
    // cloud crossing the peaks and big soft snow that stops falling well
    // above anything you have to read.
    defs: [
      veil("sky", "#5c7290", "#8fa4bd", 0.38),
      RG("sun", 214, 34, 15, [
        [0, "#fff6e2", 0.9],
        [1, "#ffe0b0", 0.3],
      ]),
      fade("far", "#8fa2ba", 56, 220, 0.75, 0),
      fade("mid", "#5f7590", 86, 240, 0.85, 0),
      fade("near", "#33455e", 120, 300, 0.95, 0),
      fade("cloud", "#e6eef8", 60, 120, 0.26, 0.02),
      fade("drift", "#c6d4e6", 320, 400, 0, 0.1),
    ],
    parts: snowlineScene(),
  },
  // ---------- water ----------
  {
    id: "rainglass",
    name: "Rain on glass",
    group: "Water",
    // Inside, looking out, with the city thrown completely out of focus.
    // Fifteen drops run down the pane, each with the trail it left behind it,
    // and every run ends above the name rather than fading through it.
    defs: [
      veil("sky", "#0e1622", "#16202e"),
      RGB("bokeh", [
        [0, "#ffd9a0", 0.55],
        [0.5, "#e8a95c", 0.2],
        [1, "#c98a3c", 0],
      ]),
      RGB("bokeh2", [
        [0, "#a9d8ff", 0.5],
        [0.5, "#5c9bd8", 0.18],
        [1, "#3a6ea8", 0],
      ]),
      LG("glass", 0, 0, 0, 260, [
        [0, "#8fb4d4", 0.14],
        [0.45, "#8fb4d4", 0.07],
        [1, "#8fb4d4", 0],
      ]),
      fade("drop", "#dbeeff", 0, 200, 0.85, 0.25),
      fade("trail", "#c3ddf2", 0, 200, 0.4, 0.08),
      fade("sill", "#0a1018", 316, 400, 0, 0.24),
    ],
    parts: rainglassScene(),
  },
  {
    id: "kelpline",
    name: "Kelp forest",
    group: "Water",
    // Looking up through a kelp forest. Each stalk is three hinged segments,
    // so the sway builds toward the fronds instead of pivoting rigidly at the
    // seabed, and the shafts of surface light swing on their own clocks.
    defs: [
      veil("sky", "#04222c", "#073241"),
      LG("shaft", 0, 0, 0, 250, [
        [0, "#bff2ff", 0.5],
        [0.45, "#7fd8ee", 0.18],
        [1, "#5fc0d8", 0],
      ]),
      LG("kelp", 0, 60, 0, 404, [
        [0, "#2f7a4e", 0.85],
        [0.22, "#2a6f48", 0.4],
        [0.42, "#256040", 0.09],
        [0.78, "#1d5236", 0.07],
        [1, "#164028", 0.16],
      ]),
      RGB("bubble", [
        [0, "#dff8ff", 0.5],
        [0.7, "#a9e6f5", 0.25],
        [1, "#7fd0e6", 0],
      ]),
      fade("fish", "#03222b", 40, 110, 0.8, 0.5),
      fade("floor", "#021a22", 300, 400, 0, 0.24),
    ],
    parts: kelplineScene(),
  },
  {
    id: "koipond",
    name: "Koi pond",
    group: "Water",
    // Straight down into a pond. Three koi swim the top of the card with
    // tails that actually flutter, under lily pads that rock, through rings
    // opening where something touched the surface.
    defs: [
      veil("sky", "#0d2a2b", "#123a38"),
      fade("ring", "#cfeee6", 20, 150, 0.5, 0.2),
      fade("pad", "#2f6b45", 24, 150, 0.9, 0.55),
      fade("vein", "#123a28", 24, 150, 0.8, 0.4),
      fade("koi1", "#ff8a3d", 30, 130, 0.95, 0.7),
      fade("koi2", "#f2f2f2", 60, 150, 0.9, 0.6),
      fade("koi3", "#ffb066", 20, 90, 0.8, 0.5),
      fade("petal", "#ffd3e2", 10, 160, 0.8, 0.35),
      fade("deep", "#08201f", 300, 400, 0, 0.22),
    ],
    parts: koipondScene(),
  },
  // ---------- night ----------
  {
    id: "orrery",
    name: "Orrery",
    group: "Night",
    // A ringed giant with two moons: the far one passes BEHIND the planet and
    // is gone for a third of its orbit, which is what makes a flat drawing
    // read as depth. The nebula behind it is blurred and perfectly still.
    defs: [
      veil("sky", "#05061a", "#0a0b24"),
      RG("neb1", 66, 62, 78, [
        [0, "#7a4bd8", 0.6],
        [0.5, "#4c2f9e", 0.3],
        [1, "#2a1a63", 0],
      ]),
      RG("neb2", 186, 108, 96, [
        [0, "#c1568f", 0.45],
        [0.5, "#7a3468", 0.22],
        [1, "#3a1c40", 0],
      ]),
      RG("neb3", 126, 30, 70, [
        [0, "#3f7fd8", 0.42],
        [1, "#1d3a76", 0],
      ]),
      RG("planet", 174, 58, 54, [
        [0, "#f0c98d", 1],
        [0.45, "#d29a5c", 1],
        [1, "#8a5730", 1],
      ]),
      RG("shade", 190, 72, 44, [
        [0.55, "#000000", 0],
        [1, "#050414", 0.62],
      ]),
      LG("ring", 124, 60, 256, 86, [
        [0, "#c9b48f", 0.15],
        [0.3, "#efe0c2", 0.8],
        [0.62, "#bda887", 0.55],
        [1, "#c9b48f", 0.12],
      ]),
      fade("far", "#8fa8ff", 226, 246, 0.5, 0.1),
      BLUR("soft", 13),
    ],
    parts: orreryScene(),
  },
  {
    id: "night-city",
    name: "Night city",
    group: "Night",
    // Two depths of rooftops, water tanks and aerials, a few hundred lit
    // windows and ten that flick off and on again as somebody crosses a room.
    // The river below is the same skyline, upside down and smeared.
    defs: [
      veil("sky", "#0a1024", "#151c38"),
      fade("far", "#2a3352", 80, 156, 0.8, 0.5),
      fade("near", "#080c18", 40, 156, 0.96, 0.72),
      fade("win", "#ffd28a", 40, 152, 0.95, 0.55),
      fade("mirror", "#141b30", 152, 260, 0.18, 0),
      fade("bottom", "#070b16", 320, 400, 0, 0.2),
    ],
    parts: skylineScene(),
  },
  {
    id: "lighthouse",
    name: "Lighthouse",
    group: "Night",
    // A beam turning once every nine seconds over four bands of swell. The
    // lamp's flare shares the beam's clock, so the tower brightens exactly
    // when the light comes round to face you.
    defs: [
      veil("sky", "#060d1c", "#0c1730"),
      RG("moonglow", 60, 40, 46, [
        [0, "#cfe0ff", 0.4],
        [0.4, "#9fb6de", 0.14],
        [1, "#7f95bd", 0],
      ]),
      LG("beam", -44, 46, 216, 46, [
        [0, "#ffe9b0", 0],
        [0.72, "#ffe0a0", 0.09],
        [1, "#fff3d4", 0.28],
      ]),
      LG("beam2", 476, 46, 216, 46, [
        [0, "#ffe9b0", 0],
        [0.72, "#ffe0a0", 0.06],
        [1, "#fff3d4", 0.2],
      ]),
      RG("lamp", 216, 46, 26, [
        [0, "#fff6d8", 0.85],
        [0.4, "#ffdf9c", 0.3],
        [1, "#ffc96a", 0],
      ]),
      fade("tower", "#e8eef6", 26, 152, 0.9, 0.35),
      fade("band", "#c94a3c", 26, 152, 0.85, 0.35),
      fade("glass", "#fff0c0", 30, 60, 0.9, 0.7),
      fade("rock", "#0a1220", 130, 156, 0.75, 0.25),
      fade("sea1", "#1b2f52", 130, 170, 0.75, 0.3),
      fade("sea2", "#16273f", 142, 186, 0.6, 0.2),
      fade("sea3", "#121f33", 158, 206, 0.4, 0.08),
      fade("sea4", "#0e1828", 180, 240, 0.24, 0),
      fade("deep", "#050b16", 300, 400, 0, 0.22),
    ],
    parts: lighthouseScene(),
  },
];

export const CARD_SCENE_BY_ID = Object.fromEntries(CARD_SCENES.map((s) => [s.id, s]));

export const CARD_SCENE_GROUPS = [...new Set(CARD_SCENES.map((s) => s.group))].map((title) => ({
  title,
  ids: CARD_SCENES.filter((s) => s.group === title).map((s) => s.id),
}));

// cardScene resolves an id to its definition, or null. Fails CLOSED, exactly as
// cardEffect does: an id invented by a peer — or one from a newer build — paints
// nothing rather than anything surprising.
export function cardScene(id) {
  return CARD_SCENE_BY_ID[id] || null;
}
