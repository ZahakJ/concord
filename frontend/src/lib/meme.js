// meme.js — the drawing half of the meme editor.
//
// Split from the component so the parts that are really just arithmetic —
// wrapping, auto-shrinking, hit-testing a rotated caption, snapping a drag —
// can be tested without a canvas or a browser. Everything here works in
// normalized coordinates (0..1 of the image), never pixels, so a caption placed
// on a 300px preview lands in the same spot when the same meme is rendered at
// full size for sending.

// The faces the app already ships in public/fonts (OFL, served from our own
// bundle — never a font host). A meme font has to be *there*: naming Impact and
// hoping is how the classic look silently degrades to Helvetica on the Linux
// boxes most of these builds run on.
//
// So `classic` still asks for Impact first — Windows and macOS have it and it
// is the real thing — but every fallback after it is a face we ship, ending at
// Space Grotesk 700, which is the closest bundled silhouette (tall, tight,
// heavy). The fat stroke below is what actually sells it either way.
export const FONTS = {
  classic: {
    label: "Impact",
    family: `Impact, Haettenschweiler, "Franklin Gothic Heavy", "Arial Narrow Bold", "Space Grotesk", sans-serif`,
    weight: 700,
    letterSpacing: 0.01,
  },
  grotesk: { label: "Grotesk", family: `"Space Grotesk", system-ui, sans-serif`, weight: 700 },
  inter: { label: "Inter", family: `"Inter", system-ui, sans-serif`, weight: 700 },
  rounded: { label: "Rounded", family: `"Nunito", system-ui, sans-serif`, weight: 800 },
  comic: { label: "Comic", family: `"Comic Neue", "Comic Sans MS", cursive`, weight: 700 },
  cyber: { label: "Cyber", family: `"Chakra Petch", system-ui, sans-serif`, weight: 700, letterSpacing: 0.02 },
  serif: { label: "Serif", family: `"Source Serif 4", Georgia, serif`, weight: 700 },
  mono: { label: "Mono", family: `"JetBrains Mono", ui-monospace, monospace`, weight: 700 },
};

// A "look" is a one-click bundle of the settings below it — font, colour,
// outline weight, casing. Picking one resets those; the per-caption controls
// then override whatever you disagree with. The keys are also what
// memes/manifest.json names in a template's caption presets, so they are part
// of that file's format and must stay stable.
export const STYLES = {
  impact: {
    label: "Classic",
    font: "classic",
    uppercase: true,
    // Stroke width as a fraction of the font size. The classic look is a fat
    // black outline; too thin and it vanishes on a busy photo.
    stroke: 0.13,
    color: "#ffffff",
    strokeColor: "#000000",
    shadow: true,
  },
  clean: {
    label: "Clean",
    font: "inter",
    uppercase: false,
    stroke: 0.09,
    color: "#ffffff",
    strokeColor: "#000000",
    shadow: true,
  },
  caption: {
    // The "dark text on a white panel" format — the top bar, or the blank half
    // of a Drake. No outline: there is nothing to fight for contrast.
    label: "Caption",
    font: "inter",
    uppercase: false,
    stroke: 0,
    // Plain black, not a near-black: the colour row highlights the swatch that
    // matches, and #111 would leave every caption-look caption showing no
    // colour selected at all.
    color: "#000000",
    strokeColor: "#ffffff",
    shadow: false,
  },
};

// How far outside the text the selection marquee (and therefore the grab area
// and the scale handle) sits, as a fraction of the font size. Shared so the
// handle a user drags is exactly where they see it.
export const SEL_PAD = 0.28;

// Ids are only ever compared, never ordered or persisted anywhere that another
// machine could collide with, so seven random base-36 characters is plenty.
// Captions and layers draw from the same pool on purpose: the editor keeps ONE
// selected id and asks both lists who owns it, which is much harder to get
// wrong than a parallel "and it's a layer this time" flag.
function uid() {
  return Math.random().toString(36).slice(2, 9);
}

// A caption in the editor. x/y are the CENTRE of the box, all values 0..1.
export function newCaption(text = "", over = {}) {
  return {
    id: uid(),
    text,
    x: 0.5,
    y: 0.12,
    w: 0.92, // max width before wrapping, as a fraction of image width
    size: 0.11, // font size as a fraction of image HEIGHT, so it scales sanely
    style: "impact",
    align: "center",
    rot: 0, // degrees, clockwise
    ...over,
  };
}

// resolve merges a caption's own overrides over its look. Every draw and every
// measurement goes through here, so a caption that has been fiddled with
// measures with the font it will actually be painted in — the alternative is a
// selection box in the wrong place for every caption that isn't on defaults.
//
// `??` and not `||` for stroke and uppercase: 0 and false are meaningful
// choices, and `||` would quietly discard both.
export function resolve(cap) {
  const base = STYLES[cap.style] || STYLES.impact;
  const font = FONTS[cap.font] || FONTS[base.font] || FONTS.classic;
  return {
    family: font.family,
    weight: font.weight,
    letterSpacing: font.letterSpacing || 0,
    uppercase: cap.caps ?? base.uppercase,
    stroke: cap.stroke ?? base.stroke,
    color: cap.color || base.color,
    strokeColor: cap.strokeColor || base.strokeColor,
    shadow: base.shadow,
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
  const style = resolve(cap);
  const maxWidth = cap.w * W;
  let size = Math.max(8, cap.size * H);
  for (let i = 0; i < 24; i++) {
    const lines = wrapLines((t) => measureAt(t, size, style), cap.text || " ", maxWidth);
    if (lines.length <= maxLines || size <= 10) return { lines, size };
    size *= 0.92;
  }
  return { lines: wrapLines((t) => measureAt(t, size, style), cap.text || " ", maxWidth), size };
}

// The box a caption occupies. `cx`/`cy` are its centre in pixels and `rot` its
// angle; `x`/`y`/`w`/`h` describe the box BEFORE rotation, i.e. in the
// caption's own frame. Everything that has to cope with a rotated caption
// (hit-testing, the handle) works in that frame and rotates the point into it,
// which is far less error-prone than rotating four corners back out.
export function captionBox(measureAt, cap, W, H) {
  const style = resolve(cap);
  const { lines, size } = fitCaption(measureAt, cap, W, H);
  const lineHeight = size * 1.12;
  const height = lines.length * lineHeight;
  let widest = 0;
  for (const l of lines) widest = Math.max(widest, measureAt(l, size, style));
  const cx = cap.x * W;
  const cy = cap.y * H;
  return {
    lines,
    size,
    lineHeight,
    cx,
    cy,
    rot: cap.rot || 0,
    x: cx - widest / 2,
    y: cy - height / 2,
    w: widest,
    h: height,
  };
}

// Rotate a point about a centre by -deg, i.e. into the frame of something that
// has been rotated by +deg.
export function unrotate(px, py, cx, cy, deg) {
  if (!deg) return { x: px - cx, y: py - cy };
  const r = (-deg * Math.PI) / 180;
  const dx = px - cx;
  const dy = py - cy;
  return { x: dx * Math.cos(r) - dy * Math.sin(r), y: dx * Math.sin(r) + dy * Math.cos(r) };
}

// Which caption is under this point? Later captions sit on top, so the search
// runs backwards — otherwise clicking overlapping text always grabs the one
// underneath, which feels broken.
export function captionAt(measureAt, captions, px, py, W, H) {
  for (let i = captions.length - 1; i >= 0; i--) {
    const b = captionBox(measureAt, captions[i], W, H);
    const p = unrotate(px, py, b.cx, b.cy, b.rot);
    // A little slop so thin text is still easy to grab, especially on touch.
    const pad = b.size * (SEL_PAD + 0.1);
    if (Math.abs(p.x) <= b.w / 2 + pad && Math.abs(p.y) <= b.h / 2 + pad) return captions[i];
  }
  return null;
}

// Where the scale/rotate grip sits: the bottom-right corner of the marquee,
// carried round with the caption as it rotates.
export function handlePos(box) {
  const pad = box.size * SEL_PAD;
  const lx = box.w / 2 + pad;
  const ly = box.h / 2 + pad;
  const r = ((box.rot || 0) * Math.PI) / 180;
  return { x: box.cx + lx * Math.cos(r) - ly * Math.sin(r), y: box.cy + lx * Math.sin(r) + ly * Math.cos(r) };
}

// Dragging that grip scales and rotates at once, the way a sticker does
// everywhere else: the distance from the centre sets the size, the angle sets
// the rotation. `start` is captured on pointer-down — the vector then, and the
// caption's size and angle then — so the maths stays absolute and a slow drag
// can't accumulate drift.
//
export const CAPTION_SCALE = { min: 0.02, max: 0.4 };
// A layer may be flicked down to a stamp or up to three canvas widths — bigger
// than the picture is a legitimate crop, and the clamp in clampLayer is what
// stops an oversized layer from being lost rather than a ceiling on its size.
export const LAYER_SCALE = { min: 0.04, max: 3 };

// The guard on the starting vector matters: grabbing a handle that happens to
// sit on the centre (an empty caption at minimum size) would otherwise divide
// by zero and blow the size up to Infinity.
//
// `limits` exists because the same grip drives image layers, whose "size" is a
// width in canvas widths and wants a completely different range from a font
// size in image heights. Defaulting to the caption range keeps every existing
// caller — and every existing test — reading exactly as it did.
export function scaleRotate(start, px, py, cx, cy, limits = CAPTION_SCALE) {
  const len0 = Math.hypot(start.vx, start.vy);
  if (len0 < 1) return { size: start.size, rot: start.rot };
  const vx = px - cx;
  const vy = py - cy;
  const len = Math.hypot(vx, vy);
  const deg = ((Math.atan2(vy, vx) - Math.atan2(start.vy, start.vx)) * 180) / Math.PI;
  // Wrapped into (-180, 180] so a few full turns don't leave the readout at
  // 900° and the reset button looking like it did nothing.
  let rot = (start.rot + deg) % 360;
  if (rot > 180) rot -= 360;
  if (rot <= -180) rot += 360;
  return { size: Math.min(limits.max, Math.max(limits.min, (start.size * len) / len0)), rot };
}

// Pull a dragged coordinate onto a nearby guide. Returns the value and which
// guide caught it, so the caller can draw the line that explains the jump —
// snapping you can't see reads as the drag being broken.
export function snapTo(v, targets, tol) {
  let best = null;
  let bestD = tol;
  for (const t of targets) {
    const d = Math.abs(v - t);
    if (d <= bestD) {
      bestD = d;
      best = t;
    }
  }
  return best === null ? { v, hit: null } : { v: best, hit: best };
}

// Turning the white top bar on grows the canvas, and every caption's y is a
// fraction of that canvas — so without this they all slide down the picture the
// moment the bar appears. Re-express y so the caption stays put over the IMAGE.
//
// A caption at fraction y of a canvas that is (1+t) image-heights tall sits at
// y*(1+t) - t image-heights down the picture; solving that for the new t is the
// whole function.
export function rebaseTopBar(y, oldT, newT) {
  const overImage = y * (1 + oldT) - oldT;
  return (overImage + newT) / (1 + newT);
}

// The widest a caption centred at x can be without hanging off either edge.
// Dropping a caption near the left of the picture with the default full-width
// box makes it wrap at the image width and then spill past the edge, so the
// first thing a new caption does is look broken.
export function fitWidthAt(x, max = 0.92) {
  return Math.max(0.1, Math.min(max, 2 * Math.min(x, 1 - x)));
}

// Centre of the top bar, in canvas fractions — where a caption meant for the
// bar wants to be.
export function topBarCentre(t) {
  return t <= 0 ? 0 : t / 2 / (1 + t);
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
    ctx.font = fontFor(style || resolve(newCaption()), size);
    return ctx.measureText(text).width;
  };
}

// drawCaption paints one caption. Stroke first then fill, so the outline sits
// BEHIND the letterform instead of eating into it — stroking after filling is
// the single most common way to make this look wrong.
export function drawCaption(ctx, ofCap, W, H, { text: override = null, alpha = 1 } = {}) {
  const cap = override === null ? ofCap : { ...ofCap, text: override };
  const style = resolve(cap);
  const box = captionBox(measurerFor(ctx), cap, W, H);
  const cased = (t) => (style.uppercase ? t.toUpperCase() : t);

  ctx.save();
  ctx.globalAlpha = alpha;
  // Draw in the caption's own frame: translate to its centre and rotate once,
  // then every line is placed with plain unrotated arithmetic.
  ctx.translate(box.cx, box.cy);
  if (box.rot) ctx.rotate((box.rot * Math.PI) / 180);
  ctx.textAlign = cap.align || "center";
  ctx.textBaseline = "top";
  ctx.font = fontFor(style, box.size);
  ctx.lineJoin = "round"; // sharp joins spike on heavy strokes
  ctx.miterLimit = 2;
  if (style.letterSpacing && "letterSpacing" in ctx) {
    ctx.letterSpacing = `${style.letterSpacing * box.size}px`;
  }
  // A soft shadow under everything buys legibility on a busy photo without
  // making the outline any heavier.
  const shadow = style.shadow && alpha === 1 ? "rgba(0,0,0,0.55)" : "transparent";
  if (style.stroke) {
    ctx.shadowColor = shadow;
    ctx.shadowBlur = box.size * 0.18;
  }
  // Lines are aligned against each other, not against the image: a left-aligned
  // caption reads as a block whose left edge is the widest line's left edge.
  const ax = { left: -box.w / 2, right: box.w / 2, center: 0 }[cap.align || "center"] ?? 0;
  box.lines.forEach((line, i) => {
    const y = -box.h / 2 + i * box.lineHeight;
    const drawn = cased(line);
    if (style.stroke) {
      ctx.strokeStyle = style.strokeColor;
      ctx.lineWidth = box.size * style.stroke;
      ctx.strokeText(drawn, ax, y);
    }
    ctx.shadowColor = "transparent";
    ctx.fillStyle = style.color;
    ctx.fillText(drawn, ax, y);
    if (style.stroke) ctx.shadowColor = shadow;
  });
  ctx.restore();
  return box;
}

// ---- image layers -------------------------------------------------------
//
// A layer is a picture stuck ON TOP of the base image — the pasted face in a
// Drake panel, the two photos held up in "They're the same picture". It is not
// the base: pasting used to call load(), which threw the template away and made
// the pasted picture the new background, so a template whose panels are meant
// to hold PICTURES could only ever hold words.
//
// Layers are deliberately dumb JSON. `asset` is a short key into whatever the
// caller uses to hold the decoded images, NOT the data URL itself: the undo
// stack snapshots this object sixty times over, and sixty inlined megabyte data
// URLs is not an undo stack, it is a leak.
//
// Size is a single number — `w`, the width as a fraction of the CANVAS width —
// and the height comes back out of the natural pixel size in `iw`/`ih`. One
// scalar plus the true aspect is what makes "aspect ratio preserved on scale"
// a property of the model rather than something every caller has to remember.
export function newLayer(asset, iw, ih, over = {}) {
  return {
    id: uid(),
    asset,
    iw: Math.max(1, iw || 1),
    ih: Math.max(1, ih || 1),
    x: 0.5, // centre, as a fraction of the canvas
    y: 0.5,
    w: 0.4,
    rot: 0, // degrees, clockwise
    ...over,
  };
}

// The box a layer occupies, in pixels, in the same shape captionBox returns —
// so handlePos, unrotate and the marquee code all work on it unchanged.
export function layerBox(lay, W, H) {
  const w = Math.max(1, lay.w * W);
  const h = (w * lay.ih) / lay.iw;
  return {
    cx: lay.x * W,
    cy: lay.y * H,
    w,
    h,
    rot: lay.rot || 0,
    // handlePos and the marquee space themselves off `size`, which for a
    // caption is its font size. A layer has no type in it, so it borrows a
    // fraction of its shorter side: the grip then sits a constant distance
    // outside the picture instead of drifting inside a tall thin one.
    size: Math.max(4, Math.min(w, h) * 0.09),
  };
}

// Which layer is under this point? Backwards, so the one drawn last — the one
// visibly on top — is the one you grab. Forwards would hand you the buried
// layer every single time two overlap, which is most of the time.
export function layerAt(layers, px, py, W, H) {
  for (let i = layers.length - 1; i >= 0; i--) {
    const b = layerBox(layers[i], W, H);
    const p = unrotate(px, py, b.cx, b.cy, b.rot);
    if (Math.abs(p.x) <= b.w / 2 && Math.abs(p.y) <= b.h / 2) return layers[i];
  }
  return null;
}

// Half-width and half-height of the axis-aligned box that contains a w×h box
// turned by `rot`. Rotating a rectangle makes its footprint grow, and clamping
// against the unrotated size lets a turned layer slide further off the canvas
// than intended.
export function aabbHalf(w, h, rot) {
  const r = ((rot || 0) * Math.PI) / 180;
  const c = Math.abs(Math.cos(r));
  const s = Math.abs(Math.sin(r));
  return { x: (w * c + h * s) / 2, y: (w * s + h * c) / 2 };
}

// Keep a dragged layer reachable. A caption is a point and simply gets pinned
// inside the picture, but a layer has extent: pinning its CENTRE would forbid
// the thing people actually want, a face peeking in from the edge. So the rule
// is about the picture, not the centre — some minimum sliver of the layer must
// stay on the canvas, and past that it may hang as far off as it likes.
//
// Without this a layer scaled up past the canvas can be dragged until neither
// it nor its grip is on screen, at which point it cannot be selected, moved or
// deleted, and undo is the only way back.
export const LAYER_KEEP = 0.06; // of the canvas, per axis

export function clampLayer(lay, W, H) {
  const b = layerBox(lay, W, H);
  const half = aabbHalf(b.w, b.h, b.rot);
  // Never demand more than the layer has: a sticker smaller than the margin
  // would otherwise be forbidden from going anywhere near an edge.
  const keepX = Math.min(half.x, W * LAYER_KEEP);
  const keepY = Math.min(half.y, H * LAYER_KEEP);
  const px = Math.min(W + half.x - keepX, Math.max(keepX - half.x, lay.x * W));
  const py = Math.min(H + half.y - keepY, Math.max(keepY - half.y, lay.y * H));
  return { x: px / W, y: py / H };
}

// Reorder: array order IS z-order, last on top. Returns a new array so the
// caller's reactivity fires; returns the SAME array when nothing moved, so a
// "bring forward" on the top layer doesn't register as an edit.
export function moveLayer(layers, id, delta) {
  const i = layers.findIndex((l) => l.id === id);
  if (i < 0) return layers;
  const j = Math.min(layers.length - 1, Math.max(0, i + delta));
  if (i === j) return layers;
  const out = [...layers];
  out.splice(j, 0, out.splice(i, 1)[0]);
  return out;
}

// ---- template image slots -----------------------------------------------
//
// A slot is a rectangle a template declares in manifest.json to say "a PICTURE
// goes here": the two blank sheets in "They're the same picture", the empty
// half of a blank Drake. Empty slots are ghosted onto the preview so the panels
// read as droppable, and a pasted picture lands in the first free one already
// the right size — which is the difference between the feature working and the
// user having to hand-scale two photos onto two tilted bits of paper.
//
// Slots are canvas fractions, like captions, and are template data only: they
// are never part of the sent picture.
export function slotBox(slot, W, H) {
  return { cx: slot.x * W, cy: slot.y * H, w: slot.w * W, h: slot.h * H, rot: slot.rot || 0 };
}

// A slot is taken when some layer's centre sits in it. Deliberately geometric
// rather than a remembered assignment: drag the picture out of the panel and
// the panel goes back to asking for one, which is what it looks like.
export function slotFilled(slot, layers, W, H) {
  const b = slotBox(slot, W, H);
  return layers.some((l) => {
    const p = unrotate(l.x * W, l.y * H, b.cx, b.cy, b.rot);
    return Math.abs(p.x) <= b.w / 2 && Math.abs(p.y) <= b.h / 2;
  });
}

export function nextSlot(slots, layers, W, H) {
  return (slots || []).find((s) => !slotFilled(s, layers, W, H)) || null;
}

// Where a picture of iw×ih should sit to land in `slot`. Contain, not cover:
// the layer is drawn whole, so covering would spill the picture past the very
// panel it was aimed at — the opposite of helpful. A margin of white inside the
// panel is the honest result and reads as intentional.
export function fitToSlot(slot, iw, ih, W, H) {
  const sw = slot.w * W;
  const sh = slot.h * H;
  const w = Math.min(sw, (sh * iw) / ih);
  return { x: slot.x, y: slot.y, w: w / W, rot: slot.rot || 0 };
}

// ---- drawing ------------------------------------------------------------

// Paint the layers. `imageFor` resolves a layer's asset key to a decoded
// element; returning null is normal and means "still loading", so the layer is
// skipped and the redraw that fires when it lands paints it.
export function drawLayers(ctx, layers, imageFor, W, H) {
  for (const lay of layers) {
    const el = imageFor && imageFor(lay);
    if (!el) continue;
    const b = layerBox(lay, W, H);
    ctx.save();
    ctx.translate(b.cx, b.cy);
    if (b.rot) ctx.rotate((b.rot * Math.PI) / 180);
    // Drawn about its own centre in its own rotated frame — the same trick
    // drawCaption uses, and the reason hit-testing can be a plain unrotate.
    ctx.drawImage(el, -b.w / 2, -b.h / 2, b.w, b.h);
    ctx.restore();
  }
}

// Ghost the slots nobody has filled yet. Editor furniture: see the guard in
// drawMeme, which requires the editor-only placeholder before this runs at all.
export function drawSlots(ctx, slots, layers, W, H) {
  for (const s of slots) {
    if (slotFilled(s, layers, W, H)) continue;
    const b = slotBox(s, W, H);
    const unit = Math.max(2, W * 0.005);
    ctx.save();
    ctx.translate(b.cx, b.cy);
    if (b.rot) ctx.rotate((b.rot * Math.PI) / 180);
    ctx.fillStyle = "rgba(88,166,255,0.14)";
    ctx.fillRect(-b.w / 2, -b.h / 2, b.w, b.h);
    ctx.strokeStyle = "rgba(88,166,255,0.9)";
    ctx.lineWidth = unit;
    ctx.setLineDash([unit * 3, unit * 2.2]);
    ctx.strokeRect(-b.w / 2, -b.h / 2, b.w, b.h);
    ctx.setLineDash([]);
    const label = s.label || "Paste a picture";
    // Start from the height and shrink to the width. A slot can be any shape,
    // and a label that overflows its own panel is worse than no label.
    let fs = Math.max(8, Math.min(b.h * 0.14, W * 0.04));
    ctx.font = `600 ${Math.round(fs)}px system-ui, sans-serif`;
    const room = b.w * 0.82;
    const wide = ctx.measureText(label).width;
    if (wide > room) {
      fs = Math.max(7, (fs * room) / wide);
      ctx.font = `600 ${Math.round(fs)}px system-ui, sans-serif`;
    }
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    ctx.fillStyle = "rgba(24,64,120,0.92)";
    ctx.fillText(label, 0, 0);
    ctx.restore();
  }
}

// Render the finished meme. `topBar` adds the white caption bar above the image
// (the "caption" format), growing the canvas rather than covering the picture.
//
// `placeholder` is editor-only: an empty caption is invisible, so a template's
// carefully-placed boxes would look like they hadn't loaded at all. Passing a
// word here ghosts it into every empty caption so you can see where they are.
// The send path passes nothing, so the ghost can never reach the picture.
//
// Order is the whole point of the layer feature: base, then the pasted
// pictures, then the captions. Text under a pasted photo is text nobody can
// read, and a photo under the base is a photo nobody can see.
export function drawMeme(
  ctx,
  img,
  captions,
  W,
  H,
  { topBar = 0, placeholder = "", layers = [], imageFor = null, slots = [] } = {},
) {
  ctx.save();
  if (topBar > 0) {
    ctx.fillStyle = "#ffffff";
    ctx.fillRect(0, 0, W, topBar);
  }
  ctx.drawImage(img, 0, topBar, W, H - topBar);
  ctx.restore();
  drawLayers(ctx, layers, imageFor, W, H);
  // Two signals, not one, before any placeholder is painted: the send path
  // passes neither, so no future caller can accidentally bake a dashed "paste a
  // picture" box into a real meme by remembering only one of them.
  if (placeholder && slots.length) drawSlots(ctx, slots, layers, W, H);
  for (const cap of captions) {
    if (cap.text) drawCaption(ctx, cap, W, H);
    else if (placeholder) drawCaption(ctx, cap, W, H, { text: placeholder, alpha: 0.32 });
  }
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

// ---- sessions -----------------------------------------------------------
//
// Everything the editor holds, flat and JSON-safe, so a sent meme can be
// reopened and edited rather than rebuilt from memory. `assets` is where the
// data URLs live — layers carry only the key into it, which is what keeps the
// undo stack cheap, so a session is the only place the two are put back
// together.
export const SESSION_V = 1;

export function newSession(over = {}) {
  return {
    v: SESSION_V,
    template: "", // manifest `file`, or "" for a bring-your-own base
    label: "",
    base: "", // the base image URL — a /memes path, or a data URL
    topBar: 0,
    captions: [],
    layers: [],
    slots: [],
    assets: {}, // asset key -> data URL
    ...over,
  };
}

// Drop assets no layer refers to any more. Undo can resurrect a deleted layer,
// so this is for saving a session, never for pruning while one is open.
export function usedAssets(layers, assets) {
  const keep = {};
  for (const l of layers) if (assets[l.asset] !== undefined) keep[l.asset] = assets[l.asset];
  return keep;
}

// Free-text search over the bundled pack. Matches the label and the keywords a
// manifest entry carries, so "yes no" finds Drake — nobody remembers that the
// template with the two panels is called that.
export function searchTemplates(list, q) {
  const terms = String(q || "")
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean);
  if (!terms.length) return list;
  return list.filter((t) => {
    const hay = `${t.label || ""} ${(t.tags || []).join(" ")}`.toLowerCase();
    return terms.every((term) => hay.includes(term));
  });
}
