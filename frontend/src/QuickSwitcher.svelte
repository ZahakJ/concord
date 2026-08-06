<script>
  // Ctrl+K command palette: fuzzy jump to any channel / DM / guild, plus app
  // actions. A leading ">" or "/" filters to actions only; otherwise actions
  // rank alongside destinations under their own section header.
  import Icon from "./Icon.svelte";
  import {
    S,
    jumpToChannel,
    selectGuild,
    markAllRead,
    toggleMute,
    isMuted,
    channelShort,
    isDMChannel,
    guildUnread,
  } from "./lib/state.svelte.js";

  let query = $state("");
  let sel = $state(0);
  let inputEl = $state(null);
  let listEl = $state(null);

  $effect(() => inputEl?.focus());

  // Subsequence fuzzy match; lower score = better. Returns the matched char
  // indices so the list can highlight them, or null on no match.
  function fuzzy(text, q) {
    const lower = text.toLowerCase();
    let ti = 0;
    let score = 0;
    const idx = [];
    for (const c of q) {
      const found = lower.indexOf(c, ti);
      if (found < 0) return null;
      score += found - ti; // penalize gaps
      idx.push(found);
      ti = found + 1;
    }
    return { score: score + lower.length * 0.01, idx };
  }

  // Split a label into plain/highlighted runs from fuzzy-match indices.
  function toParts(label, idx) {
    if (!idx?.length) return [{ t: label, hit: false }];
    const parts = [];
    let last = 0;
    for (const i of idx) {
      if (i > last) parts.push({ t: label.slice(last, i), hit: false });
      const prev = parts[parts.length - 1];
      if (prev?.hit) prev.t += label[i];
      else parts.push({ t: label[i], hit: true });
      last = i + 1;
    }
    if (last < label.length) parts.push({ t: label.slice(last), hit: false });
    return parts;
  }

  // App actions. Each runs after the palette closes.
  const actions = $derived.by(() => {
    const list = [
      { id: "newdm", label: "New message", icon: "edit", run: () => (S.modal = { kind: "newDM" }) },
      { id: "create", label: "Create guild", icon: "plus", run: () => (S.modal = { kind: "create" }) },
      { id: "join", label: "Join with invite", icon: "door", run: () => (S.modal = { kind: "join" }) },
      { id: "mycal", label: "Your calendar", icon: "calendar", run: () => (S.modal = { kind: "myCalendar" }) },
      { id: "saved", label: "Saved messages", icon: "pin", run: () => (S.modal = { kind: "saved" }) },
      { id: "settings", label: "Open settings", icon: "gear", run: () => (S.modal = { kind: "settings" }) },
      { id: "appearance", label: "Open appearance", icon: "spark", run: () => (S.modal = { kind: "appearance" }) },
      { id: "status", label: "Set status", icon: "smile", run: () => (S.statusPopRequest = true) },
      { id: "read", label: "Mark all as read", icon: "check", sub: "Shift+Esc", run: () => markAllRead() },
    ];
    if (S.activeChannelId) {
      const id = S.activeChannelId;
      const muted = !!isMuted(id);
      const name = isDMChannel(id) ? channelShort(id) : `#${channelShort(id)}`;
      list.push({
        id: "mute",
        label: `${muted ? "Unmute" : "Mute"} ${name}`,
        icon: muted ? "bell" : "bellOff",
        run: () => toggleMute(id),
      });
    }
    // A list of key bindings for a device with no keys. Off the menu on touch.
    if (!S.isMobile)
      list.push({
        id: "shortcuts",
        label: "Keyboard shortcuts",
        icon: "chevron",
        sub: "Ctrl+/",
        run: () => (S.modal = { kind: "shortcuts" }),
      });
    return list.map((a) => ({ ...a, kind: "action", key: `act:${a.id}` }));
  });

  const grouped = $derived.by(() => {
    const raw = query.trim();
    const actionOnly = raw.startsWith(">") || raw.startsWith("/");
    const q = (actionOnly ? raw.slice(1) : raw).trim().toLowerCase();

    const dests = [];
    if (!actionOnly) {
      for (const g of S.guilds) {
        if (g.kind === "dm") {
          dests.push({ kind: "dm", key: `dm:${g.id}`, label: g.name, sub: "direct", g, icon: "edit" });
          continue;
        }
        dests.push({ kind: "guild", key: `g:${g.id}`, label: g.name, sub: "guild", g, icon: "diamond" });
        for (const c of g.channels) {
          const icon = c.type === "voice" ? "speaker" : c.type === "announcement" ? "megaphone" : "hash";
          dests.push({ kind: "channel", key: `c:${c.id}`, label: c.name, sub: g.name, c, g, icon });
        }
      }
    }

    if (!q) {
      // Empty query = recency. "Jump to" is the MRU trail (minus where you
      // already are), padded with the raw channel list for fresh installs.
      const byId = new Map();
      for (const d of dests) if (d.kind === "channel") byId.set(d.c.id, d);
      const recent = S.recentChannels
        .filter((cid) => cid !== S.activeChannelId)
        .map((cid) => byId.get(cid))
        .filter(Boolean);
      const seen = new Set(recent);
      const rest = dests.filter((i) => i.kind === "channel" && !seen.has(i));
      return {
        jump: [...recent, ...rest].slice(0, 6),
        acts: actionOnly ? actions : actions.slice(0, 5),
      };
    }
    const rank = (list, cap) =>
      list
        .map((i) => ({ i, m: fuzzy(i.label, q) }))
        .filter((x) => x.m)
        .sort((a, b) => a.m.score - b.m.score)
        .slice(0, cap)
        .map((x) => ({ ...x.i, parts: toParts(x.i.label, x.m.idx) }));
    return { jump: rank(dests, 8), acts: rank(actions, actionOnly ? 12 : 5) };
  });

  const flat = $derived([...grouped.jump, ...grouped.acts]);

  $effect(() => {
    query;
    sel = 0;
  });

  // Keep the selected row visible when arrowing through a scrolled list.
  $effect(() => {
    sel;
    listEl?.querySelector(".hit.sel")?.scrollIntoView({ block: "nearest" });
  });

  // Unread state for a result row, so the list shows which hit needs you.
  // Channels read their own counter; guild/DM rows aggregate like the rail.
  function unreadFor(item) {
    if (item.kind === "channel") return S.unread[item.c.id] || null;
    if (item.kind === "guild" || item.kind === "dm") {
      const u = guildUnread(item.g);
      return u.count ? u : null;
    }
    return null;
  }

  async function go(item) {
    S.quickSwitcher = false;
    if (item.kind === "action") {
      item.run();
    } else if (item.kind === "channel") {
      if (item.c.type === "voice") await selectGuild(item.g.id);
      else await jumpToChannel(item.c.id);
    } else {
      await selectGuild(item.g.id);
    }
  }

  function onKeydown(e) {
    if (e.key === "Escape") {
      S.quickSwitcher = false;
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      sel = (sel + 1) % Math.max(flat.length, 1);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      sel = (sel - 1 + Math.max(flat.length, 1)) % Math.max(flat.length, 1);
    } else if (e.key === "Enter" && flat[sel]) {
      e.preventDefault();
      go(flat[sel]);
    }
  }
</script>

{#snippet row(item, i)}
  <button class="hit" class:sel={i === sel} onclick={() => go(item)}>
    <span class="hit-icon">
      <Icon name={item.icon} size={13} />
    </span>
    <span class="hit-label">
      {#if item.parts}
        {#each item.parts as p, j (j)}{#if p.hit}<span class="hl">{p.t}</span>{:else}{p.t}{/if}{/each}
      {:else}
        {item.label}
      {/if}
    </span>
    {#if item.sub}
      <!-- Action subs are keyboard shortcuts — render them as kbd chips. -->
      {#if item.kind === "action"}
        <kbd class="hit-kbd">{item.sub}</kbd>
      {:else}
        <span class="muted hit-sub">{item.sub}</span>
      {/if}
    {/if}
    {#if item.kind !== "action"}
      {@const u = unreadFor(item)}
      {#if u?.count}
        <span class="hit-unread" class:mention={u.mentions > 0}>{u.mentions > 0 ? u.mentions : u.count}</span>
      {/if}
    {/if}
    <span class="hit-enter" aria-hidden="true">↵</span>
  </button>
{/snippet}

<div class="overlay" role="presentation" onclick={() => (S.quickSwitcher = false)}>
  <div class="switcher" role="dialog" aria-label="Command palette" onclick={(e) => e.stopPropagation()}>
    <input
      bind:this={inputEl}
      bind:value={query}
      placeholder="Jump anywhere, or type “&gt;” for commands…"
      onkeydown={onKeydown}
    />
    <div class="results" bind:this={listEl}>
      {#if grouped.jump.length}
        <div class="sec muted">Jump to</div>
        {#each grouped.jump as item, i (item.key)}
          {@render row(item, i)}
        {/each}
      {/if}
      {#if grouped.acts.length}
        <div class="sec muted">Actions</div>
        {#each grouped.acts as item, i (item.key)}
          {@render row(item, grouped.jump.length + i)}
        {/each}
      {/if}
      {#if !flat.length}
        <div class="muted none">Nothing matches “{query}”</div>
      {/if}
    </div>
    <div class="hint muted">↑↓ navigate · ↵ select · esc close</div>
  </div>
</div>

<style>
  /* Just a dim — the frosted look comes from the palette's OWN backdrop-filter
     below (a small, bounded region). A full-screen blur here would be the exact
     GPU hazard we avoid, for no visible gain over the glass panel. */
  .overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    justify-content: center;
    padding-top: 12vh;
    z-index: 100;
    animation: qs-fade 0.12s ease;
  }
  .switcher {
    width: 460px;
    max-width: 92vw;
    height: fit-content;
    /* Glassy command palette: translucent surface over the blurred app. */
    background: color-mix(in srgb, var(--bg-1) 88%, transparent);
    backdrop-filter: blur(14px);
    -webkit-backdrop-filter: blur(14px);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    box-shadow: var(--shadow-pop);
    transform-origin: top center;
    animation: qs-pop 0.16s cubic-bezier(0.2, 0.9, 0.3, 1);
  }
  @keyframes qs-fade {
    from {
      opacity: 0;
    }
  }
  @keyframes qs-pop {
    from {
      opacity: 0;
      transform: translateY(-6px) scale(0.97);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .overlay,
    .switcher {
      animation: none;
    }
  }
  .results {
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: min(52vh, 430px);
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  .sec {
    font-size: var(--fs-tiny);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    padding: 4px 10px 2px;
  }
  .sec + .sec,
  .hit + .sec {
    margin-top: 6px;
  }
  .hit {
    position: relative;
    display: flex;
    align-items: center;
    gap: 9px;
    flex-shrink: 0;
    background: transparent;
    color: var(--text);
    text-align: left;
    padding: 8px 10px;
    border-radius: var(--radius-sm);
    font-size: var(--fs-ui);
    transition:
      background 0.1s ease,
      transform 0.12s ease;
  }
  .hit.sel {
    background: var(--bg-3);
    transform: translateX(2px);
  }
  @media (pointer: fine) {
    .hit:hover {
      background: var(--bg-3);
      transform: translateX(2px);
    }
  }
  .hit:active {
    background: var(--bg-3);
  }
  /* The selected row carries an accent edge + tinted icon, so keyboard focus
     reads instantly even while the mouse hovers elsewhere. */
  .hit.sel {
    background: color-mix(in srgb, var(--accent) 12%, var(--bg-3));
    box-shadow: inset 2px 0 0 var(--accent);
  }
  .hit.sel .hit-icon {
    color: var(--accent-hover);
  }
  .hit-icon {
    color: var(--text-muted);
    display: grid;
    place-items: center;
    transition: color 0.1s ease;
  }
  /* ↵ affordance surfaces only on the selected row. */
  .hit-enter {
    color: var(--accent-hover);
    font-size: 12px;
    opacity: 0;
    transform: translateX(-3px);
    transition:
      opacity 0.12s ease,
      transform 0.12s ease;
  }
  .hit.sel .hit-enter {
    opacity: 0.9;
    transform: none;
  }
  .hit-kbd {
    font-family: inherit;
    font-size: var(--fs-tiny);
    color: var(--text-muted);
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-bottom-width: 2px;
    border-radius: 5px;
    padding: 1px 6px;
    white-space: nowrap;
  }
  /* Same pill the channel list wears, so "this one needs you" reads the same
     everywhere. Mentions swap the count for the mention tally in accent. */
  .hit-unread {
    min-width: 18px;
    padding: 0 5px;
    height: 16px;
    border-radius: 8px;
    background: var(--text-muted);
    color: var(--bg-1);
    font-size: var(--fs-tiny);
    font-weight: 700;
    display: grid;
    place-items: center;
    font-variant-numeric: tabular-nums;
  }
  .hit-unread.mention {
    background: var(--accent);
    color: var(--accent-fg, #fff);
  }
  .hit-label {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hl {
    color: var(--accent-hover);
    font-weight: 600;
  }
  .hit-sub {
    font-size: var(--fs-compact);
  }
  .none {
    padding: 10px;
    font-size: var(--fs-ui);
  }
  .hint {
    font-size: var(--fs-small);
    text-align: center;
    padding-top: 2px;
    border-top: 1px solid var(--border);
  }
  /* Mobile: the palette drops in full-width from the top edge; keyboard
     shortcut hints are meaningless on touch. */
  @media (pointer: coarse), (max-width: 768px) {
    .overlay {
      padding-top: 0;
      align-items: flex-start;
    }
    .switcher {
      width: 100%;
      max-width: none;
      border: none;
      border-radius: 0 0 16px 16px;
      padding-top: calc(12px + var(--safe-top));
    }
    .switcher input {
      font-size: 16px; /* stops iOS auto-zoom on focus */
    }
    .hit {
      min-height: var(--tap-min);
      font-size: var(--fs-body);
    }
    /* Both name keys nobody can press, and both steal width from .hit-label,
       which ellipsizes — so a shortcut chip was truncating channel names to
       make room for itself. */
    .hit-kbd,
    .hit-enter {
      display: none;
    }
    .results {
      /* dvh: the keyboard is up the whole time this palette is open, and vh
         does not shrink for it in an Android WebView. */
      max-height: 60dvh;
    }
    .hint {
      display: none;
    }
  }
</style>
