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

if (failures) {
  console.error(`\n${failures} meme test(s) failed`);
  process.exit(1);
}
console.log("meme.js: all tests passed");
