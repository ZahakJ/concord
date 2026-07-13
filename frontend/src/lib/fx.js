// fx.js — the effects engine shared by avatar rings and profile banners.
//
// An effect is DATA, not a bespoke CSS class: a kind ("fall", "streak",
// "bolt"…), how many particles, their glyphs/colors/sizes/speeds. This file
// turns that description into a deterministic list of particles; app.css owns
// the keyframes; FxLayer.svelte paints them. A ring and a banner using the
// same kind share every line of code — snow is snow whether it falls past your
// avatar or across your banner.
//
// Deterministic: the same seed always yields the same field, so a preset looks
// identical in the picker, on the card, and on everyone else's screen — and it
// doesn't reshuffle on every re-render.

function rng(seed) {
  let h = 2166136261 >>> 0;
  for (let i = 0; i < seed.length; i++) {
    h ^= seed.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return () => {
    h += 0x6d2b79f5;
    let t = h;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const lerp = (a, b, t) => a + (b - a) * t;

// scale < 1 means a small canvas (a 32px avatar or a picker tile, not a
// banner). Small canvases get FAR fewer particles and no glow — this is the
// difference between a smooth app and a laptop fan: a member list with 40
// ringed avatars, or a picker with 43 live tiles, would otherwise animate a
// thousand blurred, glowing boxes at once. Nobody can see a 3px glow on a 5px
// snowflake anyway.
export function particles(seed, fx, scale = 1) {
  const r = rng(seed + (fx.kind || ""));
  const budget = scale >= 0.75 ? 1 : scale >= 0.5 ? 0.4 : 0.28;
  const n = Math.max(2, Math.round((fx.n ?? 10) * budget));
  const [s0, s1] = fx.size ?? [3, 6];
  const [d0, d1] = fx.dur ?? [4, 9];
  const [o0, o1] = fx.opacity ?? [0.55, 1];
  const drift = fx.drift ?? 10;
  const glow = scale < 0.7 ? 0 : (fx.glow ?? 4);
  const out = [];
  for (let i = 0; i < n; i++) {
    const du = lerp(d0, d1, r());
    const sz = lerp(s0, s1, r()) * Math.max(0.55, Math.min(1, scale));
    out.push({
      g: fx.glyphs ? fx.glyphs[Math.floor(r() * fx.glyphs.length)] : "",
      style:
        `--x:${(r() * 100).toFixed(1)}%;--y:${(r() * 100).toFixed(1)}%;` +
        `--sz:${sz.toFixed(1)}px;--du:${du.toFixed(2)}s;--d:${(-r() * du).toFixed(2)}s;` +
        `--dx:${Math.round(lerp(-drift, drift, r()) * scale)}px;` +
        `--c:${fx.colors ? fx.colors[Math.floor(r() * fx.colors.length)] : "#fff"};` +
        `--o:${lerp(o0, o1, r()).toFixed(2)};--rot:${Math.round(lerp(-540, 540, r()))}deg;` +
        `--gl:${(glow * Math.max(0.5, scale)).toFixed(1)}px`,
    });
  }
  return out;
}

// Kinds that draw a fixed number of shaped layers rather than a particle field.
export const LAYERED = new Set(["ripple", "bolt", "bars", "blobs", "grid", "waves", "sheen", "scan", "rays"]);

// How many layers each layered kind needs.
export function layers(fx) {
  switch (fx.kind) {
    case "ripple":
      return 3;
    case "bolt":
      return 2;
    case "bars":
      return fx.n ?? 12;
    case "blobs":
      return fx.n ?? 4;
    case "waves":
      return 3;
    case "rays":
      return 1;
    default:
      return 1;
  }
}
