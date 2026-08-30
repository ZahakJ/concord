<script>
  // The shared context menu. Driven by S.contextMenu = {x,y,items}. Items are
  // {label, sub?, icon?, danger?, active?, onClick} or null (skipped). `sub` is
  // a second, quieter line — for a CONSTRAINT rather than a name. Without it a
  // label had to carry both ("Instant meeting (1 hour – 30 days)") and wrapped
  // to two lines in a 300px menu whose other rows were one each; the
  // parenthetical is not part of what the row is called. A `sep:true`
  // item renders a divider, a `header:true` one an inert label over the group
  // below it — which is how a set of mutually exclusive choices (a channel's
  // notification level, a channel's type) says what it's choosing, given this
  // menu deliberately has no submenus. `active` ticks the one in force.
  //
  // A `range:true` item is a slider — {label, value, onInput, fmt?}. It exists
  // for per-participant call volume, which is a setting with a POSITION rather
  // than a choice from a list: "quieter" is not a menu entry, and the mesh has
  // applied a real per-peer gain since it was written with nothing but a
  // mute/unmute pair able to reach it. The row is not a menuitem — it doesn't
  // close the menu, and the arrow keys belong to the slider while it has focus.
  //
  // Two presentations, one call-site contract: desktop gets the classic
  // anchored popover at the cursor; mobile gets a bottom action sheet (there
  // is no meaningful cursor position under a finger, and touch targets need
  // to be finger-sized). Every openContextMenu() caller — messages, channels,
  // guilds, members — upgrades automatically.
  import Icon from "./Icon.svelte";
  import BottomSheet from "./BottomSheet.svelte";
  import Avatar from "./Avatar.svelte";
  import { S, closeContextMenu } from "./lib/state.svelte.js";
  import { syncLayer } from "./lib/navstack.svelte.js";
  import { place, pointOf, rectOf, sizeOf } from "./lib/place.js";
  import { rangefill } from "./lib/rangefill.js";

  // A menu is the shallowest thing on screen and the first thing back should
  // take away — but only because it was opened last, not because it used to be
  // hardcoded in front of everything else.
  syncLayer("menu", () => !!S.contextMenu, closeContextMenu);

  let el = $state(null);
  let sheetEl = $state(null);
  let pos = $state({ x: 0, y: 0 });
  let prevFocus = null;

  // Both presentations claim role="menu", so both have to honour the contract:
  // focus moves in on open and returns whence it came on close. The sheet used
  // to be exempt on the grounds that a finger does not tab — but S.isMobile is
  // width-based, so a desktop window dragged under 768px gets the sheet with a
  // real keyboard behind it, and that was the only path to a message's actions
  // once the hover toolbar stopped rendering there. Reads only the menu's
  // presence — a keepOpen relabel that swaps `items` must not yank focus back
  // to the first row mid-interaction.
  $effect(() => {
    const box = S.isMobile ? sheetEl : el;
    if (!S.contextMenu || !box) return;
    prevFocus = document.activeElement;
    box.querySelector(".cm-item:not(:disabled), .as-item:not(:disabled)")?.focus();
    return () => {
      // Restore — but only if nobody else has claimed the caret in the meantime.
      //
      // A menu item's action frequently OPENS something: run() closes the menu
      // and then calls onClick, so a surface raised that way mounts inside the
      // same flush as this teardown. Restoring unconditionally put focus back
      // on the row the menu came from, on top of the thing the user had just
      // asked for — which is how the thread-title prompt ended up with the
      // caret in the message composer and the title posted to the channel.
      //
      // The test is "is focus still where we left it": nothing has moved on
      // (the menu is gone, so activeElement falls back to <body>), or it is
      // still inside the menu box we are tearing down. Anything else is a
      // deliberate claim and outranks a restore.
      const now = document.activeElement;
      const stale = !now || now === document.body || box.contains(now);
      if (stale && prevFocus?.isConnected) prevFocus.focus({ preventScroll: true });
      prevFocus = null;
    };
  });

  // Roving focus over the item buttons; separators/headers/disabled rows fall
  // out of the query. Enter/Space need no handling — a focused <button>
  // activates natively. preventDefault keeps the arrows from scrolling a
  // menu tall enough to overflow. Reads its container from the event so the
  // popover and the sheet share one implementation.
  function onMenuKey(e) {
    const items = [
      ...e.currentTarget.querySelectorAll(".cm-item:not(:disabled), .as-item:not(:disabled)"),
    ];
    if (!items.length) return;
    const n = items.length;
    const i = items.indexOf(document.activeElement);
    let next = null;
    // Escape belongs to the LAYER, and it has to be answered here rather than
    // left to the window: the volume row stops propagation so the arrow keys
    // move the slider instead of the highlight, and that also stopped the
    // window's Escape handler from ever seeing the key. One touch of the
    // slider and the menu could not be closed with the keyboard at all — two
    // presses did nothing while the invisible full-screen backdrop held the
    // whole stage behind glass, and the only way out was a click.
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      closeContextMenu();
      return;
    }
    if (e.key === "ArrowDown") next = items[i < 0 ? 0 : (i + 1) % n];
    else if (e.key === "ArrowUp") next = items[i < 0 ? n - 1 : (i - 1 + n) % n];
    else if (e.key === "Home") next = items[0];
    else if (e.key === "End") next = items[n - 1];
    if (!next) return;
    e.preventDefault();
    next.focus();
  }

  // Place at the cursor: down-and-right by default, flipping up at the bottom
  // edge and hanging leftward at the right edge, clamped either way. The math
  // is lib/place.js's, so this menu is on-screen at every UI scale — see the
  // coordinate-space note there for what used to happen and why.
  $effect(() => {
    const m = S.contextMenu;
    if (!m || !el) return;
    // A ⋯ button passes its element; a right-click passes a point. Hanging a
    // menu off a zero-size point at the button's right edge put it in the
    // dim beside the dialog, which is how the Members overflow looked
    // unattached.
    const anchor = m.anchorEl ? rectOf(m.anchorEl) : pointOf(m);
    const p = place({
      anchor,
      ...sizeOf(el),
      side: m.side || "bottom",
      align: m.align || (m.anchorEl ? "end" : "start"),
      gap: m.anchorEl ? 6 : 0,
    });
    pos = { x: p.left, y: p.top };
  });

  // `keepOpen` items leave the menu up after the tap — the two-tap arming
  // pattern (delete/revoke) relabels itself in place instead of closing under
  // the finger. The caller is responsible for refreshing S.contextMenu.items.
  function run(item) {
    if (!item.keepOpen) closeContextMenu();
    item.onClick?.();
  }

  // The live read-out for the one slider a menu may carry. Seeded from the item
  // when the menu opens so the number under the thumb is right before the first
  // drag, and reset on close so the next menu doesn't inherit it.
  let rangeVal = $state(0);
  $effect(() => {
    const it = S.contextMenu?.items?.find((i) => i && i.range);
    rangeVal = it ? it.value : 0;
  });
  function slide(item, v) {
    rangeVal = v;
    item.onInput?.(v);
  }
  // The slider owns the keys that move a value — arrows, Home/End, the page
  // keys — and the menu's roving focus must not answer them as well. It does
  // NOT own Escape, which is the layer's, so that one is let through to
  // onMenuKey on the way up.
  function onRangeKey(e) {
    if (e.key === "Escape" || e.key === "Tab") return;
    e.stopPropagation();
  }
  const pct = (v) => `${Math.round(v * 100)}%`;
</script>

{#snippet volumeRow(item)}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div class="cm-range" onkeydown={onRangeKey}>
    <span class="cm-range-lbl">
      {item.label}
      <span class="cm-range-val">{pct(rangeVal)}</span>
    </span>
    <input
      type="range"
      min="0"
      max="1"
      step="0.05"
      value={rangeVal}
      aria-label={item.label}
      oninput={(e) => slide(item, +e.target.value)}
      use:rangefill={rangeVal}
    />
  </div>
{/snippet}

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
          {#if quick.onMore}
            <!-- The door to the full picker lives IN the row, where the thumb
                 already is — not as one more line in the list below. -->
            <button
              class="as-emoji as-more"
              aria-label="More reactions"
              onclick={() => {
                closeContextMenu();
                quick.onMore();
              }}
            ><Icon name="plus" size={20} /></button>
          {/if}
        </div>
      {/if}
      <div class="as-list" role="menu" tabindex="-1" bind:this={sheetEl} onkeydown={onMenuKey}>
        {#each S.contextMenu.items as item (item)}
          {#if item.sep}
            <div class="as-sep"></div>
          {:else if item.header}
            <div class="cm-header">{item.label}</div>
          {:else if item.range}
            {@render volumeRow(item)}
          {:else}
            <button
              class="as-item"
              class:danger={item.danger}
              class:active={item.active}
              role="menuitem"
              onclick={() => run(item)}
            >
              {#if item.avatar}<Avatar {...item.avatar} size={22} />
              {:else if item.swatch}<span class="cm-swatch" style="background:{item.swatch}"></span>
              {:else if item.icon}<span class="as-icon"><Icon name={item.icon} size={18} /></span>{/if}
              <span class="cm-text"
                >{item.label}{#if item.sub}<span class="cm-sub">{item.sub}</span>{/if}</span
              >
              {#if item.active}<span class="cm-tick" aria-hidden="true">✓</span>{/if}
            </button>
          {/if}
        {/each}
      </div>
    </BottomSheet>
  {:else}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="cm-backdrop" onpointerdown={closeContextMenu} oncontextmenu={(e) => e.preventDefault()}></div>
    <div class="cm" bind:this={el} style="left:{pos.x}px; top:{pos.y}px" role="menu" onkeydown={onMenuKey}>
      {#each S.contextMenu.items as item (item)}
        {#if item.sep}
          <div class="cm-sep"></div>
        {:else if item.header}
          <div class="cm-header">{item.label}</div>
        {:else if item.range}
          {@render volumeRow(item)}
        {:else}
          <!-- Hover pulls focus so the pointer and the arrow keys move one
               highlight instead of fighting over two. -->
          <button
            class="cm-item"
            class:danger={item.danger}
            class:active={item.active}
            role="menuitem"
            onclick={() => run(item)}
            onpointerenter={(e) => e.currentTarget.focus()}
          >
            <!-- A row that is about a PERSON shows their face where an icon
                 would go, so the same person is the same circle in a menu as in
                 a member list. -->
            {#if item.avatar}<Avatar {...item.avatar} size={18} />
            {:else if item.swatch}<span class="cm-swatch" style="background:{item.swatch}"></span>
            {:else if item.icon}<Icon name={item.icon} size={14} />{/if}
            <span class="cm-text"
              >{item.label}{#if item.sub}<span class="cm-sub">{item.sub}</span>{/if}</span
            >
            {#if item.active}<span class="cm-tick" aria-hidden="true">✓</span>{/if}
          </button>
        {/if}
      {/each}
    </div>
  {/if}
{/if}

<style>
  /* The row's own column, so a second line stacks under the label rather than
     beside the icon. */
  .cm-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .cm-sub {
    font-size: var(--fs-small);
    color: var(--text-muted);
  }
  /* ---- mobile action-sheet rows ---- */
  .as-quick {
    display: flex;
    justify-content: space-evenly;
    align-items: center;
    gap: var(--sp-1);
    padding: 4px 6px 12px;
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
    color: var(--text);
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
    font-size: var(--fs-body);
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
    min-width: 200px;
    max-width: 280px;
    padding: var(--sp-1);
    background: var(--bg-elevated, var(--bg-1));
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
    display: flex;
    flex-direction: column;
    gap: 1px;
    overscroll-behavior: contain;
    /* Never taller than the viewport — a long menu scrolls instead of pushing
       items off-screen where they can't be reached. */
    max-height: calc(100 * var(--vh) - 16px);
    overflow-y: auto;
    /* Gentle rise-in so the menu arrives instead of blinking into place. Only
       opacity/translate animate — never scale — so the on-open flip measurement
       (which reads width/height) stays exact. */
    animation: cm-in 0.18s var(--ease-out);
  }
  @keyframes cm-in {
    from {
      opacity: 0;
      transform: translateY(-6px);
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
    gap: var(--sp-2);
    width: 100%;
    padding: var(--sp-2) var(--sp-3);
    background: transparent;
    color: var(--text);
    text-align: left;
    font-size: var(--fs-ui);
    border-radius: var(--radius-sm);
  }
  /* A quiet plate, not a filled accent brick — a menu that paints every
     hovered row in the brand colour reads as a selected command, not as
     "this is the one under the pointer". */
  .cm-item:hover,
  .cm-item:focus-visible {
    background: var(--bg-3);
    color: var(--text);
    outline: none;
  }
  .cm-item.danger {
    color: var(--danger-text);
  }
  .cm-item.danger:hover,
  .cm-item.danger:focus-visible {
    background: var(--danger-soft);
    color: var(--danger-text);
  }
  .cm-sep {
    height: 1px;
    background: var(--border);
    margin: 4px 2px;
  }
  /* Group label. Shared by both presentations — the sheet's rows are bigger,
     but a heading that reads as a heading is the same job either way. */
  .cm-header {
    padding: var(--sp-2) var(--sp-3) var(--sp-1);
    font-size: var(--fs-tiny);
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  /* The sheet's rows are twice the size of the popover's, so its group labels
     have to grow with them or they read as debris between the rows. */
  .as-list .cm-header {
    padding: var(--sp-2) var(--sp-3) var(--sp-1);
    font-size: var(--fs-compact);
    letter-spacing: 0.02em;
  }
  /* The slider row. Two lines — a label with its read-out, then the track —
     because a slider squeezed in beside its own name has nowhere to travel. */
  .cm-range {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    padding: var(--sp-2) 8px 6px;
  }
  .cm-range-lbl {
    display: flex;
    align-items: baseline;
    gap: var(--sp-2);
    font-size: var(--fs-small);
    color: var(--text-muted);
  }
  .cm-range-val {
    margin-left: auto;
    font-variant-numeric: tabular-nums;
    color: var(--text);
  }
  .cm-range input[type="range"] {
    width: 100%;
    margin: 0;
  }
  .as-list .cm-range {
    padding: var(--sp-2) var(--sp-3) var(--sp-3);
  }
  .as-list .cm-range-lbl {
    font-size: var(--fs-compact);
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
    font-size: var(--fs-compact);
    font-weight: 800;
    color: var(--accent-hover);
  }
  .cm-item:hover .cm-tick,
  .cm-item:focus-visible .cm-tick {
    color: currentColor;
  }
</style>
