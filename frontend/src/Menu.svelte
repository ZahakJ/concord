<script>
  // A small dropdown menu anchored to a trigger button. Items are passed as a
  // snippet; the menu closes on outside-click, Escape, or item activation.
  import Icon from "./Icon.svelte";

  let { label = "More", icon = "chevron", align = "right", compact = false, children } = $props();
  let open = $state(false);
  let root = $state(null);

  function onWindowClick(e) {
    if (open && root && !root.contains(e.target)) open = false;
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
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div class="menu {align}" role="menu" onclick={() => (open = false)}>
      {@render children()}
    </div>
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
  .trigger.compact:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  /* Touch: the compact glyph stays small, but its tap area doesn't — an
     invisible overlay pads the hit box out to ~44px. (The dropdown anchors
     to .menu-root, so relative positioning here is inert.) */
  @media (pointer: coarse) {
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
    font-size: 13px;
    width: 100%;
  }
  .menu :global(.menu-item:hover) {
    background: var(--bg-3);
  }
  .menu :global(.menu-item.danger) {
    color: var(--danger-text);
  }
  .menu :global(.menu-item.danger:hover) {
    background: var(--danger-soft);
  }
  .menu :global(.menu-sep) {
    height: 1px;
    background: var(--border);
    margin: 4px 2px;
  }
</style>
