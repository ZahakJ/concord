// viewerfit.js — the image viewer's geometry, as pure functions.
//
// Every length is in layout pixels (see place.js). Points and pans are measured
// from the CENTRE of the viewer, which is where the image sits at rest; the
// image is drawn at its fitted size and carries `translate(tx, ty) scale(zoom)`
// with its centre as the transform origin, so zoom 1 is "fitted".

// The margin kept clear around a fitted image. The vertical one leaves room
// for the viewer's button bar above the picture.
export const MARGIN_DESKTOP = { x: 24, y: 64 };
export const MARGIN_PHONE = { x: 8, y: 64 };

export const ZOOM_STEP_CLICK = 2;
export const ZOOM_STEP_WHEEL = 1.25;

// The wheel zooms one ZOOM_STEP_WHEEL per WHEEL_STEP_PX of scroll. A mouse
// notch is one event of about that size; a touchpad (and a touchpad pinch,
// which Chromium sends as ctrl+wheel) sends many small events, which add up.
export const WHEEL_STEP_PX = 100;
// One event at least this large is a mouse notch on its own and takes a step
// even where the platform reports less than WHEEL_STEP_PX per notch.
export const WHEEL_NOTCH_PX = 50;
// After this long without a wheel event, a partial sum is dropped.
export const WHEEL_IDLE_MS = 250;
const LINE_PX = 40; // deltaMode 1 (Firefox reports a notch as 3 lines)
const PAGE_PX = 800; // deltaMode 2

// wheelSteps — feed one wheel event to the accumulator `acc` ({ sum, t }, or
// null at the start). Returns the new accumulator and `steps`: how many zoom
// steps the event completes, negative to zoom in (deltaY < 0 scrolls up). A
// change of direction or a pause starts the sum again.
export function wheelSteps(acc, deltaY, deltaMode, t) {
  const px = deltaMode === 1 ? deltaY * LINE_PX : deltaMode === 2 ? deltaY * PAGE_PX : deltaY;
  let sum = acc && t - acc.t <= WHEEL_IDLE_MS ? acc.sum : 0;
  if (sum && Math.sign(sum) !== Math.sign(px)) sum = 0;
  sum += px;
  const steps = Math.trunc(sum / WHEEL_STEP_PX);
  // A notch that falls short of a full step still takes one.
  if (steps === 0 && Math.abs(px) >= WHEEL_NOTCH_PX) return { acc: { sum: 0, t }, steps: Math.sign(px) };
  return { acc: { sum: sum - steps * WHEEL_STEP_PX, t }, steps };
}

// fitSize — the size the image is drawn at when the viewer opens: the whole
// picture inside the viewport less the margin, keeping its shape, and never
// larger than its natural size. `scale` is displayed / natural.
export function fitSize(natural, viewport, margin = MARGIN_DESKTOP) {
  const m = typeof margin === "number" ? { x: margin, y: margin } : margin;
  const nw = Math.max(1, natural?.w || 0);
  const nh = Math.max(1, natural?.h || 0);
  const aw = Math.max(1, viewport.w - 2 * m.x);
  const ah = Math.max(1, viewport.h - 2 * m.y);
  const scale = Math.min(aw / nw, ah / nh, 1);
  return { w: nw * scale, h: nh * scale, scale };
}

// maxZoom — the furthest the viewer zooms past fit: at least 8×, and always far
// enough to show the picture at twice its natural size.
export function maxZoom(scale) {
  return Math.max(8, 2 / (scale || 1));
}

// clampZoom keeps a zoom between fit (1) and maxZoom.
export function clampZoom(z, scale) {
  return Math.min(maxZoom(scale), Math.max(1, z));
}

// clampPan keeps the picture on screen. Along an axis where the zoomed picture
// is larger than the viewport its edges may not come inside the viewport; along
// an axis where it is smaller it stays centred.
export function clampPan(view, size, viewport) {
  const maxX = Math.max(0, (size.w * view.zoom - viewport.w) / 2);
  const maxY = Math.max(0, (size.h * view.zoom - viewport.h) / 2);
  const c = (v, m) => (m === 0 ? 0 : Math.max(-m, Math.min(m, v)));
  return { zoom: view.zoom, tx: c(view.tx, maxX), ty: c(view.ty, maxY) };
}

// refit — after the window changes size, keep the current view legal for the
// new fit: zoom within its bounds and the picture's edges outside the window.
export function refit(view, size, viewport, scale) {
  const zoom = clampZoom(view.zoom, scale);
  if (zoom === 1) return { zoom: 1, tx: 0, ty: 0 };
  return clampPan({ zoom, tx: view.tx, ty: view.ty }, size, viewport);
}

// zoomAt — zoom to `next` while the picture point under `p` (relative to the
// viewer's centre) stays under it, then clamp. Back at fit, the picture
// re-centres.
export function zoomAt(view, p, next, size, viewport, scale) {
  const z = clampZoom(next, scale);
  if (z === 1) return { zoom: 1, tx: 0, ty: 0 };
  const k = z / view.zoom;
  const moved = {
    zoom: z,
    tx: p.x - (p.x - view.tx) * k,
    ty: p.y - (p.y - view.ty) * k,
  };
  return clampPan(moved, size, viewport);
}

// toggleZoom — what a double-click asks for: from fit to the natural size (100%),
// and from any zoom back to fit. A picture already shown at its natural size
// when fitted has no 100% to go to, so it doubles instead.
export function toggleZoom(zoom, scale) {
  if (zoom > 1.001) return 1;
  const natural = 1 / (scale || 1);
  return natural > 1.001 ? natural : 2;
}
