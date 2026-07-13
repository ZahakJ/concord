// banners.js — the profile-banner preset library.
//
// Same idea as rings.js: a preset is DATA. `base` is the painted background,
// `fx` is a live effect layer from the shared engine (lib/fx.js + app.css) —
// snowfall, meteors, lightning, lava-lamp blobs, a synthwave grid, an
// equalizer dancing along the bottom. `drift` slowly pans the base so even the
// calm ones breathe. The whole thing travels as "preset:galaxy" — a dozen
// bytes instead of a 256KB image.
//
// Rendered by Banner.svelte, which is what the picker, the profile editor's
// live preview and everyone else's profile card all use — so what you pick is
// exactly what they see.

export const BANNERS = [
  // ---------- cosmic ----------
  {
    id: "galaxy",
    name: "Galaxy",
    group: "Cosmic",
    base: "radial-gradient(120% 140% at 20% 10%, #2b1a5e 0%, #120a2e 45%, #05060f 100%)",
    fx: { kind: "twinkle", n: 12, colors: ["#ffffff", "#cdd8ff", "#ffe9a8"], size: [1, 2.6], dur: [2, 6], drift: 6, glow: 5 },
  },
  {
    id: "meteors",
    name: "Meteor shower",
    group: "Cosmic",
    base: "linear-gradient(180deg, #060a1c 0%, #101a3d 60%, #1d2a52 100%)",
    fx: { kind: "streak", n: 7, colors: ["#ffffff", "#bfe9ff"], size: [1.6, 2.6], dur: [1.6, 3.4], glow: 7 },
  },
  {
    id: "warp",
    name: "Warp speed",
    group: "Cosmic",
    base: "radial-gradient(circle at 50% 50%, #1b2a5c 0%, #05060f 70%)",
    fx: { kind: "streak", n: 11, colors: ["#ffffff", "#a9d8ff", "#d7c4ff"], size: [1.2, 2.2], dur: [0.6, 1.5], glow: 5 },
  },
  {
    id: "nebula",
    name: "Nebula",
    group: "Cosmic",
    base: "linear-gradient(135deg, #16062b, #2b0a4a 55%, #06060f)",
    fx: { kind: "blobs", n: 5, colors: ["#ff5ab4", "#5a8cff", "#a06bff"], size: [5, 11], dur: [9, 16], drift: 40, opacity: [0.35, 0.6] },
  },
  {
    id: "aurora",
    name: "Aurora",
    group: "Cosmic",
    drift: true,
    base: "linear-gradient(115deg, #041423 0%, #0b3b46 30%, #17705e 50%, #2f9e7c 62%, #0b2740 80%, #04101c 100%)",
    fx: { kind: "twinkle", n: 12, colors: ["#ffffff"], size: [0.8, 1.8], dur: [2.5, 6], glow: 4, opacity: [0.3, 0.8] },
  },
  {
    id: "solar",
    name: "Solar",
    group: "Cosmic",
    base: "radial-gradient(90% 160% at 50% 120%, #ffd24d 0%, #ff8a00 32%, #b3320a 60%, #3a0d05 100%)",
    fx: { kind: "rays", colors: ["rgba(255,220,140,.35)"], dur: [26, 26], opacity: [0.35, 0.35] },
  },
  {
    id: "eclipse",
    name: "Eclipse",
    group: "Cosmic",
    base: "radial-gradient(circle at 50% 50%, #000 26%, #ff8a2b 27%, #ffd08a 29%, #2a1a3d 42%, #07060e 100%)",
    fx: { kind: "twinkle", n: 20, colors: ["#ffffff"], size: [0.8, 1.8], dur: [2, 5], glow: 4 },
  },
  {
    id: "deepspace",
    name: "Deep space",
    group: "Cosmic",
    base: "radial-gradient(140% 120% at 80% 20%, #1a1040 0%, #070713 55%, #000 100%)",
    fx: { kind: "twinkle", n: 14, colors: ["#ffffff", "#9fb6ff"], size: [0.7, 1.8], dur: [3, 8], glow: 3, opacity: [0.2, 0.9] },
  },

  // ---------- weather ----------
  {
    id: "snowfall",
    name: "Snowfall",
    group: "Weather",
    base: "linear-gradient(180deg, #0d1b2e 0%, #21405f 60%, #4a6f96 100%)",
    fx: { kind: "fall", n: 12, glyphs: ["❄", "❅", "•"], colors: ["#ffffff", "#dbeafe"], size: [1.6, 3.6], dur: [4, 10], drift: 26, glow: 3 },
  },
  {
    id: "blizzard",
    name: "Blizzard",
    group: "Weather",
    base: "linear-gradient(180deg, #37485c 0%, #6d84a0 100%)",
    fx: { kind: "fall", n: 14, colors: ["#ffffff", "#eaf3ff"], size: [1.2, 2.6], dur: [1.2, 2.8], drift: 60, glow: 2 },
  },
  {
    id: "rain",
    name: "Rain",
    group: "Weather",
    base: "linear-gradient(180deg, #172233 0%, #2c3e52 70%, #3d5468 100%)",
    fx: { kind: "rain", n: 14, colors: ["#8fd0ff", "#c7e8ff"], size: [3, 7], dur: [0.6, 1.3], drift: 10, glow: 2, opacity: [0.35, 0.85] },
  },
  {
    id: "thunder",
    name: "Thunderstorm",
    group: "Weather",
    base: "linear-gradient(180deg, #10141f 0%, #232c40 60%, #39435c 100%)",
    fx: { kind: "bolt", colors: ["#fff6c2"], dur: [3, 5], opacity: [0.8, 0.95], glow: 10 },
  },
  {
    id: "sakura",
    name: "Cherry blossom",
    group: "Weather",
    tumble: true,
    base: "linear-gradient(160deg, #2a1526 0%, #6b2a4a 45%, #b35c7e 100%)",
    fx: { kind: "fall", n: 14, glyphs: ["✿", "❀", "❁"], colors: ["#ffc2dd", "#ff9ec4", "#ffe3ef"], size: [2, 4], dur: [4, 9], drift: 40 },
  },
  {
    id: "autumn",
    name: "Autumn",
    group: "Weather",
    tumble: true,
    base: "linear-gradient(160deg, #2a1a0c 0%, #7a3f16 55%, #c47a2b 100%)",
    fx: { kind: "fall", n: 11, glyphs: ["🍂", "🍁"], size: [3, 5.5], dur: [4, 9], drift: 46, glow: 0 },
  },
  {
    id: "sunrise",
    name: "Sunrise",
    group: "Weather",
    drift: true,
    base: "linear-gradient(180deg, #2b1a4d 0%, #8a3d63 40%, #e0714f 70%, #ffc07a 100%)",
    fx: { kind: "rays", colors: ["rgba(255,220,170,.22)"], dur: [30, 30], opacity: [0.25, 0.25] },
  },

  // ---------- nature ----------
  {
    id: "fireflies",
    name: "Fireflies",
    group: "Nature",
    base: "linear-gradient(180deg, #05130d 0%, #0d2a1c 55%, #143d29 100%)",
    fx: { kind: "twinkle", n: 14, colors: ["#eaff8f", "#c8ff5a"], size: [1.6, 3.4], dur: [3, 7], drift: 30, glow: 9 },
  },
  {
    id: "ocean",
    name: "Ocean",
    group: "Nature",
    base: "linear-gradient(180deg, #04263f 0%, #0a5a7a 60%, #12a0a8 100%)",
    fx: { kind: "waves", colors: ["rgba(160,240,255,.35)", "rgba(90,200,230,.3)", "rgba(255,255,255,.18)"], size: [26, 44], dur: [4, 7], drift: 18, opacity: [0.25, 0.45] },
  },
  {
    id: "underwater",
    name: "Underwater",
    group: "Nature",
    base: "linear-gradient(180deg, #062b45 0%, #0a4a63 100%)",
    fx: { kind: "rise", n: 11, colors: ["#8fe6ff", "#d8f7ff"], size: [1.6, 5], dur: [3, 8], drift: 26, glow: 4, opacity: [0.3, 0.75] },
  },
  {
    id: "campfire",
    name: "Campfire",
    group: "Nature",
    base: "radial-gradient(90% 120% at 50% 115%, #ff8a3d 0%, #b3400f 30%, #3d1206 62%, #140704 100%)",
    fx: { kind: "rise", n: 11, colors: ["#ff8a3d", "#ffb84d", "#ff5722"], size: [1.4, 3.4], dur: [1.6, 4], drift: 30, glow: 8 },
  },
  {
    id: "lava",
    name: "Lava",
    group: "Nature",
    base: "linear-gradient(180deg, #1a0806 0%, #3d0f06 60%, #7a1d08 100%)",
    fx: { kind: "blobs", n: 5, colors: ["#ff5722", "#ffb300", "#ff2d00"], size: [5, 10], dur: [7, 13], drift: 34, opacity: [0.5, 0.75] },
  },
  {
    id: "forest",
    name: "Forest",
    group: "Nature",
    drift: true,
    base: "linear-gradient(160deg, #06170f 0%, #0f3d24 50%, #1f6b3c 100%)",
    fx: { kind: "fall", n: 12, glyphs: ["🍃"], size: [2.6, 4.4], dur: [5, 11], drift: 44, glow: 0, opacity: [0.4, 0.8] },
  },
  {
    id: "toxic",
    name: "Toxic",
    group: "Nature",
    base: "linear-gradient(180deg, #0b2306 0%, #1d4a0c 60%, #2f7a12 100%)",
    fx: { kind: "rise", n: 12, colors: ["#b6ff6b", "#e6ffb8"], size: [2, 5], dur: [3, 7], drift: 24, glow: 8, opacity: [0.3, 0.7] },
  },

  // ---------- cyber ----------
  {
    id: "matrix",
    name: "Matrix",
    group: "Cyber",
    base: "linear-gradient(180deg, #01110a 0%, #021a0f 100%)",
    fx: { kind: "matrix", n: 20, colors: ["#00ff6e", "#b6ffd8"], size: [3, 7], dur: [1.2, 3.4], drift: 0, glow: 6, opacity: [0.35, 1] },
  },
  {
    id: "synthwave",
    name: "Synthwave",
    group: "Cyber",
    base: "linear-gradient(180deg, #2b0a4a 0%, #6a1d6b 42%, #ff3cc8 58%, #ffa63d 78%, #1a0530 100%)",
    fx: { kind: "grid", colors: ["rgba(255,60,200,.55)"], dur: [3, 3], opacity: [0.55, 0.55] },
  },
  {
    id: "vaporwave",
    name: "Vaporwave",
    group: "Cyber",
    base: "radial-gradient(closest-side circle at 50% 62%, #ffb35c 0 22%, #ff6ad5 22% 34%, transparent 35%), linear-gradient(180deg, #2d1b5e 0%, #7a3d94 55%, #ff9ae0 100%)",
    fx: { kind: "grid", colors: ["rgba(110,231,255,.5)"], dur: [3.4, 3.4], opacity: [0.5, 0.5] },
  },
  {
    id: "hologram",
    name: "Hologram",
    group: "Cyber",
    drift: true,
    base: "linear-gradient(115deg, #b6f8ff, #ffd6f7 30%, #d9c8ff 60%, #b6f8ff 100%)",
    fx: { kind: "sheen", colors: ["rgba(255,255,255,.75)"], dur: [3.2, 3.2], opacity: [0.9, 0.9] },
  },
  {
    id: "crt",
    name: "CRT",
    group: "Cyber",
    base: "repeating-linear-gradient(0deg, rgba(0,0,0,.35) 0 2px, transparent 2px 4px), linear-gradient(180deg, #04211a, #0a3d2e)",
    fx: { kind: "scan", colors: ["rgba(90,255,200,.35)"], dur: [3.2, 3.2], opacity: [0.7, 0.7] },
  },
  {
    id: "circuit",
    name: "Circuit",
    group: "Cyber",
    base: "repeating-linear-gradient(90deg, rgba(60,200,255,.18) 0 1px, transparent 1px 22px), repeating-linear-gradient(0deg, rgba(60,200,255,.18) 0 1px, transparent 1px 22px), linear-gradient(135deg, #04121f, #08243d)",
    fx: { kind: "twinkle", n: 11, colors: ["#4dd2ff", "#b6f8ff"], size: [1.6, 3], dur: [1.6, 4], glow: 8 },
  },
  {
    id: "scanner",
    name: "Scanner",
    group: "Cyber",
    base: "linear-gradient(180deg, #05121c 0%, #0a2231 100%)",
    fx: { kind: "scan", colors: ["rgba(80,255,190,.8)"], dur: [2.2, 2.2], opacity: [0.85, 0.85] },
  },

  // ---------- vibes ----------
  {
    id: "equalizer",
    name: "Equalizer",
    group: "Vibes",
    base: "linear-gradient(180deg, #12071f 0%, #24103d 100%)",
    fx: { kind: "bars", n: 14, colors: ["#8b5cf6", "#22d3ee", "#f472b6"], size: [7, 12], dur: [0.4, 1.1], opacity: [0.55, 0.95] },
  },
  {
    id: "lavalamp",
    name: "Lava lamp",
    group: "Vibes",
    base: "linear-gradient(180deg, #1a0630 0%, #3a0f4a 100%)",
    fx: { kind: "blobs", n: 6, colors: ["#ff6ad5", "#ffa63d", "#6ad2ff"], size: [4, 9], dur: [8, 15], drift: 30, opacity: [0.45, 0.7] },
  },
  {
    id: "confetti",
    name: "Confetti",
    group: "Vibes",
    tumble: true,
    base: "linear-gradient(135deg, #1d1b3a 0%, #3b2a63 100%)",
    fx: { kind: "fall", n: 12, colors: ["#ff5d5d", "#ffd24d", "#4dff88", "#4dd2ff", "#c46bff"], size: [1.8, 3.6], dur: [2, 5], drift: 40, glow: 0 },
  },
  {
    id: "hearts",
    name: "Hearts",
    group: "Vibes",
    base: "linear-gradient(160deg, #3d0a24 0%, #7a1d47 55%, #c44a7d 100%)",
    fx: { kind: "rise", n: 12, glyphs: ["♥", "❥"], colors: ["#ff6b9d", "#ffb3c9", "#ffffff"], size: [2, 4.4], dur: [2.6, 6], drift: 30, glow: 6 },
  },
  {
    id: "sparkle",
    name: "Sparkle",
    group: "Vibes",
    drift: true,
    base: "linear-gradient(120deg, #1b1035 0%, #4a2b7a 50%, #1b1035 100%)",
    fx: { kind: "twinkle", n: 11, glyphs: ["✦", "✧", "✨"], colors: ["#fff6c2", "#ffffff", "#ffd6f7"], size: [1.4, 3.2], dur: [1.6, 4], glow: 7 },
  },
  {
    id: "prism",
    name: "Prism",
    group: "Vibes",
    drift: true,
    base: "linear-gradient(100deg, #ff4d4d, #ffa64d, #ffe14d, #4dff88, #4dd2ff, #7a6ff0, #d94dff)",
    fx: { kind: "sheen", colors: ["rgba(255,255,255,.5)"], dur: [4.5, 4.5], opacity: [0.8, 0.8] },
  },
  {
    id: "disco",
    name: "Disco",
    group: "Vibes",
    base: "conic-gradient(from 0deg at 50% 50%, #ff4d6d, #ffd24d, #4dff88, #4dd2ff, #c46bff, #ff4d6d)",
    fx: { kind: "twinkle", n: 14, colors: ["#ffffff"], size: [1.4, 3], dur: [0.8, 2], glow: 8 },
  },
  {
    id: "smoke",
    name: "Smoke",
    group: "Vibes",
    base: "linear-gradient(180deg, #0d0d12 0%, #23232e 100%)",
    fx: { kind: "rise", n: 14, colors: ["#8b8b9c", "#c9c9d6"], size: [4, 9], dur: [6, 12], drift: 40, glow: 10, opacity: [0.12, 0.3] },
  },

  // ---------- calm ----------
  { id: "midnight", name: "Midnight", group: "Calm", drift: true, base: "linear-gradient(120deg, #0f172a, #1e293b 55%, #334155)" },
  { id: "dusk", name: "Dusk", group: "Calm", drift: true, base: "linear-gradient(120deg, #3d2b56, #6b3f6e 50%, #c96f8a)" },
  { id: "mint", name: "Mint", group: "Calm", drift: true, base: "linear-gradient(120deg, #0f3d3a, #1f7a6b 55%, #7fd8c2)" },
  { id: "sand", name: "Sand", group: "Calm", drift: true, base: "linear-gradient(120deg, #6b4a2f, #c49a6c 55%, #f0dcc0)" },
  { id: "ink", name: "Ink", group: "Calm", base: "linear-gradient(160deg, #08080c 0%, #16161f 100%)" },
  { id: "paper", name: "Paper", group: "Calm", base: "linear-gradient(160deg, #f5f0e6 0%, #ded5c4 100%)" },
];

export const BANNER_BY_ID = Object.fromEntries(BANNERS.map((b) => [b.id, b]));
export const BANNER_GROUPS = ["Cosmic", "Weather", "Nature", "Cyber", "Vibes", "Calm"];

export const isPreset = (banner = "") => banner.startsWith("preset:");
export const presetId = (banner = "") => banner.slice(7);
export const presetOf = (banner = "") => (isPreset(banner) ? BANNER_BY_ID[presetId(banner)] : null);
