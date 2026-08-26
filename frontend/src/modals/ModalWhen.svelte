<script>
  // A small "pick a time" sheet, shared by scheduled-send and remind-me. The
  // opener passes a title + onPick(epochMs) via S.modal; we present quick
  // presets and a custom date/time, then hand back the chosen timestamp.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S } from "../lib/state.svelte.js";
  import { whenPresets } from "../lib/scheduled.svelte.js";

  let { onClose } = $props();

  const presets = whenPresets();
  const title = $derived(S.modal?.title || "Pick a time");
  const cta = $derived(S.modal?.cta || "Set");

  // Custom picker, defaulted to an hour out, formatted for datetime-local.
  let custom = $state(defaultCustom());
  function defaultCustom() {
    const d = new Date(Date.now() + 60 * 60000 - new Date().getTimezoneOffset() * 60000);
    return d.toISOString().slice(0, 16);
  }
  const customAt = $derived(custom ? new Date(custom).getTime() : NaN);
  const customValid = $derived(!isNaN(customAt) && customAt > Date.now());

  function pick(at) {
    S.modal?.onPick?.(at);
    onClose();
  }
</script>

<Modal {title} {onClose}>
  <div class="when">
    {#each presets as p (p.label)}
      <button class="preset" onclick={() => pick(p.at)}>
        <Icon name="bell" size={15} />
        <span>{p.label}</span>
      </button>
    {/each}
  </div>
  <div class="custom">
    <span class="muted tiny">Or pick a time</span>
    <div class="custom-row">
      <input type="datetime-local" bind:value={custom} />
      <button class="go" disabled={!customValid} onclick={() => pick(customAt)}>{cta}</button>
    </div>
  </div>
</Modal>

<style>
  .when {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--sp-2);
  }
  .preset {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    padding: 10px 12px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--text);
    font-size: var(--fs-ui);
    text-align: left;
    transition:
      background var(--dur-quick) ease,
      border-color var(--dur-quick) ease,
      transform var(--dur-quick) ease;
  }
  .preset:hover {
    background: var(--bg-3);
    border-color: var(--accent);
    transform: translateY(-1px);
  }
  .preset :global(svg) {
    color: var(--accent-hover);
    flex-shrink: 0;
  }
  .custom {
    margin-top: 14px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .custom-row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-2);
  }
  .custom-row input {
    flex: 1 1 60%;
    /* A datetime-local field renders its own segmented spinner and has a real
       intrinsic width; at min-width:0 it squeezed under it and clipped the
       year. Wrapping the button below is the better failure. */
    min-width: 170px;
  }
  .go {
    flex-shrink: 0;
    padding: var(--sp-2) var(--sp-4);
    background: var(--accent);
    color: var(--accent-fg);
    border-radius: var(--radius-md);
  }
  .go:disabled {
    opacity: 0.5;
  }
  .tiny {
    font-size: var(--fs-small);
  }
  /* Two columns put "Tomorrow morning" in ~150px at the narrow floor, where it
     wraps to three lines and the presets stop scanning as a list. */
  @media (max-width: 400px) {
    .when {
      grid-template-columns: 1fr;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .preset:hover {
      transform: none;
    }
  }
</style>
