// tilefit.js — how big a call tile is.
//
// The stage used to be `--cols` columns of `1fr` with rows sharing whatever
// height was left, and a tile filled its whole cell. That makes a tile's SHAPE
// a function of the panel's shape, which is a function of the window: measured
// at 1440x900, one participant got 620x496 and three got 426x540 — taller than
// wide. No camera produces that, so a video would have been letterboxed into a
// stripe and an avatar sat marooned in a slab. On a 390px phone two people were
// 173x653 each: a pair of vertical bars.
//
// A tile is 16:10 instead — the largest one that fits, with the leftover space
// as margin rather than as stretch. Fitting a fixed-aspect box inside a box of
// unknown aspect needs both dimensions at once, and CSS cannot express that
// against an element's own size, which is why this is arithmetic and not a
// stylesheet.
//
// Lives here rather than in VoicePanel so the counts a two-laptop test rig
// cannot staff — five people, nine people — are still checked (tilefit.test.mjs).

export const TILE_AR = 16 / 10;
export const TILE_MAX_W = 620; // past this a tile gains distance, not detail
export const TILE_MIN_W = 96;

// The tallest a tile is allowed to become, and the one case it is allowed to.
//
// Locking the aspect fixed the elongated slab and sent the bill somewhere else:
// two 16:10 tiles side by side in a 16:10 stage can never cover more than half
// of it, and stacking them gives the same answer. Measured at 1440x900 — an
// 864x540 stage, two 426x266 tiles, 49% used, ~180px of black above the pair
// and ~180px below. A two-person call is the commonest call there is and it was
// the emptiest screen in the app.
//
// So for exactly two people the shape is a RANGE rather than a lock: as wide as
// 16:10, as tall as square, whichever fills the box. Three and up are unchanged
// — with three or more the grid is what decides the shape and a square tile
// starts cropping cameras for no gain. One is unchanged too: a lone tile is
// held at TILE_MAX_W on purpose (a tile, not a wall), and its margin is that
// cap rather than the aspect.
export const TILE_AR_MIN = 1;
export const relaxedAr = (n) => (n === 2 ? TILE_AR_MIN : null);

// fitTiles: the stage's inner box and who is in it → the tile's size, and the
// width one row of them needs.
//
// `rowW` matters because the rows are laid out by wrapping rather than by grid
// tracks — which is what centres a short last row under a full one, so five
// people read as 3 over 2 with both rows centred instead of 3 over 2 shoved
// left. Wrapping decides where a row ends by width, so the row is pinned to
// exactly the width `cols` tiles need; otherwise a tile shortened by the height
// fit would let a fourth one onto a row sized for three and the height sum
// this function just solved would be wrong.
export function fitTiles({ w, h, n, cols, gap = 12, ar = TILE_AR, arMin = null, maxW = TILE_MAX_W, minW = TILE_MIN_W }) {
  const count = Math.max(1, n | 0);
  const c = Math.max(1, Math.min(cols | 0 || 1, count));
  const rows = Math.ceil(count / c);
  const availW = w - gap * (c - 1);
  const availH = h - gap * (rows - 1);
  if (!(availW > 0) || !(availH > 0)) return null;
  // `ar` is the widest shape allowed and `arMin` the tallest. With arMin unset
  // they are the same number and this reduces exactly to the old lock: take the
  // width you can have, take the height that shape wants, and if the box is too
  // short to hold it, give the width back until it is.
  const lo = arMin || ar;
  let tileW = Math.min(availW / c, maxW);
  let tileH = Math.min(availH / rows, tileW / lo);
  if (tileH < tileW / ar) tileW = tileH * ar;
  tileW = Math.max(minW, Math.floor(tileW));
  tileH = Math.max(minW / ar, Math.min(tileH, tileW / lo));
  return {
    tileW,
    tileH: Math.round(Math.max(tileH, tileW / ar)),
    rowW: Math.round(tileW * c + gap * (c - 1)),
    cols: c,
    rows,
  };
}

// columnsFor: the square-ish arrangement — the shape a meeting grid wants when
// the space it is in has no opinion. Capped at four across so a big call does
// not become a wall of postage stamps.
export const columnsFor = (n) => Math.min(4, Math.max(1, Math.ceil(Math.sqrt(Math.max(1, n)))));

// bestFit: how many columns, decided by which arrangement makes the biggest
// tile rather than by ceil(sqrt(n)) alone.
//
// The square rule is right for a room-shaped box and wrong for a tall thin one.
// Three people on a 390px phone: two columns gives 173x108 tiles with 200px of
// unused height above and below, while one column gives 335x209 — the same
// three faces, nearly four times the area. On a desktop the two rules agree
// (three people are still 2 over 1), so this only changes the cases the square
// rule was getting wrong.
//
// Ties go to the arrangement nearest the square one, so a box with room to
// spare keeps the conventional look instead of stacking everybody in a column.
export function bestFit({ w, h, n, gap = 12, maxCols = 4, ar = TILE_AR, arMin, maxW = TILE_MAX_W, minW = TILE_MIN_W }) {
  const count = Math.max(1, n | 0);
  const want = columnsFor(count);
  const lo = arMin === undefined ? relaxedAr(count) : arMin;
  let best = null;
  for (let c = 1; c <= Math.min(maxCols, count); c++) {
    const f = fitTiles({ w, h, n: count, cols: c, gap, ar, arMin: lo, maxW, minW });
    if (!f) continue;
    const better =
      !best ||
      f.tileW > best.tileW + 0.5 ||
      (Math.abs(f.tileW - best.tileW) <= 0.5 && Math.abs(c - want) < Math.abs(best.cols - want));
    if (better) best = f;
  }
  return best;
}
