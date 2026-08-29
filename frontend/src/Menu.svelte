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
  import { syncLayer } from "./lib/navstack.svelte.js";
  import { portal } from "./lib/portal.js";
  import { rectOf, sizeOf, viewport } from "./lib/place.js";
  import { S } from "./lib/state.svelte.js";

  // `up` opens the dropdown ABOVE the trigger — for a trigger that lives at the
  // bottom of the window (the composer's overflow), where a menu hanging below
  // it would be off-screen.
  // `trigger` replaces the icon button with a snippet of the caller's own — for
  // a picker, where the control has to SAY what is currently chosen rather than
  // being a chevron you have to open to find out. `wide` then lets the whole
  // thing fill its row, which is what a device picker in a settings sheet is.
  // Both are additive: every existing call site renders exactly as before.
  let {
    label = "More",
    icon = "chevron",
    align = "right",
    compact = false,
    up = false,
    wide = false,
    trigger,
    children,
  } = $props();
  let open = $state(false);
  // Both presentations are a layer: back closes the sheet on a phone, Escape
  // the dropdown on a desktop, and neither needs a listener of its own. Note
  // this is also why nothing below adds a window Escape handler — that would
  // pop the navstack twice and take whatever is underneath with it.
  syncLayer("menu", () => open, () => (open = false));
  let root = $state(null);
  // The PANEL, separately from the trigger wrapper: on a phone the sheet is
  // portalled out of .menu-root entirely, so `root` cannot reach the rows.
  let panel = $state(null);
  let prevFocus = null;

  function onWindowClick(e) {
    // The sheet portals outside .menu-root, so "outside the trigger" would
    // close it on its own first tap. It owns its dismissal (scrim/swipe).
    // The desktop dropdown is portalled too now, so the panel has to be part of
    // "inside" or the first click on a row would close the menu before the row
    // could act on it.
    if (!open || S.isMobile || !root) return;
    if (root.contains(e.target) || panel?.contains(e.target)) return;
    open = false;
  }

  // ---- where the dropdown goes -------------------------------------------
  //
  // It used to go `position: absolute` inside the trigger's own wrapper, which
  // made every scroll box in the app a guillotine: the channel list clips at
  // `overflow`, so the options menu on the LAST channel of a full list rendered
  // 2px tall and then nothing — 80 of its 82 pixels lived below the scroller's
  // edge. `main.chat` and `.app` clip too, so the same thing waited for any
  // trigger low in a pane. That is the "dropdown menu cut through the UI"
  // report, and it is a containment bug, not a rendering one: it reproduces
  // identically in Chromium and in the desktop build's WebKit.
  //
  // So the panel is portalled to <body> and placed against the trigger's
  // measured rect. Flipping up is now a fallback rather than a call-site
  // decision (the `up` prop is still honoured as the FIRST preference, because
  // the composer's ⊕ knows perfectly well which way it wants to open), and the
  // panel is given the height that is actually available so a long list scrolls
  // inside itself instead of running off the screen.
  const GAP = 6;
  const EDGE = 8;
  let pos = $state(null);

  // All of this is in LAYOUT pixels — the trigger's rect and the viewport come
  // through lib/place.js, which divides out the UI-scale zoom. Mixing the two
  // spaces put every dropdown opened low in a zoomed window off the bottom of
  // the screen; see the coordinate-space note in that file.
  function place() {
    if (!open || S.isMobile || !panel || !root) return;
    const t = rectOf(root);
    const vp = viewport();
    const below = vp.h - (t.y + t.h) - GAP - EDGE;
    const above = t.y - GAP - EDGE;
    // Honour `up` while there is room for it; otherwise take whichever side has
    // more, so a trigger with 40px under it does not open into 40px.
    const wantUp = up ? above > 120 || above >= below : below < Math.min(panel.scrollHeight, 340) && above > below;
    const room = Math.max(120, wantUp ? above : below);
    // Measure at the height it will actually get, or a panel that is currently
    // taller than the room reports a width shrunk by its own scrollbar.
    panel.style.maxHeight = `${Math.min(340, room)}px`;
    const { w, h } = sizeOf(panel);
    let left = align === "left" ? t.x : t.x + t.w - w;
    left = Math.max(EDGE, Math.min(left, vp.w - w - EDGE));
    const top = wantUp ? Math.max(EDGE, t.y - GAP - h) : Math.min(t.y + t.h + GAP, vp.h - h - EDGE);
    pos = { left, top, up: wantUp, width: wide ? t.w : 0 };
  }

  // Re-place on anything that can move the trigger. `scroll` is captured
  // because the movers are inner scrollers (the channel list, a settings
  // dialog), which do not bubble a scroll event to the window.
  $effect(() => {
    if (!open || S.isMobile) return;
    place();
    const on = () => place();
    window.addEventListener("resize", on);
    window.addEventListener("scroll", on, true);
    return () => {
      window.removeEventListener("resize", on);
      window.removeEventListener("scroll", on, true);
      pos = null;
    };
  });

  // role="menu" is a promise about the keyboard, and this component made it
  // twice — on the dropdown and on the sheet — while honouring none of it. Tab
  // walked straight past the open menu into the page behind it, the arrows did
  // nothing, and closing left focus wherever it had wandered. ContextMenu, a
  // hundred lines away, has had all of this the whole time; this is that
  // behaviour, adapted to the one structural difference between them.
  //
  // The difference: ContextMenu builds its rows from a data array, so it can
  // stamp role="menuitem" itself. Menu's rows arrive as a consumer snippet, so
  // the roles are applied to the rendered DOM instead. Every consumer already
  // marks its rows .menu-item and its rules .menu-sep (the stylesheet below
  // depends on it), so the query is not a new contract, it is the existing one.
  function decorate() {
    if (!panel) return;
    for (const el of panel.querySelectorAll(".menu-item")) el.setAttribute("role", "menuitem");
    for (const el of panel.querySelectorAll(".menu-sep")) el.setAttribute("role", "separator");
  }

  $effect(() => {
    if (!open || !panel) return;
    decorate();
    prevFocus = document.activeElement;
    // preventScroll: an `up` menu sits on the bottom edge of the window, and
    // focusing its first row without this scrolls the page out from under it.
    panel.querySelector(".menu-item:not(:disabled)")?.focus({ preventScroll: true });
    return () => {
      if (prevFocus?.isConnected) prevFocus.focus();
      prevFocus = null;
    };
  });

  // Roving focus. Enter/Space need no handling — the rows are real <button>s.
  // DOM order is visual order in both presentations, `up` included: that
  // variant repositions the whole panel, it does not reverse it.
  function onMenuKey(e) {
    const items = [...e.currentTarget.querySelectorAll(".menu-item:not(:disabled)")];
    if (!items.length) return;
    const n = items.length;
    const i = items.indexOf(document.activeElement);
    let next = null;
    if (e.key === "ArrowDown") next = items[i < 0 ? 0 : (i + 1) % n];
    else if (e.key === "ArrowUp") next = items[i < 0 ? n - 1 : (i - 1 + n) % n];
    else if (e.key === "Home") next = items[0];
    else if (e.key === "End") next = items[n - 1];
    if (!next) return;
    e.preventDefault();
    next.focus();
  }
</script>

<svelte:window onclick={onWindowClick} />

<div class="menu-root" class:wide bind:this={root}>
  {#if trigger}
    <button class="trigger own" aria-label={label} aria-haspopup="menu" aria-expanded={open} onclick={() => (open = !open)}>
      {@render trigger()}
    </button>
  {:else}
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
  {/if}
  {#if open}
    {#if S.isMobile}
      <BottomSheet title={label} onClose={() => (open = false)}>
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="menu sheet"
          role="menu"
          tabindex="-1"
          bind:this={panel}
          onclick={() => (open = false)}
          onkeydown={onMenuKey}
        >
          {@render children()}
        </div>
      </BottomSheet>
    {:else}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="menu {align}"
        class:up={pos ? pos.up : up}
        class:placed={!!pos}
        role="menu"
        tabindex="-1"
        bind:this={panel}
        use:portal
        style={pos
          ? `left:${pos.left}px;top:${pos.top}px;${pos.width ? `width:${pos.width}px;min-width:0;` : ""}`
          : ""}
        onclick={() => (open = false)}
        onkeydown={onMenuKey}
      >
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
  .menu-root.wide {
    display: flex;
    width: 100%;
  }
  .menu-root.wide .trigger.own {
    width: 100%;
  }
  /* A wide picker's panel matches its trigger's width; place() measures it and
     writes both width and min-width inline, because the panel is no longer a
     descendant of .menu-root and no descendant selector can reach it. */
  .trigger {
    display: grid;
    place-items: center;
    padding: 6px 9px;
  }
  /* A caller-supplied trigger brings its own everything. */
  .trigger.own {
    padding: 0;
    background: transparent;
    border: none;
    border-radius: 0;
    text-align: left;
  }
  .trigger.compact {
    padding: 2px 5px;
    background: transparent;
    color: var(--text-muted);
    border: none;
    border-radius: var(--radius-sm);
  }
  @media (pointer: fine) {
    .trigger.compact:hover {
      background: var(--bg-3);
      color: var(--text);
    }
  }
  /* Touch: the compact glyph stays small, but its tap area doesn't.
     This used to be an ::after at inset:-14px -11px, which measured 43x30 of
     real hit area rather than the intended 44x44 — the overlay is unpositioned
     in the stacking order, so the row BELOW (a later sibling) painted over its
     lower half. It was also aiming the extra 14px straight into the first
     channel row, i.e. the expansion would have stolen taps from a neighbour
     rather than won them. The rows these triggers sit in are already
     min-height:44px on touch, so the button just fills its own row instead. */
  @media (pointer: coarse), (max-width: 768px) {
    .trigger.compact {
      position: relative;
      min-width: var(--tap-min);
      min-height: var(--tap-min);
    }
  }
  .menu {
    /* Fixed and portalled to <body>: see place() above. `visibility: hidden`
       until the first measurement lands so the panel is never painted at 0,0
       for a frame on its way to the trigger. */
    position: fixed;
    left: 0;
    top: 0;
    visibility: hidden;
    min-width: 180px;
    /* A menu used to hold a handful of verbs. Select.svelte hands it option
       lists — thirty-one days, twelve months — which without a ceiling grow a
       thousand-pixel panel that runs off both ends of the window. */
    max-height: min(340px, 62 * var(--vh));
    overflow-y: auto;
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
    animation: menu-in var(--dur-quick) var(--ease-out);
  }
  .menu.placed {
    visibility: visible;
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
    transform-origin: top right;
  }
  .menu.left {
    transform-origin: top left;
  }
  /* Upward: the trigger sits on the bottom edge of the window, so the panel
     grows toward the content instead of off the screen. */
  .menu.up {
    transform-origin: bottom;
    animation: menu-in-up var(--dur-quick) var(--ease-out);
  }
  .menu.up.right {
    transform-origin: bottom right;
  }
  .menu.up.left {
    transform-origin: bottom left;
  }
  @keyframes menu-in-up {
    from {
      opacity: 0;
      transform: translateY(6px) scale(0.97);
    }
  }
  /* After the .menu.up rule above, not with the other reduced-motion block: a
     media query adds no specificity, so at equal weight the later rule wins. */
  @media (prefers-reduced-motion: reduce) {
    .menu.up {
      animation: none;
    }
  }
  /* Sheet presentation: the dropdown's positioning and chrome come off, the
     rows stay. Same markup, same consumer classes. */
  .menu.sheet {
    position: static;
    visibility: visible;
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
  /* The keyboard's highlight is the mouse's highlight. Without this the arrow
     keys moved a focus ring around rows that gave no other sign of being the
     current one, which is a different-looking menu depending on which hand you
     opened it with. Not inside the (pointer: fine) block above: a desktop
     window under 768px gets the sheet, and a real keyboard with it. */
  .menu :global(.menu-item:focus-visible) {
    background: var(--bg-3);
  }
  .menu :global(.menu-item.danger:focus-visible) {
    background: var(--danger-soft);
  }
  .menu :global(.menu-item:active) {
    background: var(--bg-3);
  }
  .menu :global(.menu-item:disabled) {
    opacity: 0.45;
    pointer-events: none;
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
