<script>
  // The picture editor: crop, turn, resize and mark up a staged picture
  // before it is sent. Opened from the first slot of a staged picture's row
  // (Composer), it hands back a new data URL in place of the staged one.
  //
  // Everything happens here, on a canvas: nothing is uploaded, and no library
  // is involved. The edit is a description (lib/photoedit.js) drawn twice: at
  // screen size for the preview, at full size for the export. The export is
  // a fresh canvas, which carries no EXIF, so an edited picture has no
  // location or camera details in it (the core strips them on send anyway).
  import { onMount, tick } from "svelte";
  import Icon from "./Icon.svelte";
  import { portal } from "./lib/portal.js";
  import { flash } from "./lib/state.svelte.js";
  import { humanError } from "./lib/errors.js";
  import {
    newEdit,
    isPristine,
    turn,
    turnedSize,
    clamp01,
    squareCrop,
    cropHandle,
    dragCrop,
    outputSize,
    drawTurned,
    drawMarks,
    render,
    encodings,
    COLORS,
    WIDTHS,
    TEXT_SIZE,
    SIZES,
  } from "./lib/photoedit.js";

  let { src, name = "", maxBytes = 5 * 1024 * 1024, onDone, onClose } = $props();

  let img = $state(null);
  let iw = $state(0);
  let ih = $state(0);
  let edit = $state(newEdit());
  let past = $state([]);
  let future = $state([]);
  let tool = $state("crop"); // crop | draw | size
  let pen = $state("pen"); // pen | arrow | text
  let color = $state(COLORS[2]);
  let widthIx = $state(1);
  let square = $state(false);
  let saving = $state(false);

  let stageEl = $state(null);
  let canvas = $state(null);
  let textEl = $state(null);
  let stageW = $state(0);
  let stageH = $state(0);
  // A text mark being typed: where it goes, and the words so far.
  let typing = $state(null);

  const T = $derived(turnedSize(iw, ih, edit.turns));
  // The part of the turned picture on screen: all of it while cropping, so
  // the crop can grow again; the crop itself otherwise, so you draw on what
  // will be sent.
  const view = $derived(tool === "crop" ? { x: 0, y: 0, w: 1, h: 1 } : edit.crop);
  const disp = $derived.by(() => {
    if (!iw || !stageW || !stageH) return { w: 0, h: 0 };
    const vw = view.w * T.w;
    const vh = view.h * T.h;
    const s = Math.min((stageW - 32) / vw, (stageH - 32) / vh);
    return { w: Math.max(1, Math.floor(vw * s)), h: Math.max(1, Math.floor(vh * s)) };
  });
  const out = $derived(iw ? outputSize(iw, ih, edit) : { w: 0, h: 0 });
  const longest = $derived(Math.max(Math.round(edit.crop.w * T.w), Math.round(edit.crop.h * T.h)));
  const pristine = $derived(isPristine(edit));

  onMount(() => {
    const im = new Image();
    im.onload = () => {
      img = im;
      iw = im.naturalWidth;
      ih = im.naturalHeight;
    };
    im.onerror = () => {
      flash("Couldn't open that picture for editing", "error");
      onClose?.();
    };
    im.src = src;
    const ro = new ResizeObserver(([e]) => {
      stageW = e.contentRect.width;
      stageH = e.contentRect.height;
    });
    if (stageEl) ro.observe(stageEl);
    return () => ro.disconnect();
  });

  // ---- drawing ----
  let raf = 0;
  function paint() {
    raf = 0;
    if (!canvas || !img || !disp.w) return;
    const dpr = Math.min(2, window.devicePixelRatio || 1);
    const bw = Math.round(disp.w * dpr);
    const bh = Math.round(disp.h * dpr);
    if (canvas.width !== bw || canvas.height !== bh) {
      canvas.width = bw;
      canvas.height = bh;
    }
    const ctx = canvas.getContext("2d");
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.clearRect(0, 0, bw, bh);
    // The whole turned picture at the scale that puts `view` on the canvas.
    const W = bw / view.w;
    const H = bh / view.h;
    ctx.translate(-view.x * W, -view.y * H);
    drawTurned(ctx, img, iw, ih, edit.turns, W, H);
    drawMarks(ctx, edit.marks, W, H);
    if (tool === "crop") paintCrop(ctx, W, H, dpr);
  }
  function paintCrop(ctx, W, H, dpr) {
    const c = edit.crop;
    const x = c.x * W;
    const y = c.y * H;
    const w = c.w * W;
    const h = c.h * H;
    ctx.save();
    ctx.fillStyle = "rgba(0,0,0,0.55)";
    ctx.beginPath();
    ctx.rect(0, 0, W, H);
    ctx.rect(x, y, w, h);
    ctx.fill("evenodd");
    ctx.strokeStyle = "rgba(255,255,255,0.4)";
    ctx.lineWidth = dpr;
    ctx.beginPath();
    for (const f of [1 / 3, 2 / 3]) {
      ctx.moveTo(x + w * f, y);
      ctx.lineTo(x + w * f, y + h);
      ctx.moveTo(x, y + h * f);
      ctx.lineTo(x + w, y + h * f);
    }
    ctx.stroke();
    ctx.strokeStyle = "#fff";
    ctx.lineWidth = 1.5 * dpr;
    ctx.strokeRect(x, y, w, h);
    // Corner brackets, the part a finger takes hold of.
    const L = Math.min(22 * dpr, w / 3, h / 3);
    ctx.lineWidth = 4 * dpr;
    ctx.lineCap = "round";
    ctx.beginPath();
    for (const [cx, cy, sx, sy] of [
      [x, y, 1, 1],
      [x + w, y, -1, 1],
      [x, y + h, 1, -1],
      [x + w, y + h, -1, -1],
    ]) {
      ctx.moveTo(cx + sx * L, cy);
      ctx.lineTo(cx, cy);
      ctx.lineTo(cx, cy + sy * L);
    }
    ctx.stroke();
    ctx.restore();
  }
  function schedule() {
    if (!raf) raf = requestAnimationFrame(paint);
  }
  $effect(() => {
    // Everything the picture depends on.
    void [img, disp.w, disp.h, edit, edit.marks.length, tool, view];
    schedule();
  });

  // ---- history ----
  const snap = () => JSON.parse(JSON.stringify(edit));
  function commit(next) {
    past = [...past.slice(-49), snap()];
    future = [];
    edit = next;
  }
  function undo() {
    if (!past.length) return;
    future = [snap(), ...future];
    edit = past[past.length - 1];
    past = past.slice(0, -1);
  }
  function redo() {
    if (!future.length) return;
    past = [...past, snap()];
    edit = future[0];
    future = future.slice(1);
  }

  // ---- pointer ----
  let drag = null;
  function pointAt(e) {
    const r = canvas.getBoundingClientRect();
    const u = (e.clientX - r.left) / r.width;
    const v = (e.clientY - r.top) / r.height;
    return [clamp01(view.x + u * view.w), clamp01(view.y + v * view.h)];
  }
  function onDown(e) {
    if (!img || (e.pointerType === "mouse" && e.button !== 0) || drag) return;
    if (typing) commitText();
    canvas.setPointerCapture?.(e.pointerId);
    const p = pointAt(e);
    const r = canvas.getBoundingClientRect();
    if (tool === "crop") {
      // A finger is wider than a cursor: grab from further away on touch.
      const tol = e.pointerType === "touch" ? 28 : 14;
      let handle = cropHandle(edit.crop, p, (tol / r.width) * view.w, (tol / r.height) * view.h);
      let start = { ...edit.crop };
      if (!handle) {
        handle = "se";
        start = { x: p[0], y: p[1], w: 0, h: 0 };
      }
      drag = { id: e.pointerId, kind: "crop", handle, start, p0: p, before: snap() };
    } else if (tool === "draw") {
      if (pen === "text") {
        drag = { id: e.pointerId, kind: "tap", p0: p };
        return;
      }
      const width = WIDTHS[widthIx];
      const mark = pen === "pen" ? { type: "pen", pts: [p], color, width } : { type: "arrow", a: p, b: p, color, width };
      drag = { id: e.pointerId, kind: pen, before: snap() };
      edit = { ...edit, marks: [...edit.marks, mark] };
    }
  }
  function onMove(e) {
    if (!drag || e.pointerId !== drag.id) return;
    const p = pointAt(e);
    if (drag.kind === "crop") {
      const aspect = T.w / T.h;
      edit = { ...edit, crop: dragCrop(drag.start, drag.handle, p[0] - drag.p0[0], p[1] - drag.p0[1], square, aspect) };
    } else if (drag.kind === "pen" || drag.kind === "arrow") {
      const marks = edit.marks.slice();
      const m = { ...marks[marks.length - 1] };
      if (m.type === "pen") {
        const last = m.pts[m.pts.length - 1];
        if (Math.hypot(p[0] - last[0], p[1] - last[1]) < 0.002) return;
        m.pts = [...m.pts, p];
      } else m.b = p;
      marks[marks.length - 1] = m;
      edit = { ...edit, marks };
    }
  }
  function onUp(e) {
    if (!drag || e.pointerId !== drag.id) return;
    const d = drag;
    drag = null;
    if (d.kind === "tap") {
      openText(d.p0);
      return;
    }
    if (d.kind === "arrow") {
      const m = edit.marks[edit.marks.length - 1];
      if (Math.hypot(m.b[0] - m.a[0], m.b[1] - m.a[1]) < 0.01) {
        edit = d.before;
        return;
      }
    }
    // The change is already on screen; record what it replaced.
    past = [...past.slice(-49), d.before];
    future = [];
  }

  // ---- text ----
  async function openText(at) {
    typing = { at, text: "" };
    await tick();
    textEl?.focus();
  }
  function commitText() {
    const t = typing;
    typing = null;
    if (!t || !t.text.trim()) return;
    commit({ ...edit, marks: [...edit.marks, { type: "text", at: t.at, text: t.text.trim(), color, size: TEXT_SIZE }] });
  }
  const textPos = $derived.by(() => {
    if (!typing) return null;
    return { left: ((typing.at[0] - view.x) / view.w) * 100, top: ((typing.at[1] - view.y) / view.h) * 100 };
  });

  // ---- actions ----
  function rotate(dir) {
    commit(turn(edit, dir));
  }
  function setSquare(on) {
    square = on;
    if (on) commit({ ...edit, crop: squareCrop(edit.crop, T.w / T.h) });
  }
  function resetAll() {
    if (pristine) return;
    commit(newEdit());
  }
  async function done() {
    if (typing) commitText();
    if (pristine) return onClose?.();
    saving = true;
    await tick();
    try {
      const c = render((w, h) => Object.assign(document.createElement("canvas"), { width: w, height: h }), img, iw, ih, edit);
      for (const [type, q] of encodings(src)) {
        const dataUrl = c.toDataURL(type, q);
        if (dataUrl.length * 0.75 <= maxBytes) {
          onDone?.({ dataUrl, w: c.width, h: c.height });
          return;
        }
      }
      flash("The edited picture is too large to send; try a smaller size", "error");
    } catch (err) {
      flash(humanError(String(err?.message || err)), "error");
    } finally {
      saving = false;
    }
  }

  function onKey(e) {
    if (e.key === "Escape") {
      e.stopPropagation();
      e.preventDefault();
      if (typing) typing = null;
      else onClose?.();
      return;
    }
    if (typing || e.target?.closest?.("input")) return;
    const mod = e.ctrlKey || e.metaKey;
    if (mod && e.key.toLowerCase() === "z") {
      e.preventDefault();
      if (e.shiftKey) redo();
      else undo();
    } else if (mod && e.key.toLowerCase() === "y") {
      e.preventDefault();
      redo();
    } else if (e.key === "Enter") {
      e.preventDefault();
      done();
    }
  }

  const TOOLS = [
    { id: "crop", label: "Crop", icon: "crop" },
    { id: "draw", label: "Mark up", icon: "pen" },
    { id: "size", label: "Size", icon: "resize" },
  ];
  const PENS = [
    { id: "pen", label: "Pen", icon: "pen" },
    { id: "arrow", label: "Arrow", icon: "arrowup" },
    { id: "text", label: "Text", icon: "texttool" },
  ];
  const sizeLabel = (s) => (s ? `${s} px` : "Original");
</script>

<svelte:window onkeydown={onKey} />

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="pe-scrim" use:portal onclick={(e) => e.target === e.currentTarget && onClose?.()}>
  <div class="pe" role="dialog" aria-modal="true" aria-label="Edit picture" data-testid="photo-editor">
    <header class="pe-top">
      <button type="button" class="pe-text" onclick={() => onClose?.()}>Cancel</button>
      <div class="pe-title">
        <strong>Edit picture</strong>
        <span class="pe-dims" aria-live="polite">{out.w ? `${out.w} × ${out.h}` : ""}{name ? ` · ${name}` : ""}</span>
      </div>
      <div class="pe-hist">
        <button type="button" class="pe-icon" aria-label="Undo" title="Undo · Ctrl+Z" disabled={!past.length} onclick={undo}><Icon name="undo" size={18} /></button>
        <button type="button" class="pe-icon" aria-label="Redo" title="Redo · Ctrl+Shift+Z" disabled={!future.length} onclick={redo}><Icon name="redo" size={18} /></button>
      </div>
      <button type="button" class="primary pe-done" onclick={done} disabled={saving || !img}>{saving ? "Saving…" : "Done"}</button>
    </header>

    <div class="pe-stage" bind:this={stageEl} class:crop={tool === "crop"}>
      {#if !img}<span class="pe-spin" aria-label="Loading"></span>{/if}
      <div class="pe-frame" style:width="{disp.w}px" style:height="{disp.h}px">
        <canvas
          bind:this={canvas}
          class="pe-canvas"
          class:drawing={tool === "draw"}
          style:width="{disp.w}px"
          style:height="{disp.h}px"
          aria-label={tool === "crop" ? "Drag the corners to crop" : tool === "draw" ? "Draw on the picture" : "The picture"}
          onpointerdown={onDown}
          onpointermove={onMove}
          onpointerup={onUp}
          onpointercancel={onUp}
        ></canvas>
        {#if typing && textPos}
          <input
            bind:this={textEl}
            class="pe-type"
            style:left="{textPos.left}%"
            style:top="{textPos.top}%"
            style:color={color}
            placeholder="Type, then Enter"
            aria-label="Text to place"
            maxlength="80"
            bind:value={typing.text}
            onkeydown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                commitText();
              }
            }}
            onblur={commitText}
          />
        {/if}
      </div>
    </div>

    <div class="pe-panel">
      {#if tool === "crop"}
        <div class="pe-row" role="group" aria-label="Crop">
          <button type="button" class="pe-chip" onclick={() => rotate(-1)} aria-label="Turn left"><Icon name="rotate" size={16} />Left</button>
          <button type="button" class="pe-chip" onclick={() => rotate(1)} aria-label="Turn right"><span class="mirror"><Icon name="rotate" size={16} /></span>Right</button>
          <span class="pe-sep" aria-hidden="true"></span>
          <button type="button" class="pe-chip" class:on={!square} aria-pressed={!square} onclick={() => setSquare(false)}>Free</button>
          <button type="button" class="pe-chip" class:on={square} aria-pressed={square} onclick={() => setSquare(true)}>Square</button>
          <span class="pe-sep" aria-hidden="true"></span>
          <button type="button" class="pe-chip" onclick={resetAll} disabled={pristine}>Reset</button>
        </div>
      {:else if tool === "draw"}
        <div class="pe-row" role="group" aria-label="Mark up">
          <div class="pe-seg" role="radiogroup" aria-label="Tool">
            {#each PENS as p (p.id)}
              <button type="button" role="radio" aria-checked={pen === p.id} class:on={pen === p.id} onclick={() => (pen = p.id)} aria-label={p.label} title={p.label}>
                <Icon name={p.icon} size={16} /><span class="lbl">{p.label}</span>
              </button>
            {/each}
          </div>
          <div class="pe-swatches" role="radiogroup" aria-label="Colour">
            {#each COLORS as c (c)}
              <button type="button" role="radio" class="sw" class:on={color === c} aria-checked={color === c} aria-label="Colour {c}" style:--sw={c} onclick={() => (color = c)}></button>
            {/each}
          </div>
          {#if pen !== "text"}
            <div class="pe-widths" role="radiogroup" aria-label="Line width">
              {#each WIDTHS as w, i (w)}
                <button type="button" role="radio" class="wd" class:on={widthIx === i} aria-checked={widthIx === i} aria-label={["Thin", "Medium", "Thick"][i]} onclick={() => (widthIx = i)}>
                  <span style:--d="{4 + i * 4}px"></span>
                </button>
              {/each}
            </div>
          {/if}
        </div>
      {:else}
        <div class="pe-row" role="radiogroup" aria-label="Size">
          {#each SIZES as s (s)}
            {@const off = s > 0 && s >= longest}
            <button type="button" role="radio" class="pe-chip" class:on={edit.size === s} aria-checked={edit.size === s} disabled={off} onclick={() => commit({ ...edit, size: s })}>
              {sizeLabel(s)}
            </button>
          {/each}
          <span class="pe-note">Sends at {out.w} × {out.h}</span>
        </div>
      {/if}
    </div>

    <nav class="pe-tabs" aria-label="Edit">
      {#each TOOLS as t (t.id)}
        <button type="button" class:on={tool === t.id} aria-pressed={tool === t.id} onclick={() => (tool = t.id)}>
          <Icon name={t.icon} size={20} />
          <span>{t.label}</span>
        </button>
      {/each}
    </nav>
  </div>
</div>

<style>
  .pe-scrim {
    position: fixed;
    inset: 0;
    z-index: var(--z-studio, 400);
    background: var(--scrim);
    display: grid;
    place-items: center;
    padding: var(--sp-5);
    animation: pe-fade var(--dur-standard) ease both;
  }
  @keyframes pe-fade {
    from {
      opacity: 0;
    }
  }
  .pe {
    width: min(1100px, 100%);
    height: min(820px, 100%);
    display: grid;
    grid-template-rows: auto minmax(0, 1fr) auto auto;
    background: var(--bg-elevated);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: var(--radius-xl);
    box-shadow: var(--shadow-pop);
    overflow: hidden;
    animation: pe-rise var(--dur-enter) var(--ease-spring) both;
  }
  @keyframes pe-rise {
    from {
      opacity: 0;
      transform: translateY(14px) scale(0.98);
    }
  }
  :global(:root[data-motion="reduced"]) .pe-scrim,
  :global(:root[data-motion="reduced"]) .pe {
    animation: none;
  }

  .pe-top {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto auto;
    align-items: center;
    gap: var(--sp-3);
    padding: var(--sp-3) var(--sp-4);
    border-bottom: 1px solid var(--hairline);
  }
  .pe-title {
    min-width: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    line-height: 1.25;
  }
  .pe-title strong {
    font-family: var(--display-font);
    font-size: var(--fs-ui);
  }
  .pe-dims {
    max-width: 100%;
    font-size: var(--fs-small);
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .pe-text {
    height: 36px;
    padding: 0 var(--sp-3);
    border-radius: 999px;
    background: transparent;
    color: var(--text);
    font-weight: 500;
  }
  .pe-text:hover {
    background: var(--bg-2);
  }
  .pe-hist {
    display: flex;
    gap: var(--sp-1);
  }
  .pe-icon {
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    border-radius: 999px;
    background: transparent;
    color: var(--text);
  }
  .pe-icon:hover:not(:disabled) {
    background: var(--bg-2);
  }
  .pe-icon:disabled {
    opacity: 0.35;
    cursor: default;
  }
  .pe-done {
    height: 36px;
    padding: 0 var(--sp-5);
    border-radius: 999px;
  }

  /* The stage is the dark of a darkroom in either theme: a picture is judged
     against neutral, not against the app's paper. */
  .pe-stage {
    position: relative;
    min-height: 0;
    display: grid;
    place-items: center;
    background:
      radial-gradient(120% 90% at 50% 40%, #1c2124, #0c0f10);
    overflow: hidden;
  }
  .pe-frame {
    position: relative;
  }
  .pe-canvas {
    display: block;
    touch-action: none;
    border-radius: var(--radius-sm);
    box-shadow: 0 10px 40px rgba(0, 0, 0, 0.45);
    -webkit-touch-callout: none;
    user-select: none;
    -webkit-user-select: none;
  }
  .pe-stage.crop .pe-canvas {
    border-radius: 0;
    cursor: crosshair;
  }
  .pe-canvas.drawing {
    cursor: crosshair;
  }
  .pe-type {
    position: absolute;
    transform: translate(-50%, -50%);
    width: min(280px, 80%);
    padding: var(--sp-2) var(--sp-3);
    border-radius: var(--radius-md);
    border: 1px solid rgba(255, 255, 255, 0.5);
    background: rgba(0, 0, 0, 0.55);
    font-weight: 700;
    font-size: var(--fs-body);
    text-align: center;
  }
  .pe-type::placeholder {
    color: rgba(255, 255, 255, 0.6);
  }
  .pe-spin {
    position: absolute;
    width: 28px;
    height: 28px;
    border-radius: 50%;
    border: 3px solid rgba(255, 255, 255, 0.2);
    border-top-color: #fff;
    animation: pe-spin 0.8s linear infinite;
  }
  @keyframes pe-spin {
    to {
      transform: rotate(1turn);
    }
  }

  .pe-panel {
    padding: var(--sp-3) var(--sp-4);
    border-top: 1px solid var(--hairline);
    min-height: 64px;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .pe-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: center;
    gap: var(--sp-2);
    max-width: 100%;
  }
  .pe-chip {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-2);
    height: 36px;
    padding: 0 var(--sp-3) 0 var(--sp-3);
    border-radius: 999px;
    background: var(--bg-2);
    color: var(--text);
    font-size: var(--fs-small);
    font-weight: 500;
    white-space: nowrap;
  }
  .pe-chip:hover:not(:disabled, .on) {
    background: var(--bg-3);
  }
  .pe-chip.on {
    background: var(--accent-soft);
    color: var(--accent-hover);
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--accent) 50%, transparent);
  }
  .pe-chip:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .mirror {
    display: inline-grid;
    transform: scaleX(-1);
  }
  .pe-sep {
    width: 1px;
    height: 20px;
    background: var(--border);
    margin: 0 var(--sp-1);
  }
  .pe-note {
    font-size: var(--fs-small);
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
    margin-inline-start: var(--sp-2);
  }
  .pe-seg {
    display: inline-flex;
    padding: 3px;
    border-radius: 999px;
    background: var(--bg-2);
  }
  .pe-seg button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 32px;
    padding: 0 var(--sp-3);
    border-radius: 999px;
    background: transparent;
    color: var(--text-muted);
    font-size: var(--fs-small);
    font-weight: 600;
  }
  .pe-seg button.on {
    background: var(--bg-elevated);
    color: var(--text);
    box-shadow: var(--shadow-rest);
  }
  .pe-swatches,
  .pe-widths {
    display: inline-flex;
    align-items: center;
    gap: 2px;
  }
  .sw,
  .wd {
    display: grid;
    place-items: center;
    width: 32px;
    height: 32px;
    border-radius: 999px;
    background: transparent;
    color: var(--text);
  }
  .sw::before {
    content: "";
    width: 20px;
    height: 20px;
    border-radius: 999px;
    background: var(--sw);
    box-shadow: inset 0 0 0 1px rgba(127, 127, 127, 0.45);
    transition: transform var(--dur-quick) var(--ease-spring);
  }
  .sw.on::before {
    transform: scale(1.18);
    box-shadow:
      0 0 0 2px var(--bg-elevated),
      0 0 0 4px var(--accent);
  }
  .wd span {
    width: var(--d);
    height: var(--d);
    border-radius: 999px;
    background: currentColor;
    opacity: 0.5;
  }
  .wd.on span {
    opacity: 1;
  }
  .wd.on {
    background: var(--bg-2);
  }

  .pe-tabs {
    display: flex;
    justify-content: center;
    gap: var(--sp-2);
    padding: var(--sp-2) var(--sp-4) var(--sp-3);
  }
  .pe-tabs button {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2px;
    min-width: 88px;
    padding: var(--sp-2) var(--sp-3);
    border-radius: var(--radius-md);
    background: transparent;
    color: var(--text-muted);
    font-size: var(--fs-small);
    font-weight: 600;
  }
  .pe-tabs button:hover {
    color: var(--text);
  }
  .pe-tabs button.on {
    color: var(--accent-hover);
    background: var(--accent-soft);
  }

  /* A phone: the whole screen, the bars inside the safe areas. */
  @media (max-width: 768px) {
    .pe-scrim {
      padding: 0;
    }
    .pe {
      width: 100%;
      height: 100%;
      border: 0;
      border-radius: 0;
      animation-name: pe-sheet;
    }
    @keyframes pe-sheet {
      from {
        transform: translateY(6%);
        opacity: 0;
      }
    }
    .pe-top {
      padding: calc(var(--safe-top, 0px) + var(--sp-2)) var(--sp-3) var(--sp-2);
      gap: var(--sp-2);
    }
    .pe-title strong {
      font-size: var(--fs-small);
    }
    .pe-panel {
      padding: var(--sp-2) var(--sp-3);
    }
    .pe-seg .lbl,
    .pe-sep {
      display: none;
    }
    .pe-row {
      gap: 6px;
    }
    .pe-chip {
      padding: 0 var(--sp-3);
    }
    .pe-tabs {
      padding: var(--sp-1) var(--sp-3) calc(var(--safe-bottom, 0px) + var(--sp-2));
    }
    .pe-tabs button {
      flex: 1;
      min-width: 0;
    }
  }
</style>
