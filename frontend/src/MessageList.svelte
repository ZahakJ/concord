<script>
  // The feed: date dividers, consecutive-sender grouping, drag-and-drop
  // attachments, pins/search panels, and the out-of-sync banner.
  import Icon from "./Icon.svelte";
  import Message from "./Message.svelte";
  import Avatar from "./Avatar.svelte";
  import {
    S,
    activeGuild,
    activeChannel,
    registerFeed,
    scrollSoon,
    feedNearBottom,
    channelName,
    jumpToChannel,
    scrollToMessage,
    memberByFpr,
    nameFor,
    nameColorFor,
    flash,
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { previewText } from "./lib/attachments.js";
  import { untrack } from "svelte";

  let { onDropFiles } = $props();

  let feedEl = $state(null);
  let atBottom = $state(true);
  $effect(() => registerFeed(feedEl));

  // Entrance animation: only a genuinely-APPENDED newest message animates in.
  // Channel switches, history loads, and jump-to-message replacements render
  // statically — old rows never re-animate.
  let animateId = $state("");
  let prevCh = null;
  let prevLastId = "";
  let prevIds = new Set();
  $effect(() => {
    const msgs = S.messages;
    const ch = S.activeChannelId;
    untrack(() => {
      const ids = new Set(msgs.map((m) => m.id));
      const last = msgs[msgs.length - 1];
      // Append = same conversation AND the previous tail is still present
      // (a wholesale replacement — switch/jump — drops it).
      const appended = ch === prevCh && (!prevLastId || ids.has(prevLastId));
      if (last && appended && !prevIds.has(last.id)) animateId = last.id;
      else if (!appended) animateId = "";
      prevCh = ch;
      prevIds = ids;
      prevLastId = last?.id || "";
    });
  });

  function fmtTime(iso) {
    try {
      return new Date(iso).toLocaleString([], {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      });
    } catch {
      return "";
    }
  }

  // Clicking a pinned message jumps to it in the feed (like Discord) and
  // closes the panel so the flash-highlighted row isn't hidden behind it.
  function jumpToPin(m) {
    if (scrollToMessage(m.id)) S.showPins = false;
    else flash("That message isn't loaded yet");
  }

  const pinned = $derived(S.messages.filter((m) => m.pinned && !m.deleted));
  const byId = $derived(new Map(S.messages.map((m) => [m.id, m])));
  const isDMView = $derived(activeGuild()?.kind === "dm");

  // Empty-channel greeting: what to show before the first message exists.
  // Notes and DMs get personal copy; guild channels get "start of #channel".
  const emptyInfo = $derived.by(() => {
    const g = activeGuild();
    if (g?.dmNotes)
      return {
        icon: "edit",
        title: "Your private notes",
        body: "A scratchpad only you can read — drafts, links, reminders. It syncs to your other devices, encrypted the whole way.",
      };
    if (g?.kind === "dm") {
      const group = (g.dmMembers ?? 2) > 2;
      return {
        icon: "smile",
        title: g.name || "New conversation",
        body: group
          ? `This is the very start of ${g.name || "this group"}. Everything here is end-to-end encrypted. Say hi 👋`
          : `This is the very beginning of your conversation with ${g.name || "your friend"}. It's end-to-end encrypted — just the two of you. Say hi 👋`,
      };
    }
    const c = activeChannel();
    const name = c?.name || "this-channel";
    return {
      icon: c?.type === "voice" ? "speaker" : "hash",
      title: `Welcome to #${name}`,
      body:
        c?.type === "voice"
          ? `This is the chat alongside the ${name} voice channel. Drop a link or a note for whoever's on the call.`
          : `This is the start of #${name}. Say hi 👋`,
    };
  });

  // The id of the first message newer than where we left off (and not our own),
  // marking where the "New messages" divider goes. "" when nothing is new.
  const newLineId = $derived.by(() => {
    if (!S.readAnchor) return "";
    const m = S.messages.find(
      (x) => x.kind === "" && x.sender !== S.identity.fingerprint && x.sent > S.readAnchor,
    );
    return m ? m.id : "";
  });

  // rows: messages annotated with divider/grouping info.
  const GROUP_WINDOW_MS = 5 * 60 * 1000;
  const rows = $derived.by(() => {
    const out = [];
    let prev = null;
    for (const m of S.messages) {
      const day = new Date(m.sent).toDateString();
      const newDay = !prev || new Date(prev.sent).toDateString() !== day;
      const compact =
        !newDay &&
        prev &&
        prev.kind === "" &&
        m.kind === "" &&
        prev.sender === m.sender &&
        !m.replyTo &&
        !prev.deleted &&
        new Date(m.sent) - new Date(prev.sent) < GROUP_WINDOW_MS;
      out.push({ m, newDay, day, compact });
      prev = m;
    }
    return out;
  });

  function fmtDay(day) {
    const d = new Date(day);
    const today = new Date().toDateString();
    const yesterday = new Date(Date.now() - 86400000).toDateString();
    if (day === today) return "Today";
    if (day === yesterday) return "Yesterday";
    return d.toLocaleDateString([], { weekday: "long", month: "long", day: "numeric" });
  }

  // Drag & drop attachments over the feed.
  let dragOver = $state(false);
  let dragDepth = 0;
  function onDragEnter(e) {
    if (![...(e.dataTransfer?.types || [])].includes("Files")) return;
    dragDepth++;
    dragOver = true;
  }
  function onDragLeave() {
    if (--dragDepth <= 0) {
      dragDepth = 0;
      dragOver = false;
    }
  }
  function onDrop(e) {
    e.preventDefault();
    dragDepth = 0;
    dragOver = false;
    onDropFiles?.([...(e.dataTransfer?.files || [])]);
  }

  async function openSearchResult(m) {
    S.searchResults = null;
    S.searchQuery = "";
    await jumpToChannel(m.channelId);
    // Flash the hit once the channel's messages are in the DOM (silently a
    // no-op if the message is further back than the loaded window).
    requestAnimationFrame(() => scrollToMessage(m.id));
  }
</script>

{#if activeGuild()?.outOfSync}
  <div class="oos-banner">
    ⚠ Catching up… this guild changed while you were away. It'll sync automatically as soon as
    someone who has those updates comes online. If it's stuck, ask an owner or moderator to
    re-invite you (your history stays either way).
  </div>
{/if}

{#if S.showPins}
  <div class="pins-anchor">
    <section class="pins-pop" aria-label="Pinned messages">
      <header class="pins-head">
        <span class="pins-title">
          <Icon name="pin" size={13} />
          Pinned messages
          {#if pinned.length}<span class="pins-count">{pinned.length}</span>{/if}
        </span>
        <button class="mini" title="Close" aria-label="Close pinned messages" onclick={() => (S.showPins = false)}>
          <Icon name="close" size={12} />
        </button>
      </header>
      <div class="pins-list">
        {#each pinned as m (m.id)}
          {@const mem = memberByFpr(m.sender)}
          <div class="pin-item">
            <button class="pin-jump" title="Jump to message" onclick={() => jumpToPin(m)}>
              <Avatar
                name={nameFor(m.sender, m.senderName)}
                emoji={mem?.emoji}
                color={mem?.color}
                image={mem?.avatar}
                size={28}
              />
              <span class="pin-body">
                <span class="pin-meta">
                  <strong style={nameColorFor(m.sender) ? `color:${nameColorFor(m.sender)}` : ""}
                    >{nameFor(m.sender, m.senderName)}</strong
                  >
                  <span class="muted tiny">{fmtTime(m.sent)}</span>
                </span>
                <span class="pin-text">{previewText(m.content).replace(/\s+/g, " ").trim().slice(0, 160) || "(empty message)"}</span>
              </span>
            </button>
            <button
              class="mini unpin"
              title="Unpin"
              aria-label="Unpin message"
              onclick={() => api.pinMessage(m.channelId, m.id)}
            >
              <Icon name="close" size={11} />
            </button>
          </div>
        {:else}
          <div class="pins-empty">
            <span class="pins-empty-badge"><Icon name="pin" size={18} /></span>
            <strong>No pinned messages yet</strong>
            <span class="muted small">Hover a message and hit the pin — it'll show up here for everyone.</span>
          </div>
        {/each}
      </div>
    </section>
  </div>
{/if}

{#if S.searchResults !== null}
  <div class="side-panel">
    <div class="search-head">
      <span class="muted">
        {S.searchResults.length} result{S.searchResults.length === 1 ? "" : "s"}
      </span>
      <button class="mini" aria-label="Close search" onclick={() => ((S.searchResults = null), (S.searchQuery = ""))}>
        <Icon name="close" size={11} />
      </button>
    </div>
    {#each S.searchResults as m (m.id)}
      <button class="search-hit" onclick={() => openSearchResult(m)}>
        <span class="muted small">{channelName(m.channelId)}</span>
        <span><strong>{m.senderName || m.sender.slice(0, 9)}</strong>: {previewText(m.content).slice(0, 100)}</span>
      </button>
    {/each}
  </div>
{/if}

<div
  class="feed"
  bind:this={feedEl}
  role="log"
  aria-label="Messages"
  ondragenter={onDragEnter}
  ondragleave={onDragLeave}
  ondragover={(e) => e.preventDefault()}
  ondrop={onDrop}
  onscroll={() => {
    atBottom = feedNearBottom();
    if (S.newBelow && atBottom) S.newBelow = false;
  }}
>
  {#each rows as row (row.m.id)}
    {#if row.newDay}
      <div class="day-divider"><span>{fmtDay(row.day)}</span></div>
    {/if}
    {#if row.m.id === newLineId}
      <div class="new-divider"><span>NEW</span></div>
    {/if}
    {#if row.m.kind === "system" && isDMView}
      <!-- DMs skip join/create notices — noise in a 1:1 -->
    {:else if row.m.kind === "system"}
      <div class="system-msg" class:enter={row.m.id === animateId}>
        <span>
          <Icon name="spark" size={11} />
          <strong>{row.m.senderName || row.m.sender.slice(0, 9)}</strong>
          {row.m.content}
        </span>
      </div>
    {:else}
      <Message
        m={row.m}
        compact={row.compact}
        entering={row.m.id === animateId}
        replyRef={row.m.replyTo ? byId.get(row.m.replyTo) : null}
      />
    {/if}
  {:else}
    <div class="empty">
      <div class="empty-badge">
        <Icon name={emptyInfo.icon} size={28} />
      </div>
      <h3>{emptyInfo.title}</h3>
      <p class="muted">{emptyInfo.body}</p>
    </div>
  {/each}

  {#if S.newBelow}
    <button class="new-below" onclick={scrollSoon}>
      New messages <span class="arrow">↓</span>
    </button>
  {:else if !atBottom}
    <button class="jump-bottom" title="Jump to latest" aria-label="Jump to latest" onclick={scrollSoon}>
      <Icon name="chevron" size={18} />
    </button>
  {/if}

  {#if dragOver}
    <div class="drop-overlay">
      <div class="drop-card">
        <Icon name="attach" size={28} />
        <strong>Drop to send</strong>
        <span class="muted">Files up to 25 MB, end-to-end encrypted</span>
      </div>
    </div>
  {/if}
</div>

<style>
  .oos-banner {
    border-bottom: 1px solid var(--border);
    background: var(--danger-soft);
    color: var(--text);
    padding: 8px 16px;
    font-size: 13px;
  }
  .side-panel {
    border-bottom: 1px solid var(--border);
    background: var(--bg-1);
    padding: 8px 16px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    max-height: 200px;
    overflow-y: auto;
  }
  /* Pinned-messages popover: floats below the header's pin button, over the
     feed, via a zero-height positioning anchor (the chat column clips it). */
  .pins-anchor {
    position: relative;
    height: 0;
    z-index: 25;
  }
  .pins-pop {
    position: absolute;
    top: 8px;
    right: 14px;
    width: min(380px, calc(100vw - 48px));
    max-height: min(420px, 60vh);
    display: flex;
    flex-direction: column;
    background: var(--bg-elevated, var(--bg-1));
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-pop);
    overflow: hidden;
    animation: pins-in 0.16s cubic-bezier(0.2, 0.8, 0.2, 1);
    transform-origin: top right;
  }
  @keyframes pins-in {
    from {
      opacity: 0;
      transform: translateY(-4px) scale(0.98);
    }
  }
  .pins-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    padding: 9px 8px 9px 12px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-1);
  }
  .pins-title {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    font-weight: 700;
    letter-spacing: 0.02em;
    color: var(--text);
  }
  .pins-count {
    font-size: 10px;
    font-weight: 700;
    padding: 1px 6px;
    border-radius: 999px;
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .pins-list {
    overflow-y: auto;
    padding: 6px;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .pin-item {
    display: flex;
    align-items: flex-start;
    gap: 2px;
    font-size: 13px;
    border-radius: var(--radius-sm);
  }
  .pin-item .unpin {
    opacity: 0;
    margin-top: 6px;
    transition: opacity 0.1s ease;
  }
  .pin-item:hover .unpin,
  .pin-item:focus-within .unpin {
    opacity: 1;
  }
  .pin-item .unpin:hover {
    background: var(--danger-soft);
    color: var(--danger);
  }
  .pin-text {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-muted);
  }
  .pin-jump:hover .pin-text {
    color: var(--text);
  }
  .pins-empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    gap: 4px;
    padding: 22px 18px;
  }
  .pins-empty-badge {
    width: 40px;
    height: 40px;
    border-radius: 14px;
    display: grid;
    place-items: center;
    background: var(--accent-soft);
    color: var(--accent-hover);
    margin-bottom: 4px;
  }
  .pins-empty strong {
    font-size: 13px;
  }
  .pins-empty .small {
    line-height: 1.45;
  }
  .search-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 12px;
  }
  .search-hit {
    background: transparent;
    color: var(--text);
    text-align: left;
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 6px 8px;
    border-radius: var(--radius-sm);
    font-size: 13px;
  }
  .search-hit:hover {
    background: var(--bg-3);
  }
  .mini {
    padding: 2px 6px;
    background: transparent;
    color: var(--text-muted);
    display: grid;
    place-items: center;
  }
  .mini:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .feed {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    /* Spacing tracks the density vars (Appearance: Cozy/Compact) in app.css. */
    padding: var(--feed-pad, 16px);
    display: flex;
    flex-direction: column;
    gap: var(--msg-gap, 12px);
    position: relative;
  }
  .empty {
    margin: auto;
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    gap: 4px;
    max-width: 400px;
    padding: 24px 16px;
  }
  .empty-badge {
    position: relative;
    width: 64px;
    height: 64px;
    border-radius: 22px;
    display: grid;
    place-items: center;
    background: var(--accent-soft);
    color: var(--accent-hover);
    margin-bottom: 10px;
  }
  /* A dashed "orbit" ring + a small satellite dot make it feel illustrated
     without any image assets (strict CSP: inline CSS only). */
  .empty-badge::before {
    content: "";
    position: absolute;
    inset: -9px;
    border-radius: 28px;
    border: 1.5px dashed color-mix(in srgb, var(--accent) 38%, transparent);
  }
  .empty-badge::after {
    content: "";
    position: absolute;
    top: -13px;
    right: -11px;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    opacity: 0.55;
  }
  .empty h3 {
    margin: 0;
    font-size: 18px;
  }
  .empty p {
    margin: 0;
    font-size: 13px;
    line-height: 1.55;
  }
  .day-divider {
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--text-muted);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    margin: 6px 0 -4px;
  }
  .day-divider::before,
  .day-divider::after {
    content: "";
    flex: 1;
    height: 1px;
    /* rule fades out toward the edges — reads as a soft centered break */
    background: linear-gradient(to right, transparent, var(--border));
  }
  .day-divider::after {
    background: linear-gradient(to left, transparent, var(--border));
  }
  .day-divider span {
    padding: 2px 10px;
    font-weight: 600;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: 999px;
  }
  .new-divider {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--accent);
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.08em;
    margin: 2px 0 -4px;
  }
  .new-divider::before,
  .new-divider::after {
    content: "";
    flex: 1;
    height: 1px;
    background: color-mix(in srgb, var(--accent) 55%, transparent);
  }
  .new-divider span {
    padding: 1px 7px;
    background: var(--accent);
    color: #fff;
    border-radius: 999px;
    /* one gentle pulse when the divider appears, then settle */
    animation: new-pulse 1.5s ease-out 0.35s 1;
  }
  @keyframes new-pulse {
    0% {
      box-shadow: 0 0 0 0 color-mix(in srgb, var(--accent) 55%, transparent);
    }
    100% {
      box-shadow: 0 0 0 9px transparent;
    }
  }
  .system-msg {
    text-align: center;
    font-size: 12px;
    color: var(--text-muted);
    padding: 2px 0;
  }
  /* Newest appended system row slides in like a message (zeroed under the
     global reduced-motion override in app.css). */
  .system-msg.enter {
    animation: row-in 0.26s cubic-bezier(0.2, 0.8, 0.2, 1) backwards;
  }
  @keyframes row-in {
    from {
      opacity: 0;
      transform: translateY(8px);
    }
  }
  .system-msg span {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .system-msg strong {
    color: var(--text);
  }
  .new-below {
    position: sticky;
    bottom: 6px;
    align-self: center;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 6px 14px;
    border-radius: 14px;
    background: var(--accent);
    color: white;
    font-size: 12px;
    font-weight: 600;
    box-shadow: var(--shadow-pop);
    z-index: 15;
  }
  .new-below .arrow {
    font-size: 13px;
  }
  .jump-bottom {
    position: sticky;
    bottom: 6px;
    align-self: flex-end;
    width: 38px;
    height: 38px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    background: var(--bg-1);
    border: 1px solid var(--border);
    color: var(--text);
    box-shadow: var(--shadow-pop);
    z-index: 15;
    padding: 0;
  }
  .jump-bottom :global(svg) {
    transform: rotate(90deg);
  }
  .jump-bottom:hover {
    background: var(--bg-3);
    color: var(--accent-hover);
  }
  .pin-jump {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: flex-start;
    gap: 8px;
    background: transparent;
    color: var(--text);
    text-align: left;
    padding: 4px 6px;
    border-radius: var(--radius-sm);
  }
  .pin-jump:hover {
    background: var(--bg-3);
  }
  .pin-body {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
  }
  .pin-meta {
    display: flex;
    align-items: baseline;
    gap: 6px;
  }
  .tiny {
    font-size: 10px;
  }
  .drop-overlay {
    position: fixed;
    inset: 0;
    display: grid;
    place-items: center;
    background: color-mix(in srgb, var(--bg-0) 72%, transparent);
    z-index: 60;
    pointer-events: none;
  }
  .drop-card {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    border: 2px dashed var(--accent);
    border-radius: var(--radius-lg);
    background: var(--bg-1);
    color: var(--accent-hover);
    padding: 26px 42px;
    font-size: 14px;
  }
  .small {
    font-size: 12px;
  }
  /* Shared jump-target flash (applied by scrollToMessage): a brief accent
     wash + hairline ring that fades, so the eye finds the row. Duration
     matches the 1.2s class removal in state.svelte.js; the global
     reduced-motion override in app.css collapses it to a blink. */
  :global(.flash-highlight) {
    animation: flash-bg 1.2s ease;
  }
  @keyframes flash-bg {
    0%,
    35% {
      background: var(--accent-soft);
      box-shadow: inset 2px 0 0 var(--accent);
    }
    100% {
      background: transparent;
      box-shadow: inset 2px 0 0 transparent;
    }
  }
</style>
