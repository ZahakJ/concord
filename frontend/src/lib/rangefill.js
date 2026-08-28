// rangefill.js — use:rangefill, the one thing a styled range slider cannot do
// on its own.
//
// app.css draws the track, the thumb and the focus ring for every
// `input[type="range"]` in the app. The *filled* portion — the accent-coloured
// run from the left edge to the thumb — is the one part CSS cannot compute,
// because no selector knows where the thumb is. Firefox exposes
// `::-moz-range-progress`; WebKit and Blink expose nothing equivalent, and the
// usual workaround (a huge `box-shadow` on the thumb, clipped by `overflow:
// hidden`) clips to the input's own box rather than to the 4px track and paints
// a full-height bar. The shipping desktop build is WebKitGTK, so an engine
// trick that only demonstrably works in the Chromium used for verification is
// not good enough.
//
// So the position is measured here and published as `--range-pct`, which
// app.css uses as a gradient stop. `input` covers dragging; the action's
// `update` covers a value written from code (a "Reset zoom" button, a preset
// applied to a dial) — a delegated document listener would have missed exactly
// those, and left the fill disagreeing with the thumb.
//
// Usage: `<input type="range" bind:value={zoom} use:rangefill={zoom} />` — the
// parameter exists only to re-run on change; the value is always read back off
// the element, so a caller cannot make the two disagree by passing the wrong
// thing.
function paint(el) {
  const min = Number(el.min === "" ? 0 : el.min);
  const max = Number(el.max === "" ? 100 : el.max);
  const span = max - min;
  const pct = span > 0 ? ((Number(el.value) - min) / span) * 100 : 0;
  el.style.setProperty("--range-pct", `${Math.max(0, Math.min(100, pct))}%`);
}

export function rangefill(el) {
  const on = () => paint(el);
  el.addEventListener("input", on);
  paint(el);
  return {
    update: () => paint(el),
    destroy: () => el.removeEventListener("input", on),
  };
}
