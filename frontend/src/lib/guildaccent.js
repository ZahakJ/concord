// guildaccent.js — a guild's colour identity, derived from the banner it
// already owns, and the guard that keeps the USER's accent legible.
//
// A guild's colour is decoration. It used to be stamped as --accent over the
// whole view, which made every guild a scene change — and also repainted the
// one colour that means "you": your mention badges, your focus ring, the New
// divider and your own reactions turned orange in one guild and pink in the
// next, so the signal stopped meaning anything. It is now --guild-accent, set
// only on that guild's own marks (its strip tile, its switcher row icon, a thin
// tint on its place header), and --accent stays yours everywhere.
//
// Derivation, not annotation: the presets' bases are ordinary CSS gradient
// strings, so we mine them for their most saturated usable color instead of
// hand-labelling 40+ templates (labels rot; the parse can't). Data-URI image
// banners are opaque to this — they simply yield no accent, which is the
// correct quiet fallback, not an error.

import { GUILD_BANNERS } from "./guildbanners.js";
import { isPreset, presetId } from "./banners.js";

const COLOR_RE = /#([0-9a-f]{6})\b|rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)/gi;

function hsl(r, g, b) {
  (r /= 255), (g /= 255), (b /= 255);
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const l = (max + min) / 2;
  const d = max - min;
  const s = d === 0 ? 0 : d / (1 - Math.abs(2 * l - 1));
  return { s, l };
}

// The most saturated color in a usable lightness band. Too dark and the
// color-mix'd hover/glow tokens vanish; too light and --accent-fg flips to
// dark text on a pastel chip nobody chose. Grayscale banners (asphalt,
// paper) yield nothing on purpose — a gray accent is worse than none.
function dominant(css) {
  let best = "";
  let bestScore = 0;
  for (const m of css.matchAll(COLOR_RE)) {
    const [r, g, b] = m[1]
      ? [parseInt(m[1].slice(0, 2), 16), parseInt(m[1].slice(2, 4), 16), parseInt(m[1].slice(4, 6), 16)]
      : [+m[2], +m[3], +m[4]];
    const { s, l } = hsl(r, g, b);
    if (s < 0.3 || l < 0.22 || l > 0.72) continue;
    const score = s * (1 - Math.abs(l - 0.5));
    if (score > bestScore) {
      bestScore = score;
      best = `rgb(${r}, ${g}, ${b})`;
    }
  }
  return best;
}

const cache = new Map();

// guildAccent(banner) -> a CSS color, or "" when the banner offers none
// (no banner, an image banner, or a grayscale preset).
export function guildAccent(banner) {
  if (!banner || !isPreset(banner)) return "";
  const id = presetId(banner);
  if (cache.has(id)) return cache.get(id);
  const t = GUILD_BANNERS.find((x) => x.id === id);
  // Mine the fx layer's colors too — some templates keep their hue in the
  // particles (white-based bases with colored streaks).
  const c = t ? dominant(t.base + " " + (t.fx?.colors || []).join(" ")) : "";
  cache.set(id, c);
  return c;
}

// tileColor is the ground a guild's tile wears when it has no icon: its
// banner's colour, else a hue hashed from its id so the same guild is always
// the same colour. Solid, never a gradient: tiles are chrome.
export function tileColor(g) {
  const c = guildAccent(g?.banner);
  if (c) return c;
  const id = String(g?.id || "");
  let h = 0;
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0;
  return `hsl(${h % 360} 38% 38%)`;
}

// ---- the user's accent, held legible ----
//
// The accent is the user's to choose, and a weak one — a pale preset in the
// light theme, a greyish profile colour in the dark — used to make the badges
// that say "this needs you" disappear into the sidebar. clampAccent walks a
// chosen colour toward white (dark theme) or black (light theme), keeping its
// hue, just far enough that the three things it must carry still clear:
//   the accent as ink (--accent-hover) ≥ 4.5:1 on the canvas and on its own
//   soft tint, and the badge fill (--accent itself) ≥ 3:1 on the sidebar.
// The surfaces and mixes are app.css's, both themes.
const SURFACES = {
  dark: { bg1: [18, 21, 22], bg2: [23, 26, 28], hoverMix: [255, 255, 255, 0.24], soft: 0.16 },
  light: { bg1: [234, 236, 230], bg2: [252, 252, 251], hoverMix: [0, 0, 0, 0.22], soft: 0.12 },
};
export const TEAL = { dark: "#14a394", light: "#0e7c70" };

function parseRGB(color) {
  const s = String(color || "").trim();
  let m = s.match(/^#([0-9a-f]{3})$/i);
  if (m) return [...m[1]].map((c) => parseInt(c + c, 16));
  m = s.match(/^#([0-9a-f]{6})$/i);
  if (m) return [0, 2, 4].map((i) => parseInt(m[1].slice(i, i + 2), 16));
  m = s.match(/^rgba?\(\s*(\d+)[,\s]+(\d+)[,\s]+(\d+)/i);
  if (m) return [+m[1], +m[2], +m[3]];
  return null;
}
function lum([r, g, b]) {
  const lin = (v) => {
    v /= 255;
    return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}
export function contrast(a, b) {
  const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p);
  return (x + 0.05) / (y + 0.05);
}
const mix = (a, b, t) => a.map((v, i) => v + (b[i] - v) * t);

// accentHolds reports whether an accent (rgb triple) carries every signal in
// this theme.
export function accentHolds(rgb, theme) {
  const s = SURFACES[theme] || SURFACES.dark;
  const [hr, hg, hb, ht] = s.hoverMix;
  const hover = mix(rgb, [hr, hg, hb], ht);
  const soft = mix(s.bg2, rgb, s.soft);
  return contrast(hover, s.bg2) >= 4.5 && contrast(hover, soft) >= 4.5 && contrast(rgb, s.bg1) >= 3;
}

// clampAccent(color, theme) -> a colour that holds, as "rgb(r, g, b)", or the
// colour unchanged when it already does. An unreadable colour falls back to
// the brand teal for the theme.
export function clampAccent(color, theme = "dark") {
  const rgb = parseRGB(color);
  const t = theme === "light" ? "light" : "dark";
  if (!rgb) return TEAL[t];
  if (accentHolds(rgb, t)) return color;
  const target = t === "light" ? [0, 0, 0] : [255, 255, 255];
  // Rounded at every step, so the colour that is tested is the colour that is
  // stamped: an unrounded pass that rounds into a fail is still a fail.
  const at = (k) => mix(rgb, target, k).map((v) => Math.round(v));
  let lo = 0;
  let hi = 1;
  for (let i = 0; i < 14; i++) {
    const mid = (lo + hi) / 2;
    if (accentHolds(at(mid), t)) hi = mid;
    else lo = mid;
  }
  if (!accentHolds(at(hi), t)) return TEAL[t];
  return `rgb(${at(hi).join(", ")})`;
}

// ---- which colour is the accent ----
//
// The accent is the brand teal for everyone unless the user picks otherwise.
// A profile colour is the colour of the PROFILE (avatar ring, card); it used
// to become the whole app's accent by default, so a warm avatar colour turned
// every badge, focus ring and mention row into something that read as an
// alert. Following the profile colour is now an explicit choice (the
// Appearance value "profile"), and even then a hue in the red–orange band is
// toned down, because the signals that mean "you" must never be mistaken for
// errors or warnings.
export const ACCENT_PROFILE = "profile";

function toHSL([r, g, b]) {
  (r /= 255), (g /= 255), (b /= 255);
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const l = (max + min) / 2;
  const d = max - min;
  if (d === 0) return [0, 0, l];
  const s = d / (1 - Math.abs(2 * l - 1));
  let h;
  if (max === r) h = ((g - b) / d) % 6;
  else if (max === g) h = (b - r) / d + 2;
  else h = (r - g) / d + 4;
  return [(h * 60 + 360) % 360, s, l];
}
function fromHSL([h, s, l]) {
  const c = (1 - Math.abs(2 * l - 1)) * s;
  const x = c * (1 - Math.abs(((h / 60) % 2) - 1));
  const m = l - c / 2;
  const [r, g, b] = h < 60 ? [c, x, 0] : h < 120 ? [x, c, 0] : h < 180 ? [0, c, x] : h < 240 ? [0, x, c] : h < 300 ? [x, 0, c] : [c, 0, x];
  return [r, g, b].map((v) => Math.round((v + m) * 255));
}
const hex = (rgb) => "#" + rgb.map((v) => v.toString(16).padStart(2, "0")).join("");

// The band where a saturated hue reads as danger or warning on graphite and
// on paper: magenta-red through amber.
export function isAlertHue(h) {
  return h >= 335 || h <= 55;
}
const ALERT_MAX_SAT = 0.32;

// profileAccent(color) -> the profile colour fit to be an accent: unchanged
// outside the alert band, desaturated (same hue and lightness) inside it.
export function profileAccent(color) {
  const rgb = parseRGB(color);
  if (!rgb) return "";
  const [h, s, l] = toHSL(rgb);
  if (!isAlertHue(h) || s <= ALERT_MAX_SAT) return hex(rgb);
  return hex(fromHSL([h, ALERT_MAX_SAT, l]));
}

// resolveAccent(pref, profileColor) -> the explicit accent colour to stamp, or
// "" for "leave the stylesheet's accent alone" (the brand teal, or a theme
// pack's own accent). pref "" is the default and means teal.
export function resolveAccent(pref, profileColor) {
  if (!pref) return "";
  if (pref === ACCENT_PROFILE) return profileAccent(profileColor);
  return pref;
}

// hueOf is exported for the tests.
export function hueOf(color) {
  const rgb = parseRGB(color);
  return rgb ? toHSL(rgb) : null;
}
