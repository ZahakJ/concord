<script>
  // In-app replacement for window.confirm(): consistent styling, keyboard
  // accessible. CANCEL is focused on open — a stray Enter right after the
  // dialog appears must never fire the destructive action; Tab (or a click)
  // reaches the confirm button deliberately.
  import Modal from "./Modal.svelte";
  import { S } from "../lib/state.svelte.js";
  import { haptic } from "../lib/touch.js";

  let { title = "Are you sure?", body = "", confirmLabel = "Confirm", danger = true, onConfirm, onClose } = $props();

  let cancelBtn;
  // Focusing the cancel button on a phone pops nothing (it isn't a field) but
  // it also isn't needed: there is no stray Enter to guard against, and the
  // focus ring on a full-width button reads as "this one is selected".
  $effect(() => {
    if (!S.isMobile) cancelBtn?.focus();
  });

  function confirm() {
    haptic(danger ? "heavy" : "medium"); // the last acknowledgement before it happens
    onConfirm?.();
  }
</script>

<Modal {title} {onClose}>
  <p>{body}</p>
  <div class="actions">
    <button bind:this={cancelBtn} class="ghost" onclick={onClose}>Cancel</button>
    <button class:danger onclick={confirm}>{confirmLabel}</button>
  </div>
</Modal>

<style>
  p {
    font-size: var(--fs-ui);
    color: var(--text-muted);
    line-height: 1.5;
    margin: 0 0 6px;
  }
  button.danger {
    background: var(--danger);
    /* A soft halo keeps the destructive choice unmistakable. */
    box-shadow: 0 0 12px color-mix(in srgb, var(--danger) 35%, transparent);
    transition: background var(--dur-standard) ease;
  }
  button.danger:hover {
    background: color-mix(in srgb, var(--danger) 85%, white);
  }
  button.danger:active {
    background: color-mix(in srgb, var(--danger) 75%, black);
  }
  /* Phone: two equal, full-width choices — no tiny side-by-side chips. */
  @media (pointer: coarse), (max-width: 768px) {
    .actions {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 10px;
    }
    .actions button {
      min-height: 48px;
    }
  }
</style>
