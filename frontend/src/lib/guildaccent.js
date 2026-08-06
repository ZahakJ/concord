// guildaccent.js — a guild's color identity, derived from the banner it
// already owns. Guilds carry banners on the wire ("preset:<id>" or a data
// URI) but stepping into one changed nothing outside the channel-list header.
// Stamping the banner's dominant hue as --accent on the guild's view container
// makes every derived token (--accent-hover/-soft/-glow are color-mix'd from
// it) follow along — switching guilds becomes a scene change, for free,
// because every existing guild already chose a banner.
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
