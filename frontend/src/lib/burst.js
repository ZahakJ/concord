// burst.js — one-shot celebration effects. The FX engine (lib/fx.js) can only
// LOOP: every keyframe it drives is infinite, which decorates but never
// celebrates. This module is the other half: a particle layer that plays once
// at a moment that earned it — your reaction landing, a poll crowning its
// winner, a device linking — and removes itself. Same determinism rules as
// fx.js where it matters: a message effect seeded by the message id renders
// the identical field on every peer's screen.
//
// Two primitives, both transform/opacity-only (the codebase's own GPU rule):
//   radialBurst(x, y, opts)  — a small ring of glyphs flying out of a point
//   confettiBurst(opts)      — a full-viewport fall, once
// Both bail entirely under prefers-reduced-motion, and a global cap keeps a
// spammed channel from stacking layers into a fan-spinner.

import { rng } from "./fx.js";

const lerp = (a, b, t) => a + (b - a) * t;

const reduced = () =>
  window.matchMedia?.("(prefers-reduced-motion: reduce)")?.matches ?? false;

// At most this many burst layers alive at once; extra requests are dropped
// (not queued — a celebration that plays late celebrates the wrong thing).
const MAX_LIVE = 3;
let live = 0;

function mountLayer(style, ttlMs) {
  const el = document.createElement("div");
  el.className = "fx-once";
  el.setAttribute("aria-hidden", "true");
  el.style.cssText = style;
  document.body.appendChild(el);
  live++;
  setTimeout(() => {
    el.remove();
    live--;
  }, ttlMs);
  return el;
}

// A ring of glyphs bursting out of a viewport point — the reaction payoff.
export function radialBurst(x, y, opts = {}) {
  if (reduced() || live >= MAX_LIVE) return;
  const {
    glyphs = ["✦"],
    colors = ["var(--accent)"],
    n = 8,
    dist = [26, 58],
    size = [10, 15],
    dur = [0.5, 0.85],
    seed = `${Date.now()}`,
  } = opts;
  const r = rng(seed);
  const el = mountLayer(
    `position:fixed;left:${x}px;top:${y}px;width:0;height:0;overflow:visible;` +
      `pointer-events:none;z-index:210;`,
    Math.ceil(dur[1] * 1000) + 60,
  );
  for (let i = 0; i < n; i++) {
    // Evenly spread angles with a jittered radius read as a burst; fully
    // random angles read as a glitch.
    const a = (i / n) * Math.PI * 2 + r() * 0.6;
    const d = lerp(dist[0], dist[1], r());
    const p = document.createElement("span");
    p.className = "fxo fxo-radial";
    p.dataset.g = glyphs[Math.floor(r() * glyphs.length)];
    p.style.cssText =
      `--tx:${Math.round(Math.cos(a) * d)}px;--ty:${Math.round(Math.sin(a) * d)}px;` +
      `--sz:${lerp(size[0], size[1], r()).toFixed(1)}px;` +
      `--du:${lerp(dur[0], dur[1], r()).toFixed(2)}s;` +
      `--c:${colors[Math.floor(r() * colors.length)]};` +
      `--rot:${Math.round(lerp(-260, 260, r()))}deg`;
    el.appendChild(p);
  }
}

// The classic full-screen confetti drop (or any glyph field falling once).
// Seed it with something shared (a message id) when every peer should see the
// same sky.
export function confettiBurst(opts = {}) {
  if (reduced() || live >= MAX_LIVE) return;
  const {
    glyphs = ["▰", "▮", "●", "◆"],
    colors = ["#f43f5e", "#f59e0b", "#22c55e", "#3b82f6", "#a855f7", "#14a394"],
    n = 26,
    size = [6, 11],
    dur = [1.7, 2.8],
    drift = 90,
    seed = `${Date.now()}`,
  } = opts;
  const r = rng(seed);
  const el = mountLayer(
    "position:fixed;inset:0;overflow:hidden;pointer-events:none;z-index:210;",
    Math.ceil(dur[1] * 1000) + 60,
  );
  for (let i = 0; i < n; i++) {
    const p = document.createElement("span");
    p.className = "fxo fxo-fall";
    p.dataset.g = glyphs[Math.floor(r() * glyphs.length)];
    p.style.cssText =
      `--x:${(r() * 100).toFixed(1)}%;` +
      `--dx:${Math.round(lerp(-drift, drift, r()))}px;` +
      `--sz:${lerp(size[0], size[1], r()).toFixed(1)}px;` +
      `--du:${lerp(dur[0], dur[1], r()).toFixed(2)}s;` +
      `--d:${(r() * 0.5).toFixed(2)}s;` +
      `--c:${colors[Math.floor(r() * colors.length)]};` +
      `--rot:${Math.round(lerp(-540, 540, r()))}deg`;
    el.appendChild(p);
  }
}

// Fireworks: a few staggered radial bursts across the upper half. Composed
// from radialBurst so it shares the cap and the reduced-motion bail.
export function fireworksBurst(seed = `${Date.now()}`) {
  const r = rng(seed);
  const w = window.innerWidth;
  const h = window.innerHeight;
  const colors = ["#f43f5e", "#f59e0b", "#22c55e", "#3b82f6", "#a855f7"];
  for (let i = 0; i < 3; i++) {
    const x = w * lerp(0.2, 0.8, r());
    const y = h * lerp(0.15, 0.45, r());
    const c = colors[Math.floor(r() * colors.length)];
    setTimeout(
      () =>
        radialBurst(x, y, {
          glyphs: ["✦", "✧", "●"],
          colors: [c],
          n: 12,
          dist: [40, 110],
          size: [8, 14],
          dur: [0.7, 1.1],
          seed: seed + i,
        }),
      i * 260,
    );
  }
}
