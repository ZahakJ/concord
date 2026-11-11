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

<!-- The presets are the shared choice list (app.css .choices): one tap picks
     and closes. The custom time is a labelled field with the dialog's footer
     under it — the commit button used to sit left-aligned in the body. -->
<Modal {title} {onClose}>
  <div class="choices">
    {#each presets as p (p.label)}
      <button class="choice" onclick={() => pick(p.at)}>
        <span>{p.label}</span>
        <Icon name="chevron" size={14} />
      </button>
    {/each}
  </div>
  <label class="custom">
    <span class="lbl">Or pick a time</span>
    <input type="datetime-local" bind:value={custom} />
  </label>
  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button class="primary" disabled={!customValid} onclick={() => pick(customAt)}>{cta}</button>
  </div>
</Modal>

<style>
  .choices :global(svg) {
    color: var(--text-faint);
  }
  .custom {
    margin-top: var(--sp-2);
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .lbl {
    font-size: var(--fs-compact);
    font-weight: 500;
  }
  /* A datetime-local field renders its own segmented spinner and has a real
     intrinsic width; ask the control how much room it needs (the desktop
     build's WebKitGTK draws the AM/PM segment wider than Blink). */
  .custom input {
    min-width: fit-content;
  }
</style>
