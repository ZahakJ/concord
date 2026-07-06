<script>
  // Chat column header: channel name, search, pins, voice, guild actions.
  import Icon from "./Icon.svelte";
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

  let { onJoinVoice, onLeaveVoice, onToggleMute } = $props();

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
        flash(g.isOwner ? "Server deleted" : "Left server");
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
    {#if ch}
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
    {#if ch}
      <button
        class="ghost iconbtn"
        class:pin-active={S.showPins}
        title="Pinned messages"
        aria-label="Pinned messages"
        onclick={() => (S.showPins = !S.showPins)}
      >
        <Icon name="pin" /> <span class="n">{pinnedCount}</span>
      </button>
      <button class="ghost iconbtn" title="Export history" aria-label="Export history" onclick={exportChannel}>
        <Icon name="download" />
      </button>
    {/if}
    {#if S.voice && S.voice.channelId === S.activeChannelId}
      <span class="voice-pill">
        <Icon name="speaker" size={12} />
        {S.voiceParticipants.length + 1} in voice
      </span>
      <button class="ghost iconbtn" title={S.muted ? "Unmute mic" : "Mute mic"} aria-label={S.muted ? "Unmute mic" : "Mute mic"} onclick={onToggleMute}>
        <Icon name={S.muted ? "micOff" : "mic"} />
      </button>
      <button class="ghost leave" onclick={onLeaveVoice}>Leave</button>
    {:else if ch && !S.voice}
      <button class="ghost iconbtn" title="Join voice" onclick={onJoinVoice}>
        <Icon name="speaker" /> <span class="n">Voice</span>
      </button>
    {/if}
    {#if g?.isOwner}
      <button class="ghost" onclick={showInvite}>Invite</button>
      <button class="ghost iconbtn" title="Rename server" aria-label="Rename server" onclick={() => (S.modal = { kind: "rename" })}>
        <Icon name="edit" />
      </button>
    {/if}
    {#if g}
      <button
        class="ghost leave iconbtn"
        title={g.isOwner ? "Delete server (for you)" : "Leave server"}
        aria-label={g.isOwner ? "Delete server" : "Leave server"}
        onclick={confirmLeave}
      >
        <Icon name={g.isOwner ? "trash" : "door"} />
      </button>
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
    gap: 5px;
    font-size: 12px;
    color: var(--ok);
    padding: 4px 10px;
    background: var(--ok-soft);
    border-radius: 12px;
    white-space: nowrap;
  }
  .leave {
    color: var(--danger);
  }
</style>
