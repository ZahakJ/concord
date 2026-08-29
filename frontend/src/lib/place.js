// place.js — where a floating surface goes, and the one coordinate system it
// is allowed to think in.
//
// THE PROBLEM THIS EXISTS FOR. The UI-scale setting (Ctrl+= / Ctrl+− / the
// Appearance slider) is CSS `zoom` on <html>. That makes the DOM speak two
// different pixel units at once, and at 100% they are the same number, so
// nothing that mixes them is ever wrong until somebody zooms. Measured in
// Chromium at zoom 1.5, on a 1440×900 window:
//
//   VISUAL pixels — what the screen shows:
//     getBoundingClientRect(), window.innerWidth/innerHeight,
//     MouseEvent.clientX/clientY, elementFromPoint(), visualViewport.
//     A fixed div told `left:200px` reports left = 300.
//
//   LAYOUT pixels — what an element is told, before the zoom multiplies it:
//     offsetWidth/offsetHeight/offsetLeft/offsetTop, document.body.clientWidth,
//     percentages, and EVERY length written into style.left / style.top /
//     style.maxHeight / a transform.
//
// So `el.style.left = rect.left + "px"` — measure visual, write layout — lands
// the surface at 1.5× its intended distance from the origin. At 125% every
// menu opened from the lower half of a 900px window fell off the bottom of the
// screen and rendered as nothing; the profile card opened off-screen with its
// full-screen scrim still over the app, which is why avatars stopped answering
// clicks at all. One class, eight consumers.
//
// THE RULE. Placement math happens in LAYOUT pixels, start to finish. Take
// anchors through rectOf()/pointOf(), take the viewport from viewport(), and
// write the result out unconverted. Nothing else in the app should divide by a
// zoom factor.

// uiZoom — the zoom in force on the document root, measured rather than parsed.
// The ratio of the root's visual box to the layout box it was given IS the
// scale between the two spaces, whatever produced it, so this also stays true
// if the root ever carries a transform instead.
export function uiZoom() {
  const root = document.documentElement;
  const layout = root.offsetWidth;
  if (!layout) return 1;
  const z = root.getBoundingClientRect().width / layout;
  return Number.isFinite(z) && z > 0.05 && z < 20 ? z : 1;
}

// viewport — the visible area, in layout pixels: the box a floating surface has
// to fit inside. innerWidth/innerHeight are visual, so they come back scaled.
export function viewport(z = uiZoom()) {
  return { w: window.innerWidth / z, h: window.innerHeight / z };
}

// rectOf — an element's box (or a raw client rect) in layout pixels.
// Accepts an element, a DOMRect, or any {x|left, y|top, w|width, h|height}.
export function rectOf(target, z = uiZoom()) {
  const r = target && typeof target.getBoundingClientRect === "function"
    ? target.getBoundingClientRect()
    : target || { x: 0, y: 0, width: 0, height: 0 };
  const x = r.x ?? r.left ?? 0;
  const y = r.y ?? r.top ?? 0;
  const w = r.width ?? r.w ?? 0;
  const h = r.height ?? r.h ?? 0;
  return { x: x / z, y: y / z, w: w / z, h: h / z };
}

// pointOf — a pointer event's position as a zero-sized anchor, in layout
// pixels. Synthetic points built from a measured rect (the ⋯ button, an event
// card's menu) go through the same door: they are client coordinates too.
export function pointOf(e, z = uiZoom()) {
  const x = e?.clientX ?? e?.x ?? 0;
  const y = e?.clientY ?? e?.y ?? 0;
  return { x: x / z, y: y / z, w: 0, h: 0 };
}

// sizeOf — an element's own size in layout pixels. offsetWidth/Height are
// already layout, and are what a placement wants: the size the box will be
// given, not the size the zoom will paint it at.
export function sizeOf(el) {
  return { w: el?.offsetWidth || 0, h: el?.offsetHeight || 0 };
}

// clampBox — pull a box back inside the viewport, both axes, never letting the
// clamp push the near edge off (a surface taller than the screen sits at the
// margin and scrolls inside itself).
export function clampBox({ left, top, w, h }, { pad = 8, vp = viewport() } = {}) {
  return {
    left: Math.max(pad, Math.min(left, vp.w - w - pad)),
    top: Math.max(pad, Math.min(top, vp.h - h - pad)),
  };
}

// place — put a `w×h` box beside `anchor`, flipping to the opposite side when
// the preferred one has no room, and clamping to the viewport either way.
//
//   anchor  {x, y, w, h} in LAYOUT pixels (rectOf / pointOf)
//   side    "bottom" | "top" | "right" | "left" — the preference, not a promise
//   align   "start" | "center" | "end" along the free axis; flips at the edge
//   gap     anchor → surface distance
//   pad     minimum inset from the viewport edge
//
// Returns {left, top, side, room} — `room` is the space the chosen side
// actually has, which is what a scrolling menu should cap its height to.
export function place({ anchor, w, h, side = "bottom", align = "center", gap = 6, pad = 8, vp = viewport() }) {
  const vertical = side === "bottom" || side === "top";
  const before = vertical ? anchor.y - pad - gap : anchor.x - pad - gap;
  const after = vertical
    ? vp.h - (anchor.y + anchor.h) - pad - gap
    : vp.w - (anchor.x + anchor.w) - pad - gap;
  const wants = side === "top" || side === "left";
  const need = vertical ? h : w;
  // Keep the preferred side while it fits; otherwise take the other one if IT
  // fits, and failing both take whichever has more room and let the clamp
  // decide. A menu with 40px under it must not open into 40px.
  const room0 = wants ? before : after;
  const room1 = wants ? after : before;
  const flip = room0 < need && (room1 >= need || room1 > room0);
  const useBefore = flip ? !wants : wants;
  const room = Math.max(0, useBefore ? before : after);

  let left, top;
  if (vertical) {
    top = useBefore ? anchor.y - gap - h : anchor.y + anchor.h + gap;
    left = alignAxis(anchor.x, anchor.w, w, align, vp.w, pad);
  } else {
    left = useBefore ? anchor.x - gap - w : anchor.x + anchor.w + gap;
    top = alignAxis(anchor.y, anchor.h, h, align, vp.h, pad);
  }
  const c = clampBox({ left, top, w, h }, { pad, vp });
  return {
    left: c.left,
    top: c.top,
    side: vertical ? (useBefore ? "top" : "bottom") : useBefore ? "left" : "right",
    room,
  };
}

// The free axis. "start" flips to "end" when the box would run off the far
// edge — a context menu opened near the right edge should hang leftward from
// the cursor rather than sliding sideways away from it.
function alignAxis(pos, span, size, align, extent, pad) {
  let v =
    align === "center" ? pos + span / 2 - size / 2 : align === "end" ? pos + span - size : pos;
  if (align === "start" && v + size > extent - pad) v = pos + span - size;
  else if (align === "end" && v < pad) v = pos;
  return v;
}
