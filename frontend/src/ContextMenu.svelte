<script>
  // The shared right-click menu. Driven by S.contextMenu = {x,y,items}. Items
  // are {label, icon?, danger?, onClick} or null (skipped). A `sep:true` item
  // renders a divider.
  import Icon from "./Icon.svelte";
  import { S, closeContextMenu } from "./lib/state.svelte.js";

  let el = $state(null);
  let pos = $state({ x: 0, y: 0 });

  // Place at the cursor, flipping so the menu stays on-screen.
  $effect(() => {
    const m = S.contextMenu;
    if (!m || !el) return;
    const r = el.getBoundingClientRect();
    const x = Math.min(m.x, window.innerWidth - r.width - 8);
    const y = Math.min(m.y, window.innerHeight - r.height - 8);
    pos = { x: Math.max(8, x), y: Math.max(8, y) };
  });

  function run(item) {
    closeContextMenu();
    item.onClick?.();
  }
</script>

<svelte:window
  onkeydown={(e) => e.key === "Escape" && closeContextMenu()}
  onresize={closeContextMenu}
/>

{#if S.contextMenu}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="cm-backdrop" onpointerdown={closeContextMenu} oncontextmenu={(e) => e.preventDefault()}></div>
  <div class="cm" bind:this={el} style="left:{pos.x}px; top:{pos.y}px" role="menu">
    {#each S.contextMenu.items as item (item)}
      {#if item.sep}
        <div class="cm-sep"></div>
      {:else}
        <button class="cm-item" class:danger={item.danger} role="menuitem" onclick={() => run(item)}>
          {#if item.icon}<Icon name={item.icon} size={14} />{/if}
          <span>{item.label}</span>
        </button>
      {/if}
    {/each}
  </div>
{/if}

<style>
  .cm-backdrop {
    position: fixed;
    inset: 0;
    z-index: 400;
  }
  .cm {
    position: fixed;
    z-index: 401;
    min-width: 180px;
    max-width: 260px;
    padding: 5px;
    background: var(--bg-elevated, var(--bg-1));
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-pop);
    display: flex;
    flex-direction: column;
    gap: 1px;
    /* Never taller than the viewport — a long menu scrolls instead of pushing
       items off-screen where they can't be reached. */
    max-height: calc(100vh - 16px);
    overflow-y: auto;
  }
  .cm-item {
    display: flex;
    align-items: center;
    gap: 9px;
    width: 100%;
    padding: 7px 10px;
    background: transparent;
    color: var(--text);
    text-align: left;
    font-size: 13px;
    border-radius: var(--radius-sm);
  }
  .cm-item:hover {
    background: var(--accent);
    color: #fff;
  }
  .cm-item.danger {
    color: var(--danger);
  }
  .cm-item.danger:hover {
    background: var(--danger);
    color: #fff;
  }
  .cm-sep {
    height: 1px;
    background: var(--border);
    margin: 4px 2px;
  }
</style>
