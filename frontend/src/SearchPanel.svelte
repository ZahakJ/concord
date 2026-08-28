<script>
  // Global search results panel: filter chips, match-highlighted hits across
  // every conversation, and jump-to-message. Rendered above the feed while a
  // search is open (S.searchResults !== null) or in flight.
  import { tick } from "svelte";
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import EmptyState from "./EmptyState.svelte";
  import {
    S,
    jumpToChannel,
    scrollToMessage,
    memberByFpr,
    flash,
    clockOpts,
  } from "./lib/state.svelte.js";
  import { removeChip, closeSearch, insertFilter, FILTERS } from "./lib/search.js";
  import { previewText } from "./lib/attachments.js";
  import { stripMarkdown } from "./lib/forum.js";
  import { syncLayer } from "./lib/navstack.svelte.js";

  const open = $derived(S.searchResults !== null || S.searchLoading);
  // Results cover the whole feed; back closes them rather than the app.
  syncLayer("panel", () => open, closeSearch);
  const results = $derived(S.searchResults ?? []);

  function fmtTime(iso) {
    try {
      return new Date(iso).toLocaleString([], {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        // Honor the 12/24h preference here too — the pref promises "the feed,
        // pins and search", and search was the one place not keeping it.
        ...clockOpts(),
      });
    } catch {
      return "";
    }
  }

  // whereFor: human label for the conversation a hit lives in — "#channel ·
  // guild" for guilds, the counterpart's name for DMs.
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
    // Through the markdown stripper first: a result read "> **the** plan" where
    // the message says "the plan", so the one line whose whole job is to show
    // you the words you searched for was showing you the syntax around them.
    const text = stripMarkdown(previewText(content));
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
      <button class="sp-close" aria-label="Close search" title="Close search" onclick={closeSearch}>
        <Icon name="close" size={11} />
      </button>
    </div>

    <!-- The operator vocabulary, clickable. It used to live in a native title=
         on the input: a tooltip nobody hovers a text field long enough to see,
         and one a touch device cannot open at all. A chip that types the prefix
         teaches the syntax by using it. -->
    <div class="sp-syntax">
      <span class="sp-syntax-lead">Narrow it</span>
      {#each FILTERS as f (f.prefix)}
        <button class="sp-add" onclick={() => insertFilter(f.prefix)}>
          <code>{f.prefix}</code><span class="sp-add-hint">{f.hint}</span>
        </button>
      {/each}
    </div>

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
                <span class="sp-time">{fmtTime(m.sent)}</span>
              </span>
              <span class="sp-text">
                {#each segs(snippet(m.content)) as p, i (i)}{#if p.hit}<mark>{p.t}</mark>{:else}{p.t}{/if}{/each}
              </span>
            </span>
          </button>
        {:else}
          <div class="sp-empty">
            <EmptyState
              icon="search"
              headline="No matches"
              sub="Nothing in any conversation matches that — try fewer words or drop a filter."
            />
          </div>
        {/each}
      {/if}
    </div>
  </div>
{/if}

<style>
  /* The results OWN the column while they are open.
     They used to be a 380px band in the flow with the live channel continuing
     underneath at full brightness — poll, messages, composer, all still
     readable and clickable — and the band's hard bottom edge landed wherever it
     landed, which was usually through the middle of a message row. A container
     that ends mid-row reads as a rendering fault, not as a panel, and a set of
     results with a different conversation showing under them is not a set of
     results anyone can read.
     Absolute rather than replacing the feed, because the feed must still be
     mounted and laid out: unmounting it would throw away the scroll position
     you were reading at, so closing the search would drop you at the bottom of
     the channel. It is covered, not destroyed. */
  .search-panel {
    position: absolute;
    inset: 0;
    z-index: 4;
    border-bottom: 1px solid var(--border);
    background: var(--bg-1);
    display: flex;
    flex-direction: column;
    box-shadow: var(--shadow-pop);
  }
  .sp-head {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    padding: 8px var(--sp-edge) 6px;
    font-size: var(--fs-compact);
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
    font-size: var(--fs-tiny);
    letter-spacing: 0.03em;
    text-transform: uppercase;
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
    padding: 0 var(--sp-edge) 8px;
  }
  /* Quieter than the active-filter chips above them on purpose: these are an
     offer, those are state. */
  .sp-syntax {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 5px;
    padding: 0 var(--sp-edge) 8px;
  }
  .sp-syntax-lead {
    font-size: var(--fs-tiny);
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-faint);
    margin-right: 2px;
  }
  .sp-add {
    display: inline-flex;
    align-items: baseline;
    gap: 5px;
    padding: 2px 9px;
    border-radius: 999px;
    background: var(--bg-3);
    border: 1px solid var(--border);
    color: var(--text-muted);
    font-size: var(--fs-small);
    line-height: 1.5;
    transition:
      border-color var(--dur-quick) ease,
      color var(--dur-quick) ease;
  }
  .sp-add code {
    font-family: ui-monospace, monospace;
    font-weight: 600;
    color: var(--text);
  }
  .sp-add-hint {
    font-size: var(--fs-tiny);
    color: var(--text-faint);
  }
  .sp-add:hover,
  .sp-add:focus-visible {
    border-color: var(--accent);
    color: var(--text);
  }
  .sp-add:hover code,
  .sp-add:focus-visible code {
    color: var(--accent-hover);
  }
  .sp-chip {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    padding: 2px 4px 2px 9px;
    border-radius: 999px;
    background: var(--accent-soft);
    border: 1px solid color-mix(in srgb, var(--accent) 35%, transparent);
    color: var(--accent-hover);
    font-size: var(--fs-small);
    font-weight: 600;
    line-height: 1.5;
  }
  .sp-chip-x {
    position: relative;
    padding: 2px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    background: transparent;
    color: inherit;
  }
  /* A 13px ✕ hanging off the end of a filter pill. A pad rather than a bigger
     dot, so the pill keeps its height; kept modest on purpose — chips sit 6px
     apart and an overlapping pad would put one chip's remove button on top of
     its neighbour's. The phone block below grows the pill itself instead. */
  .sp-chip-x::after {
    content: "";
    position: absolute;
    inset: -9px -3px;
  }
  .sp-chip-x:hover {
    background: color-mix(in srgb, var(--accent) 28%, transparent);
  }
  .sp-list {
    overflow-y: auto;
    -webkit-overflow-scrolling: touch;
    overscroll-behavior-y: contain;
    padding: 0 10px 8px;
    display: flex;
    flex-direction: column;
    gap: 2px;
    transition: opacity var(--dur-standard) ease;
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
    font-size: var(--fs-ui);
    transition:
      background 0.1s ease,
      transform var(--dur-quick) ease;
    animation: sp-in 0.2s ease backwards;
  }
  /* Entrance stagger, capped after the first rows — anything past the fold
     shouldn't hold a long animation chain. */
  .sp-hit:nth-child(2) { animation-delay: 0.025s; }
  .sp-hit:nth-child(3) { animation-delay: 0.05s; }
  .sp-hit:nth-child(4) { animation-delay: 0.075s; }
  .sp-hit:nth-child(5) { animation-delay: 0.1s; }
  .sp-hit:nth-child(6) { animation-delay: 0.125s; }
  .sp-hit:nth-child(7) { animation-delay: var(--dur-standard); }
  .sp-hit:nth-child(n + 8) { animation-delay: 0.17s; }
  @keyframes sp-in {
    from {
      opacity: 0;
      transform: translateY(5px);
    }
  }
  /* Android's WebView holds :hover after a tap, so an ungated rule left the
     result you just jumped from nudged 2px sideways and highlighted for the
     rest of the session. :active covers the touch feedback instead. */
  @media (pointer: fine) {
    .sp-hit:hover {
      background: var(--bg-3);
      transform: translateX(2px);
    }
  }
  .sp-hit:active {
    background: var(--bg-3);
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
    gap: var(--sp-2);
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
    font-size: var(--fs-small);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .sp-time {
    margin-left: auto;
    color: var(--text-faint);
    font-size: var(--fs-tiny);
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
  /* The illustrated EmptyState centers itself content-wise; this wrapper only
     centers it inside the results strip (EmptyState leaves that to parents). */
  .sp-empty {
    display: grid;
    place-items: center;
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
    border-radius: var(--radius-sm);
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
  @media (pointer: coarse), (max-width: 768px) {
    .sp-hit {
      padding: 10px 8px;
      min-height: var(--tap-min);
    }
    .sp-close {
      min-width: var(--tap-min);
      min-height: var(--tap-min);
    }
    .sp-scope {
      display: none;
    }
    /* The pill grows rather than the ✕ growing a pad: the pads on chips 6px
       apart would overlap and one chip's remove button would sit over its
       neighbour's. */
    .sp-chips {
      gap: var(--sp-2);
    }
    .sp-add {
      min-height: var(--tap-min);
      padding: 0 12px;
      font-size: var(--fs-ui);
    }
    .sp-chip {
      min-height: 40px;
      padding: 0 4px 0 12px;
      font-size: var(--fs-ui);
    }
    .sp-chip-x {
      width: 32px;
      height: 32px;
    }
    .sp-chip-x::after {
      inset: -6px;
    }
  }
</style>
