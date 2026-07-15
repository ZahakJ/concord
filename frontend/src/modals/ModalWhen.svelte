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
    gap: 8px;
  }
  .preset {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 12px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--text);
    font-size: 13px;
    text-align: left;
    transition:
      background 0.12s ease,
      border-color 0.12s ease,
      transform 0.12s ease;
  }
  .preset:hover {
    background: var(--bg-3);
    border-color: var(--accent);
    transform: translateY(-1px);
  }
  .preset :global(svg) {
    color: var(--accent);
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
    gap: 8px;
  }
  .custom-row input {
    flex: 1;
    min-width: 0;
  }
  .go {
    flex-shrink: 0;
    padding: 8px 16px;
    background: var(--accent);
    color: #fff;
    border-radius: var(--radius-md);
  }
  .go:disabled {
    opacity: 0.5;
  }
  .tiny {
    font-size: 11px;
  }
  @media (prefers-reduced-motion: reduce) {
    .preset:hover {
      transform: none;
    }
  }
</style>
