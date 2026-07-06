<script>
  // The feed: date dividers, consecutive-sender grouping, drag-and-drop
  // attachments, pins/search panels, and the out-of-sync banner.
  import Icon from "./Icon.svelte";
  import Message from "./Message.svelte";
  import {
    S,
    activeGuild,
    registerFeed,
    scrollSoon,
    feedNearBottom,
    channelName,
    jumpToChannel,
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { previewText } from "./lib/attachments.js";

  let { onDropFiles } = $props();

  let feedEl = $state(null);
  $effect(() => registerFeed(feedEl));

  const pinned = $derived(S.messages.filter((m) => m.pinned && !m.deleted));
  const byId = $derived(new Map(S.messages.map((m) => [m.id, m])));

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
    ⚠ Out of sync — this server moved on while you were away and no online member could bridge
    the gap. Ask the owner to re-invite you (your history stays).
  </div>
{/if}

{#if S.showPins}
  <div class="side-panel">
    {#each pinned as m (m.id)}
      <div class="pin-item">
        <span class="pin-text">
          <Icon name="pin" size={11} />
          <strong>{m.senderName || m.sender.slice(0, 9)}</strong>: {previewText(m.content).slice(0, 80)}
        </span>
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
    if (S.newBelow && feedNearBottom()) S.newBelow = false;
  }}
>
  {#each rows as row (row.m.id)}
    {#if row.newDay}
      <div class="day-divider"><span>{fmtDay(row.day)}</span></div>
    {/if}
    {#if row.m.kind === "system"}
      <div class="system-msg">
        <span>
          <Icon name="spark" size={11} />
          <strong>{row.m.senderName || row.m.sender.slice(0, 9)}</strong>
          {row.m.content}
        </span>
      </div>
    {:else}
      <Message m={row.m} compact={row.compact} replyRef={row.m.replyTo ? byId.get(row.m.replyTo) : null} />
    {/if}
  {:else}
    <div class="empty muted">No messages yet. Say hello 👋</div>
  {/each}

  {#if S.newBelow}
    <button class="new-below" onclick={scrollSoon}>
      New messages <span class="arrow">↓</span>
    </button>
  {/if}

  {#if dragOver}
    <div class="drop-overlay">
      <div class="drop-card">
        <Icon name="attach" size={28} />
        <strong>Drop to send</strong>
        <span class="muted">Images up to 5 MB, end-to-end encrypted</span>
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
    overflow-y: auto;
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    position: relative;
  }
  .empty {
    margin: auto;
  }
  .day-divider {
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--text-faint);
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
    background: var(--border);
  }
  .system-msg {
    text-align: center;
    font-size: 12px;
    color: var(--text-muted);
    padding: 2px 0;
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
