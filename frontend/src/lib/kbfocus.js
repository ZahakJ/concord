// kbfocus.js — raising the phone keyboard from a tap.
//
// A mobile browser shows the software keyboard for a focus() only while it is
// handling the tap that asked for it. Focus the field a frame later (after a
// tick, after an await, after the surface has mounted) and the caret lands with
// no keyboard under it: Reply and Edit looked like they had done nothing until
// you tapped the box a second time.
//
// So the focus has to happen inside the tap handler, synchronously. Where the
// field already exists (the composer, for Reply) that is one call. Where it
// does not exist yet (a DM that opens after a round trip to the core), the tap
// focuses an invisible stand-in so the keyboard comes up NOW, and land() moves
// focus to the real field once it is there. Moving focus from one text field to
// another keeps the keyboard up.

let keeper = null;

function keeperEl() {
  if (keeper?.isConnected) return keeper;
  keeper = document.createElement("textarea");
  keeper.className = "kb-keeper";
  keeper.setAttribute("aria-hidden", "true");
  keeper.tabIndex = -1;
  // 16px: a smaller font makes iOS zoom the page to the field.
  Object.assign(keeper.style, {
    position: "fixed",
    left: "0",
    bottom: "0",
    width: "1px",
    height: "1px",
    opacity: "0",
    padding: "0",
    border: "0",
    fontSize: "16px",
    pointerEvents: "none",
  });
  document.body.appendChild(keeper);
  return keeper;
}

// holdKeyboard: call inside the tap. Focuses `el` if given, else the stand-in.
export function holdKeyboard(el) {
  (el || keeperEl()).focus({ preventScroll: true });
}

// landKeyboard moves focus to the field the stand-in was holding for, with the
// caret at the end, and carries over anything typed in the meantime. With no
// field to land on, the stand-in lets go and the keyboard goes down.
export function landKeyboard(el) {
  const held = keeper && document.activeElement === keeper ? keeper.value : "";
  if (keeper) keeper.value = "";
  if (!el) {
    if (keeper && document.activeElement === keeper) keeper.blur();
    return false;
  }
  el.focus({ preventScroll: true });
  if (typeof el.setSelectionRange === "function") {
    if (held) {
      el.setRangeText(held, el.value.length, el.value.length, "end");
      el.dispatchEvent(new Event("input", { bubbles: true }));
    }
    const n = el.value.length;
    el.setSelectionRange(n, n);
  }
  return true;
}

// keepInView keeps `row` visible inside its scroller while the keyboard comes
// up and the feed above the composer shrinks under it. The keyboard's arrival
// is a resize the page sees over several frames (and, in the Android shell, a
// value pushed from native code), so this watches for a short while rather
// than guessing one moment.
export function keepInView(row, { ms = 900 } = {}) {
  if (!row) return;
  const scroller = row.closest(".feed");
  if (!scroller) return;
  const until = performance.now() + ms;
  const step = () => {
    if (!row.isConnected) return;
    const f = scroller.getBoundingClientRect();
    const r = row.getBoundingClientRect();
    // Taller than the feed: show its last lines, the part nearest the reply.
    if (r.bottom > f.bottom) scroller.scrollTop += Math.ceil(r.bottom - f.bottom) + 8;
    else if (r.top < f.top) scroller.scrollTop -= Math.ceil(f.top - r.top) + 8;
    if (performance.now() < until) requestAnimationFrame(step);
  };
  requestAnimationFrame(step);
}
