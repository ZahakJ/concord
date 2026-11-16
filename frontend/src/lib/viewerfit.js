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
