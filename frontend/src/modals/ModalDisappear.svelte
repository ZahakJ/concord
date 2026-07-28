<script>
  // Pick a disappearing-messages timer for one conversation (this device's
  // choice — see lib/ephemeral). Messages you send here after this carry an
  // expiry and erase on every side when it passes.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S, flash } from "../lib/state.svelte.js";
  import { TTL_OPTIONS, channelTTL, setChannelTTL } from "../lib/ephemeral.svelte.js";

  let { onClose } = $props();

  const channelId = $derived(S.modal?.channelId || S.activeChannelId);
  const current = $derived(channelTTL(channelId));

  function pick(secs) {
    setChannelTTL(channelId, secs);
    flash(secs ? `Messages now disappear after ${TTL_OPTIONS.find((o) => o.secs === secs)?.label}` : "Disappearing messages off", "success");
    onClose();
  }
</script>

<Modal title="Disappearing messages" {onClose}>
  <p class="muted intro">
    New messages you send in this conversation will erase themselves — on every
    device — once the timer runs out. Applies to messages you type from now on.
  </p>
  <div class="opts" role="radiogroup" aria-label="Disappear timer">
    {#each TTL_OPTIONS as o (o.secs)}
      <button
        class="opt"
        class:sel={current === o.secs}
        role="radio"
        aria-checked={current === o.secs}
        onclick={() => pick(o.secs)}
      >
        <span>{o.label}</span>
        {#if current === o.secs}<Icon name="check" size={15} />{/if}
      </button>
    {/each}
  </div>
</Modal>

<style>
  .intro {
    font-size: 13px;
    line-height: 1.5;
    margin: 0 0 12px;
  }
  .opts {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .opt {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 11px 14px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--text);
    font-size: 14px;
    text-align: left;
    transition:
      background 0.12s ease,
      border-color 0.12s ease;
  }
  .opt:hover {
    background: var(--bg-3);
    border-color: var(--accent);
  }
  .opt.sel {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .opt :global(svg) {
    color: var(--accent-hover);
  }
</style>
