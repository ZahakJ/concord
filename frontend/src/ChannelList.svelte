<script>
  // Second column: the active guild's channels, unread counts, per-channel
  // mute, and the self row (profile + network settings) pinned to the bottom.
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import Menu from "./Menu.svelte";
  import { S, activeGuild, selectChannel, toggleMute } from "./lib/state.svelte.js";

  let { onJoinVoice } = $props();

  const g = $derived(activeGuild());

  // Group channels under their category (uncategorized first), each group
  // ordered by the channel's position.
  const groups = $derived.by(() => {
    if (!g) return [];
    const cats = [...(g.categories || [])].sort((a, b) => a.position - b.position);
    const byCat = (id) =>
      g.channels.filter((c) => (c.category || "") === id).sort((a, b) => a.position - b.position);
    const out = [{ id: "", name: "", channels: byCat("") }];
    for (const cat of cats) out.push({ id: cat.id, name: cat.name, channels: byCat(cat.id) });
    return out.filter((grp) => grp.channels.length || grp.id);
  });

  const typeIcon = (t) => (t === "voice" ? "speaker" : t === "announcement" ? "megaphone" : "hash");

  function clickChannel(c) {
    if (c.type === "voice") onJoinVoice?.(c.id);
    else selectChannel(c.id);
  }
</script>

<aside class="cols">
  <header class="guild-name">
    <strong>{g?.name ?? "Concord"}</strong>
  </header>

  <div class="scroll">
    {#if g?.kind === "dm"}
      <div class="dm-intro">
        <div class="dm-icon"><Icon name="edit" size={22} /></div>
        <strong>Notes</strong>
        <p class="muted">
          Your private, end-to-end-encrypted space. Jot things down, drop links and files —
          only you can read it. It'll follow you to your other devices once they're linked.
        </p>
      </div>
    {:else if g}
      <div class="section-head">
        <span>Channels</span>
        <Menu label="Add channel or category" icon="plus" align="right" compact>
          <button class="menu-item" onclick={() => (S.modal = { kind: "channel" })}>
            <Icon name="hash" size={14} /> New channel
          </button>
          <button class="menu-item" onclick={() => (S.modal = { kind: "category" })}>
            <Icon name="chevron" size={14} /> New category
          </button>
        </Menu>
      </div>

      {#each groups as grp (grp.id || "_uncat")}
        {#if grp.name}
          <div class="cat-head">{grp.name}</div>
        {/if}
        {#each grp.channels as c (c.id)}
          {@const u = S.unread[c.id]}
          {@const active = c.id === S.activeChannelId && c.type !== "voice"}
          {@const inVoice = S.voice && S.voice.channelId === c.id}
          <div class="channel-row" class:active class:voice-active={inVoice}>
            <button class="channel" class:muted-ch={S.mutes[c.id]} onclick={() => clickChannel(c)}>
              <Icon name={typeIcon(c.type)} size={13} />
              <span class="ch-name">{c.name}</span>
              {#if c.type !== "voice" && c.id !== S.activeChannelId && u && !S.mutes[c.id]}
                <span class="count" class:mention={u.mentions > 0}>{u.count > 99 ? "99+" : u.count}</span>
              {/if}
              {#if inVoice}<Icon name="speaker" size={12} />{/if}
            </button>
            {#if c.type !== "voice"}
              <button
                class="mute-btn"
                title={S.mutes[c.id] ? "Unmute channel" : "Mute channel"}
                aria-label={S.mutes[c.id] ? "Unmute channel" : "Mute channel"}
                onclick={() => toggleMute(c.id)}
              >
                <Icon name={S.mutes[c.id] ? "bellOff" : "bell"} size={13} />
              </button>
            {/if}
          </div>
        {/each}
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
  .cat-head {
    text-transform: uppercase;
    font-size: 10px;
    letter-spacing: 0.06em;
    color: var(--text-faint);
    font-weight: 700;
    margin: 10px 8px 2px;
  }
  .voice-active {
    background: var(--accent-soft) !important;
  }
  .channel-row {
    position: relative;
    display: flex;
    align-items: center;
    border-radius: var(--radius-sm);
  }
  .channel-row.active::before {
    content: "";
    position: absolute;
    left: -8px;
    top: 50%;
    transform: translateY(-50%);
    width: 3px;
    height: 60%;
    border-radius: 0 3px 3px 0;
    background: var(--accent);
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
  .dm-intro {
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    gap: 8px;
    padding: 24px 14px;
  }
  .dm-icon {
    width: 48px;
    height: 48px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .dm-intro p {
    font-size: 12px;
    line-height: 1.5;
    margin: 0;
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
</style>
