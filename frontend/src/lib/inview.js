// motionInView — hold a tile's animations still while it is off screen.
//
// The cosmetic studios are grids of LIVE previews: 108 decoration and ring
// tiles, 102 effect scenes and particle fields, 43 banner scenes, 26 card
// frames, 51 theme packs. Every one of them is the real component, wearing the
// real art, because a picker that shows a still of a moving thing is a picker
// that lies about what you are choosing. The cost is that opening one starts
// every animation in the catalogue at once and keeps them all running: measured
// in the decoration studio, 692 CSS animations in the `running` state, of which
// a screenful is maybe forty. Chromium absorbs it; the desktop build's WebKit
// does not, and "the studios scroll like treacle" is what that feels like.
//
// `content-visibility: auto` was already on these tiles and is not the same
// promise — it skips LAYOUT and PAINT for skipped content, but an animation on
// a skipped subtree still ticks, still invalidates, and still keeps the
// compositor awake. Pausing is the part that was missing.
//
// One observer for the whole app rather than one per tile: a hundred observers
// watching one viewport is a hundred callbacks per scroll. `root: null` is
// correct even though the tiles live inside a dialog's own scroller — the
// intersection rectangle is clipped by every scrolling ancestor on the way up,
// so a tile scrolled out of the panel counts as out of view without this
// having to know which element the panel scrolls in.
//
// The margin is generous on purpose. A tile that starts moving only once it is
// fully on screen announces itself with a pop; 200px is roughly one row of
// tiles, so by the time one is readable it has been running for a moment.
const MARGIN = "200px";
const CLS = "motion-idle";

let io = null;
function observer() {
  if (io || typeof IntersectionObserver === "undefined") return io;
  io = new IntersectionObserver(
    (entries) => {
      for (const e of entries) e.target.classList.toggle(CLS, !e.isIntersecting);
    },
    { rootMargin: MARGIN, threshold: 0 },
  );
  return io;
}

// Svelte action. `enabled` is a hatch for a caller that wants the plain
// behaviour back (a single hero preview, where there is nothing to save).
export function motionInView(node, enabled = true) {
  const o = enabled && observer();
  if (!o) return {};
  // Start held. The observer's first callback arrives a frame later, and
  // starting every tile running and stopping most of them again is the exact
  // burst this exists to avoid.
  node.classList.add(CLS);
  o.observe(node);
  return {
    destroy() {
      o.unobserve(node);
      node.classList.remove(CLS);
    },
  };
}
