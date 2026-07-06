<script>
  import { EMOJI, searchEmoji } from "./lib/emoji.js";

  // Small searchable emoji grid. onPick(emoji) fires on selection.
  let { onPick, onClose } = $props();
  let query = $state("");

  const results = $derived(
    query.trim() ? searchEmoji(query.trim(), 64) : Object.entries(EMOJI).slice(0, 120),
  );
</script>

<div class="picker" role="dialog">
  <div class="row">
    <input placeholder="Search emoji…" bind:value={query} autofocus />
    <button class="mini" onclick={onClose}>✕</button>
  </div>
  <div class="grid">
    {#each results as [name, e] (name)}
      <button class="cell" title=":{name}:" onclick={() => onPick(e)}>{e}</button>
    {:else}
      <div class="muted none">No match</div>
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
  .none {
    grid-column: 1 / -1;
    text-align: center;
    padding: 12px;
    font-size: 13px;
  }
</style>
