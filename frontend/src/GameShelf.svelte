<script module>
  // Generated box art: a muted duotone from the title hash — deliberately
  // restrained so placeholder tiles sit quietly next to real covers instead
  // of screaming random rainbow. Module-scoped and exported so the other
  // now-playing renderings (member-list line, profile PLAYING block) can tint
  // themselves to match the game's cover.
  export function gameHash(name) {
    let h = 0;
    for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0;
    return h;
  }
  export const gameHue = (name) => gameHash(name) % 360;
  export function coverStyle(name) {
    const h = gameHash(name);
    const h1 = h % 360;
    const h2 = (h1 + 30 + (h % 50)) % 360;
    return `background:linear-gradient(165deg, hsl(${h1} 32% 30%), hsl(${h2} 38% 15%))`;
  }
</script>

<script>
  // Game collection: profile cards show a COMPACT strip (a few
  // mini covers + "+N"), and clicking it opens a popup with the full library —
  // big tiles, titles, and (on your own card) add/remove. Adding suggests real
  // games with real box art (backend-proxied Steam search); titles without
  // art — or with remote images disabled — get a generated gradient cover.
  //
  // The popup is rendered INSIDE the host popover's DOM (position:fixed), so
  // the popovers' outside-click/hover-close logic treats it as "inside" and
  // the card stays open underneath.
  import Icon from "./Icon.svelte";
  import EmptyState from "./EmptyState.svelte";
  import { S, flash, patchProfile, refreshRightPanel, setOffDeviceSearch } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { splitStatus, joinStatus } from "./lib/presence.js";
  import { syncLayer } from "./lib/navstack.svelte.js";

  let { games = [], editable = false, onchange } = $props();

  // Box art is fetched by the WEBVIEW, straight from Valve's CDN — the backend
  // allowlist (validGameCover, service.go) bounds WHICH host that is, but it
  // cannot stop the request, and the request is what carries your IP. Opening
  // someone's profile would otherwise be a zero-click disclosure to a third
  // party, which is the same shape as the link-preview problem and gets the
  // same answer: off by default, one switch in Privacy & safety to turn on.
  // Off, every tile falls back to the generated gradient cover below.

  const STRIP = 5; // mini covers shown before "+N"

  let expanded = $state(false);
  let adding = $state(false);
  let q = $state("");
  let results = $state([]);
  let busy = $state(false);
  let broken = $state({}); // name -> cover 404'd; fall back to generated art
  let searchSeq = 0;
  let debounce;

  function openLibrary(withAdd = false) {
    expanded = true;
    adding = withAdd;
  }
  function closeLibrary() {
    expanded = false;
    resetAdd();
  }
  function resetAdd() {
    adding = false;
    q = "";
    results = [];
    clearTimeout(debounce);
  }
  // The library covers the card that opened it, so it is a layer of its own —
  // which is also what makes Escape and back close it first, without the
  // capture-phase listener that used to arrange that by hand.
  syncLayer("panel", () => expanded, closeLibrary);

  function onInput() {
    clearTimeout(debounce);
    // Suggestions off: the editor is still a working editor — you type a title
    // and it is added as written — so this returns without arming the debounce
    // rather than firing a call the backend would refuse anyway. The row below
    // the box says so and offers the switch, because a search box that answers
    // nothing and explains nothing is the worst of the three options.
    if (!S.offDevice.gameSearch) {
      results = [];
      return;
    }
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
    resetAdd();
    if (!have(entry.name)) commit([...games, entry]);
  }

  // Enter takes the top suggestion; with none, the typed title is added as-is.
  function submit(e) {
    e?.preventDefault();
    if (results.length) return pick(results[0]);
    const name = q.trim();
    resetAdd();
    if (name && !have(name)) commit([...games, { name }]);
  }

  const remove = (name) => commit(games.filter((g) => g.name !== name));

  // ---- now playing ----
  // Deliberately manual — no process detection, you press Play when you sit
  // down. "Now playing" IS just the custom status set to "🎮 <name>" through
  // the exact save path the status popover uses, so peers without this build
  // see a plain, sensible status instead of a format they can't render.
  const playing = $derived.by(() => {
    const { emoji, text } = splitStatus(S.identity.status);
    return emoji === "🎮" ? text : "";
  });
  async function togglePlay(name) {
    if (busy) return;
    busy = true;
    try {
      await patchProfile({ status: playing === name ? "" : joinStatus("🎮", name) });
      await refreshRightPanel(); // your own status line in the member list
    } catch (err) {
      flash(err);
    } finally {
      busy = false;
    }
  }

  const initials = (name) =>
    name
      .split(/\s+/)
      .slice(0, 2)
      .map((w) => w[0]?.toUpperCase() || "")
      .join("");

  const showCover = (g) => g.cover && !broken[g.name] && S.prefs.gameCovers;
</script>


<div class="sec-head">
  <span class="sec-label muted">
    Game collection{#if games.length}&nbsp;· {games.length}{/if}
  </span>
  {#if editable}
    <button class="g-add" onclick={() => openLibrary(true)} title="Add a game">
      <Icon name="plus" size={12} /> Add
    </button>
  {/if}
</div>

{#if games.length}
  <button class="g-strip" onclick={() => openLibrary(false)} title="View the collection">
    {#each games.slice(0, STRIP) as g (g.name)}
      <span class="g-mini" style={showCover(g) ? "" : coverStyle(g.name)}>
        {#if showCover(g)}
          <img src={g.cover} alt="" loading="lazy" onerror={() => (broken = { ...broken, [g.name]: true })} />
        {:else}
          <span class="g-mini-glyph">{initials(g.name)}</span>
        {/if}
      </span>
    {/each}
    {#if games.length > STRIP}
      <span class="g-mini g-more">+{games.length - STRIP}</span>
    {/if}
    <span class="g-chev">›</span>
  </button>
{:else if editable}
  <div class="g-empty muted">Show off what you play — add your first game.</div>
{/if}

{#if expanded}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="gs-overlay" onclick={(e) => e.target === e.currentTarget && closeLibrary()}>
    <div class="gs-card" role="dialog" aria-label="Game collection">
      <div class="gs-head">
        <strong>Game collection · {games.length}</strong>
        <span class="gs-actions">
          {#if editable && !adding}
            <button class="g-add" onclick={() => (adding = true)}><Icon name="plus" size={12} /> Add</button>
          {/if}
          <button class="gs-close tap-hit" onclick={closeLibrary} aria-label="Close">
            <Icon name="close" size={14} />
          </button>
        </span>
      </div>

      {#if adding}
        <form class="g-form" onsubmit={submit}>
          <input
            bind:value={q}
            oninput={onInput}
            placeholder={S.offDevice.gameSearch ? "Search games…" : "Type a game title…"}
            maxlength="64"
            disabled={busy}
          />
          <button type="button" class="g-cancel tap-hit" onclick={resetAdd} aria-label="Cancel">
            <Icon name="close" size={13} />
          </button>
        </form>
        {#if !S.offDevice.gameSearch}
          <p class="g-off muted">
            Title suggestions are off, so nothing is sent to Steam.
            <button type="button" class="g-off-on" onclick={() => setOffDeviceSearch("gameSearch", true)}>
              Turn them on
            </button>
          </p>
        {/if}
        {#if results.length}
          <div class="g-results">
            {#each results as r (r.name)}
              <button class="g-result" onclick={() => pick(r)} disabled={have(r.name)}>
                {#if r.thumb && S.prefs.gameCovers}
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
        <div class="gs-body">
        <div class="g-shelf">
          {#each games as g, i (g.name)}
            <div class="g-tile" style="--tile-i:{Math.min(i, 20)}" title={g.name}>
              <div class="g-art" style={showCover(g) ? "" : coverStyle(g.name)}>
                {#if showCover(g)}
                  <img
                    class="g-img"
                    src={g.cover}
                    alt=""
                    loading="lazy"
                    onerror={() => (broken = { ...broken, [g.name]: true })}
                  />
                {:else}
                  <!-- Placeholder "box art": the title set like a cover, not a monogram. -->
                  <span class="g-ph-title">{g.name}</span>
                  <span class="g-ph-base"></span>
                {/if}
                <span class="g-sheen"></span>
                {#if editable}
                  <!-- Play/Stop only on your OWN shelf: it writes your status. -->
                  <button
                    class="g-play"
                    class:on={playing === g.name}
                    onclick={() => togglePlay(g.name)}
                    disabled={busy}
                    title={playing === g.name ? "Stop playing" : `Play ${g.name}`}
                    aria-label={playing === g.name ? `Stop playing ${g.name}` : `Play ${g.name}`}
                  >
                    {#if playing === g.name}<span class="g-stop"></span>{:else}<Icon name="play" size={12} />{/if}
                  </button>
                  <button class="g-x" onclick={() => remove(g.name)} title="Remove {g.name}" aria-label="Remove {g.name}">
                    <Icon name="trash" size={11} />
                  </button>
                {/if}
              </div>
              <span class="g-name">{g.name}</span>
            </div>
          {/each}
        </div>
        </div>
      {:else}
        <div class="gs-empty">
          <EmptyState
            icon="die"
            headline="Nothing here yet"
            sub="Show off what you play — add your first game and it lands on the shelf."
          />
        </div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .sec-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-2);
  }
  .sec-label {
    font-size: var(--fs-tiny);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .g-add {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    font-size: var(--fs-tiny);
    padding: 2px 8px;
    border-radius: 999px;
    border: none;
    background: color-mix(in srgb, var(--accent) 16%, transparent);
    color: var(--accent-hover);
    cursor: pointer;
  }
  .g-add:hover {
    background: color-mix(in srgb, var(--accent) 26%, transparent);
  }
  /* ---- collapsed strip (on the card) ---- */
  .g-strip {
    display: flex;
    align-items: center;
    gap: 5px;
    width: 100%;
    padding: 5px 6px;
    border: none;
    border-radius: var(--radius-sm);
    background: transparent;
    cursor: pointer;
    transition: background var(--dur-quick) ease;
  }
  @media (pointer: fine) {
    .g-strip:hover {
      background: var(--bg-3);
    }
  }
  .g-strip:active {
    background: var(--bg-3);
  }
  .g-mini {
    position: relative;
    width: 26px;
    height: 35px;
    border-radius: var(--radius-sm);
    overflow: hidden;
    display: grid;
    place-items: center;
    flex: none;
    box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.09);
  }
  .g-mini img {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .g-mini-glyph {
    font-size: var(--fs-micro);
    font-weight: 800;
    color: rgba(255, 255, 255, 0.92);
    user-select: none;
  }
  .g-more {
    background: var(--bg-2);
    color: var(--text-muted);
    font-size: var(--fs-tiny);
    font-weight: 700;
  }
  .g-chev {
    margin-left: auto;
    color: var(--text-faint);
    font-size: var(--fs-body);
    transition: transform var(--dur-quick) ease;
  }
  .g-strip:hover .g-chev {
    transform: translateX(2px);
    color: var(--text-muted);
  }
  /* ---- the library popup ---- */
  .gs-overlay {
    position: fixed;
    inset: 0;
    z-index: 320;
    background: var(--scrim);
    display: grid;
    place-items: center;
    padding: calc(5 * var(--vh)) calc(5 * var(--vw));
    animation: gs-fade var(--dur-quick) ease;
  }
  @keyframes gs-fade {
    from {
      opacity: 0;
    }
  }
  .gs-card {
    width: 496px;
    max-width: calc(94 * var(--vw));
    max-height: calc(80 * var(--vh));
    background: var(--bg-elevated, var(--bg-1));
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
    padding: var(--sp-4);
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    overflow: hidden; /* the GRID scrolls (gs-body), the header stays put */
    animation: gs-pop 0.22s var(--ease-spring);
  }
  .gs-body {
    overflow-y: auto;
    overscroll-behavior: contain;
    margin: 0 -6px;
    padding: 2px 6px 6px;
    scrollbar-width: thin;
    /* Soft fade at the scroll edge instead of a hard mid-tile chop. */
    mask-image: linear-gradient(to bottom, black calc(100% - 18px), transparent);
  }
  @keyframes gs-pop {
    from {
      opacity: 0;
      transform: translateY(10px) scale(0.96);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .gs-overlay,
    .gs-card,
    .g-tile {
      animation: none;
    }
  }
  .gs-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-2);
  }
  .gs-head strong {
    font-size: var(--fs-body);
  }
  .gs-actions {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-2);
  }
  .gs-close {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    padding: 0; /* global button padding would shove the icon off center */
    line-height: 0;
    border-radius: 50%;
    border: none;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
  }
  .gs-close:hover {
    background: var(--bg-3);
  }
  /* ---- add form + autocomplete ---- */
  .g-form {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .g-form input {
    flex: 1;
    min-width: 0;
    font-size: var(--fs-compact);
    padding: 7px 10px;
  }
  .g-cancel {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    padding: 0;
    line-height: 0;
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
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    overflow: hidden;
    max-height: 190px;
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  .g-result {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    width: 100%;
    padding: 5px 8px;
    background: var(--bg-0);
    border: none;
    color: var(--text);
    font-size: var(--fs-compact);
    text-align: left;
    cursor: pointer;
  }
  .g-result + .g-result {
    border-top: 1px solid color-mix(in srgb, var(--border) 55%, transparent);
  }
  @media (pointer: fine) {
    .g-result:hover:not(:disabled) {
      background: var(--bg-3);
    }
  }
  .g-result:active:not(:disabled) {
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
    font-size: var(--fs-tiny);
    color: var(--text-faint);
  }
  .g-free {
    border: 1px dashed var(--border);
    border-radius: var(--radius-sm);
    justify-content: center;
    color: var(--text-muted);
  }
  /* One line where the suggestions would have been: says what is switched off
     and offers the switch, rather than leaving an empty box that looks broken. */
  .g-off {
    margin: 6px 2px 0;
    font-size: 12px;
    line-height: 1.5;
  }
  .g-off-on {
    background: none;
    border: 0;
    padding: 0;
    font: inherit;
    color: var(--accent);
    cursor: pointer;
    text-decoration: underline;
  }
  /* ---- full shelf grid (in the popup) ---- */
  .g-shelf {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: var(--sp-3);
  }
  .g-tile {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    min-width: 0;
    animation: g-in 0.25s ease both;
    animation-delay: calc(var(--tile-i, 0) * 0.02s);
  }
  @keyframes g-in {
    from {
      opacity: 0;
      transform: translateY(5px);
    }
  }
  .g-art {
    position: relative;
    aspect-ratio: 2 / 3;
    border-radius: var(--radius-sm);
    display: grid;
    place-items: center;
    overflow: hidden;
    box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.08);
    transition:
      transform var(--dur-standard) ease,
      box-shadow var(--dur-standard) ease;
  }
  @media (pointer: fine) {
    .g-tile:hover .g-art {
      transform: translateY(-2px) scale(1.03);
      box-shadow:
        inset 0 0 0 1px rgba(255, 255, 255, 0.14),
        0 6px 14px rgba(0, 0, 0, 0.35);
    }
    .g-tile:hover .g-sheen {
      transform: translateX(120%);
    }
  }
  .g-img {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  /* Placeholder box art: the title typeset like a cover. */
  .g-ph-title {
    position: relative;
    z-index: 1;
    padding: 10px 8px;
    font-size: 11px;
    font-weight: 800;
    line-height: 1.25;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    text-align: center;
    color: rgba(255, 255, 255, 0.92);
    text-shadow: 0 1px 3px rgba(0, 0, 0, 0.45);
    display: -webkit-box;
    -webkit-line-clamp: 4;
    -webkit-box-orient: vertical;
    overflow: hidden;
    user-select: none;
  }
  /* Subtle vignette + base band so placeholders read as designed covers. */
  .g-ph-base {
    position: absolute;
    inset: 0;
    background:
      radial-gradient(120% 90% at 50% 0%, rgba(255, 255, 255, 0.09), transparent 55%),
      linear-gradient(to top, rgba(0, 0, 0, 0.38), transparent 34%);
    pointer-events: none;
  }
  .g-sheen {
    position: absolute;
    inset: 0;
    background: linear-gradient(115deg, transparent 30%, rgba(255, 255, 255, 0.2) 48%, transparent 62%);
    transform: translateX(-120%);
    transition: transform 0.5s ease;
    pointer-events: none;
  }
  .g-name {
    font-size: var(--fs-small);
    line-height: 1.25;
    color: var(--text-muted);
    text-align: center;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    overflow-wrap: anywhere;
  }
  /* Remove control: a quiet scrim button inside the art, revealed on hover. */
  .g-x {
    position: absolute;
    top: 4px;
    right: 4px;
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    padding: 0; /* global button padding pushed the trash glyph off center */
    line-height: 0;
    border-radius: var(--radius-sm);
    border: none;
    background: rgba(0, 0, 0, 0.55);
    color: rgba(255, 255, 255, 0.85);
    backdrop-filter: blur(2px);
    opacity: 0;
    cursor: pointer;
    transition:
      opacity var(--dur-quick) ease,
      background var(--dur-quick) ease;
  }
  @media (pointer: fine) {
    .g-tile:hover .g-x {
      opacity: 1;
    }
    .g-x:hover {
      background: rgba(190, 40, 45, 0.85);
      color: #fff;
    }
  }
  .g-x:focus-visible {
    opacity: 1;
  }
  .g-x:active {
    background: rgba(190, 40, 45, 0.85);
    color: #fff;
  }
  /* Play/Stop: the manual now-playing toggle. Same quiet scrim as .g-x —
     except while ACTIVE it stays visible, so the shelf shows what's currently
     being played at a glance. */
  .g-play {
    position: absolute;
    top: 4px;
    left: 4px;
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    padding: 0; /* same off-center hazard as .g-x */
    line-height: 0;
    border-radius: var(--radius-sm);
    border: none;
    background: rgba(0, 0, 0, 0.55);
    color: rgba(255, 255, 255, 0.85);
    backdrop-filter: blur(2px);
    opacity: 0;
    cursor: pointer;
    transition:
      opacity var(--dur-quick) ease,
      background var(--dur-quick) ease;
  }
  @media (pointer: fine) {
    .g-tile:hover .g-play {
      opacity: 1;
    }
    .g-play:hover {
      background: color-mix(in srgb, var(--accent) 80%, black);
      color: #fff;
    }
  }
  .g-play:focus-visible {
    opacity: 1;
  }
  .g-play.on {
    opacity: 1;
    background: color-mix(in srgb, var(--accent) 75%, black);
    color: #fff;
  }
  /* The stop glyph is a plain square — the stroked icon set has no "stop". */
  .g-stop {
    width: 8px;
    height: 8px;
    border-radius: 1.5px;
    background: currentColor;
  }
  .g-empty {
    font-size: var(--fs-small);
  }
  /* The empty library dialog: EmptyState leaves centering to the parent. */
  .gs-empty {
    display: grid;
    place-items: center;
    padding: 4px 0 12px;
  }
  /* Touch: "Add a game" is an 18px pill in a tight card header, so the tap area
     is padded out to 44px rather than the chip itself. */
  @media (pointer: coarse), (max-width: 768px) {
    .g-add {
      position: relative;
    }
    .g-add::after {
      content: "";
      position: absolute;
      inset: -13px -4px;
    }
    /* A centred dialog is a desktop shape. On a phone the library arrives from
       the bottom edge like every other sheet in the app. */
    .gs-overlay {
      align-items: end;
      padding: 0;
    }
    .gs-card {
      width: 100%;
      max-width: none;
      max-height: calc(88 * var(--dvh));
      border: none;
      border-radius: var(--radius-sheet) var(--radius-sheet) 0 0;
      padding: 16px 14px calc(16px + var(--safe-bottom));
    }
    /* The remove button was revealed only on :hover — on touch that is an
       invisible control that still takes taps, which is worse than no control
       at all. It shows, on its own scrim, and gets a real hit box. */
    .g-x,
    .g-play {
      opacity: 1;
      width: 30px;
      height: 30px;
      border-radius: var(--radius-md);
    }
    .g-x::after,
    .g-play::after {
      content: "";
      position: absolute;
      inset: -7px;
    }
    /* Four columns of box art on a 360px screen is a 72px tile; three is a
       cover you can actually recognise. */
    .g-shelf {
      grid-template-columns: repeat(3, 1fr);
      gap: 10px;
    }
    .g-result {
      min-height: var(--tap-min);
    }
  }
</style>
