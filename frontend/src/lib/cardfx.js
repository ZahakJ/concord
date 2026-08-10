// cardfx.js — the profile-card effect library.
//
// The animated layer that plays over someone's profile card. Same idea as
// banners.js and rings.js: an effect is DATA for the shared engine in
// lib/fx.js, painted by FxLayer.svelte, with the keyframes living in app.css.
// It travels as a short id, so a card effect costs a dozen bytes rather than a
// video.
//
// This replaces four hardcoded `card-effect-*` CSS classes. The engine already
// knew seventeen kinds; only four of them had ever been offered here, which is
// why the choice felt thin. Nothing new was needed to fix that except data.
//
// A note on taste, since these animate BEHIND a person's name and avatar: an
// effect that makes a profile hard to read is a failed effect however pretty it
// looks alone. Opacities stay low, particle counts stay modest, and the busiest
// options are deliberately tuned down from what they could be.
//
// `kind` must be one the engine implements (fx.js + the .fx-* rules in
// app.css). `tumble` is a FLAG alongside a kind, not a kind of its own — it
// adds per-particle rotation to a falling field.

export const CARD_EFFECTS = [
  // ---------- weather ----------
  {
    id: "hush",
    name: "Hush",
    group: "Weather",
    // Sparse, oversized flakes wandering down through still air — the quiet of the first snow of the year.
    fx: {
      kind: "fall",
      n: 12,
      glyphs: ["\u2744", "\u2745", "\u2746", "\u00b7"],
      colors: ["#ffffff", "#e6f1ff", "#c8dcf4"],
      size: [1.8, 4.0],
      dur: [7, 14],
      drift: 30,
      glow: 2,
      opacity: [0.40, 0.85],
    },
  },
  {
    id: "downpour",
    name: "Downpour",
    group: "Weather",
    // Thin pale drops falling almost straight and very fast — rain seen against a window, not through it.
    fx: {
      kind: "rain",
      n: 14,
      colors: ["#b7d0e8", "#dbe9f7", "#8fb0cd"],
      size: [2.2, 4.0],
      dur: [0.55, 1.1],
      drift: 8,
      glow: 0,
      opacity: [0.40, 0.85],
    },
  },
  {
    id: "fogbound",
    name: "Fogbound",
    group: "Weather",
    // Four huge soft grey banks crawling across the card so slowly you only notice they moved.
    fx: {
      kind: "blobs",
      n: 4,
      colors: ["#e6ecf2", "#b9c7d3", "#93a4b2"],
      size: [7, 12],
      dur: [20, 32],
      drift: 34,
      glow: 0,
      opacity: [0.1, 0.22],
    },
  },
  {
    id: "distant-thunder",
    name: "Distant thunder",
    group: "Weather",
    // A dim flare from one edge every several seconds, with a pale fork behind it — a storm happening somewhere else.
    fx: {
      kind: "bolt",
      colors: ["#dfe8ff", "#fff3cf"],
      dur: [7, 11],
      glow: 4,
      opacity: [0.28, 0.42],
    },
  },
  {
    id: "heat-shimmer",
    name: "Heat shimmer",
    group: "Weather",
    // Warm gold bands pooling along the bottom edge, rocking against each other — air too hot to hold still.
    fx: {
      kind: "waves",
      colors: ["#ffd9a0", "#ffc06a", "#fff0d0"],
      size: [8, 16],
      dur: [7, 12],
      drift: 24,
      glow: 0,
      opacity: [0.08, 0.18],
    },
  },
  {
    id: "leaf-turn",
    name: "Leaf turn",
    group: "Weather",
    // A handful of amber leaves turning over as they come down, wide and unhurried.
    fx: {
      kind: "fall",
      n: 12,
      glyphs: ["\ud83c\udf42", "\ud83c\udf41"],
      size: [2.6, 4.6],
      dur: [8, 15],
      drift: 34,
      glow: 0,
      opacity: [0.45, 0.85],
      tumble: true,
    },
  },
  // ---------- cosmic ----------
  {
    id: "deep-field",
    name: "Deep field",
    group: "Cosmic",
    // A still, very distant starfield: ~14 pinpricks that hold position and slowly breathe in and out of visibility, cold white through pale blue, never bright enough to compete with the name.
    fx: {
      kind: "twinkle",
      n: 14,
      colors: ["#ffffff", "#cfe0ff", "#9fb6ff"],
      size: [1.8, 4.0],
      dur: [4, 9],
      drift: 5,
      glow: 3,
      opacity: [0.40, 0.85],
    },
  },
  {
    id: "meteor-watch",
    name: "Meteor watch",
    group: "Cosmic",
    // A quiet sky that is mostly empty, punctuated by a handful of bright diagonal streaks with long trailing glows — you notice motion out of the corner of your eye, then it is gone.
    fx: {
      kind: "streak",
      n: 12,
      colors: ["#ffffff", "#ffe6b8", "#bfe9ff"],
      size: [1.8, 4.0],
      dur: [2.4, 5],
      glow: 6,
      opacity: [0.45, 0.90],
    },
  },
  {
    id: "nebula-bloom",
    name: "Nebula bloom",
    group: "Cosmic",
    // Four huge, heavily blurred clouds of indigo, violet and dusty rose that swell and slide across the card over ~15 seconds — colour weather behind the avatar rather than anything you can point at.
    fx: {
      kind: "blobs",
      n: 4,
      colors: ["#6b4bff", "#c05cff", "#ff6aa8", "#3a6bff"],
      size: [6, 11],
      dur: [12, 20],
      drift: 34,
      glow: 0,
      opacity: [0.2, 0.38],
    },
  },
  {
    id: "aurora-hush",
    name: "Aurora hush",
    group: "Cosmic",
    // Three soft green-to-teal curtains lying along the bottom edge, tilting and sliding past each other very slowly — the whole effect stays low in the frame, well clear of the name and avatar.
    fx: {
      kind: "waves",
      colors: [
        "rgba(110,255,190,.30)",
        "rgba(90,200,255,.24)",
        "rgba(180,150,255,.18)",
      ],
      size: [30, 54],
      dur: [7, 12],
      drift: 16,
      glow: 0,
      opacity: [0.18, 0.34],
    },
  },
  {
    id: "weightless",
    name: "Weightless",
    group: "Cosmic",
    // Fine deep-space dust migrating upward over 8-14 seconds, faint and evenly spread with a wide horizontal wander — a slow exhale, the opposite direction to every falling effect and far calmer than the twinkle field.
    fx: {
      kind: "rise",
      n: 12,
      colors: ["#e6e2ff", "#a9c8ff", "#ffffff"],
      size: [1.8, 4.0],
      dur: [8, 14],
      drift: 30,
      glow: 3,
      opacity: [0.40, 0.85],
    },
  },
  {
    id: "pulsar",
    name: "Pulsar",
    group: "Cosmic",
    // Three hairline cyan rings expanding out from behind the avatar and fading as they reach the edge, staggered so a new one starts every couple of seconds — geometric and centred, with no particle field at all.
    fx: {
      kind: "ripple",
      colors: ["#7fe6ff", "#b9d6ff"],
      dur: [5, 7],
      glow: 0,
      opacity: [0.3, 0.5],
    },
  },
  // ---------- elemental ----------
  {
    id: "emberlight",
    name: "Emberlight",
    group: "Elemental",
    // Tiny hot sparks lift off the bottom edge, wander sideways and burn out before they reach the top.
    fx: {
      kind: "rise",
      n: 12,
      colors: ["#ff8a3d", "#ffb347", "#ff5a1f", "#ffd98a"],
      size: [1.8, 4.0],
      dur: [3, 7],
      drift: 20,
      glow: 5,
      opacity: [0.40, 0.85],
    },
  },
  {
    id: "hoarfrost",
    name: "Hoarfrost",
    group: "Elemental",
    // Pale ice crystals turn slowly as they sink, almost weightless, with barely any sideways wander.
    fx: {
      kind: "fall",
      n: 12,
      glyphs: ["\u274b", "\u2748", "\u2726"],
      colors: ["#dbf4ff", "#a9e4ff", "#ffffff"],
      size: [2.2, 4.6],
      dur: [9, 16],
      drift: 12,
      glow: 2,
      opacity: [0.40, 0.85],
      tumble: true,
    },
  },
  {
    id: "stillwater",
    name: "Stillwater",
    group: "Elemental",
    // Three faint teal rings open outward on a slow cycle, like a single drop landing in a dark pool.
    fx: {
      kind: "ripple",
      colors: ["#5fd8d0", "#8ff0e6", "#2e9fb5"],
      size: [2, 3],
      dur: [6, 10],
      glow: 2,
      opacity: [0.14, 0.3],
    },
  },
  {
    id: "sirocco",
    name: "Sirocco",
    group: "Elemental",
    // Dim sand grains skitter diagonally across the card on a dry wind — short, matte, never glowing.
    fx: {
      kind: "streak",
      n: 12,
      colors: ["#d9b382", "#c99a5b", "#f0dcb4"],
      size: [1.8, 4.0],
      dur: [2.4, 5],
      drift: 8,
      glow: 0,
      opacity: [0.40, 0.85],
    },
  },
  // ---------- cyber ----------
  {
    id: "deep-dive",
    name: "Deep dive",
    group: "Cyber",
    // Small dim katakana and binary falling dead straight down, like console output behind glass.
    fx: {
      kind: "fall",
      n: 14,
      glyphs: ["\uff71", "\uff77", "\uff82", "\uff9c", "0", "1"],
      colors: ["#3ef08a", "#9dffc7"],
      size: [2.0, 4.0],
      dur: [3.5, 7.5],
      drift: 0,
      glow: 2,
      opacity: [0.40, 0.85],
    },
  },
  {
    id: "neon-horizon",
    name: "Neon horizon",
    group: "Cyber",
    // A magenta perspective grid scrolling toward you across the bottom half, fading out before it reaches the name.
    fx: {
      kind: "grid",
      colors: ["rgba(255,72,198,.5)"],
      dur: [3.4, 3.4],
      opacity: [0.42, 0.42],
    },
  },
  {
    id: "nightclub",
    name: "Nightclub",
    group: "Cyber",
    // A cyan-to-violet equaliser twitching along the bottom edge, bright at the tips and fading downward.
    fx: {
      kind: "bars",
      n: 12,
      colors: ["#22d3ee", "#7c5cff", "#38f5c8"],
      size: [2, 5],
      dur: [0.5, 1.3],
      opacity: [0.12, 0.28],
    },
  },
  {
    id: "uplink",
    name: "Uplink",
    group: "Cyber",
    // A handful of bright cyan packets shooting diagonally across the card with short comet tails, arriving in an irregular rhythm.
    fx: {
      kind: "streak",
      n: 12,
      colors: ["#7df9ff", "#ffffff", "#5aa8ff"],
      size: [1.8, 4.0],
      dur: [1.2, 2.8],
      glow: 5,
      opacity: [0.40, 0.85],
    },
  },
  {
    id: "dead-channel",
    name: "Dead channel",
    group: "Cyber",
    // One soft pale-green CRT scanline crawling slowly down the whole card, barely there.
    fx: {
      kind: "scan",
      colors: ["rgba(150,255,225,.3)"],
      dur: [4.6, 4.6],
      opacity: [0.55, 0.55],
    },
  },
  {
    id: "iridescent",
    name: "Iridescent",
    group: "Cyber",
    // A single cold specular sweep slicing across the card like light on a holo foil, then a long pause before the next pass.
    fx: {
      kind: "sheen",
      colors: ["rgba(186,226,255,.32)"],
      dur: [5.5, 5.5],
      opacity: [0.4, 0.4],
    },
  },
  // ---------- whimsy ----------
  {
    id: "fireflies-dusk",
    name: "Fireflies at dusk",
    group: "Whimsy",
    // Nine warm green-gold pinpricks hovering and blinking softly in place, never crossing the card — a summer garden at last light.
    fx: {
      kind: "twinkle",
      n: 12,
      colors: ["#d8ff8a", "#ffe08a", "#b6ff9e"],
      size: [1.8, 4.0],
      dur: [2.8, 6.5],
      drift: 14,
      glow: 7,
      opacity: [0.40, 0.85],
    },
  },
  {
    id: "smitten",
    name: "Smitten",
    group: "Whimsy",
    // A slow, sparse stream of small pink hearts floating up the card and wandering sideways as they go — fond, not saccharine.
    fx: {
      kind: "rise",
      n: 12,
      glyphs: ["\u2665", "\u2661", "\u2765"],
      colors: ["#ff9ec4", "#ffc2dd", "#ff7aa8"],
      size: [2.8, 4.8],
      dur: [5, 10],
      drift: 24,
      glow: 3,
      opacity: [0.40, 0.85],
    },
  },
  {
    id: "cue-the-confetti",
    name: "Cue the confetti",
    group: "Whimsy",
    // Small multicoloured paper rectangles spinning end over end as they drop past the avatar, fast and wide-drifting — a party popper just went off. (Engine note: current CSS treats tumble as a modifier — if kind:"tumble" does not fall, use kind:"fall" with tumble:true.)
    fx: {
      kind: "fall",
      n: 12,
      glyphs: ["\u25ac", "\u25ae", "\u25c6", "\u25aa"],
      colors: ["#ff5d5d", "#ffd24d", "#4dff88", "#4dd2ff", "#c46bff"],
      size: [1.8, 4.0],
      dur: [2.4, 5],
      drift: 30,
      glow: 0,
      opacity: [0.55, 0.90],
      tumble: true,
    },
  },
  {
    id: "bubble-bath",
    name: "Bubble bath",
    group: "Whimsy",
    // Four oversized pastel orbs, blurred and barely there, swelling and sliding across the card over fifteen seconds — soap film catching the light.
    fx: {
      kind: "blobs",
      n: 4,
      colors: ["#a8e6ff", "#ffd6f2", "#d8ffe8", "#fff3c4"],
      size: [3, 5],
      dur: [12, 20],
      drift: 34,
      glow: 0,
      opacity: [0.14, 0.28],
    },
  },
  {
    id: "fast-asleep",
    name: "Fast asleep",
    group: "Whimsy",
    // Three or four pale periwinkle Zs drifting lazily upward over ten-plus seconds with a wide lean — comically drowsy, almost still.
    fx: {
      kind: "rise",
      n: 12,
      glyphs: ["z", "Z", "\u1dbb"],
      colors: ["#cfd8ff", "#b8c6f0"],
      size: [2.6, 5.0],
      dur: [9, 16],
      drift: 34,
      glow: 2,
      opacity: [0.40, 0.85],
    },
  },
  // ---------- calm ----------
  {
    id: "slow-glass",
    name: "Slow glass",
    group: "Calm",
    // One pale diagonal band of light sweeps across the card every ~15s, then the card is plain again for a long beat.
    fx: {
      kind: "sheen",
      colors: ["#eef3fb"],
      dur: [15, 15],
      glow: 0,
      opacity: [0.14, 0.14],
    },
  },
  {
    id: "late-afternoon",
    name: "Late afternoon",
    group: "Calm",
    // A handful of near-invisible warm motes sinking slowly through the card, wandering sideways like dust in a shaft of light.
    fx: {
      kind: "fall",
      n: 12,
      colors: ["#f3e7d2", "#e8dcc6", "#fff6e6"],
      size: [1.8, 4.0],
      dur: [16, 26],
      drift: 22,
      glow: 0,
      opacity: [0.40, 0.85],
    },
  },
];

export const CARD_EFFECT_BY_ID = Object.fromEntries(
  CARD_EFFECTS.map((e) => [e.id, e]),
);

export const CARD_EFFECT_GROUPS = [
  ...new Set(CARD_EFFECTS.map((e) => e.group)),
].map((title) => ({
  title,
  ids: CARD_EFFECTS.filter((e) => e.group === title).map((e) => e.id),
}));

// cardEffect resolves an id to its definition, or null. Fails CLOSED so an id
// invented by a peer renders nothing rather than anything surprising.
export function cardEffect(id) {
  return CARD_EFFECT_BY_ID[id] || null;
}
