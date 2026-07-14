<script>
  // BottomSheet — the mobile modal primitive: a panel that slides up from the
  // bottom edge under a scrim, with a grab handle you can drag or fling down
  // to dismiss. Content renders via the children snippet and scrolls
  // independently; only the handle/header region drives the drag, so a
  // scrollable list inside never fights the gesture. Desktop never mounts
  // this — it's the touch counterpart of popovers and context menus.
  let { title = "", onClose, maxHeight = "72vh", children } = $props();

  let sheetEl = $state(null);
  let dragY = $state(0); // translateY while dragging (px, downward only)
  let dragging = $state(false);
  let sheetH = $state(0); // sheet height captured at grab, for the scrim dim
  let prevY = 0;
  let prevT = 0;
  let velocity = 0; // px/ms, positive = downward

  // Scrim tracks the pull: as the sheet slides down, the backdrop lightens —
  // the sheet feels physically attached to the dim behind it (iOS-style).
  const scrimO = $derived(dragging && sheetH ? Math.max(0.3, 1 - dragY / sheetH) : 1);

  function onTouchStart(e) {
    const t = e.touches[0];
    if (!t) return;
    dragging = true;
    sheetH = sheetEl?.offsetHeight || 0;
    prevY = t.clientY;
    prevT = performance.now();
    velocity = 0;
  }
  function onTouchMove(e) {
    if (!dragging) return;
    const t = e.touches[0];
    if (!t) return;
    const now = performance.now();
    if (now > prevT) velocity = (t.clientY - prevY) / (now - prevT);
    dragY = Math.max(0, dragY + (t.clientY - prevY));
    prevY = t.clientY;
    prevT = now;
  }
  function onTouchEnd() {
    if (!dragging) return;
    dragging = false;
    const h = sheetEl?.offsetHeight || 300;
    // Fling down or drag past 40% of the sheet → dismiss; else spring back.
    if (dragY > h * 0.4 || velocity > 0.55) onClose?.();
    else dragY = 0;
  }
</script>

<svelte:window onkeydown={(e) => e.key === "Escape" && onClose?.()} />

<button
  class="bs-scrim"
  class:dragging
  style="opacity:{scrimO}"
  aria-label="Close"
  onclick={onClose}
></button>
<div
  class="bs-sheet"
  class:dragging
  bind:this={sheetEl}
  style="max-height:{maxHeight}; transform:translateY({dragY}px)"
  role="dialog"
  aria-label={title || "Sheet"}
>
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="bs-grab"
    ontouchstart={onTouchStart}
    ontouchmove={onTouchMove}
    ontouchend={onTouchEnd}
    ontouchcancel={onTouchEnd}
  >
    <span class="bs-handle"></span>
    {#if title}<div class="bs-title">{title}</div>{/if}
  </div>
  <div class="bs-body">
    {@render children?.()}
  </div>
</div>

<style>
  .bs-scrim {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    z-index: 400;
    border: none;
    animation: bs-fade 0.16s ease;
    /* Springs the dim back in sync with the sheet on a released half-swipe. */
    transition: opacity 0.18s ease;
  }
  .bs-scrim.dragging {
    transition: none;
  }
  .bs-sheet {
    position: fixed;
    left: 0;
    right: 0;
    bottom: 0;
    z-index: 401;
    display: flex;
    flex-direction: column;
    background: var(--bg-elevated, var(--bg-1));
    border-radius: 16px 16px 0 0;
    box-shadow: var(--shadow-pop);
    animation: bs-up 0.22s cubic-bezier(0.2, 0.9, 0.3, 1);
    transition: transform 0.18s ease;
  }
  .bs-sheet.dragging {
    transition: none;
  }
  .bs-grab {
    flex-shrink: 0;
    padding: 8px 16px 4px;
    cursor: grab;
    /* The grab zone owns its touches — without this the browser treats the
       drag as a scroll/refresh gesture and the sheet stutters. */
    touch-action: none;
  }
  .bs-handle {
    display: block;
    width: 38px;
    height: 4px;
    margin: 0 auto;
    border-radius: 2px;
    background: var(--border);
  }
  .bs-title {
    padding: 10px 2px 6px;
    font-size: 13px;
    font-weight: 700;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .bs-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    -webkit-overflow-scrolling: touch;
    padding: 0 10px calc(12px + env(safe-area-inset-bottom));
  }
  @keyframes bs-fade {
    from {
      opacity: 0;
    }
  }
  @keyframes bs-up {
    from {
      transform: translateY(100%);
    }
  }
</style>
