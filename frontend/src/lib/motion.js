// motion.js — the one switch for reduced motion.
//
// Two things ask for less motion: the operating system's setting, and
// Appearance › Motion ("Full" or "Reduced", a device-local pref). Either one
// stamps `data-motion="reduced"` on <html> (applyAppearance), and that one
// attribute is what every stylesheet keys off, so a person who asks inside the
// app gets exactly what the OS setting gets. A media query alone could not do
// that: CSS has no way to hear an in-app preference, and the app's stand-downs
// used to be a hundred `@media (prefers-reduced-motion)` blocks.
//
// Script that animates on its own (Svelte transitions, smooth scrolls, the
// celebration bursts, the sheet physics) asks reducedMotion() instead of the
// media query, for the same reason.

const os = typeof matchMedia === "function" ? matchMedia("(prefers-reduced-motion: reduce)") : null;

// motionMode is the attribute's value for a pref: reduced when either asks.
export function motionMode(pref) {
  return pref === "reduced" || os?.matches ? "reduced" : "full";
}

export function reducedMotion() {
  const v = typeof document !== "undefined" ? document.documentElement.dataset.motion : "";
  return v ? v === "reduced" : !!os?.matches;
}

// The OS setting can change while the app is open.
export function onOsMotionChange(fn) {
  os?.addEventListener?.("change", fn);
}
