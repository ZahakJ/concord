<script>
  import {
    EMOJI, CATEGORIES, searchEmoji, recentEmoji, pushRecentEmoji,
    SKIN_TONES, TONABLE, applyTone, emojiTone, setEmojiTone, emojiName,
  } from "./lib/emoji.js";
  import { S, activeGuild, registerOverlay } from "./lib/state.svelte.js";

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
  $effect(() => registerOverlay(onClose));
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
  const display = (name) => (TONABLE.has(name) ? applyTone(EMOJI[name], tone) : EMOJI[name]);

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
</script>

<svelte:window onpointerdown={onOutside} onkeydown={(e) => e.key === "Escape" && onClose()} />

<div class="picker" role="dialog" bind:clientHeight={panelH}>
  {#if S.isMobile}
    <!-- Scrim lives INSIDE .picker (z-index:-1 within its stacking context):
         taps on it close the picker without also landing on the chat below,
         it matches the .picker exclusion in the shell's swipe handler, and
         the window-pointerdown outside-close skips it (closest(".picker")). -->
    <button class="ep-scrim" aria-label="Close emoji picker" onclick={onClose}></button>
  {/if}
  <div class="row">
    <!-- svelte-ignore a11y_autofocus -->
    <!-- No autofocus on touch: it would pop the keyboard over the grid the
         moment the picker opens. -->
    <input placeholder="Search emoji…" bind:value={query} autofocus={!S.isMobile} />
    <div class="tones">
      <button
        class="mini tone-btn"
        title="Skin tone"
        aria-label="Choose skin tone"
        onclick={() => (toneOpen = !toneOpen)}>{applyTone("✋", tone)}</button>
      {#if toneOpen}
        <div class="tone-pop">
          {#each SKIN_TONES as t (t.key)}
            <button
              class="cell tone-cell"
              class:sel={tone === t.key}
              title={t.label}
              onclick={() => chooseTone(t.key)}>{applyTone("✋", t.key)}</button>
          {/each}
        </div>
      {/if}
    </div>
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

  <div class="grid" role="group" aria-label="Emoji" onmouseleave={() => (preview = null)}>
    {#if q}
      {#if searchCustom.length}
        <div class="section-label">Guild</div>
        {#each searchCustom as e (e.name)}
          <button
            class="cell"
            onmouseenter={() => (preview = { img: e.image, name: e.name })}
            onclick={() => pick(`:${e.name}:`)}>
            <img class="cimg" src={e.image} alt=":{e.name}:" />
          </button>
        {/each}
      {/if}
      {#if searchHits.length}<div class="section-label">Emoji</div>{/if}
      {#each searchHits as [name] (name)}
        <button
          class="cell"
          onmouseenter={() => (preview = { char: display(name), name })}
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
          onmouseenter={() => (preview = { char: e, name: emojiName(e) })}
          onclick={() => pick(e)}>{e}</button>
      {/each}
    {:else if activeCat === "guild"}
      {#each customList as e (e.name)}
        <button
          class="cell"
          onmouseenter={() => (preview = { img: e.image, name: e.name })}
          onclick={() => pick(`:${e.name}:`)}>
          <img class="cimg" src={e.image} alt=":{e.name}:" />
        </button>
      {/each}
    {:else}
      {#each catNames as name (name)}
        <button
          class="cell"
          onmouseenter={() => (preview = { char: display(name), name })}
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
    gap: 8px;
    box-shadow: var(--shadow-pop);
    z-index: 50;
    transform-origin: bottom right;
    animation: ep-pop 0.16s cubic-bezier(0.2, 0.9, 0.3, 1);
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
    padding: 4px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    box-shadow: var(--shadow-pop);
    z-index: 5;
    transform-origin: top right;
    animation: ep-pop 0.13s cubic-bezier(0.2, 0.9, 0.3, 1);
  }
  .tone-cell.sel {
    background: var(--bg-input);
    box-shadow: inset 0 0 0 1.5px var(--accent);
  }
  .tabs {
    display: flex;
    gap: 2px;
    border-bottom: 1px solid var(--border);
  }
  .tab {
    position: relative;
    background: transparent;
    font-size: 17px;
    padding: 4px 6px 7px;
    border-radius: 6px 6px 0 0;
    line-height: 1;
    opacity: 0.55;
    filter: grayscale(0.6);
    transition: opacity 0.12s ease, filter 0.12s ease, background 0.12s ease;
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
    transition: opacity 0.15s ease, transform 0.15s ease;
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
  .grid {
    display: grid;
    grid-template-columns: repeat(8, 1fr);
    gap: 2px;
    height: 220px;
    overflow-y: auto;
    align-content: start;
    scroll-behavior: smooth;
    overscroll-behavior: contain;
  }
  .cell {
    background: transparent;
    font-size: 20px;
    padding: 4px;
    border-radius: 6px;
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
    gap: 8px;
    min-height: 34px;
    padding: 2px 4px 0;
    border-top: 1px solid var(--border);
    overflow: hidden;
  }
  .pchar {
    font-size: 26px;
    line-height: 1;
    animation: pv-pop 0.14s cubic-bezier(0.34, 1.56, 0.64, 1);
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
  @media (pointer: coarse), (max-width: 700px) {
    .ep-scrim {
      display: block;
      position: fixed;
      inset: 0;
      z-index: -1; /* behind the panel, above everything under it */
      background: rgba(0, 0, 0, 0.5);
      border: none;
      border-radius: 0;
      animation: ep-fade 0.16s ease;
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
      border-radius: 18px 18px 0 0;
      padding-bottom: calc(10px + env(safe-area-inset-bottom));
      box-shadow: var(--shadow-pop);
      z-index: 90;
      /* Bottom-panel presentation slides up like the app's sheets. */
      transform-origin: bottom center;
      animation: ep-rise 0.22s cubic-bezier(0.2, 0.9, 0.3, 1);
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
      height: 46vh;
      gap: 4px;
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
