// touch.js — mobile touch helpers. Capacitor plugins are reached through the
// runtime global (window.Capacitor.Plugins) rather than imported, so the web/
// desktop bundle carries no Capacitor dependency and these no-op off-device.

// haptic fires a short vibration for a confirmed touch gesture — a long-press
// firing, a drawer snapping, a message leaving. Silently does nothing on
// web/desktop. Keep it for things the user CAUSED and would otherwise have to
// look at the screen to confirm; a buzz on every tap is noise.
export function haptic(style = "medium") {
  const H = window.Capacitor?.Plugins?.Haptics;
  if (!H) return;
  try {
    H.impact({ style: style === "light" ? "LIGHT" : style === "heavy" ? "HEAVY" : "MEDIUM" });
  } catch {
    /* plugin missing or denied — ignore */
  }
}

// hapticNotify is the OS's success/warning/error pattern — a different texture
// from impact(), which is what makes "it sent" and "it failed" distinguishable
// in a pocket. Falls back to a plain impact where notification() is missing.
export function hapticNotify(type = "SUCCESS") {
  const H = window.Capacitor?.Plugins?.Haptics;
  if (!H) return;
  try {
    if (H.notification) H.notification({ type });
    else haptic(type === "ERROR" ? "heavy" : "light");
  } catch {
    /* ignore */
  }
}

// longpress is a Svelte action: it calls the handler after the finger is held
// still for `duration` ms, passing a synthetic {clientX, clientY, target} so
// existing context-menu code (which positions at the pointer and inspects
// `target` to specialise the menu) works unchanged. A small move cancels it (so
// it doesn't fire mid-scroll). Only arms for touch input — mouse right-click
// keeps using the native contextmenu event.
//
// 400ms matches Android's own long-press timeout. It used to be 450, which is
// long enough that a user lifts a beat early and gets nothing — and on messages,
// channel rows and members the long-press is the ONLY way to the menu, so a
// missed one reads as the feature not existing. The .lp-press class the node
// wears while the timer runs is the other half of that: without it there is no
// signal at all that the press registered.
export function longpress(node, { handler, duration = 400, moveTolerance = 10 } = {}) {
  let timer = null;
  let pressTimer = null;
  let startX = 0;
  let startY = 0;

  function clear() {
    if (timer) {
      clearTimeout(timer);
      timer = null;
    }
    if (pressTimer) {
      clearTimeout(pressTimer);
      pressTimer = null;
    }
    node.classList.remove("lp-press");
    node.removeEventListener("touchmove", onMove);
    node.removeEventListener("touchend", clear);
    node.removeEventListener("touchcancel", clear);
  }
  function onMove(e) {
    const t = e.touches[0];
    if (!t) return;
    if (Math.abs(t.clientX - startX) > moveTolerance || Math.abs(t.clientY - startY) > moveTolerance) {
      clear();
    }
  }
  function onStart(e) {
    const t = e.touches[0];
    if (!t) return;
    startX = t.clientX;
    startY = t.clientY;
    // Press feedback only once the finger has proven it is STAYING. Applying it
    // on touchstart meant every ordinary scroll-touch scaled and flashed the row
    // under the finger for its first few pixels — the whole feed "shook" while
    // scrolling, which is precisely the effect that got this reported. 180ms is
    // past the point where a scroll or tap has already moved on, and still well
    // before the 400ms menu, so a held press reads as held.
    pressTimer = setTimeout(() => node.classList.add("lp-press"), 180);
    node.addEventListener("touchmove", onMove, { passive: true });
    node.addEventListener("touchend", clear);
    node.addEventListener("touchcancel", clear);
    timer = setTimeout(() => {
      clear();
      // Light, not medium: this fires on every menu open, dozens of times a
      // day, and the sheet sliding in is already the confirmation.
      haptic("light");
      // The finger lift after a long-press still synthesizes a click — which
      // would land on whatever the handler just opened (e.g. the first row of
      // an action sheet). Eat it.
      const eat = (ev) => {
        ev.preventDefault();
        ev.stopPropagation();
      };
      window.addEventListener("click", eat, { capture: true, once: true });
      setTimeout(() => window.removeEventListener("click", eat, { capture: true }), 600);
      // Handlers written for `contextmenu` read e.target to decide what was hit
      // (an inline image gets an image menu, not the message menu). Resolve it
      // from the touch point — `node` would be the whole row and lose that.
      handler?.({
        clientX: startX,
        clientY: startY,
        target: document.elementFromPoint(startX, startY) || node,
        preventDefault() {},
        stopPropagation() {},
      });
    }, duration);
  }

  node.addEventListener("touchstart", onStart, { passive: true });
  return {
    update(params) {
      handler = params.handler;
      duration = params.duration ?? duration;
    },
    destroy() {
      clear();
      node.removeEventListener("touchstart", onStart);
    },
  };
}
