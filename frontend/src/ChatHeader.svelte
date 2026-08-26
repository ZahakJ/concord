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
    voiceMembersFor,
    channelTypeIcon,
    toggleMemberPanel,
    openProfilePopover,
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { saveText } from "./lib/savefile.js";
  // Operator parsing (from:/in:/has:/before:/after:) + the backend call live
  // in lib/search.js, shared with the results panel's chip refinement.
  import { runSearch, closeSearch, queueSearch, registerSearchInput } from "./lib/search.js";
  import { channelTTL, ttlLabel } from "./lib/ephemeral.svelte.js";
  // Icon buttons carry use:tooltip (below-center, default delay) instead of
  // native title= — instant theme-matched labels; aria-label stays and is the
  // tip's text unless the tip needs richer wording than the label.
  import { tooltip } from "./lib/tooltip.js";

  let { onJoinVoice, onLeaveVoice, onToggleMute, onToggleShare, onToggleCamera } = $props();

  // Handed to lib/search.js so a filter chip clicked in the results panel can
  // type its prefix here and put the caret back after it.
  let searchEl = $state(null);
  $effect(() => registerSearchInput(searchEl));

  const g = $derived(activeGuild());
  const ch = $derived(activeChannel());
  const ephTTL = $derived(ch ? channelTTL(S.activeChannelId) : 0);
  // In a DM (or meeting), is the other side already in the call while we're not?
  // Drives a "🔴 Live · Join" affordance so a call in progress is obvious.
  const peerInCall = $derived(
    !!ch &&
      (g?.kind === "dm" || g?.kind === "meeting") &&
      S.voice?.channelId !== ch.id &&
      (voiceMembersFor(ch.id) || []).some((m) => !m.self),
  );
  const peerSharing = $derived(peerInCall && (voiceMembersFor(ch.id) || []).some((m) => !m.self && m.sharing));
  const pinnedCount = $derived(S.messages.filter((m) => m.pinned && !m.deleted).length);
  // Squeezed column (see S.narrow): the action row is 566px of nowrap buttons
  // that never shrank, so below ~1150px it simply pushed the channel name to
  // zero width and then overflowed the column anyway. The two occasional
  // actions — disappearing messages, events — move into the menu that is
  // already here, and the button labels shrink to their glyphs.
  const tight = $derived(!S.isMobile && S.narrow);

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

  // The transcript comes from the backend, which reads the store. Building it
  // here from S.messages meant exporting only the page the reader had loaded —
  // the last 200 plus whatever they scrolled through — which looked like a full
  // history right up until someone needed the rest of it.
  async function exportChannel() {
    if (!ch) return;
    try {
      const how = await saveText(
        `${ch.name}-history.md`,
        await api.exportMarkdown(S.activeGuildId, S.activeChannelId),
        "text/markdown",
      );
      if (how === "file") flash("History exported", "success");
      else if (how === "clipboard") flash("History copied to the clipboard", "success");
    } catch (err) {
      flash(err);
    }
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
      <Icon name={channelTypeIcon(ch.type)} size={15} />
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
    <form
      class="search-wrap"
      class:open={!!S.searchQuery || S.searchResults !== null}
      onsubmit={runSearch}
    >
      <!-- The operator syntax used to be documented in a title= on this field.
           It now lives as clickable chips in the results panel directly below,
           where it can be read without hovering and used without typing. -->
      <input
        class="search-box"
        class:busy={S.searchLoading || S.searchQuery || S.searchResults !== null}
        placeholder="Search all conversations"
        aria-label="Search messages across all conversations"
        bind:this={searchEl}
        bind:value={S.searchQuery}
        oninput={() => queueSearch()}
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
      <!-- Only ever seen in the squeezed band, where the field itself is a
           32px stub: without it the stub is a small empty box that says
           nothing about what it does. -->
      <span class="search-glyph" aria-hidden="true"><Icon name="search" size={14} /></span>
    </form>

    {#if S.voice && S.voice.channelId === S.activeChannelId && (g?.kind === "dm" || g?.kind === "meeting")}
      <!-- In a DM call, the call box carries the controls; the header is just a
           one-click hang-up so clicking "call" again intuitively leaves. -->
      <button class="ghost iconbtn endcall" use:tooltip aria-label="Leave call" onclick={onLeaveVoice}>
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
    {:else if ch && peerInCall}
      <!-- The other side is already on a call — make it obvious and one-click. -->
      <button class="ghost iconbtn live-join" use:tooltip={"Join the call"} onclick={() => onJoinVoice()}>
        <span class="live-dot"></span>
        <span class="n">Live{peerSharing ? " · sharing" : ""} · Join</span>
      </button>
    {:else if ch}
      <button
        class="ghost iconbtn"
        class:call={g?.kind === "dm" || g?.kind === "meeting"}
        use:tooltip={{ text: g?.kind === "dm" || g?.kind === "meeting" ? "Start a call" : "Join voice" }}
        onclick={() => onJoinVoice()}
      >
        <Icon name="speaker" /> <span class="n">{g?.kind === "dm" || g?.kind === "meeting" ? "Call" : "Voice"}</span>
      </button>
    {/if}

    {#if ch}
      <button
        class="ghost iconbtn"
        class:pin-active={S.showPins}
        use:tooltip
        aria-label="Pinned messages"
        onclick={() => (S.showPins = !S.showPins)}
      >
        <Icon name="pin" />{#if pinnedCount}<span class="n count">{pinnedCount}</span>{/if}
      </button>
    {/if}

    {#if ch && !tight}
      <button
        class="ghost iconbtn"
        class:pin-active={ephTTL > 0}
        use:tooltip={{ text: ephTTL > 0 ? `Disappearing after ${ttlLabel(ephTTL)}` : "Disappearing messages" }}
        aria-label="Disappearing messages"
        onclick={() => (S.modal = { kind: "disappear", channelId: S.activeChannelId })}
      >
        <Icon name="clock" />
      </button>
    {/if}

    {#if g && !tight}
      <!-- The calendar — the thing that replaces "so when are we on?"
           scroll-back. Every room gets one: a guild's is the crew's shared
           board, a DM's is "when are we hopping on?" between its people, and
           Notes' is your private list (a single-member group, so private by
           construction). Mounted like Pins: a button here, a sheet entry on
           phones (MobileShell's ⋯, since this header never renders there). -->
      <button
        class="ghost iconbtn"
        use:tooltip={{ text: g.dmNotes ? "Private events" : "Events" }}
        aria-label={g.dmNotes ? "Private events" : g.kind === "dm" ? "Events in this conversation" : "Guild events"}
        onclick={() => (S.modal = { kind: "events" })}
      >
        <Icon name="calendar" />
      </button>
    {/if}

    {#if ch && g?.kind !== "dm"}
      <button
        class="ghost iconbtn"
        class:pin-active={S.prefs.memberPanel}
        use:tooltip={{ text: "Toggle member list (Ctrl+U)" }}
        aria-label="Toggle member list"
        aria-pressed={S.prefs.memberPanel}
        onclick={toggleMemberPanel}
      >
        <Icon name="members" />
      </button>
    {/if}

    {#if g?.canManage && g?.kind !== "dm" && !tight}
      <button class="ghost invite" onclick={showInvite}>Invite</button>
    {/if}

    {#if g}
      <Menu label="More" icon="chevron">
        {#if tight && ch}
          <button
            class="menu-item"
            onclick={() => (S.modal = { kind: "disappear", channelId: S.activeChannelId })}
          >
            <Icon name="clock" size={14} />
            {ephTTL > 0 ? `Disappearing after ${ttlLabel(ephTTL)}` : "Disappearing messages"}
          </button>
        {/if}
        {#if tight}
          <button class="menu-item" onclick={() => (S.modal = { kind: "events" })}>
            <Icon name="calendar" size={14} /> {g.dmNotes ? "Private events" : "Events"}
          </button>
        {/if}
        {#if tight && g.canManage && g.kind !== "dm"}
          <button class="menu-item" onclick={showInvite}>
            <Icon name="plus" size={14} /> Invite people
          </button>
        {/if}
        {#if tight}
          <div class="menu-sep"></div>
        {/if}
        {#if ch}
          <button class="menu-item" onclick={exportChannel}>
            <Icon name="download" size={14} /> Export history
          </button>
        {/if}
        {#if g.kind === "dm"}
          {#if g.dmPeer}
            <button class="menu-item" onclick={(e) => openProfilePopover(g.dmPeer, e.currentTarget)}>
              <Icon name="spark" size={14} /> View profile
            </button>
          {/if}
          {#if (g.dmMembers ?? 2) > 2}
            <button
              class="menu-item"
              onclick={() =>
                (S.modal = { kind: "renameGroup", guildId: g.id, current: g.dmNamed ? g.name : "" })}
            >
              <Icon name="edit" size={14} /> Rename group
            </button>
          {/if}
        {:else}
          <!-- One door instead of a pile: emoji, roles, bans and rename all
               live inside the guild hub now (renaming is the hub's Overview
               panel). The menu keeps only the exit — leaving isn't managing. -->
          <button class="menu-item" onclick={() => (S.modal = { kind: "guildHub" })}>
            <Icon name="gear" size={14} /> Guild settings
          </button>
          <div class="menu-sep"></div>
          <button class="menu-item danger" onclick={confirmLeave}>
            <Icon name={g.isOwner ? "trash" : "door"} size={14} />
            {g.isOwner ? "Delete guild" : "Leave guild"}
          </button>
        {/if}
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
    /* Above the message rows (which raise their hover bar / attachments to
       z-30): the header's dropdown menus overhang the feed, and a menu that
       paints UNDER a message also hands it the pointer — that's what made
       hovering the guild menu pop message hover-bars and images. */
    z-index: 40;
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
  /* Where you ARE outranks everything you can do here. The title used to be a
     plain flex item next to an action row of nowrap buttons: flex handed the
     whole width to the side that refused to shrink, so between 769 and 1200px
     the channel name measured exactly 0px and only its `#` survived. It now
     takes the slack and keeps a floor; the search box is the piece that gives.
     8ch is a name you can still recognise, not a name you can read. */
  .title {
    gap: 6px;
    color: var(--text-muted);
    flex: 1 1 auto;
    min-width: 8ch;
  }
  /* The action row gives up its own slack — the search box has min-width:0 and
     collapses first — but never goes below what its buttons actually measure.
     With a plain `min-width: 0` flex was happy to hand it a box narrower than
     its contents, and since none of the buttons wrap, the last two simply
     spilled past the column's edge and were clipped: at 800px the Invite
     button and the overflow menu were on screen but unreachable. */
  .chat-head > .row:last-child {
    flex: 0 1 auto;
    min-width: min-content;
  }
  .search-wrap {
    flex: 0 1 auto;
  }
  /* The channel-type glyph carries the accent — a small "you are here" tint. */
  .title :global(svg) {
    color: var(--accent-hover);
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
    font-size: var(--fs-compact);
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
    min-width: 0;
  }
  .search-box {
    /* Fluid: shrinks with the window so it never overlaps the channel name,
       but stays usable (min 84px) and caps at its comfortable width. Focus
       stretches it — room appears exactly when you start typing. */
    width: clamp(84px, 20vw, 190px);
    min-width: 0;
    padding: 5px 10px;
    font-size: var(--fs-ui);
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
  .search-glyph {
    display: none;
  }
  /* Below 1000px there is no width left to share: giving the channel name its
     8ch floor squeezed this field to literally zero, which is worse than not
     drawing it. So it becomes a 32px stub with a magnifier in it and floats
     back out over the button row the moment it has focus or a query — the
     buttons it covers are all still one Escape away. */
  @media (max-width: 1000px) {
    .search-wrap {
      flex: 0 0 auto;
      width: 32px;
    }
    .search-box,
    .search-box:focus {
      width: 100%;
      min-width: 0;
      padding: 5px 6px;
    }
    .search-box.busy {
      padding-right: 26px;
    }
    .search-wrap:focus-within,
    .search-wrap.open {
      position: absolute;
      right: 16px;
      top: 50%;
      transform: translateY(-50%);
      width: min(320px, calc(100% - 48px));
      z-index: 3;
    }
    .search-glyph {
      display: grid;
      place-items: center;
      position: absolute;
      inset: 0;
      color: var(--text-muted);
      pointer-events: none;
    }
    .search-wrap:focus-within .search-glyph,
    .search-wrap.open .search-glyph {
      display: none;
    }
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
    font-size: var(--fs-compact);
  }
  /* Squeezed column: the words come off the call buttons and the glyph plus its
     tooltip carries them instead. The pin COUNT stays — that is data, not a
     label, and there is no other place it appears. */
  @media (max-width: 1150px) {
    .n:not(.count) {
      display: none;
    }
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
    font-size: var(--fs-compact);
    font-weight: 600;
    color: var(--ok-text);
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
    color: var(--ok-text);
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
    color: var(--ok-fg);
  }
  .pill-btn.leave {
    color: var(--danger-text);
  }
  .pill-btn.leave:hover {
    background: var(--danger-soft);
  }
  /* Round controls keep a round focus ring (the global :focus-visible rule
     would otherwise square their corners to --radius-sm). */
  .pill-btn:focus-visible,
  .search-clear:focus-visible {
    border-radius: 50%;
  }
  .invite {
    padding: 6px 12px;
  }
  .iconbtn.call {
    color: var(--ok-text);
    border-color: color-mix(in srgb, var(--ok) 45%, transparent);
  }
  .iconbtn.call:hover {
    background: var(--ok-soft);
  }
  /* Peer is already on the call — a live, inviting affordance. */
  .iconbtn.live-join {
    color: var(--danger-text);
    border-color: color-mix(in srgb, var(--danger) 50%, transparent);
    background: color-mix(in srgb, var(--danger) 12%, transparent);
    font-weight: 600;
  }
  .iconbtn.live-join:hover {
    background: color-mix(in srgb, var(--danger) 20%, transparent);
  }
  .live-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: #f04747;
    animation: ch-live-pulse 1.4s ease-in-out infinite;
  }
  @keyframes ch-live-pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.35;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .live-dot {
      animation: none;
    }
  }
  .iconbtn.endcall {
    color: var(--danger-text);
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
    font-size: var(--fs-ui);
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
