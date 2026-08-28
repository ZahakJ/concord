<script>
  // The meme editor. Opened by /meme, by the + menu, or by "Make a meme" on any
  // image already in the conversation.
  //
  // Everything is local: templates are bundled or come from this channel, the
  // render happens in a canvas here, and the result is sent as an ordinary
  // encrypted attachment. Nothing is uploaded anywhere to be composited.
  //
  // Two screens, not one: a template gallery you can search, and the editor
  // itself. Cramming the gallery into a sidebar next to the canvas was the old
  // shape and it gave the templates a strip four thumbnails wide with no names,
  // which is unusable — the picture IS the name of a meme template only if you
  // can see it.
  //
  // The layout maths (wrapping, auto-shrink, hit-testing, snapping, the
  // scale/rotate grip) lives in lib/meme.js so it can be tested without a
  // browser; this file is interaction only.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import {
    drawMeme,
    newCaption,
    captionAt,
    captionBox,
    handlePos,
    scaleRotate,
    snapTo,
    measurerFor,
    rebaseTopBar,
    topBarCentre,
    fitWidthAt,
    searchTemplates,
    resolve,
    newLayer,
    layerBox,
    layerAt,
    clampLayer,
    moveLayer,
    nextSlot,
    fitToSlot,
    LAYER_SCALE,
    STYLES,
    FONTS,
    SEL_PAD,
    renderSize,
    newSession,
    usedAssets,
  } from "../lib/meme.js";
  import { saveRecipe, loadRecipe, dropRecipe } from "../lib/memerecipe.js";
  import { rangefill } from "../lib/rangefill.js";

  // `edit` turns this from "compose a meme" into "reopen the one already in the
  // channel": { channelId, messageId, blobId }. Saving then re-renders and
  // EDITS that message rather than posting a second one, which is the whole
  // point — a meme you fixed a typo in should be one meme, not two.
  let { onClose, src = "", edit = null } = $props();

  let img = $state(null); // HTMLImageElement, once decoded
  let captions = $state([]);
  // Pictures stuck on top of the base: the two photos in "They're the same
  // picture", a pasted face in a Drake panel. Drawn between the base and the
  // captions, and — unlike the base — added without destroying anything.
  let layers = $state([]);
  let slots = $state([]); // the current template's declared picture panels
  let selId = $state("");
  let topBar = $state(0); // white caption strip above the picture, 0..1 of image height
  let sending = $state(false);
  let canvas = $state(null);
  // Two inputs, because the two answers differ: the gallery card means "make
  // this the background", the editor's Add means "put this on top".
  let fileInput;
  let layerInput;
  let textBox = $state(null);
  let tplName = $state("");
  // The two things a recipe needs to rebuild the BASE picture, kept as load()
  // sets them. `tplFile` is the manifest key and `baseSrc` the url it was
  // decoded from — a "/memes/..." path for a bundled template (a few dozen
  // bytes to store) or a data URL for a bring-your-own image (megabytes, and
  // the reason the recipe store has a budget at all).
  let tplFile = $state("");
  let baseSrc = $state("");
  let query = $state("");
  let dropping = $state(false);
  // Which screen: the gallery until something is loaded, and again whenever
  // "Change" is pressed. Reopening a sent meme goes straight to the editor.
  let browsing = $state(!src && !edit);

  // One selected id, asked of both lists. Ids come from the same random pool,
  // so at most one of these is ever non-null, and every control below stays
  // gated on the plain `sel`/`selLay` it already used.
  const sel = $derived(captions.find((c) => c.id === selId) || null);
  const selLay = $derived(layers.find((l) => l.id === selId) || null);
  const dims = $derived(img ? renderSize(img.naturalWidth, img.naturalHeight, topBar) : null);
  const TOP_BAR_H = 0.22; // bar height as a fraction of the image, when on

  // Decoded pictures for the layers, keyed by the short asset key a layer
  // carries. Kept out of the layer objects on purpose: the undo stack
  // JSON-stringifies the model on every edit, and a data URL in there would be
  // copied sixty times over. Never pruned while the editor is open, because
  // undo can bring a deleted layer back and it must still have its picture.
  const assets = new Map(); // key -> { src, el }
  let assetN = 0;
  const imageFor = (lay) => assets.get(lay.asset)?.el || null;

  // ---- undo -----------------------------------------------------------
  // A plain snapshot stack. Deep-cloned through JSON because captions are flat
  // data by design — anything that can't survive that round trip has no place
  // in the model.
  let past = [];
  let lastTag = "";
  let lastAt = 0;
  let canUndo = $state(false);

  // Every snapshot is taken BEFORE the edit lands, which is why the controls
  // below assign through `set()` instead of `bind:` — a two-way binding writes
  // first and would leave undo one step behind for its whole life.
  //
  // `tag` coalesces a run of the same kind of edit: typing a word is one undo
  // step, not eleven, and dragging a slider is one, not forty. A different tag,
  // or a pause, starts a new step.
  function snap(tag = "") {
    const now = Date.now();
    if (tag && tag === lastTag && now - lastAt < 900) {
      lastAt = now;
      return;
    }
    lastTag = tag;
    lastAt = now;
    // Layers ride along, so adding, moving, scaling or deleting a pasted
    // picture is as undoable as anything else. This is cheap only because a
    // layer carries an asset KEY and not the data URL behind it.
    past.push(JSON.stringify({ captions, topBar, layers }));
    if (past.length > 60) past.shift();
    canUndo = true;
  }

  // set(tag, key, value) — snapshot, then write the field on the selection.
  function set(tag, key, value) {
    if (!sel) return;
    snap(tag);
    sel[key] = value;
  }

  function setLay(tag, key, value) {
    if (!selLay) return;
    snap(tag);
    selLay[key] = value;
  }

  function undo() {
    const prev = past.pop();
    if (!prev) return;
    const s = JSON.parse(prev);
    captions = s.captions;
    topBar = s.topBar;
    layers = s.layers || [];
    if (!captions.some((c) => c.id === selId) && !layers.some((l) => l.id === selId)) {
      selId = captions.at(-1)?.id || "";
    }
    lastTag = "";
    canUndo = past.length > 0;
  }

  // ---- templates -------------------------------------------------------
  // The bundled pack is optional: if public/memes/manifest.json isn't present
  // the gallery simply shows the bring-your-own card, so the editor still works
  // in a build without the assets.
  let templates = $state([]);
  let manifestDone = $state(false);
  (async () => {
    try {
      const r = await fetch("/memes/manifest.json");
      if (r.ok) templates = await r.json();
    } catch {
      /* no bundled pack in this build */
    }
    manifestDone = true;
  })();

  const shown = $derived(searchTemplates(templates, query));

  // Decode a URL into an element, or throw. Shared by the base image and by
  // every pasted layer, which needs the natural size before it can be placed.
  function decode(url) {
    const el = new Image();
    el.crossOrigin = "anonymous";
    return new Promise((res, rej) => {
      el.onload = () => res(el);
      el.onerror = () => rej(new Error("decode"));
      el.src = url;
    });
  }

  // tpl is a manifest entry ({ file, label, topBar?, captions?, slots? }) or
  // null for a bring-your-own image.
  //
  // This is the one path that legitimately throws the document away — you asked
  // for a different picture. Pasting must NOT come through here: it used to,
  // which is how a stray Ctrl+V replaced the template and every caption on it
  // with no way back. See addImage.
  async function load(url, tpl = null) {
    try {
      const el = await decode(url);
      img = el;
      // A template can ship its own caption boxes — picking "Drake" should put
      // two correctly-placed captions on screen, not make you position them.
      // A template can ship its own caption boxes. `captions: []` is a real
      // answer, not a missing one: a template whose panels want PICTURES (see
      // slots below) should open with slot ghosts alone, not with two
      // full-width "Your text" ghosts sprawled across them as well.
      captions = tpl?.captions
        ? tpl.captions.map((p) => newCaption("", p))
        : [newCaption("", { y: 0.11 }), newCaption("", { y: 0.89 })];
      selId = captions[0]?.id || "";
      topBar = tpl?.topBar || 0;
      // A template may also declare picture SLOTS: blank panels that want a
      // photo rather than words. They ghost onto the preview while empty and
      // catch the next paste at the right size and angle.
      slots = tpl?.slots || [];
      layers = [];
      tplName = tpl?.label || "Your image";
      tplFile = tpl?.file || "";
      baseSrc = url;
      browsing = false;
      past = [];
      canUndo = false;
      queueMicrotask(() => textBox?.focus());
    } catch {
      flash("Couldn't open that image", "error");
      // Land somewhere usable rather than on an editor with no picture in it —
      // this is the path a broken `src` from "Make a meme" takes.
      if (!img) browsing = true;
    }
  }

  // ---- reopening a sent meme -------------------------------------------
  //
  // The picture that went out is a flattened JPEG — the captions are IN the
  // pixels. So editing works off the recipe saved beside it at send time, and
  // rebuilding the editor from one is exactly: load the base as a template,
  // then put the captions, the layer pictures and their geometry back.
  async function restore(rec) {
    await load(rec.base, {
      file: rec.template,
      label: rec.label,
      topBar: rec.topBar,
      captions: rec.captions,
      slots: rec.slots,
    });
    if (!img) return; // load() already complained and dropped us in the gallery
    // Assigned outright rather than left to load()'s fallback: that fallback
    // invents two empty captions when a template declares none, and a meme
    // whose words are all in its pasted pictures would come back with two
    // "Your text" ghosts it never had.
    captions = (rec.captions || []).map((c) => ({ ...c }));
    // Decode every layer picture into the asset map BEFORE the layers land, so
    // the first redraw already has something to draw. Layers reference assets
    // by key, so nothing in them needs rewriting.
    let highest = 0;
    for (const [key, url] of Object.entries(rec.assets || {})) {
      const n = /^a(\d+)$/.exec(key);
      if (n) highest = Math.max(highest, +n[1]);
      try {
        assets.set(key, { src: url, el: await decode(url) });
      } catch {
        // One unreadable layer shouldn't cost you the rest of the meme; it
        // simply doesn't draw, and imageFor() already treats that as normal.
      }
    }
    // New pastes mint `a{n}` keys from this counter. Without this a paste
    // would reuse a restored key and silently swap that layer's picture.
    assetN = Math.max(assetN, highest);
    layers = (rec.layers || []).map((l) => ({ ...l }));
    selId = captions[0]?.id || layers.at(-1)?.id || "";
    // Restoring is not an edit, so there is nothing behind it to undo.
    past = [];
    canUndo = false;
  }

  async function reopen() {
    const rec = await loadRecipe(edit.blobId);
    if (!rec) {
      // The menu only offers "Edit meme" when a recipe is on this device, so
      // arriving here means it aged out between the click and now. Say so —
      // opening a blank editor that then posts a SECOND meme is the dishonest
      // version of this.
      flash("That meme's recipe is no longer on this device", "error");
      browsing = true;
      return;
    }
    await restore(rec);
  }

  if (edit) reopen();
  else if (src) load(src);

  function readFile(file) {
    return new Promise((res, rej) => {
      const r = new FileReader();
      r.onload = () => res(String(r.result));
      r.onerror = () => rej(new Error("read"));
      r.readAsDataURL(file);
    });
  }

  // The gallery's "Your own image" card: this one really does mean "make this
  // the background", so it goes through load().
  async function pickBase(file) {
    if (!file || !file.type.startsWith("image/")) return;
    try {
      await load(await readFile(file));
    } catch {
      flash("Couldn't open that image", "error");
    }
  }

  // Paste or drop a picture. With no picture open yet the paste IS the meme, so
  // it becomes the base; with a template already loaded it goes ON TOP as a
  // layer, because a template whose panels are meant to hold pictures is
  // exactly the case the old "replace everything" behaviour made impossible.
  async function addImage(file) {
    if (!file || !file.type.startsWith("image/")) return;
    let src2;
    try {
      src2 = await readFile(file);
    } catch {
      flash("Couldn't read that image", "error");
      return;
    }
    if (!img) {
      load(src2);
      return;
    }
    let el;
    try {
      el = await decode(src2);
    } catch {
      flash("Couldn't open that image", "error");
      return;
    }
    // snap(), not `past = []`. This is the whole bug: adding a picture is an
    // edit like any other and must be undoable, not a reset.
    snap("layer");
    const key = `a${++assetN}`;
    assets.set(key, { src: src2, el });
    const iw = el.naturalWidth;
    const ih = el.naturalHeight;
    // Land in the template's next free panel if it declared any. Otherwise sit
    // in the middle at a size that reads as "a picture on top", not as a
    // replacement for the picture underneath.
    const slot = nextSlot(slots, layers, dims.W, dims.H);
    const place = fitToSlot(slot || { x: 0.5, y: 0.5, w: 0.55, h: 0.55 }, iw, ih, dims.W, dims.H);
    const lay = newLayer(key, iw, ih, place);
    layers = [...layers, lay];
    selId = lay.id;
    browsing = false; // a paste from the gallery screen should show its result
  }

  function onPaste(e) {
    const item = [...(e.clipboardData?.items || [])].find((i) => i.type.startsWith("image/"));
    if (!item) return;
    e.preventDefault();
    addImage(item.getAsFile());
  }

  // ---- drawing ---------------------------------------------------------
  // Canvas silently substitutes a fallback for a face the document hasn't
  // finished loading, so the first render of a meme can come out in the wrong
  // font with no error anywhere. Redraw once the bundled woff2s have landed.
  let fontsReady = $state(false);
  document.fonts?.ready.then(() => (fontsReady = true));

  // Redraws whenever anything visible changes. The canvas backing store is the
  // real render size and CSS scales it down, so the preview is pixel-exact:
  // what you position is literally what gets sent, not an approximation of it.
  $effect(() => {
    if (!canvas || !img || !dims) return;
    // Touch the reactive state this depends on so Svelte re-runs the effect.
    void JSON.stringify(captions);
    // Layers by fingerprint rather than JSON.stringify: they are small, but
    // stringifying them on every pointer-move is pointless work and the shape
    // that would quietly become expensive if a data URL ever moved in.
    void layers.map((l) => `${l.id}:${l.x}:${l.y}:${l.w}:${l.rot}`).join("|");
    void selId;
    void guides.x;
    void guides.y;
    void fontsReady;
    canvas.width = dims.W;
    canvas.height = dims.H;
    const ctx = canvas.getContext("2d");
    ctx.clearRect(0, 0, dims.W, dims.H);
    // The ghost is editor-only. A template's boxes are its whole value and an
    // empty caption draws nothing at all, so without this a freshly-picked
    // Drake looks identical to a blank one.
    drawMeme(ctx, img, captions, dims.W, dims.H, {
      topBar: dims.topBar,
      placeholder: "Your text",
      layers,
      imageFor,
      slots,
    });
    if (guides.x !== null || guides.y !== null) drawGuides(ctx);
    // Whichever is selected — a caption or a layer — gets the same marquee and
    // the same grip, because they are the same interaction.
    if (sel) outline(ctx, boxOf(sel));
    else if (selLay) outline(ctx, layerBox(selLay, dims.W, dims.H));
  });

  // The selection marquee is drawn on the same canvas rather than as a DOM
  // overlay, so it can never drift out of alignment with the thing it marks.
  // Takes a box, not a caption: a layer's box comes from layerBox and is
  // deliberately the same shape.
  function outline(ctx, b) {
    const pad = b.size * SEL_PAD;
    const unit = Math.max(2, dims.W * 0.004);
    ctx.save();
    ctx.translate(b.cx, b.cy);
    if (b.rot) ctx.rotate((b.rot * Math.PI) / 180);
    ctx.strokeStyle = "rgba(88,166,255,0.95)";
    ctx.lineWidth = unit;
    ctx.setLineDash([dims.W * 0.02, dims.W * 0.014]);
    ctx.strokeRect(-b.w / 2 - pad, -b.h / 2 - pad, b.w + pad * 2, b.h + pad * 2);
    // The grip. Drawn in the same rotated frame as the marquee it hangs off, so
    // it stays on the corner the user can see rather than sliding away as the
    // caption turns.
    ctx.setLineDash([]);
    ctx.beginPath();
    ctx.arc(b.w / 2 + pad, b.h / 2 + pad, unit * 3.2, 0, Math.PI * 2);
    ctx.fillStyle = "rgba(88,166,255,0.95)";
    ctx.fill();
    ctx.strokeStyle = "#fff";
    ctx.lineWidth = unit * 0.9;
    ctx.stroke();
    ctx.restore();
  }

  // Guides only exist while a drag is snapped to one, and they are drawn on the
  // preview canvas only — never on the export.
  let guides = $state({ x: null, y: null });
  function drawGuides(ctx) {
    ctx.save();
    ctx.strokeStyle = "rgba(255,213,79,0.9)";
    ctx.lineWidth = Math.max(1.5, dims.W * 0.003);
    ctx.setLineDash([dims.W * 0.015, dims.W * 0.015]);
    if (guides.x !== null) {
      ctx.beginPath();
      ctx.moveTo(guides.x * dims.W, 0);
      ctx.lineTo(guides.x * dims.W, dims.H);
      ctx.stroke();
    }
    if (guides.y !== null) {
      ctx.beginPath();
      ctx.moveTo(0, guides.y * dims.H);
      ctx.lineTo(dims.W, guides.y * dims.H);
      ctx.stroke();
    }
    ctx.restore();
  }

  // ---- pointer: select, drag, scale, add -------------------------------
  let drag = null;
  const SNAP_X = [0.25, 0.5, 0.75];
  const SNAP_Y = [0.12, 0.25, 0.5, 0.75, 0.88];

  function toImage(e) {
    const r = canvas.getBoundingClientRect();
    return {
      x: ((e.clientX - r.left) / r.width) * dims.W,
      y: ((e.clientY - r.top) / r.height) * dims.H,
    };
  }

  // The grip radius is measured in IMAGE pixels, but what the finger has to hit
  // is CSS pixels: an 800px-wide template drawn into ~350px of phone sheet
  // shrank a 28px image-space radius to ~12px on screen. Convert a 24px CSS
  // floor back into image space so the grip is grabbable at any display scale.
  function gripRadius() {
    const w = canvas?.getBoundingClientRect().width || dims.W;
    return Math.max(18, dims.W * 0.035, (24 * dims.W) / w);
  }

  function boxOf(cap) {
    const ctx = canvas.getContext("2d");
    // measurerFor, not a hand-rolled one pinned to a single face: captions can
    // each use a different font, and measuring them all in one puts the grab
    // area in the wrong place for every caption that isn't using it.
    return captionBox(measurerFor(ctx), cap.text ? cap : { ...cap, text: "Your text" }, dims.W, dims.H);
  }

  function onDown(e) {
    if (!img) return;
    const p = toImage(e);
    // The grip belongs to whatever is selected and sits OUTSIDE its box, so it
    // has to be tested before anything else or the thing behind it wins.
    const active = sel || selLay;
    if (active) {
      const isLay = !sel;
      const b = isLay ? layerBox(selLay, dims.W, dims.H) : boxOf(sel);
      const h = handlePos(b);
      if (Math.hypot(p.x - h.x, p.y - h.y) <= gripRadius()) {
        snap("scale");
        drag = {
          kind: "scale",
          id: active.id,
          layer: isLay,
          cx: b.cx,
          cy: b.cy,
          vx: h.x - b.cx,
          vy: h.y - b.cy,
          // A caption scales its font size; a layer scales its width. Same
          // grip, same maths — see scaleRotate's `limits`.
          size: isLay ? active.w : active.size,
          rot: active.rot || 0,
        };
        canvas.setPointerCapture(e.pointerId);
        return;
      }
    }
    const ctx = canvas.getContext("2d");
    const ghosted = captions.map((c) => (c.text ? c : { ...c, text: "Your text" }));
    const hitGhost = captionAt(measurerFor(ctx), ghosted, p.x, p.y, dims.W, dims.H);
    const hit = hitGhost && captions.find((c) => c.id === hitGhost.id);
    // Captions before layers, because captions are painted OVER the layers.
    // Testing layers first would make any text sitting on a pasted picture
    // impossible to grab, which is most text on a picture meme.
    const target = hit || layerAt(layers, p.x, p.y, dims.W, dims.H);
    if (!target) {
      selId = "";
      return;
    }
    selId = target.id;
    snap("move");
    drag = { kind: "move", id: target.id, layer: !hit, dx: target.x * dims.W - p.x, dy: target.y * dims.H - p.y };
    canvas.setPointerCapture(e.pointerId);
  }

  function onMove(e) {
    if (!drag) return;
    e.preventDefault();
    const p = toImage(e);
    const c = (drag.layer ? layers : captions).find((x) => x.id === drag.id);
    if (!c) return;
    if (drag.kind === "scale") {
      const r = scaleRotate(drag, p.x, p.y, drag.cx, drag.cy, drag.layer ? LAYER_SCALE : undefined);
      // A layer's height is never written: it comes back out of the picture's
      // own natural size, which is what keeps the aspect ratio exact however
      // far the grip is flung.
      if (drag.layer) c.w = r.size;
      else c.size = r.size;
      // Held straight, snap the angle back to level: nobody wants 0.4°.
      c.rot = Math.abs(r.rot) < 3 ? 0 : Math.round(r.rot);
      return;
    }
    // Pulled onto the centre/third lines when it comes close — with a guide
    // drawn, so the jump reads as help rather than as the drag misbehaving.
    const sx = snapTo((p.x + drag.dx) / dims.W, SNAP_X, 0.012);
    const sy = snapTo((p.y + drag.dy) / dims.H, SNAP_Y, 0.012);
    if (drag.layer) {
      // A layer has extent, so it clamps against its own footprint rather than
      // its centre: a face may hang off the edge, but never so far that nothing
      // is left to grab. See clampLayer.
      const cl = clampLayer({ ...c, x: sx.v, y: sy.v }, dims.W, dims.H);
      c.x = cl.x;
      c.y = cl.y;
    } else {
      // A caption is a point: its centre simply may not leave the picture.
      c.x = Math.min(1, Math.max(0, sx.v));
      c.y = Math.min(1, Math.max(0, sy.v));
    }
    guides = { x: sx.hit, y: sy.hit };
  }

  function onUp() {
    drag = null;
    guides = { x: null, y: null };
  }

  // Double-click on bare image drops a caption right where you clicked, which
  // is what people try before they find the Add button.
  function onDouble(e) {
    if (!img) return;
    const p = toImage(e);
    const ctx = canvas.getContext("2d");
    const ghosted = captions.map((c) => (c.text ? c : { ...c, text: "Your text" }));
    if (captionAt(measurerFor(ctx), ghosted, p.x, p.y, dims.W, dims.H)) return;
    const x = p.x / dims.W;
    addCaption({ x, y: p.y / dims.H, w: fitWidthAt(x) });
  }

  function addCaption(at = null) {
    snap("add");
    const c = newCaption("", at || { y: captions.length % 2 === 0 ? 0.11 : 0.89 });
    captions = [...captions, c];
    selId = c.id;
    queueMicrotask(() => textBox?.focus());
  }

  function removeCaption() {
    if (!sel) return;
    snap("remove");
    captions = captions.filter((c) => c.id !== selId);
    selId = captions.at(-1)?.id || "";
  }

  function removeLayer() {
    if (!selLay) return;
    snap("unlayer");
    layers = layers.filter((l) => l.id !== selId);
    selId = layers.at(-1)?.id || captions.at(-1)?.id || "";
    // The asset stays in the map: undo can bring this layer straight back, and
    // it would have nothing to draw if the picture had been thrown away.
  }

  function reorder(delta) {
    if (!selLay) return;
    const next = moveLayer(layers, selId, delta);
    if (next === layers) return; // already at the end — not an edit
    snap("order");
    layers = next;
  }

  // Picking a look is meant to be visible, so it clears the per-caption
  // overrides it would otherwise be fighting: choose "Classic" after hand-
  // picking a pink Comic Sans and you get the classic look, not silence.
  function applyLook(id) {
    if (!sel) return;
    snap("look");
    sel.style = id;
    for (const k of ["font", "color", "strokeColor", "stroke", "caps"]) delete sel[k];
  }

  function setTopBar(on) {
    snap("bar");
    const next = on ? TOP_BAR_H : 0;
    // Every caption's y is a fraction of a canvas that just changed height, so
    // without rebasing they all slide down the picture the moment the bar
    // appears. Layers are positioned the same way and slide the same way.
    for (const c of captions) c.y = rebaseTopBar(c.y, topBar, next);
    for (const l of layers) l.y = rebaseTopBar(l.y, topBar, next);
    if (on) {
      const c = newCaption("", { y: topBarCentre(next), size: 0.05, w: 0.9, style: "caption" });
      captions = [...captions, c];
      selId = c.id;
      queueMicrotask(() => textBox?.focus());
    }
    topBar = next;
  }

  // Arrow keys nudge the selected caption when focus isn't in a text field —
  // the difference between "roughly there" and "exactly there".
  function onKey(e) {
    const typing = e.target?.tagName === "TEXTAREA" || e.target?.tagName === "INPUT";
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "z") {
      e.preventDefault();
      undo();
      return;
    }
    const active = sel || selLay;
    if (!active || typing) return;
    const step = e.shiftKey ? 0.05 : 0.005;
    const moves = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] };
    const m = moves[e.key];
    if (m) {
      e.preventDefault();
      snap("nudge");
      if (selLay) {
        const cl = clampLayer({ ...selLay, x: selLay.x + m[0], y: selLay.y + m[1] }, dims.W, dims.H);
        selLay.x = cl.x;
        selLay.y = cl.y;
      } else {
        sel.x = Math.min(1, Math.max(0, sel.x + m[0]));
        sel.y = Math.min(1, Math.max(0, sel.y + m[1]));
      }
    } else if (e.key === "Delete" || e.key === "Backspace") {
      e.preventDefault();
      if (selLay) removeLayer();
      else removeCaption();
    }
  }

  // ---- send ------------------------------------------------------------

  // The editor's whole document, flat and JSON-safe. Deep-cloned through JSON
  // for the same reason the undo stack is: these are Svelte proxies, and what
  // goes into IndexedDB has to be plain data with no live references back into
  // a component that is about to be destroyed.
  const plain = (v) => JSON.parse(JSON.stringify(v));

  function session() {
    // usedAssets drops the pictures of layers that were deleted before sending.
    // Safe HERE and nowhere else: the editor is closing, so no undo can bring
    // one of those layers back and find its picture missing.
    const srcs = {};
    for (const [k, v] of assets) srcs[k] = v.src;
    return newSession({
      template: tplFile,
      label: tplName,
      base: baseSrc,
      topBar,
      captions: plain(captions),
      layers: plain(layers),
      slots: plain(slots),
      assets: usedAssets(layers, srcs),
    });
  }

  async function send() {
    if (!img || sending) return;
    sending = true;
    try {
      // Redraw without the marquee, the guides, the empty-slot boxes and the
      // placeholder ghosts, all of which are editor furniture that must never
      // end up in the picture. The LAYERS do go in — they are the picture.
      const out = document.createElement("canvas");
      out.width = dims.W;
      out.height = dims.H;
      const ctx = out.getContext("2d");
      drawMeme(ctx, img, captions, dims.W, dims.H, { topBar: dims.topBar, layers, imageFor });
      // JPEG: a photo with text over it, where PNG would be several times the
      // size for no visible gain and could breach the 5 MiB attachment cap.
      const dataUrl = out.toDataURL("image/jpeg", 0.9);
      // Editing goes back to the message's OWN channel, which need not be the
      // one on screen — a menu can be opened, the channel switched, and the
      // save arrive somewhere else entirely.
      const channelId = edit ? edit.channelId : S.activeChannelId;
      // Both calls resolve to the blob id of the picture that actually went
      // out. That id is minted inside the seal, so it is the only thing the
      // recipe can honestly be keyed by.
      const blobId = edit
        ? await api.editAttachment(channelId, edit.messageId, dataUrl, out.width, out.height)
        : await api.sendAttachment(channelId, dataUrl, out.width, out.height, "");
      // Saved AFTER the picture is in the channel and never allowed to fail the
      // send: losing the recipe costs the ability to edit again, losing the
      // meme costs the meme.
      await saveRecipe(blobId, session());
      // A re-render is a different picture and therefore a different blob, so
      // the old key is now unreachable — forget it instead of holding
      // megabytes for a blob no message points at.
      if (edit && edit.blobId !== blobId) await dropRecipe(edit.blobId);
      onClose();
    } catch (err) {
      flash(err);
      sending = false;
    }
  }

  const TEXT_COLORS = ["#ffffff", "#000000", "#ffe066", "#ff6b6b", "#4dabf7", "#51cf66", "#ff8cc8"];
  const EDGE_COLORS = ["#000000", "#ffffff", "#ff2d55", "#1e1e1e"];
  const ALIGNS = [
    ["left", "Left"],
    ["center", "Centre"],
    ["right", "Right"],
  ];
  // What the sliders read as, so the labels aren't bare numbers.
  const pct = (v) => `${Math.round(v * 100)}`;
</script>

<svelte:window onpaste={onPaste} onkeydown={onKey} />

<Modal title={edit ? "Edit meme" : "Meme"} {onClose} size="xl">
  <div
    class="wrap"
    class:dropping
    ondragover={(e) => {
      e.preventDefault();
      dropping = true;
    }}
    ondragleave={() => (dropping = false)}
    ondrop={(e) => {
      e.preventDefault();
      dropping = false;
      addImage(e.dataTransfer?.files?.[0]);
    }}
    role="application"
    aria-label="Meme editor"
  >
    {#if browsing}
      <!-- ---- gallery ------------------------------------------------ -->
      <div class="gallery">
        <div class="gtop">
          <div class="gtitle">
            <h4>Start with a template</h4>
            <p>
              {templates.length ? `${templates.length} classics, or bring` : "Bring"} your own picture — drop it here or just
              paste.
            </p>
          </div>
          {#if templates.length}
            <label class="find">
              <Icon name="search" size={14} />
              <input placeholder="Search templates…" bind:value={query} aria-label="Search templates" />
              {#if query}
                <button class="clearq" onclick={() => (query = "")} aria-label="Clear search">
                  <Icon name="close" size={12} />
                </button>
              {/if}
            </label>
          {/if}
        </div>

        <div class="cards">
          <button class="card own" onclick={() => fileInput.click()}>
            <span class="thumb own-thumb">
              <Icon name="imagetext" size={34} />
            </span>
            <span class="name">Your own image</span>
          </button>
          {#each shown as t (t.file)}
            <!-- the whole manifest entry, not just its captions: load() also
                 reads topBar and the label off it -->
            <button class="card" onclick={() => load(`/memes/${t.file}`, t)}>
              <span class="thumb">
                <img src={`/memes/${t.file}`} alt="" loading="lazy" />
              </span>
              <span class="name">{t.label}</span>
            </button>
          {/each}
        </div>

        {#if manifestDone && templates.length && !shown.length}
          <p class="none">Nothing matches “{query}”. Try a word from the picture — “fire”, “cat”, “yes no”.</p>
        {/if}
        {#if img}
          <button class="ghostbtn back-to-edit" onclick={() => (browsing = false)}>
            <Icon name="chevron" size={13} /> Back to {tplName}
          </button>
        {/if}
      </div>
    {:else}
      <!-- ---- editor ------------------------------------------------- -->
      <div class="bar">
        <button class="ghostbtn" onclick={() => (browsing = true)} title="Pick a different template">
          <Icon name="folder" size={14} /> <span class="btxt">{tplName}</span>
        </button>
        <div class="spacer"></div>
        <label class="toggle" title="Add a white caption strip above the picture">
          <input type="checkbox" checked={topBar > 0} onchange={(e) => setTopBar(e.currentTarget.checked)} />
          <span>Top bar</span>
        </label>
        <button class="ghostbtn" onclick={undo} disabled={!canUndo} title="Undo (Ctrl+Z)">
          <Icon name="reply" size={14} /> <span class="btxt">Undo</span>
        </button>
      </div>

      <div class="edit">
        <div class="stage">
          {#if img}
            <canvas
              bind:this={canvas}
              onpointerdown={onDown}
              onpointermove={onMove}
              onpointerup={onUp}
              onpointercancel={onUp}
              ondblclick={onDouble}
            ></canvas>
            <!-- Both halves of this line named gestures a phone doesn't have. -->
            <p class="hint">
              {S.isMobile
                ? "Drag the text · drag the corner grip to scale and spin · double-tap to add one"
                : "Drag the text · corner grip scales & spins · double-click to add"}
            </p>
          {/if}
        </div>

        <div class="side">
          <div class="caphead">
            <span class="lbl">Captions</span>
            <div class="capbtns">
              <button class="mini" onclick={() => addCaption()} title="Add a caption">
                <Icon name="plus" size={13} /> Add
              </button>
              <button class="mini danger" onclick={removeCaption} disabled={!sel} title="Remove the selected caption">
                <Icon name="trash" size={13} />
              </button>
            </div>
          </div>

          <div class="chips">
            {#each captions as c, i (c.id)}
              <button class="chip" class:on={c.id === selId} onclick={() => (selId = c.id)}>
                <b>{i + 1}</b>
                {c.text ? c.text.slice(0, 18) : "empty"}
              </button>
            {/each}
          </div>

          <div class="caphead">
            <span class="lbl">Pictures</span>
            <div class="capbtns">
              <button class="mini" onclick={() => layerInput.click()} title="Put a picture on top of this one">
                <Icon name="imagetext" size={13} /> Add
              </button>
              <button class="mini danger" onclick={removeLayer} disabled={!selLay} title="Remove the selected picture">
                <Icon name="trash" size={13} />
              </button>
            </div>
          </div>

          {#if layers.length}
            <!-- Last in the list is drawn on top, so the chips are shown top
                 first: the order you see is the order you're looking at. -->
            <div class="chips">
              {#each [...layers].reverse() as l (l.id)}
                <button class="chip" class:on={l.id === selId} onclick={() => (selId = l.id)}>
                  <img class="lthumb" src={assets.get(l.asset)?.src} alt="" />
                  {Math.round(l.w * 100)}%
                </button>
              {/each}
            </div>
          {:else}
            <p class="pick">
              <!-- Ctrl+V and drag-drop are the only routes this used to name,
                   and neither exists on a phone — so a phone user read an
                   instruction they couldn't follow and concluded the feature was
                   desktop-only. The Add button above has always worked. -->
              {#if S.isMobile}
                {slots.length
                  ? `This template has ${slots.length} picture panel${slots.length > 1 ? "s" : ""} — tap Add and your picture lands in the next one.`
                  : "Tap Add to put a picture on top of this one."}
              {:else}
                {slots.length
                  ? `This template has ${slots.length} picture panel${slots.length > 1 ? "s" : ""} — paste (Ctrl+V) or drop a picture and it lands in the next one.`
                  : "Paste (Ctrl+V) or drop a picture and it goes on top of this one."}
              {/if}
            </p>
          {/if}

          {#if selLay}
            <label class="row">
              <span>Size</span>
              <input
                type="range"
                min={LAYER_SCALE.min}
                max="1.2"
                step="0.01"
                value={selLay.w}
                oninput={(e) => setLay("lsize", "w", +e.currentTarget.value)}
                use:rangefill={selLay.w}
              />
              <em>{pct(selLay.w)}</em>
            </label>

            <!-- Not a <label>: the straighten button lives in this row, and a
                 click inside a label is forwarded to its input. -->
            <div class="row">
              <span>Rotate</span>
              <input
                type="range"
                min="-180"
                max="180"
                step="1"
                value={selLay.rot || 0}
                aria-label="Rotate picture"
                oninput={(e) => setLay("lrot", "rot", +e.currentTarget.value)}
                use:rangefill={selLay.rot || 0}
              />
              <button class="em-btn" onclick={() => setLay("lrot0", "rot", 0)} title="Straighten">{selLay.rot || 0}°</button>
            </div>

            <div class="row">
              <span>Order</span>
              <div class="seg tight">
                <button onclick={() => reorder(1)} title="Bring the picture forward">Forward</button>
                <button onclick={() => reorder(-1)} title="Send the picture back">Back</button>
              </div>
            </div>
            <p class="pick">Pictures sit over the template and under the words. Drag it, or use the corner grip.</p>
          {:else if sel}
            <textarea
              bind:this={textBox}
              value={sel.text}
              oninput={(e) => set("text", "text", e.currentTarget.value)}
              rows="2"
              placeholder="Type the words…"
            ></textarea>

            <div class="seg" role="radiogroup" aria-label="Look">
              {#each Object.entries(STYLES) as [id, s] (id)}
                <button class:sel={sel.style === id} role="radio" aria-checked={sel.style === id} onclick={() => applyLook(id)}>
                  {s.label}
                </button>
              {/each}
            </div>

            <span class="lbl">Font</span>
            <div class="fonts">
              {#each Object.entries(FONTS) as [id, f] (id)}
                <button
                  class="font"
                  class:on={resolve(sel).family === f.family}
                  style="font-family:{f.family};font-weight:{f.weight}"
                  onclick={() => set("font", "font", id)}
                >
                  {f.label}
                </button>
              {/each}
            </div>

            <div class="row">
              <span>Text</span>
              <div class="swatches">
                {#each TEXT_COLORS as c (c)}
                  <button
                    class="sw"
                    class:on={resolve(sel).color === c}
                    style="background:{c}"
                    aria-label="Text colour {c}"
                    onclick={() => set("color", "color", c)}
                  ></button>
                {/each}
              </div>
            </div>

            <div class="row">
              <span>Outline</span>
              <div class="swatches">
                {#each EDGE_COLORS as c (c)}
                  <button
                    class="sw"
                    class:on={resolve(sel).strokeColor === c}
                    style="background:{c}"
                    aria-label="Outline colour {c}"
                    onclick={() => set("edge", "strokeColor", c)}
                  ></button>
                {/each}
              </div>
            </div>

            <label class="row">
              <span>Thickness</span>
              <input
                type="range"
                min="0"
                max="0.24"
                step="0.005"
                value={resolve(sel).stroke}
                oninput={(e) => set("weight", "stroke", +e.currentTarget.value)}
                use:rangefill={resolve(sel).stroke}
              />
              <em>{pct(resolve(sel).stroke)}</em>
            </label>

            <label class="row">
              <span>Size</span>
              <input
                type="range"
                min="0.02"
                max="0.3"
                step="0.005"
                value={sel.size}
                oninput={(e) => set("size", "size", +e.currentTarget.value)}
                use:rangefill={sel.size}
              />
              <em>{pct(sel.size)}</em>
            </label>

            <label class="row">
              <span>Width</span>
              <input
                type="range"
                min="0.2"
                max="1"
                step="0.02"
                value={sel.w}
                oninput={(e) => set("width", "w", +e.currentTarget.value)}
                use:rangefill={sel.w}
              />
              <em>{pct(sel.w)}</em>
            </label>

            <!-- Not a <label>: the reset button lives in this row, and a click
                 inside a label is forwarded to its input. -->
            <div class="row">
              <span>Rotate</span>
              <input
                type="range"
                min="-45"
                max="45"
                step="1"
                value={sel.rot || 0}
                aria-label="Rotate"
                oninput={(e) => set("rot", "rot", +e.currentTarget.value)}
                use:rangefill={sel.rot || 0}
              />
              <button class="em-btn" onclick={() => set("rot0", "rot", 0)} title="Straighten">{sel.rot || 0}°</button>
            </div>

            <div class="row">
              <span>Align</span>
              <div class="seg tight" role="radiogroup" aria-label="Alignment">
                {#each ALIGNS as [id, label] (id)}
                  <button
                    class:sel={(sel.align || "center") === id}
                    role="radio"
                    aria-checked={(sel.align || "center") === id}
                    onclick={() => set("align", "align", id)}>{label}</button
                  >
                {/each}
              </div>
            </div>

            <label class="row">
              <span>ALL CAPS</span>
              <input
                type="checkbox"
                checked={resolve(sel).uppercase}
                onchange={(e) => set("caps", "caps", e.currentTarget.checked)}
              />
            </label>
          {:else}
            <p class="pick">Pick a caption above, or double-click the picture to add one.</p>
          {/if}

          <button class="send" onclick={send} disabled={!img || sending}>
            {#if sending}
              {edit ? "Saving…" : "Sending…"}
            {:else}
              {edit ? "Save changes" : "Send it"}
            {/if}
          </button>
        </div>
      </div>
    {/if}
  </div>
</Modal>

<input
  type="file"
  accept="image/*"
  bind:this={fileInput}
  style="display:none"
  onchange={(e) => {
    pickBase(e.target.files?.[0]);
    e.target.value = "";
  }}
/>
<input
  type="file"
  accept="image/*"
  bind:this={layerInput}
  style="display:none"
  onchange={(e) => {
    addImage(e.target.files?.[0]);
    e.target.value = "";
  }}
/>

<style>
  /* The dialog is a fixed-height flex column; without this the editor sits at
     content height and leaves a third of the workspace empty below it. */
  .wrap {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
    border-radius: var(--radius-md);
  }
  .wrap.dropping {
    outline: 2px dashed var(--accent);
    outline-offset: -4px;
  }

  /* ---- gallery ---- */
  .gallery {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .gtop {
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    gap: 14px;
    flex-wrap: wrap;
  }
  .gtitle h4 {
    margin: 0 0 3px;
    font-size: var(--fs-body);
    font-weight: 700;
  }
  .gtitle p {
    margin: 0;
    font-size: var(--fs-compact);
    color: var(--text-muted);
  }
  .find {
    display: flex;
    align-items: center;
    gap: 7px;
    padding: 0 10px;
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: 999px;
    color: var(--text-muted);
    min-width: 210px;
    transition:
      border-color var(--dur-standard) ease,
      box-shadow var(--dur-standard) ease;
  }
  /* The input strips its own border and background, which is exactly what the
     app-wide focus rule paints — so without a ring on the pill that holds it,
     a keyboard lands here and nothing on screen changes. */
  .find:focus-within {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  .find input {
    flex: 1;
    min-width: 0;
    background: transparent;
    border: 0;
    outline: none;
    color: var(--text);
    font: inherit;
    font-size: var(--fs-ui);
    padding: 8px 0;
  }
  .clearq {
    background: transparent;
    color: var(--text-muted);
    padding: 2px;
    display: grid;
    place-items: center;
  }
  .cards {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(148px, 1fr));
    gap: 10px;
    padding: 2px 4px 4px 2px;
    align-content: start;
  }
  .card {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 0;
    background: transparent;
    text-align: left;
    border-radius: var(--radius-md);
    transition:
      transform var(--dur-quick) ease,
      box-shadow var(--dur-quick) ease;
  }
  .card:hover {
    transform: translateY(-2px);
  }
  .card:hover .thumb {
    box-shadow: 0 6px 18px rgba(0, 0, 0, 0.35);
    border-color: var(--accent);
  }
  .thumb {
    display: grid;
    place-items: center;
    position: relative;
    aspect-ratio: 1;
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }
  /* contain, not cover: a cropped Drake is unrecognisable, and recognising the
     picture is the entire job of this grid. Absolutely positioned rather than
     sized with percentages — against an aspect-ratio box a percentage height
     doesn't resolve, and every tall template gets its bottom sliced off. */
  .thumb img {
    position: absolute;
    inset: 3px;
    width: calc(100% - 6px);
    height: calc(100% - 6px);
    object-fit: contain;
  }
  .own-thumb {
    border-style: dashed;
    color: var(--text-muted);
  }
  .name {
    font-size: 12px;
    line-height: 1.25;
    color: var(--text);
    padding: 0 2px;
  }
  .none {
    margin: 0;
    font-size: var(--fs-ui);
    color: var(--text-muted);
    text-align: center;
  }
  .back-to-edit {
    align-self: center;
  }

  /* ---- editor ---- */
  .bar {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    flex: none;
  }
  .spacer {
    flex: 1;
  }
  .ghostbtn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: var(--fs-compact);
    padding: 6px 10px;
    border-radius: var(--radius-sm);
    background: var(--bg-3);
    color: var(--text);
    max-width: 46%;
    overflow: hidden;
    white-space: nowrap;
  }
  .ghostbtn:disabled {
    opacity: 0.45;
  }
  .btxt {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .toggle {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: var(--fs-compact);
    color: var(--text-muted);
    white-space: nowrap;
  }

  .edit {
    flex: 1;
    min-height: 0;
    display: grid;
    grid-template-columns: minmax(0, 1fr) 274px;
    gap: 14px;
  }
  /* A grid, not a flex column: the picture gets a `minmax(0, 1fr)` row, which
     is a definite height, so `max-height: 100%` on the canvas actually bounds
     it. In a flex column the same canvas grows to its natural size and pushes
     the hint line out through the bottom of the dialog. */
  .stage {
    display: grid;
    grid-template-rows: minmax(0, 1fr) auto;
    justify-items: center;
    align-items: center;
    gap: var(--sp-2);
    background: var(--bg-3);
    border-radius: var(--radius-md);
    padding: var(--sp-3);
    min-height: 0;
    overflow: hidden;
  }
  /* The preview fills the stage rather than sitting at the template's own
     600-ish pixels in the middle of a grey field. Upscaling costs a little
     sharpness on screen and nothing at all in the file: the backing store is
     still the real render size, so the export is unaffected and the drag
     targets get twice as big. */
  canvas {
    width: 100%;
    height: auto;
    max-height: 100%;
    border-radius: var(--radius-sm);
    cursor: grab;
    touch-action: none; /* let us handle the drag, not the browser's scroll */
    box-shadow: 0 6px 22px rgba(0, 0, 0, 0.28);
  }
  canvas:active {
    cursor: grabbing;
  }
  .hint {
    margin: 0;
    flex: none;
    font-size: var(--fs-small);
    color: var(--text-muted);
    text-align: center;
  }
  .side {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    min-width: 0;
    overflow-y: auto;
    padding-right: 2px;
  }
  .lbl {
    font-size: var(--fs-tiny);
    font-weight: 700;
    letter-spacing: 0.4px;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  .caphead {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .capbtns {
    display: flex;
    gap: var(--sp-1);
  }
  .mini {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    font-size: 12px;
    padding: var(--sp-1) var(--sp-2);
    border-radius: var(--radius-sm);
    background: var(--bg-3);
    color: var(--text);
  }
  .mini:disabled {
    opacity: 0.4;
  }
  .mini.danger:not(:disabled):hover {
    background: var(--danger);
    color: var(--danger-fg);
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-1);
  }
  .chip {
    display: inline-flex;
    align-items: baseline;
    gap: 5px;
    font-size: var(--fs-small);
    padding: 4px 9px;
    border-radius: 999px;
    background: var(--bg-3);
    color: var(--text-muted);
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .chip b {
    font-size: var(--fs-tiny);
    opacity: 0.7;
  }
  .chip.on {
    background: var(--accent);
    color: var(--accent-fg);
  }
  /* A layer chip is identified by the picture in it — a row of "42%" labels
     tells you nothing about which pasted photo is which. */
  .lthumb {
    width: 18px;
    height: 18px;
    object-fit: cover;
    border-radius: 3px;
    align-self: center;
    background: var(--bg-1);
  }
  textarea {
    width: 100%;
    resize: vertical;
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    padding: var(--sp-2);
    font: inherit;
    font-size: var(--fs-ui);
  }
  .seg {
    display: flex;
    background: var(--bg-3);
    border-radius: var(--radius-sm);
    padding: 2px;
    gap: 2px;
  }
  .seg button {
    flex: 1;
    font-size: 12px;
    padding: 5px 4px;
    border-radius: calc(var(--radius-sm) - 2px);
    background: transparent;
    color: var(--text-muted);
  }
  .seg button.sel {
    background: var(--bg-1);
    color: var(--text);
    font-weight: 600;
  }
  .seg.tight button {
    font-size: 11px;
    padding: 3px 2px;
  }
  /* Each chip is set in the face it selects — the only honest preview of a
     font, and it makes the row scannable without reading a single label. */
  .fonts {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: var(--sp-1);
  }
  .font {
    font-size: 12px;
    padding: 6px 2px;
    border-radius: var(--radius-sm);
    background: var(--bg-3);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .font.on {
    background: var(--accent);
    color: var(--accent-fg);
  }
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-2);
    font-size: var(--fs-compact);
    color: var(--text-muted);
  }
  .row > span:first-child {
    flex: none;
    min-width: 58px;
  }
  .row input[type="range"] {
    flex: 1;
    min-width: 0;
  }
  .row .seg {
    flex: 1;
    min-width: 0;
  }
  .row em {
    font-style: normal;
    font-variant-numeric: tabular-nums;
    font-size: 11px;
    min-width: 24px;
    text-align: right;
  }
  .em-btn {
    background: transparent;
    color: var(--text-muted);
    font-size: 11px;
    font-variant-numeric: tabular-nums;
    padding: 2px 4px;
    min-width: 34px;
    text-align: right;
    border-radius: var(--radius-sm);
  }
  .em-btn:hover {
    color: var(--text);
    background: var(--bg-3);
  }
  .swatches {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--sp-1);
  }
  .sw {
    width: 18px;
    height: 18px;
    border-radius: 50%;
    padding: 0;
    border: 2px solid transparent;
    box-shadow: 0 0 0 1px var(--border);
  }
  .sw.on {
    border-color: var(--accent);
    box-shadow: 0 0 0 2px var(--accent);
  }
  .pick {
    margin: 0;
    font-size: var(--fs-compact);
    color: var(--text-muted);
  }
  .send {
    margin-top: auto;
    position: sticky;
    bottom: 0;
    background: var(--accent);
    color: var(--accent-fg);
    font-weight: 600;
    padding: 10px;
    border-radius: var(--radius-sm);
    flex: none;
  }
  .send:disabled {
    opacity: 0.5;
  }

  /* Phone: one column with the picture on top and the controls scrolling under
     it. The template gallery keeps its grid — on a 390px screen that is two
     named columns, which still beats a nameless strip. */
  @media (pointer: coarse), (max-width: 768px) {
    /* The bottom sheet is the scroll container here, and it has no height of
       its own, so nothing inside may claim a share of it: `flex: 1` against an
       auto-height parent resolves to nothing and collapses the picture to a
       twenty-pixel smear. Everything lays out at its natural height and the
       sheet scrolls as one. */
    .wrap,
    .gallery {
      flex: none;
    }
    .cards {
      flex: none;
      overflow: visible;
      grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
    }
    .edit {
      grid-template-columns: minmax(0, 1fr);
      grid-template-rows: auto auto;
      gap: 10px;
    }
    /* The picture stays put while the controls scroll under it — a slider you
       can't see the effect of is a slider you have to guess at. `top` clears
       the sheet's own sticky header. */
    /* Full-bleed to the sheet's padding edges: a sticky band with rounded
       corners lets whatever is scrolling underneath show through the cut-outs,
       which reads as a rendering fault. */
    .stage {
      grid-template-rows: auto auto;
      position: sticky;
      top: 34px;
      z-index: 1;
      margin: 0 -20px;
      padding: 8px 20px 10px;
      border-radius: 0;
    }
    canvas {
      /* A viewport unit, not a percentage: the stage has no definite height in
         this layout, so a percentage max-height would resolve to none. */
      max-height: 38vh;
    }
    .side {
      overflow: visible;
    }
    .find {
      min-width: 0;
      width: 100%;
    }
    .gtop {
      align-items: stretch;
    }
    .fonts {
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 5px;
    }
    /* The sheet floors every button at 44px tall, which turns a circular swatch
       into an ellipse. Two classes so it outranks that rule. */
    .side .sw {
      width: 34px;
      height: 34px;
      min-height: 34px;
    }
    .row > span:first-child {
      min-width: 64px;
    }
  }
</style>
