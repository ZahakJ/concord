<script>
  // The drawing pad. Draw with a finger, a pen or a mouse; what gets sent is a
  // stroke list, not a picture (see lib/doodle.js).
  //
  // The canvas here is a VIEWFINDER, not the format. Every stroke is recorded
  // as points in the pad's own 640×400 space, so what leaves the machine is
  // independent of how big this dialog happened to be — a doodle drawn on a
  // phone and a doodle drawn on a desktop encode identically, and both render
  // at whatever width the reader's message row is.
  import { onMount } from "svelte";
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { haptic } from "../lib/touch.js";
  import { tooltip } from "../lib/tooltip.js";
  import { plural } from "../lib/plural.js";
  import {
    DOODLE_W,
    DOODLE_H,
    DOODLE_COLOURS,
    DOODLE_WIDTHS,
    MAX_STROKES,
    MAX_TOTAL_POINTS,
    encodeDoodle,
    prepareStroke,
    doodleTotalPoints,
  } from "../lib/doodle.js";

  let { onClose } = $props();

  let canvas = $state(null);
  let colour = $state(0);
  let width = $state(1);
  let busy = $state(false);
  // Committed strokes, in pad coordinates. Plain, not $state: the canvas is
  // repainted by hand and nothing in the template reads the points, so making
  // them reactive would only cost a proxy per point.
  let strokes = [];
  // The stroke under the pointer right now, drawn live but not yet simplified.
  let live = null;
  // Reactive mirrors of what the buttons need to know, updated at the two
  // moments the drawing changes.
  let strokeCount = $state(0);
  let pointCount = $state(0);

  const full = $derived(strokeCount >= MAX_STROKES || pointCount >= MAX_TOTAL_POINTS);
  const empty = $derived(strokeCount === 0);

  // The palette is CSS — `var(--accent)`, a color-mix against `--text` — and a
  // canvas needs a concrete colour. Resolve each one through a throwaway
  // element in this dialog's own tree, so the values come out of the live
  // theme rather than a table that would need re-tuning per theme pack.
  // Resolved once: the theme cannot change while the pad is open.
  let inks = DOODLE_COLOURS.map((c) => c.css);
  function resolveInks(host) {
    const probe = document.createElement("span");
    probe.setAttribute("aria-hidden", "true");
    probe.style.position = "absolute";
    probe.style.opacity = "0";
    host.appendChild(probe);
    inks = DOODLE_COLOURS.map((c) => {
      probe.style.color = "";
      probe.style.color = c.css;
      return getComputedStyle(probe).color;
    });
    probe.remove();
  }

  // ---- painting -------------------------------------------------------------

  // The backing store is sized in device pixels so a stroke is not a blur on a
  // 2× screen, and the context is scaled so everything below can go on
  // thinking in pad units.
  function sizeCanvas() {
    if (!canvas) return;
    const dpr = Math.min(window.devicePixelRatio || 1, 3);
    const rect = canvas.getBoundingClientRect();
    if (!rect.width) return;
    canvas.width = Math.round(rect.width * dpr);
    canvas.height = Math.round(rect.height * dpr);
    repaint();
  }

  function paintStroke(ctx, s) {
    const p = s.pts;
    if (p.length < 2) return;
    ctx.strokeStyle = inks[s.c] || inks[0];
    ctx.lineWidth = DOODLE_WIDTHS[s.w] ?? DOODLE_WIDTHS[0];
    ctx.beginPath();
    ctx.moveTo(p[0], p[1]);
    if (p.length === 2) ctx.lineTo(p[0], p[1]); // a tap, drawn as a dot by the round cap
    for (let i = 2; i < p.length; i += 2) ctx.lineTo(p[i], p[i + 1]);
    ctx.stroke();
  }

  function repaint() {
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    // One transform maps pad units onto the backing store, so the same numbers
    // that will be encoded are the numbers that get drawn.
    const sx = canvas.width / DOODLE_W;
    const sy = canvas.height / DOODLE_H;
    ctx.setTransform(sx, 0, 0, sy, 0, 0);
    ctx.lineCap = "round";
    ctx.lineJoin = "round";
    for (const s of strokes) paintStroke(ctx, s);
    if (live) paintStroke(ctx, live);
  }

  // ---- drawing --------------------------------------------------------------

  function padPoint(e) {
    const r = canvas.getBoundingClientRect();
    return [((e.clientX - r.left) / r.width) * DOODLE_W, ((e.clientY - r.top) / r.height) * DOODLE_H];
  }

  function down(e) {
    if (full || live) return;
    // Capture, so a stroke that leaves the canvas mid-drag keeps arriving here
    // instead of being abandoned over the dialog chrome.
    canvas.setPointerCapture?.(e.pointerId);
    const [x, y] = padPoint(e);
    live = { c: colour, w: width, pts: [x, y] };
    repaint();
    e.preventDefault();
  }

  function move(e) {
    if (!live) return;
    const [x, y] = padPoint(e);
    live.pts.push(x, y);
    repaint();
    e.preventDefault();
  }

  function up(e) {
    if (!live) return;
    canvas.releasePointerCapture?.(e.pointerId);
    // Simplify and quantize HERE, at the moment the hand lifts — the drawing
    // in memory is then already the drawing that will be encoded, so the
    // preview cannot flatter what actually gets sent.
    const s = prepareStroke(live);
    live = null;
    if (s.pts.length >= 2 && strokes.length < MAX_STROKES) {
      const room = MAX_TOTAL_POINTS - doodleTotalPoints(strokes);
      if (room > 0) {
        if (s.pts.length / 2 > room) s.pts = s.pts.slice(0, room * 2);
        strokes.push(s);
      }
    }
    strokeCount = strokes.length;
    pointCount = doodleTotalPoints(strokes);
    repaint();
  }

  function undo() {
    if (!strokes.length) return;
    haptic("light");
    strokes.pop();
    strokeCount = strokes.length;
    pointCount = doodleTotalPoints(strokes);
    repaint();
  }

  function clear() {
    if (!strokes.length) return;
    haptic("light");
    strokes = [];
    strokeCount = 0;
    pointCount = 0;
    repaint();
  }

  async function send() {
    const chId = S.activeChannelId;
    if (!chId || empty || busy) return;
    busy = true;
    try {
      await api.sendMessage(chId, encodeDoodle(strokes), "");
      onClose();
    } catch (err) {
      flash(err);
      busy = false;
    }
  }

  onMount(() => {
    resolveInks(canvas.parentElement);
    sizeCanvas();
    // The dialog is a bottom sheet on a phone and a centred box on a desktop;
    // either can be resized under us, and the backing store has to follow or
    // the strokes go soft.
    const ro = new ResizeObserver(() => sizeCanvas());
    ro.observe(canvas);
    return () => ro.disconnect();
  });
</script>

<Modal title="Draw something" {onClose}>
  <div class="pad-wrap">
    <!-- The pad is an input surface, not an image, so it takes a label and an
         explicit note about how to use it. Drawing needs a pointer; every
         CONTROL below is a real button and reachable from the keyboard. -->
    <canvas
      bind:this={canvas}
      class="pad"
      class:full
      aria-label="Drawing pad — draw with a finger, pen or mouse"
      onpointerdown={down}
      onpointermove={move}
      onpointerup={up}
      onpointercancel={up}
    ></canvas>
    {#if empty}
      <span class="pad-hint" aria-hidden="true">Draw here</span>
    {/if}
  </div>

  <!-- Two labelled groups with a rule between them. They used to be eleven
       identical circles in one row — eight colours and three stroke widths
       drawn as a small dot, a medium dot and a large grey disc — which read as
       three broken colours. A brush is a LINE, so it is drawn as one. -->
  <div class="tools">
    <div class="grp">
      <span class="glabel">Colour</span>
      <div class="inks" role="radiogroup" aria-label="Colour">
      {#each DOODLE_COLOURS as c, i (c.id)}
        <button
          type="button"
          class="ink"
          class:on={colour === i}
          role="radio"
          aria-checked={colour === i}
          aria-label={c.label}
          use:tooltip
          style:--ink={c.css}
          onclick={() => (colour = i)}
        ></button>
        {/each}
      </div>
    </div>

    <span class="grule" aria-hidden="true"></span>

    <div class="grp">
      <span class="glabel">Brush</span>
      <div class="nibs" role="radiogroup" aria-label="Brush size">
        {#each DOODLE_WIDTHS as w, i (w)}
          <button
            type="button"
            class="nib"
            class:on={width === i}
            role="radio"
            aria-checked={width === i}
            aria-label={["Thin", "Medium", "Thick"][i]}
            use:tooltip
            onclick={() => (width = i)}
          >
            <span class="stroke" style:--d="{Math.min(w + 2, 10)}px"></span>
          </button>
        {/each}
      </div>
    </div>

    <div class="spacer"></div>

    <button type="button" class="txtbtn" disabled={empty} onclick={undo}>
      <Icon name="undo" size={14} /> Undo
    </button>
    <button type="button" class="txtbtn" disabled={empty} onclick={clear}>
      <Icon name="trash" size={14} /> Clear
    </button>
  </div>

  <p class="cap" aria-live="polite">
    {#if full}
      That's a full pad — undo a stroke to keep drawing.
    {:else if empty}
      Strokes, not pixels: a doodle is a few hundred bytes and re-themes with the app.
    {:else}
      {plural(strokeCount, "stroke")}, {pointCount} of {MAX_TOTAL_POINTS} points.
    {/if}
  </p>

  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={send} disabled={empty || busy}>Send doodle</button>
  </div>
</Modal>

<style>
  .pad-wrap {
    position: relative;
  }
  /* Only while the pad is empty, and never in the way: pointer-events off, and
     it is gone the moment the first stroke lands. */
  .pad-hint {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    pointer-events: none;
    font-size: var(--fs-ui);
    color: var(--text-faint);
  }
  /* touch-action: none is load-bearing on a phone. Without it the browser
     claims the first vertical movement for a scroll (and the sheet's own
     drag-to-dismiss claims the rest), so a downward stroke dismisses the
     dialog instead of drawing a line. */
  /* A dashed edge and a hint say "draw here". A plain filled rectangle above
     a palette is as likely to read as a preview of nothing. */
  .pad {
    display: block;
    width: 100%;
    aspect-ratio: 8 / 5;
    touch-action: none;
    background: var(--bg-1);
    border: 1px dashed var(--border);
    border-radius: var(--radius-md);
    cursor: crosshair;
  }
  .pad.full {
    cursor: not-allowed;
  }

  .tools {
    display: flex;
    align-items: flex-end;
    gap: var(--sp-3);
    margin-top: var(--sp-3);
    flex-wrap: wrap;
  }
  .grp {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }
  .glabel {
    font-size: var(--fs-tiny);
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  /* The separator the two groups needed. Without it "eight circles then three
     circles" is one row of eleven. */
  .grule {
    align-self: stretch;
    flex: none;
    width: 1px;
    margin: 2px 0;
    background: var(--border);
  }
  .spacer {
    flex: 1;
  }

  .inks,
  .nibs {
    display: flex;
    align-items: center;
    gap: var(--sp-1);
  }
  .ink {
    width: 22px;
    height: 22px;
    padding: 0;
    border-radius: 50%;
    border: 1px solid var(--border);
    background: var(--ink);
    cursor: pointer;
    transition: transform var(--dur-quick) var(--ease-out);
  }
  .ink:hover {
    transform: scale(1.12);
  }
  .ink.on {
    outline: var(--focus-ring);
    outline-offset: var(--focus-ring-offset);
  }

  .nib {
    width: 26px;
    height: 26px;
    padding: 0;
    display: grid;
    place-items: center;
    /* A visible box even when unselected: three bare dots in a row read as
       decoration, not as a control you can press. */
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--bg-2);
    cursor: pointer;
  }
  .nib.on {
    background: var(--bg-3);
    border-color: var(--border);
  }
  /* A stroke, not a dot. A brush weight is a LINE — drawing it as a disc at
     the same size and shape as the colour swatches beside it is what made the
     three of them read as broken colours. */
  .nib .stroke {
    width: 15px;
    height: var(--d);
    border-radius: 999px;
    background: var(--text-muted);
  }
  .nib.on .stroke {
    background: var(--text);
  }
  .nib.on .dot {
    background: var(--text);
  }

  /* Labelled. Two unlabelled 16px glyphs floating under a palette are a
     guess; "Undo" and "Clear" are not, and they are the two actions in this
     dialog you least want to press by mistake. */
  .txtbtn {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    height: 30px;
    padding: 0 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--bg-3);
    color: var(--text-muted);
    font-size: var(--fs-compact);
    cursor: pointer;
  }
  .txtbtn:hover:not(:disabled) {
    color: var(--text);
  }
  .txtbtn:disabled {
    opacity: 0.4;
    cursor: default;
  }

  .cap {
    margin: var(--sp-2) 0 0;
    font-size: var(--fs-small);
    color: var(--text-muted);
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--sp-2);
    margin-top: var(--sp-4);
  }

  @media (max-width: 768px) {
    /* Fingers, not a pointer: the swatches and nibs have to clear the tap
       floor, and the row wraps rather than shrinking them. */
    .ink,
    .nib,
    .iconbtn {
      width: var(--tap-min);
      height: var(--tap-min);
    }
    .ink {
      /* A circle at 48px is a lot of colour; keep the swatch itself the size
         it reads best at and let the padding carry the target. */
      background-clip: content-box;
      padding: var(--sp-3);
    }
  }
</style>
