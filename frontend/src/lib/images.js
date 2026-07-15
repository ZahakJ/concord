// images.js — one place for image-data-URI safety. Banner/avatar values can
// land inside a CSS `url("…")`, so before emitting one we confirm it's a plain
// base64 raster data URI (no SVG, no CSS breakout). Mirrors the backend's
// validImageDataURI (internal/app/service.go) — keep the two in lockstep.
const SAFE_IMAGE_DATA_URI = /^data:image\/(png|jpe?g|gif|webp);base64,[A-Za-z0-9+/=]+$/;

export function isSafeImageDataURI(s) {
  return typeof s === "string" && SAFE_IMAGE_DATA_URI.test(s);
}
