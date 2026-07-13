<script>
  // The banner editor: one place, three ways in.
  //   • Presets — 40+ LIVE scenes: snow falls, meteors streak, lightning
  //     strikes, lava bubbles, an equalizer dances. They cost a dozen bytes on
  //     the wire ("preset:galaxy"), not a 256KB image.
  //   • Image — upload or paste, then FRAME it: drag to pan, slider to zoom,
  //     and only the part you choose is baked into the banner.
  //   • Colors — your two profile colors, angle included.
  import Icon from "./Icon.svelte";
  import Banner from "./Banner.svelte";
  import { BANNERS, BANNER_GROUPS, isPreset, presetId } from "./lib/banners.js";

  let {
    banner = "",
    color = "#14a394",
    color2 = "",
    angle = 120,
    overlay = "", // NOT `effect`: that name shadows the $effect rune
    onApply,
    onClose,
  } = $props();

  // An overlay that plays ON TOP of whatever banner you chose — preset, image
  // or colors. It lives here, with the banner, because that's the only thing it
  // affects (it used to be a mystery field on the profile page).
  const OVERLAYS = [
    { id: "", label: "None" },
    { id: "aurora", label: "Aurora" },
    { id: "sheen", label: "Sheen" },
    { id: "sparkle", label: "Sparkle" },
    { id: "nebula", label: "Nebula" },
  ];
  let fx = $state(overlay);

  // The banner box the card actually shows: 3:1-ish.
  const OUT_W = 900;
  const OUT_H = 300;
  const VIEW_W = 420;
  const VIEW_H = 140;

  let tab = $state(isPreset(banner) ? "presets" : banner ? "image" : "presets");
  let sel = $state(isPreset(banner) ? presetId(banner) : "");
  let ang = $state(angle);

  // ---- image framing ----
  let rawImg = $state(null); // HTMLImageElement being framed
  let zoom = $state(1);
  let dragX = $state(0);
  let dragY = $state(0);
  let canvas = $state(null);
  let fileInput;
  let dragging = false;
  let last = { x: 0, y: 0 };

  function loadFile(file) {
    if (!file || !file.type.startsWith("image/")) return;
    const img = new Image();
    const url = URL.createObjectURL(file);
    img.onload = () => {
      URL.revokeObjectURL(url);
      rawImg = img;
      zoom = 1;
      dragX = 0;
      dragY = 0;
      tab = "image";
    };
    img.onerror = () => URL.revokeObjectURL(url);
    img.src = url;
  }

  function onPaste(e) {
    const item = [...(e.clipboardData?.items || [])].find((i) => i.type.startsWith("image/"));
    if (item) {
      e.preventDefault();
      loadFile(item.getAsFile());
    }
  }

  // Draw the framed crop into a canvas of the given size; returns clamped drag.
  function draw(cv, w, h) {
    if (!rawImg || !cv) return;
    const iw = rawImg.naturalWidth;
    const ih = rawImg.naturalHeight;
    const base = Math.max(w / iw, h / ih); // cover
    const eff = base * zoom;
    const dw = iw * eff;
    const dh = ih * eff;
    const slackX = Math.max(0, (dw - w) / 2);
    const slackY = Math.max(0, (dh - h) / 2);
    const scale = w / VIEW_W;
    const dx = Math.max(-slackX, Math.min(slackX, dragX * scale));
    const dy = Math.max(-slackY, Math.min(slackY, dragY * scale));
    const ctx = cv.getContext("2d");
    ctx.clearRect(0, 0, w, h);
    ctx.drawImage(rawImg, (w - dw) / 2 + dx, (h - dh) / 2 + dy, dw, dh);
    return { dx: dx / scale, dy: dy / scale };
  }

  $effect(() => {
    // Re-render the framing preview whenever the image or its transform moves.
    zoom;
    dragX;
    dragY;
    rawImg;
    if (canvas && rawImg) {
      const clamped = draw(canvas, VIEW_W, VIEW_H);
      if (clamped && (clamped.dx !== dragX || clamped.dy !== dragY)) {
        dragX = clamped.dx;
        dragY = clamped.dy;
      }
    }
  });

  function onDown(e) {
    if (!rawImg) return;
    dragging = true;
    last = { x: e.clientX, y: e.clientY };
    e.currentTarget.setPointerCapture?.(e.pointerId);
  }
  function onMove(e) {
    if (!dragging) return;
    dragX += e.clientX - last.x;
    dragY += e.clientY - last.y;
    last = { x: e.clientX, y: e.clientY };
  }
  const onUp = () => (dragging = false);

  function apply() {
    if (tab === "presets" && sel) return onApply({ banner: `preset:${sel}`, angle: ang, effect: fx });
    if (tab === "image" && rawImg) {
      const cv = document.createElement("canvas");
      cv.width = OUT_W;
      cv.height = OUT_H;
      draw(cv, OUT_W, OUT_H);
      return onApply({ banner: cv.toDataURL("image/jpeg", 0.85), angle: ang, effect: fx });
    }
    // "Your colors" (and a cleared banner) both mean: no banner asset — the
    // card falls back to the member's own gradient, at the angle they chose.
    onApply({ banner: "", angle: ang, effect: fx });
  }
</script>

<svelte:window onpaste={onPaste} />

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="bs-scrim" onclick={(e) => e.target === e.currentTarget && onClose()}>
  <div class="bs" role="dialog" aria-label="Edit banner">
    <div class="bs-head">
      <strong>Edit banner</strong>
      <button class="x" onclick={onClose} aria-label="Close"><Icon name="close" size={14} /></button>
    </div>

    <div class="tabs">
      <button class:on={tab === "presets"} onclick={() => (tab = "presets")}>Presets</button>
      <button class:on={tab === "image"} onclick={() => (tab = "image")}>Image</button>
      <button class:on={tab === "colors"} onclick={() => (tab = "colors")}>Your colors</button>
    </div>

    {#if tab === "presets"}
      <!-- The selection, shown at real size and really moving. -->
      <div class="hero-wrap card-effect-{fx || 'none'}">
        <Banner banner={sel ? `preset:${sel}` : ""} {color} {color2} style={{ angle: ang }} class="hero banner" />
      </div>
      <div class="shelves">
        {#each BANNER_GROUPS as g (g)}
          <div class="gtitle">{g}</div>
          <div class="grid">
            {#each BANNERS.filter((b) => b.group === g) as p (p.id)}
              <button class="tile" class:sel={sel === p.id} onclick={() => (sel = p.id)} title={p.name}>
                <Banner banner={`preset:${p.id}`} scale={0.45} class="art" />
                <span class="tname">{p.name}</span>
                {#if p.fx}<span class="anim-badge" title="Animated">✨</span>{/if}
              </button>
            {/each}
          </div>
        {/each}
      </div>
    {:else if tab === "image"}
      <div class="img-pane">
        {#if rawImg}
          <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
          <canvas
            bind:this={canvas}
            width={VIEW_W}
            height={VIEW_H}
            class="frame"
            onpointerdown={onDown}
            onpointermove={onMove}
            onpointerup={onUp}
            onpointercancel={onUp}
          ></canvas>
          <label class="zoom">
            <span class="tiny muted">Zoom</span>
            <input type="range" min="1" max="3" step="0.01" bind:value={zoom} />
          </label>
          <p class="tiny muted">Drag the image to choose exactly what shows.</p>
          <button class="ghost small" onclick={() => (rawImg = null)}>Pick another image</button>
        {:else}
          <button class="drop" onclick={() => fileInput.click()}>
            <Icon name="attach" size={22} />
            <strong>Upload an image</strong>
            <span class="tiny muted">…or just paste one (⌘/Ctrl+V)</span>
          </button>
        {/if}
        <input
          type="file"
          accept="image/*"
          bind:this={fileInput}
          style="display:none"
          onchange={(e) => {
            loadFile(e.target.files?.[0]);
            e.target.value = "";
          }}
        />
      </div>
    {:else}
      <div class="colors-pane">
        <div class="hero-wrap card-effect-{fx || 'none'}">
          <Banner banner="" {color} {color2} style={{ angle: ang }} class="hero banner" />
        </div>
        <label class="zoom">
          <span class="tiny muted">Angle · {ang}°</span>
          <input type="range" min="0" max="360" step="5" bind:value={ang} />
        </label>
        <p class="tiny muted">Uses the two profile colors you picked below the banner.</p>
      </div>
    {/if}

    <div class="overlays">
      <span class="tiny muted">Overlay</span>
      <div class="ovs">
        {#each OVERLAYS as o (o.id)}
          <button class="ov" class:on={fx === o.id} onclick={() => (fx = o.id)}>{o.label}</button>
        {/each}
      </div>
    </div>

    <div class="bs-foot">
      <button class="ghost" onclick={() => onApply({ banner: "", angle: ang, effect: fx })}>
        Remove banner
      </button>
      <span class="spacer"></span>
      <button class="ghost" onclick={onClose}>Cancel</button>
      <button onclick={apply}>Apply</button>
    </div>
  </div>
</div>

<style>
  .bs-scrim {
    position: fixed;
    inset: 0;
    z-index: 400;
    /* No backdrop blur: it would re-blur the whole animated app behind this
       dialog every frame (measured ~17fps of the cost). */
    background: rgba(0, 0, 0, 0.68);
    display: grid;
    place-items: center;
    padding: 4vh 4vw;
    animation: bsf 0.15s ease;
  }
  @keyframes bsf {
    from {
      opacity: 0;
    }
  }
  .bs {
    width: 560px;
    max-width: 94vw;
    max-height: 88vh;
    display: flex;
    flex-direction: column;
    gap: 10px;
    background: var(--bg-elevated, var(--bg-1));
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
    padding: 14px;
    animation: bsp 0.2s cubic-bezier(0.34, 1.4, 0.5, 1);
  }
  @keyframes bsp {
    from {
      opacity: 0;
      transform: translateY(10px) scale(0.97);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .bs-scrim,
    .bs {
      animation: none;
    }
  }
  .bs-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .x {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    padding: 0;
    line-height: 0;
    border: none;
    border-radius: 50%;
    background: transparent;
    color: var(--text-muted);
  }
  .x:hover {
    background: var(--bg-3);
  }
  .tabs {
    display: flex;
    gap: 6px;
  }
  .tabs button {
    padding: 6px 14px;
    font-size: 12.5px;
    border-radius: 999px;
    border: 1px solid var(--border);
    background: transparent;
    color: var(--text-muted);
  }
  .tabs button.on {
    border-color: var(--accent);
    background: var(--accent-soft);
    color: var(--text);
  }
  /* The hero: the chosen scene at card size, animating for real — with the
     overlay playing over it, exactly as the profile card will show it. */
  .hero-wrap {
    position: relative;
    flex: none;
    border-radius: var(--radius-md);
    border: 1px solid var(--border);
    overflow: hidden;
  }
  .bs :global(.hero) {
    height: 118px;
  }
  .overlays {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .ovs {
    display: flex;
    gap: 5px;
    flex-wrap: wrap;
  }
  .ov {
    padding: 4px 11px;
    font-size: 12px;
    border-radius: 999px;
    border: 1px solid var(--border);
    background: transparent;
    color: var(--text-muted);
  }
  .ov:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .ov.on {
    border-color: var(--accent);
    background: var(--accent-soft);
    color: var(--text);
  }
  .shelves {
    overflow-y: auto;
    padding: 2px;
  }
  .gtitle {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-faint);
    margin: 8px 0 4px;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 8px;
  }
  /* Offscreen tiles don't paint and don't animate: with 43 live scenes in the
     grid, rendering only what's actually scrolled into view is the difference
     between a smooth picker and a spinning fan. */
  .tile {
    content-visibility: auto;
    contain-intrinsic-size: 120px 62px;
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 0;
    background: transparent;
    border: 2px solid transparent;
    border-radius: 9px;
    cursor: pointer;
    overflow: hidden;
  }
  .tile.sel {
    border-color: var(--accent);
  }
  /* Idle tiles hold still: the effects only run under the cursor (and on the
     one you picked). 43 live scenes at once is a laptop fan; one is delightful. */
  .tile:not(:hover):not(.sel) :global(.fxfield) {
    display: none;
  }
  .tile:not(:hover):not(.sel) :global(.drift) {
    animation-play-state: paused;
  }
  .tile :global(.art) {
    display: block;
    width: 100%;
    height: 46px;
    border-radius: 6px;
  }
  .tname {
    font-size: 10.5px;
    color: var(--text-muted);
    text-align: center;
    padding-bottom: 3px;
  }
  .anim-badge {
    position: absolute;
    top: 3px;
    right: 4px;
    font-size: 10px;
  }
  .img-pane,
  .colors-pane {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
  }
  .frame {
    border-radius: 8px;
    border: 1px solid var(--border);
    cursor: grab;
    touch-action: none;
  }
  .frame:active {
    cursor: grabbing;
  }
  .zoom {
    display: flex;
    flex-direction: column;
    gap: 3px;
    width: 100%;
    max-width: 420px;
  }
  .drop {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    width: 100%;
    padding: 28px;
    border: 1px dashed var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-0);
    color: var(--text-muted);
  }
  .drop:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .small {
    font-size: 12px;
    padding: 5px 12px;
  }
  .tiny {
    font-size: 11px;
    margin: 0;
  }
  .bs-foot {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .spacer {
    flex: 1;
  }
</style>
