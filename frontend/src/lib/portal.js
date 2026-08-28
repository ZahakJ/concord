// portal — move a node to <body> for as long as it is mounted.
//
// `position: fixed` is not enough on its own, and the four cosmetic studios are
// where that stopped being a footnote. All four are rendered from inside the
// profile dialog, which is a scrollable box that plays a slide-in animation, so
// a fixed child of it has two ways to lose the viewport: any ancestor with a
// transform (or a running animation that implies one) becomes the containing
// block, and WebKit resolves a fixed descendant of an `overflow: auto` element
// against the SCROLLER rather than the screen. Either way the studio stops
// being a full-screen surface and starts being a panel clipped by the dialog it
// came from — which is what "the banner editor bleeds through the settings
// popup" looks like on the desktop build, and only on the desktop build.
//
// Out at <body> nothing can clip or re-anchor it. InfoDot has done this since it
// was written, for the same reason and with the same three lines; this is that
// code, named, so the next full-screen surface does not have to rediscover it.
export function portal(node, target) {
  const host = target || document.body;
  host.appendChild(node);
  return {
    destroy() {
      node.remove();
    },
  };
}
