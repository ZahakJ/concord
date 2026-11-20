// rings.js — the avatar-ring library.
//
// A ring is DATA. Building blocks, freely combined:
//   art     — a disc painted behind the avatar (spins unless `static`)
//   palette — that disc's colorway is a CONFIG, not twenty near-identical presets
//   orbit   — something circling you: a dot, or a rider (🐄 🚀, or your own picture)
//   halo    — a breathing glow
//   fx      — a live effect layer IN FRONT of your face (lib/fx.js + app.css):
//             snow, rain, embers, sparkles, a holographic sheen…
//
// Everything obeys the wearer's dials (speed / direction / glow / thickness),
// costs one short id on the wire, and renders through one component
// (AvatarRing.svelte). var(--c1)/var(--c2) are the wearer's own two profile
// colors, so the "yours" rings adapt to whoever wears them.

import { table } from "./lookup.js";
import { ringId } from "./cosmeticalias.js";

const conic = (...stops) => `conic-gradient(from 0deg, ${stops.join(", ")})`;

// PALETTES — the colorways for the Gradient ring. These used to be twenty
// separate presets that differed only in their stops; they're one effect with a
// config, which is what they always were.
export const PALETTES = [
  { id: "yours", name: "Your colors", stops: ["var(--c1)", "var(--c2)", "var(--c1)"] },
  { id: "gold", name: "Gold", stops: ["#8a6b1f", "#f5e08a 20%", "#d9a93f 45%", "#fff3c4 60%", "#d9a93f 80%", "#8a6b1f"] },
  { id: "silver", name: "Silver", stops: ["#6f7783", "#e8edf5 25%", "#aeb7c4 50%", "#ffffff 65%", "#6f7783"] },
  { id: "bronze", name: "Bronze", stops: ["#6b3f1d", "#d08b4e 30%", "#f0b880 55%", "#6b3f1d"] },
  { id: "platinum", name: "Platinum", stops: ["#cfd6e2", "#ffffff 20%", "#9fb0c9 45%", "#ffffff 70%", "#cfd6e2"] },
  { id: "rose-gold", name: "Rose gold", stops: ["#8c4a44", "#f0b8ab 30%", "#e8a08f 55%", "#8c4a44"] },
  // The specular band used to top out at #6f6f8a, which is a highlight against
  // the default ground and nothing at all against Nord's or Dracula's — 2.5:1,
  // so on two of the shipped dark packs the "obsidian" ring was a black ring on
  // a nearly black page. Volcanic glass has a sharp sheen; this is that sheen.
  { id: "obsidian", name: "Obsidian", stops: ["#0b0b10", "#3a3a4a 30%", "#9a9ab8 45%", "#0b0b10"] },
  { id: "gem", name: "Diamond", stops: ["#b9f2ff", "#ffffff 12%", "#b9f2ff 24%", "#7ad7f0 40%", "#ffffff 55%", "#b9f2ff 70%", "#7ad7f0 88%", "#b9f2ff"] },
  { id: "ruby", name: "Ruby", stops: ["#6b0f1a", "#ff4d6d 30%", "#ffb3c1 50%", "#6b0f1a"] },
  { id: "emerald", name: "Emerald", stops: ["#06402b", "#2fd08a 30%", "#b8ffdf 52%", "#06402b"] },
  { id: "sapphire", name: "Sapphire", stops: ["#08205e", "#3d7dd6 35%", "#bcd9ff 55%", "#08205e"] },
  { id: "amethyst", name: "Amethyst", stops: ["#3a1060", "#a06bff 35%", "#e6d2ff 55%", "#3a1060"] },
  { id: "topaz", name: "Topaz", stops: ["#6b4300", "#ffbf3d 35%", "#ffe9b0 55%", "#6b4300"] },
  { id: "opal", name: "Opal", stops: ["#b6f8ff", "#ffd6f7 25%", "#d9c8ff 50%", "#c8ffe0 75%", "#b6f8ff"] },
  { id: "frost", name: "Frost", stops: ["#6aa5c9", "#eafaff 30%", "#bfe3ff 55%", "#6aa5c9"] },
  { id: "glacier", name: "Glacier", stops: ["#12405c", "#7fd8f7 35%", "#ffffff 55%", "#12405c"] },
  { id: "magma", name: "Magma", stops: ["#1a0806", "#b3260c 35%", "#ff8a3d 55%", "#1a0806"] },
  { id: "ember", name: "Ember", stops: ["#5a1a06", "#ff7a1a 30%", "#ffd08a 50%", "#5a1a06"] },
  { id: "storm", name: "Storm", stops: ["#1b2130", "#6f8bb5 30%", "#eaf2ff 45%", "#1b2130 60%"] },
  { id: "voltage", name: "Voltage", stops: ["#05203a", "#22d3ee 20%", "#ffffff 30%", "#22d3ee 45%", "#05203a 70%"] },
  { id: "aurora", name: "Aurora", stops: ["#35e0c8", "#6f7cff", "#a06bff", "#35e0c8"] },
  { id: "rainbow", name: "Rainbow", stops: ["#ff4d4d", "#ffa64d", "#ffe14d", "#4dff88", "#4dd2ff", "#7a6ff0", "#d94dff", "#ff4d4d"] },
  { id: "nebula", name: "Nebula", stops: ["#12061f", "#ff5ab4 30%", "#5a8cff 55%", "#12061f"] },
  { id: "eclipse", name: "Eclipse", stops: ["#05060a 55%", "#ffbe5a 62%", "#ff7828 70%", "#05060a 80%"] },
  { id: "neon", name: "Neon", stops: ["#29d5e8", "#d640d6 50%", "#29d5e8"] },
  { id: "synth", name: "Synthwave", stops: ["#2b0a4a", "#ff3cc8 30%", "#ffa63d 55%", "#2b0a4a"] },
  { id: "vapor", name: "Vaporwave", stops: ["#ff9ae0", "#a48bff 45%", "#6ee7ff", "#ff9ae0"] },
];
export const PAL_BY_ID = table(Object.fromEntries(PALETTES.map((p) => [p.id, p])));

export const RINGS = [
  { id: "", name: "None", kind: "none" },

  // ---------- the gradient ring: one effect, 27 colorways (a config) ----------
  { id: "spin", name: "Gradient", palette: true },
  { id: "theme", name: "Your colors", art: conic("var(--c1)", "var(--c2)", "var(--c1)") },

  // ---------- orbits: same ring, the RIDER is a config ----------
  { id: "orbit", name: "Orbit", orbit: { dot: "" }, band: true },
  { id: "comet", name: "Comet", orbit: { dot: "#bfe9ff", trail: true }, band: true },

  // ---------- weather ----------
  { id: "snow", name: "Snowfall", fx: { kind: "fall", n: 8, glyphs: ["❄", "❅", "✻"], colors: ["#ffffff", "#dbeafe"], size: [2.4, 4.4], dur: [4, 9], drift: 8, glow: 3 }, band: "#cfe6ff" },
  { id: "rain", name: "Rain", fx: { kind: "rain", n: 10, colors: ["#8fd0ff", "#c7e8ff"], size: [2, 4], dur: [0.8, 1.6], drift: 5, glow: 2 }, band: "#4b7fa8" },
  { id: "sakura", name: "Cherry blossom", tumble: true, fx: { kind: "fall", n: 10, glyphs: ["✿", "❀", "❁"], colors: ["#ffc2dd", "#ff9ec4", "#ffe3ef"], size: [2.4, 4.2], dur: [4, 8], drift: 16 }, band: "#ffb7d5" },

  // ---------- cosmic ----------
  { id: "galaxy", name: "Galaxy", art: conic("#0b0b25", "#6a3bd6 25%", "#ff6ad5 45%", "#2ad6ff 65%", "#0b0b25"), fx: { kind: "twinkle", n: 8, colors: ["#ffffff"], size: [1, 2], dur: [2, 4], glow: 4 } },

  // ---------- fire & light ----------
  { id: "embers", name: "Embers", fx: { kind: "rise", n: 8, colors: ["#ff8a3d", "#ffb84d", "#ff5722"], size: [1.6, 3.2], dur: [1.8, 4], drift: 14, glow: 7 }, band: "#b3400f" },
  { id: "sparkles", name: "Sparkles", fx: { kind: "twinkle", n: 8, glyphs: ["✨", "✦"], colors: ["#fff6c2", "#ffffff"], size: [1.6, 3], dur: [1.8, 3.6], glow: 6 } },
  { id: "holo-ring", name: "Holographic", art: "conic-gradient(from 200deg, #b6f8ff, #ffd6f7, #d9c8ff, #b6f8ff)", fx: { kind: "sheen", colors: ["rgba(255,255,255,.6)"], dur: [3.2, 3.2], opacity: [0.9, 0.9] } },
  { id: "pulse", name: "Pulse", halo: "" },
];

export const RING_BY_ID = table(Object.fromEntries(RINGS.map((r) => [r.id, r])));

// The picker's shelves.
export const RING_GROUPS = [
  { title: "Gradient", ids: ["spin", "theme"] },
  { title: "Orbits", ids: ["orbit", "comet"] },
  { title: "Weather", ids: ["snow", "rain", "sakura"] },
  { title: "Cosmic", ids: ["galaxy", "sparkles"] },
  { title: "Fire & light", ids: ["embers", "holo-ring", "pulse"] },
];
// Riders: what goes around you. Any orbit ring can carry one — an emoji from
// this shelf, or a picture you upload. No rider = the classic dot.
export const SATELLITES = [
  "🐄", "🐧", "🚗", "🚀", "🛸", "🦆", "🐈", "🍕", "🦈", "🦖", "👻", "👑",
  "🐝", "💀", "🌙", "🐢", "🦀", "🍩", "⚡", "🎧", "🏀", "🐙", "🔥", "🍺",
  "🐸", "🦄", "🍄", "☕", "🎸", "🛹", "🪐", "🧊",
];

// Rings saved before the orbiting-friends presets collapsed into one Orbit ring
// with a rider config. Old profiles keep their cow.
const LEGACY_RIDER = table({
  "orbit-cow": "🐄", "orbit-penguin": "🐧", "orbit-car": "🚗", "orbit-rocket": "🚀",
  "orbit-ufo": "🛸", "orbit-duck": "🦆", "orbit-cat": "🐈", "orbit-pizza": "🍕",
  "orbit-shark": "🦈", "orbit-dino": "🦖", "orbit-ghost": "👻", "orbit-crown": "👑",
  "orbit-bee": "🐝", "orbit-skull": "💀", "orbit-moon": "🌙", "orbit-custom": "",
});

// Rings saved when each colorway was its own preset ("gold" → the Gradient ring
// wearing the gold palette).
const LEGACY_PAL = table({
  gold: "gold", silver: "silver", bronze: "bronze", platinum: "platinum",
  "rose-gold": "rose-gold", obsidian: "obsidian", gem: "gem", ruby: "ruby",
  emerald: "emerald", sapphire: "sapphire", amethyst: "amethyst", topaz: "topaz",
  opal: "opal", frost: "frost", glacier: "glacier", magma: "magma", ember: "ember",
  storm: "storm", voltage: "voltage", aurora: "aurora", rainbow: "rainbow",
  "nebula-ring": "nebula", neon: "neon", synth: "synth", vapor: "vapor",
});

export const hasRider = (id) => !!RING_BY_ID[ringId(id)]?.orbit;
export const hasPalette = (id) => !!RING_BY_ID[ringId(id)]?.palette;

// Everything AvatarRing needs to paint one ring: the palette it wears, the rider
// going around it, and the wearer's own colors.
export function ringArt(id, c1 = "", c2 = "", sat = "", pal = "") {
  let ring = id;
  let rider = sat;
  let palette = pal;
  if (LEGACY_RIDER[id] !== undefined) {
    rider = rider || LEGACY_RIDER[id];
    ring = "orbit";
  } else if (!RING_BY_ID[id] && LEGACY_PAL[id]) {
    palette = palette || LEGACY_PAL[id];
    ring = "spin";
  }
  // A ring this build no longer has is drawn as the kept ring nearest to it
  // (lib/cosmeticalias.js). The profile keeps the id it had.
  ring = ringId(ring);
  const r = RING_BY_ID[ring];
  if (!r || !ring) return null;
  const orbit = r.orbit ? { ...r.orbit, sat: rider || "" } : null;
  return {
    ...r,
    art: r.palette ? conic(...(PAL_BY_ID[palette] || PAL_BY_ID.yours).stops) : r.art,
    orbit,
    // A rider big enough to notice must pass IN FRONT of your face, not vanish
    // behind your head.
    front: !!orbit?.sat,
    isImg: !!(orbit?.sat && orbit.sat.startsWith("data:")),
    vars: `--c1:${c1 || "var(--accent)"};--c2:${c2 || c1 || "var(--accent-hover)"};`,
  };
}
