<script>
  import Modal from "./Modal.svelte";
  import Select from "../Select.svelte";
  import { S, activeGuild, refreshGuilds, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { PERM, has } from "../lib/perms.js";
  import { RETAIN_OPTIONS, retainLabel as retainSecsLabel } from "../lib/retention.js";
  let { channel, onSubmit, onClose } = $props();
  let topic = $state(channel?.topic || "");

  // Slow mode rides this modal because it IS channel settings — a per-channel
  // governed interval, set by the same people who set the topic. The op is a
  // signed govOp (manage-channels); enforcement is advisory like mutes.
  const SLOW_OPTS = [
    [0, "Off"],
    [5, "5 seconds"],
    [30, "30 seconds"],
    [60, "1 minute"],
    [300, "5 minutes"],
    [900, "15 minutes"],
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
    ...RETAIN_OPTIONS,
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
  const retainLabel = (secs) =>
    secs < 0 ? "Guild default" : retainSecsLabel(secs);

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

<Modal title="Channel settings" subtitle={`#${channel?.name || "channel"}`} size="m" {onClose}>
  <label class="fld">
    <strong class="slow-label">Topic</strong>
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
  </label>
  <div class="slow">
    <strong class="slow-label">Slow mode</strong>
    <Select
      label="Slow mode interval"
      value={slow}
      options={SLOW_OPTS.map(([secs, label]) => ({ value: secs, label }))}
      onPick={(v) => (slow = Number(v))}
    />
    <p class="muted tiny">
      One message per member per interval. Moderators are exempt.
    </p>
  </div>
  {#if canRetain}
    <div class="slow">
      <strong class="slow-label">Message history</strong>
      <Select
        label="How long this channel keeps messages"
        value={retain}
        options={RETAIN.map((r) => ({ value: r.secs, label: r.label }))}
        onPick={(v) => (retain = Number(v))}
      />
      <p class="muted tiny">
        {#if retain < 0}
          Follows the guild ({retainLabel(guildRetain)}).
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
    <button class="primary" onclick={save}>Save</button>
  </div>
</Modal>

<style>
  p {
    margin: 0;
    font-size: var(--fs-compact);
  }
  .fld {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .slow {
    margin-top: var(--sp-3);
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .slow-label {
    font-size: var(--fs-compact);
    font-weight: 500;
  }
  textarea {
    width: 100%;
    resize: vertical;
    font-family: inherit;
    font-size: var(--fs-ui);
    padding: 8px 10px;
    margin-top: var(--sp-1);
    box-sizing: border-box;
  }
</style>
