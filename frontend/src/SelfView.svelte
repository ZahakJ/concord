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
  import { pointOf, rectOf, viewport } from "./lib/place.js";

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
  // Layout pixels — the unit style.left/top are written in. See lib/place.js.
  let pos = $state(saved() || { x: 16, y: Math.max(80, viewport().h - 200) });
  let placed = $state(!!saved()); // a remembered corner needs no default
  let drag = null;
  let dragging = $state(false);

  // ---- what is already living in the corners ----
  //
  // The preview used to open at (16, viewport − 200) — the bottom-left — which
  // is the exact patch of screen the persistent call bar occupies, and that bar
  // is the whole point of leaving the call's channel. Measured at 1440x900 the
  // two overlapped by 112x73px, and because the preview is `position: fixed;
  // z-index: 91` it won the hit test: Return to call, Mute, Deafen and Turn off
  // camera all belonged to the preview, fully painted and completely dead. The
  // two that still worked were Share screen and Hang up.
  //
  // A `pointer-events: none` preview would trade one bug for another — this one
  // is draggable, and that is the defence against broadcasting your ceiling. So
  // the clamp learns the boxes instead: the call bar when there is a call, and
  // the floating dock's own lane. It is a DOM read rather than shared state
  // because both are somebody else's layout, and a number passed between three
  // components is a number that goes stale.
  const OBSTACLES = [".voice-bar", ".dock"];
  const GAP = 12;
  function obstacles() {
    const out = [];
    for (const sel of OBSTACLES) {
      const node = document.querySelector(sel);
      if (!node) continue;
      const r = rectOf(node);
      if (r.w > 0 && r.h > 0) out.push(r);
    }
    return out;
  }
  // Push a box out of one obstacle by the shortest move that clears it, then
  // keep it on screen. Two passes, because clearing the bar can walk you into
  // the dock; a third would only oscillate, so the second answer stands.
  function avoid(box, blocks) {
    let { x, y } = box;
    const vp = viewport();
    for (let pass = 0; pass < 2; pass++) {
      for (const b of blocks) {
        const ox = Math.min(x + box.w, b.x + b.w) - Math.max(x, b.x);
        const oy = Math.min(y + box.h, b.y + b.h) - Math.max(y, b.y);
        if (ox <= -GAP || oy <= -GAP) continue; // already clear, with room
        const up = y + box.h - (b.y - GAP);
        const down = b.y + b.h + GAP - y;
        const left = x + box.w - (b.x - GAP);
        const right = b.x + b.w + GAP - x;
        const moves = [
          [up, () => (y -= up), b.y - GAP - box.h >= 8],
          [down, () => (y += down), b.y + b.h + GAP + box.h <= vp.h - 8],
          [left, () => (x -= left), b.x - GAP - box.w >= 8],
          [right, () => (x += right), b.x + b.w + GAP + box.w <= vp.w - 8],
        ]
          .filter((m) => m[2] && m[0] > 0)
          .sort((p, q) => p[0] - q[0]);
        if (moves.length) moves[0][1]();
      }
    }
    return { x, y };
  }
  function size() {
    return { w: el?.offsetWidth || 168, h: el?.offsetHeight || 126 };
  }
  function clamp(x, y) {
    const { w, h } = size();
    const vp = viewport();
    const inside = {
      x: Math.max(8, Math.min(vp.w - w - 8, x)),
      y: Math.max(8, Math.min(vp.h - h - 8, y)),
    };
    const out = avoid({ ...inside, w, h }, obstacles());
    return {
      x: Math.max(8, Math.min(vp.w - w - 8, out.x)),
      y: Math.max(8, Math.min(vp.h - h - 8, out.y)),
    };
  }
  // Where it opens the first time: the bottom-left corner it has always used,
  // lifted to sit ABOVE the call bar rather than on it.
  function defaultCorner() {
    const { w, h } = size();
    const vp = viewport();
    const bar = obstacles()[0];
    const y = bar ? bar.y - GAP - h : vp.h - 16 - h;
    return clamp(16, Math.max(8, y));
  }
  function onDown(e) {
    if (e.target.closest?.("button")) return;
    el?.setPointerCapture?.(e.pointerId);
    const p = pointOf(e);
    drag = { dx: p.x - pos.x, dy: p.y - pos.y };
    dragging = true;
  }
  function onMove(e) {
    if (drag) {
      const p = pointOf(e);
      pos = clamp(p.x - drag.dx, p.y - drag.dy);
    }
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
  // A remembered corner of a big monitor is off the edge of a small one, and a
  // corner chosen before the call bar arrived is a corner on top of it. Only
  // written when it actually moves: `clamp` returns a new object every call,
  // and an effect that reads `pos` and always reassigns it never settles.
  //
  // S.voice and S.isMobile are read so this re-runs when the obstacles appear
  // or change shape — the bar arrives with the call, the dock with the walk
  // away from it.
  $effect(() => {
    void S.voice;
    void S.isMobile;
    void S.activeChannelId; // the dock mounts on the walk away from the call
    if (!show || !el) return;
    const c = placed ? clamp(pos.x, pos.y) : defaultCorner();
    placed = true;
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
