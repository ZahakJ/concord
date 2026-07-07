<script>
  // Chat column header: channel name, search, pins, voice, guild actions.
  import Icon from "./Icon.svelte";
  import Menu from "./Menu.svelte";
  import {
    S,
    activeGuild,
    activeChannel,
    channelName,
    flash,
    refreshGuilds,
    selectGuild,
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";

  let { onJoinVoice, onLeaveVoice, onToggleMute, onToggleShare, onToggleCamera } = $props();

  const g = $derived(activeGuild());
  const ch = $derived(activeChannel());
  const pinnedCount = $derived(S.messages.filter((m) => m.pinned && !m.deleted).length);

  async function runSearch(e) {
    e?.preventDefault();
    const q = S.searchQuery.trim();
    if (!q) {
      S.searchResults = null;
      return;
    }
    try {
      S.searchResults = (await api.searchMessages(q)) || [];
    } catch (err) {
      flash(err);
    }
  }

  async function showInvite() {
    S.modal = { kind: "invite", code: await api.inviteCode(S.activeGuildId) };
  }

  function confirmLeave() {
    if (!g) return;
    const verb = g.isOwner ? "Delete" : "Leave";
    S.modal = {
      kind: "confirm",
      title: `${verb} "${g.name}"?`,
      body: "Its messages will be removed from this device.",
      confirmLabel: verb,
      onConfirm: async () => {
        S.modal = null;
        await api.leaveGuild(g.id);
        S.activeGuildId = "";
        S.activeChannelId = "";
        S.messages = [];
        await refreshGuilds();
        if (S.guilds.length) selectGuild(S.guilds[0].id);
        flash(g.isOwner ? "Guild deleted" : "Left guild");
      },
    };
  }

  function exportChannel() {
    if (!ch) return;
    const lines = S.messages
      .filter((m) => !m.deleted)
      .map((m) =>
        m.kind === "system"
          ? `> ✨ ${m.senderName || m.sender} ${m.content}`
          : `**${m.senderName || m.sender}** (${m.sent}):\n${m.content}\n`,
      );
    const blob = new Blob([`# ${channelName(S.activeChannelId)}\n\n` + lines.join("\n")], {
      type: "text/markdown",
    });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `${ch.name}-history.md`;
    a.click();
    URL.revokeObjectURL(a.href);
    flash("History exported");
  }
</script>

<header class="chat-head">
  <div class="row title">
    {#if g?.kind === "dm"}
      <Icon name="edit" size={15} />
      <strong>{g.name}</strong>
    {:else if ch}
      <Icon name="hash" size={15} />
      <strong>{ch.name}</strong>
    {:else}
      <span class="muted">No channel</span>
    {/if}
  </div>
  <div class="row">
    <form onsubmit={runSearch}>
      <input class="search-box" placeholder="Search…  (Ctrl+K to jump)" bind:value={S.searchQuery} />
    </form>

    {#if S.voice && S.voice.channelId === S.activeChannelId}
      <!-- Active voice collapses to one pill with mute + leave inside it. -->
      <span class="voice-pill">
        <Icon name="speaker" size={12} />
        {S.voiceParticipants.length + 1}
        <button class="pill-btn" title={S.muted ? "Unmute mic" : "Mute mic"} aria-label={S.muted ? "Unmute mic" : "Mute mic"} onclick={onToggleMute}>
          <Icon name={S.muted ? "micOff" : "mic"} size={13} />
        </button>
        <button class="pill-btn" class:on={S.cameraOn} title={S.cameraOn ? "Turn off camera" : "Turn on camera"} aria-label={S.cameraOn ? "Turn off camera" : "Turn on camera"} onclick={onToggleCamera}>
          <Icon name={S.cameraOn ? "cameraOff" : "camera"} size={13} />
        </button>
        <button class="pill-btn" class:on={S.sharing} title={S.sharing ? "Stop sharing" : "Share screen"} aria-label={S.sharing ? "Stop sharing" : "Share screen"} onclick={onToggleShare}>
          <Icon name={S.sharing ? "screenOff" : "screen"} size={13} />
        </button>
        <button class="pill-btn leave" title="Leave voice" aria-label="Leave voice" onclick={onLeaveVoice}>
          <Icon name="door" size={13} />
        </button>
      </span>
    {:else if ch && g?.kind !== "dm"}
      <button class="ghost iconbtn" title="Join voice" onclick={onJoinVoice}>
        <Icon name="speaker" /> <span class="n">Voice</span>
      </button>
    {/if}

    {#if ch}
      <button
        class="ghost iconbtn"
        class:pin-active={S.showPins}
        title="Pinned messages"
        aria-label="Pinned messages"
        onclick={() => (S.showPins = !S.showPins)}
      >
        <Icon name="pin" />{#if pinnedCount}<span class="n">{pinnedCount}</span>{/if}
      </button>
    {/if}

    {#if g?.canManage && g?.kind !== "dm"}
      <button class="ghost invite" onclick={showInvite}>Invite</button>
    {/if}

    {#if g && g.kind !== "dm"}
      <Menu label="More" icon="chevron">
        {#if ch}
          <button class="menu-item" onclick={exportChannel}>
            <Icon name="download" size={14} /> Export history
          </button>
        {/if}
        <button class="menu-item" onclick={() => (S.modal = { kind: "emoji" })}>
          <Icon name="smile" size={14} /> Guild emoji
        </button>
        {#if g.isOwner}
          <button class="menu-item" onclick={() => (S.modal = { kind: "rename" })}>
            <Icon name="edit" size={14} /> Rename guild
          </button>
        {/if}
        {#if g.canManage}
          <button class="menu-item" onclick={() => (S.modal = { kind: "bans" })}>
            <Icon name="door" size={14} /> Banned members
          </button>
        {/if}
        <div class="menu-sep"></div>
        <button class="menu-item danger" onclick={confirmLeave}>
          <Icon name={g.isOwner ? "trash" : "door"} size={14} />
          {g.isOwner ? "Delete guild" : "Leave guild"}
        </button>
      </Menu>
    {/if}
  </div>
</header>

<style>
  .chat-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 11px 16px;
    border-bottom: 1px solid var(--border);
    gap: 10px;
  }
  .title {
    gap: 6px;
    color: var(--text-muted);
    min-width: 0;
  }
  .title strong {
    color: var(--text);
    white-space: nowrap;
  }
  .search-box {
    width: 190px;
    padding: 5px 10px;
    font-size: 13px;
  }
  .iconbtn {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 6px 9px;
  }
  .n {
    font-size: 12px;
  }
  .pin-active {
    color: var(--accent-hover);
    border-color: var(--accent);
  }
  .voice-pill {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    font-weight: 600;
    color: var(--ok);
    padding: 3px 6px 3px 10px;
    background: var(--ok-soft);
    border-radius: 13px;
    white-space: nowrap;
  }
  .pill-btn {
    background: transparent;
    color: var(--ok);
    padding: 3px;
    display: grid;
    place-items: center;
    border-radius: 50%;
  }
  .pill-btn:hover {
    background: color-mix(in srgb, var(--ok) 22%, transparent);
  }
  .pill-btn.on {
    background: var(--ok);
    color: #fff;
  }
  .pill-btn.leave {
    color: var(--danger);
  }
  .pill-btn.leave:hover {
    background: var(--danger-soft);
  }
  .invite {
    padding: 6px 12px;
  }
</style>
