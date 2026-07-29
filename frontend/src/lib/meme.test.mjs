// Tests for the meme editor's layout maths. A fake measurer stands in for the
// canvas: every character is exactly 10px wide at size 100, scaling linearly.
// That makes expected widths countable by hand, which is the whole point —
// wrapping bugs are invisible against a real font's fractional metrics.
import { wrapLines, fitCaption, captionBox, captionAt, newCaption } from "./meme.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};
const eq = (a, b, msg) =>
  assert(JSON.stringify(a) === JSON.stringify(b), `${msg}\n  got ${JSON.stringify(a)}\n  exp ${JSON.stringify(b)}`);

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
  assert(b.h > 0 && b.lineHeight > b.size, "line height leaves room between lines");
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

if (failures) {
  console.error(`\n${failures} meme test(s) failed`);
  process.exit(1);
}
console.log("meme.js: all tests passed");
