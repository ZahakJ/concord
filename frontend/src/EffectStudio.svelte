<script>
  // The profile-card effect picker.
  //
  // This used to be a five-item strip inside the banner editor, on the
  // reasoning that an overlay only affected the banner. That stopped being true
  // — the effect now plays across the whole card — and it was the reason the
  // choice stayed at four while the engine underneath had seventeen kinds.
  //
  // Every tile runs the REAL effect through the same component the card uses,
  // so a tile cannot drift from what picking it does. That costs a lot of
  // simultaneous animation, which is what `content-visibility` on the tiles is
  // for: the ones you have not scrolled to do not composite.
  import { registerOverlay, S } from "./lib/state.svelte.js";
  import Icon from "./Icon.svelte";
  import FxLayer from "./FxLayer.svelte";
  import { CARD_EFFECT_BY_ID, CARD_EFFECT_GROUPS, cardEffect } from "./lib/cardfx.js";

  // `current`, NOT `effect`: a prop of that name shadows the $effect rune, and
  // the call below silently compiles to a store subscription instead
  // ("e.subscribe is not a function" at runtime). BannerStudio carries the same
  // warning for the same reason.
  let { current = "", color = "#14a394", color2 = "", onApply, onClose } = $props();
  $effect(() => registerOverlay(() => onClose?.()));

  let sel = $state(current);
  const cur = $derived(cardEffect(sel));
  // Tiles are small, so the engine gets a smaller scale — its own signal to cut
  // the particle count rather than shrink a full field into a thumbnail.
  const tileScale = $derived(S.isMobile ? 0.3 : 0.45);
</script>

<div class="es-scrim" role="presentation" onclick={onClose}></div>
<div class="es" role="dialog" aria-label="Choose a profile effect">
  <div class="es-head">
    <button class="icon-btn" onclick={onClose} aria-label="Back"><Icon name="chevron" size={16} /></button>
    <strong>Profile effect</strong>
    <span class="tiny muted">{CARD_EFFECT_BY_ID[sel]?.name || "None"}</span>
  </div>

  <div class="preview" style="--c1:{color};--c2:{color2 || color}">
    <div class="card">
      {#if cur}
        <span class="cfx"><FxLayer fx={cur.fx} seed={sel} /></span>
      {/if}
      <div class="who"><span class="av"></span><b>{S.displayName || "You"}</b></div>
    </div>
    <p class="tiny muted">
      Plays behind your name on your profile card. Chosen to stay out of the way
      of the text — this is decoration, not weather you have to read through.
    </p>
  </div>

  <div class="library">
    <button class="opt none" class:sel={sel === ""} onclick={() => (sel = "")}>None</button>
    {#each CARD_EFFECT_GROUPS as g (g.title)}
      <div class="gtitle">{g.title}</div>
      <div class="grid">
        {#each g.ids as id (id)}
          <button class="opt" class:sel={sel === id} onclick={() => (sel = id)}>
            <span class="tile">
              <FxLayer fx={CARD_EFFECT_BY_ID[id].fx} seed={id} scale={tileScale} />
            </span>
            <span class="oname">{CARD_EFFECT_BY_ID[id].name}</span>
          </button>
        {/each}
      </div>
    {/each}
  </div>

  <div class="es-foot">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={() => onApply({ effect: sel })}>Apply</button>
  </div>
</div>

<style>
  .es-scrim {
    position: fixed;
    inset: 0;
    background: #0006;
    z-index: 60;
  }
  .es {
    position: fixed;
    inset: 50% auto auto 50%;
    transform: translate(-50%, -50%);
    width: min(600px, 94vw);
    max-height: 88vh;
    display: flex;
    flex-direction: column;
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    z-index: 61;
    overflow: hidden;
  }
  .es-head {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px 14px;
    border-bottom: 1px solid var(--border);
  }
  .es-head .tiny {
    margin-left: auto;
  }
  .preview {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 16px;
    border-bottom: 1px solid var(--border);
  }
  .preview p {
    margin: 0;
    line-height: 1.5;
  }
  .card {
    position: relative;
    flex: none;
    width: 190px;
    height: 92px;
    border-radius: var(--radius-sm);
    overflow: hidden;
    background: linear-gradient(140deg, var(--c1), var(--c2));
  }
  .cfx {
    position: absolute;
    inset: 0;
  }
  .who {
    position: absolute;
    left: 10px;
    bottom: 9px;
    display: flex;
    align-items: center;
    gap: 7px;
    color: #fff;
    text-shadow: 0 1px 3px #0009;
    font-size: var(--fs-tiny);
  }
  .av {
    width: 22px;
    height: 22px;
    border-radius: 50%;
    background: #0004;
    border: 2px solid #0003;
  }
  .library {
    overflow-y: auto;
    padding: 12px 14px 4px;
  }
  .gtitle {
    font-size: var(--fs-tiny);
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-muted);
    margin: 14px 0 8px;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(96px, 1fr));
    gap: 8px;
  }
  .opt {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 0 0 7px;
    background: var(--bg-1);
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    color: var(--text);
    font: inherit;
    cursor: pointer;
    overflow: hidden;
    /* Every tile animates. Off-screen ones must not composite, or a 29-tile
       gallery is 29 live particle fields at once. */
    content-visibility: auto;
    contain-intrinsic-size: 96px 74px;
  }
  .opt:hover {
    border-color: var(--border);
  }
  .opt.sel {
    border-color: var(--accent);
  }
  .opt.none {
    padding: 10px;
    text-align: center;
    font-size: var(--fs-tiny);
    color: var(--text-muted);
  }
  .tile {
    position: relative;
    display: block;
    height: 50px;
    background: linear-gradient(140deg, #2a2f3a, #1b1f27);
  }
  .oname {
    font-size: var(--fs-tiny);
    color: var(--text-muted);
    padding: 0 6px;
    line-height: 1.25;
  }
  .es-foot {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    padding: 12px 14px;
    border-top: 1px solid var(--border);
  }
</style>
