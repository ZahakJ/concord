// popoverfocus.js — a popover that is a dialog takes the keyboard with it.
//
// The profile card and the status popover are role="dialog", and opening
// either from the keyboard left focus on the trigger behind it: Tab walked on
// through the page, and the safety number, "Message …", the status presets
// and the custom status were mouse-only. A dialog that does not take focus is
// a promise the keyboard cannot cash.
//
//   <div role="dialog" use:popoverFocus={!openedByHover}>
//
// On open, focus goes to the first control (not a text field: on a phone that
// would raise the keyboard over the card), or the popover itself. Tab and
// Shift+Tab cycle inside it. Escape and the way back are the layer stack's
// (lib/navstack.svelte.js), which also returns focus to the trigger.

import { tabbables } from "./menukeys.js";

function shown(node) {
  const cs = getComputedStyle(node);
  return cs.visibility !== "hidden" && cs.pointerEvents !== "none" && Number(cs.opacity) > 0;
}

export function popoverFocus(node, enabled = true) {
  let frame = 0;
  let tries = 30;
  function place() {
    frame = 0;
    if (!node.isConnected) return;
    // The card is placed after it is measured, and drawn transparent and
    // inert until then; focus must wait for it.
    if (!shown(node) && tries-- > 0) {
      frame = requestAnimationFrame(place);
      return;
    }
    if (node.contains(document.activeElement)) return;
    const first = tabbables(node).find((el) => el.tagName !== "INPUT" && el.tagName !== "TEXTAREA");
    if (first) first.focus({ preventScroll: true });
    else {
      if (!node.hasAttribute("tabindex")) node.setAttribute("tabindex", "-1");
      node.focus({ preventScroll: true });
    }
  }
  function onKey(e) {
    if (e.key !== "Tab" || e.altKey || e.ctrlKey || e.metaKey) return;
    const list = tabbables(node);
    if (!list.length) return;
    const i = list.indexOf(document.activeElement);
    const next = e.shiftKey ? (i <= 0 ? list[list.length - 1] : list[i - 1]) : i < 0 || i === list.length - 1 ? list[0] : list[i + 1];
    e.preventDefault();
    next.focus();
  }
  node.addEventListener("keydown", onKey);
  if (enabled) frame = requestAnimationFrame(place);
  return {
    destroy() {
      cancelAnimationFrame(frame);
      node.removeEventListener("keydown", onKey);
    },
  };
}
