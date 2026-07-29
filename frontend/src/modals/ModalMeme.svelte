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
    STYLES,
    FONTS,
    SEL_PAD,
    renderSize,
  } from "../lib/meme.js";

  let { onClose, src = "" } = $props();

  let img = $state(null); // HTMLImageElement, once decoded
  let captions = $state([]);
  let selId = $state("");
  let topBar = $state(0); // white caption strip above the picture, 0..1 of image height
  let sending = $state(false);
  let canvas = $state(null);
  let fileInput;
  let textBox = $state(null);
  let tplName = $state("");
  let query = $state("");
  let dropping = $state(false);
  // Which screen: the gallery until something is loaded, and again whenever
  // "Change" is pressed.
  let browsing = $state(!src);

  const sel = $derived(captions.find((c) => c.id === selId) || null);
  const dims = $derived(img ? renderSize(img.naturalWidth, img.naturalHeight, topBar) : null);
  const TOP_BAR_H = 0.22; // bar height as a fraction of the image, when on

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
    past.push(JSON.stringify({ captions, topBar }));
    if (past.length > 60) past.shift();
    canUndo = true;
  }

  // set(tag, key, value) — snapshot, then write the field on the selection.
  function set(tag, key, value) {
    if (!sel) return;
    snap(tag);
    sel[key] = value;
  }

  function undo() {
    const prev = past.pop();
    if (!prev) return;
    const s = JSON.parse(prev);
    captions = s.captions;
    topBar = s.topBar;
    if (!captions.some((c) => c.id === selId)) selId = captions.at(-1)?.id || "";
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

  // tpl is a manifest entry ({ file, label, topBar?, captions? }) or null for a
  // bring-your-own image.
  async function load(url, tpl = null) {
    try {
      const el = new Image();
      el.crossOrigin = "anonymous";
      await new Promise((res, rej) => {
        el.onload = res;
        el.onerror = () => rej(new Error("decode"));
        el.src = url;
      });
      img = el;
      // A template can ship its own caption boxes — picking "Drake" should put
      // two correctly-placed captions on screen, not make you position them.
      captions = tpl?.captions?.length
        ? tpl.captions.map((p) => newCaption("", p))
        : [newCaption("", { y: 0.11 }), newCaption("", { y: 0.89 })];
      selId = captions[0].id;
      topBar = tpl?.topBar || 0;
      tplName = tpl?.label || "Your image";
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

  if (src) load(src);

  function pickFile(file) {
    if (!file || !file.type.startsWith("image/")) return;
    const r = new FileReader();
    r.onload = () => load(String(r.result));
    r.readAsDataURL(file);
  }

  // Paste an image straight in — the fastest path from "saw a picture" to
  // "sent a meme", and the one people try first.
  function onPaste(e) {
    const item = [...(e.clipboardData?.items || [])].find((i) => i.type.startsWith("image/"));
    if (!item) return;
    e.preventDefault();
    pickFile(item.getAsFile());
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
    drawMeme(ctx, img, captions, dims.W, dims.H, { topBar: dims.topBar, placeholder: "Your text" });
    if (guides.x !== null || guides.y !== null) drawGuides(ctx);
    if (sel) outline(ctx, sel);
  });

  // The selection marquee is drawn on the same canvas rather than as a DOM
  // overlay, so it can never drift out of alignment with the text it marks.
  function outline(ctx, cap) {
    // Re-derived rather than cached from the draw: cheap, and always in step
    // with what was actually painted.
    const b = captionBox(measurerFor(ctx), cap.text ? cap : { ...cap, text: "Your text" }, dims.W, dims.H);
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
    // The grip belongs to the selected caption and sits outside its box, so it
    // has to be tested before the captions or a caption behind it would win.
    if (sel) {
      const b = boxOf(sel);
      const h = handlePos(b);
      if (Math.hypot(p.x - h.x, p.y - h.y) <= Math.max(18, dims.W * 0.035)) {
        snap("scale");
        drag = { kind: "scale", id: sel.id, cx: b.cx, cy: b.cy, vx: h.x - b.cx, vy: h.y - b.cy, size: sel.size, rot: sel.rot || 0 };
        canvas.setPointerCapture(e.pointerId);
        return;
      }
    }
    const ctx = canvas.getContext("2d");
    const ghosted = captions.map((c) => (c.text ? c : { ...c, text: "Your text" }));
    const hitGhost = captionAt(measurerFor(ctx), ghosted, p.x, p.y, dims.W, dims.H);
    const hit = hitGhost && captions.find((c) => c.id === hitGhost.id);
    if (!hit) {
      selId = "";
      return;
    }
    selId = hit.id;
    snap("move");
    drag = { kind: "move", id: hit.id, dx: hit.x * dims.W - p.x, dy: hit.y * dims.H - p.y };
    canvas.setPointerCapture(e.pointerId);
  }

  function onMove(e) {
    if (!drag) return;
    e.preventDefault();
    const p = toImage(e);
    const c = captions.find((x) => x.id === drag.id);
    if (!c) return;
    if (drag.kind === "scale") {
      const r = scaleRotate(drag, p.x, p.y, drag.cx, drag.cy);
      c.size = r.size;
      // Held straight, snap the angle back to level: nobody wants 0.4°.
      c.rot = Math.abs(r.rot) < 3 ? 0 : Math.round(r.rot);
      return;
    }
    // Clamped so a caption can't be dragged off the image and lost, and pulled
    // onto the centre/third lines when it comes close — with a guide drawn, so
    // the jump reads as help rather than as the drag misbehaving.
    const rawX = Math.min(1, Math.max(0, (p.x + drag.dx) / dims.W));
    const rawY = Math.min(1, Math.max(0, (p.y + drag.dy) / dims.H));
    const sx = snapTo(rawX, SNAP_X, 0.012);
    const sy = snapTo(rawY, SNAP_Y, 0.012);
    c.x = sx.v;
    c.y = sy.v;
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
    // appears.
    for (const c of captions) c.y = rebaseTopBar(c.y, topBar, next);
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
    if (!sel || typing) return;
    const step = e.shiftKey ? 0.05 : 0.005;
    const moves = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] };
    const m = moves[e.key];
    if (m) {
      e.preventDefault();
      snap("nudge");
      sel.x = Math.min(1, Math.max(0, sel.x + m[0]));
      sel.y = Math.min(1, Math.max(0, sel.y + m[1]));
    } else if (e.key === "Delete" || e.key === "Backspace") {
      e.preventDefault();
      removeCaption();
    }
  }

  // ---- send ------------------------------------------------------------
  async function send() {
    if (!img || sending) return;
    sending = true;
    try {
      // Redraw without the marquee, the guides and the placeholder ghosts, all
      // of which are editor furniture that must never end up in the picture.
      const out = document.createElement("canvas");
      out.width = dims.W;
      out.height = dims.H;
      const ctx = out.getContext("2d");
      drawMeme(ctx, img, captions, dims.W, dims.H, { topBar: dims.topBar });
      // JPEG: a photo with text over it, where PNG would be several times the
      // size for no visible gain and could breach the 5 MiB attachment cap.
      const dataUrl = out.toDataURL("image/jpeg", 0.9);
      await api.sendAttachment(S.activeChannelId, dataUrl, out.width, out.height, "");
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

<Modal title="Meme" {onClose} size="xl">
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
      pickFile(e.dataTransfer?.files?.[0]);
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
            <p class="hint">Drag the text · corner grip scales &amp; spins · double-click to add</p>
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

          {#if sel}
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
            {sending ? "Sending…" : "Send it"}
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
    pickFile(e.target.files?.[0]);
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
    gap: 12px;
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
    font-size: 15px;
    font-weight: 700;
  }
  .gtitle p {
    margin: 0;
    font-size: 12.5px;
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
  }
  .find input {
    flex: 1;
    min-width: 0;
    background: transparent;
    border: 0;
    outline: none;
    color: var(--text);
    font: inherit;
    font-size: 13px;
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
      transform 0.14s ease,
      box-shadow 0.14s ease;
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
    font-size: 13px;
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
    gap: 8px;
    flex: none;
  }
  .spacer {
    flex: 1;
  }
  .ghostbtn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12.5px;
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
    font-size: 12.5px;
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
    gap: 8px;
    background: var(--bg-3);
    border-radius: var(--radius-md);
    padding: 12px;
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
    border-radius: 4px;
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
    font-size: 11.5px;
    color: var(--text-muted);
    text-align: center;
  }
  .side {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
    overflow-y: auto;
    padding-right: 2px;
  }
  .lbl {
    font-size: 10.5px;
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
    gap: 4px;
  }
  .mini {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    padding: 4px 8px;
    border-radius: var(--radius-sm);
    background: var(--bg-3);
    color: var(--text);
  }
  .mini:disabled {
    opacity: 0.4;
  }
  .mini.danger:not(:disabled):hover {
    background: var(--danger, #b23);
    color: #fff;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }
  .chip {
    display: inline-flex;
    align-items: baseline;
    gap: 5px;
    font-size: 11.5px;
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
    font-size: 10px;
    opacity: 0.7;
  }
  .chip.on {
    background: var(--accent);
    color: #fff;
  }
  textarea {
    width: 100%;
    resize: vertical;
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    padding: 8px;
    font: inherit;
    font-size: 13px;
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
    gap: 4px;
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
    color: #fff;
  }
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    font-size: 12.5px;
    color: var(--text-muted);
  }
  .row > span:first-child {
    flex: none;
    min-width: 58px;
  }
  .row input[type="range"] {
    flex: 1;
    min-width: 0;
    accent-color: var(--accent); /* every other slider in the app sets this */
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
    gap: 4px;
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
    font-size: 12.5px;
    color: var(--text-muted);
  }
  .send {
    margin-top: auto;
    position: sticky;
    bottom: 0;
    background: var(--accent);
    color: #fff;
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
  @media (max-width: 760px) {
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
