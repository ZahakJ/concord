// photoedit.js — the picture editor's model and drawing, without the DOM's
// event plumbing (PhotoEditor.svelte is the interaction).
//
// An edit is a small description, not pixels: a quarter-turn count, a crop
// rectangle, marks (pen strokes, arrows, text) and an output size. Everything
// is in coordinates normalised to the TURNED picture (0..1 on each axis), so
// the preview at screen size and the export at full size draw from the same
// numbers, and a turn rotates the numbers instead of resampling anything.
// Stroke widths and text sizes are fractions of the picture's shorter side,
// which a quarter turn does not change.
//
// The export is a fresh canvas encoded by the browser. A canvas carries no
// metadata, so whatever EXIF the original had (location, camera, time) does
// not survive an edit; the core strips it again on send regardless.

export const COLORS = ["#ffffff", "#111111", "#ff4d4f", "#ffc53d", "#36cfc9", "#597ef7"];
export const WIDTHS = [0.006, 0.012, 0.022]; // of the shorter side
export const TEXT_SIZE = 0.07; // of the shorter side
export const SIZES = [0, 2048, 1280, 800]; // longest edge; 0 is as it is
export const MIN_CROP = 0.04;

export function newEdit() {
  return { turns: 0, crop: { x: 0, y: 0, w: 1, h: 1 }, marks: [], size: 0 };
}

// isPristine: nothing to apply, so Done can hand back the original bytes.
export function isPristine(e) {
  const c = e.crop;
  return e.turns % 4 === 0 && !e.marks.length && !e.size && c.x === 0 && c.y === 0 && c.w === 1 && c.h === 1;
}

// The turned picture's size in pixels.
export function turnedSize(w, h, turns) {
  return turns % 2 ? { w: h, h: w } : { w, h };
}

// A point turned a quarter clockwise (dir 1) or anticlockwise (dir -1).
export function turnPoint([x, y], dir) {
  return dir > 0 ? [1 - y, x] : [y, 1 - x];
}

function turnRect(r, dir) {
  const a = turnPoint([r.x, r.y], dir);
  const b = turnPoint([r.x + r.w, r.y + r.h], dir);
  const x = Math.min(a[0], b[0]);
  const y = Math.min(a[1], b[1]);
  return { x, y, w: Math.abs(a[0] - b[0]), h: Math.abs(a[1] - b[1]) };
}

// turn rotates the whole edit a quarter: the picture, the crop and every mark.
export function turn(e, dir) {
  const marks = e.marks.map((m) => {
    if (m.type === "pen") return { ...m, pts: m.pts.map((p) => turnPoint(p, dir)) };
    if (m.type === "arrow") return { ...m, a: turnPoint(m.a, dir), b: turnPoint(m.b, dir) };
    return { ...m, at: turnPoint(m.at, dir) };
  });
  return { ...e, turns: (((e.turns + dir) % 4) + 4) % 4, crop: turnRect(e.crop, dir), marks };
}

export const clamp01 = (v) => Math.max(0, Math.min(1, v));

// The crop rectangle kept inside the picture and no smaller than MIN_CROP.
export function clampCrop(r) {
  const w = Math.max(MIN_CROP, Math.min(1, r.w));
  const h = Math.max(MIN_CROP, Math.min(1, r.h));
  return { x: Math.max(0, Math.min(1 - w, r.x)), y: Math.max(0, Math.min(1 - h, r.y)), w, h };
}

// A square crop, as large as fits, centred on the current one. `aspect` is the
// turned picture's width over height, so "square" is square in pixels.
export function squareCrop(c, aspect) {
  const side = Math.min(c.w * aspect, c.h); // in units of the picture's height
  const w = side / aspect;
  const h = side;
  return clampCrop({ x: c.x + (c.w - w) / 2, y: c.y + (c.h - h) / 2, w, h });
}

// cropHandle: which part of the crop rectangle a point grabs. Corners within
// `tol` (normalised, per axis), else the inside to move it, else nothing.
export function cropHandle(c, [x, y], tolX, tolY) {
  const near = (a, b, t) => Math.abs(a - b) <= t;
  const l = near(x, c.x, tolX);
  const r = near(x, c.x + c.w, tolX);
  const t = near(y, c.y, tolY);
  const b = near(y, c.y + c.h, tolY);
  if (t && l) return "nw";
  if (t && r) return "ne";
  if (b && l) return "sw";
  if (b && r) return "se";
  const inX = x > c.x && x < c.x + c.w;
  const inY = y > c.y && y < c.y + c.h;
  if (inX && t) return "n";
  if (inX && b) return "s";
  if (inY && l) return "w";
  if (inY && r) return "e";
  if (inX && inY) return "move";
  return "";
}

// dragCrop: the crop after dragging `handle` by (dx, dy) from `start`.
// `square` keeps it square in pixels (aspect = width / height of the picture).
export function dragCrop(start, handle, dx, dy, square = false, aspect = 1) {
  let { x, y, w, h } = start;
  if (handle === "move") {
    return clampCrop({ x: x + dx, y: y + dy, w, h });
  }
  let x2 = x + w;
  let y2 = y + h;
  if (handle.includes("w")) x = Math.min(x + dx, x2 - MIN_CROP);
  if (handle.includes("e")) x2 = Math.max(x2 + dx, x + MIN_CROP);
  if (handle.includes("n")) y = Math.min(y + dy, y2 - MIN_CROP);
  if (handle.includes("s")) y2 = Math.max(y2 + dy, y + MIN_CROP);
  x = Math.max(0, x);
  y = Math.max(0, y);
  x2 = Math.min(1, x2);
  y2 = Math.min(1, y2);
  let r = { x, y, w: x2 - x, h: y2 - y };
  if (square) {
    // The smaller side decides; the corner opposite the handle stays put.
    const side = Math.min(r.w * aspect, r.h);
    const nw = side / aspect;
    const nh = side;
    r = {
      x: handle.includes("w") ? r.x + r.w - nw : r.x,
      y: handle.includes("n") ? r.y + r.h - nh : r.y,
      w: nw,
      h: nh,
    };
  }
  return clampCrop(r);
}

// outputSize: the pixel size an edit exports at.
export function outputSize(w, h, e) {
  const t = turnedSize(w, h, e.turns);
  const cw = Math.max(1, Math.round(e.crop.w * t.w));
  const ch = Math.max(1, Math.round(e.crop.h * t.h));
  const s = e.size ? Math.min(1, e.size / Math.max(cw, ch)) : 1;
  return { w: Math.max(1, Math.round(cw * s)), h: Math.max(1, Math.round(ch * s)) };
}

// drawTurned paints `img` (natural size w × h) turned by e.turns onto ctx,
// filling a W × H box (the turned size, at whatever scale).
export function drawTurned(ctx, img, w, h, turns, W, H) {
  ctx.save();
  ctx.translate(W / 2, H / 2);
  ctx.rotate((turns * Math.PI) / 2);
  const sx = turns % 2 ? H / w : W / w;
  const sy = turns % 2 ? W / h : H / h;
  ctx.drawImage(img, (-w * sx) / 2, (-h * sy) / 2, w * sx, h * sy);
  ctx.restore();
}

// drawMarks paints the marks onto a W × H box holding the turned picture.
export function drawMarks(ctx, marks, W, H) {
  const short = Math.min(W, H);
  for (const m of marks) {
    ctx.save();
    ctx.strokeStyle = ctx.fillStyle = m.color;
    ctx.lineCap = "round";
    ctx.lineJoin = "round";
    if (m.type === "pen") {
      ctx.lineWidth = m.width * short;
      ctx.beginPath();
      m.pts.forEach(([x, y], i) => (i ? ctx.lineTo(x * W, y * H) : ctx.moveTo(x * W, y * H)));
      if (m.pts.length === 1) ctx.lineTo(m.pts[0][0] * W + 0.01, m.pts[0][1] * H);
      ctx.stroke();
    } else if (m.type === "arrow") {
      const lw = m.width * short;
      const ax = m.a[0] * W;
      const ay = m.a[1] * H;
      const bx = m.b[0] * W;
      const by = m.b[1] * H;
      const ang = Math.atan2(by - ay, bx - ax);
      const head = Math.max(lw * 3.2, short * 0.03);
      ctx.lineWidth = lw;
      ctx.beginPath();
      ctx.moveTo(ax, ay);
      // The shaft stops inside the head, so its round cap does not poke out.
      ctx.lineTo(bx - Math.cos(ang) * head * 0.6, by - Math.sin(ang) * head * 0.6);
      ctx.stroke();
      ctx.beginPath();
      ctx.moveTo(bx, by);
      ctx.lineTo(bx - head * Math.cos(ang - 0.45), by - head * Math.sin(ang - 0.45));
      ctx.lineTo(bx - head * Math.cos(ang + 0.45), by - head * Math.sin(ang + 0.45));
      ctx.closePath();
      ctx.fill();
    } else if (m.type === "text") {
      const px = m.size * short;
      ctx.font = `700 ${px}px system-ui, sans-serif`;
      ctx.textAlign = "center";
      ctx.textBaseline = "middle";
      // A dark or light outline, whichever the fill is not, so the words read
      // on any picture.
      ctx.lineWidth = Math.max(2, px * 0.14);
      ctx.strokeStyle = isLight(m.color) ? "rgba(0,0,0,0.75)" : "rgba(255,255,255,0.85)";
      ctx.strokeText(m.text, m.at[0] * W, m.at[1] * H);
      ctx.fillText(m.text, m.at[0] * W, m.at[1] * H);
    }
    ctx.restore();
  }
}

export function isLight(hex) {
  const n = parseInt(String(hex).slice(1), 16);
  const r = (n >> 16) & 255;
  const g = (n >> 8) & 255;
  const b = n & 255;
  return 0.2126 * r + 0.7152 * g + 0.0722 * b > 150;
}

// render produces the edited picture on a new canvas (DOM or Offscreen).
export function render(makeCanvas, img, w, h, e) {
  const t = turnedSize(w, h, e.turns);
  const full = makeCanvas(t.w, t.h);
  const fc = full.getContext("2d");
  drawTurned(fc, img, w, h, e.turns, t.w, t.h);
  drawMarks(fc, e.marks, t.w, t.h);
  const out = outputSize(w, h, e);
  const c = makeCanvas(out.w, out.h);
  const cc = c.getContext("2d");
  cc.imageSmoothingQuality = "high";
  cc.drawImage(full, e.crop.x * t.w, e.crop.y * t.h, e.crop.w * t.w, e.crop.h * t.h, 0, 0, out.w, out.h);
  return c;
}

// The encodings to try, in order: PNG stays PNG (screenshots, drawings, with
// their sharp edges) when it fits; anything else, or a PNG too big, is JPEG.
export function encodings(srcDataUrl) {
  const png = String(srcDataUrl).startsWith("data:image/png");
  return [...(png ? [["image/png"]] : []), ["image/jpeg", 0.9], ["image/jpeg", 0.8], ["image/jpeg", 0.68]];
}

// Pictures the editor does not take: an animation would lose all but its first
// frame.
export function editable(dataUrl) {
  return /^data:image\/(png|jpeg|webp)/.test(String(dataUrl));
}
