// sheet.js — the phone's bottom-sheet physics, in one place.
//
// There were three of these, and they did not feel like the same app.
// BottomSheet.svelte listened for touch events, dismissed past 40% of its own
// height or a fling of 0.55 px/ms, tracked the scrim to the pull — and then
// vanished instantly, with no exit, while the finger was still moving.
// modals/Modal.svelte listened for pointer events, dismissed past a flat 120px
// or 0.6 px/ms, refused the gesture when its body was scrolled, and DID play an
// exit. ProfilePopover and StatusPopover drew a grip and wired nothing to it at
// all: the profile card's handle was a button that closed on click, which is
// worse than no handle — it invites a pull and answers with a tap.
//
// One set of numbers now, and one implementation of the behaviour. The
// presentations stay different, because a dialog, an action sheet and a profile
// card are different things; what a finger does to them is not.
//
// The physics, stated once:
//   • pointer events, so a trackpad drag works and a captured pointer cannot be
//     lost to a scroll the browser decides to start halfway through;
//   • the sheet tracks the finger 1:1 downward and never upward;
//   • the scrim lightens as it goes, so the dim reads as attached to the sheet
//     rather than to the screen;
//   • it goes if it was pulled past a third of its own height (capped at 120px,
//     so a tall sheet is not a workout) OR flung faster than 0.6 px/ms;
//   • otherwise it springs back;
//   • a committed dismissal is announced in the hand and then ANIMATES out.
//     Every dismissal does — the ✕ and the back button use the same exit, which
//     is why playExit is exported rather than living inside the drag;
//   • and the gesture is refused outright while the body is scrolled, so the
//     header hands the move to the scroller instead. Deciding that per-move
//     made a pinned header a dead zone on exactly the tall sheets that need it.
//
// That last point is also why this action writes `touch-action` itself. A grab
// strip declared `touch-action: pan-y` — which is what the dialog sheets had,
// so a scrolled sheet could still be scrolled from its header — hands every
// downward drag to the browser as a scroll, and the browser answers the second
// move with `pointercancel`. Drag-to-dismiss on all 49 dialogs was dead on
// Android for exactly that reason: it was measured here at two events, a
// pointerdown and one pointermove, before the stream was taken away. The value
// has to be right BEFORE the finger lands (the compositor reads it at gesture
// start), so it is kept in step with the scroller's position rather than
// decided on contact: `none` at the top, where the strip owns the gesture, and
// `pan-y` once there is something below to scroll back up to.
import { haptic } from "./touch.js";

const DISMISS_PX = 120; // absolute cap on the pull a dismissal needs
const DISMISS_FRACTION = 0.33; // …or a third of the sheet, whichever is less
const FLING = 0.6; // px/ms, downward
const EXIT_MS = 190;
const SETTLE = "transform 0.24s cubic-bezier(0.22, 1.1, 0.36, 1)";
const EXIT = `transform ${EXIT_MS}ms cubic-bezier(0.4, 0, 1, 1)`;
const SCRIM_MIN = 0.25; // the dim never goes fully clear mid-pull

const reduced = () =>
  typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;

// playExit slides a sheet out under the bottom edge and calls `done` when it
// has landed. Reduced motion gets there immediately — waiting would be 190ms of
// nothing happening.
export function playExit(sheet, scrim, done) {
  if (!sheet || reduced()) {
    done?.();
    return;
  }
  sheet.style.animation = "none"; // beat any entrance still running
  sheet.style.transition = EXIT;
  sheet.style.transform = "translateY(100%)";
  if (scrim) {
    scrim.style.transition = `opacity ${EXIT_MS}ms ease`;
    scrim.style.opacity = "0";
  }
  setTimeout(() => done?.(), EXIT_MS);
}

// sheetdrag — a Svelte action for the GRAB area (the grip and the title strip
// that travels with it). It moves elements it is handed rather than owning any
// markup, which is what lets one physics serve four different sheets.
//
// params:
//   sheet()     the element that slides            (required)
//   scrim()     the dim behind it, dimmed in step  (optional)
//   scroller()  what scrolls inside                (optional; defaults to sheet)
//   onDismiss() called once the exit has played    (required)
//   enabled     false on desktop, where there is no gesture to make
export function sheetdrag(node, params) {
  let p = params;
  let dragging = false;
  let startY = 0;
  let startT = 0;
  let dy = 0;
  let height = 0;
  let done = false; // an exit is playing; ignore everything

  const sheetEl = () => p.sheet?.();
  const scrimEl = () => p.scrim?.() || null;

  function paint(y) {
    const s = sheetEl();
    if (s) s.style.transform = y ? `translateY(${y}px)` : "";
    const sc = scrimEl();
    if (sc && height) sc.style.opacity = String(Math.max(SCRIM_MIN, 1 - y / height));
  }

  function onDown(e) {
    if (p.enabled === false || done || dragging) return;
    // Primary pointer only: a second finger arriving mid-drag must not re-seed
    // the origin and teleport the sheet.
    if (e.isPrimary === false) return;
    const sc = p.scroller?.() || sheetEl();
    if ((sc?.scrollTop ?? 0) > 0) return;
    const s = sheetEl();
    if (!s) return;
    dragging = true;
    height = s.offsetHeight || 1;
    startY = e.clientY;
    startT = performance.now();
    dy = 0;
    s.style.transition = "none";
    const scr = scrimEl();
    if (scr) scr.style.transition = "none";
    node.setPointerCapture?.(e.pointerId);
  }

  function onMove(e) {
    if (!dragging) return;
    dy = Math.max(0, e.clientY - startY); // down only
    paint(dy);
  }

  function onUp(e) {
    if (!dragging) return;
    dragging = false;
    node.releasePointerCapture?.(e.pointerId);
    const speed = dy / Math.max(1, performance.now() - startT);
    const threshold = Math.min(DISMISS_PX, height * DISMISS_FRACTION);
    if (dy > threshold || speed > FLING) {
      done = true;
      haptic("light"); // the OS acknowledges a committed dismissal
      playExit(sheetEl(), scrimEl(), () => p.onDismiss?.());
      return;
    }
    // Spring back, and hand the styles back to the stylesheet afterwards so a
    // later entrance animation is not fighting an inline transition.
    const s = sheetEl();
    const scr = scrimEl();
    if (s) {
      s.style.transition = SETTLE;
      s.style.transform = "";
    }
    if (scr) {
      scr.style.transition = "opacity 0.18s ease";
      scr.style.opacity = "";
    }
  }

  // Keep touch-action in step with what is under the strip. Bound to the
  // scroller, so a sheet with nothing to scroll simply stays at `none`.
  let watched = null;
  function syncTouchAction() {
    if (p.enabled === false) return;
    node.style.touchAction = (watched?.scrollTop ?? 0) > 0 ? "pan-y" : "none";
  }
  function rewatch() {
    const next = p.scroller?.() || sheetEl();
    if (next === watched) return;
    watched?.removeEventListener("scroll", syncTouchAction);
    watched = next;
    watched?.addEventListener("scroll", syncTouchAction, { passive: true });
    syncTouchAction();
  }
  rewatch();
  // An action runs before the component's own bind:this assignments have all
  // landed, so the scroller may not exist yet on the first pass.
  queueMicrotask(rewatch);

  node.addEventListener("pointerdown", onDown);
  node.addEventListener("pointermove", onMove);
  node.addEventListener("pointerup", onUp);
  node.addEventListener("pointercancel", onUp);

  return {
    update(next) {
      p = next;
      rewatch();
    },
    destroy() {
      watched?.removeEventListener("scroll", syncTouchAction);
      node.removeEventListener("pointerdown", onDown);
      node.removeEventListener("pointermove", onMove);
      node.removeEventListener("pointerup", onUp);
      node.removeEventListener("pointercancel", onUp);
    },
  };
}
