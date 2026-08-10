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
  //
  // Two libraries feed one choice. A SCENE (lib/cardscenes.js) is drawn art —
  // a ghost, a canopy, a planet with moons; a FIELD (lib/cardfx.js) is the
  // particle engine. They share the `effect` id space and are shown in one
  // gallery under two headings, exactly as drawn frames and gradient rings
  // share `frame`.
  import { registerOverlay, S } from "./lib/state.svelte.js";
  import Icon from "./Icon.svelte";
  import FxLayer from "./FxLayer.svelte";
  import CardScene from "./CardScene.svelte";
  import { CARD_EFFECT_BY_ID, CARD_EFFECT_GROUPS, cardEffect } from "./lib/cardfx.js";
  import { CARD_SCENE_BY_ID, CARD_SCENE_GROUPS, cardScene } from "./lib/cardscenes.js";

  // `current`, NOT `effect`: a prop of that name shadows the $effect rune, and
  // the call below silently compiles to a store subscription instead
  // ("e.subscribe is not a function" at runtime). BannerStudio carries the same
  // warning for the same reason.
  let { current = "", color = "#14a394", color2 = "", onApply, onClose } = $props();
  $effect(() => registerOverlay(() => onClose?.()));

  let sel = $state(current);
  const cur = $derived(cardEffect(sel));
  const curScene = $derived(cardScene(sel));
  const selName = $derived(CARD_SCENE_BY_ID[sel]?.name || CARD_EFFECT_BY_ID[sel]?.name || "None");
  // Tiles are small, so the engine gets a smaller scale — its own signal to cut
  // the particle count rather than shrink a full field into a thumbnail.
  const tileScale = $derived(S.isMobile ? 0.3 : 0.45);
</script>

<div class="es-scrim" role="presentation" onclick={onClose}></div>
<div class="es" role="dialog" aria-label="Choose a profile effect">
  <div class="es-head">
    <button class="icon-btn" onclick={onClose} aria-label="Back"><Icon name="chevron" size={16} /></button>
    <strong>Profile effect</strong>
    <span class="tiny muted">{selName}</span>
  </div>

  <div class="preview" style="--c1:{color};--c2:{color2 || color}">
    <div class="card">
      {#if curScene}
        <span class="cfx"><CardScene id={sel} {color} {color2} /></span>
      {:else if cur}
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

    <div class="stitle">
      Scenes <span class="tiny muted">drawn art, animated</span>
    </div>
    <!-- A scene is painted over the banner, because a scene IS the art: its
         subject sits in the top third, exactly where a banner would bury it.
         That is the right call for the picture but a surprise for the person
         who uploaded a banner and watched it vanish without being told why, so
         the trade is stated here rather than discovered. -->
    <p class="snote tiny muted">
      A scene is painted over your banner image, so you'll see the scene instead
      while one is on.
    </p>
    {#each CARD_SCENE_GROUPS as g (g.title)}
      <div class="gtitle">{g.title}</div>
      <div class="grid">
        {#each g.ids as id (id)}
          <button class="opt" class:sel={sel === id} onclick={() => (sel = id)}>
            <span class="tile scene">
              <CardScene {id} {color} {color2} scale={tileScale} />
            </span>
            <span class="oname">{CARD_SCENE_BY_ID[id].name}</span>
          </button>
        {/each}
      </div>
    {/each}

    <div class="stitle">
      Fields <span class="tiny muted">weather and particles</span>
    </div>
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
    background: rgba(0, 0, 0, 0.55);
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
    /* Matches modals/Modal.svelte deliberately. These panels had drifted to
       --bg-2 on a 0.4 scrim with no shadow, which put them BELOW the surface
       every other dialog in the app sits on and left them reading as
       half-transparent — the panel edge fell too close in value to the page
       behind it to register as an edge at all. */
    background: var(--bg-elevated);
    box-shadow: var(--shadow-pop);
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
  /* The two libraries are genuinely different kinds of thing, so the gallery
     says so once rather than leaving you to work out why half the tiles are
     drawings. */
  .stitle {
    display: flex;
    align-items: baseline;
    gap: 8px;
    font-size: var(--fs-ui);
    font-weight: 600;
    color: var(--text);
    margin: 16px 0 2px;
    padding-bottom: 6px;
    border-bottom: 1px solid var(--border);
  }
  .snote {
    margin: 8px 0 0;
    line-height: 1.45;
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
  /* A scene paints its own sky, and it needs the room to show a subject. */
  .tile.scene {
    height: 62px;
    background: #0d1017;
  }
  /* Scene tiles hold their opening frame until you point at one or pick it —
     the same bargain the ring picker already strikes with its orbits, and the
     big preview above stays live regardless.

     Measured, because the obvious lever was the wrong one: cutting the number
     of ANIMATED elements per tile from 219 to 124 across the library barely
     moved the picker at all. The cost is not per moving element — one running
     animation repaints its ENTIRE tile, every one of its few hundred paths —
     so it is per animated TILE. Twelve drawn scenes playing at once cost about
     33 points of a core on top of the 21 the particle gallery already spends;
     with this rule they cost about 1. Stopping the animation rather than
     pausing it is safe here for the same reason prefers-reduced-motion is: no
     scene is authored off-canvas or at zero opacity, so the frame you get with
     the motion switched off is the picture. */
  .opt:not(:hover):not(.sel) .tile.scene :global(.n) {
    animation: none;
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
