<script>
  import {
    emojiCategories, emojiChar, emojiTonable, searchEmoji, recentEmoji,
    pushRecentEmoji, SKIN_TONES, applyTone, emojiTone, setEmojiTone, emojiName,
  } from "./lib/emoji.js";
  import { emojiTable } from "./lib/emojifull.svelte.js";
  import { S, activeGuild, keySurface } from "./lib/state.svelte.js";
  import { focusOnMount } from "./lib/focus.js";
  import { pushLayer } from "./lib/navstack.svelte.js";

  // Searchable, tabbed emoji grid. onPick(emoji) fires on selection. Closes on
  // Escape or an outside click (a short guard ignores the opening click).
  //
  // onHeight reports the panel's height while it is a mobile bottom panel, so
  // the composer can lift itself clear of it — otherwise the picker covers the
  // very draft it types into, which is why picking used to close it every time.
  let { onPick, onClose, onHeight } = $props();
  let query = $state("");
  let panelH = $state(0);
  $effect(() => {
    onHeight?.(S.isMobile ? panelH : 0);
    return () => onHeight?.(0);
  });
  // Hardware back closes the picker. Without this it skipped straight past to
  // the drawers or App.exitApp() — and since the picker opens with the keyboard
  // up, the reflexive "back to close" dismissed the IME and then quit the app.
  $effect(() => pushLayer("picker", onClose));
  let recents = $state(recentEmoji());
  const openedAt = Date.now();

  // Skin tone: persisted Fitzpatrick modifier applied to the TONABLE set.
  let tone = $state(emojiTone());
  let toneOpen = $state(false);
  function chooseTone(t) {
    tone = t;
    setEmojiTone(t);
    toneOpen = false;
  }
  // display renders a shortcode's char with the active tone when supported.
  // The tone reaches the WHOLE grid now, not the 33 names the curated set
  // hand-picked: Unicode says which sequences take a modifier and the generated
  // table carries that answer for all 249 of them.
  const display = (name) => (emojiTonable(name) ? applyTone(emojiChar(name), tone) : emojiChar(name));

  // Reading emojiTable() is what asks for the generated chunk AND what makes
  // this component re-render when it lands: until then the five curated
  // categories are what the tabs show, which is the right thing to show while
  // the real answer is a network hop away.
  const categories = $derived.by(() => {
    emojiTable(); // the read is the subscription, and the request
    return emojiCategories();
  });
  const customList = $derived(activeGuild()?.emoji || []);
  // Tabs: recent (if any) + the standard categories + guild (if any custom).
  const tabs = $derived([
    ...(recents.length ? [{ key: "recent", label: "Recently used", icon: "🕘" }] : []),
    ...categories,
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
  const catNames = $derived(categories.find((c) => c.key === activeCat)?.names || []);

  // Hovered-emoji preview: { char, name } or { img, name } for guild emoji.
  let preview = $state(null);
  const hint = $derived(
    q ? `${searchHits.length + searchCustom.length} results`
      : tabs.find((t) => t.key === activeCat)?.label || "",
  );

  function pick(e) {
    if (!/^:[a-z0-9_]+:$/i.test(e)) {
      pushRecentEmoji(e); // store unicode chars only (custom emoji are guild-scoped)
      recents = recentEmoji();
    }
    onPick(e);
  }

  function onOutside(e) {
    if (toneOpen && !e.target.closest(".tones")) toneOpen = false;
    if (Date.now() - openedAt > 250 && !e.target.closest(".picker")) onClose();
  }

  // ---- Escape, one layer at a time -------------------------------------
  //
  // Escape used to close the whole picker from anywhere inside it, which threw
  // away a half-typed search — and did it twice over, since navstack also holds
  // a layer for this component. The rungs here are the things that are open
  // INSIDE the picker; once none of them are, the press falls through to
  // navstack and the picker closes, exactly as before.
  //
  // Capture phase for the same reason RichEditor uses it: lib/shortcuts.js
  // listens on window in the bubble phase and would pop the layer first.
  $effect(() => {
    const onEscapeCapture = (e) => {
      if (e.key !== "Escape") return;
      if (toneOpen) {
        toneOpen = false;
      } else if (query) {
        query = "";
        inputEl?.focus();
      } else {
        return; // nothing left inside — navstack closes the picker
      }
      e.preventDefault();
      e.stopPropagation();
    };
    window.addEventListener("keydown", onEscapeCapture, true);
    return () => window.removeEventListener("keydown", onEscapeCapture, true);
  });

  // ---- keyboard over the grid -------------------------------------------
  //
  // The picker opens with the caret in the search box and, until now, that was
  // the end of what a keyboard could do: reaching a result meant Tab, sixty-four
  // times. Enter takes the first hit — the one thing you almost always want
  // after typing a name — and ↓ steps into the grid, where the arrows walk it in
  // two dimensions like the grid it looks like.
  let gridEl = $state(null);
  let inputEl = $state(null);

  const cells = () => (gridEl ? [...gridEl.querySelectorAll(".cell")] : []);
  // Read the column count off the live grid rather than hard-coding 8: the
  // phone layout re-grids to auto-fill and ↓ has to mean "one row down" there
  // too.
  function cols() {
    if (!gridEl) return 8;
    const t = getComputedStyle(gridEl).gridTemplateColumns;
    return Math.max(1, t.split(" ").filter(Boolean).length);
  }
  function focusCell(i) {
    const list = cells();
    if (!list.length) return;
    const el = list[Math.max(0, Math.min(list.length - 1, i))];
    el.focus();
    el.scrollIntoView({ block: "nearest" });
  }
  function onInputKey(e) {
    if (e.key === "Enter") {
      e.preventDefault();
      cells()[0]?.click();
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      focusCell(0);
    }
  }
  function onGridKey(e) {
    const list = cells();
    const i = list.indexOf(document.activeElement);
    if (i < 0) return;
    const c = cols();
    let n = null;
    if (e.key === "ArrowRight") n = i + 1;
    else if (e.key === "ArrowLeft") n = i - 1;
    else if (e.key === "ArrowDown") n = i + c;
    else if (e.key === "ArrowUp") n = i - c;
    else if (e.key === "Home") n = 0;
    else if (e.key === "End") n = list.length - 1;
    if (n === null) return;
    e.preventDefault();
    // Off the top of the grid is the way back to the search box, so a typo is
    // one ↑ away from being fixed rather than a hunt for the field.
    if (n < 0) inputEl?.focus();
    else focusCell(n);
  }
</script>

<svelte:window onpointerdown={onOutside} />

<!-- keySurface: the picker owns the printable keys while it is open, so a
     keystroke that lands before the field has focus (or after a click on the
     grid moved it to a button) cannot be pulled into the message draft. -->
<div class="picker" role="dialog" bind:clientHeight={panelH} use:keySurface>
  {#if S.isMobile}
    <!-- Scrim lives INSIDE .picker (z-index:-1 within its stacking context):
         taps on it close the picker without also landing on the chat below,
         it matches the .picker exclusion in the shell's swipe handler, and
         the window-pointerdown outside-close skips it (closest(".picker")). -->
    <button class="ep-scrim" aria-label="Close emoji picker" onclick={onClose}></button>
  {/if}
  <div class="row">
    <!-- Not on touch: focusing would pop the keyboard over the grid the moment
         the picker opens. Everywhere else the caret belongs here — the search
         field is what the picker is FOR, and while `autofocus` sat here doing
         nothing (it is a parse-time attribute on an element inserted long
         after parse) typing "cat" to filter typed "cat" into the message
         underneath, with the picker still open on top of it. The GIF picker
         beside it has always focused its own field, so the two behaved
         oppositely on the same gesture. -->
    <input
      placeholder="Search emoji…"
      aria-label="Search emoji"
      bind:this={inputEl}
      bind:value={query}
      onkeydown={onInputKey}
      use:focusOnMount={{ skip: S.isMobile }}
    />
    <div class="tones">
      <button
        class="mini tone-btn"
        title="Skin tone"
        aria-label="Choose skin tone"
        onclick={() => (toneOpen = !toneOpen)}>{applyTone("✋", tone)}</button>
      {#if toneOpen}
        <div class="tone-pop">
          {#each SKIN_TONES as t (t.key)}
            <!-- The glyph is the same raised hand five times over, so it is the
                 label and not the content that says which tone this is. -->
            <button
              class="cell tone-cell"
              class:sel={tone === t.key}
              title={t.label}
              aria-label={t.label}
              onclick={() => chooseTone(t.key)}>{applyTone("✋", t.key)}</button>
          {/each}
        </div>
      {/if}
    </div>
    <button class="mini" aria-label="Close emoji picker" onclick={onClose}>✕</button>
  </div>

  {#if !q}
    <div class="tabs">
      {#each tabs as t (t.key)}
        <button
          class="tab"
          class:sel={activeCat === t.key}
          title={t.label}
          aria-label={t.label}
          onclick={() => (activeCat = t.key)}>{t.icon}</button>
      {/each}
    </div>
  {/if}

  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div
    class="grid"
    role="group"
    aria-label="Emoji"
    bind:this={gridEl}
    onkeydown={onGridKey}
    onmouseleave={() => (preview = null)}
  >
    {#if q}
      {#if searchCustom.length}
        <div class="section-label">Guild</div>
        {#each searchCustom as e (e.name)}
          <button
            class="cell"
            aria-label=":{e.name}:"
            onmouseenter={() => (preview = { img: e.image, name: e.name })}
            onfocus={() => (preview = { img: e.image, name: e.name })}
            onclick={() => pick(`:${e.name}:`)}>
            <img class="cimg" src={e.image} alt=":{e.name}:" />
          </button>
        {/each}
      {/if}
      {#if searchHits.length}<div class="section-label">Emoji</div>{/if}
      {#each searchHits as [name] (name)}
        <button
          class="cell"
          aria-label={name}
          onmouseenter={() => (preview = { char: display(name), name })}
          onfocus={() => (preview = { char: display(name), name })}
          onclick={() => pick(display(name))}>{display(name)}</button>
      {/each}
      {#if !searchHits.length && !searchCustom.length}
        <div class="none">
          <span class="none-face">🫥</span>
          <span>No emoji match “{query.trim()}”</span>
        </div>
      {/if}
    {:else if activeCat === "recent"}
      {#each recents as e, i (e + i)}
        <button
          class="cell"
          aria-label={emojiName(e) || e}
          onmouseenter={() => (preview = { char: e, name: emojiName(e) })}
          onfocus={() => (preview = { char: e, name: emojiName(e) })}
          onclick={() => pick(e)}>{e}</button>
      {/each}
    {:else if activeCat === "guild"}
      {#each customList as e (e.name)}
        <button
          class="cell"
          aria-label=":{e.name}:"
          onmouseenter={() => (preview = { img: e.image, name: e.name })}
          onfocus={() => (preview = { img: e.image, name: e.name })}
          onclick={() => pick(`:${e.name}:`)}>
          <img class="cimg" src={e.image} alt=":{e.name}:" />
        </button>
      {/each}
    {:else}
      {#each catNames as name (name)}
        <button
          class="cell"
          aria-label={name}
          onmouseenter={() => (preview = { char: display(name), name })}
          onfocus={() => (preview = { char: display(name), name })}
          onclick={() => pick(display(name))}>{display(name)}</button>
      {/each}
    {/if}
  </div>

  <div class="preview">
    {#if preview}
      <!-- keyed so the glyph re-mounts (and pops) as the hover moves -->
      {#key preview.name || preview.char}
        {#if preview.img}
          <img class="pimg" src={preview.img} alt=":{preview.name}:" />
        {:else}
          <span class="pchar">{preview.char}</span>
        {/if}
      {/key}
      {#if preview.name}<span class="pname">:{preview.name}:</span>{/if}
    {:else}
      <span class="phint">{hint}</span>
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
    border-radius: var(--radius-md);
    padding: 10px;
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    box-shadow: var(--shadow-pop);
    z-index: 50;
    transform-origin: bottom right;
    animation: ep-pop var(--dur-standard) var(--ease-out);
  }
  @keyframes ep-pop {
    from {
      opacity: 0;
      transform: translateY(6px) scale(0.97);
    }
  }
  .row {
    display: flex;
    gap: 6px;
    align-items: center;
  }
  .row input {
    flex: 1;
    min-width: 0;
  }
  .tones {
    position: relative;
    display: flex;
  }
  /* Quiet header buttons (skin tone, ✕): without this they fall through to
     the global accent-filled button style — two loud blue boxes by the search. */
  .mini {
    display: grid;
    place-items: center;
    min-width: 36px;
    min-height: 36px;
    padding: 0;
    background: transparent;
    color: var(--text-muted);
    border-radius: var(--radius-sm);
  }
  .mini:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .tone-btn {
    font-size: 16px;
    line-height: 1;
  }
  .tone-pop {
    position: absolute;
    top: calc(100% + 6px);
    right: 0;
    display: flex;
    gap: 2px;
    padding: var(--sp-1);
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-pop);
    z-index: 5;
    transform-origin: top right;
    animation: ep-pop var(--dur-quick) var(--ease-out);
  }
  .tone-cell.sel {
    background: var(--bg-input);
    box-shadow: inset 0 0 0 1.5px var(--accent);
  }
  /* Five categories became nine, and with recents and a guild pack that is
     eleven tabs in a 290px panel. They share the width evenly and clip nothing:
     a strip that scrolls sideways inside a popover is a scrollbar nobody finds,
     and a strip that overflows silently loses Flags off the right edge. */
  .tabs {
    display: flex;
    gap: 1px;
    border-bottom: 1px solid var(--border);
  }
  .tab {
    position: relative;
    flex: 1 1 0;
    min-width: 0;
    background: transparent;
    font-size: var(--fs-body);
    padding: 5px 2px 7px;
    border-radius: var(--radius-sm) var(--radius-sm) 0 0;
    line-height: 1;
    opacity: 0.55;
    filter: grayscale(0.6);
    transition: opacity var(--dur-quick) ease, filter var(--dur-quick) ease, background var(--dur-quick) ease;
  }
  .tab::after {
    content: "";
    position: absolute;
    left: 4px;
    right: 4px;
    bottom: -1px;
    height: 2px;
    border-radius: 2px;
    background: var(--accent);
    opacity: 0;
    transform: scaleX(0.4);
    transition: opacity var(--dur-standard) ease, transform var(--dur-standard) ease;
  }
  .tab:hover {
    background: var(--bg-input);
    opacity: 1;
    filter: none;
  }
  .tab.sel {
    opacity: 1;
    filter: none;
  }
  .tab.sel::after {
    opacity: 1;
    transform: scaleX(1);
  }
  /* The viewport is expressed in ROWS, not pixels. At a flat 220px the last row
     landed 40% into itself and the picker's bottom edge ran through the middle
     of eight glyphs — which reads as a rendering fault rather than as "there is
     more below". Snapping keeps that true once it is scrolling, and
     scroll-padding keeps a row the keyboard jumps to off the edges. */
  .grid {
    --egrid-cell: 28px; /* 20px glyph + 2 × 4px padding */
    --egrid-gap: 2px;
    --egrid-rows: 8;
    display: grid;
    grid-template-columns: repeat(8, 1fr);
    gap: var(--egrid-gap);
    height: calc(
      var(--egrid-rows) * var(--egrid-cell) + (var(--egrid-rows) - 1) * var(--egrid-gap)
    );
    overflow-y: auto;
    align-content: start;
    scroll-behavior: smooth;
    scroll-snap-type: y proximity;
    scroll-padding-block: var(--egrid-gap);
    overscroll-behavior: contain;
  }
  .grid > * {
    scroll-snap-align: start;
  }
  .cell {
    background: transparent;
    font-size: 20px;
    padding: var(--sp-1);
    border-radius: var(--radius-sm);
    line-height: 1;
    transition: background 0.1s ease, transform 0.08s ease;
  }
  .cell:hover {
    background: var(--bg-input);
    transform: scale(1.14);
  }
  .cell:active {
    transform: scale(0.92); /* satisfying squash on pick */
  }
  .cimg {
    width: 24px;
    height: 24px;
    object-fit: contain;
  }
  .section-label {
    grid-column: 1 / -1;
    font-size: var(--fs-micro);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-muted);
    padding: 4px 2px 2px;
  }
  .none {
    grid-column: 1 / -1;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    padding: 34px 12px;
    font-size: var(--fs-ui);
    color: var(--text-muted);
  }
  .none-face {
    font-size: 30px;
    filter: grayscale(0.4);
  }
  .preview {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    min-height: 34px;
    padding: 2px 4px 0;
    border-top: 1px solid var(--border);
    overflow: hidden;
  }
  .pchar {
    font-size: 26px;
    line-height: 1;
    animation: pv-pop var(--dur-quick) var(--ease-spring);
  }
  @keyframes pv-pop {
    from {
      transform: scale(0.6);
      opacity: 0;
    }
  }
  .pimg {
    width: 26px;
    height: 26px;
    object-fit: contain;
  }
  .pname {
    font-size: var(--fs-ui);
    font-family: var(--font-mono, monospace);
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .phint {
    font-size: var(--fs-compact);
    color: var(--text-muted);
  }
  /* Hidden on desktop (anchored popover closes via outside-click). */
  .ep-scrim {
    display: none;
  }
  /* Mobile: a full-width bottom panel with finger-sized cells and tabs,
     instead of the small anchored popover. */
  @media (pointer: coarse), (max-width: 768px) {
    .ep-scrim {
      display: block;
      position: fixed;
      inset: 0;
      z-index: -1; /* behind the panel, above everything under it */
      background: var(--scrim);
      border: none;
      border-radius: 0;
      animation: ep-fade var(--dur-standard) ease;
    }
    @keyframes ep-fade {
      from {
        opacity: 0;
      }
    }
    .picker {
      position: fixed;
      left: 0;
      right: 0;
      /* Clears the software keyboard when the platform draws it over the page
         instead of resizing the layout viewport. Composer.svelte maintains it. */
      bottom: var(--kb-inset, 0px);
      width: auto;
      border-left: none;
      border-right: none;
      border-bottom: none;
      border-radius: var(--radius-sheet) var(--radius-sheet) 0 0;
      padding-bottom: calc(10px + var(--safe-bottom));
      box-shadow: var(--shadow-pop);
      z-index: 90;
      /* Bottom-panel presentation slides up like the app's sheets. */
      transform-origin: bottom center;
      animation: ep-rise 0.22s var(--ease-out);
    }
    @keyframes ep-rise {
      from {
        transform: translateY(48px);
        opacity: 0.4;
      }
    }
    .row input {
      font-size: 16px; /* stops iOS auto-zoom on focus */
    }
    .row :global(.mini) {
      min-width: 44px;
      min-height: 40px;
    }
    .tabs {
      justify-content: space-between;
    }
    .tab {
      font-size: 20px;
      padding: 11px 8px 13px; /* 20px glyph + padding = the 44px floor */
      min-width: 44px;
    }
    /* The header's tone picker and close button keep their 36px glyph boxes;
       an invisible overlay carries the tap area to 44px. */
    .mini {
      position: relative;
    }
    .mini::after {
      content: "";
      position: absolute;
      inset: -4px;
    }
    .grid {
      /* Was fixed at 8 columns, which is 39px a cell at 360px and 43px at 393 —
         every target under the floor on the horizontal axis, in a grid where
         every neighbour is another live target and a mis-pick can go straight
         out as a reaction. Let the count follow the width instead. */
      grid-template-columns: repeat(auto-fill, minmax(44px, 1fr));
      height: calc(46 * var(--vh));
      gap: var(--sp-1);
    }
    .cell {
      font-size: 27px;
      padding: 8px 0;
      min-height: 44px;
    }
    .cimg {
      width: 30px;
      height: 30px;
    }
    /* `preview` is set only by onmouseenter, which a finger never fires — so on
       touch this was ~45px of a bottom sheet permanently reserved for a feature
       that cannot happen, on the surface with the least room to spare. The grid
       above takes the height back. */
    .preview {
      display: none;
    }
  }
</style>
