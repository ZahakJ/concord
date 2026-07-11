<script>
  // `wide` widens the desktop dialog for content that benefits from the room
  // (sectioned settings); the mobile sheet presentation ignores it.
  let { title, onClose, wide = false, children } = $props();
  let dialog = $state(null);

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
    onclick={(e) => e.stopPropagation()}
    role="presentation"
  >
    <div class="head">
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
    background: rgba(0, 0, 0, 0.55);
    display: grid;
    place-items: center;
    z-index: 100;
    animation: fade 0.12s ease;
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
    animation: pop 0.14s ease;
  }
  .dialog.wide {
    width: 460px;
  }
  @keyframes fade {
    from {
      opacity: 0;
    }
  }
  @keyframes pop {
    from {
      opacity: 0;
      transform: translateY(8px) scale(0.98);
    }
  }
  /* Mobile: dialogs present as full-width bottom sheets instead of floating
     cards — thumb-reachable, roomy, and keyboard-friendly. Desktop (fine
     pointer + wide viewport) is untouched. */
  @media (pointer: coarse), (max-width: 700px) {
    .overlay {
      place-items: end stretch;
    }
    /* Both selectors so `wide` (higher specificity) can't pin the sheet to a
       fixed desktop width. */
    .dialog,
    .dialog.wide {
      width: auto;
      max-width: none;
      border: none;
      border-radius: 16px 16px 0 0;
      padding-bottom: calc(20px + env(safe-area-inset-bottom));
      animation: sheet-up 0.22s cubic-bezier(0.2, 0.9, 0.3, 1);
    }
    /* ≥16px inputs stop iOS auto-zoom on focus; ≥44px buttons are the
       touch-target floor. Reaches into each modal's own markup. */
    .dialog :global(input:not([type="checkbox"]):not([type="radio"])),
    .dialog :global(textarea),
    .dialog :global(select) {
      font-size: 16px;
    }
    .dialog :global(button) {
      min-height: 44px;
    }
    .head .x {
      min-height: 0;
      width: 40px;
      height: 40px;
      display: grid;
      place-items: center;
    }
  }
  @keyframes sheet-up {
    from {
      transform: translateY(100%);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .overlay,
    .dialog {
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
    z-index: 1;
  }
  h3 {
    margin: 0;
    font-size: 16px;
  }
  .x {
    background: transparent;
    color: var(--text-muted);
    padding: 4px 8px;
  }
  .x:hover {
    color: var(--text);
    background: var(--bg-input);
  }
</style>
