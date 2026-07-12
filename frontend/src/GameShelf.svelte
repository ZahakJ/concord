<script>
  // The game-collection shelf shown on profile cards, plus (when editable)
  // Discord-style add/remove: typing a title suggests real games with real
  // box art (backend-proxied Steam search); picking one stores its cover URL.
  // Titles without art — or with remote images disabled — render a generated
  // gradient cover, so the shelf always looks intentional.
  import Icon from "./Icon.svelte";
  import { S, flash } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";

  let { games = [], editable = false, onchange } = $props();

  // Own covers always render (you picked them); others' remote covers follow
  // the link-previews privacy pref, same as now-playing album art.
  const allowRemote = $derived(editable || !!S.prefs.linkPreviews);

  let adding = $state(false);
  let q = $state("");
  let results = $state([]);
  let busy = $state(false);
  let broken = $state({}); // name -> cover 404'd; fall back to generated art
  let searchSeq = 0;
  let debounce;

  function reset() {
    adding = false;
    q = "";
    results = [];
    clearTimeout(debounce);
  }

  function onInput() {
    clearTimeout(debounce);
    const term = q;
    debounce = setTimeout(async () => {
      const seq = ++searchSeq;
      if (term.trim().length < 2) {
        results = [];
        return;
      }
      try {
        const r = (await api.searchGames(term.trim())) || [];
        if (seq === searchSeq) results = r;
      } catch {
        if (seq === searchSeq) results = [];
      }
    }, 250);
  }

  async function commit(next) {
    if (busy) return;
    busy = true;
    try {
      await onchange(next);
    } catch (err) {
      flash(err);
    } finally {
      busy = false;
    }
  }

  const have = (name) => games.some((g) => g.name.toLowerCase() === name.toLowerCase());

  function pick(r) {
    const entry = { name: r.name, cover: r.cover || "" };
    reset();
    if (!have(entry.name)) commit([...games, entry]);
  }

  // Enter takes the top suggestion; with none, the typed title is added as-is.
  function submit(e) {
    e?.preventDefault();
    if (results.length) return pick(results[0]);
    const name = q.trim();
    reset();
    if (name && !have(name)) commit([...games, { name }]);
  }

  const remove = (name) => commit(games.filter((g) => g.name !== name));

  // Generated box art: title hash -> gradient pair + monogram.
  function coverStyle(name) {
    let h = 0;
    for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0;
    const h1 = h % 360;
    const h2 = (h1 + 40 + (h % 80)) % 360;
    return `background:linear-gradient(160deg, hsl(${h1} 62% 42%), hsl(${h2} 72% 24%))`;
  }
  const initials = (name) =>
    name
      .split(/\s+/)
      .slice(0, 2)
      .map((w) => w[0]?.toUpperCase() || "")
      .join("");
</script>

<div class="sec-head">
  <span class="sec-label muted">
    Game collection{#if games.length}&nbsp;· {games.length}{/if}
  </span>
  {#if editable && !adding}
    <button class="g-add" onclick={() => (adding = true)} title="Add a game">
      <Icon name="plus" size={12} /> Add
    </button>
  {/if}
</div>

{#if adding}
  <form class="g-form" onsubmit={submit}>
    <input
      bind:value={q}
      oninput={onInput}
      placeholder="Search games…"
      maxlength="64"
      disabled={busy}
    />
    <button type="button" class="g-cancel" onclick={reset} aria-label="Cancel">
      <Icon name="close" size={13} />
    </button>
  </form>
  {#if results.length}
    <div class="g-results">
      {#each results as r (r.name)}
        <button class="g-result" onclick={() => pick(r)} disabled={have(r.name)}>
          {#if r.thumb}
            <img class="g-rthumb" src={r.thumb} alt="" loading="lazy" />
          {:else}
            <span class="g-rthumb ph" style={coverStyle(r.name)}></span>
          {/if}
          <span class="g-rname">{r.name}</span>
          {#if have(r.name)}<span class="g-rhave">added</span>{/if}
        </button>
      {/each}
    </div>
  {:else if q.trim().length >= 2}
    <button class="g-result g-free" onclick={submit}>
      <Icon name="plus" size={12} /> Add “{q.trim()}”
    </button>
  {/if}
{/if}

{#if games.length}
  <div class="g-shelf">
    {#each games as g, i (g.name)}
      <div class="g-tile" style="--tile-i:{i}" title={g.name}>
        <div class="g-art" style={g.cover && !broken[g.name] && allowRemote ? "" : coverStyle(g.name)}>
          {#if g.cover && !broken[g.name] && allowRemote}
            <img
              class="g-img"
              src={g.cover}
              alt=""
              loading="lazy"
              onerror={() => (broken = { ...broken, [g.name]: true })}
            />
          {:else}
            <span class="g-glyph">{initials(g.name)}</span>
          {/if}
          <span class="g-sheen"></span>
          {#if editable}
            <button class="g-x" onclick={() => remove(g.name)} title="Remove {g.name}" aria-label="Remove {g.name}">
              <Icon name="trash" size={11} />
            </button>
          {/if}
        </div>
        <span class="g-name">{g.name}</span>
      </div>
    {/each}
  </div>
{:else if editable && !adding}
  <div class="g-empty muted">Show off what you play — add your first game.</div>
{/if}

<style>
  .sec-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  .sec-label {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .g-add {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    font-size: 10.5px;
    padding: 2px 8px;
    border-radius: 999px;
    border: none;
    background: color-mix(in srgb, var(--accent) 16%, transparent);
    color: var(--accent);
    cursor: pointer;
  }
  .g-add:hover {
    background: color-mix(in srgb, var(--accent) 26%, transparent);
  }
  .g-form {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 2px;
  }
  .g-form input {
    flex: 1;
    min-width: 0;
    font-size: 12px;
    padding: 6px 9px;
  }
  .g-cancel {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border-radius: 50%;
    border: none;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
  }
  .g-cancel:hover {
    background: var(--bg-3);
  }
  .g-results {
    display: flex;
    flex-direction: column;
    margin-top: 4px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    overflow: hidden;
    max-height: 190px;
    overflow-y: auto;
    animation: g-in 0.15s ease;
  }
  .g-result {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 5px 8px;
    background: var(--bg-0);
    border: none;
    color: var(--text);
    font-size: 12px;
    text-align: left;
    cursor: pointer;
  }
  .g-result + .g-result {
    border-top: 1px solid color-mix(in srgb, var(--border) 55%, transparent);
  }
  .g-result:hover:not(:disabled) {
    background: var(--bg-3);
  }
  .g-result:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .g-rthumb {
    width: 53px;
    height: 20px;
    border-radius: 3px;
    object-fit: cover;
    flex: none;
  }
  .g-rthumb.ph {
    display: block;
  }
  .g-rname {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .g-rhave {
    font-size: 10px;
    color: var(--text-faint);
  }
  .g-free {
    border: 1px dashed var(--border);
    border-radius: var(--radius-sm);
    margin-top: 4px;
    justify-content: center;
    color: var(--text-muted);
  }
  .g-shelf {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 8px;
    margin-top: 2px;
  }
  .g-tile {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
    animation: g-in 0.25s ease both;
    animation-delay: calc(var(--tile-i, 0) * 0.03s);
  }
  @keyframes g-in {
    from {
      opacity: 0;
      transform: translateY(5px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .g-tile,
    .g-results {
      animation: none;
    }
  }
  .g-art {
    position: relative;
    aspect-ratio: 2 / 3;
    border-radius: 7px;
    display: grid;
    place-items: center;
    overflow: hidden;
    box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.08);
    transition:
      transform 0.16s ease,
      box-shadow 0.16s ease;
  }
  .g-tile:hover .g-art {
    transform: translateY(-2px) scale(1.03);
    box-shadow:
      inset 0 0 0 1px rgba(255, 255, 255, 0.14),
      0 6px 14px rgba(0, 0, 0, 0.35);
  }
  .g-img {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .g-glyph {
    font-size: 15px;
    font-weight: 800;
    letter-spacing: 0.03em;
    color: rgba(255, 255, 255, 0.92);
    text-shadow: 0 1px 3px rgba(0, 0, 0, 0.35);
    user-select: none;
  }
  .g-sheen {
    position: absolute;
    inset: 0;
    background: linear-gradient(115deg, transparent 30%, rgba(255, 255, 255, 0.2) 48%, transparent 62%);
    transform: translateX(-120%);
    transition: transform 0.5s ease;
    pointer-events: none;
  }
  .g-tile:hover .g-sheen {
    transform: translateX(120%);
  }
  .g-name {
    font-size: 10.5px;
    line-height: 1.2;
    color: var(--text-muted);
    text-align: center;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* Remove control: a quiet scrim button INSIDE the art, revealed on hover —
     no floating × bleeding outside the tile. */
  .g-x {
    position: absolute;
    top: 4px;
    right: 4px;
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border-radius: 6px;
    border: none;
    background: rgba(0, 0, 0, 0.55);
    color: rgba(255, 255, 255, 0.85);
    backdrop-filter: blur(2px);
    opacity: 0;
    cursor: pointer;
    transition:
      opacity 0.12s ease,
      background 0.12s ease;
  }
  .g-tile:hover .g-x,
  .g-x:focus-visible {
    opacity: 1;
  }
  .g-x:hover {
    background: rgba(190, 40, 45, 0.85);
    color: #fff;
  }
  .g-empty {
    font-size: 11.5px;
  }
</style>
