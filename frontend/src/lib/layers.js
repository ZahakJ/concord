// layers.js — the vertical order of the app, written down once.
//
// Every floating surface used to pick its own z-index, and they were picked by
// different hands at different times: a dropdown at 80, the dialogs at 100 and
// up, the profile card at 250, the context menu and the phone's sheets at 400.
// Each number was right on the day it was written and wrong the first time
// one surface was opened from inside another. A device picker in Settings ›
// Voice & video, and the role filter in Guild settings › Members, both opened
// at 80 underneath a dialog drawn at 100: the menu was in the DOM, focused and
// listening, and nobody could see it or click it.
//
// The scale (mirrored as --z-* tokens in app.css, for stylesheets):
//
//   page      0     the app itself
//   sticky   30     things pinned inside a scroller (day dividers, toolbars)
//   raised   60     the app's own overlays on its own content (jump pills)
//   dock     90     floating call window, self view: above chat, below layers
//   layer   100     the band every dismissible layer draws in: dialogs,
//                   menus, popovers, pickers, sheets. Inside the band the
//                   ORDER THEY OPENED IN decides, not a constant — see zAbove.
//   veil    210     the join veil: closes over every layer
//   studio  400     full-screen editors raised from inside a dialog
//   fx      420     weather and effects, purely decorative, never clickable
//   toast   450     announcements: above everything you can act on
//   tooltip 500     a tip must never be under the thing it describes
export const Z = {
  page: 0,
  sticky: 30,
  raised: 60,
  dock: 90,
  layer: 100,
  veil: 210,
  studio: 400,
  fx: 420,
  toast: 450,
  tooltip: 500,
};

// zAboveChain — the z-index a surface needs to draw above the stacking chain
// it was opened from, given the numeric z-indexes found walking up from the
// opener (null/NaN/"auto" entries are ignored). Never below `floor`, so a
// menu on the bare page still clears the dock.
export function zAboveChain(chain, floor = Z.layer) {
  let top = -Infinity;
  for (const v of chain) {
    const n = typeof v === "number" ? v : parseInt(v, 10);
    if (Number.isFinite(n) && n > top) top = n;
  }
  return Math.max(floor, Number.isFinite(top) ? top + 1 : floor);
}

// zAbove — the z-index for a body-portalled surface opened from `el`.
//
// A portalled panel competes with whatever is drawn at the root of the page,
// and what it must beat is the stacking context its OPENER lives in: a dialog
// at 104, a profile card at 250, a studio at 400. So the answer is read off
// the opener's own ancestors instead of being a constant anybody has to keep
// in step with the rest of the app — a picker opened from a dialog opened from
// a sheet clears all three without either of them knowing it exists.
export function zAbove(el, floor = Z.layer) {
  const chain = [];
  for (let n = el; n && n.nodeType === 1; n = n.parentElement) {
    const z = getComputedStyle(n).zIndex;
    if (z && z !== "auto") chain.push(z);
  }
  return zAboveChain(chain, floor);
}
