import { tick } from "svelte";

// focusOnMount — the one way an inline surface claims the caret.
//
// `autofocus` does not work here and never did. The HTML attribute is honoured
// once per Document, at parse time; every field in this app is inserted years
// later in page-lifetime terms, so the browser has long since stopped looking.
// The worklog recorded that for the edit box and fixed it there, and the
// attribute survived on five other inline fields where it reads exactly like a
// working feature. What it actually bought was:
//
//   • "Start thread" opened a title field nothing focused, `typeToFocus` pulled
//     the next keystroke into the message draft, and Enter POSTED the thread
//     title to the channel as an ordinary message, publicly, for everyone.
//   • The emoji picker's search never took focus, so typing to filter typed
//     into your message instead — while the picker sat open over it.
//
// Modals are the exception and stay on the attribute: Modal.svelte reads
// `[autofocus]` as a MARKER to decide what to focus, so there it is load-bearing
// markup rather than a dead browser feature. Everything else uses this.
//
// The timing is the edit box's, and it is not superstition. `tick()` waits for
// the DOM to settle, and the frame after it waits for two things that land
// later: a `bind:value` writing the element's value (which resets the caret to
// 0, throwing away any selection placed before it), and the context menu's
// focus RESTORE, which fires from an effect teardown when the menu closes and
// otherwise lands on the row the menu was opened from — after the surface the
// menu just opened has mounted. Whoever focuses last wins, and this is designed
// to be last.
//
// Options: `select` selects the whole value (a rename box, where the first
// keystroke usually replaces the name); `end` puts the caret after the text (an
// edit box, where it usually continues). Neither, and the caret lands wherever
// the browser puts it.
export function focusOnMount(node, opts = {}) {
  let cancelled = false;
  let frame = 0;
  const place = (o) => {
    tick().then(() => {
      if (cancelled || !node.isConnected) return;
      frame = requestAnimationFrame(() => {
        if (cancelled || !node.isConnected) return;
        node.focus({ preventScroll: true });
        if (typeof node.setSelectionRange !== "function") return;
        const n = node.value?.length ?? 0;
        if (o.select) node.setSelectionRange(0, n);
        else if (o.end) node.setSelectionRange(n, n);
      });
    });
  };
  // `skip` exists for the one honest reason to decline: a phone, where taking
  // focus raises the software keyboard over the thing the user came to look at.
  // Written as an option rather than left to each call site so the reason is
  // recorded once.
  if (!opts.skip) place(opts);
  return {
    update(next = {}) {
      // Re-arm when a call site flips `skip` off (a picker reopening, a field
      // that becomes eligible). Same guard, so it stays a no-op otherwise.
      if (!next.skip && opts.skip) place(next);
      opts = next;
    },
    destroy() {
      cancelled = true;
      cancelAnimationFrame(frame);
    },
  };
}
