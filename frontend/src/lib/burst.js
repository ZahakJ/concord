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

// Fireworks: a real show, not three polite pops. Five rockets climb from the
// bottom edge on staggered fuses, each exploding into a white core flash, a
// colored shell ring that droops under gravity, and slow-fading embers —
// then a two-shell finale. All of it lives in ONE layer (a show is one
// celebration, not six against the cap), all transform/opacity, and the
// timeline tops out ~3.6s so nothing lingers to cost frames.
export function fireworksBurst(seed = `${Date.now()}`) {
  if (reduced() || live >= MAX_LIVE) return;
  const r = rng(seed);
  const w = window.innerWidth;
  const h = window.innerHeight;
  const colors = ["#f43f5e", "#f59e0b", "#22c55e", "#3b82f6", "#a855f7", "#22d3ee"];
  const el = mountLayer(
    "position:fixed;inset:0;overflow:hidden;pointer-events:none;z-index:210;",
    4200,
  );

  const explode = (x, y, c, big) => {
    // The core: a white flash that outruns the shell and dies first.
    for (let i = 0; i < 6; i++) {
      const a = (i / 6) * Math.PI * 2 + r();
      const d = lerp(14, 30, r()) * (big ? 1.5 : 1);
      const p = document.createElement("span");
      p.className = "fxo fxo-glow fxo-radial";
      p.dataset.g = "✦";
      p.style.cssText =
        `left:${x}px;top:${y}px;--tx:${Math.round(Math.cos(a) * d)}px;--ty:${Math.round(Math.sin(a) * d)}px;` +
        `--sz:${lerp(9, 13, r()).toFixed(1)}px;--du:0.45s;--c:#fff;--rot:0deg`;
      el.appendChild(p);
    }
    // The shell: a full colored ring that expands, then droops under gravity.
    const n = big ? 20 : 14;
    for (let i = 0; i < n; i++) {
      const a = (i / n) * Math.PI * 2 + r() * 0.4;
      const d = lerp(55, 120, r()) * (big ? 1.35 : 1);
      const p = document.createElement("span");
      p.className = "fxo fxo-glow fxo-shell";
      p.dataset.g = r() < 0.3 ? "✧" : "●";
      p.style.cssText =
        `left:${x}px;top:${y}px;--tx:${Math.round(Math.cos(a) * d)}px;--ty:${Math.round(Math.sin(a) * d)}px;` +
        `--sz:${lerp(7, 12, r()).toFixed(1)}px;--du:${lerp(1.1, 1.6, r()).toFixed(2)}s;` +
        `--c:${c};--rot:${Math.round(lerp(-180, 180, r()))}deg`;
      el.appendChild(p);
    }
  };

  const shoot = (delayMs, big) => {
    const x = w * lerp(0.15, 0.85, r());
    const apexY = h * lerp(0.14, 0.42, r());
    const c = colors[Math.floor(r() * colors.length)];
    const rise = lerp(0.5, 0.7, r());
    setTimeout(() => {
      if (!el.isConnected) return;
      const rocket = document.createElement("span");
      rocket.className = "fxo fxo-glow fxo-shot";
      rocket.style.cssText =
        `left:${x}px;top:${h}px;--apex:${Math.round(apexY - h)}px;--du:${rise.toFixed(2)}s;--c:${c};--sz:4px`;
      el.appendChild(rocket);
      setTimeout(() => {
        rocket.remove();
        if (el.isConnected) explode(x, apexY, c, big);
      }, rise * 1000);
    }, delayMs);
  };

  for (let i = 0; i < 5; i++) shoot(i * 380 + r() * 120, false);
  // The finale: two big shells nearly together, center stage.
  shoot(2300, true);
  shoot(2450, true);
}
