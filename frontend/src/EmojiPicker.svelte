<script>
  import { EMOJI, searchEmoji } from "./lib/emoji.js";
  import { activeGuild } from "./lib/state.svelte.js";

  // Small searchable emoji grid. onPick(emoji) fires on selection. Closes on
  // Escape or an outside click (a short guard ignores the opening click, which
  // also bubbles to the window).
  let { onPick, onClose } = $props();
  let query = $state("");
  const openedAt = Date.now();

  const results = $derived(
    query.trim() ? searchEmoji(query.trim(), 64) : Object.entries(EMOJI).slice(0, 120),
  );
  // The active guild's custom emoji, filtered by the search query.
  const custom = $derived.by(() => {
    const list = activeGuild()?.emoji || [];
    const q = query.trim().toLowerCase();
    return q ? list.filter((e) => e.name.includes(q)) : list;
  });

  function onOutside(e) {
    if (Date.now() - openedAt > 250 && !e.target.closest(".picker")) onClose();
  }
</script>

<svelte:window
  onpointerdown={onOutside}
  onkeydown={(e) => e.key === "Escape" && onClose()}
/>

<div class="picker" role="dialog">
  <div class="row">
    <input placeholder="Search emoji…" bind:value={query} autofocus />
    <button class="mini" onclick={onClose}>✕</button>
  </div>
  <div class="grid">
    {#if custom.length}
      <div class="section-label">Guild</div>
      {#each custom as e (e.name)}
        <button class="cell" title=":{e.name}:" onclick={() => onPick(`:${e.name}:`)}>
          <img class="cimg" src={e.image} alt=":{e.name}:" />
        </button>
      {/each}
      <div class="section-label">Emoji</div>
    {/if}
    {#each results as [name, e] (name)}
      <button class="cell" title=":{name}:" onclick={() => onPick(e)}>{e}</button>
    {:else}
      {#if !custom.length}<div class="muted none">No match</div>{/if}
    {/each}
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
