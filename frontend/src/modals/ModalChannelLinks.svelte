<script>
  // Announcement → consumer wiring: pick which text channels this
  // announcement channel publishes into (Discord's "following", scoped to
  // the guild). Members with ManageChannels only.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S, activeGuild, refreshGuilds, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";

  let { channel, onClose } = $props();

  const g = activeGuild();
  const candidates = (g?.channels || []).filter(
    (c) => c.id !== channel.id && (c.type === "text" || !c.type) && !c.parent,
  );
  let selected = $state(new Set(channel.links || []));
  let busy = $state(false);

  function toggle(id) {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    selected = next;
  }

  async function save() {
    busy = true;
    try {
      await api.setChannelLinks(g.id, channel.id, [...selected]);
      await refreshGuilds();
      flash(selected.size ? `Publishing to ${selected.size} channel${selected.size > 1 ? "s" : ""}` : "Links cleared", "success");
      onClose();
    } catch (err) {
      flash(err);
    } finally {
      busy = false;
    }
  }
</script>

<Modal title="Linked channels" {onClose}>
  <p class="muted intro">
    Messages published from <strong>📣 {channel.name}</strong> are copied into
    every linked channel.
  </p>
  {#if candidates.length}
    <div class="list">
      {#each candidates as c (c.id)}
        <button class="row" class:on={selected.has(c.id)} onclick={() => toggle(c.id)}>
          <Icon name="hash" size={14} />
          <span class="name">{c.name}</span>
          {#if selected.has(c.id)}<span class="check"><Icon name="check" size={14} /></span>{/if}
        </button>
      {/each}
    </div>
  {:else}
    <p class="muted">No text channels to link yet.</p>
  {/if}
  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={save} disabled={busy}>Save</button>
  </div>
</Modal>

<style>
  .intro {
    font-size: 13px;
    margin: 0 0 8px;
    line-height: 1.5;
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: 260px;
    overflow-y: auto;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 4px;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 7px 10px;
    background: transparent;
    border: none;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    font-size: 13.5px;
    text-align: left;
    cursor: pointer;
  }
  .row:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .row.on {
    background: var(--accent-soft);
    color: var(--text);
  }
  .name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .check {
    color: var(--accent-hover);
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 10px;
  }
</style>
