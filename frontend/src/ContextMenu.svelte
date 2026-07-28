<script>
  // The shared context menu. Driven by S.contextMenu = {x,y,items}. Items are
  // {label, icon?, danger?, onClick} or null (skipped). A `sep:true` item
  // renders a divider.
  //
  // Two presentations, one call-site contract: desktop gets the classic
  // anchored popover at the cursor; mobile gets a bottom action sheet (there
  // is no meaningful cursor position under a finger, and touch targets need
  // to be finger-sized). Every openContextMenu() caller — messages, channels,
  // guilds, members — upgrades automatically.
  import Icon from "./Icon.svelte";
  import BottomSheet from "./BottomSheet.svelte";
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
  {#if S.isMobile}
    <BottomSheet onClose={closeContextMenu} title={S.contextMenu.title || ""}>
      {#if S.contextMenu.quick}
        {@const quick = S.contextMenu.quick}
        <!-- Quick-reaction row: the most common action gets the top slot. -->
        <div class="as-quick">
          {#each quick.emojis as em (em)}
            <button
              class="as-emoji"
              aria-label={"React " + em}
              onclick={() => {
                closeContextMenu();
                quick.onPick(em);
              }}
            >{em}</button>
          {/each}
        </div>
      {/if}
      <div class="as-list" role="menu">
        {#each S.contextMenu.items as item (item)}
          {#if item.sep}
            <div class="as-sep"></div>
          {:else}
            <button
              class="as-item"
              class:danger={item.danger}
              class:active={item.active}
              role="menuitem"
              onclick={() => run(item)}
            >
              {#if item.swatch}<span class="cm-swatch" style="background:{item.swatch}"></span>
              {:else if item.icon}<span class="as-icon"><Icon name={item.icon} size={18} /></span>{/if}
              <span>{item.label}</span>
              {#if item.active}<span class="cm-tick" aria-hidden="true">✓</span>{/if}
            </button>
          {/if}
        {/each}
      </div>
    </BottomSheet>
  {:else}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="cm-backdrop" onpointerdown={closeContextMenu} oncontextmenu={(e) => e.preventDefault()}></div>
    <div class="cm" bind:this={el} style="left:{pos.x}px; top:{pos.y}px" role="menu">
      {#each S.contextMenu.items as item (item)}
        {#if item.sep}
          <div class="cm-sep"></div>
        {:else}
          <button class="cm-item" class:danger={item.danger} class:active={item.active} role="menuitem" onclick={() => run(item)}>
            {#if item.swatch}<span class="cm-swatch" style="background:{item.swatch}"></span>
            {:else if item.icon}<Icon name={item.icon} size={14} />{/if}
            <span>{item.label}</span>
            {#if item.active}<span class="cm-tick" aria-hidden="true">✓</span>{/if}
          </button>
        {/if}
      {/each}
    </div>
  {/if}
{/if}

<style>
  /* ---- mobile action-sheet rows ---- */
  .as-quick {
    display: flex;
    justify-content: space-between;
    gap: 4px;
    padding: 2px 6px 10px;
    border-bottom: 1px solid var(--border);
    margin-bottom: 6px;
  }
  .as-emoji {
    display: grid;
    place-items: center;
    width: 44px;
    height: 44px;
    padding: 0; /* beat the global button padding — it un-centers the glyph */
    font-size: 24px;
    line-height: 1;
    background: var(--bg-2);
    border-radius: 50%;
  }
  .as-emoji:active {
    background: var(--bg-3);
    transform: scale(1.15);
  }
  .as-list {
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding-bottom: 2px;
  }
  .as-item {
    display: flex;
    align-items: center;
    gap: 14px;
    width: 100%;
    min-height: 48px;
    padding: 10px 12px;
    background: transparent;
    color: var(--text);
    text-align: left;
    font-size: 15px;
    border-radius: var(--radius-md);
  }
  .as-item:active {
    background: var(--bg-3);
  }
  .as-icon {
    display: grid;
    place-items: center;
    width: 24px;
    color: var(--text-muted);
    flex-shrink: 0;
  }
  .as-item.danger,
  .as-item.danger .as-icon {
    color: var(--danger-text);
  }
  .as-sep {
    height: 1px;
    background: var(--border);
    margin: 5px 10px;
  }

  /* ---- desktop popover ---- */
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
    /* Gentle rise-in so the menu arrives instead of blinking into place. Only
       opacity/translate animate — never scale — so the on-open flip measurement
       (which reads width/height) stays exact. */
    animation: cm-in 0.13s cubic-bezier(0.2, 0.9, 0.3, 1);
  }
  @keyframes cm-in {
    from {
      opacity: 0;
      transform: translateY(-5px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .cm {
      animation: none;
    }
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
    color: var(--accent-fg);
  }
  .cm-item.danger {
    color: var(--danger-text);
  }
  .cm-item.danger:hover {
    background: var(--danger);
    color: var(--danger-fg);
  }
  .cm-sep {
    height: 1px;
    background: var(--border);
    margin: 4px 2px;
  }
  /* Colour swatch dot, sized to sit where an icon would. */
  .cm-swatch {
    width: 14px;
    height: 14px;
    border-radius: 50%;
    flex-shrink: 0;
    box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.25);
  }
  .cm-tick {
    margin-left: auto;
    font-size: 12px;
    font-weight: 800;
    color: var(--accent-hover);
  }
  .cm-item:hover .cm-tick {
    color: currentColor;
  }
</style>
