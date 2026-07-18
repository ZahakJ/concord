<script>
  // Global search results panel: filter chips, match-highlighted hits across
  // every conversation, and jump-to-message. Rendered above the feed while a
  // search is open (S.searchResults !== null) or in flight.
  import { tick } from "svelte";
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import {
    S,
    jumpToChannel,
    scrollToMessage,
    memberByFpr,
    flash,
  } from "./lib/state.svelte.js";
  import { removeChip, closeSearch, expandSearch } from "./lib/search.js";
  import { engineLabel } from "./lib/assist.js";
  import { previewText } from "./lib/attachments.js";

  // The local assistant can widen a search with related terms — offered only
  // when the user has switched it on (S.assist) and strictly on-device.
  const assistOn = $derived(!!S.assist?.enabled);

  const open = $derived(S.searchResults !== null || S.searchLoading);
  const results = $derived(S.searchResults ?? []);

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

  // whereFor: human label for the conversation a hit lives in — "#channel ·
  // guild" for servers, the counterpart's name for DMs.
  function whereFor(chId) {
    for (const g of S.guilds) {
      const c = g.channels.find((x) => x.id === chId);
      if (!c) continue;
      if (g.kind === "dm") return { icon: "edit", label: g.name || "Direct message" };
      return { icon: "hash", label: `${c.name} · ${g.name}` };
    }
    return { icon: "hash", label: "unknown channel" };
  }

  const escapeRe = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

  // snippet: readable preview windowed around the first matched term, so the
  // hit is visible even in a long message.
  function snippet(content) {
    const text = previewText(content);
    const lower = text.toLowerCase();
    let idx = -1;
    for (const t of S.searchTerms) {
      const i = lower.indexOf(t.toLowerCase());
      if (i !== -1 && (idx === -1 || i < idx)) idx = i;
    }
    const start = idx > 70 ? Math.max(0, idx - 40) : 0;
    let s = text.slice(start, start + 160);
    if (start > 0) s = "…" + s;
    if (start + 160 < text.length) s += "…";
    return s;
  }

  // segs: split a snippet into plain/matched runs. A single capture group
  // around the alternation makes String.split alternate miss/hit segments.
  function segs(text) {
    const terms = S.searchTerms;
    if (!terms.length) return [{ t: text, hit: false }];
    const re = new RegExp(`(${terms.map(escapeRe).join("|")})`, "gi");
    return text.split(re).map((t, i) => ({ t, hit: i % 2 === 1 }));
  }

  // Jump: close the panel, land in the hit's conversation, then flash the row.
  // The double-rAF runs after selectChannel's own scroll-to-bottom rAF, so the
  // jump wins the race.
  async function openResult(m) {
    const { id, channelId } = m;
    closeSearch();
    await jumpToChannel(channelId);
    await tick();
    requestAnimationFrame(() =>
      requestAnimationFrame(() => {
        if (!scrollToMessage(id)) flash("That message isn't loaded yet");
      }),
    );
  }
</script>

{#if open}
  <div class="search-panel" role="region" aria-label="Search results">
    <div class="sp-head">
      <span class="sp-title">
        <Icon name="search" size={13} />
        {#if S.searchLoading}
          Searching…
        {:else}
          {results.length} result{results.length === 1 ? "" : "s"}
        {/if}
      </span>
      <span class="sp-scope" title="Search covers every channel and DM, not just this one">
        all conversations
      </span>
      {#if assistOn && !S.searchLoading}
        <button
          class="sp-assist"
          title="Ask your local assistant for related terms and fold their matches in — runs entirely on this machine"
          onclick={expandSearch}
        >
          <Icon name="spark" size={11} /> related terms
        </button>
      {/if}
      <button class="sp-close" aria-label="Close search" title="Close search" onclick={closeSearch}>
        <Icon name="close" size={11} />
      </button>
    </div>

    {#if S.searchAssistTerms.length}
      <!-- The provenance line says which engine suggested these terms, from the
           response — not "your local model", which was only ever true while
           Ollama was the only engine that could answer. -->
      <div class="sp-assist-note" role="note">
        <Icon name="spark" size={10} />
        <span class="sp-engine" class:brain={S.searchAssistEngine === "brain"}>
          {engineLabel(S.searchAssistEngine)}
        </span>
        {S.searchAssistNote || "also searched:"}
        {#each S.searchAssistTerms as t (t)}<span class="sp-aterm">{t}</span>{/each}
      </div>
    {/if}

    {#if S.searchChips.length}
      <div class="sp-chips" aria-label="Active search filters">
        {#each S.searchChips as c (c.raw)}
          <span class="sp-chip">
            {c.label}
            <button
              class="sp-chip-x"
              aria-label={`Remove filter ${c.label}`}
              title="Remove filter"
              onclick={() => removeChip(c)}
            >
              <Icon name="close" size={9} />
            </button>
          </span>
        {/each}
      </div>
    {/if}

    <div class="sp-list" class:dim={S.searchLoading}>
      {#if S.searchLoading && !results.length}
        {#each [0, 1, 2] as i (i)}
          <div class="sp-skel" aria-hidden="true">
            <span class="sk-dot"></span>
            <span class="sk-lines"><span class="sk-a"></span><span class="sk-b"></span></span>
          </div>
        {/each}
      {:else}
        {#each results as m (m.id)}
          {@const mem = memberByFpr(m.sender)}
          {@const where = whereFor(m.channelId)}
          <button class="sp-hit" onclick={() => openResult(m)} title="Jump to message">
            <Avatar
              name={m.senderName || m.sender.slice(0, 2)}
              emoji={mem?.emoji}
              color={mem?.color}
              image={mem?.avatar}
              size={30}
            />
            <span class="sp-body">
              <span class="sp-meta">
                <strong>{m.senderName || m.sender.slice(0, 9)}</strong>
                <span class="sp-where"><Icon name={where.icon} size={10} />{where.label}</span>
                {#if m.ocrMatch}
                  <span
                    class="sp-ocr"
                    title="Your search matched text found inside this message's image — read out locally on this device"
                  >
                    <Icon name="imagetext" size={10} /> matched text in image
                  </span>
                {/if}
                <span class="sp-time">{fmtTime(m.sent)}</span>
              </span>
              <span class="sp-text">
                {#each segs(snippet(m.content)) as p, i (i)}{#if p.hit}<mark>{p.t}</mark>{:else}{p.t}{/if}{/each}
              </span>
            </span>
          </button>
        {:else}
          <div class="sp-empty">
            <Icon name="search" size={18} />
            <strong>No matches</strong>
            <span>Nothing in any conversation matches that — try fewer words or drop a filter.</span>
          </div>
        {/each}
      {/if}
    </div>
  </div>
{/if}

<style>
  .search-panel {
    border-bottom: 1px solid var(--border);
    background: var(--bg-1);
    display: flex;
    flex-direction: column;
    max-height: min(46vh, 380px);
  }
  .sp-head {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 16px 6px;
    font-size: 12px;
  }
  .sp-title {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-weight: 600;
    color: var(--text);
  }
  .sp-scope {
    margin-right: auto;
    padding: 1px 8px;
    border-radius: 999px;
    border: 1px solid var(--border);
    background: var(--bg-2);
    color: var(--text-muted);
    font-size: 10.5px;
    letter-spacing: 0.03em;
    text-transform: uppercase;
    white-space: nowrap;
  }
  .sp-assist {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 2px 10px;
    border-radius: 999px;
    background: var(--accent-soft);
    border: 1px solid color-mix(in srgb, var(--accent) 35%, transparent);
    color: var(--accent-hover);
    font-size: 11px;
    font-weight: 600;
    white-space: nowrap;
  }
  .sp-assist:hover {
    background: color-mix(in srgb, var(--accent) 24%, transparent);
  }
  .sp-assist-note {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 5px;
    padding: 0 16px 8px;
    color: var(--text-muted);
    font-size: 11.5px;
  }
  /* Engine chip: distinct fills for "local" and "shared brain" so the two
     never read as the same badge with different wording. */
  .sp-engine {
    padding: 1px 7px;
    border-radius: 999px;
    background: var(--accent-soft);
    border: 1px solid color-mix(in srgb, var(--accent) 35%, transparent);
    color: var(--accent-hover);
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    white-space: nowrap;
  }
  .sp-engine.brain {
    background: color-mix(in srgb, var(--warn) 14%, transparent);
    border-color: color-mix(in srgb, var(--warn) 45%, transparent);
    color: var(--warn);
  }
  .sp-aterm {
    padding: 1px 7px;
    border-radius: 999px;
    background: var(--bg-2);
    border: 1px solid var(--border);
    color: var(--text);
    font-size: 11px;
  }
  .sp-ocr {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 1px 7px;
    border-radius: 999px;
    background: var(--accent-soft);
    border: 1px solid color-mix(in srgb, var(--accent) 30%, transparent);
    color: var(--accent-hover);
    font-size: 10.5px;
    font-weight: 600;
    white-space: nowrap;
  }
  .sp-close {
    padding: 3px 6px;
    background: transparent;
    color: var(--text-muted);
    display: grid;
    place-items: center;
    border-radius: var(--radius-sm);
  }
  .sp-close:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .sp-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    padding: 0 16px 8px;
  }
  .sp-chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 2px 4px 2px 9px;
    border-radius: 999px;
    background: var(--accent-soft);
    border: 1px solid color-mix(in srgb, var(--accent) 35%, transparent);
    color: var(--accent-hover);
    font-size: 11.5px;
    font-weight: 600;
    line-height: 1.5;
  }
  .sp-chip-x {
    padding: 2px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    background: transparent;
    color: inherit;
  }
  .sp-chip-x:hover {
    background: color-mix(in srgb, var(--accent) 28%, transparent);
  }
  .sp-list {
    overflow-y: auto;
    padding: 0 10px 8px;
    display: flex;
    flex-direction: column;
    gap: 2px;
    transition: opacity 0.15s ease;
  }
  .sp-list.dim {
    opacity: 0.55;
    pointer-events: none;
  }
  .sp-hit {
    display: flex;
    align-items: flex-start;
    gap: 9px;
    padding: 7px 8px;
    background: transparent;
    color: var(--text);
    text-align: left;
    border-radius: var(--radius-sm);
    font-size: 13px;
    transition:
      background 0.1s ease,
      transform 0.12s ease;
    animation: sp-in 0.2s ease backwards;
  }
  /* Entrance stagger, capped after the first rows — anything past the fold
     shouldn't hold a long animation chain. */
  .sp-hit:nth-child(2) { animation-delay: 0.025s; }
  .sp-hit:nth-child(3) { animation-delay: 0.05s; }
  .sp-hit:nth-child(4) { animation-delay: 0.075s; }
  .sp-hit:nth-child(5) { animation-delay: 0.1s; }
  .sp-hit:nth-child(6) { animation-delay: 0.125s; }
  .sp-hit:nth-child(7) { animation-delay: 0.15s; }
  .sp-hit:nth-child(n + 8) { animation-delay: 0.17s; }
  @keyframes sp-in {
    from {
      opacity: 0;
      transform: translateY(5px);
    }
  }
  .sp-hit:hover {
    background: var(--bg-3);
    transform: translateX(2px);
  }
  .sp-body {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    flex: 1;
  }
  .sp-meta {
    display: flex;
    align-items: baseline;
    gap: 8px;
    min-width: 0;
  }
  .sp-meta strong {
    flex-shrink: 0;
  }
  .sp-where {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    color: var(--text-muted);
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .sp-time {
    margin-left: auto;
    color: var(--text-faint);
    font-size: 10.5px;
    flex-shrink: 0;
  }
  .sp-text {
    color: var(--text-muted);
    overflow: hidden;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    line-height: 1.4;
    overflow-wrap: anywhere;
  }
  .sp-text mark {
    background: var(--accent-soft);
    color: var(--accent-hover);
    font-weight: 600;
    border-radius: 3px;
    padding: 0 1px;
  }
  .sp-empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    gap: 3px;
    padding: 18px 24px 14px;
    color: var(--text-muted);
    font-size: 12.5px;
  }
  .sp-empty strong {
    color: var(--text);
    font-size: 13px;
  }
  /* Loading skeleton: three shimmering placeholder rows. */
  .sp-skel {
    display: flex;
    align-items: center;
    gap: 9px;
    padding: 7px 8px;
  }
  .sk-dot {
    width: 30px;
    height: 30px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .sk-lines {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 5px;
  }
  .sk-a,
  .sk-b {
    height: 9px;
    border-radius: 4px;
  }
  .sk-a {
    width: 38%;
  }
  .sk-b {
    width: 82%;
  }
  .sk-dot,
  .sk-a,
  .sk-b {
    background: linear-gradient(100deg, var(--bg-2) 35%, var(--bg-3) 50%, var(--bg-2) 65%);
    background-size: 240% 100%;
    animation: sp-shimmer 1.1s linear infinite;
  }
  @keyframes sp-shimmer {
    from {
      background-position: 120% 0;
    }
    to {
      background-position: -120% 0;
    }
  }
  /* Touch: roomier hit rows and more of the (full-screen) column for results;
     drop the scope pill to keep the top bar to one clean line. */
  @media (pointer: coarse), (max-width: 700px) {
    .search-panel {
      max-height: 60vh;
    }
    .sp-hit {
      padding: 10px 8px;
      font-size: 14px;
    }
    .sp-close {
      min-width: 40px;
      min-height: 40px;
    }
    .sp-scope {
      display: none;
    }
  }
</style>
