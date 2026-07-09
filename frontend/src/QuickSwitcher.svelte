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
    channelShort,
    isDMChannel,
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
      { id: "settings", label: "Open settings", icon: "gear", run: () => (S.modal = { kind: "settings" }) },
      { id: "appearance", label: "Open appearance", icon: "spark", run: () => (S.modal = { kind: "appearance" }) },
      { id: "status", label: "Set status", icon: "smile", run: () => (S.statusPopRequest = true) },
      { id: "read", label: "Mark all as read", icon: "check", sub: "Shift+Esc", run: () => markAllRead() },
    ];
    if (S.activeChannelId) {
      const id = S.activeChannelId;
      const muted = !!S.mutes[id];
      const name = isDMChannel(id) ? channelShort(id) : `#${channelShort(id)}`;
      list.push({
        id: "mute",
        label: `${muted ? "Unmute" : "Mute"} ${name}`,
        icon: muted ? "bell" : "bellOff",
        run: () => toggleMute(id),
      });
    }
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
      return {
        jump: dests.filter((i) => i.kind === "channel").slice(0, 6),
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
    {#if item.sub}<span class="muted hit-sub">{item.sub}</span>{/if}
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
  .overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.55);
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
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    box-shadow: var(--shadow-pop);
    transform-origin: top center;
    animation: qs-pop 0.14s ease;
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
  }
  .sec {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    padding: 4px 10px 2px;
  }
  .sec + .sec,
  .hit + .sec {
    margin-top: 6px;
  }
  .hit {
    display: flex;
    align-items: center;
    gap: 9px;
    flex-shrink: 0;
    background: transparent;
    color: var(--text);
    text-align: left;
    padding: 8px 10px;
    border-radius: var(--radius-sm);
    font-size: 14px;
  }
  .hit.sel,
  .hit:hover {
    background: var(--bg-3);
  }
  .hit-icon {
    color: var(--text-muted);
    display: grid;
    place-items: center;
  }
  .hit-label {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hl {
    color: var(--accent);
    font-weight: 600;
  }
  .hit-sub {
    font-size: 12px;
  }
  .none {
    padding: 10px;
    font-size: 13px;
  }
  .hint {
    font-size: 11px;
    text-align: center;
    padding-top: 2px;
    border-top: 1px solid var(--border);
  }
</style>
