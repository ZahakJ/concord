<script>
  // Your own camera, small, mirrored and draggable — the universal convention,
  // and the only defence against broadcasting your ceiling.
  //
  // It appears whenever your camera is on and the stage isn't already showing
  // you at a useful size (see lib/selfview.svelte.js): in theater mode focused
  // on somebody's share, and in every channel that isn't the call's own, where
  // there is no stage at all. Mirrored, because a self-view that isn't is a
  // self-view people wave at the wrong way.
  import { S, getVideoStream } from "./lib/state.svelte.js";
  import { selfViewCovered } from "./lib/selfview.svelte.js";
  import Icon from "./Icon.svelte";

  let { onToggleCamera } = $props();

  const tile = $derived(S.videoTiles.find((t) => t.kind === "camera" && t.self) || null);
  const show = $derived(!!S.cameraOn && !!tile && !selfViewCovered());

  // Where you parked it, remembered per device — same reasoning as the floating
  // dock, and for the same reason: a preview that jumps back to a corner every
  // time it reappears is a preview you stop moving.
  const KEY = "concord.selfViewPos";
  function saved() {
    try {
      const p = JSON.parse(localStorage.getItem(KEY) || "null");
      if (p && Number.isFinite(p.x) && Number.isFinite(p.y)) return p;
    } catch {
      /* absent or corrupt */
    }
    return null;
  }
  let el = $state(null);
  let pos = $state(saved() || { x: 16, y: Math.max(80, window.innerHeight - 200) });
  let drag = null;
  let dragging = $state(false);

  function clamp(x, y) {
    const w = el?.offsetWidth || 160;
    const h = el?.offsetHeight || 120;
    return {
      x: Math.max(8, Math.min(window.innerWidth - w - 8, x)),
      y: Math.max(8, Math.min(window.innerHeight - h - 8, y)),
    };
  }
  function onDown(e) {
    if (e.target.closest?.("button")) return;
    el?.setPointerCapture?.(e.pointerId);
    drag = { dx: e.clientX - pos.x, dy: e.clientY - pos.y };
    dragging = true;
  }
  function onMove(e) {
    if (drag) pos = clamp(e.clientX - drag.dx, e.clientY - drag.dy);
  }
  function onUp(e) {
    if (!drag) return;
    drag = null;
    dragging = false;
    el?.releasePointerCapture?.(e.pointerId);
    try {
      localStorage.setItem(KEY, JSON.stringify(pos));
    } catch {
      /* private mode — it still works, it just forgets */
    }
  }
  // A remembered corner of a big monitor is off the edge of a small one. Only
  // written when it actually moves: `clamp` returns a new object every call,
  // and an effect that reads `pos` and always reassigns it never settles.
  $effect(() => {
    if (!show || !el) return;
    const c = clamp(pos.x, pos.y);
    if (c.x !== pos.x || c.y !== pos.y) pos = c;
  });

  // Bind the MediaStream to the element, not a src attribute.
  function srcObject(node, key) {
    const attach = (k) => (node.srcObject = k ? getVideoStream(k) : null);
    attach(key);
    return { update: attach, destroy: () => (node.srcObject = null) };
  }
</script>

<svelte:window onresize={() => (pos = clamp(pos.x, pos.y))} />

{#if show}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="selfview"
    class:dragging
    bind:this={el}
    style="left:{pos.x}px; top:{pos.y}px"
    onpointerdown={onDown}
    onpointermove={onMove}
    onpointerup={onUp}
    onpointercancel={onUp}
  >
    <!-- svelte-ignore a11y_media_has_caption -->
    <video use:srcObject={tile.key} autoplay playsinline muted></video>
    <span class="sv-tag">You</span>
    <button
      class="sv-off"
      title="Turn off camera"
      aria-label="Turn off camera"
      onclick={() => onToggleCamera?.()}
    >
      <Icon name="cameraOff" size={13} />
    </button>
  </div>
{/if}

<style>
  .selfview {
    position: fixed;
    z-index: 91; /* over the dock, under modals */
    width: 168px;
    aspect-ratio: 4 / 3;
    border-radius: var(--radius-md);
    overflow: hidden;
    background: #000;
    border: 1px solid var(--border);
    box-shadow: var(--shadow-pop);
    cursor: move;
    /* Ours, not the scroller's: without it the browser claims a vertical drag
       and cancels the pointer mid-move (the same trap the dock's header hit). */
    touch-action: none;
    user-select: none;
  }
  .selfview.dragging {
    border-color: var(--accent);
  }
  .selfview video {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
    /* The whole point. An unmirrored preview is a preview you correct for. */
    transform: scaleX(-1);
  }
  .sv-tag {
    position: absolute;
    left: 6px;
    bottom: 6px;
    padding: 1px 7px;
    font-size: var(--fs-tiny);
    font-weight: 600;
    color: #fff;
    background: rgba(0, 0, 0, 0.55);
    border-radius: var(--radius-sm);
    pointer-events: none;
  }
  .sv-off {
    position: absolute;
    top: 5px;
    right: 5px;
    width: 26px;
    height: 26px;
    padding: 0;
    display: grid;
    place-items: center;
    border: none;
    border-radius: 50%;
    color: #fff;
    background: rgba(0, 0, 0, 0.5);
    opacity: 0;
    transition: opacity var(--dur-quick) ease;
  }
  .selfview:hover .sv-off,
  .sv-off:focus-visible {
    opacity: 1;
  }
  .sv-off:hover {
    background: var(--danger);
  }
  @media (pointer: coarse), (max-width: 768px) {
    .selfview {
      width: 118px;
    }
    /* No hover to reveal it with, and the preview is small enough that a 26px
       button over it is most of a face — the camera toggle is in the control
       bar four inches below, where a thumb already is. */
    .sv-off {
      display: none;
    }
  }
</style>
