// fittext.js — a one-line ellipsis that does not end on a space.
//
// CSS text-overflow cuts wherever the box runs out, and when that falls just
// after a word it keeps the space: the channel topic "the couch. kick your
// shoes off" read "kick your shoes …", a gap between the last word and the
// ellipsis that looks like a rendering fault. CSS has no way to trim it, so
// the cut is made here, at the longest prefix that fits, with its trailing
// whitespace dropped before the "…" goes on.

const ELLIPSIS = "…";

// cut is the candidate string for a prefix of k characters.
export function cut(text, k) {
  if (k >= text.length) return text;
  return text.slice(0, k).trimEnd() + ELLIPSIS;
}

// fitPrefix returns the longest cut(text, k) that `fits` accepts — the whole
// text when it fits, and a bare "…" when nothing does. `fits` is the only
// measurement, so this is testable without a layout engine.
export function fitPrefix(text, fits) {
  if (fits(text)) return text;
  let lo = 0;
  let hi = text.length - 1;
  while (lo < hi) {
    const mid = Math.ceil((lo + hi) / 2);
    if (fits(cut(text, mid))) lo = mid;
    else hi = mid - 1;
  }
  return cut(text, lo);
}

// fitText is the Svelte action. It replaces the node's text only when the
// text overflows, and takes the cut over from text-overflow. The width
// on offer is read with the WHOLE text in place — the node may be sized by
// its content, so once shortened it would report its own new width — and it
// is re-fitted when its container changes width.
export function fitText(node, text) {
  let full = text || "";
  let lastW = -1;
  // Fractional widths: scrollWidth rounds up and clientWidth rounds down, so
  // text that exactly fills its box would read as a pixel over and lose a
  // character it had room for. Ranges measure the real width.
  const range = document.createRange();
  const textW = () => {
    range.selectNodeContents(node);
    return range.getBoundingClientRect().width;
  };
  const boxW = () => node.getBoundingClientRect().width;
  const SLACK = 0.5;
  function fit() {
    node.textContent = full;
    const avail = boxW();
    if (!full || textW() <= avail + SLACK) return;
    node.textContent = fitPrefix(full, (s) => {
      node.textContent = s;
      return textW() <= avail + SLACK;
    });
  }
  // The cut is ours, so the browser's is switched off. A box that shrinks is
  // never quite free of its neighbours: shortening the text hands space back
  // to a flexible one (the header's search field takes it), and this box
  // settles a fraction of a pixel narrower than its new text — enough for
  // text-overflow to cut a second time, behind the fit, and draw "kic…".
  node.style.textOverflow = "clip";
  const host = node.parentElement;
  const ro =
    typeof ResizeObserver === "function" && host
      ? new ResizeObserver(() => {
          const w = host.clientWidth;
          if (w === lastW) return;
          lastW = w;
          fit();
        })
      : null;
  ro?.observe(host);
  fit();
  return {
    update(next) {
      full = next || "";
      fit();
    },
    destroy() {
      ro?.disconnect();
    },
  };
}
