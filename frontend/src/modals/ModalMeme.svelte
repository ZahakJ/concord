<script>
  // The meme editor. Opened by /meme, by the + menu, or by "Make a meme" on any
  // image already in the conversation.
  //
  // Everything is local: templates are bundled or come from this channel, the
  // render happens in a canvas here, and the result is sent as an ordinary
  // encrypted attachment. Nothing is uploaded anywhere to be composited.
  //
  // The layout maths (wrapping, auto-shrink, hit-testing) lives in lib/meme.js
  // so it can be tested without a browser; this file is interaction only.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import {
    drawMeme,
    newCaption,
    captionAt,
    captionBox,
    measurerFor,
    STYLES,
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

  const sel = $derived(captions.find((c) => c.id === selId) || null);
  const dims = $derived(img ? renderSize(img.naturalWidth, img.naturalHeight, topBar) : null);

  // ---- templates -------------------------------------------------------
  // The bundled pack is optional: if public/memes/manifest.json isn't present
  // the section simply doesn't appear, so the editor still works as a
  // bring-your-own-image tool in a build without the assets.
  let templates = $state([]);
  (async () => {
    try {
      const r = await fetch("/memes/manifest.json");
      if (r.ok) templates = await r.json();
    } catch {
      /* no bundled pack in this build */
    }
  })();

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
      queueMicrotask(() => textBox?.focus());
    } catch {
      flash("Couldn't open that image", "error");
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
  // Redraws whenever anything visible changes. The canvas backing store is the
  // real render size and CSS scales it down, so the preview is pixel-exact:
  // what you position is literally what gets sent, not an approximation of it.
  $effect(() => {
    if (!canvas || !img || !dims) return;
    // Touch the reactive state this depends on so Svelte re-runs the effect.
    void captions.length;
    void selId;
    void JSON.stringify(captions);
    canvas.width = dims.W;
    canvas.height = dims.H;
    const ctx = canvas.getContext("2d");
    ctx.clearRect(0, 0, dims.W, dims.H);
    drawMeme(ctx, img, captions, dims.W, dims.H, { topBar: dims.topBar });
    if (sel) outline(ctx, sel);
  });

  // The selection marquee is drawn on the same canvas rather than as a DOM
  // overlay, so it can never drift out of alignment with the text it marks.
  function outline(ctx, cap) {
    const style = STYLES[cap.style] || STYLES.impact;
    const m = measurerFor(ctx, style);
    // Re-derived rather than cached from the draw: cheap, and always in step
    // with what was actually painted.
    const b = captionBox(m, cap, dims.W, dims.H);
    const pad = b.size * 0.28;
    ctx.save();
    ctx.strokeStyle = "rgba(88,166,255,0.95)";
    ctx.lineWidth = Math.max(2, dims.W * 0.004);
    ctx.setLineDash([dims.W * 0.02, dims.W * 0.014]);
    ctx.strokeRect(b.x - pad, b.y - pad, b.w + pad * 2, b.h + pad * 2);
    ctx.restore();
  }

  // ---- pointer: select, drag, add --------------------------------------
  let drag = null;

  function toImage(e) {
    const r = canvas.getBoundingClientRect();
    return {
      x: ((e.clientX - r.left) / r.width) * dims.W,
      y: ((e.clientY - r.top) / r.height) * dims.H,
    };
  }

  function onDown(e) {
    if (!img) return;
    const p = toImage(e);
    const ctx = canvas.getContext("2d");
    const hit = captionAt(
      (t, size) => {
        ctx.font = `${STYLES.impact.weight} ${Math.round(size)}px ${STYLES.impact.family}`;
        return ctx.measureText(t).width;
      },
      captions,
      p.x,
      p.y,
      dims.W,
      dims.H,
    );
    if (!hit) return;
    selId = hit.id;
    drag = { id: hit.id, dx: hit.x * dims.W - p.x, dy: hit.y * dims.H - p.y };
    canvas.setPointerCapture(e.pointerId);
  }

  function onMove(e) {
    if (!drag) return;
    e.preventDefault();
    const p = toImage(e);
    const c = captions.find((x) => x.id === drag.id);
    if (!c) return;
    // Clamped so a caption can't be dragged off the image and lost.
    c.x = Math.min(1, Math.max(0, (p.x + drag.dx) / dims.W));
    c.y = Math.min(1, Math.max(0, (p.y + drag.dy) / dims.H));
    captions = captions;
  }

  const onUp = () => (drag = null);

  function addCaption() {
    const c = newCaption("", { y: captions.length % 2 === 0 ? 0.11 : 0.89 });
    captions = [...captions, c];
    selId = c.id;
    queueMicrotask(() => textBox?.focus());
  }

  function removeCaption() {
    if (!sel) return;
    captions = captions.filter((c) => c.id !== selId);
    selId = captions.at(-1)?.id || "";
  }

  // Arrow keys nudge the selected caption when focus isn't in a text field —
  // the difference between "roughly there" and "exactly there".
  function onKey(e) {
    if (!sel || e.target?.tagName === "TEXTAREA" || e.target?.tagName === "INPUT") return;
    const step = e.shiftKey ? 0.05 : 0.005;
    const moves = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] };
    const m = moves[e.key];
    if (m) {
      e.preventDefault();
      sel.x = Math.min(1, Math.max(0, sel.x + m[0]));
      sel.y = Math.min(1, Math.max(0, sel.y + m[1]));
      captions = captions;
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
      // Redraw without the selection marquee, which is editor furniture and
      // must never end up in the picture.
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

  const COLORS = ["#ffffff", "#000000", "#ffe066", "#ff6b6b", "#4dabf7", "#51cf66"];
</script>

<svelte:window onpaste={onPaste} onkeydown={onKey} />

<Modal title="Meme" {onClose} size="xl">
  <div class="wrap" class:empty={!img}>
    <!-- stage -->
    <div
      class="stage"
      ondragover={(e) => e.preventDefault()}
      ondrop={(e) => {
        e.preventDefault();
        pickFile(e.dataTransfer?.files?.[0]);
      }}
      role="application"
      aria-label="Meme canvas"
    >
      {#if img}
        <canvas
          bind:this={canvas}
          onpointerdown={onDown}
          onpointermove={onMove}
          onpointerup={onUp}
          onpointercancel={onUp}
        ></canvas>
      {:else}
        <button class="drop" onclick={() => fileInput.click()}>
          <Icon name="image" size={30} />
          <strong>Pick a template, or drop an image here</strong>
          <span>You can paste one straight in too</span>
        </button>
      {/if}
    </div>

    <!-- controls -->
    <div class="side">
      {#if img}
        <div class="cap-head">
          <span class="lbl">Captions</span>
          <div class="cap-btns">
            <button class="mini" onclick={addCaption} title="Add a caption">
              <Icon name="plus" size={13} /> Add
            </button>
            <button class="mini" onclick={removeCaption} disabled={!sel} title="Remove selected">
              <Icon name="trash" size={13} />
            </button>
          </div>
        </div>

        {#if captions.length > 1}
          <div class="chips">
            {#each captions as c, i (c.id)}
              <button class="chip" class:on={c.id === selId} onclick={() => (selId = c.id)}>
                {c.text ? c.text.slice(0, 14) : `Caption ${i + 1}`}
              </button>
            {/each}
          </div>
        {/if}

        {#if sel}
          <textarea
            bind:this={textBox}
            bind:value={sel.text}
            oninput={() => (captions = captions)}
            rows="2"
            placeholder="Type the words…"
          ></textarea>

          <div class="seg" role="radiogroup" aria-label="Text style">
            {#each Object.entries(STYLES) as [id, s] (id)}
              <button
                class:sel={sel.style === id}
                role="radio"
                aria-checked={sel.style === id}
                onclick={() => {
                  sel.style = id;
                  captions = captions;
                }}>{s.label}</button
              >
            {/each}
          </div>

          <label class="row">
            <span>Size</span>
            <input
              type="range"
              min="0.04"
              max="0.24"
              step="0.005"
              bind:value={sel.size}
              oninput={() => (captions = captions)}
            />
          </label>

          <div class="row">
            <span>Colour</span>
            <div class="swatches">
              {#each COLORS as c (c)}
                <button
                  class="sw"
                  class:on={(sel.color || STYLES[sel.style].color) === c}
                  style="background:{c}"
                  aria-label={c}
                  onclick={() => {
                    sel.color = c;
                    captions = captions;
                  }}
                ></button>
              {/each}
            </div>
          </div>

          <label class="row check">
            <input
              type="checkbox"
              checked={topBar > 0}
              onchange={(e) => (topBar = e.currentTarget.checked ? 0.18 : 0)}
            />
            <span>White caption bar on top</span>
          </label>
        {/if}
      {/if}

      <div class="templates">
        <span class="lbl">Templates</span>
        <div class="grid">
          <button class="tpl add" onclick={() => fileInput.click()} title="Use your own image">
            <Icon name="plus" size={16} />
          </button>
          {#each templates as t (t.file)}
            <button class="tpl" onclick={() => load(`/memes/${t.file}`, t.captions)} title={t.label}>
              <img src={`/memes/${t.file}`} alt={t.label} loading="lazy" />
            </button>
          {/each}
        </div>
      </div>

      <button class="send" onclick={send} disabled={!img || sending}>
        {sending ? "Sending…" : "Send it"}
      </button>
    </div>
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
  .wrap {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 260px;
    gap: 14px;
    min-height: 420px;
  }
  .stage {
    display: grid;
    place-items: center;
    background: var(--bg-3);
    border-radius: var(--radius-md);
    padding: 12px;
    min-height: 300px;
    overflow: hidden;
  }
  canvas {
    max-width: 100%;
    max-height: 62vh;
    border-radius: 4px;
    cursor: grab;
    touch-action: none; /* let us handle the drag, not the browser's scroll */
    box-shadow: 0 6px 22px rgba(0, 0, 0, 0.28);
  }
  canvas:active {
    cursor: grabbing;
  }
  .drop {
    display: grid;
    place-items: center;
    gap: 6px;
    padding: 34px 22px;
    border: 2px dashed var(--border);
    border-radius: var(--radius-md);
    background: transparent;
    color: var(--text-muted);
    width: 100%;
  }
  .drop strong {
    color: var(--text);
    font-size: 14px;
  }
  .drop span {
    font-size: 12px;
  }
  .side {
    display: flex;
    flex-direction: column;
    gap: 10px;
    min-width: 0;
  }
  .lbl {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.4px;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  .cap-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .cap-btns {
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
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }
  .chip {
    font-size: 11.5px;
    padding: 3px 8px;
    border-radius: 999px;
    background: var(--bg-3);
    color: var(--text-muted);
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
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
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    font-size: 12.5px;
    color: var(--text-muted);
  }
  .row input[type="range"] {
    flex: 1;
    max-width: 150px;
  }
  .row.check {
    justify-content: flex-start;
    gap: 6px;
    cursor: pointer;
  }
  .swatches {
    display: flex;
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
  }
  .templates {
    margin-top: auto;
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-height: 0;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 5px;
    max-height: 168px;
    overflow-y: auto;
    padding-right: 2px;
  }
  .tpl {
    aspect-ratio: 1;
    padding: 0;
    border-radius: var(--radius-sm);
    overflow: hidden;
    background: var(--bg-3);
    display: grid;
    place-items: center;
    color: var(--text-muted);
  }
  .tpl img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .tpl.add {
    border: 1px dashed var(--border);
  }
  .send {
    background: var(--accent);
    color: #fff;
    font-weight: 600;
    padding: 9px;
    border-radius: var(--radius-sm);
  }

  /* Phone: one column, canvas first, controls under it. The template strip
     becomes a horizontal scroller so it never eats the height the canvas
     needs — on a phone the picture is the thing you're working on. */
  @media (max-width: 720px) {
    .wrap {
      grid-template-columns: minmax(0, 1fr);
      min-height: 0;
    }
    canvas {
      max-height: 42vh;
    }
    .grid {
      display: flex;
      overflow-x: auto;
      max-height: none;
      padding-bottom: 4px;
    }
    .tpl {
      width: 56px;
      flex: 0 0 56px;
    }
  }
</style>
