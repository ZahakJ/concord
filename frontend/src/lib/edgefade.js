// edgefade.js — soft edges on a scrolling list. Where content passes under an
// edge (the header above the feed, the composer below it) it fades out over a
// few pixels instead of being cut by a hard line or sitting under a solid band.
//
// The fade is a mask on the scroller itself (app.css .edge-fade), so it costs
// no element, follows every theme and wallpaper (it reveals what is behind,
// rather than painting a colour that must match it), and is composited, not
// repainted, as the list scrolls. Each edge fades only while there is content
// beyond it: at rest at the end of the feed nothing is faded, so the last
// message reads at full strength.

// Within this many pixels of an edge counts as at it (fractional scroll
// positions on high-density screens never land exactly on 0 or the end).
export const FADE_SLACK = 2;

// edgeState: which edges have content beyond them — "", "top", "bottom" or
// "both" — for the element's data-fade attribute.
export function edgeState(scrollTop, scrollHeight, clientHeight) {
  const top = scrollTop > FADE_SLACK;
  const bottom = scrollHeight - clientHeight - scrollTop > FADE_SLACK;
  return top && bottom ? "both" : top ? "top" : bottom ? "bottom" : "";
}

// edgeFade: a Svelte action keeping node.dataset.fade current as it scrolls,
// resizes or its content changes. One passive listener and one observer, the
// work batched to a frame.
export function edgeFade(node) {
  let raf = 0;
  const update = () => {
    raf = 0;
    const s = edgeState(node.scrollTop, node.scrollHeight, node.clientHeight);
    if (node.dataset.fade !== s) node.dataset.fade = s;
  };
  const soon = () => {
    if (!raf) raf = requestAnimationFrame(update);
  };
  node.classList.add("edge-fade");
  node.addEventListener("scroll", soon, { passive: true });
  const ro =
    typeof ResizeObserver !== "undefined" ? new ResizeObserver(soon) : null;
  ro?.observe(node);
  // Content growing (a message arriving, a picture loading) changes what is
  // beyond the bottom edge without a scroll.
  const mo =
    typeof MutationObserver !== "undefined" ? new MutationObserver(soon) : null;
  mo?.observe(node, { childList: true, subtree: true });
  update();
  return {
    destroy() {
      if (raf) cancelAnimationFrame(raf);
      node.removeEventListener("scroll", soon);
      ro?.disconnect();
      mo?.disconnect();
    },
  };
}
