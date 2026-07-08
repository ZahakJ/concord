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
  import { PERM, has } from "./lib/perms.js";

  let { onJoinVoice, onLeaveVoice, onToggleMute, onToggleShare, onToggleCamera } = $props();

  const g = $derived(activeGuild());
  const ch = $derived(activeChannel());
  const pinnedCount = $derived(S.messages.filter((m) => m.pinned && !m.deleted).length);

  // Parse Discord-style search operators out of the query. The remaining free
  // text is the substring the backend searches; filters are applied on-device.
  //   from:name  in:#channel  has:link|image|file  before:YYYY-MM-DD  after:…
  function parseQuery(raw) {
    const f = { from: null, in: null, has: [], before: null, after: null };
    const text = raw
      .replace(/(\w+):("[^"]+"|\S+)/g, (m, key, val) => {
        val = val.replace(/^"|"$/g, "");
        switch (key.toLowerCase()) {
          case "from":
            f.from = val.toLowerCase();
            return "";
          case "in":
            f.in = val.replace(/^#/, "").toLowerCase();
            return "";
          case "has":
            f.has.push(val.toLowerCase());
            return "";
          case "before":
            f.before = new Date(val);
            return "";
          case "after":
            f.after = new Date(val);
            return "";
          default:
            return m; // unknown operator: keep as search text
        }
      })
      .trim();
    return { text, filters: f };
  }

  function channelNameFor(chId) {
    for (const gg of S.guilds) {
      const c = gg.channels.find((x) => x.id === chId);
      if (c) return c.name;
    }
    return "";
  }

  function matchFilters(m, f) {
    if (f.from && !(m.senderName || m.name || "").toLowerCase().includes(f.from)) return false;
    if (f.in) {
      const cn = channelNameFor(m.channelId).toLowerCase();
      if (!cn.includes(f.in)) return false;
    }
    if (f.before && !isNaN(f.before) && new Date(m.sent) >= f.before) return false;
    if (f.after && !isNaN(f.after) && new Date(m.sent) <= f.after) return false;
    const c = m.content || "";
    for (const h of f.has) {
      if (h === "link" && !/https?:\/\//.test(c)) return false;
      if (h === "image" && !/concord:\/\/attach|data:image\//.test(c)) return false;
      if (h === "file" && !/concord:\/\/file/.test(c)) return false;
    }
    return true;
  }

  async function runSearch(e) {
    e?.preventDefault();
    const raw = S.searchQuery.trim();
    if (!raw) {
      S.searchResults = null;
      return;
    }
    const { text, filters } = parseQuery(raw);
    try {
      const res = (await api.searchMessages(text)) || [];
      S.searchResults = res.filter((m) => matchFilters(m, filters));
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
      {#if ch.topic}
        <span class="topic-sep"></span>
        <span class="chan-topic" title={ch.topic}>{ch.topic}</span>
      {/if}
    {:else}
      <span class="muted">No channel</span>
    {/if}
  </div>
  <div class="row">
    <form onsubmit={runSearch}>
      <input
        class="search-box"
        placeholder="Search — try from: in: has: before:"
        title="Filters: from:name  in:#channel  has:link|image|file  before:YYYY-MM-DD  after:YYYY-MM-DD"
        bind:value={S.searchQuery}
      />
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
  }
  .title {
    gap: 6px;
    color: var(--text-muted);
    min-width: 0;
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
  .search-box {
    /* Fluid: shrinks with the window so it never overlaps the channel name,
       but stays usable (min 84px) and caps at its comfortable width. */
    width: clamp(84px, 20vw, 190px);
    min-width: 0;
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
    gap: 4px;
    font-size: 12px;
    font-weight: 600;
    color: var(--ok);
    padding: 3px 5px 3px 9px;
    background: var(--ok-soft);
    border-radius: 13px;
    white-space: nowrap;
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
</style>
