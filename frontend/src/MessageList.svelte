<script>
  // The feed: date dividers, consecutive-sender grouping, drag-and-drop
  // attachments, pins/search panels, and the out-of-sync banner.
  import Icon from "./Icon.svelte";
  import Message from "./Message.svelte";
  import Avatar from "./Avatar.svelte";
  import {
    S,
    activeGuild,
    registerFeed,
    scrollSoon,
    feedNearBottom,
    channelName,
    jumpToChannel,
    scrollToMessage,
    memberByFpr,
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

  // Clicking a pinned message jumps to it in the feed (like Discord).
  function jumpToPin(m) {
    if (!scrollToMessage(m.id)) flash("That message isn't loaded yet");
  }

  const pinned = $derived(S.messages.filter((m) => m.pinned && !m.deleted));
  const byId = $derived(new Map(S.messages.map((m) => [m.id, m])));
  const isDMView = $derived(activeGuild()?.kind === "dm");

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
  <div class="side-panel">
    {#each pinned as m (m.id)}
      {@const mem = memberByFpr(m.sender)}
      <div class="pin-item">
        <button class="pin-jump" title="Jump to message" onclick={() => jumpToPin(m)}>
          <Avatar
            name={m.senderName || m.sender.slice(0, 2)}
            emoji={mem?.emoji}
            color={mem?.color}
            image={mem?.avatar}
            size={28}
          />
          <span class="pin-body">
            <span class="pin-meta">
              <strong>{m.senderName || m.sender.slice(0, 9)}</strong>
              <span class="muted tiny">{fmtTime(m.sent)}</span>
            </span>
            <span class="pin-text">{previewText(m.content).slice(0, 90)}</span>
          </span>
        </button>
        <button class="mini" title="Unpin" aria-label="Unpin" onclick={() => api.pinMessage(m.channelId, m.id)}>
          <Icon name="close" size={11} />
        </button>
      </div>
    {:else}
      <div class="muted small">No pinned messages — hover a message and hit the pin.</div>
    {/each}
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
    <div class="empty muted">No messages yet. Say hello 👋</div>
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
  .pin-item {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    font-size: 13px;
  }
  .pin-text {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
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
  :global(.flash-highlight) {
    animation: flash-bg 1.6s ease;
  }
  @keyframes flash-bg {
    0%,
    40% {
      background: var(--accent-soft);
    }
    100% {
      background: transparent;
    }
  }
</style>
