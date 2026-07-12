<script>
  // One encrypted image attachment: reserves layout space from the token's
  // dimensions, fetches + decrypts through the backend, and renders with
  // spinner / error+retry states. Clicking opens a Discord-style lightbox:
  // click zooms to the cursor, wheel zooms, drag pans, pinch works on touch,
  // Esc / backdrop / ✕ close.
  import Icon from "./Icon.svelte";
  import { loadAttachment } from "./lib/attachments.js";

  let { channelId, tok } = $props();

  let state = $state("loading"); // loading | done | error
  let src = $state("");
  let errMsg = $state("");

  // Reserve space: scale token dims into the same box the CSS allows.
  const MAXW = 380;
  const MAXH = 280;
  const dims = $derived.by(() => {
    if (!tok.w || !tok.h) return { w: 220, h: 140 };
    const scale = Math.min(MAXW / tok.w, MAXH / tok.h, 1);
    return { w: Math.round(tok.w * scale), h: Math.round(tok.h * scale) };
  });

  async function fetchNow() {
    state = "loading";
    try {
      src = await loadAttachment(channelId, tok);
      state = "done";
    } catch (err) {
      errMsg = String(err?.message || err);
      state = "error";
    }
  }

  $effect(() => {
    tok.blobId;
    fetchNow();
  });

  // ---- lightbox ----
  // The image renders fitted to the viewport; zoom/pan are a transform on top:
  // translate(tx,ty) then scale(s), both around the viewport center.
  const ZMIN = 1;
  const ZMAX = 8;
  let lightbox = $state(false);
  let zoom = $state(1);
  let tx = $state(0);
  let ty = $state(0);
  let dragging = $state(false);
  let overlayEl; // for backdrop-click detection and pointer math
  let imgEl; // for pan clamping (fitted size × zoom = rendered size)

  function openLightbox() {
    zoom = 1;
    tx = 0;
    ty = 0;
    lightbox = true;
  }
  function closeLightbox() {
    lightbox = false;
    pointers.clear();
    dragging = false;
  }

  function onKeydown(e) {
    if (!lightbox) return;
    if (e.key === "Escape") closeLightbox();
    else if (e.key === "+" || e.key === "=") zoomAround(center(), zoom * 1.5);
    else if (e.key === "-") zoomAround(center(), zoom / 1.5);
    else if (e.key === "0") zoomAround(center(), 1);
  }

  function center() {
    const r = overlayEl.getBoundingClientRect();
    return { x: r.left + r.width / 2, y: r.top + r.height / 2 };
  }

  // zoomAround rescales while keeping the point under `p` (client coords)
  // visually fixed — Discord/maps-style anchored zoom. At fit scale (1) the
  // pan resets so the image is always centered when fully zoomed out.
  function zoomAround(p, next) {
    next = Math.min(ZMAX, Math.max(ZMIN, next));
    const c = center();
    const k = next / zoom;
    tx = p.x - c.x - (p.x - c.x - tx) * k;
    ty = p.y - c.y - (p.y - c.y - ty) * k;
    zoom = next;
    if (zoom === 1) {
      tx = 0;
      ty = 0;
    }
    clampPan();
  }

  // clampPan keeps the image on screen: edges never pan past the viewport
  // center-ish, so a wild drag can't strand you on an all-black overlay.
  function clampPan() {
    if (!imgEl || !overlayEl) return;
    const r = overlayEl.getBoundingClientRect();
    const maxX = Math.max(0, (imgEl.offsetWidth * zoom - r.width) / 2) + r.width * 0.25;
    const maxY = Math.max(0, (imgEl.offsetHeight * zoom - r.height) / 2) + r.height * 0.25;
    tx = Math.max(-maxX, Math.min(maxX, tx));
    ty = Math.max(-maxY, Math.min(maxY, ty));
  }

  function onWheel(e) {
    e.preventDefault(); // never scroll the chat behind the overlay
    const factor = Math.exp(-e.deltaY * 0.0015);
    zoomAround({ x: e.clientX, y: e.clientY }, zoom * factor);
  }

  // Pointers: one finger/button drags (pan) — or clicks (zoom toggle) when it
  // never really moved; two fingers pinch.
  const pointers = new Map(); // pointerId -> {x, y}
  let moved = 0; // px travelled since pointerdown (click vs drag)
  let pinchDist = 0;
  let downOnBackdrop = false; // pointer capture retargets pointerup, so record at DOWN

  function onPointerDown(e) {
    if (e.button !== 0 && e.pointerType === "mouse") return;
    if (e.target.closest?.(".lb-bar")) return; // let the ✕ button be a button
    downOnBackdrop = e.target === overlayEl;
    overlayEl.setPointerCapture?.(e.pointerId);
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (pointers.size === 1) moved = 0;
    if (pointers.size === 2) pinchDist = pinchDistance();
  }

  function pinchDistance() {
    const [a, b] = [...pointers.values()];
    return Math.hypot(a.x - b.x, a.y - b.y);
  }

  function onPointerMove(e) {
    const prev = pointers.get(e.pointerId);
    if (!prev) return;
    const cur = { x: e.clientX, y: e.clientY };
    pointers.set(e.pointerId, cur);
    if (pointers.size === 2) {
      // Pinch: zoom around the midpoint, pan with the midpoint drift.
      const [a, b] = [...pointers.values()];
      const mid = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
      const d = pinchDistance();
      if (pinchDist > 0) zoomAround(mid, zoom * (d / pinchDist));
      pinchDist = d;
      moved = 99;
      return;
    }
    const dx = cur.x - prev.x;
    const dy = cur.y - prev.y;
    moved += Math.abs(dx) + Math.abs(dy);
    if (moved > 4 && zoom > 1) {
      dragging = true;
      tx += dx;
      ty += dy;
      clampPan();
    }
  }

  function onPointerUp(e) {
    const wasPinching = pointers.size === 2;
    pointers.delete(e.pointerId);
    if (pointers.size > 0 || wasPinching) return;
    const wasDrag = dragging || moved > 4;
    dragging = false;
    if (wasDrag) return;
    // A clean click: on the backdrop it closes; on the image it toggles zoom
    // at the click point (in), or back to fit (out).
    if (downOnBackdrop) {
      closeLightbox();
    } else if (zoom === 1) {
      zoomAround({ x: e.clientX, y: e.clientY }, 2.5);
    } else {
      zoomAround(center(), 1);
    }
  }
</script>

<svelte:window onkeydown={onKeydown} />

{#if state === "done"}
  <button class="frame done" onclick={openLightbox} title="Click to enlarge">
    <img {src} alt="attachment" style="max-width:{MAXW}px;max-height:{MAXH}px" />
  </button>
  {#if lightbox}
    <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
    <div
      class="lightbox"
      role="dialog"
      aria-modal="true"
      aria-label="Image viewer"
      bind:this={overlayEl}
      onwheel={onWheel}
      onpointerdown={onPointerDown}
      onpointermove={onPointerMove}
      onpointerup={onPointerUp}
      onpointercancel={onPointerUp}
    >
      <img
        {src}
        alt="attachment full size"
        bind:this={imgEl}
        class:zoomed={zoom > 1}
        class:dragging
        style="transform: translate({tx}px, {ty}px) scale({zoom})"
        draggable="false"
      />
      <div class="lb-bar">
        <span class="lb-zoom" class:show={zoom > 1}>{Math.round(zoom * 100)}%</span>
        <button class="lb-close" onclick={closeLightbox} aria-label="Close image (Esc)">
          <Icon name="close" size={18} />
        </button>
      </div>
    </div>
  {/if}
{:else}
  <div class="frame placeholder" style="width:{dims.w}px;height:{dims.h}px">
    {#if state === "loading"}
      <span class="spin"></span>
      <span class="muted small">fetching image…</span>
    {:else}
      <Icon name="close" size={18} />
      <span class="muted small">{errMsg.includes("not found") ? "no one online has this image yet" : "couldn't load image"}</span>
      <button class="ghost retry" onclick={fetchNow}>Retry</button>
    {/if}
  </div>
{/if}

<style>
  .frame {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    margin-top: 4px;
    border-radius: var(--radius-sm);
    overflow: hidden;
  }
  .frame.done {
    background: transparent;
    padding: 0;
    display: block;
    width: fit-content;
    cursor: zoom-in;
  }
  .frame.done:hover {
    background: transparent;
  }
  .frame.done img {
    display: block;
    border-radius: var(--radius-sm);
  }
  .placeholder {
    background: var(--bg-1);
    border: 1px solid var(--border);
    color: var(--text-faint);
    max-width: 380px;
    max-height: 280px;
  }
  .spin {
    width: 20px;
    height: 20px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: rot 0.8s linear infinite;
  }
  @keyframes rot {
    to {
      transform: rotate(360deg);
    }
  }
  .small {
    font-size: 12px;
  }
  .retry {
    padding: 3px 12px;
    font-size: 12px;
  }
  .lightbox {
    position: fixed;
    inset: 0;
    z-index: 300;
    background: rgba(0, 0, 0, 0.85);
    display: grid;
    place-items: center;
    padding: 4vh 4vw;
    overflow: hidden;
    touch-action: none; /* pointer events own pinch/drag */
    animation: lb-in 0.15s ease;
  }
  @keyframes lb-in {
    from {
      opacity: 0;
    }
  }
  .lightbox img {
    max-width: 100%;
    max-height: 100%;
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-pop);
    cursor: zoom-in;
    user-select: none;
    -webkit-user-drag: none;
    transition: transform 0.18s ease;
    will-change: transform;
  }
  .lightbox img.zoomed {
    cursor: grab;
  }
  .lightbox img.dragging {
    cursor: grabbing;
    transition: none; /* 1:1 with the pointer while panning */
  }
  .lb-bar {
    position: absolute;
    top: 14px;
    right: 16px;
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .lb-zoom {
    font-size: 12px;
    font-variant-numeric: tabular-nums;
    color: rgba(255, 255, 255, 0.85);
    background: rgba(0, 0, 0, 0.45);
    border-radius: 999px;
    padding: 3px 10px;
    opacity: 0;
    transition: opacity 0.15s ease;
    pointer-events: none;
  }
  .lb-zoom.show {
    opacity: 1;
  }
  .lb-close {
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    border-radius: 50%;
    background: rgba(0, 0, 0, 0.45);
    color: rgba(255, 255, 255, 0.9);
    border: none;
    cursor: pointer;
  }
  .lb-close:hover {
    background: rgba(255, 255, 255, 0.18);
  }
</style>
