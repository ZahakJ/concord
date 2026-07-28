<script>
  import { onDestroy } from "svelte";
  import { S, modalNav, backPanel } from "../lib/state.svelte.js";
  import Icon from "../Icon.svelte";
  // `wide` widens the desktop dialog for content that benefits from the room
  // (sectioned settings); `size="xl"` makes it a large workspace (the advanced
  // composer). The mobile sheet presentation ignores both.
  let { title, onClose, wide = false, size = "", children } = $props();
  let dialog = $state(null);

  // Back is offered whenever there's somewhere to go back TO — either a panel
  // on the stack we drilled through, or a plain `from` on a panel opened
  // directly.
  const canBack = $derived(S.modalStack.length > 0 || !!S.modal?.from);

  // Settings and its sub-panels read as one stack you move through, not a pile
  // of unrelated dialogs: a panel opened from another slides in from the right,
  // and going back slides in from the left. Read once at mount (a CSS animation
  // only runs then) and consumed, so the next open starts from a clean slate.
  const enterDir = modalNav.dir || (S.modal?.from ? 1 : 0);
  modalNav.dir = 0;

  // Closing for real drops the whole trail; navigating to another panel keeps
  // it. Which happened is simply whether a modal is still open by the time this
  // one is torn down.
  onDestroy(() => {
    if (!S.modal) S.modalStack = [];
  });

  // Mobile: the sheet can be flicked/dragged DOWN to dismiss — the native
  // gesture people expect, so they don't have to reach the tiny ✕ in the top
  // corner one-handed. Grab starts on the grip/header; if the body has scrolled,
  // a downward drag scrolls it first (we only start dismissing at scrollTop 0).
  let dragY = $state(0);
  let dragging = false;
  let startY = 0;
  let startT = 0;
  function onGrab(e) {
    if (!S.isMobile) return;
    dragging = true;
    startY = e.clientY;
    startT = Date.now();
    e.currentTarget.setPointerCapture?.(e.pointerId);
  }
  function onDrag(e) {
    if (!dragging) return;
    const dy = e.clientY - startY;
    // Only pull down (never up), and only when the content is scrolled to top.
    if (dy > 0 && (dialog?.scrollTop ?? 0) <= 0) dragY = dy;
  }
  function onRelease(e) {
    if (!dragging) return;
    dragging = false;
    const dist = dragY;
    const speed = dragY / Math.max(1, Date.now() - startT); // px/ms
    if (dist > 120 || speed > 0.6) onClose();
    else dragY = 0; // snap back
  }

  // Escape closes reliably regardless of focus (the overlay keydown only fired
  // when focus was inside it). Tab is trapped within the dialog so focus can't
  // wander onto the page behind. Focus/return is handled by the browser's
  // inert-less default plus the initial autofocus in each modal's first field.
  function onKeydown(e) {
    if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    } else if (e.key === "Tab" && dialog) {
      const f = dialog.querySelectorAll(
        'a[href],button:not([disabled]),textarea,input,select,[tabindex]:not([tabindex="-1"])',
      );
      if (!f.length) return;
      const first = f[0];
      const last = f[f.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }
  }
</script>

<svelte:window onkeydown={onKeydown} />

<div class="overlay" onclick={onClose} role="presentation">
  <div
    bind:this={dialog}
    class="dialog"
    class:wide
    class:xl={size === "xl"}
    class:deeper={enterDir === 1}
    class:shallower={enterDir === -1}
    class:dragging
    style={dragY ? `transform:translateY(${dragY}px)` : ""}
    onclick={(e) => e.stopPropagation()}
    role="presentation"
  >
    <!-- Mobile drag-to-dismiss grip (hidden on desktop via CSS). -->
    <div
      class="grip"
      onpointerdown={onGrab}
      onpointermove={onDrag}
      onpointerup={onRelease}
      onpointercancel={onRelease}
      role="presentation"
    ></div>
    <div
      class="head"
      onpointerdown={onGrab}
      onpointermove={onDrag}
      onpointerup={onRelease}
      onpointercancel={onRelease}
      role="presentation"
    >
      {#if canBack}
        <button class="back" onclick={backPanel} aria-label="Back" title="Back">
          <Icon name="chevron" size={16} />
        </button>
      {/if}
      <h3>{title}</h3>
      <button class="x" onclick={onClose} aria-label="Close">✕</button>
    </div>
    {@render children()}
  </div>
</div>

<style>
  .overlay {
    position: fixed;
    inset: 0;
    /* Frosted scrim: the app dims AND recedes, so the dialog reads as the
       only in-focus surface. */
    background: rgba(0, 0, 0, 0.55);
    display: grid;
    place-items: center;
    z-index: 100;
    animation: fade 0.16s ease;
  }
  .dialog {
    width: 380px;
    max-width: 90vw;
    /* Never taller than the viewport; scroll inside on short screens (laptops)
       so long content like the 24-word recovery phrase stays reachable. */
    max-height: 90vh;
    overflow-y: auto;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 20px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    box-shadow: var(--shadow-pop);
    /* A touch of spring on entry — overshoots ~1% then settles. */
    animation: pop 0.26s cubic-bezier(0.34, 1.4, 0.5, 1);
  }
  .dialog.wide {
    width: 460px;
  }
  /* A large workspace (the advanced composer): most of the viewport, and a
     fixed tall height so its inner panes get real room instead of collapsing to
     content height. */
  .dialog.xl {
    width: min(1080px, 94vw);
    height: min(780px, 88vh);
  }
  @keyframes fade {
    from {
      opacity: 0;
    }
  }
  @keyframes pop {
    from {
      opacity: 0;
      transform: translateY(14px) scale(0.95);
    }
  }
  .grip {
    display: none; /* desktop: no drag handle */
  }
  /* Mobile: dialogs present as full-width bottom sheets instead of floating
     cards — thumb-reachable, roomy, and keyboard-friendly. Desktop (fine
     pointer + wide viewport) is untouched. */
  @media (pointer: coarse), (max-width: 700px) {
    .overlay {
      place-items: end stretch;
    }
    /* All variants collapse to the bottom-sheet presentation; higher-specificity
       selectors (`wide`, `xl`) must be listed so they can't pin a desktop size. */
    .dialog,
    .dialog.wide,
    .dialog.xl {
      width: auto;
      max-width: none;
      height: auto;
      max-height: 92vh;
      border: none;
      border-radius: 18px 18px 0 0;
      padding-bottom: calc(20px + env(safe-area-inset-bottom));
      animation: sheet-up 0.28s cubic-bezier(0.22, 1.1, 0.36, 1);
      touch-action: pan-y;
    }
    /* Snap back smoothly when a drag doesn't cross the dismiss threshold. */
    .dialog:not(.dragging) {
      transition: transform 0.24s cubic-bezier(0.22, 1.1, 0.36, 1);
    }
    /* The pill grip — the universal "grab me and pull down" affordance. */
    .grip {
      display: block;
      align-self: center;
      width: 40px;
      height: 5px;
      margin: -8px 0 6px;
      border-radius: 999px;
      background: var(--border);
      flex: none;
      cursor: grab;
      touch-action: none;
    }
    .head {
      touch-action: none;
    }
    /* ≥16px inputs stop iOS auto-zoom on focus; ≥44px buttons are the
       touch-target floor. Reaches into each modal's own markup. */
    .dialog :global(input:not([type="checkbox"]):not([type="radio"])),
    .dialog :global(textarea),
    .dialog :global(select) {
      font-size: 16px;
    }
    /* The floor applied to buttons only, so the pickers and text fields a
       settings row hosts stayed 33-38px tall. */
    .dialog :global(button),
    .dialog :global(input:not([type="checkbox"]):not([type="radio"])),
    .dialog :global(select) {
      min-height: 44px;
    }
    /* Dismiss and go-back are the two controls every sheet has; they were the
       only ones that floor didn't reach (the close button opted out of
       min-height, and .back is sized by its padding). */
    .head .x {
      min-height: 44px;
      width: 44px;
      display: grid;
      place-items: center;
    }
    .head .back {
      min-width: 44px;
      display: grid;
      place-items: center;
    }
  }
  @keyframes sheet-up {
    from {
      transform: translateY(100%);
    }
  }
  /* Navigating the settings stack: a panel opened from another slides in from
     the right, going back slides in from the left, so depth reads as movement
     between places instead of one dialog blinking into another. Declared after
     both the desktop and mobile entrances so it wins over either — on a phone a
     stack should push sideways too, which is what the sheet is already doing
     underneath. */
  .dialog.deeper,
  .dialog.shallower {
    animation: panel-in 0.28s cubic-bezier(0.22, 1, 0.36, 1);
  }
  .dialog.deeper {
    --panel-from: 42px;
  }
  .dialog.shallower {
    --panel-from: -42px;
  }
  @keyframes panel-in {
    from {
      opacity: 0;
      transform: translateX(var(--panel-from)) scale(0.99);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .overlay,
    .dialog,
    .dialog.deeper,
    .dialog.shallower {
      animation: none;
    }
  }
  .head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    /* Keep the title + close button visible while the body scrolls. */
    position: sticky;
    top: -20px;
    margin: -20px -20px 0;
    padding: 20px 20px 8px;
    background: var(--bg-elevated);
    /* Comfortably above anything a modal's body might layer internally — at
       z-index 1 it tied with ordinary content and lost on DOM order, letting
       scrolled content slide over the title. */
    z-index: 3;
  }
  h3 {
    margin: 0;
    font-size: 16.5px;
    font-weight: 700;
    letter-spacing: 0.01em;
  }
  /* Back arrow sits before the title; title takes the slack so ✕ stays right. */
  .head h3 {
    margin-right: auto;
  }
  .back {
    display: grid;
    place-items: center;
    background: transparent;
    color: var(--text-muted);
    padding: 4px 6px;
    margin: 0 4px 0 -4px;
    border-radius: 8px;
    transition:
      color 0.15s ease,
      background 0.15s ease;
  }
  .back :global(svg) {
    transform: rotate(180deg);
  }
  .back:hover {
    color: var(--text);
    background: var(--bg-input);
  }
  .x {
    background: transparent;
    color: var(--text-muted);
    padding: 4px 8px;
    border-radius: 8px;
    transition:
      color 0.15s ease,
      background 0.15s ease,
      transform 0.2s cubic-bezier(0.34, 1.56, 0.64, 1);
  }
  .x:hover {
    color: var(--text);
    background: var(--bg-input);
    transform: rotate(90deg);
  }
  @media (prefers-reduced-motion: reduce) {
    .x {
      transition: none;
    }
  }
</style>
