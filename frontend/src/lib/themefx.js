// themefx.js — the catalogue of stackable visual effects.
//
// An effect is a SEPARATE axis from the theme pack: it composes with whichever
// pack is active rather than belonging to one, so "Gruvbox with snow" and
// "Paper with scanlines" are both reachable. The pref is one string id in
// S.prefs.themeFx ("" = none), device-local like every other appearance pref.
//
// Each effect is built from up to two things, and most use both:
//
//   sheets    — how many tiled-gradient layers app.css paints for it. Cheap
//               and dense: one composited texture the GPU slides, however many
//               specks are drawn on it. But every speck on a sheet moves at
//               one speed, and the tile repeats, so a sheet on its own reads
//               as a moving wallpaper.
//   particle  — a descriptor for the shared engine (lib/fx.js), which gives
//               every speck its OWN size, speed, drift and starting phase.
//               Irregular, but one element each, so it stays sparse.
//
// Sheets behind, particles in front, is the answer to both problems: the
// far field is dense and free, the near field is where the eye looks and
// nothing in it is on a grid or in step. Games have done parallax weather this
// way for thirty years.
//
// Nothing here is fetched. Every speck is a gradient or a glyph the browser
// already has; there is no image, no sprite sheet and no animation library
// anywhere in the feature.

// Particle colours defer to --fx-ink, the same token the sheets use, so a near
// flake is the same colour as the far ones on both dark and light grounds. The
// engine writes this straight into `--c`, and custom properties substitute
// lazily, so it resolves against whatever ground is active.
const INK = "rgb(var(--fx-ink))";

// `size` is the engine's px range before its own scaling; `drift` is how far a
// speck wanders sideways over one fall, which is the single biggest reason
// these do not read as a grid. `glow: 0` everywhere — a blurred shadow on a
// full-window field is the one thing app.css's FX section says never to do.
const PARTICLE = {
  snow: {
    kind: "fall",
    n: 18,
    colors: [INK],
    size: [2.5, 5.5],
    dur: [9, 20],
    opacity: [0.4, 0.85],
    drift: 90,
    glow: 0,
  },
  rain: {
    // "rain" is an engine kind, not just a name: it draws the speck as an
    // elongated drop instead of a dot.
    kind: "rain",
    n: 16,
    colors: [INK],
    size: [4, 9],
    dur: [0.9, 1.9],
    opacity: [0.3, 0.6],
    drift: 26,
    glow: 0,
  },
  embers: {
    kind: "rise",
    n: 14,
    // The one field that can't be --fx-ink: a grey ember is not an ember. It
    // gets its own trio of tokens instead, flipped for the daylight packs the
    // same way and for the same reason — three near-white oranges over a bright
    // page were a field of nothing at 1.4:1. See app.css, --fx-ember-*.
    colors: ["var(--fx-ember-mid)", "var(--fx-ember-low)", "var(--fx-ember-hot)"],
    size: [1.5, 3.5],
    dur: [7, 16],
    opacity: [0.4, 0.85],
    drift: 70,
    glow: 0,
  },
  leaves: {
    kind: "fall",
    // Deliberately NOT `tumble: true`. The leaves DO tumble, but app.css folds
    // the spin into the fall keyframe on the element instead of animating each
    // speck's ::before — measured, that is the difference between 11% and 36%
    // of a CPU core at window size. The reasoning is written out beside the
    // .fx-overlay.fxo-leaves rule.
    n: 24,
    glyphs: ["🍂", "🍁"],
    colors: ["#d97706"],
    size: [7, 13],
    dur: [11, 20],
    opacity: [0.35, 0.7],
    drift: 40,
    glow: 0,
  },
  // Spring's half of Seasonal: the leaves' engine and fall, pink and fewer.
  petals: {
    kind: "fall",
    n: 16,
    glyphs: ["🌸"],
    colors: ["#f9a8d4"],
    size: [6, 11],
    dur: [12, 21],
    opacity: [0.35, 0.65],
    drift: 50,
    glow: 0,
  },
};

// Six, not sixteen. Each one is a different KIND of motion — falling,
// sheeting, twinkling, rising, rolling, tumbling — so no two of them read as
// the same idea in a different colour. Picking an effect never changes your
// pack, and picking a pack never changes your effect.
//
// Seasonal is the seventh choice and not an effect of its own: it is one of
// the others, chosen by this device's clock (resolveFx). It used to be a
// separate switch that drew only on the Rail layout's guild column, so on the
// default layout and on every phone it did nothing at all.
export const THEME_FX = [
  { id: "", label: "None", note: "No overlay", sheets: 0 },
  { id: "snow", label: "Snow", note: "Drifting flakes", sheets: 3, particle: PARTICLE.snow },
  { id: "rain", label: "Rain", note: "Three sheets, falling", sheets: 3, particle: PARTICLE.rain },
  { id: "stars", label: "Starfield", note: "Slow, faint, twinkling", sheets: 3 },
  { id: "embers", label: "Embers", note: "Warm motes rising", sheets: 3, particle: PARTICLE.embers },
  { id: "crt", label: "CRT", note: "Scanlines, roll, flicker", sheets: 2 },
  // No sheets: a tiled gradient has no per-speck rotation, and a leaf that
  // does not turn over is confetti.
  { id: "leaves", label: "Leaves", note: "Tumbling, autumn", sheets: 0, particle: PARTICLE.leaves },
  { id: "seasonal", label: "Seasonal", note: "Snow, petals or leaves, by month", sheets: 0 },
];

// Not offered on its own: Seasonal's spring.
const PETALS = { id: "petals", label: "Petals", note: "", sheets: 0, particle: PARTICLE.petals };

const BY_ID = new Map([...THEME_FX, PETALS].map((f) => [f.id, f]));

// A pref saved by a newer build (or hand-edited) must not paint an unstyled
// layer, so anything unrecognised reads as off.
export function validFx(id) {
  return BY_ID.has(id) && id !== "petals" ? id : "";
}

// What Seasonal is this month, by this device's clock. The same months the
// rail drift used: snow in December and January, petals in March and April,
// leaves in October and November, and nothing in between.
export function seasonFx(date = new Date()) {
  const m = date.getMonth();
  if (m === 11 || m === 0) return "snow";
  if (m === 2 || m === 3) return "petals";
  if (m === 9 || m === 10) return "leaves";
  return "";
}

// The effect actually drawn for a pref: Seasonal resolved, the rest as is.
export function resolveFx(id, date = new Date()) {
  const v = validFx(id);
  return v === "seasonal" ? seasonFx(date) : v;
}

// The spec for a pref OR for an already-resolved id ("petals" included).
export function fxSpec(id) {
  return BY_ID.get(id === "petals" ? id : resolveFx(id)) || BY_ID.get("");
}
