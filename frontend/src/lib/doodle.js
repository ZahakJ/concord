// doodle.js — the stroke format behind the composer's drawing pad.
//
// A doodle is NOT an image. It is a list of strokes — a colour index, a width
// index and a run of quantized points — carried inside the ordinary MLS message
// as `[doodle](concord://doodle/v1/<b64url>)`, exactly the way polls and send
// effects ride. That buys three things at once: zero backend change, history
// sync for free, and rendering as SVG, so a doodle is crisp at any zoom and
// re-themes with the app instead of baking one theme's ink into pixels.
//
// It also buys the bound that matters. A raster doodle is an attacker-chosen
// number of bytes decoded by an image decoder; a stroke list is an
// attacker-chosen number of NUMBERS, and numbers can be counted before anything
// is drawn. Everything below exists to make that count cheap and to make
// exceeding it render nothing at all.
//
// CONTAINMENT (see decode()): strokes per doodle, points per stroke, points per
// doodle, coordinate range and the encoded length are all capped, and a token
// that breaks ANY of them decodes to null — which renders nothing, the same
// fail-closed shape every other token has. Nothing is truncated and nothing is
// clamped: a drawing that arrives too big is not a smaller drawing, it is a
// drawing this client will not vouch for.

import { bytesToB64url, b64urlToBytes } from "./b64url.js";

// The drawing space. Fixed and integral so a point costs a small varint and
// every client folds the same geometry; the SVG viewBox scales it to whatever
// width the message row happens to be.
export const DOODLE_W = 640;
export const DOODLE_H = 400;

// The bounds. MAX_TOTAL_POINTS is the one that actually protects the render
// path — 64 strokes of 512 points each would be 32,768 points, and the whole
// point of a cap is that the worst case is small.
export const MAX_STROKES = 64;
export const MAX_STROKE_POINTS = 512;
export const MAX_TOTAL_POINTS = 1000;

// The encoded cap, checked BEFORE any decoding work happens — so an attacker's
// megabyte is refused on its length and never reaches atob at all.
//
// It is set from the point caps rather than guessed. A delta pair costs at most
// four bytes (either axis can jump the full width of the pad), plus three bytes
// of stroke header, so the largest doodle this format can express is
// 1000×4 + 64×3 = 4,192 bytes — 5,590 base64 characters. 6,000 sits just above
// that, which means the POINT caps are what bind for anything real and this cap
// only ever catches something that was never a drawing. A drawing someone
// actually makes is a few hundred bytes; see the measurement in the tests.
export const MAX_TOKEN_CHARS = 6000;

// Stroke widths, in doodle units. Three, because a fourth is a decision nobody
// makes and a third is the difference between writing and drawing.
export const DOODLE_WIDTHS = [2, 5, 12];

// The palette is token-driven so a doodle is legible in both themes without the
// author choosing a theme for the reader. Every hue is mixed toward var(--text)
// — the same derivation the syntax highlighter uses — so on a light ground the
// ink darkens and on a dark ground it lifts, and "red" stays recognisably red
// either way. `ink` and `accent` are pure tokens: ink is the body text colour,
// accent is whatever the wearer's guild accent currently is.
export const DOODLE_COLOURS = [
  { id: "ink", label: "Ink", css: "var(--text)" },
  { id: "accent", label: "Accent", css: "var(--accent)" },
  { id: "red", label: "Red", css: "color-mix(in srgb, #e5484d 72%, var(--text))" },
  { id: "orange", label: "Orange", css: "color-mix(in srgb, #f76b15 72%, var(--text))" },
  { id: "yellow", label: "Yellow", css: "color-mix(in srgb, #ffb224 68%, var(--text))" },
  { id: "green", label: "Green", css: "color-mix(in srgb, #30a46c 72%, var(--text))" },
  { id: "blue", label: "Blue", css: "color-mix(in srgb, #0091ff 72%, var(--text))" },
  { id: "violet", label: "Violet", css: "color-mix(in srgb, #8e4ec6 72%, var(--text))" },
];

export const DOODLE_RE = /\[doodle\]\(concord:\/\/doodle\/v1\/([A-Za-z0-9_-]+)\)/;

const VERSION = 1;

// ---- varint plumbing --------------------------------------------------------
// LEB128 for counts and absolute coordinates, zigzag LEB128 for deltas. A
// typical stroke moves a handful of units per sample, so a delta pair costs two
// bytes; that is the whole reason a 400-point scribble fits in a few hundred.

function pushVarint(out, n) {
  let v = n >>> 0;
  while (v >= 0x80) {
    out.push((v & 0x7f) | 0x80);
    v >>>= 7;
  }
  out.push(v);
}

function zigzag(n) {
  return (n << 1) ^ (n >> 31);
}

function unzigzag(n) {
  return (n >>> 1) ^ -(n & 1);
}

// A reader that fails by SETTING A FLAG rather than throwing. Every read is on
// the hot path of a message row; an exception per malformed token is a
// try/catch around the whole decode, which is exactly how a partial decode ends
// up half-applied. `bad` makes "we stopped believing this input" a value.
function reader(bytes) {
  return { bytes, i: 0, bad: false };
}

function readVarint(r) {
  let shift = 0;
  let out = 0;
  for (;;) {
    if (r.i >= r.bytes.length || shift > 28) {
      r.bad = true;
      return 0;
    }
    const b = r.bytes[r.i++];
    out |= (b & 0x7f) << shift;
    if ((b & 0x80) === 0) return out >>> 0;
    shift += 7;
  }
}

// ---- encode -----------------------------------------------------------------

function clampInt(n, lo, hi) {
  const v = Math.round(Number(n));
  if (!Number.isFinite(v)) return lo;
  return v < lo ? lo : v > hi ? hi : v;
}

// encodeStrokes(strokes) -> base64url payload, or "" if there is nothing to
// send. Strokes are [{ c, w, pts: [x, y, x, y, …] }].
//
// The SEND side clamps — coordinates come from a pointer on this machine and a
// finger dragged past the edge of the pad means "the edge", not "refuse to
// draw". The RECEIVE side rejects instead; see decode().
export function encodeStrokes(strokes) {
  const out = [VERSION];
  const keep = [];
  let total = 0;
  for (const s of strokes || []) {
    const pts = s?.pts || [];
    const n = Math.min(Math.floor(pts.length / 2), MAX_STROKE_POINTS);
    if (n < 1) continue;
    if (keep.length >= MAX_STROKES) break;
    if (total + n > MAX_TOTAL_POINTS) break;
    total += n;
    keep.push({ c: clampInt(s.c, 0, DOODLE_COLOURS.length - 1), w: clampInt(s.w, 0, DOODLE_WIDTHS.length - 1), pts, n });
  }
  if (!keep.length) return "";
  pushVarint(out, keep.length);
  for (const s of keep) {
    out.push(s.c, s.w);
    pushVarint(out, s.n);
    let px = 0;
    let py = 0;
    for (let i = 0; i < s.n; i++) {
      const x = clampInt(s.pts[i * 2], 0, DOODLE_W);
      const y = clampInt(s.pts[i * 2 + 1], 0, DOODLE_H);
      if (i === 0) {
        pushVarint(out, x);
        pushVarint(out, y);
      } else {
        pushVarint(out, zigzag(x - px));
        pushVarint(out, zigzag(y - py));
      }
      px = x;
      py = y;
    }
  }
  return bytesToB64url(Uint8Array.from(out));
}

export function encodeDoodle(strokes) {
  const payload = encodeStrokes(strokes);
  return payload ? `[doodle](concord://doodle/v1/${payload})` : "";
}

// ---- decode -----------------------------------------------------------------

// decodeStrokes(payload) -> [{ c, w, pts }] or null.
//
// null is the ONLY failure mode, and it means the message renders no drawing at
// all. Every check below is a reason to return it:
//
//   • the payload is longer than MAX_TOKEN_CHARS (checked first, so an
//     attacker's megabyte never reaches atob);
//   • the version byte is not one we wrote;
//   • the stroke count, any stroke's point count, or the running total is over
//     its cap;
//   • a colour or width index names an entry that does not exist;
//   • any coordinate lands outside the drawing space;
//   • the bytes run out mid-number, or bytes are left over at the end.
//
// The last one is deliberate strictness. Trailing bytes mean the encoder that
// produced this was not this encoder, and a doodle is not a format worth being
// generous about.
export function decodeStrokes(payload) {
  if (typeof payload !== "string" || !payload || payload.length > MAX_TOKEN_CHARS) return null;
  const bytes = b64urlToBytes(payload);
  if (!bytes || bytes.length < 2) return null;
  const r = reader(bytes);
  if (bytes[r.i++] !== VERSION) return null;
  const nStrokes = readVarint(r);
  if (r.bad || nStrokes < 1 || nStrokes > MAX_STROKES) return null;
  const strokes = [];
  let total = 0;
  for (let s = 0; s < nStrokes; s++) {
    if (r.i + 2 > bytes.length) return null;
    const c = bytes[r.i++];
    const w = bytes[r.i++];
    if (c >= DOODLE_COLOURS.length || w >= DOODLE_WIDTHS.length) return null;
    const n = readVarint(r);
    if (r.bad || n < 1 || n > MAX_STROKE_POINTS) return null;
    total += n;
    if (total > MAX_TOTAL_POINTS) return null;
    const pts = new Array(n * 2);
    let x = 0;
    let y = 0;
    for (let i = 0; i < n; i++) {
      if (i === 0) {
        x = readVarint(r);
        y = readVarint(r);
      } else {
        x += unzigzag(readVarint(r));
        y += unzigzag(readVarint(r));
      }
      if (r.bad) return null;
      if (x < 0 || x > DOODLE_W || y < 0 || y > DOODLE_H) return null;
      pts[i * 2] = x;
      pts[i * 2 + 1] = y;
    }
    strokes.push({ c, w, pts });
  }
  if (r.i !== bytes.length) return null;
  return strokes;
}

// parseDoodle(content) -> strokes or null. The message body may carry words
// alongside the token, exactly like a poll or an effect.
export function parseDoodle(content) {
  if (!content) return null;
  const m = content.match(DOODLE_RE);
  return m ? decodeStrokes(m[1]) : null;
}

export function stripDoodle(content) {
  return content ? content.replace(DOODLE_RE, "").trim() : content;
}

// ---- rendering --------------------------------------------------------------

// strokePath(stroke) -> an SVG path `d`. A single-point stroke (a tap) becomes
// a zero-length line, which with round caps is the dot the author meant.
export function strokePath(stroke) {
  const p = stroke.pts;
  if (p.length === 2) return `M${p[0]} ${p[1]}L${p[0]} ${p[1]}`;
  let d = `M${p[0]} ${p[1]}`;
  for (let i = 2; i < p.length; i += 2) d += `L${p[i]} ${p[i + 1]}`;
  return d;
}

export function strokeColour(stroke) {
  return (DOODLE_COLOURS[stroke.c] || DOODLE_COLOURS[0]).css;
}

export function strokeWidth(stroke) {
  return DOODLE_WIDTHS[stroke.w] ?? DOODLE_WIDTHS[0];
}

// ---- capture ----------------------------------------------------------------

// simplify() is Douglas–Peucker: it throws away the points a reader could not
// tell were missing. A pointer at 120 Hz emits a point every 8 ms whether the
// hand moved or not, so a two-second stroke arrives with 240 points describing
// a line that three would draw. Running it before quantization is what keeps a
// real drawing in the hundreds of bytes rather than the thousands, and it is
// what makes MAX_TOTAL_POINTS a bound nobody normal ever meets.
//
// Iterative rather than recursive: the input is attacker-free (it is this
// machine's own pointer) but a 5,000-point stroke recursing per split is a
// stack nobody needs to gamble on.
export function simplify(pts, eps = 1.2) {
  const n = Math.floor(pts.length / 2);
  if (n < 3) return pts.slice();
  const keep = new Uint8Array(n);
  keep[0] = 1;
  keep[n - 1] = 1;
  const stack = [[0, n - 1]];
  while (stack.length) {
    const [lo, hi] = stack.pop();
    if (hi - lo < 2) continue;
    const ax = pts[lo * 2];
    const ay = pts[lo * 2 + 1];
    const bx = pts[hi * 2];
    const by = pts[hi * 2 + 1];
    const dx = bx - ax;
    const dy = by - ay;
    const len = Math.hypot(dx, dy);
    let worst = -1;
    let worstAt = -1;
    for (let i = lo + 1; i < hi; i++) {
      const px = pts[i * 2];
      const py = pts[i * 2 + 1];
      // Distance from the point to the segment. A degenerate segment (the hand
      // came back to where it started) measures from the endpoint instead,
      // which is what keeps a loop from collapsing to nothing.
      const d = len === 0 ? Math.hypot(px - ax, py - ay) : Math.abs(dy * px - dx * py + bx * ay - by * ax) / len;
      if (d > worst) {
        worst = d;
        worstAt = i;
      }
    }
    if (worst > eps && worstAt > 0) {
      keep[worstAt] = 1;
      stack.push([lo, worstAt], [worstAt, hi]);
    }
  }
  const out = [];
  for (let i = 0; i < n; i++) {
    if (keep[i]) out.push(pts[i * 2], pts[i * 2 + 1]);
  }
  return out;
}

// quantize() rounds to the integer grid the encoder writes and drops points
// that landed on the one before. Done after simplify, because simplification
// measures against the curve the hand actually drew.
export function quantize(pts) {
  const out = [];
  let lx = NaN;
  let ly = NaN;
  for (let i = 0; i + 1 < pts.length; i += 2) {
    const x = clampInt(pts[i], 0, DOODLE_W);
    const y = clampInt(pts[i + 1], 0, DOODLE_H);
    if (x === lx && y === ly) continue;
    out.push(x, y);
    lx = x;
    ly = y;
  }
  return out;
}

// prepareStroke turns raw pointer samples into the stroke that gets encoded.
export function prepareStroke(stroke, eps = 1.2) {
  const pts = quantize(simplify(stroke.pts || [], eps)).slice(0, MAX_STROKE_POINTS * 2);
  return { c: stroke.c | 0, w: stroke.w | 0, pts };
}

// doodleTotalPoints reports how full a drawing is, so the pad can stop
// accepting ink at the ceiling instead of silently dropping the end of it.
export function doodleTotalPoints(strokes) {
  let n = 0;
  for (const s of strokes || []) n += Math.floor((s?.pts?.length || 0) / 2);
  return n;
}
