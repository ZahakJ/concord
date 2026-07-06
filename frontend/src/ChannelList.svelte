<script>
  // Second column: the active guild's channels, unread counts, per-channel
  // mute, and the self row (profile + network settings) pinned to the bottom.
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import { S, activeGuild, selectChannel, toggleMute } from "./lib/state.svelte.js";

  const g = $derived(activeGuild());
</script>

<aside class="cols">
  <header class="guild-name">
    <strong>{g?.name ?? "Concord"}</strong>
  </header>

  <div class="scroll">
    {#if g}
      <div class="section-head">
        <span>Channels</span>
        <button class="mini" onclick={() => (S.modal = { kind: "channel" })} title="Add channel" aria-label="Add channel">
          <Icon name="plus" size={12} />
        </button>
      </div>
      {#each g.channels as c (c.id)}
        {@const u = S.unread[c.id]}
        <div class="channel-row" class:active={c.id === S.activeChannelId}>
          <button class="channel" class:muted-ch={S.mutes[c.id]} onclick={() => selectChannel(c.id)}>
            <Icon name="hash" size={13} />
            <span class="ch-name">{c.name}</span>
            {#if c.id !== S.activeChannelId && u && !S.mutes[c.id]}
              <span class="count" class:mention={u.mentions > 0}>{u.count > 99 ? "99+" : u.count}</span>
            {/if}
          </button>
          <button
            class="mute-btn"
            title={S.mutes[c.id] ? "Unmute channel" : "Mute channel"}
            aria-label={S.mutes[c.id] ? "Unmute channel" : "Mute channel"}
            onclick={() => toggleMute(c.id)}
          >
            <Icon name={S.mutes[c.id] ? "bellOff" : "bell"} size={13} />
          </button>
        </div>
      {/each}
    {:else}
      <p class="muted empty-hint">
        No servers yet. Create one with <Icon name="plus" size={12} /> in the rail, or join a
        friend's with their invite code.
      </p>
    {/if}
  </div>

  <div class="me-row">
    <button class="me" onclick={() => (S.modal = { kind: "profile" })} title="Edit profile">
      <Avatar
        name={S.displayName}
        emoji={S.identity.emoji}
        color={S.identity.color}
        image={S.identity.avatar}
        size={34}
      />
      <span class="me-text">
        <strong>{S.displayName || "Set your name"}</strong>
        <span class="muted small-status">{S.identity.status || "click to edit profile"}</span>
      </span>
    </button>
    <button class="me-gear ghost" title="Network settings" aria-label="Network settings" onclick={() => (S.modal = { kind: "settings" })}>
      <Icon name="gear" />
    </button>
  </div>
</aside>

<style>
  .cols {
    background: var(--bg-1);
    border-right: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .guild-name {
    padding: 14px;
    border-bottom: 1px solid var(--border);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .scroll {
    flex: 1;
    overflow-y: auto;
    padding: 8px;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .section-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    text-transform: uppercase;
    font-size: 11px;
    letter-spacing: 0.05em;
    color: var(--text-muted);
    margin: 6px 6px 4px;
  }
  .channel-row {
    display: flex;
    align-items: center;
    border-radius: var(--radius-sm);
  }
  .channel-row:hover,
  .channel-row.active {
    background: var(--bg-3);
  }
  .channel-row.active .channel {
    color: var(--text);
  }
  .channel {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 7px;
    background: transparent;
    color: var(--text-muted);
    padding: 7px 8px;
    font-size: 14px;
    text-align: left;
    min-width: 0;
    border-radius: var(--radius-sm);
  }
  .channel:hover {
    background: transparent;
    color: var(--text);
  }
  .channel.muted-ch {
    color: var(--text-faint);
  }
  .ch-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .count {
    min-width: 18px;
    padding: 0 5px;
    height: 16px;
    border-radius: 8px;
    background: var(--text-faint);
    color: white;
    font-size: 10px;
    font-weight: 700;
    display: grid;
    place-items: center;
  }
  .count.mention {
    background: var(--danger);
  }
  .mute-btn {
    background: transparent;
    color: var(--text-faint);
    padding: 4px 6px;
    opacity: 0;
  }
  .channel-row:hover .mute-btn,
  .mute-btn:focus-visible {
    opacity: 1;
  }
  .mute-btn:hover {
    background: transparent;
    color: var(--text);
  }
  .empty-hint {
    font-size: 13px;
    line-height: 1.5;
    padding: 8px;
  }
  .me-row {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 8px;
    border-top: 1px solid var(--border);
    background: var(--bg-0);
  }
  .me {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 10px;
    background: transparent;
    color: var(--text);
    text-align: left;
    padding: 6px;
    border-radius: var(--radius-sm);
    min-width: 0;
  }
  .me:hover {
    background: var(--bg-3);
  }
  .me-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
    font-size: 13px;
  }
  .small-status {
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .me-gear {
    padding: 8px;
    border: none;
  }
  .mini {
    padding: 2px 6px;
    font-size: 12px;
    background: transparent;
    color: var(--text-muted);
    border-radius: 5px;
    display: grid;
    place-items: center;
  }
  .mini:hover {
    background: var(--bg-3);
    color: var(--text);
  }
</style>
