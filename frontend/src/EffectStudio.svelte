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
  // a ghost, a canopy, a planet with moons. A FIELD (lib/cardfx.js) is the
  // particle engine: snow, rain, embers, confetti.
  //
  // Both are offered, and the fields were briefly not. That was a mistake made
  // by comparing them on the wrong axis: a field loses to a scene at being a
  // PICTURE, which is not what a field is for. Somebody who wants quiet snow
  // falling behind their name does not want a snowline, and taking the choice
  // away did not give them the scene — it gave them nothing. Scenes lead the
  // gallery because they are the bigger thing to look at; the fields keep
  // their shelf under them.
  import { S } from "./lib/state.svelte.js";
  import { pushLayer } from "./lib/navstack.svelte.js";
  import Icon from "./Icon.svelte";
  import FxLayer from "./FxLayer.svelte";
  import CardScene from "./CardScene.svelte";
  import ProfileCardPreview from "./ProfileCardPreview.svelte";
  import { CARD_EFFECT_BY_ID, CARD_EFFECT_GROUPS, cardEffect } from "./lib/cardfx.js";
  import { CARD_SCENE_BY_ID, CARD_SCENE_GROUPS, cardScene } from "./lib/cardscenes.js";

  // `current`, NOT `effect`: a prop of that name shadows the $effect rune, and
  // the call below silently compiles to a store subscription instead
  // ("e.subscribe is not a function" at runtime). BannerStudio carries the same
  // warning for the same reason.
  let { current = "", color = "#14a394", color2 = "", card = {}, onApply, onClose } = $props();
  $effect(() => pushLayer("studio", () => onClose?.()));

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
    <button class="icon-btn" onclick={onClose} aria-label="Back"><Icon name="back" size={16} /></button>
    <strong>Profile effect</strong>
    <span class="tiny muted">{selName}</span>
  </div>

  <!-- The real card, wearing the effect. The preview used to be a 190x92
       gradient strip with a 22px black disc where your face goes, which told
       you what the effect looks like over a rectangle and nothing about what it
       looks like over YOUR card — the only place it will ever play. -->
  <div class="preview">
    <div class="stage">
      <ProfileCardPreview {...card} {color} {color2} effect={sel} scale={0.8} />
    </div>
  </div>

  <div class="library">
    <div class="grid">
      <button class="opt none" class:sel={sel === ""} onclick={() => (sel = "")}>
        <span class="tile none-tile"><span class="none-word">None</span></span>
        <span class="oname">No effect</span>
      </button>
    </div>

    <div class="stitle">
      Scenes <span class="tiny muted">drawn art, animated</span>
    </div>
    <!-- A scene is painted over the banner, because a scene IS the art: its
         subject sits in the top third, exactly where a banner would bury it.
         That is the right call for the picture but a surprise for the person
         who uploaded a banner and watched it vanish without being told why, so
         the trade is stated here rather than discovered. -->
    <p class="snote tiny muted">A scene replaces your banner while it's on.</p>
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
    background: var(--scrim);
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
  /* Quiet. app.css fills a bare button with the accent, so an unstyled
     .icon-btn made the BACK arrow the loudest control on the panel — louder
     than Apply. A back button is chrome; it gets a ghost's treatment, the same
     one Modal's own back arrow has. */
  .icon-btn {
    display: grid;
    place-items: center;
    width: 30px;
    height: 30px;
    padding: 0;
    background: transparent;
    color: var(--text-muted);
    border: none;
    border-radius: var(--radius-md);
    transition:
      color var(--dur-standard) ease,
      background var(--dur-standard) ease;
  }
  @media (pointer: fine) {
    .icon-btn:hover {
      color: var(--text);
      background: var(--bg-3);
    }
  }
  @media (pointer: coarse), (max-width: 768px) {
    .icon-btn {
      width: var(--tap-min);
      height: var(--tap-min);
    }
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
    justify-content: center;
    padding: var(--sp-4);
    border-bottom: 1px solid var(--border);
  }
  .stage {
    display: grid;
    place-items: center;
    width: min(100%, 360px);
    padding: 14px 0;
    overflow: hidden;
    border-radius: var(--radius-sm);
    background: var(--bg-0);
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
    gap: var(--sp-2);
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
    gap: var(--sp-2);
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
  /* A tile, like everything else it sits above. "None" used to be a 49x36 grey
     pill floating alone over the first section header — a different species of
     control for the option people reach for most often. Same footprint as an
     art tile, dashed interior because this one IS the empty choice, label
     centred. */
  .none-tile {
    display: grid;
    place-items: center;
    border: 1px dashed var(--border);
    border-radius: var(--radius-sm);
    background: transparent;
  }
  .none-word {
    font-size: var(--fs-compact);
    color: var(--text-muted);
  }
  .opt.none.sel .none-word {
    color: var(--text);
  }
  /* The tile is a stand-in for a profile card, so it wears the surface a
     profile card is painted on — not a dark gradient nailed on in hex. On the
     six daylight packs those two literals made every preview a dark chip
     floating on a bright page, and the pale effects inside them (of which
     there are plenty) were invisible against the wrong ground. The decoration
     picker next door has always used the token. */
  .tile {
    position: relative;
    display: block;
    height: 50px;
    background: linear-gradient(140deg, var(--bg-1), var(--bg-0));
  }
  /* A scene paints its own sky, and it needs the room to show a subject. */
  .tile.scene {
    height: 62px;
    background: var(--bg-0);
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
    gap: var(--sp-2);
    padding: 12px 14px;
    border-top: 1px solid var(--border);
  }
</style>
