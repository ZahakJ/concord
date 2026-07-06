<script>
  // Ctrl+K quick switcher: fuzzy jump to any channel or server.
  import Icon from "./Icon.svelte";
  import { S, jumpToChannel, selectGuild } from "./lib/state.svelte.js";

  let query = $state("");
  let sel = $state(0);
  let inputEl = $state(null);

  $effect(() => inputEl?.focus());

  // Subsequence fuzzy match; lower score = better.
  function fuzzy(text, q) {
    text = text.toLowerCase();
    let ti = 0;
    let score = 0;
    for (const c of q) {
      const found = text.indexOf(c, ti);
      if (found < 0) return -1;
      score += found - ti; // penalize gaps
      ti = found + 1;
    }
    return score + text.length * 0.01;
  }

  const items = $derived.by(() => {
    const all = [];
    for (const g of S.guilds) {
      if (g.kind === "dm") {
        all.push({ kind: "dm", label: g.name, sub: "direct", g, icon: "edit" });
        continue;
      }
      all.push({ kind: "guild", label: g.name, sub: "server", g, icon: "diamond" });
      for (const c of g.channels) {
        const icon = c.type === "voice" ? "speaker" : c.type === "announcement" ? "megaphone" : "hash";
        all.push({ kind: "channel", label: c.name, sub: g.name, c, g, icon });
      }
    }
    const q = query.trim().toLowerCase();
    if (!q) {
      return all.filter((i) => i.kind === "channel").slice(0, 8);
    }
    return all
      .map((i) => ({ i, s: fuzzy(i.label, q) }))
      .filter((x) => x.s >= 0)
      .sort((a, b) => a.s - b.s)
      .slice(0, 8)
      .map((x) => x.i);
  });

  $effect(() => {
    query;
    sel = 0;
  });

  async function go(item) {
    S.quickSwitcher = false;
    if (item.kind === "channel") {
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
      sel = (sel + 1) % Math.max(items.length, 1);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      sel = (sel - 1 + Math.max(items.length, 1)) % Math.max(items.length, 1);
    } else if (e.key === "Enter" && items[sel]) {
      e.preventDefault();
      go(items[sel]);
    }
  }
</script>

<div class="overlay" role="presentation" onclick={() => (S.quickSwitcher = false)}>
  <div class="switcher" role="dialog" aria-label="Quick switcher" onclick={(e) => e.stopPropagation()}>
    <input
      bind:this={inputEl}
      bind:value={query}
      placeholder="Jump to a channel or server…"
      onkeydown={onKeydown}
    />
    <div class="results">
      {#each items as item, i (item.kind + (item.c?.id || item.g.id))}
        <button class="hit" class:sel={i === sel} onclick={() => go(item)}>
          <span class="hit-icon">
            <Icon name={item.icon} size={13} />
          </span>
          <span class="hit-label">{item.label}</span>
          <span class="muted hit-sub">{item.sub}</span>
        </button>
      {:else}
        <div class="muted none">Nothing matches “{query}”</div>
      {/each}
    </div>
    <div class="hint muted">↑↓ navigate · Enter jump · Esc close</div>
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
  }
  .results {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .hit {
    display: flex;
    align-items: center;
    gap: 9px;
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
