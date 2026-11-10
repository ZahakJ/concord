// menukeys.js — the keyboard half of a menu, and of leaving one.
//
// Kept out of the components so the rules can be tested without a browser:
// Menu.svelte and ContextMenu.svelte both promise role="menu", and a menu is a
// promise about which row the next key lands on.

// menuKeyTarget — which row a keydown moves to, or null if the key is not one
// a menu answers. `items` are the enabled rows in visual order; `active` is the
// row that has focus now (or anything else, meaning "none of them").
//
//   ↓ / ↑      next / previous, wrapping
//   Home / End first / last
//   a letter   the next row whose label starts with it, after the current
//              one, wrapping — so pressing D twice in a menu with "Delete"
//              and "Download" visits both
export function menuKeyTarget(items, active, e) {
  const n = items.length;
  if (!n || e.altKey || e.ctrlKey || e.metaKey) return null;
  const i = items.indexOf(active);
  if (e.key === "ArrowDown") return items[i < 0 ? 0 : (i + 1) % n];
  if (e.key === "ArrowUp") return items[i < 0 ? n - 1 : (i - 1 + n) % n];
  if (e.key === "Home") return items[0];
  if (e.key === "End") return items[n - 1];
  if (e.key && e.key.length === 1 && e.key !== " ") {
    const ch = e.key.toLocaleLowerCase();
    for (let k = 1; k <= n; k++) {
      const it = items[(Math.max(i, -1) + k + n) % n];
      if (labelOf(it).startsWith(ch)) return it;
    }
  }
  return null;
}

// The words a row is known by: its accessible name if it has one, else its
// text with any leading glyphs (an icon's text, an emoji) stripped.
function labelOf(el) {
  const raw = el?.getAttribute?.("aria-label") || el?.textContent || "";
  return raw.replace(/^[^\p{L}\p{N}]+/u, "").trim().toLocaleLowerCase();
}

const TABBABLE =
  'a[href],button:not([disabled]),textarea:not([disabled]),input:not([disabled]):not([type="hidden"]),' +
  'select:not([disabled]),[tabindex]:not([tabindex="-1"]),[contenteditable="true"]';

// tabbables — what Tab visits inside `scope`, in order. Hidden, inert and
// negative-tabindex elements are not stops.
export function tabbables(scope) {
  return [...scope.querySelectorAll(TABBABLE)].filter(
    (el) =>
      el.tabIndex >= 0 &&
      !el.closest("[inert]") &&
      !el.closest('[role="menu"]') &&
      (el.offsetWidth || el.offsetHeight || el.getClientRects().length) &&
      getComputedStyle(el).visibility !== "hidden",
  );
}

// nextStop — the pure half of moveFocusFrom: the stop after (dir 1) or before
// (dir -1) `from` in `list`, wrapping only when `wrap` is set (inside a modal
// dialog, whose Tab trap wraps; on the page, Tab runs off the end).
export function nextStop(list, from, dir, wrap) {
  const i = list.indexOf(from);
  if (i < 0) return list[dir > 0 ? 0 : list.length - 1] ?? null;
  const j = i + dir;
  if (j >= 0 && j < list.length) return list[j];
  return wrap && list.length ? list[(j + list.length) % list.length] : null;
}

// moveFocusFrom — do what Tab would have done had focus been on `from`. Used
// when a surface that lives elsewhere in the DOM (a portalled menu) gives the
// key back to the control that opened it. Inside a modal dialog the walk stays
// in the dialog, the same as its trap.
export function moveFocusFrom(from, dir) {
  const dialog = from.closest('[role="dialog"][aria-modal="true"]');
  const scope = dialog || document.body;
  const next = nextStop(tabbables(scope), from, dir, !!dialog);
  next?.focus({ preventScroll: false });
}
