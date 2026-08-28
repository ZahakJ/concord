<script>
  import Modal from "./Modal.svelte";
  import { S, activeGuild, refreshGuilds, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { PERM, has } from "../lib/perms.js";
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

  // Per-channel retention. The API has taken a channel since the day it was
  // written and nothing ever passed one: ModalRetention hard-wires the empty
  // guild-wide key, and lib/govlog.js has always known how to render a
  // per-channel sentence that nothing could produce. "#general: 30 days,
  // #announcements: forever" is the ordinary shape of a community's policy.
  //
  // It rides MANAGE GUILD, not manage-channels — the same bit ModalRetention
  // asks for — because it deletes other people's copies of the conversation
  // on their machines, and that is not a channel tweak.
  const RETAIN = [
    { secs: -1, label: "Guild default" },
    { secs: 0, label: "Forever" },
    { secs: 86400, label: "24h" },
    { secs: 7 * 86400, label: "7d" },
    { secs: 30 * 86400, label: "30d" },
    { secs: 90 * 86400, label: "90d" },
  ];
  const canRetain = $derived(has(activeGuild()?.myPerms || 0, PERM.MANAGE_GUILD));
  // ChannelView.retention already resolves to the effective policy, so the
  // channel's own override is only knowable as "differs from the guild's".
  // -1 means "inherit", which is what a delete of the override produces.
  let retain = $state(-1);
  let guildRetain = $state(0);
  (async () => {
    try {
      guildRetain = (await api.guildRetention(S.activeGuildId)) || 0;
    } catch {
      /* older backend: no policy support */
    }
    const eff = Number(channel?.retention) || 0;
    retain = eff === guildRetain ? -1 : eff;
  })();
  const retainChanged = $derived(
    retain !== (Number(channel?.retention) === guildRetain ? -1 : Number(channel?.retention) || 0),
  );
  const retainLabel = (secs) => RETAIN.find((r) => r.secs === secs)?.label || `${secs}s`;

  async function save() {
    const gid = activeGuild()?.id || S.activeGuildId;
    if (slowChanged) {
      try {
        await api.setSlowMode(gid, channel.id, slow);
        refreshGuilds();
      } catch (err) {
        flash(err);
        return;
      }
    }
    if (canRetain && retainChanged) {
      try {
        // 0 seconds is how the op says "no override" as well as "keep
        // forever", so inheriting and keeping forever are the same wire
        // value — and inheriting a guild that keeps forever IS keeping
        // forever, so nothing is lost by that.
        await api.setRetention(gid, channel.id, retain < 0 ? 0 : retain);
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
  {#if canRetain}
    <div class="slow">
      <strong class="slow-label">Message history</strong>
      <div class="seg" role="radiogroup" aria-label="How long this channel keeps messages">
        {#each RETAIN as r (r.secs)}
          <button
            class:sel={retain === r.secs}
            role="radio"
            aria-checked={retain === r.secs}
            onclick={() => (retain = r.secs)}
          >{r.label}</button>
        {/each}
      </div>
      <p class="muted tiny">
        {#if retain < 0}
          Follows the guild ({guildRetain ? retainLabel(guildRetain) : "forever"}).
        {:else if retain === 0}
          Nothing in this channel is removed by age, whatever the guild says.
        {:else}
          Every member's app prunes its own copy of this channel past {retainLabel(retain)}. There
          is no server to enforce it — a modified client can keep whatever it likes.
        {/if}
      </p>
    </div>
  {/if}
  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={save}>Save</button>
  </div>
</Modal>

<style>
  p {
    margin: 0;
    font-size: var(--fs-ui);
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
    gap: var(--sp-1);
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
    font-size: var(--fs-ui);
    padding: 8px 10px;
    margin-top: var(--sp-2);
    box-sizing: border-box;
  }
</style>
