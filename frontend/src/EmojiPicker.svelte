<script>
  import { EMOJI, CATEGORIES, searchEmoji, recentEmoji, pushRecentEmoji } from "./lib/emoji.js";
  import { activeGuild } from "./lib/state.svelte.js";

  // Searchable, tabbed emoji grid. onPick(emoji) fires on selection. Closes on
  // Escape or an outside click (a short guard ignores the opening click).
  let { onPick, onClose } = $props();
  let query = $state("");
  let recents = $state(recentEmoji());
  const openedAt = Date.now();

  const customList = $derived(activeGuild()?.emoji || []);
  // Tabs: recent (if any) + the standard categories + guild (if any custom).
  const tabs = $derived([
    ...(recents.length ? [{ key: "recent", label: "Recently Used", icon: "🕘" }] : []),
    ...CATEGORIES,
    ...(customList.length ? [{ key: "guild", label: "Guild", icon: "🖼️" }] : []),
  ]);
  let activeCat = $state("");
  // Default to the first available tab.
  $effect(() => {
    if (!tabs.some((t) => t.key === activeCat)) activeCat = tabs[0]?.key || "people";
  });

  const q = $derived(query.trim().toLowerCase());
  const searchHits = $derived(q ? searchEmoji(q, 64) : []);
  const searchCustom = $derived(q ? customList.filter((e) => e.name.includes(q)) : []);
  const catNames = $derived(CATEGORIES.find((c) => c.key === activeCat)?.names || []);

  function pick(e) {
    if (!/^:[a-z0-9_]+:$/i.test(e)) {
      pushRecentEmoji(e); // store unicode chars only (custom emoji are guild-scoped)
      recents = recentEmoji();
    }
    onPick(e);
  }

  function onOutside(e) {
    if (Date.now() - openedAt > 250 && !e.target.closest(".picker")) onClose();
  }
</script>

<svelte:window onpointerdown={onOutside} onkeydown={(e) => e.key === "Escape" && onClose()} />

<div class="picker" role="dialog">
  <div class="row">
    <input placeholder="Search emoji…" bind:value={query} autofocus />
    <button class="mini" onclick={onClose}>✕</button>
  </div>

  {#if !q}
    <div class="tabs">
      {#each tabs as t (t.key)}
        <button
          class="tab"
          class:sel={activeCat === t.key}
          title={t.label}
          onclick={() => (activeCat = t.key)}>{t.icon}</button>
      {/each}
    </div>
  {/if}

  <div class="grid">
    {#if q}
      {#if searchCustom.length}
        <div class="section-label">Guild</div>
        {#each searchCustom as e (e.name)}
          <button class="cell" title=":{e.name}:" onclick={() => pick(`:${e.name}:`)}>
            <img class="cimg" src={e.image} alt=":{e.name}:" />
          </button>
        {/each}
      {/if}
      {#if searchHits.length}<div class="section-label">Emoji</div>{/if}
      {#each searchHits as [name, e] (name)}
        <button class="cell" title=":{name}:" onclick={() => pick(e)}>{e}</button>
      {/each}
      {#if !searchHits.length && !searchCustom.length}<div class="muted none">No match</div>{/if}
    {:else if activeCat === "recent"}
      {#each recents as e, i (e + i)}
        <button class="cell" onclick={() => pick(e)}>{e}</button>
      {/each}
    {:else if activeCat === "guild"}
      {#each customList as e (e.name)}
        <button class="cell" title=":{e.name}:" onclick={() => pick(`:${e.name}:`)}>
          <img class="cimg" src={e.image} alt=":{e.name}:" />
        </button>
      {/each}
    {:else}
      {#each catNames as name (name)}
        <button class="cell" title=":{name}:" onclick={() => pick(EMOJI[name])}>{EMOJI[name]}</button>
      {/each}
    {/if}
  </div>
</div>

<style>
  .picker {
    position: absolute;
    bottom: 54px;
    right: 12px;
    width: 320px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 10px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.45);
    z-index: 50;
  }
  .row {
    display: flex;
    gap: 6px;
    align-items: center;
  }
  .tabs {
    display: flex;
    gap: 2px;
    border-bottom: 1px solid var(--border);
    padding-bottom: 6px;
  }
  .tab {
    background: transparent;
    font-size: 17px;
    padding: 3px 6px;
    border-radius: 6px;
    line-height: 1;
    opacity: 0.7;
  }
  .tab:hover {
    background: var(--bg-input);
    opacity: 1;
  }
  .tab.sel {
    background: var(--bg-input);
    opacity: 1;
    box-shadow: inset 0 -2px 0 var(--accent);
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(8, 1fr);
    gap: 2px;
    max-height: 220px;
    overflow-y: auto;
  }
  .cell {
    background: transparent;
    font-size: 20px;
    padding: 4px;
    border-radius: 6px;
    line-height: 1;
  }
  .cell:hover {
    background: var(--bg-input);
  }
  .cimg {
    width: 24px;
    height: 24px;
    object-fit: contain;
  }
  .section-label {
    grid-column: 1 / -1;
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-muted);
    padding: 4px 2px 2px;
  }
  .none {
    grid-column: 1 / -1;
    text-align: center;
    padding: 12px;
    font-size: 13px;
  }
</style>
