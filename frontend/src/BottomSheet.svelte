<script>
  // BottomSheet — the mobile modal primitive: a panel that slides up from the
  // bottom edge under a scrim, with a grab handle you can drag or fling down
  // to dismiss. Content renders via the children snippet and scrolls
  // independently; only the handle/header region drives the drag, so a
  // scrollable list inside never fights the gesture. Desktop never mounts
  // this — it's the touch counterpart of popovers and context menus.
  import { haptic } from "./lib/touch.js";

  // dvh, not vh: Android's WebView does not shrink 100vh when the software
  // keyboard opens, so a sheet holding an input measured itself against the
  // whole screen and put its own field underneath the keyboard. The .bs-sheet
  // rule carries a vh value as the fallback for engines that drop the unit.
  let { title = "", onClose, maxHeight = "72dvh", children } = $props();

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
    if (dragY > h * 0.4 || velocity > 0.55) {
      // The gesture committed — say so in the hand, the way the platform's own
      // sheets do. Silent on web/desktop.
      haptic("light");
      onClose?.();
    } else dragY = 0;
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
    /* Fallback for engines without dvh: the inline max-height above carries the
       dvh value and is simply dropped there, leaving this one standing. */
    max-height: 72vh;
    box-shadow: var(--shadow-pop);
    animation: bs-up 0.22s cubic-bezier(0.2, 0.9, 0.3, 1);
    transition: transform 0.18s ease;
  }
  .bs-sheet.dragging {
    transition: none;
  }
  .bs-grab {
    flex-shrink: 0;
    /* The pill itself is 4px tall; a thumb aims at the strip, not the pill, so
       the strip has to be worth aiming at. */
    padding: 12px 16px 8px;
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
  /* This is the title of every sheet in the app, and it names a real thing —
     a guild, a person, a channel. It was set as a micro-label: 13px, uppercase
     and tracked out, the least legible combination in the vocabulary. Let a
     title read as a title. */
  .bs-title {
    padding: 8px 4px 6px;
    font-size: var(--fs-body);
    font-weight: 600;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .bs-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    -webkit-overflow-scrolling: touch;
    /* Momentum that reaches the end of the sheet stops there. Without this the
       remainder is handed to the message feed behind the scrim, which is the
       classic "the wrong thing moved" feeling. */
    overscroll-behavior: contain;
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
