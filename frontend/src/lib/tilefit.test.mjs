// Zero-dependency test for the call stage's tile fit (`npm test`).
//
// The property that matters is the one the old grid broke: a tile is a video
// frame, so it is wider than it is tall, whatever the shape of the panel it
// lands in. The stage boxes below are MEASURED from the running app — 620x496
// and 620x540 on a 1440x900 desktop, 358x608 and 358x653 on a 390x844 phone —
// so these are the real cases, at the participant counts a two-laptop rig
// cannot staff.
import { bestFit, columnsFor, fitTiles, TILE_AR, TILE_AR_MIN } from "./tilefit.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};
const eq = (a, b, msg) => assert(a === b, `${msg}\n  got ${a}\n  exp ${b}`);

// ---- the column rule -----------------------------------------------------
eq(columnsFor(1), 1, "one person, one column");
eq(columnsFor(2), 2, "two people side by side");
eq(columnsFor(3), 2, "three: two over one");
eq(columnsFor(4), 2, "four: a square");
eq(columnsFor(5), 3, "five: three over two");
eq(columnsFor(9), 3, "nine: a 3x3");
eq(columnsFor(20), 4, "big calls cap at four across");

// ---- the fit -------------------------------------------------------------
const GAP = 12;
const BOXES = [
  ["desktop 1440x900", 620, 540],
  ["phone 390x844", 358, 653],
  ["short window", 900, 220], // a laptop with the chat strip taking most of it
  ["tall narrow", 320, 900],
];

for (const [where, w, h] of BOXES) {
  for (const n of [1, 2, 3, 4, 5, 6, 9, 12]) {
    const cols = columnsFor(n);
    const f = fitTiles({ w, h, n, cols, gap: GAP });
    assert(f, `${where}, ${n}: a fit exists`);
    if (!f) continue;
    // Shape: 16:10, to the pixel the rounding allows.
    const ar = f.tileW / f.tileH;
    assert(
      Math.abs(ar - TILE_AR) < 0.02,
      `${where}, ${n}: tile is 16:10 — got ${f.tileW}x${f.tileH} (${ar.toFixed(2)})`,
    );
    assert(f.tileW > f.tileH, `${where}, ${n}: a tile is wider than it is tall`);
    // Fit: the rows must not need more room than there is. (The minimum tile
    // width can overflow a genuinely tiny box; that is the floor doing its job,
    // and the stage scrolls. Everything above it must fit.)
    const rows = Math.ceil(n / f.cols);
    const needH = f.tileH * rows + GAP * (rows - 1);
    const needW = f.rowW;
    if (f.tileW > 96) {
      assert(needH <= h + 1, `${where}, ${n}: ${rows} rows fit in ${h}px (need ${needH})`);
      assert(needW <= w + 1, `${where}, ${n}: a row fits in ${w}px (need ${needW})`);
    }
    // …and it must use the room it has: within a pixel of touching one edge.
    if (f.tileW > 96 && f.tileW < 620) {
      const slackH = h - needH;
      const slackW = w - needW;
      assert(
        slackH < f.tileH || slackW < f.tileW,
        `${where}, ${n}: tiles are as large as they can be (slack ${slackW}x${slackH})`,
      );
    }
  }
}

// ---- the specific shapes the owner saw ----------------------------------
{
  // One person, desktop: was 620x496 (portrait-ish slab).
  const f = fitTiles({ w: 620, h: 496, n: 1, cols: 1, gap: GAP });
  eq(`${f.tileW}x${f.tileH}`, "620x388", "a lone participant gets a 16:10 frame, not the whole panel");
}
{
  // Three, desktop: was 426x540 — taller than wide.
  const f = fitTiles({ w: 620, h: 540, n: 3, cols: 2, gap: GAP });
  assert(f.tileW > f.tileH, `three people: ${f.tileW}x${f.tileH} is landscape`);
}
{
  // Two, phone: was 173x653 — a pair of vertical bars.
  const f = fitTiles({ w: 358, h: 653, n: 2, cols: 2, gap: GAP });
  assert(f.tileW > f.tileH, `two on a phone: ${f.tileW}x${f.tileH} is landscape`);
}

// ---- choosing the arrangement -------------------------------------------
{
  // A desktop stage: the square rule and the fit agree, so nothing moves.
  for (const [n, cols] of [[1, 1], [2, 2], [3, 2], [4, 2], [5, 3], [9, 3]]) {
    const f = bestFit({ w: 864, h: 540, n, gap: GAP });
    eq(f.cols, cols, `desktop, ${n} people: ${cols} columns`);
  }
}
{
  // A phone: two columns of slivers is worse than one column of frames.
  const two = fitTiles({ w: 358, h: 653, n: 3, cols: 2, gap: GAP });
  const best = bestFit({ w: 358, h: 653, n: 3, gap: GAP });
  eq(best.cols, 1, "phone, three people: one column");
  assert(
    best.tileW > two.tileW * 1.7,
    `phone, three people: ${best.tileW}px beats the square rule's ${two.tileW}px`,
  );
}
{
  // …and it never picks an arrangement that is worse than the square one.
  for (const [w, h] of [[864, 540], [358, 653], [1200, 300], [300, 1200], [600, 600]]) {
    for (let n = 1; n <= 12; n++) {
      const sq = fitTiles({ w, h, n, cols: columnsFor(n), gap: GAP });
      const best = bestFit({ w, h, n, gap: GAP });
      assert(
        best && sq && best.tileW >= sq.tileW,
        `${w}x${h}, ${n}: chosen ${best?.cols} cols (${best?.tileW}px) is not worse than the square ${sq?.cols} (${sq?.tileW}px)`,
      );
    }
  }
}

// ---- two people, and the half-black stage --------------------------------
//
// Locking the aspect is what made the pair of 16:10 tiles cover 49% of a 16:10
// stage. For exactly two the shape is a range (TILE_AR down to TILE_AR_MIN),
// and the numbers below are the measured stage.
{
  const stage = { w: 864, h: 540 };
  const locked = fitTiles({ ...stage, n: 2, cols: 2, gap: GAP });
  const relaxed = bestFit({ ...stage, n: 2, gap: GAP });
  const fill = (f, n) => (n * f.tileW * f.tileH) / (stage.w * stage.h);
  assert(fill(locked, 2) < 0.5, `the lock covers half the stage (${(fill(locked, 2) * 100).toFixed(0)}%)`);
  assert(
    fill(relaxed, 2) > 0.7,
    `two people fill the stage (${(fill(relaxed, 2) * 100).toFixed(0)}% from ${relaxed.tileW}x${relaxed.tileH})`,
  );
  const ar = relaxed.tileW / relaxed.tileH;
  assert(ar >= TILE_AR_MIN - 0.01 && ar <= TILE_AR + 0.01, `two people stay inside the range (${ar.toFixed(2)})`);
  // The tiles still fit the box they were solved for.
  assert(relaxed.rowW <= stage.w + 1, `a row of two fits (${relaxed.rowW} in ${stage.w})`);
  assert(relaxed.tileH * relaxed.rows + GAP * (relaxed.rows - 1) <= stage.h + 1, "two rows fit");
}
{
  // Everyone else keeps the lock, so a relaxed pair cannot leak into a grid.
  for (const n of [1, 3, 4, 5, 9]) {
    const f = bestFit({ w: 864, h: 540, n, gap: GAP });
    const ar = f.tileW / f.tileH;
    assert(Math.abs(ar - TILE_AR) < 0.02, `${n} people keep 16:10 (${ar.toFixed(2)})`);
  }
}
{
  // A box too short for a square pair still gives landscape tiles, not a
  // silently-broken fit.
  const f = bestFit({ w: 900, h: 220, n: 2, gap: GAP });
  assert(f.tileW > f.tileH, `a short stage keeps two people landscape (${f.tileW}x${f.tileH})`);
  assert(f.tileH * f.rows <= 220 + 1, "…and still fits");
}

// ---- degenerate boxes ---------------------------------------------------
assert(fitTiles({ w: 0, h: 400, n: 2, cols: 2 }) === null, "no width, no fit");
assert(fitTiles({ w: 400, h: 0, n: 2, cols: 2 }) === null, "no height, no fit");

if (failures) {
  console.error(`${failures} failure(s)`);
  process.exit(1);
}
console.log("tilefit.test.mjs: ok");
