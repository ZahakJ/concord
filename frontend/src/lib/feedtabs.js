// feedtabs.js — the message feed is ONE stop in the Tab order.
//
// The feed already had a roving tab stop on its rows (↑/↓/Home/End move it,
// Enter opens the row's menu), but only the rows roved. Every row's avatar,
// sender name, links, reaction pills and ten-button toolbar stayed at
// tabindex 0, so seven messages were eighty Tab stops and Shift+Tab out of the
// composer walked every message in the channel, one control at a time, before
// it reached anything else on the page.
//
// So a row's own controls are in the Tab order only while focus is inside that
// row: Tab lands on the current row, Tab again steps into ITS controls, and
// Tab past them leaves the feed. Every other row's controls are tabindex -1
// until you arrive on it. The rows come and go under a virtualizer and their
// contents re-render (a reaction lands, a link preview loads), so this is one
// observer on the feed, not a rule each row has to remember.

const INNER =
  'a[href], button, input, select, textarea, summary, [tabindex]:not([data-msg-id]), [contenteditable="true"]';

// rowFocusables — what to manage inside one row: its focusables, minus the
// row itself and anything that is the row of a nested message.
function rowFocusables(row) {
  return [...row.querySelectorAll(INNER)].filter((el) => el.closest("[data-msg-id]") === row);
}

// The attribute that remembers a control's own tabindex while it is parked.
// "" means it had none (a natively focusable element).
const PARKED = "data-feed-tab";

export function park(el) {
  if (!el.hasAttribute(PARKED)) el.setAttribute(PARKED, el.getAttribute("tabindex") ?? "");
  if (el.getAttribute("tabindex") !== "-1") el.setAttribute("tabindex", "-1");
}

export function unpark(el) {
  if (!el.hasAttribute(PARKED)) return;
  const was = el.getAttribute(PARKED);
  el.removeAttribute(PARKED);
  if (was === "") el.removeAttribute("tabindex");
  else el.setAttribute("tabindex", was);
}

export function feedTabs(feed) {
  let frame = 0;
  function sync() {
    frame = 0;
    rescue();
    const active = document.activeElement;
    const here = active && feed.contains(active) ? active.closest("[data-msg-id]") : null;
    for (const row of feed.querySelectorAll("[data-msg-id]")) {
      const open = row === here;
      for (const el of rowFocusables(row)) (open ? unpark : park)(el);
    }
  }
  const later = () => {
    if (!frame) frame = requestAnimationFrame(sync);
  };
  // Focus moving between rows (or out of the feed) is a sync NOW, not next
  // frame: the Tab that follows it is already on its way.
  const now = () => {
    cancelAnimationFrame(frame);
    sync();
  };
  // A control that unmounts while it holds focus — the edit box on save, a
  // reaction pill that goes away, a toolbar that hides — drops focus on
  // <body>, and the next Tab starts from the top of the document. Put it back
  // on the row it was in, which is still there. Checked from the observer as
  // well as on focusout, because an engine need not fire focusout for a
  // focused node that is simply removed.
  let lastEl = null;
  let lastRow = "";
  function rescue() {
    if (!lastEl) return;
    const at = document.activeElement;
    if (lastEl.isConnected) {
      // Still there and focus has moved on: it left on purpose.
      if (!feed.contains(at)) lastEl = null;
      return;
    }
    if (at && at !== document.body && at !== document.documentElement) {
      lastEl = null;
      return;
    }
    const row = lastRow && feed.querySelector(`[data-msg-id="${CSS.escape(lastRow)}"]`);
    lastEl = null;
    row?.focus({ preventScroll: true });
  }
  function onIn(e) {
    lastEl = e.target;
    lastRow = e.target?.closest?.("[data-msg-id]")?.dataset.msgId || "";
    now();
  }
  function onOut() {
    later();
    requestAnimationFrame(rescue);
  }
  feed.addEventListener("focusin", onIn);
  feed.addEventListener("focusout", onOut);
  const mo = new MutationObserver(later);
  mo.observe(feed, { childList: true, subtree: true, attributes: true, attributeFilter: ["tabindex"] });
  sync();
  return {
    destroy() {
      cancelAnimationFrame(frame);
      feed.removeEventListener("focusin", onIn);
      feed.removeEventListener("focusout", onOut);
      mo.disconnect();
    },
  };
}
