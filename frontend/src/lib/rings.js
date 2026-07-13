// rings.js — the avatar-ring library.
//
// A ring is DATA. Building blocks, freely combined:
//   art     — a disc painted behind the avatar (spins unless `static`)
//   palette — that disc's colorway is a CONFIG, not twenty near-identical presets
//   orbit   — something circling you: a dot, or a rider (🐄 🚀, or your own picture)
//   halo    — a breathing glow
//   fx      — a live effect layer IN FRONT of your face (lib/fx.js + app.css):
//             snow, rain, embers, fireflies, meteors, lightning, ripples, glitch…
//
// Everything obeys the wearer's dials (speed / direction / glow / thickness),
// costs one short id on the wire, and renders through one component
// (AvatarRing.svelte). var(--c1)/var(--c2) are the wearer's own two profile
// colors, so the "yours" rings adapt to whoever wears them.

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
  { id: "obsidian", name: "Obsidian", stops: ["#0b0b10", "#3a3a4a 30%", "#6f6f8a 45%", "#0b0b10"] },
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
export const PAL_BY_ID = Object.fromEntries(PALETTES.map((p) => [p.id, p]));

export const RINGS = [
  { id: "", name: "None", kind: "none" },

  // ---------- the gradient ring: one effect, 27 colorways (a config) ----------
  { id: "spin", name: "Gradient", palette: true },
  { id: "theme", name: "Your colors", art: conic("var(--c1)", "var(--c2)", "var(--c1)") },
  { id: "theme-solid", name: "Your solid", static: true, art: "linear-gradient(var(--c1), var(--c1))" },

  // ---------- orbits: same ring, the RIDER is a config ----------
  { id: "orbit", name: "Orbit", orbit: { dot: "" }, band: true },
  { id: "comet", name: "Comet", orbit: { dot: "#bfe9ff", trail: true }, band: true },
  { id: "satellite", name: "Satellite", orbit: { dot: "#e8eaed" }, band: true },
  { id: "binary", name: "Binary stars", orbit: { dot: "#ffd76b", dot2: "#6ad2ff" }, band: true },
  { id: "theme-orbit", name: "Your orbit", orbit: { dot: "var(--c1)" }, band: true },
  { id: "theme-duo", name: "Your duo", orbit: { dot: "var(--c1)", dot2: "var(--c2)" }, band: true },

  // ---------- weather ----------
  { id: "snow", name: "Snowfall", fx: { kind: "fall", n: 8, glyphs: ["❄", "❅", "✻"], colors: ["#ffffff", "#dbeafe"], size: [2.4, 4.4], dur: [4, 9], drift: 8, glow: 3 }, band: "#cfe6ff" },
  { id: "blizzard", name: "Blizzard", fx: { kind: "fall", n: 8, colors: ["#ffffff", "#e6f2ff"], size: [1.4, 2.8], dur: [1.4, 3], drift: 26, glow: 2 }, band: "#9fc7ef" },
  { id: "rain", name: "Rain", fx: { kind: "rain", n: 10, colors: ["#8fd0ff", "#c7e8ff"], size: [2, 4], dur: [0.8, 1.6], drift: 5, glow: 2 }, band: "#4b7fa8" },
  { id: "thunder", name: "Thunderstorm", fx: { kind: "bolt", colors: ["#fff6c2"], dur: [2.6, 4.2], opacity: [0.75, 0.95], glow: 8 }, band: "#3a4256" },
  { id: "sakura", name: "Cherry blossom", tumble: true, fx: { kind: "fall", n: 10, glyphs: ["✿", "❀", "❁"], colors: ["#ffc2dd", "#ff9ec4", "#ffe3ef"], size: [2.4, 4.2], dur: [4, 8], drift: 16 }, band: "#ffb7d5" },
  { id: "autumn", name: "Autumn", tumble: true, fx: { kind: "fall", n: 9, glyphs: ["🍂", "🍁"], size: [3, 5], dur: [4, 8], drift: 18, glow: 0 }, band: "#c2703a" },
  { id: "ash", name: "Ashfall", fx: { kind: "fall", n: 9, colors: ["#9aa0a8", "#d7dbe0"], size: [1.4, 2.6], dur: [5, 11], drift: 12, glow: 2, opacity: [0.35, 0.8] }, band: "#6b7078" },

  // ---------- cosmic ----------
  { id: "meteor", name: "Meteor shower", fx: { kind: "streak", n: 4, colors: ["#ffffff", "#bfe9ff"], size: [1.8, 2.6], dur: [1.8, 3.6], glow: 6 }, band: "#20304d" },
  { id: "starfall", name: "Starfall", fx: { kind: "streak", n: 3, colors: ["#ffe9a8", "#fffbe6"], size: [2, 3], dur: [2.2, 4], glow: 7 }, band: "#3b2f5c" },
  { id: "stardust", name: "Stardust", fx: { kind: "twinkle", n: 9, glyphs: ["✦", "✧", "·"], colors: ["#ffffff", "#cdd8ff", "#ffe9a8"], size: [1.4, 3], dur: [2.4, 5], glow: 6 } },
  { id: "warp", name: "Warp speed", fx: { kind: "streak", n: 8, colors: ["#ffffff", "#a9d8ff"], size: [1.2, 2], dur: [0.7, 1.4], glow: 4 }, band: "#101a33" },
  { id: "galaxy", name: "Galaxy", art: conic("#0b0b25", "#6a3bd6 25%", "#ff6ad5 45%", "#2ad6ff 65%", "#0b0b25"), fx: { kind: "twinkle", n: 8, colors: ["#ffffff"], size: [1, 2], dur: [2, 4], glow: 4 } },
  { id: "solar", name: "Solar flare", art: conic("#b34700", "#ffd24d 25%", "#fffbe6 40%", "#ff8a00 60%", "#b34700"), fx: { kind: "rays", colors: ["rgba(255,200,80,.55)"], dur: [14, 14], opacity: [0.55, 0.55] } },
  { id: "eclipse-ring", name: "Eclipse", art: conic("#05060a 55%", "#ffbe5a 62%", "#ff7828 70%", "#05060a 80%"), fx: { kind: "rays", colors: ["rgba(255,190,90,.4)"], dur: [22, 22], opacity: [0.4, 0.4] } },
  { id: "moon", name: "Moonlight", halo: "#dfe8ff" },

  // ---------- fire & water ----------
  { id: "embers", name: "Embers", fx: { kind: "rise", n: 8, colors: ["#ff8a3d", "#ffb84d", "#ff5722"], size: [1.6, 3.2], dur: [1.8, 4], drift: 14, glow: 7 }, band: "#b3400f" },
  { id: "inferno", name: "Inferno", art: conic("#2b0500", "#ff2d00 25%", "#ffb300 45%", "#ff2d00 70%", "#2b0500"), fx: { kind: "rise", n: 10, colors: ["#ffd08a", "#ff7a1a"], size: [1.4, 2.8], dur: [1.2, 2.6], drift: 10, glow: 8 } },
  { id: "candle", name: "Candlelight", halo: "#ffb765", fx: { kind: "rise", n: 5, colors: ["#ffcf87"], size: [1.2, 2], dur: [2, 3.6], drift: 8, glow: 6 } },
  { id: "bubbles", name: "Bubbles", fx: { kind: "rise", n: 11, colors: ["#8fe6ff", "#d8f7ff"], size: [2, 5], dur: [3, 6.5], drift: 12, glow: 3, opacity: [0.35, 0.8] }, band: "#3fa9d1" },
  { id: "ripple", name: "Ripples", fx: { kind: "ripple", colors: ["#7fd8f7"], dur: [3, 3], opacity: [0.8, 0.8] } },
  { id: "sonar", name: "Sonar", fx: { kind: "ripple", colors: ["#4dff9e"], dur: [2.2, 2.2], opacity: [0.9, 0.9] }, band: "#1c5c3a" },
  { id: "toxic", name: "Toxic", art: conic("#123d0c", "#7cff3d 30%", "#e6ffb8 50%", "#123d0c"), fx: { kind: "rise", n: 7, colors: ["#b6ff6b"], size: [1.6, 3], dur: [2.5, 5], drift: 10, glow: 6, opacity: [0.3, 0.7] } },

  // ---------- cute ----------
  { id: "fireflies", name: "Fireflies", fx: { kind: "twinkle", n: 10, colors: ["#eaff8f", "#c8ff5a"], size: [1.8, 3.2], dur: [3, 6], drift: 14, glow: 8 } },
  { id: "hearts", name: "Hearts", fx: { kind: "rise", n: 8, glyphs: ["♥", "❥"], colors: ["#ff6b9d", "#ffb3c9"], size: [2.2, 4], dur: [2.6, 5], drift: 14, glow: 5 }, band: "#ff88b0" },
  { id: "notes", name: "Music", fx: { kind: "rise", n: 8, glyphs: ["♪", "♫", "♬"], colors: ["#c4a8ff", "#8fd0ff"], size: [2.4, 4], dur: [2.6, 5.5], drift: 16, glow: 5 }, band: "#8b6bd6" },
  { id: "confetti", name: "Confetti", tumble: true, fx: { kind: "fall", n: 9, colors: ["#ff5d5d", "#ffd24d", "#4dff88", "#4dd2ff", "#c46bff"], size: [1.8, 3.4], dur: [2, 4.5], drift: 20, glow: 0 } },
  { id: "sparkles", name: "Sparkles", fx: { kind: "twinkle", n: 8, glyphs: ["✨", "✦"], colors: ["#fff6c2", "#ffffff"], size: [1.6, 3], dur: [1.8, 3.6], glow: 6 } },
  { id: "theme-spark", name: "Your sparks", band: "var(--c1)", fx: { kind: "twinkle", n: 9, colors: ["var(--c1)", "var(--c2)"], size: [1.6, 3], glow: 5 } },

  // ---------- cyber ----------
  { id: "matrix-ring", name: "Matrix", fx: { kind: "matrix", n: 8, colors: ["#00ff6e", "#b6ffd8"], size: [1.6, 3], dur: [1.4, 3.2], drift: 2, glow: 5 }, band: "#0c8a4a" },
  { id: "scanner", name: "Scanner", fx: { kind: "scan", colors: ["rgba(80,255,190,.75)"], dur: [2.4, 2.4], opacity: [0.8, 0.8] }, band: "#22c39a" },
  { id: "glitch", name: "Glitch", art: conic("#0b0b12", "#ff2d75 18%", "#0b0b12 34%", "#22d3ee 52%", "#0b0b12 68%", "#ff2d75 86%", "#0b0b12"), fx: { kind: "scan", colors: ["rgba(255,45,117,.5)"], dur: [1.1, 1.1], opacity: [0.75, 0.75] } },
  { id: "holo-ring", name: "Holographic", art: "conic-gradient(from 200deg, #b6f8ff, #ffd6f7, #d9c8ff, #b6f8ff)", fx: { kind: "sheen", colors: ["rgba(255,255,255,.6)"], dur: [3.2, 3.2], opacity: [0.9, 0.9] } },
  { id: "pulse", name: "Pulse", halo: "" },

  // ---------- patterns ----------
  { id: "candy-ring", name: "Candy", art: "repeating-conic-gradient(from 0deg, #ff8fb1 0 18deg, #ffffff 18deg 36deg)" },
  { id: "barber", name: "Barber pole", art: "repeating-conic-gradient(from 0deg, #e0555b 0 20deg, #ffffff 20deg 40deg)" },
  { id: "checker", name: "Checker", art: "repeating-conic-gradient(from 0deg, #1d2025 0 15deg, #e8eaed 15deg 30deg)" },
  { id: "sparkline", name: "Dashes", art: "repeating-conic-gradient(from 0deg, transparent 0 12deg, #fff2a8 12deg 15deg)" },
];

export const RING_BY_ID = Object.fromEntries(RINGS.map((r) => [r.id, r]));

// The picker's shelves.
export const RING_GROUPS = [
  { title: "Gradient", ids: ["spin", "theme", "theme-solid"] },
  { title: "Orbits", ids: ["orbit", "comet", "satellite", "binary", "theme-orbit", "theme-duo"] },
  { title: "Weather", ids: ["snow", "blizzard", "rain", "thunder", "sakura", "autumn", "ash"] },
  { title: "Cosmic", ids: ["meteor", "starfall", "stardust", "warp", "galaxy", "solar", "eclipse-ring", "moon"] },
  { title: "Fire & water", ids: ["embers", "inferno", "candle", "bubbles", "ripple", "sonar", "toxic"] },
  { title: "Cute", ids: ["fireflies", "hearts", "notes", "confetti", "sparkles", "theme-spark"] },
  { title: "Cyber", ids: ["matrix-ring", "scanner", "glitch", "holo-ring", "pulse"] },
  { title: "Patterns", ids: ["candy-ring", "barber", "checker", "sparkline"] },
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
const LEGACY_RIDER = {
  "orbit-cow": "🐄", "orbit-penguin": "🐧", "orbit-car": "🚗", "orbit-rocket": "🚀",
  "orbit-ufo": "🛸", "orbit-duck": "🦆", "orbit-cat": "🐈", "orbit-pizza": "🍕",
  "orbit-shark": "🦈", "orbit-dino": "🦖", "orbit-ghost": "👻", "orbit-crown": "👑",
  "orbit-bee": "🐝", "orbit-skull": "💀", "orbit-moon": "🌙", "orbit-custom": "",
};

// Rings saved when each colorway was its own preset ("gold" → the Gradient ring
// wearing the gold palette).
const LEGACY_PAL = {
  gold: "gold", silver: "silver", bronze: "bronze", platinum: "platinum",
  "rose-gold": "rose-gold", obsidian: "obsidian", gem: "gem", ruby: "ruby",
  emerald: "emerald", sapphire: "sapphire", amethyst: "amethyst", topaz: "topaz",
  opal: "opal", frost: "frost", glacier: "glacier", magma: "magma", ember: "ember",
  storm: "storm", voltage: "voltage", aurora: "aurora", rainbow: "rainbow",
  "nebula-ring": "nebula", neon: "neon", synth: "synth", vapor: "vapor",
};

export const hasRider = (id) => !!RING_BY_ID[id]?.orbit;
export const hasPalette = (id) => !!RING_BY_ID[id]?.palette;

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
