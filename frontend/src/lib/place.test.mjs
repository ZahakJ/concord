// Zero-dependency test for the floating-surface placement model (`npm test`).
//
// place() is the whole of "where does this menu go?", and the property that
// matters is not which side it picked — it is that the box it returns is
// INSIDE the viewport. Every case below asserts that, including the ones with
// no good answer (a surface bigger than the screen), because the bug this
// module exists for rendered menus at coordinates nobody could reach.
//
// The DOM-facing half (uiZoom/rectOf/pointOf) is exercised by the driven
// matrix in .dev/critics/zoom-matrix.mjs — it needs a real zoomed document,
// which is exactly the thing a unit test cannot fake convincingly.
import { clampBox, place } from "./place.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};
const eq = (a, b, msg) =>
  assert(
    JSON.stringify(a) === JSON.stringify(b),
    `${msg}\n  got ${JSON.stringify(a)}\n  exp ${JSON.stringify(b)}`,
  );

const VP = { w: 1000, h: 800 };
const inside = (r, w, h, pad = 8, vp = VP) =>
  r.left >= pad - 0.001 &&
  r.top >= pad - 0.001 &&
  r.left + w <= vp.w - pad + 0.001 &&
  r.top + h <= vp.h - pad + 0.001;

// ---- the preferred side is taken when it fits --------------------------
{
  const p = place({ anchor: { x: 400, y: 200, w: 40, h: 20 }, w: 200, h: 100, vp: VP });
  eq(p.side, "bottom", "opens below when there is room below");
  eq(p.top, 226, "sits one gap under the anchor");
  eq(p.left, 320, "centred on the anchor");
}

// ---- …and flipped when it does not -------------------------------------
{
  const p = place({ anchor: { x: 400, y: 740, w: 40, h: 20 }, w: 200, h: 300, vp: VP });
  eq(p.side, "top", "flips above when the bottom has no room");
  assert(inside(p, 200, 300), "flipped box is on screen");
}

// ---- a cursor menu at the bottom-right corner --------------------------
// The reported bug: right-clicking the last message put the menu below the
// fold, where it rendered as nothing at all.
{
  const p = place({
    anchor: { x: 995, y: 795, w: 0, h: 0 },
    w: 220,
    h: 400,
    side: "bottom",
    align: "start",
    gap: 0,
    vp: VP,
  });
  assert(inside(p, 220, 400), `corner menu clamped on screen: ${JSON.stringify(p)}`);
  // 995 - 220 = 775, then held 8px off the edge by the clamp.
  eq(p.left, 772, "hangs leftward from the cursor rather than sliding away from it");
}

// ---- a cursor menu at the top-left corner ------------------------------
{
  const p = place({
    anchor: { x: 2, y: 2, w: 0, h: 0 },
    w: 220,
    h: 400,
    side: "bottom",
    align: "start",
    gap: 0,
    vp: VP,
  });
  assert(inside(p, 220, 400), `top-left menu clamped on screen: ${JSON.stringify(p)}`);
}

// ---- a surface taller than the viewport keeps its TOP edge -------------
// It scrolls inside itself; what it must never do is push its own header off
// the top of the screen, which is what a one-sided clamp does.
{
  const p = place({ anchor: { x: 400, y: 400, w: 40, h: 20 }, w: 200, h: 2000, vp: VP });
  eq(p.top, 8, "a too-tall card sits at the top margin");
  eq(p.left, 320, "…and is still centred horizontally");
}

// ---- horizontal placement flips too ------------------------------------
{
  const p = place({ anchor: { x: 940, y: 400, w: 30, h: 30 }, w: 200, h: 60, side: "right", vp: VP });
  eq(p.side, "left", "a right-side tooltip flips left at the edge");
  assert(inside(p, 200, 60), "flipped tooltip is on screen");
}
{
  const p = place({ anchor: { x: 10, y: 400, w: 30, h: 30 }, w: 200, h: 60, side: "left", vp: VP });
  eq(p.side, "right", "a left-side tooltip flips right at the edge");
}

// ---- `room` reports what the chosen side actually has -------------------
{
  const p = place({ anchor: { x: 400, y: 100, w: 40, h: 20 }, w: 200, h: 50, gap: 6, vp: VP });
  eq(p.room, 800 - 120 - 8 - 6, "room below = viewport - anchor bottom - pad - gap");
}

// ---- clampBox on its own ------------------------------------------------
{
  eq(clampBox({ left: -50, top: -50, w: 100, h: 100 }, { vp: VP }), { left: 8, top: 8 }, "clamps the near edges");
  eq(
    clampBox({ left: 5000, top: 5000, w: 100, h: 100 }, { vp: VP }),
    { left: 892, top: 692 },
    "clamps the far edges",
  );
  eq(
    clampBox({ left: 0, top: 0, w: 5000, h: 5000 }, { vp: VP }),
    { left: 8, top: 8 },
    "an oversized box keeps its near edge rather than being pushed off",
  );
}

// ---- a sweep: wherever the cursor is, the menu is reachable -------------
{
  let bad = 0;
  for (let x = 0; x <= VP.w; x += 17) {
    for (let y = 0; y <= VP.h; y += 19) {
      const p = place({
        anchor: { x, y, w: 0, h: 0 },
        w: 260,
        h: 520,
        side: "bottom",
        align: "start",
        gap: 0,
        vp: VP,
      });
      if (!inside(p, 260, 520)) bad++;
    }
  }
  eq(bad, 0, "every cursor position produces an on-screen menu");
}

if (failures) {
  console.error(`${failures} failure(s)`);
  process.exit(1);
}
console.log("place.test.mjs: ok");
