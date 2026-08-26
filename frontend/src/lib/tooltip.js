// tooltip.js — use:tooltip, the shared hover tooltip for primary chrome.
//
// Native title= is the wrong tool for the guild rail and header icon buttons:
// the OS sits on it for ~1s, renders it in the platform theme (not ours), and
// its delay can't be tuned per surface. This action replaces it with ONE
// positioned <div> reused app-wide — created lazily on first show, styled by a
// <style> tag injected alongside it, both living outside any component so no
// app.css edit is needed and unmounting a component never tears the tip down.
//
// Text comes from the element's aria-label unless a string / {text} is passed,
// so accessibility and the tooltip can't drift apart: the label IS the tip.
// aria-label stays on the element; only title= gets removed by callers.
//
// Options: use:tooltip, use:tooltip={"text"}, or
//   use:tooltip={{ text, side: "right"|"bottom", delay }}.
// The rail passes delay: 80 (identifying an icon-only column should feel
// instant); everything else keeps the ~300ms default so sweeping the pointer
// across the header doesn't strobe. Flips to the opposite side when the
// preferred one would leave the viewport, and clamps to it either way.

// Touch has no hover: a tap would both act AND flash a tooltip, so bail out
// entirely on coarse pointers — same guard the rail uses for its menus.
const coarse = window.matchMedia?.("(pointer: coarse)")?.matches ?? false;

const GAP = 8; // anchor → tip distance
const PAD = 6; // minimum viewport inset

let tip = null; // the one shared element (lazy)
let anchor = null; // node currently owning the tip, if visible
let showTimer = 0; // module-level: only one tip can ever be pending

function ensureTip() {
  if (tip) return tip;
  const style = document.createElement("style");
  // --bg-3 + --shadow-pop is the house voice for floating chrome (Menu,
  // ContextMenu); the tokens re-resolve per theme, so the tip follows the
  // active skin with no work here.
  style.textContent = `
    .app-tooltip {
      position: fixed;
      z-index: 500; /* above ContextMenu (401) — a tip must never be under a menu it describes */
      display: none;
      max-width: 260px;
      padding: 5px 9px;
      background: var(--bg-3, var(--bg-2));
      border: 1px solid var(--border);
      border-radius: var(--radius-md);
      color: var(--text);
      font-size: var(--fs-tiny);
      font-weight: 600;
      line-height: 1.35;
      overflow-wrap: break-word;
      box-shadow: var(--shadow-pop, 0 4px 12px rgba(0, 0, 0, 0.3));
      pointer-events: none; /* the tip must never steal the hover that opened it */
      opacity: 0;
      transition: opacity var(--dur-quick) ease;
    }
    .app-tooltip.show { opacity: 1; }
    @media (prefers-reduced-motion: reduce) {
      .app-tooltip { transition: none; }
    }
  `;
  document.head.appendChild(style);
  tip = document.createElement("div");
  tip.className = "app-tooltip";
  // The tip mirrors an aria-label the element already carries — exposing it
  // would make screen readers announce everything twice.
  tip.setAttribute("aria-hidden", "true");
  document.body.appendChild(tip);
  return tip;
}

function show(node, opts) {
  // Read at show time, not mount time: rail labels are live ("N message
  // requests waiting") and must not fossilise.
  const text = opts.text || node.getAttribute("aria-label") || "";
  if (!text) return;
  const el = ensureTip();
  el.textContent = text;
  // Park it off-screen to measure — positioning needs the wrapped size.
  el.classList.remove("show");
  el.style.left = "-9999px";
  el.style.top = "0px";
  el.style.display = "block";
  const w = el.offsetWidth;
  const h = el.offsetHeight;
  const r = node.getBoundingClientRect();
  let x, y;
  if (opts.side === "right") {
    x = r.right + GAP;
    if (x + w > window.innerWidth - PAD) x = r.left - GAP - w; // flip left
    y = r.top + r.height / 2 - h / 2;
  } else {
    y = r.bottom + GAP;
    if (y + h > window.innerHeight - PAD) y = r.top - GAP - h; // flip above
    x = r.left + r.width / 2 - w / 2;
  }
  el.style.left = `${Math.max(PAD, Math.min(x, window.innerWidth - PAD - w))}px`;
  el.style.top = `${Math.max(PAD, Math.min(y, window.innerHeight - PAD - h))}px`;
  anchor = node;
  // Next frame so the display flip commits first and the opacity transition
  // actually runs (reduced-motion zeroes it via the media query above).
  requestAnimationFrame(() => {
    if (anchor === node) el.classList.add("show");
  });
  // A scroll anywhere (the rail is its own scroller) moves the anchor out from
  // under the tip — hide rather than chase it. Capture catches inner scrollers.
  window.addEventListener("scroll", hide, true);
}

function hide() {
  clearTimeout(showTimer);
  showTimer = 0;
  anchor = null;
  if (!tip) return;
  tip.classList.remove("show");
  tip.style.display = "none";
  window.removeEventListener("scroll", hide, true);
}

const normalize = (params) =>
  typeof params === "string"
    ? { text: params, side: "bottom", delay: 300 }
    : { text: params?.text || "", side: params?.side || "bottom", delay: params?.delay ?? 300 };

export function tooltip(node, params = {}) {
  if (coarse) return {};
  let opts = normalize(params);

  const cancel = () => {
    clearTimeout(showTimer);
    showTimer = 0;
  };
  const enter = () => {
    cancel();
    showTimer = setTimeout(() => show(node, opts), opts.delay);
  };
  // Keyboard focus gets the tip immediately: tabbing is deliberate, and the
  // delay only exists to keep pointer sweeps quiet. :focus-visible skips
  // click-focus, which would double the pointer path's tip.
  const focus = () => {
    if (node.matches(":focus-visible")) {
      cancel();
      show(node, opts);
    }
  };
  // pointerdown too: the click's result (a menu, a modal, a navigation) is the
  // feedback now, and a tip lingering over it reads as debris.
  const leave = () => {
    cancel();
    if (anchor === node) hide();
  };

  node.addEventListener("pointerenter", enter);
  node.addEventListener("pointerleave", leave);
  node.addEventListener("pointerdown", leave);
  node.addEventListener("focus", focus);
  node.addEventListener("blur", leave);

  return {
    update(p) {
      opts = normalize(p);
      // Live label changed under an open tip (mute toggled, count moved) —
      // re-render in place rather than showing stale text.
      if (anchor === node) show(node, opts);
    },
    destroy() {
      leave();
      node.removeEventListener("pointerenter", enter);
      node.removeEventListener("pointerleave", leave);
      node.removeEventListener("pointerdown", leave);
      node.removeEventListener("focus", focus);
      node.removeEventListener("blur", leave);
    },
  };
}
