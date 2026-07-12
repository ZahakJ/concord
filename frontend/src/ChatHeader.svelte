<script>
  // Chat column header: channel name, search, pins, voice, guild actions.
  import Icon from "./Icon.svelte";
  import Menu from "./Menu.svelte";
  import {
    S,
    activeGuild,
    selectChannel,
    activeChannel,
    channelName,
    flash,
    refreshGuilds,
    selectGuild,
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { PERM, has } from "./lib/perms.js";
  // Operator parsing (from:/in:/has:/before:/after:) + the backend call live
  // in lib/search.js, shared with the results panel's chip refinement.
  import { runSearch, closeSearch } from "./lib/search.js";

  let { onJoinVoice, onLeaveVoice, onToggleMute, onToggleShare, onToggleCamera } = $props();

  const g = $derived(activeGuild());
  const ch = $derived(activeChannel());
  const pinnedCount = $derived(S.messages.filter((m) => m.pinned && !m.deleted).length);

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
    flash("History exported", "success");
  }
</script>

<header class="chat-head">
  <div class="row title">
    {#if g?.kind === "dm"}
      <Icon name="edit" size={15} />
      <strong>{g.name}</strong>
    {:else if ch?.parent}
      <!-- A forum post: breadcrumb back to its board. -->
      <button
        class="thread-back"
        title="Back to the forum"
        aria-label="Back to the forum"
        onclick={() => selectChannel(ch.parent)}
      >
        <Icon name="forum" size={14} />
        {activeGuild()?.channels.find((c) => c.id === ch.parent)?.name || "forum"}
        <span class="tb-sep">›</span>
      </button>
      <strong>{ch.name}</strong>
    {:else if ch}
      <Icon name={ch.type === "forum" ? "forum" : ch.type === "announcement" ? "megaphone" : "hash"} size={15} />
      <strong>{ch.name}</strong>
      {#if ch.topic}
        <span class="topic-sep"></span>
        <span class="chan-topic" title={ch.topic}>{ch.topic}</span>
      {/if}
    {:else}
      <span class="muted">No channel</span>
    {/if}
  </div>
  <div class="row">
    <form class="search-wrap" onsubmit={runSearch}>
      <input
        class="search-box"
        class:busy={S.searchLoading || S.searchQuery || S.searchResults !== null}
        placeholder="Search all conversations"
        title="Searches every channel and DM. Filters: from:name  in:#channel  has:link|image|file  before:YYYY-MM-DD  after:YYYY-MM-DD"
        aria-label="Search messages across all conversations"
        bind:value={S.searchQuery}
        onkeydown={(e) => {
          if (e.key === "Escape") {
            closeSearch();
            e.currentTarget.blur();
          }
        }}
      />
      {#if S.searchLoading}
        <span class="search-spin" aria-hidden="true"></span>
      {:else if S.searchQuery || S.searchResults !== null}
        <button
          type="button"
          class="search-clear"
          aria-label="Clear search"
          title="Clear search"
          onclick={closeSearch}
        >
          <Icon name="close" size={11} />
        </button>
      {/if}
    </form>

    {#if S.voice && S.voice.channelId === S.activeChannelId && g?.kind === "dm"}
      <!-- In a DM call, the call box carries the controls; the header is just a
           one-click hang-up so clicking "call" again intuitively leaves. -->
      <button class="ghost iconbtn endcall" title="Leave call" aria-label="Leave call" onclick={onLeaveVoice}>
        <Icon name="door" /> <span class="n">End call</span>
      </button>
    {:else if S.voice && S.voice.channelId === S.activeChannelId}
      <!-- Active voice collapses to one pill with mute + leave inside it. -->
      <span class="voice-pill">
        <span class="pill-label" title="{S.voiceParticipants.length + 1} in this call">
          <Icon name="speaker" size={12} />
          {S.voiceParticipants.length + 1}
        </span>
        <span class="pill-sep"></span>
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
    {:else if ch}
      <button
        class="ghost iconbtn"
        class:call={g?.kind === "dm"}
        title={g?.kind === "dm" ? "Start a call" : "Join voice"}
        onclick={() => onJoinVoice()}
      >
        <Icon name="speaker" /> <span class="n">{g?.kind === "dm" ? "Call" : "Voice"}</span>
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
        {#if has(g.myPerms, PERM.MANAGE_ROLES) || g.isOwner}
          <button class="menu-item" onclick={() => (S.modal = { kind: "roles" })}>
            <Icon name="spark" size={14} /> Roles
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
    /* Faint drop under the header: elevation over the feed it caps. */
    box-shadow: 0 2px 10px rgba(0, 0, 0, 0.1);
    position: relative;
    z-index: 5;
  }
  /* An accent thread under the channel name, fading out rightward — the
     header quietly points at where you are. */
  .chat-head::after {
    content: "";
    position: absolute;
    left: 16px;
    bottom: -1px;
    width: 180px;
    height: 1px;
    background: linear-gradient(90deg, color-mix(in srgb, var(--accent) 55%, transparent), transparent);
    pointer-events: none;
  }
  .title {
    gap: 6px;
    color: var(--text-muted);
    min-width: 0;
  }
  /* The channel-type glyph carries the accent — a small "you are here" tint. */
  .title :global(svg) {
    color: var(--accent);
    flex-shrink: 0;
  }
  .title strong {
    color: var(--text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .topic-sep {
    width: 1px;
    align-self: stretch;
    background: var(--border);
    margin: 3px 2px;
    flex-shrink: 0;
  }
  .chan-topic {
    font-size: 12px;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .search-wrap {
    position: relative;
    display: inline-flex;
    align-items: center;
  }
  .search-box {
    /* Fluid: shrinks with the window so it never overlaps the channel name,
       but stays usable (min 84px) and caps at its comfortable width. Focus
       stretches it — room appears exactly when you start typing. */
    width: clamp(84px, 20vw, 190px);
    min-width: 0;
    padding: 5px 10px;
    font-size: 13px;
    transition: width 0.25s cubic-bezier(0.2, 0.9, 0.3, 1), border-color 0.16s ease, box-shadow 0.16s ease, background 0.16s ease;
  }
  .search-box:focus {
    width: clamp(84px, 28vw, 260px);
  }
  @media (prefers-reduced-motion: reduce) {
    .search-box {
      transition: border-color 0.16s ease, box-shadow 0.16s ease;
    }
  }
  /* Leave room for the clear button / spinner once there's something to show. */
  .search-box.busy {
    padding-right: 26px;
  }
  .search-clear {
    position: absolute;
    right: 5px;
    padding: 2px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    background: transparent;
    color: var(--text-muted);
  }
  .search-clear:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .search-spin {
    position: absolute;
    right: 8px;
    width: 12px;
    height: 12px;
    border-radius: 50%;
    border: 2px solid var(--accent-soft);
    border-top-color: var(--accent);
    animation: search-spin 0.7s linear infinite;
    pointer-events: none;
  }
  @keyframes search-spin {
    to {
      transform: rotate(360deg);
    }
  }
  .iconbtn {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 6px 9px;
    transition:
      background 0.15s ease,
      color 0.15s ease,
      border-color 0.15s ease,
      transform 0.12s ease;
  }
  .iconbtn:hover {
    transform: translateY(-1px);
  }
  .iconbtn:active {
    transform: none;
  }
  .n {
    font-size: 12px;
  }
  .pin-active {
    color: var(--accent-hover);
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .voice-pill {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    font-weight: 600;
    color: var(--ok);
    padding: 3px 5px 3px 9px;
    background: var(--ok-soft);
    border-radius: 13px;
    white-space: nowrap;
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--ok) 25%, transparent);
    /* One quiet breath while the call is live (the pill only exists then). */
    animation: pill-breathe 3.6s ease-in-out infinite;
  }
  @keyframes pill-breathe {
    0%,
    100% {
      box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--ok) 25%, transparent);
    }
    50% {
      box-shadow:
        inset 0 0 0 1px color-mix(in srgb, var(--ok) 35%, transparent),
        0 0 10px color-mix(in srgb, var(--ok) 30%, transparent);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .voice-pill {
      animation: none;
    }
    .iconbtn:hover {
      transform: none;
    }
  }
  .pill-label {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    line-height: 1;
  }
  .pill-sep {
    width: 1px;
    align-self: stretch;
    margin: 2px 2px;
    background: color-mix(in srgb, var(--ok) 35%, transparent);
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
  .iconbtn.call {
    color: var(--ok);
    border-color: color-mix(in srgb, var(--ok) 45%, transparent);
  }
  .iconbtn.call:hover {
    background: var(--ok-soft);
  }
  .iconbtn.endcall {
    color: var(--danger);
    border-color: color-mix(in srgb, var(--danger) 45%, transparent);
  }
  .iconbtn.endcall:hover {
    background: var(--danger-soft);
  }
  .thread-back {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 3px 8px;
    background: transparent;
    border: none;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    font-size: 13px;
    cursor: pointer;
  }
  .thread-back:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .tb-sep {
    color: var(--text-faint);
  }
</style>
