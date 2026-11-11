// regions.js — F6 / Shift+F6: move between the app's regions.
//
// The shell is five regions in reading order: the sidebar, the conversation's
// header, the feed, the composer and the inspector. Tab walks every control on
// the way, which from the composer to the channel list is dozens of stops; F6
// is the jump between regions, and it lands where you last were inside the
// region rather than at its top.

export const REGIONS = [
  { id: "sidebar", sel: 'nav[aria-label="Guilds and channels"]' },
  { id: "header", sel: ".chat-head" },
  { id: "feed", sel: '.feed[role="log"]' },
  { id: "composer", sel: "textarea.draft" },
  { id: "inspector", sel: 'aside[aria-label="Inspector"]' },
];

// nextRegion(present, current, dir) -> the id to go to. `present` is the ids
// on screen, in order; `current` the region focus is in ("" for none). With
// no current region, forward starts at the first and backward at the last.
export function nextRegion(present, current, dir = 1) {
  if (!present.length) return "";
  const i = present.indexOf(current);
  if (i < 0) return dir > 0 ? present[0] : present[present.length - 1];
  return present[(i + dir + present.length) % present.length];
}

const visible = (el) => !!el && el.getClientRects().length > 0;
const FOCUSABLE = 'button:not([disabled]), a[href], input:not([disabled]), textarea:not([disabled]), select, [tabindex]:not([tabindex="-1"])';

// cycleRegion moves focus to the next region on screen. `last` remembers the
// element each region last held; `focusFeed` is the feed's own roving entry.
const last = new Map();
export function cycleRegion(dir, { doc = document, focusFeed } = {}) {
  const roots = REGIONS.map((r) => ({ ...r, el: doc.querySelector(r.sel) })).filter((r) => visible(r.el));
  const active = doc.activeElement;
  const cur = roots.find((r) => r.el === active || r.el.contains(active));
  if (cur && active !== cur.el) last.set(cur.id, active);
  const id = nextRegion(
    roots.map((r) => r.id),
    cur?.id || "",
    dir,
  );
  const to = roots.find((r) => r.id === id);
  if (!to) return false;
  const remembered = last.get(id);
  if (remembered && to.el.contains(remembered) && visible(remembered)) {
    remembered.focus();
    return true;
  }
  if (id === "feed" && focusFeed?.()) return true;
  if (to.el.matches(FOCUSABLE)) {
    to.el.focus();
    return true;
  }
  // The sidebar's natural landing is the place you are in.
  const pick =
    (id === "sidebar" && to.el.querySelector(".channel-row.active .channel")) ||
    [...to.el.querySelectorAll(FOCUSABLE)].find(visible);
  if (pick) {
    pick.focus();
    return true;
  }
  return false;
}
