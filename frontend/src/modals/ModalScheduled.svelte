<script>
  // Manage everything time-based on this device: queued scheduled messages and
  // pending message reminders, each with when it fires, where it lives, and a
  // cancel. Jumping takes you to the channel/message.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S, jumpToChannel } from "../lib/state.svelte.js";
  import {
    scheduled,
    reminders,
    cancelScheduled,
    cancelReminder,
    whenLabel,
  } from "../lib/scheduled.svelte.js";

  let { onClose } = $props();

  function channelName(channelId) {
    for (const g of S.guilds) {
      const c = g.channels?.find((x) => x.id === channelId);
      if (c) return g.kind === "dm" ? g.name : `#${c.name}`;
    }
    return "a channel";
  }
  function go(channelId) {
    jumpToChannel(channelId);
    onClose();
  }
</script>

<Modal title="Scheduled & reminders" {onClose}>
  <section>
    <strong class="label">Scheduled messages</strong>
    {#if scheduled.length === 0}
      <p class="muted empty">Nothing queued. Write a message, then use the clock to send it later.</p>
    {:else}
      {#each scheduled as s (s.id)}
        <div class="row">
          <button class="rmain" onclick={() => go(s.channelId)} title="Go to channel">
            <span class="txt">{s.text}</span>
            <span class="sub muted">{channelName(s.channelId)} · {whenLabel(s.at)}</span>
          </button>
          <button class="x" onclick={() => cancelScheduled(s.id)} aria-label="Cancel" title="Cancel">
            <Icon name="close" size={13} />
          </button>
        </div>
      {/each}
    {/if}
  </section>

  <hr />

  <section>
    <strong class="label">Reminders</strong>
    {#if reminders.length === 0}
      <p class="muted empty">
        No reminders. {S.isMobile ? "Long-press" : "Right-click"} a message → “Remind me”.
      </p>
    {:else}
      {#each reminders as r (r.id)}
        <div class="row">
          <button class="rmain" onclick={() => go(r.channelId)} title="Go to message">
            <span class="txt">{r.preview || "a message"}</span>
            <span class="sub muted">{channelName(r.channelId)} · {whenLabel(r.at)}</span>
          </button>
          <button class="x" onclick={() => cancelReminder(r.id)} aria-label="Cancel" title="Cancel">
            <Icon name="close" size={13} />
          </button>
        </div>
      {/each}
    {/if}
  </section>
</Modal>

<style>
  section {
    display: flex;
    flex-direction: column;
    gap: 6px;
    text-align: left;
  }
  .label {
    font-size: var(--fs-small);
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  .empty {
    font-size: var(--fs-ui);
    margin: 2px 0 0;
  }
  hr {
    border: none;
    border-top: 1px solid var(--border);
    margin: 12px 0;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .rmain {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 8px 10px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    text-align: left;
    color: var(--text);
    transition:
      background 0.12s ease,
      border-color 0.12s ease;
  }
  .rmain:hover {
    background: var(--bg-3);
    border-color: var(--accent);
  }
  .txt {
    font-size: var(--fs-ui);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sub {
    font-size: var(--fs-tiny);
  }
  .x {
    flex-shrink: 0;
    width: 26px;
    height: 26px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    color: var(--text-muted);
  }
  .x:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  /* Cancel is destructive and sits 6px from a row that navigates away; 26px is
     well under the floor. Modal's mobile rule only stretches the height. */
  @media (pointer: coarse) {
    .row {
      gap: 10px;
    }
    .x {
      width: var(--tap-min);
      height: var(--tap-min);
    }
  }
</style>
