// meme.js — the drawing half of the meme editor.
//
// Split from the component so the parts that are really just arithmetic —
// wrapping, auto-shrinking, hit-testing a caption — can be tested without a
// canvas or a browser. Everything here works in normalized coordinates (0..1 of
// the image), never pixels, so a caption placed on a 300px preview lands in the
// same spot when the same meme is rendered at full size for sending.

// Impact is THE meme face, but it ships with Windows and macOS and is absent on
// most Linux boxes, so this is a stack rather than a name. The fallbacks are
// chosen for the same silhouette: tall, narrow, very heavy. Whatever lands, the
// heavy stroke below is what actually makes it read as a meme.
const IMPACT_STACK = `Impact, Haettenschweiler, "Anton", "Arial Narrow Bold", "Franklin Gothic Heavy", sans-serif`;
const CLEAN_STACK = `"Inter", "Helvetica Neue", Arial, sans-serif`;

export const STYLES = {
  impact: {
    label: "Classic",
    family: IMPACT_STACK,
    weight: "400",
    uppercase: true,
    // Stroke width as a fraction of the font size. The classic look is a fat
    // black outline; too thin and it vanishes on a busy photo.
    stroke: 0.13,
    color: "#ffffff",
    strokeColor: "#000000",
    letterSpacing: 0.01,
  },
  clean: {
    label: "Clean",
    family: CLEAN_STACK,
    weight: "800",
    uppercase: false,
    stroke: 0.09,
    color: "#ffffff",
    strokeColor: "#000000",
    letterSpacing: 0,
  },
  caption: {
    // The "caption above the image on a white bar" format.
    label: "Caption",
    family: CLEAN_STACK,
    weight: "600",
    uppercase: false,
    stroke: 0,
    color: "#111111",
    strokeColor: "transparent",
    letterSpacing: 0,
  },
};

// A caption in the editor. x/y are the CENTRE of the box, all values 0..1.
export function newCaption(text = "", over = {}) {
  return {
    id: Math.random().toString(36).slice(2, 9),
    text,
    x: 0.5,
    y: 0.12,
    w: 0.92, // max width before wrapping, as a fraction of image width
    size: 0.11, // font size as a fraction of image HEIGHT, so it scales sanely
    style: "impact",
    align: "center",
    ...over,
  };
}

// wrapLines breaks text into lines that fit maxWidth, given a measuring
// function. Taking `measure` as an argument rather than a canvas context is
// what makes this testable — and it's the piece most likely to be wrong.
//
// A single word longer than the line (a URL, or someone holding down a key) is
// broken mid-word rather than allowed to overflow: a meme that runs off the
// side of the image is worse than an ugly break.
export function wrapLines(measure, text, maxWidth) {
  const out = [];
  for (const para of String(text).split("\n")) {
    if (!para) {
      out.push("");
      continue;
    }
    let line = "";
    for (const word of para.split(/\s+/).filter(Boolean)) {
      const attempt = line ? `${line} ${word}` : word;
      if (measure(attempt) <= maxWidth) {
        line = attempt;
        continue;
      }
      if (line) out.push(line);
      // The word now starts a fresh line. It may still be wider than the box on
      // its own, and that check has to happen here rather than only when the
      // line was already empty — otherwise a long word is spared the hard break
      // purely because something short preceded it.
      if (measure(word) > maxWidth) {
        let chunk = "";
        for (const ch of word) {
          if (chunk && measure(chunk + ch) > maxWidth) {
            out.push(chunk);
            chunk = ch;
          } else {
            chunk += ch;
          }
        }
        line = chunk;
      } else {
        line = word;
      }
    }
    out.push(line);
  }
  return out;
}

// fitCaption finds the largest font size at or below the caption's own size
// that keeps the wrapped text inside its box. Returns the lines and the size
// actually used, so the caller draws exactly what was measured.
//
// maxLines exists because auto-shrinking alone produces unreadable six-point
// text when someone pastes a paragraph; past that the text simply gets small
// and the user can see they've overrun.
export function fitCaption(measureAt, cap, W, H, maxLines = 6) {
  const style = STYLES[cap.style] || STYLES.impact;
  const maxWidth = cap.w * W;
  let size = Math.max(8, cap.size * H);
  for (let i = 0; i < 24; i++) {
    const lines = wrapLines((t) => measureAt(t, size, style), cap.text || " ", maxWidth);
    if (lines.length <= maxLines || size <= 10) return { lines, size };
    size *= 0.92;
  }
  return { lines: wrapLines((t) => measureAt(t, size, style), cap.text || " ", maxWidth), size };
}

// The box a caption occupies, in normalized coords — used for hit-testing and
// for drawing the selection outline.
export function captionBox(measureAt, cap, W, H) {
  const style = STYLES[cap.style] || STYLES.impact;
  const { lines, size } = fitCaption(measureAt, cap, W, H);
  const lineHeight = size * 1.12;
  const height = lines.length * lineHeight;
  let widest = 0;
  for (const l of lines) widest = Math.max(widest, measureAt(l, size, style));
  return {
    lines,
    size,
    lineHeight,
    x: cap.x * W - widest / 2,
    y: cap.y * H - height / 2,
    w: widest,
    h: height,
  };
}

// Which caption is under this point? Later captions sit on top, so the search
// runs backwards — otherwise clicking overlapping text always grabs the one
// underneath, which feels broken.
export function captionAt(measureAt, captions, px, py, W, H) {
  for (let i = captions.length - 1; i >= 0; i--) {
    const b = captionBox(measureAt, captions[i], W, H);
    // A little slop so thin text is still easy to grab, especially on touch.
    const pad = b.size * 0.35;
    if (px >= b.x - pad && px <= b.x + b.w + pad && py >= b.y - pad && py <= b.y + b.h + pad) {
      return captions[i];
    }
  }
  return null;
}

function fontFor(style, size) {
  return `${style.weight} ${Math.round(size)}px ${style.family}`;
}

// measurerFor returns a (text, size, style) -> width function bound to a real
// canvas context. The style arrives per call rather than being baked in,
// because a single hit-test walks captions that may each use a different face —
// measuring them all with one font puts the selection box in the wrong place.
export function measurerFor(ctx) {
  return (text, size, style) => {
    ctx.font = fontFor(style || STYLES.impact, size);
    return ctx.measureText(text).width;
  };
}

// drawCaption paints one caption. Stroke first then fill, so the outline sits
// BEHIND the letterform instead of eating into it — stroking after filling is
// the single most common way to make this look wrong.
export function drawCaption(ctx, cap, W, H) {
  const style = STYLES[cap.style] || STYLES.impact;
  const box = captionBox(measurerFor(ctx), cap, W, H);
  const text = (t) => (style.uppercase ? t.toUpperCase() : t);

  ctx.save();
  ctx.textAlign = "center";
  ctx.textBaseline = "top";
  ctx.font = fontFor(style, box.size);
  ctx.lineJoin = "round"; // sharp joins spike on heavy strokes
  ctx.miterLimit = 2;
  if (style.letterSpacing && "letterSpacing" in ctx) {
    ctx.letterSpacing = `${style.letterSpacing * box.size}px`;
  }
  // A soft shadow under everything buys legibility on a busy photo without
  // making the outline any heavier.
  if (style.stroke) {
    ctx.shadowColor = "rgba(0,0,0,0.55)";
    ctx.shadowBlur = box.size * 0.18;
  }
  const cx = cap.x * W;
  box.lines.forEach((line, i) => {
    const y = box.y + i * box.lineHeight;
    const drawn = text(line);
    if (style.stroke) {
      ctx.strokeStyle = style.strokeColor;
      ctx.lineWidth = box.size * style.stroke;
      ctx.strokeText(drawn, cx, y);
    }
    ctx.shadowColor = "transparent";
    ctx.fillStyle = cap.color || style.color;
    ctx.fillText(drawn, cx, y);
    if (style.stroke) ctx.shadowColor = "rgba(0,0,0,0.55)";
  });
  ctx.restore();
  return box;
}

// Render the finished meme. `topBar` adds the white caption bar above the image
// (the "caption" format), growing the canvas rather than covering the picture.
export function drawMeme(ctx, img, captions, W, H, { topBar = 0 } = {}) {
  ctx.save();
  if (topBar > 0) {
    ctx.fillStyle = "#ffffff";
    ctx.fillRect(0, 0, W, topBar);
  }
  ctx.drawImage(img, 0, topBar, W, H - topBar);
  ctx.restore();
  for (const cap of captions) drawCaption(ctx, cap, W, H);
}

// Longest edge of a rendered meme. Attachments are capped at 5 MiB and get
// re-encoded as JPEG below; 1200px is generous for a meme and keeps the encode
// comfortably inside that budget.
export const MAX_RENDER = 1200;

export function renderSize(iw, ih, topBarPct = 0) {
  const scale = Math.min(1, MAX_RENDER / Math.max(iw, ih));
  const W = Math.max(1, Math.round(iw * scale));
  const imgH = Math.max(1, Math.round(ih * scale));
  const topBar = Math.round(imgH * topBarPct);
  return { W, H: imgH + topBar, topBar };
}
