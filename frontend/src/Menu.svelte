<script>
  // A small dropdown menu anchored to a trigger button. Items are passed as a
  // snippet; the menu closes on outside-click, Escape, or item activation.
  //
  // Two presentations, one call-site contract — the same split ContextMenu
  // makes. On a phone the anchored dropdown was unusable: 180px of absolutely
  // positioned menu hanging off a right-aligned trigger inside a ~300px drawer,
  // with 33px rows stacked 2px apart. It becomes a bottom sheet instead, where
  // there is room for finger-sized rows and nothing can be clipped off-screen.
  import Icon from "./Icon.svelte";
  import BottomSheet from "./BottomSheet.svelte";
  import { S } from "./lib/state.svelte.js";

  let { label = "More", icon = "chevron", align = "right", compact = false, children } = $props();
  let open = $state(false);
  let root = $state(null);

  function onWindowClick(e) {
    // The sheet portals outside .menu-root, so "outside the trigger" would
    // close it on its own first tap. It owns its dismissal (scrim/swipe).
    if (open && !S.isMobile && root && !root.contains(e.target)) open = false;
  }
</script>

<svelte:window onclick={onWindowClick} onkeydown={(e) => e.key === "Escape" && (open = false)} />

<div class="menu-root" bind:this={root}>
  <button
    class="trigger"
    class:ghost={!compact}
    class:compact
    title={label}
    aria-label={label}
    aria-haspopup="menu"
    aria-expanded={open}
    onclick={() => (open = !open)}
  >
    <Icon name={icon} size={compact ? 12 : 16} />
  </button>
  {#if open}
    {#if S.isMobile}
      <BottomSheet title={label} onClose={() => (open = false)}>
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <div class="menu sheet" role="menu" onclick={() => (open = false)}>
          {@render children()}
        </div>
      </BottomSheet>
    {:else}
      <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
      <div class="menu {align}" role="menu" onclick={() => (open = false)}>
        {@render children()}
      </div>
    {/if}
  {/if}
</div>

<style>
  .menu-root {
    position: relative;
    display: inline-flex;
  }
  .trigger {
    display: grid;
    place-items: center;
    padding: 6px 9px;
  }
  .trigger.compact {
    padding: 2px 5px;
    background: transparent;
    color: var(--text-muted);
    border: none;
    border-radius: 5px;
  }
  @media (pointer: fine) {
    .trigger.compact:hover {
      background: var(--bg-3);
      color: var(--text);
    }
  }
  /* Touch: the compact glyph stays small, but its tap area doesn't — an
     invisible overlay pads the hit box out to ~44px. (The dropdown anchors
     to .menu-root, so relative positioning here is inert.) */
  @media (pointer: coarse), (max-width: 768px) {
    .trigger.compact {
      position: relative;
    }
    .trigger.compact::after {
      content: "";
      position: absolute;
      inset: -14px -11px;
    }
  }
  .menu {
    position: absolute;
    top: calc(100% + 6px);
    min-width: 180px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 5px;
    box-shadow: var(--shadow-pop);
    z-index: 80;
    display: flex;
    flex-direction: column;
    gap: 2px;
    transform-origin: top;
    animation: menu-in 0.13s cubic-bezier(0.2, 0.9, 0.3, 1);
  }
  @keyframes menu-in {
    from {
      opacity: 0;
      transform: translateY(-6px) scale(0.97);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .menu {
      animation: none;
    }
  }
  .menu.right {
    right: 0;
    transform-origin: top right;
  }
  .menu.left {
    left: 0;
    transform-origin: top left;
  }
  /* Sheet presentation: the dropdown's positioning and chrome come off, the
     rows stay. Same markup, same consumer classes. */
  .menu.sheet {
    position: static;
    min-width: 0;
    background: transparent;
    border: none;
    padding: 0 0 2px;
    box-shadow: none;
    gap: 1px;
    animation: none;
  }
  /* Menu items are plain buttons with .menu-item from the consumer. */
  .menu :global(.menu-item) {
    display: flex;
    align-items: center;
    gap: 9px;
    background: transparent;
    color: var(--text);
    text-align: left;
    padding: 8px 10px;
    border-radius: var(--radius-sm);
    font-size: var(--fs-ui);
    width: 100%;
  }
  @media (pointer: fine) {
    .menu :global(.menu-item:hover) {
      background: var(--bg-3);
    }
    .menu :global(.menu-item.danger:hover) {
      background: var(--danger-soft);
    }
  }
  .menu :global(.menu-item:active) {
    background: var(--bg-3);
  }
  .menu :global(.menu-item.danger) {
    color: var(--danger-text);
  }
  /* Sheet rows are finger-sized and spaced like the action sheet's, so the two
     menu surfaces on a phone don't feel like different apps. */
  .menu.sheet :global(.menu-item) {
    min-height: 48px;
    gap: 14px;
    padding: 10px 12px;
    font-size: var(--fs-body);
    border-radius: var(--radius-md);
  }
  .menu :global(.menu-sep) {
    height: 1px;
    background: var(--border);
    margin: 4px 2px;
  }
  .menu.sheet :global(.menu-sep) {
    margin: 5px 10px;
  }
</style>
