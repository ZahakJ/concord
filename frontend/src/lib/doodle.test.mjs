// Tests for the doodle stroke format. Three things are being defended here and
// only one of them is about drawing:
//
//   1. BOUNDS. A doodle arrives from a stranger and turns into SVG paths in a
//      windowed message list. Every cap has to be provable, and "provable"
//      means a hand-forged token that breaks it decodes to null — not to a
//      truncated drawing, not to an exception.
//   2. ROUND TRIP. What one person drew is what everybody else sees. A codec
//      that quietly loses the last point of a stroke is a codec nobody notices
//      is broken until a drawing is wrong.
//   3. SIZE. The whole argument for strokes over pixels is that a doodle is a
//      few hundred bytes. That number is measured here, not asserted.
import {
  encodeStrokes,
  decodeStrokes,
  encodeDoodle,
  parseDoodle,
  stripDoodle,
  simplify,
  quantize,
  prepareStroke,
  strokePath,
  doodleTotalPoints,
  DOODLE_W,
  DOODLE_H,
  MAX_STROKES,
  MAX_STROKE_POINTS,
  MAX_TOTAL_POINTS,
  MAX_TOKEN_CHARS,
  DOODLE_COLOURS,
  DOODLE_WIDTHS,
} from "./doodle.js";

let failures = 0;
function check(name, got, want) {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) {
    failures++;
    console.error(`FAIL ${name}\n  got:  ${g}\n  want: ${w}`);
  }
}

// A doodle the encoder never made. The bounds are enforced on DECODE, so the
// only honest way to test them is to build the bytes by hand — which is also
// exactly what an attacker does.
function b64url(bytes) {
  return Buffer.from(Uint8Array.from(bytes)).toString("base64url");
}
function varint(n) {
  const out = [];
  let v = n >>> 0;
  while (v >= 0x80) {
    out.push((v & 0x7f) | 0x80);
    v >>>= 7;
  }
  out.push(v);
  return out;
}
function zig(n) {
  return (n << 1) ^ (n >> 31);
}
// forge({ version, strokes: [{ c, w, n, pts }] }) — writes the wire format with
// no checks at all, so any field can be pushed past its cap.
function forge({ version = 1, strokes = [], count = null, trailing = [] } = {}) {
  const out = [version, ...varint(count == null ? strokes.length : count)];
  for (const s of strokes) {
    out.push(s.c ?? 0, s.w ?? 0);
    const n = s.n == null ? Math.floor(s.pts.length / 2) : s.n;
    out.push(...varint(n));
    let px = 0;
    let py = 0;
    for (let i = 0; i * 2 < s.pts.length; i++) {
      const x = s.pts[i * 2];
      const y = s.pts[i * 2 + 1];
      if (i === 0) out.push(...varint(x), ...varint(y));
      else out.push(...varint(zig(x - px)), ...varint(zig(y - py)));
      px = x;
      py = y;
    }
  }
  out.push(...trailing);
  return b64url(out);
}

// ---- round trip -------------------------------------------------------------

{
  const strokes = [
    { c: 0, w: 1, pts: [10, 20, 40, 22, 90, 180, 91, 181] },
    { c: 5, w: 2, pts: [DOODLE_W, DOODLE_H, 0, 0] },
    { c: 1, w: 0, pts: [300, 200] }, // a tap: one point, which must survive
  ];
  const back = decodeStrokes(encodeStrokes(strokes));
  check("round trip is exact", back, strokes);
}

// The extremes of the coordinate space are legal, both ends. A codec that
// treats the far corner as out of range loses the edge of every drawing.
check("corners survive", decodeStrokes(encodeStrokes([{ c: 0, w: 0, pts: [0, 0, DOODLE_W, DOODLE_H] }])), [
  { c: 0, w: 0, pts: [0, 0, DOODLE_W, DOODLE_H] },
]);

// Every colour and every width index round-trips, so adding one to the table
// cannot silently become unreachable.
{
  const all = DOODLE_COLOURS.map((_, c) => ({ c, w: c % DOODLE_WIDTHS.length, pts: [c, c] }));
  check("every palette entry round-trips", decodeStrokes(encodeStrokes(all)), all);
}

// The token wrapper, and the strip that has to happen whether or not the
// drawing decoded. (Message.svelte strips unconditionally: a doodle this client
// refuses must leave no base64 behind in the body.)
{
  const tok = encodeDoodle([{ c: 0, w: 0, pts: [1, 2, 3, 4] }]);
  check("token shape", /^\[doodle\]\(concord:\/\/doodle\/v1\/[A-Za-z0-9_-]+\)$/.test(tok), true);
  check("parses out of a body with words", parseDoodle(`look at this ${tok}`), [{ c: 0, w: 0, pts: [1, 2, 3, 4] }]);
  check("strip leaves the words", stripDoodle(`look at this ${tok}`), "look at this");
  check("strip removes a token this client REFUSED", stripDoodle(`hi [doodle](concord://doodle/v1/AAAA)`), "hi");
}

check("nothing to encode is not a token", encodeDoodle([]), "");
check("no token, no doodle", parseDoodle("just words"), null);
check("no content, no doodle", parseDoodle(""), null);

// ---- bounds: every one of them renders NOTHING ------------------------------

check("a payload over the character cap is refused unread", decodeStrokes("A".repeat(MAX_TOKEN_CHARS + 1)), null);
check("garbage is refused", decodeStrokes("!!!not base64!!!"), null);
check("empty is refused", decodeStrokes(""), null);
check("a non-string is refused", decodeStrokes(null), null);

check("a version we did not write is refused", decodeStrokes(forge({ version: 2, strokes: [{ pts: [1, 1] }] })), null);
check("zero strokes is refused", decodeStrokes(forge({ strokes: [] })), null);

// Strokes per doodle. At the cap it decodes; one over, nothing.
{
  const at = [];
  for (let i = 0; i < MAX_STROKES; i++) at.push({ c: 0, w: 0, pts: [i, i] });
  check("at the stroke cap it renders", decodeStrokes(forge({ strokes: at }))?.length, MAX_STROKES);
  check("one stroke over the cap renders nothing", decodeStrokes(forge({ strokes: [...at, { c: 0, w: 0, pts: [1, 1] }] })), null);
}

// Points per stroke.
{
  const pts = [];
  for (let i = 0; i < MAX_STROKE_POINTS; i++) pts.push(i % DOODLE_W, i % DOODLE_H);
  check("at the per-stroke point cap it renders", decodeStrokes(forge({ strokes: [{ pts }] }))?.[0].pts.length, MAX_STROKE_POINTS * 2);
  const over = [...pts, 5, 5];
  check("one point over the per-stroke cap renders nothing", decodeStrokes(forge({ strokes: [{ pts: over }] })), null);
}

// Points per doodle — the cap that actually protects the render path, because
// MAX_STROKES × MAX_STROKE_POINTS is far more geometry than anyone may send.
{
  const wide = [];
  let left = MAX_TOTAL_POINTS + 1;
  while (left > 0) {
    const n = Math.min(left, MAX_STROKE_POINTS);
    const pts = [];
    for (let i = 0; i < n; i++) pts.push(i % DOODLE_W, i % DOODLE_H);
    wide.push({ pts });
    left -= n;
  }
  check("over the total point cap renders nothing", decodeStrokes(forge({ strokes: wide })), null);
  check("the forged token really was over the cap", doodleTotalPoints(wide), MAX_TOTAL_POINTS + 1);
}

// A colour or width index naming a table entry that does not exist. This is the
// "resolution failing yields nothing" rule: the safety is the lookup, and a
// lookup that cannot fail closed is not one.
check("an unknown colour renders nothing", decodeStrokes(forge({ strokes: [{ c: DOODLE_COLOURS.length, pts: [1, 1] }] })), null);
check("an unknown width renders nothing", decodeStrokes(forge({ strokes: [{ w: DOODLE_WIDTHS.length, pts: [1, 1] }] })), null);

// Coordinates outside the drawing space. The proposal's own containment note
// said clamp; this refuses instead, because a drawing whose points were moved
// is not the drawing that was sent, and rendering a lie is worse than
// rendering nothing.
check("x past the right edge renders nothing", decodeStrokes(forge({ strokes: [{ pts: [DOODLE_W + 1, 10] }] })), null);
check("y past the bottom renders nothing", decodeStrokes(forge({ strokes: [{ pts: [10, DOODLE_H + 1] }] })), null);
check("a delta that walks off the canvas renders nothing", decodeStrokes(forge({ strokes: [{ pts: [10, 10, DOODLE_W + 40, 10] }] })), null);

// Truncation and padding. Bytes that run out mid-number, and bytes left over.
{
  const good = forge({ strokes: [{ pts: [10, 10, 20, 20, 30, 30] }] });
  const raw = Buffer.from(good, "base64url");
  check("a truncated token renders nothing", decodeStrokes(raw.subarray(0, raw.length - 2).toString("base64url")), null);
  check("a lying point count renders nothing", decodeStrokes(forge({ strokes: [{ n: 40, pts: [10, 10] }] })), null);
  check("a lying stroke count renders nothing", decodeStrokes(forge({ count: 4, strokes: [{ pts: [1, 1] }] })), null);
  check("trailing bytes render nothing", decodeStrokes(forge({ strokes: [{ pts: [1, 1] }], trailing: [0, 0, 0] })), null);
}

// ---- simplification, quantization and the size claim ------------------------

// Douglas–Peucker keeps the shape and drops the samples nobody could see. A
// straight line sampled 100 times is two points.
{
  const line = [];
  for (let i = 0; i < 100; i++) line.push(i * 6, 200);
  check("a straight line simplifies to its ends", simplify(line), [0, 200, 594, 200]);
}

// A corner is a point the reader CAN see, so it survives.
{
  const corner = [];
  for (let i = 0; i < 50; i++) corner.push(i * 4, 100);
  for (let i = 0; i < 50; i++) corner.push(196, 100 + i * 4);
  const s = simplify(corner);
  check("a corner survives simplification", s.includes(196) && s.length <= 8, true);
}

// Two points cannot be simplified, and a single point is not lost.
check("one point passes through", simplify([5, 5]), [5, 5]);
check("two points pass through", simplify([5, 5, 9, 9]), [5, 5, 9, 9]);

// Quantization rounds onto the grid the encoder writes and collapses the
// duplicates that produces — a hand held still for a second is one point.
check("quantize rounds and dedupes", quantize([10.2, 20.4, 10.4, 20.1, 11.9, 20.6]), [10, 20, 12, 21]);
check("quantize clamps a finger dragged off the pad", quantize([-30, -5, DOODLE_W + 90, DOODLE_H + 90]), [0, 0, DOODLE_W, DOODLE_H]);

// THE SIZE CLAIM. A realistic scribble — six strokes of a few hundred raw
// samples each, the way a pointer at 120 Hz actually delivers them — has to
// come out in the hundreds of bytes, not the thousands. The numbers are
// printed so a regression is visible rather than merely failing.
{
  let raw = 0;
  const drawn = [];
  for (let s = 0; s < 6; s++) {
    const pts = [];
    // A wobbling arc: smooth enough that simplification earns its keep,
    // irregular enough that it is not secretly a straight line.
    for (let i = 0; i < 320; i++) {
      const t = i / 320;
      pts.push(60 + t * 500 + Math.sin(t * 9 + s) * 26, 90 + s * 40 + Math.cos(t * 7 + s) * 30);
    }
    raw += pts.length / 2;
    drawn.push(prepareStroke({ c: s % DOODLE_COLOURS.length, w: s % DOODLE_WIDTHS.length, pts }));
  }
  const payload = encodeStrokes(drawn);
  const kept = doodleTotalPoints(drawn);
  console.log(
    `doodle.js: a six-stroke scribble — ${raw} raw samples -> ${kept} points -> ` +
      `${Buffer.from(payload, "base64url").length}B binary, ${payload.length} chars encoded ` +
      `(${(payload.length / kept).toFixed(2)} chars/point)`,
  );
  check("a realistic scribble stays well under the cap", payload.length < 1500, true);
  check("a realistic scribble round-trips", decodeStrokes(payload)?.length, 6);
}

// And the worst case the format can express — the point ceiling, every point a
// long jump so no delta is cheap — still fits the character cap. That is the
// arithmetic MAX_TOKEN_CHARS is derived from: if this ever fails, the two caps
// have drifted apart and a legal drawing has become unsendable.
{
  const strokes = [];
  let left = MAX_TOTAL_POINTS;
  let seed = 7;
  while (left > 0) {
    const n = Math.min(left, MAX_STROKE_POINTS);
    const pts = [];
    for (let i = 0; i < n; i++) {
      seed = (seed * 1103515245 + 12345) & 0x7fffffff;
      pts.push(seed % (DOODLE_W + 1), (seed >> 8) % (DOODLE_H + 1));
    }
    strokes.push({ c: 0, w: 0, pts });
    left -= n;
  }
  const payload = encodeStrokes(strokes);
  console.log(`doodle.js: the worst legal doodle — ${MAX_TOTAL_POINTS} scattered points, ${payload.length} chars encoded`);
  check("the worst legal doodle fits the character cap", payload.length <= MAX_TOKEN_CHARS, true);
  check("the worst legal doodle still decodes", doodleTotalPoints(decodeStrokes(payload)), MAX_TOTAL_POINTS);
}

// The encoder is the other half of the containment: it never emits a doodle
// that its own decoder would refuse, so the pad cannot build an unsendable one.
{
  const tooMany = [];
  for (let i = 0; i < MAX_STROKES + 20; i++) tooMany.push({ c: 0, w: 0, pts: [i % DOODLE_W, i % DOODLE_H] });
  check("the encoder stops at the stroke cap", decodeStrokes(encodeStrokes(tooMany))?.length, MAX_STROKES);
  const longRun = [];
  for (let i = 0; i < MAX_STROKE_POINTS + 100; i++) longRun.push(i % DOODLE_W, i % DOODLE_H);
  check("the encoder stops at the point cap", decodeStrokes(encodeStrokes([{ c: 0, w: 0, pts: longRun }]))?.[0].pts.length, MAX_STROKE_POINTS * 2);
  check("the encoder clamps an out-of-range colour rather than emitting one nobody can read", decodeStrokes(encodeStrokes([{ c: 99, w: 99, pts: [1, 1] }])), [
    { c: DOODLE_COLOURS.length - 1, w: DOODLE_WIDTHS.length - 1, pts: [1, 1] },
  ]);
}

// ---- rendering --------------------------------------------------------------

check("a path is a move and lines", strokePath({ pts: [1, 2, 3, 4, 5, 6] }), "M1 2L3 4L5 6");
check("a tap is a zero-length line, which round caps draw as a dot", strokePath({ pts: [7, 8] }), "M7 8L7 8");

if (failures) {
  console.error(`\n${failures} doodle test(s) failed`);
  process.exit(1);
}
console.log("doodle.js: all tests passed");
