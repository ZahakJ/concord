// profilestyle.js — turns a member's style dials into CSS custom properties.
// One place decides what "fast, counter-clockwise, strong glow, 3px ring"
// means, so the profile card, the avatar, and the editor's live preview can
// never drift apart.

const SPEED_MS = { slow: 7000, normal: 3600, fast: 1600 };
const GLOW_PX = { off: 0, soft: 10, strong: 22 };

// ringVars: CSS vars consumed by the .avatar-frame-* rules in app.css.
export function ringVars(style = null, color = "") {
  const st = style || {};
  const dur = SPEED_MS[st.speed] || SPEED_MS.normal;
  const dir = st.dir === "ccw" ? "reverse" : "normal";
  const glow = GLOW_PX[st.glow] ?? GLOW_PX.soft;
  const width = Math.min(5, Math.max(1, st.width || 2));
  return (
    `--ring-dur:${dur}ms;` +
    `--ring-dir:${dir};` +
    `--ring-glow:${glow}px;` +
    `--ring-w:${width}px;` +
    (color ? `--ring-color:${color};` : "")
  );
}

// bannerStyle: the profile-card banner background — image, gradient of the
// member's two theme colors (with their chosen angle), or a solid.
export function bannerStyle(mem) {
  if (!mem) return "";
  if (mem.banner) return `background-image:url(${mem.banner})`;
  const st = mem.style || {};
  const c1 = mem.color;
  const c2 = mem.color2;
  if (!c1) return ""; // fall through to the CSS default accent gradient
  if (st.fill === "solid" || !c2) return `background:${c1}`;
  const angle = Number.isFinite(st.angle) ? st.angle : 120;
  return `background:linear-gradient(${angle}deg, ${c1}, ${c2})`;
}
