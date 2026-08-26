// fieldEscape — Escape leaves the FIELD before it leaves the layer.
//
// A popover that contains a text box has two things a dismissal could plausibly
// mean, and until now every one of them picked the second: Escape went straight
// to navstack, the popover closed, and whatever had been typed into it went with
// it. Setting a nickname or writing a status is a sentence or two of work, and
// Escape is the reflex for "no, not that" — losing the whole thing to it is a
// harsh reading of a very common keypress.
//
// So: with the caret in one of the layer's own fields, the first Escape steps
// OUT of the field and stops there. The draft is still on screen, still
// editable, and a second Escape — now with focus outside the field — falls
// through to the layer stack and closes the popover as it always did. Nothing
// is destroyed without being asked twice.
//
// Capture phase, for the same reason RichEditor's unwind uses it: lib/shortcuts
// listens on window in the bubble phase and would have popped the layer before
// a bubble-phase handler here ever ran.
//
// Deliberately NOT applied where the field IS the layer's state — the emoji
// picker's search box clears itself instead, because an emptied search is the
// meaningful middle rung there and an unfocused one is not.
export function fieldEscape(node) {
  const onKey = (e) => {
    if (e.key !== "Escape") return;
    const a = document.activeElement;
    if (!a || !node.contains(a)) return;
    if (!a.matches?.("input, textarea, [contenteditable='true']")) return;
    e.preventDefault();
    e.stopPropagation();
    a.blur();
  };
  window.addEventListener("keydown", onKey, true);
  return {
    destroy() {
      window.removeEventListener("keydown", onKey, true);
    },
  };
}
