// touch.js — mobile touch helpers. Capacitor plugins are reached through the
// runtime global (window.Capacitor.Plugins) rather than imported, so the web/
// desktop bundle carries no Capacitor dependency and these no-op off-device.

// haptic fires a short vibration for a confirmed touch gesture (long-press,
// action-sheet open). Silently does nothing on web/desktop.
export function haptic(style = "medium") {
  const H = window.Capacitor?.Plugins?.Haptics;
  if (!H) return;
  try {
    H.impact({ style: style === "light" ? "LIGHT" : style === "heavy" ? "HEAVY" : "MEDIUM" });
  } catch {
    /* plugin missing or denied — ignore */
  }
}

// longpress is a Svelte action: it calls the handler after the finger is held
// still for `duration` ms, passing a synthetic {clientX, clientY, target} so
// existing context-menu code (which positions at the pointer and inspects
// `target` to specialise the menu) works unchanged. A small move cancels it (so
// it doesn't fire mid-scroll). Only arms for touch input — mouse right-click
// keeps using the native contextmenu event.
export function longpress(node, { handler, duration = 450, moveTolerance = 10 } = {}) {
  let timer = null;
  let startX = 0;
  let startY = 0;

  function clear() {
    if (timer) {
      clearTimeout(timer);
      timer = null;
    }
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
    node.addEventListener("touchmove", onMove, { passive: true });
    node.addEventListener("touchend", clear);
    node.addEventListener("touchcancel", clear);
    timer = setTimeout(() => {
      clear();
      haptic("medium");
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
