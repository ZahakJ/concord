<script>
  import {
    EMOJI, CATEGORIES, searchEmoji, recentEmoji, pushRecentEmoji,
    SKIN_TONES, TONABLE, applyTone, emojiTone, setEmojiTone, emojiName,
  } from "./lib/emoji.js";
  import { activeGuild } from "./lib/state.svelte.js";

  // Searchable, tabbed emoji grid. onPick(emoji) fires on selection. Closes on
  // Escape or an outside click (a short guard ignores the opening click).
  let { onPick, onClose } = $props();
  let query = $state("");
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

<div class="picker" role="dialog">
  <div class="row">
    <input placeholder="Search emoji…" bind:value={query} autofocus />
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
      {#if preview.img}
        <img class="pimg" src={preview.img} alt=":{preview.name}:" />
      {:else}
        <span class="pchar">{preview.char}</span>
      {/if}
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
  .row input {
    flex: 1;
    min-width: 0;
  }
  .tones {
    position: relative;
    display: flex;
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
    box-shadow: 0 6px 18px rgba(0, 0, 0, 0.4);
    z-index: 5;
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
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    padding: 34px 12px;
    font-size: 13px;
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
  }
  .pimg {
    width: 26px;
    height: 26px;
    object-fit: contain;
  }
  .pname {
    font-size: 13px;
    font-family: var(--font-mono, monospace);
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .phint {
    font-size: 12px;
    color: var(--text-muted);
  }
</style>
