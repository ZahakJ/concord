// roving.js — the keyboard contract a `role="toolbar"` promises.
//
// A toolbar is ONE tab stop with ←/→ (and Home/End) moving between its
// buttons. The composer had two elements announcing the role and implementing
// none of it: getting past the icon cluster with the keyboard cost nine Tab
// presses, and past the formatting bar another six, so leaving the composer at
// all took fifteen — after which the next stop was the Moments tray, the
// message feed having been skipped entirely.
//
// Announcing a role without its behaviour is worse than not announcing it, so
// this is that behaviour, as one action both bars use.
//
//   <div role="toolbar" use:roving>…buttons…</div>
//
// It reads the buttons out of the DOM on every keystroke rather than caching
// them, because both bars change what they contain: the composer's cluster
// folds five controls into a menu below 1150px, and the mic swaps for send the
// moment there is something to send. A cached list would rove onto a button
// that is no longer there.

const FOCUSABLE = "button:not([disabled]):not([hidden]), [role='button']:not([aria-disabled='true'])";

function itemsOf(node) {
  return [...node.querySelectorAll(FOCUSABLE)].filter(
    (el) => el.offsetParent !== null || el === document.activeElement,
  );
}

// The stop is remembered as an INDEX, not an element: an element reference
// survives a re-render as a detached node that can still be focused, which is
// how a toolbar ends up with focus on something nobody can see.
export function roving(node) {
  let stop = 0;

  function sync() {
    const items = itemsOf(node);
    if (!items.length) return;
    if (stop >= items.length) stop = items.length - 1;
    items.forEach((el, i) => el.setAttribute("tabindex", i === stop ? "0" : "-1"));
  }

  function onKeydown(e) {
    if (e.altKey || e.ctrlKey || e.metaKey) return;
    const items = itemsOf(node);
    if (!items.length) return;
    const at = items.indexOf(document.activeElement);
    let next = null;
    if (e.key === "ArrowRight") next = items[((at < 0 ? 0 : at) + 1) % items.length];
    else if (e.key === "ArrowLeft") next = items[((at < 0 ? 0 : at) - 1 + items.length) % items.length];
    else if (e.key === "Home") next = items[0];
    else if (e.key === "End") next = items[items.length - 1];
    if (!next) return;
    e.preventDefault();
    stop = items.indexOf(next);
    sync();
    next.focus();
  }

  // Whatever was last focused becomes the tab stop, so returning to the bar
  // lands where you left it rather than at the far left every time.
  function onFocusIn(e) {
    const items = itemsOf(node);
    const i = items.indexOf(e.target);
    if (i >= 0 && i !== stop) {
      stop = i;
      sync();
    }
  }

  node.addEventListener("keydown", onKeydown);
  node.addEventListener("focusin", onFocusIn);
  // The contents change with the draft, the window width and the channel, and
  // every one of those can add or remove a button. One observer per bar is
  // cheap; getting it wrong strands the tab stop on a removed node.
  const mo = new MutationObserver(sync);
  mo.observe(node, { childList: true, subtree: true, attributes: true, attributeFilter: ["disabled", "hidden"] });
  sync();

  return {
    destroy() {
      node.removeEventListener("keydown", onKeydown);
      node.removeEventListener("focusin", onFocusIn);
      mo.disconnect();
    },
  };
}
