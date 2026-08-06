<script>
  import Modal from "./Modal.svelte";
  import { S, activeGuild, refreshGuilds, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  let { channel, onSubmit, onClose } = $props();
  let topic = $state(channel?.topic || "");

  // Slow mode rides this modal because it IS channel settings — a per-channel
  // governed interval, set by the same people who set the topic. The op is a
  // signed govOp (manage-channels); enforcement is advisory like mutes.
  const SLOW_OPTS = [
    [0, "Off"],
    [5, "5s"],
    [30, "30s"],
    [60, "1m"],
    [300, "5m"],
    [900, "15m"],
  ];
  let slow = $state(Number(channel?.slowMode) || 0);
  const slowChanged = $derived(slow !== (Number(channel?.slowMode) || 0));

  async function save() {
    if (slowChanged) {
      try {
        await api.setSlowMode(activeGuild()?.id || S.activeGuildId, channel.id, slow);
        refreshGuilds();
      } catch (err) {
        flash(err);
        return;
      }
    }
    onSubmit(topic);
  }
</script>

<Modal title="Channel settings" {onClose}>
  <p class="muted">
    Shown in the header of <strong>#{channel?.name}</strong>. Leave blank to clear it.
  </p>
  <!-- svelte-ignore a11y_autofocus -->
  <textarea
    bind:value={topic}
    rows="3"
    maxlength="300"
    placeholder="What's this channel about?"
    autofocus={!S.isMobile}
  ></textarea>
  <div class="slow">
    <strong class="slow-label">Slow mode</strong>
    <div class="seg" role="radiogroup" aria-label="Slow mode interval">
      {#each SLOW_OPTS as [secs, label] (secs)}
        <button
          class:sel={slow === secs}
          role="radio"
          aria-checked={slow === secs}
          onclick={() => (slow = secs)}
        >{label}</button>
      {/each}
    </div>
    <p class="muted tiny">
      One message per member per interval. Moderators are exempt.
    </p>
  </div>
  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={save}>Save</button>
  </div>
</Modal>

<style>
  p {
    margin: 0;
    font-size: 13px;
  }
  .slow {
    margin-top: 14px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .slow-label {
    font-size: var(--fs-ui);
  }
  .seg {
    display: flex;
    gap: 4px;
  }
  .seg button {
    flex: 1;
    padding: 6px 0;
    font-size: var(--fs-ui);
    border-radius: var(--radius-md);
    background: var(--bg-2);
    border: 1px solid var(--border);
  }
  .seg button.sel {
    background: var(--accent);
    color: var(--accent-fg);
    border-color: var(--accent);
  }
  textarea {
    width: 100%;
    resize: vertical;
    font-family: inherit;
    font-size: 13px;
    padding: 8px 10px;
    margin-top: 8px;
    box-sizing: border-box;
  }
</style>
