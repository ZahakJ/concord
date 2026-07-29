// Tests for the meme editor's layout maths. A fake measurer stands in for the
// canvas: every character is exactly 10px wide at size 100, scaling linearly.
// That makes expected widths countable by hand, which is the whole point —
// wrapping bugs are invisible against a real font's fractional metrics.
import {
  wrapLines,
  fitCaption,
  captionBox,
  captionAt,
  newCaption,
  resolve,
  unrotate,
  handlePos,
  scaleRotate,
  snapTo,
  rebaseTopBar,
  topBarCentre,
  fitWidthAt,
  renderSize,
  searchTemplates,
  newLayer,
  layerBox,
  layerAt,
  aabbHalf,
  clampLayer,
  moveLayer,
  slotBox,
  slotFilled,
  nextSlot,
  fitToSlot,
  drawMeme,
  newSession,
  usedAssets,
  LAYER_SCALE,
  LAYER_KEEP,
  STYLES,
  FONTS,
  MAX_RENDER,
} from "./meme.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};
const eq = (a, b, msg) =>
  assert(JSON.stringify(a) === JSON.stringify(b), `${msg}\n  got ${JSON.stringify(a)}\n  exp ${JSON.stringify(b)}`);
const near = (a, b, tol, msg) => assert(Math.abs(a - b) <= tol, `${msg}\n  got ${a}\n  exp ~${b}`);

const measure = (w) => (t) => t.length * w;
const measureAt = (t, size) => t.length * (size / 10);

// ---- wrapping ----
eq(wrapLines(measure(10), "one two", 100), ["one two"], "text that fits stays on one line");
eq(wrapLines(measure(10), "one two three", 100), ["one two", "three"], "it breaks at the last word that fits");
eq(wrapLines(measure(10), "a\nb", 100), ["a", "b"], "explicit newlines are kept");
eq(wrapLines(measure(10), "a\n\nb", 100), ["a", "", "b"], "a blank line survives as a blank line");
eq(wrapLines(measure(10), "", 100), [""], "empty text is one empty line, not zero");
// The overflow case: one word wider than the box. Letting it run off the edge
// would silently crop the joke, so it hard-breaks instead.
eq(
  wrapLines(measure(10), "aaaaaaaaaaaaaaa", 100),
  ["aaaaaaaaaa", "aaaaa"],
  "a single over-long word is broken rather than allowed to overflow",
);
eq(
  wrapLines(measure(10), "hi aaaaaaaaaaaaaaa", 100),
  ["hi", "aaaaaaaaaa", "aaaaa"],
  "an over-long word breaks even when it follows a normal one",
);
// Whitespace shouldn't create phantom words.
eq(wrapLines(measure(10), "  one   two  ", 100), ["one two"], "runs of spaces collapse");

// ---- auto-shrink ----
{
  // 40 chars at size 100 measures 400px wide in the fake font. In a 100px box
  // that's 4 lines, already inside the limit, so nothing shrinks.
  const cap = newCaption("a".repeat(40), { w: 1, size: 1 });
  const { size } = fitCaption(measureAt, cap, 100, 100, 6);
  assert(size === 100, `text that already fits is not shrunk (got ${size})`);
}
{
  // Far too much text for 6 lines at full size: it must come back smaller.
  const cap = newCaption("a".repeat(400), { w: 1, size: 1 });
  const { lines, size } = fitCaption(measureAt, cap, 100, 100, 6);
  assert(size < 100, `overlong text shrinks (got ${size})`);
  assert(lines.length <= 6, `and lands within the line budget (got ${lines.length})`);
}
{
  // The escape hatch: shrinking stops at 10px rather than looping to zero.
  const cap = newCaption("a".repeat(100000), { w: 1, size: 1 });
  const { size } = fitCaption(measureAt, cap, 100, 100, 2);
  assert(size >= 10 && Number.isFinite(size), `shrink bottoms out instead of vanishing (got ${size})`);
}

// ---- box geometry ----
{
  // size 0.1 of a 1000px-tall image = 100px type, where the fake font is 10px
  // per character, so 10 characters measure 100px. Centred at x=0.5 of a
  // 2000px-wide image the box therefore starts at 1000 - 50 = 950.
  const cap = newCaption("abcdefghij", { w: 1, size: 0.1, x: 0.5, y: 0.5 });
  const b = captionBox(measureAt, cap, 2000, 1000);
  assert(Math.abs(b.w - 100) < 1, `box width follows the measured text (got ${b.w})`);
  assert(Math.abs(b.x - 950) < 1, `box is centred on cap.x (got ${b.x})`);
  assert(b.cx === 1000 && b.cy === 500, `centre is reported in pixels (got ${b.cx},${b.cy})`);
  assert(b.h > 0 && b.lineHeight > b.size, "line height leaves room between lines");
}

// ---- per-caption overrides ----
{
  // The look supplies the defaults; anything set on the caption wins. The two
  // that must survive are the falsy ones — a zero outline and caps-off are real
  // choices, and `||` defaulting would silently undo both.
  const base = resolve(newCaption("x"));
  assert(base.uppercase === true && base.stroke === STYLES.impact.stroke, "an untouched caption resolves to its look");
  const off = resolve(newCaption("x", { caps: false, stroke: 0 }));
  assert(off.uppercase === false, "caps:false is honoured, not treated as unset");
  assert(off.stroke === 0, "stroke:0 is honoured, not treated as unset");
  const fonted = resolve(newCaption("x", { font: "comic" }));
  assert(fonted.family === FONTS.comic.family, "a per-caption font replaces the look's font");
  const coloured = resolve(newCaption("x", { color: "#ff0000" }));
  assert(coloured.color === "#ff0000", "a per-caption colour replaces the look's colour");
}
{
  // A different font must actually reach the measurer, or a caption in a wide
  // face gets a selection box sized for a narrow one.
  const seen = [];
  const spy = (t, size, style) => {
    seen.push(style.family);
    return t.length * (size / 10);
  };
  captionBox(spy, newCaption("hi", { font: "mono" }), 100, 100);
  assert(
    seen.length > 0 && seen.every((f) => f === FONTS.mono.family),
    `measurement uses the caption's own face (saw ${JSON.stringify([...new Set(seen)])})`,
  );
}

// ---- rotation ----
{
  // A quarter turn maps the point straight above the centre onto the point
  // straight to its left, in the caption's own frame.
  const p = unrotate(100, 0, 100, 100, 90);
  near(p.x, -100, 1e-9, "unrotate takes a point into the rotated frame (x)");
  near(p.y, 0, 1e-9, "unrotate takes a point into the rotated frame (y)");
  const same = unrotate(120, 130, 100, 100, 0);
  eq([same.x, same.y], [20, 30], "no rotation is a plain translation");
}

// ---- hit testing ----
{
  const top = newCaption("top", { y: 0.1, size: 0.1 });
  const bottom = newCaption("bottom", { y: 0.9, size: 0.1 });
  const caps = [top, bottom];
  assert(captionAt(measureAt, caps, 500, 100, 1000, 1000)?.id === top.id, "a point on the top caption finds it");
  assert(captionAt(measureAt, caps, 500, 900, 1000, 1000)?.id === bottom.id, "and on the bottom one finds that");
  assert(captionAt(measureAt, caps, 20, 500, 1000, 1000) === null, "empty space finds nothing");
}
{
  // Overlapping captions: the last one is drawn on top, so it must also be the
  // one you grab. Searching forwards would always return the buried one.
  const under = newCaption("under", { x: 0.5, y: 0.5, size: 0.1 });
  const over = newCaption("over", { x: 0.5, y: 0.5, size: 0.1 });
  const hit = captionAt(measureAt, [under, over], 500, 500, 1000, 1000);
  assert(hit?.id === over.id, "the topmost caption wins a hit test");
}
{
  // A quarter turn swaps which way a caption is long. This one measures 150px
  // wide and 56px tall, so 60px out sideways is inside it and 60px up is not —
  // and rotating it must swap exactly that. Hit-testing the unrotated box gets
  // both cases backwards.
  const text = "a".repeat(30);
  const flat = newCaption(text, { x: 0.5, y: 0.5, size: 0.05, w: 1 });
  const turned = newCaption(text, { x: 0.5, y: 0.5, size: 0.05, w: 1, rot: 90 });
  assert(captionAt(measureAt, [flat], 560, 500, 1000, 1000)?.id === flat.id, "unrotated: 60px out sideways hits");
  assert(captionAt(measureAt, [flat], 500, 440, 1000, 1000) === null, "unrotated: 60px up misses");
  assert(captionAt(measureAt, [turned], 500, 440, 1000, 1000)?.id === turned.id, "rotated 90°: 60px up now hits");
  assert(captionAt(measureAt, [turned], 560, 500, 1000, 1000) === null, "rotated 90°: 60px sideways now misses");
}

// ---- scale/rotate handle ----
{
  const cap = newCaption("abcdefghij", { x: 0.5, y: 0.5, size: 0.1, w: 1 });
  const b = captionBox(measureAt, cap, 1000, 1000);
  const h = handlePos(b);
  assert(h.x > b.cx && h.y > b.cy, "the grip sits past the bottom-right corner");
  // Rotating the caption carries the grip round with it rather than leaving it
  // pinned to the corner of an axis-aligned box.
  const hr = handlePos({ ...b, rot: 180 });
  near(hr.x, b.cx - (h.x - b.cx), 1e-6, "a half turn mirrors the grip in x");
  near(hr.y, b.cy - (h.y - b.cy), 1e-6, "a half turn mirrors the grip in y");
}
{
  const start = { vx: 100, vy: 0, size: 0.1, rot: 0 };
  // Twice as far from the centre = twice the type size.
  const bigger = scaleRotate(start, 300, 100, 100, 100);
  near(bigger.size, 0.2, 1e-9, "dragging the grip out doubles the size");
  near(bigger.rot, 0, 1e-9, "and straight out doesn't rotate it");
  // Straight down from the centre is a quarter turn.
  const turned = scaleRotate(start, 100, 200, 100, 100);
  near(turned.rot, 90, 1e-9, "swinging the grip a quarter turn rotates 90°");
  // Wrapping: 350° of extra turn should read as -10, not 350.
  near(scaleRotate({ ...start, rot: 350 }, 200, 100, 100, 100).rot, -10, 1e-9, "the angle wraps into (-180,180]");
  // Clamps, so a flick can't produce a caption bigger than the image or one
  // shrunk to nothing.
  assert(scaleRotate(start, 100000, 100, 100, 100).size <= 0.4, "size is clamped at the top");
  assert(scaleRotate(start, 100.001, 100, 100, 100).size >= 0.02, "and at the bottom");
  // Degenerate grab: a zero-length starting vector must not divide by zero.
  const degenerate = scaleRotate({ vx: 0, vy: 0, size: 0.1, rot: 12 }, 400, 400, 100, 100);
  eq([degenerate.size, degenerate.rot], [0.1, 12], "a zero-length grab leaves the caption alone");
}

// ---- drag snapping ----
{
  eq(snapTo(0.51, [0.5], 0.02), { v: 0.5, hit: 0.5 }, "a near miss snaps onto the guide");
  eq(snapTo(0.56, [0.5], 0.02), { v: 0.56, hit: null }, "a clear miss is left alone");
  // Ties and multiple guides: the closest one wins.
  eq(snapTo(0.44, [0.5, 0.4], 0.06).v, 0.4, "the nearest of several guides wins");
  eq(snapTo(0.3, [], 0.02), { v: 0.3, hit: null }, "no guides means no snapping");
}

// ---- top bar rebasing ----
{
  // Turning on a bar 0.2 image-heights tall must leave a caption sitting at the
  // very top of the picture still sitting at the very top of the picture.
  near(rebaseTopBar(0, 0, 0.2), 0.2 / 1.2, 1e-9, "the top of the image stays the top of the image");
  near(rebaseTopBar(1, 0, 0.2), 1, 1e-9, "and the bottom stays the bottom");
  near(rebaseTopBar(0.5, 0, 0.2), (0.5 + 0.2) / 1.2, 1e-9, "the middle shifts down by exactly the bar");
  // Turning it back off is the inverse.
  near(rebaseTopBar(rebaseTopBar(0.37, 0, 0.2), 0.2, 0), 0.37, 1e-9, "off then on again is a round trip");
  near(rebaseTopBar(0.42, 0.3, 0.3), 0.42, 1e-9, "no change means no movement");
  near(topBarCentre(0.2), 0.1 / 1.2, 1e-9, "the bar's centre is half its height up the canvas");
  assert(topBarCentre(0) === 0, "no bar has no centre to place anything in");
}

// ---- width of a caption dropped off-centre ----
{
  near(fitWidthAt(0.5), 0.92, 1e-9, "dead centre gets the full default width");
  near(fitWidthAt(0.3), 0.6, 1e-9, "a third of the way in, the box stops at the near edge");
  near(fitWidthAt(0.8), 0.4, 1e-9, "and mirrors on the other side");
  near(fitWidthAt(0.5, 0.5), 0.5, 1e-9, "the cap is still a cap");
  // Right on the edge the box would be zero wide, which wraps every word onto
  // its own line forever; a floor keeps it usable.
  assert(fitWidthAt(0) >= 0.1 && fitWidthAt(1) >= 0.1, "a caption dropped on the edge still has a usable box");
}

// ---- render size ----
{
  const small = renderSize(400, 300);
  eq([small.W, small.H, small.topBar], [400, 300, 0], "an image under the cap is sent at its own size");
  const big = renderSize(4000, 2000);
  assert(Math.max(big.W, big.H) === MAX_RENDER, `the long edge is capped at ${MAX_RENDER} (got ${big.W}x${big.H})`);
  near(big.W / big.H, 2, 0.01, "and the aspect ratio survives the cap");
  const barred = renderSize(1000, 1000, 0.2);
  eq([barred.W, barred.topBar, barred.H], [1000, 200, 1200], "the bar grows the canvas instead of covering the image");
}

// ---- template search ----
{
  const list = [
    { file: "a", label: "Drake", tags: ["yes", "no", "prefer"] },
    { file: "b", label: "This is fine", tags: ["dog", "fire"] },
    { file: "c", label: "Surprised Pikachu" },
  ];
  eq(searchTemplates(list, "").length, 3, "an empty query matches everything");
  eq(searchTemplates(list, "   ").length, 3, "and so does whitespace");
  eq(
    searchTemplates(list, "pika").map((t) => t.file),
    ["c"],
    "a partial label matches",
  );
  eq(
    searchTemplates(list, "FIRE").map((t) => t.file),
    ["b"],
    "keywords match, case-insensitively",
  );
  eq(
    searchTemplates(list, "yes no").map((t) => t.file),
    ["a"],
    "every term must match, not just one",
  );
  eq(searchTemplates(list, "yes fire").length, 0, "terms from different templates match nothing");
}

// ---- image layers: geometry ----
{
  // A 200x100 picture at w=0.5 on a 1000x800 canvas is 500px wide, and its
  // height must come from the picture's own aspect — 250px — not from anything
  // the caller passed. That is what "aspect preserved" means here.
  const lay = newLayer("a1", 200, 100, { x: 0.5, y: 0.5, w: 0.5 });
  const b = layerBox(lay, 1000, 800);
  near(b.w, 500, 1e-9, "layer width is a fraction of the CANVAS width");
  near(b.h, 250, 1e-9, "and its height falls out of the picture's own aspect");
  near(b.cx, 500, 1e-9, "centre is in pixels (x)");
  near(b.cy, 400, 1e-9, "centre is in pixels (y)");
  // Scaling is one number, so the ratio cannot drift however far it is dragged.
  for (const w of [0.04, 0.37, 1.5, LAYER_SCALE.max]) {
    const s = layerBox({ ...lay, w }, 1000, 800);
    near(s.w / s.h, 2, 1e-9, `aspect survives scaling to w=${w}`);
  }
  // A degenerate picture (a decode that reported nothing) must not produce NaN
  // geometry that silently poisons every later hit test.
  const zero = layerBox(newLayer("a2", 0, 0, { w: 0.5 }), 1000, 800);
  assert(Number.isFinite(zero.w) && Number.isFinite(zero.h) && zero.h > 0, "a zero-sized picture still boxes finitely");
}

// ---- image layers: hit testing ----
{
  const lay = newLayer("a1", 100, 100, { x: 0.5, y: 0.5, w: 0.2 }); // 200x200 on 1000x1000
  assert(layerAt([lay], 500, 500, 1000, 1000)?.id === lay.id, "the centre of a layer hits it");
  assert(layerAt([lay], 590, 590, 1000, 1000)?.id === lay.id, "and so does a point just inside a corner");
  assert(layerAt([lay], 620, 500, 1000, 1000) === null, "a point outside misses");
  assert(layerAt([], 500, 500, 1000, 1000) === null, "no layers means no hit");
}
{
  // The rotation case, which is the one a second hand-rolled hit test always
  // gets wrong. A 400x100 layer turned a quarter turn is tall, not wide: 150px
  // above the centre is now inside it and 150px to the side is now outside.
  const flat = newLayer("a1", 400, 100, { x: 0.5, y: 0.5, w: 0.4 }); // 400x100 px
  const turned = { ...flat, rot: 90 };
  assert(layerAt([flat], 650, 500, 1000, 1000)?.id === flat.id, "unrotated: 150px sideways is inside");
  assert(layerAt([flat], 500, 650, 1000, 1000) === null, "unrotated: 150px up/down is outside");
  assert(layerAt([turned], 500, 650, 1000, 1000)?.id === turned.id, "rotated 90°: 150px down is now inside");
  assert(layerAt([turned], 650, 500, 1000, 1000) === null, "rotated 90°: 150px sideways is now outside");
  // 45° is where an axis-aligned test is most obviously wrong: the corner of
  // the unrotated box is well outside the turned one.
  const tilt = { ...flat, rot: 45 };
  assert(layerAt([tilt], 690, 540, 1000, 1000) === null, "rotated 45°: the old corner is no longer inside");
  assert(layerAt([tilt], 570, 570, 1000, 1000)?.id === tilt.id, "rotated 45°: the new diagonal is");
}
{
  // Topmost wins, and it must be the LAST in the array, because that is the one
  // drawLayers paints over the others.
  const under = newLayer("a1", 100, 100, { x: 0.5, y: 0.5, w: 0.5 });
  const over = newLayer("a2", 100, 100, { x: 0.5, y: 0.5, w: 0.5 });
  assert(layerAt([under, over], 500, 500, 1000, 1000)?.id === over.id, "the topmost layer wins a hit test");
  assert(layerAt([over, under], 500, 500, 1000, 1000)?.id === under.id, "and swapping them swaps the winner");
}

// ---- image layers: z-order ----
{
  const a = newLayer("a", 1, 1);
  const b = newLayer("b", 1, 1);
  const c = newLayer("c", 1, 1);
  const ids = (l) => l.map((x) => x.asset).join("");
  eq(ids(moveLayer([a, b, c], b.id, 1)), "acb", "forward swaps with the one above");
  eq(ids(moveLayer([a, b, c], b.id, -1)), "bac", "back swaps with the one below");
  eq(ids(moveLayer([a, b, c], c.id, 5)), "abc", "past the top is a no-op, not a wrap");
  eq(ids(moveLayer([a, b, c], a.id, -5)), "abc", "and past the bottom likewise");
  eq(ids(moveLayer([a, b, c], "nope", 1)), "abc", "an unknown id changes nothing");
}
{
  // The identity above is the bit that matters for reactivity: an edit that
  // moved nothing must not look like an edit.
  const list = [newLayer("a", 1, 1), newLayer("b", 1, 1)];
  assert(moveLayer(list, list[1].id, 1) === list, "moving the top layer up returns the very same array");
  assert(moveLayer(list, list[0].id, 1) !== list, "a real move returns a new one");
}

// ---- image layers: clamping ----
{
  near(aabbHalf(100, 100, 0).x, 50, 1e-9, "an unrotated square's footprint is itself");
  near(aabbHalf(100, 100, 45).x, (100 * Math.SQRT2) / 2 / 1, 1e-6, "turned 45° a square's footprint grows by √2");
  near(aabbHalf(400, 100, 90).x, 50, 1e-9, "a quarter turn swaps the footprint's axes (x)");
  near(aabbHalf(400, 100, 90).y, 200, 1e-9, "a quarter turn swaps the footprint's axes (y)");
}
{
  // Flung far off the canvas, a layer must come back with a grabbable sliver
  // still on it — otherwise it cannot be selected, moved or deleted again.
  const lay = newLayer("a1", 100, 100, { w: 0.2 }); // 200x200 on 1000x1000
  const far = clampLayer({ ...lay, x: 99, y: -99 }, 1000, 1000);
  const b = layerBox({ ...lay, ...far }, 1000, 1000);
  const half = aabbHalf(b.w, b.h, 0);
  const onX = Math.min(1000, b.cx + half.x) - Math.max(0, b.cx - half.x);
  const onY = Math.min(1000, b.cy + half.y) - Math.max(0, b.cy - half.y);
  near(onX, 1000 * LAYER_KEEP, 1e-6, "a layer dragged off the right keeps a sliver on the canvas");
  near(onY, 1000 * LAYER_KEEP, 1e-6, "and one dragged off the top keeps one too");
  // A layer that is already comfortably inside is not moved at all: clamping
  // that nudges things it shouldn't feels like the drag fighting back.
  const inside = clampLayer({ ...lay, x: 0.42, y: 0.66 }, 1000, 1000);
  eq([inside.x, inside.y], [0.42, 0.66], "a layer inside the canvas is left exactly where it is");
}
{
  // A layer bigger than the canvas is the case the naive "pin the centre to
  // 0..1" rule gets wrong in the other direction: it forbids sliding a big
  // picture across to show its far side.
  const big = newLayer("a1", 100, 100, { w: 2, x: 0.5, y: 0.5 }); // 2000px on a 1000px canvas
  const pushed = clampLayer({ ...big, x: 1.6 }, 1000, 1000);
  assert(pushed.x > 1, `a layer larger than the canvas may sit past the edge (got ${pushed.x})`);
  const way = clampLayer({ ...big, x: 50 }, 1000, 1000);
  const bb = layerBox({ ...big, ...way }, 1000, 1000);
  near(1000 - (bb.cx - aabbHalf(bb.w, bb.h, 0).x), 1000 * LAYER_KEEP, 1e-6, "but never past the last sliver");
  // And a sticker smaller than the margin isn't pinned to the middle.
  const tiny = newLayer("a1", 100, 100, { w: 0.02 });
  const edge = clampLayer({ ...tiny, x: 0.999, y: 0.5 }, 1000, 1000);
  near(edge.x, 0.999, 1e-9, "a layer smaller than the keep-margin can still sit on the edge");
}

// ---- template slots ----
{
  const slot = { x: 0.25, y: 0.2, w: 0.4, h: 0.3 };
  const b = slotBox(slot, 1000, 1000);
  eq([b.cx, b.cy, b.w, b.h, b.rot], [250, 200, 400, 300, 0], "a slot boxes into pixels");
  const lay = newLayer("a1", 100, 100, { x: 0.25, y: 0.2, w: 0.1 });
  assert(slotFilled(slot, [lay], 1000, 1000), "a layer centred in the slot fills it");
  assert(!slotFilled(slot, [{ ...lay, x: 0.9 }], 1000, 1000), "and dragging it out empties the slot again");
  assert(!slotFilled(slot, [], 1000, 1000), "no layers means no slot is filled");
  // Rotated slots use the same unrotate the captions do, so a point that is
  // inside the tilted panel counts even though it is outside the flat one.
  const tilted = { ...slot, rot: 90 };
  assert(slotFilled(tilted, [{ ...lay, x: 0.25, y: 0.36 }], 1000, 1000), "a rotated slot tests in its own frame");
  assert(!slotFilled(slot, [{ ...lay, x: 0.25, y: 0.36 }], 1000, 1000), "where the unrotated one would miss");
}
{
  // Paste twice, land in both panels: the behaviour the whole feature exists
  // for. The first paste takes slot one, and because it now fills it, the
  // second is offered slot two rather than stacking on top of the first.
  const slots = [
    { x: 0.25, y: 0.2, w: 0.4, h: 0.3 },
    { x: 0.75, y: 0.2, w: 0.4, h: 0.3 },
  ];
  const first = nextSlot(slots, [], 1000, 1000);
  eq(first.x, 0.25, "an empty template offers its first slot");
  const l1 = newLayer("a1", 100, 100, { ...fitToSlot(first, 100, 100, 1000, 1000) });
  const second = nextSlot(slots, [l1], 1000, 1000);
  eq(second.x, 0.75, "with the first taken, the next paste is offered the second");
  const l2 = newLayer("a2", 100, 100, { ...fitToSlot(second, 100, 100, 1000, 1000) });
  eq(nextSlot(slots, [l1, l2], 1000, 1000), null, "with both taken there is nothing left to offer");
  eq(nextSlot([], [], 1000, 1000), null, "a template with no slots offers none");
  eq(nextSlot(undefined, [], 1000, 1000), null, "and neither does one with no slots field at all");
}
{
  // Contain, not cover. A square picture into a wide panel is limited by the
  // panel's HEIGHT, and must not spill past its sides.
  const wide = { x: 0.5, y: 0.5, w: 0.6, h: 0.2 }; // 600x200 px
  const p = fitToSlot(wide, 100, 100, 1000, 1000);
  near(p.w * 1000, 200, 1e-9, "a square into a wide panel is limited by the panel's height");
  const b = layerBox(newLayer("a", 100, 100, p), 1000, 1000);
  assert(b.w <= 600 + 1e-9 && b.h <= 200 + 1e-9, "and the result is inside the panel on both axes");
  near(b.w / b.h, 1, 1e-9, "with the picture's aspect intact");
  // The other way round: a wide picture into a tall panel is limited by width.
  const tall = { x: 0.5, y: 0.5, w: 0.2, h: 0.6 };
  const q = fitToSlot(tall, 400, 100, 1000, 1000);
  near(q.w * 1000, 200, 1e-9, "a wide picture into a tall panel is limited by the panel's width");
  // A slot's tilt is inherited, so a picture dropped on a tilted sheet of paper
  // arrives already lying on it.
  eq(fitToSlot({ ...wide, rot: 7 }, 100, 100, 1000, 1000).rot, 7, "a layer inherits its slot's angle");
  eq([p.x, p.y], [0.5, 0.5], "and its centre");
}

// ---- compositing order ----
{
  // The single most important property, and the one the old paste path broke:
  // the base goes down first, the layers over it, the captions over those. A
  // recording context proves the order rather than a screenshot proving it once.
  const calls = [];
  const ctx = {
    save: () => {},
    restore: () => {},
    translate: () => {},
    rotate: () => {},
    fillRect: () => {},
    strokeRect: () => {},
    setLineDash: () => {},
    beginPath: () => {},
    arc: () => {},
    fill: () => {},
    stroke: () => {},
    measureText: (t) => ({ width: t.length * 6 }),
    drawImage: (el) => calls.push(`img:${el.tag}`),
    strokeText: (t) => calls.push(`stroke:${t}`),
    fillText: (t) => calls.push(`text:${t}`),
  };
  const base = { tag: "base" };
  const over = { tag: "over" };
  const layers = [newLayer("a1", 100, 100, { w: 0.3 })];
  drawMeme(ctx, base, [newCaption("HI")], 800, 800, { layers, imageFor: () => over });
  eq(calls, ["img:base", "img:over", "stroke:HI", "text:HI"], "base, then layers, then captions");
}
{
  // Editor furniture must never reach the picture. The send path passes no
  // placeholder and no slots; both guards are checked, since forgetting either
  // one is how a dashed "paste a picture" box ends up in someone's meme.
  const painted = [];
  const ctx = {
    save: () => {},
    restore: () => {},
    translate: () => {},
    rotate: () => {},
    fillRect: () => painted.push("slotfill"),
    strokeRect: () => painted.push("slotbox"),
    setLineDash: () => {},
    measureText: (t) => ({ width: t.length * 6 }),
    drawImage: () => {},
    strokeText: () => {},
    fillText: (t) => painted.push(`text:${t}`),
  };
  const slots = [{ x: 0.5, y: 0.5, w: 0.4, h: 0.4 }];
  drawMeme(ctx, { tag: "b" }, [], 800, 800, { slots, layers: [] });
  eq(painted, [], "the send path paints no slot placeholder even if slots are passed");
  painted.length = 0;
  drawMeme(ctx, { tag: "b" }, [], 800, 800, { slots, layers: [], placeholder: "Your text" });
  assert(painted.includes("slotbox"), `the editor does paint the empty slot (got ${JSON.stringify(painted)})`);
  painted.length = 0;
  const filling = newLayer("a1", 100, 100, { x: 0.5, y: 0.5, w: 0.3 });
  drawMeme(ctx, { tag: "b" }, [], 800, 800, { slots, layers: [filling], placeholder: "Your text" });
  eq(painted, [], "and stops the moment a picture lands in it");
}
{
  // A layer whose picture hasn't decoded yet is skipped, not drawn as a blank
  // rectangle and not thrown over.
  let drew = 0;
  const ctx = {
    save: () => {},
    restore: () => {},
    translate: () => {},
    rotate: () => {},
    fillRect: () => {},
    drawImage: () => drew++,
    measureText: (t) => ({ width: t.length * 6 }),
    strokeText: () => {},
    fillText: () => {},
  };
  drawMeme(ctx, { tag: "b" }, [], 800, 800, { layers: [newLayer("a1", 10, 10)], imageFor: () => null });
  eq(drew, 1, "a layer still decoding is skipped, leaving only the base drawn");
}

// ---- sessions ----
{
  const s = newSession({ template: "2za3u1.webp", captions: [newCaption("hi")] });
  eq(s.v, 1, "a session is versioned");
  eq(s.layers, [], "and starts with no layers");
  assert(JSON.parse(JSON.stringify(s)).template === "2za3u1.webp", "a session survives a JSON round trip");
  // Layers hold a key, not a data URL — that is what keeps sixty undo snapshots
  // from carrying sixty copies of the same megabyte.
  const lay = newLayer("a1", 10, 10);
  assert(!("src" in lay) && lay.asset === "a1", "a layer carries an asset key, never the image data");
  eq(usedAssets([lay], { a1: "data:x", a2: "data:y" }), { a1: "data:x" }, "saving a session drops orphaned assets");
  eq(usedAssets([], { a1: "data:x" }), {}, "and all of them when there are no layers left");
}

if (failures) {
  console.error(`\n${failures} meme test(s) failed`);
  process.exit(1);
}
console.log("meme.js: all tests passed");
