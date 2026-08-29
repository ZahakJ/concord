<script>
  // The profile-CARD frame picker.
  //
  // Every tile paints the real frame through the same component the card uses,
  // around a miniature of the card itself, because a card frame is defined by
  // what it does to a card's edges — a swatch of the art alone would tell you
  // nothing about how it sits. The tiles animate for the same reason the
  // decoration picker's do: motion is half of what you are choosing between,
  // and freezing it in the one place it has to sell itself was a bug once
  // already.
  import { S } from "./lib/state.svelte.js";
  import { motionInView } from "./lib/inview.js";
  import { pushLayer } from "./lib/navstack.svelte.js";
  import Icon from "./Icon.svelte";
  import { portal } from "./lib/portal.js";
  import CardFrame from "./CardFrame.svelte";
  import ProfileCardPreview from "./ProfileCardPreview.svelte";
  import { CARD_FRAME_BY_ID, CARD_FRAME_GROUPS, CARD_FRAMES } from "./lib/cardframes.js";

  // `current`, not `frame`: this file has no $effect, but the neighbouring
  // studios were both bitten by a prop shadowing a rune and the convention is
  // cheaper to keep than to re-learn.
  let { current = "", color = "#14a394", color2 = "", card = {}, onApply, onClose } = $props();
  $effect(() => pushLayer("studio", () => onClose?.()));

  let sel = $state(current);
</script>

<div class="cfs-scrim" role="presentation" onclick={onClose} use:portal></div>
<div class="cfs" role="dialog" use:portal aria-label="Choose a card frame" style="--c1:{color};--c2:{color2 || color}">
  <div class="cfs-head">
    <button class="icon-btn" onclick={onClose} aria-label="Back"><Icon name="back" size={16} /></button>
    <strong>Card frame</strong>
    <span class="tiny muted">{CARD_FRAME_BY_ID[sel]?.name || "None"}</span>
  </div>

  <!-- Your real card, at 0.8, centred. It used to be a grey circle and three
       grey bars in a left-aligned band with 60% of the hero empty beside it —
       a wireframe standing in for the one thing this picker exists to sell. -->
  <div class="preview">
    <div class="stage">
      <ProfileCardPreview {...card} {color} {color2} frame={sel} scale={0.8} />
    </div>
  </div>

  <div class="library">
    <div class="grid">
      <button class="opt none" class:sel={sel === ""} onclick={() => (sel = "")}>
        <span class="tile none-tile"><span class="none-word">None</span></span>
        <span class="oname">No frame</span>
      </button>
    </div>
    {#each CARD_FRAME_GROUPS as g (g.title)}
      <div class="gtitle">{g.title}</div>
      <div class="grid">
        {#each g.ids as id (id)}
          <button class="opt" class:sel={sel === id} onclick={() => (sel = id)} use:motionInView>
            <span class="tile">
              <span class="mini">
                <CardFrame {id} {color} color2={color2 || color} />
                <span class="mini-banner"></span>
                <span class="mini-av"></span>
              </span>
            </span>
            <span class="oname">{CARD_FRAME_BY_ID[id].name}</span>
          </button>
        {/each}
      </div>
    {/each}
  </div>

  <div class="cfs-foot">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={() => onApply({ cf: sel })}>Apply</button>
  </div>
</div>

<style>
  .cfs-scrim {
    position: fixed;
    inset: 0;
    background: var(--scrim);
    /* Above modals/Modal.svelte's own overlay (z-index 100 + depth). These
       studios are portalled to <body>, so they no longer inherit the dialog's
       stacking context and have to out-rank it explicitly. */
    z-index: 400;
  }
  .cfs {
    position: fixed;
    inset: 50% auto auto 50%;
    transform: translate(-50%, -50%);
    width: min(640px, 94 * var(--vw));
    max-height: calc(88 * var(--vh));
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
    z-index: 401;
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
  .cfs-head {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px 14px;
    border-bottom: 1px solid var(--border);
  }
  .cfs-head .tiny {
    margin-left: auto;
  }
  .preview {
    display: flex;
    justify-content: center;
    padding: var(--sp-4);
    border-bottom: 1px solid var(--border);
  }
  /* The stage is what gives the overhang somewhere to be. Without the room
     the art is drawn, then clipped by the panel, and every frame looks docked. */
  /* Room for the overhang, and a hard edge so it cannot escape the panel: a
     frame's art is drawn well outside the card it wraps, and the nearest thing
     with an overflow is otherwise the page. */
  .stage {
    display: grid;
    place-items: center;
    width: min(100%, 360px);
    height: 250px;
    overflow: hidden;
    border-radius: var(--radius-sm);
    background: var(--bg-0);
  }
  /* Half a real card, at a real card's PROPORTIONS (272 × ~353). Get the aspect
     wrong here and the preview stretches the art differently from the thing it
     is previewing. Bottom-aligned, because nearly all the overhang is above. */
  .card {
    position: relative;
    width: 136px;
    height: 177px;
    border-radius: var(--radius-sm);
    background: var(--bg-1);
    box-shadow: var(--shadow-pop);
  }
  .pv-banner {
    position: absolute;
    inset: 0 0 auto;
    height: 56px;
    border-radius: var(--radius-sm) var(--radius-sm) 0 0;
    background: linear-gradient(140deg, var(--c1), var(--c2));
  }
  .pv-av {
    position: absolute;
    left: 7px;
    top: 37px;
    width: 36px;
    height: 36px;
    border-radius: 50%;
    background: var(--bg-3);
    border: 2px solid var(--bg-1);
  }
  .pv-name {
    position: absolute;
    left: 8px;
    top: 80px;
    width: 58px;
    height: 7px;
    border-radius: var(--radius-sm);
    background: var(--text);
    opacity: 0.85;
  }
  .pv-line {
    position: absolute;
    left: 8px;
    top: 94px;
    width: 110px;
    height: 5px;
    border-radius: 3px;
    background: var(--text-faint);
  }
  .pv-line.short {
    top: 105px;
    width: 80px;
  }
  .pv-input {
    position: absolute;
    left: 9px;
    right: 9px;
    bottom: 9px;
    height: 20px;
    border-radius: var(--radius-sm);
    background: var(--bg-0);
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
    grid-template-columns: repeat(auto-fill, minmax(112px, 1fr));
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
    /* Twelve live frames at once is a lot of compositing; the ones nobody has
       scrolled to should not be paying for it. */
    content-visibility: auto;
    contain-intrinsic-size: 112px 152px;
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
  .tile {
    position: relative;
    display: grid;
    align-items: end;
    justify-items: center;
    height: 126px;
    padding-bottom: var(--sp-2);
    overflow: hidden;
    background: var(--bg-0);
  }
  .mini {
    position: relative;
    display: block;
    width: 56px;
    height: 73px;
    border-radius: 3px;
    background: var(--bg-2);
  }
  .mini-banner {
    position: absolute;
    inset: 0 0 auto;
    height: 24px;
    border-radius: 3px 3px 0 0;
    background: linear-gradient(140deg, var(--c1, #14a394), var(--c2, #14a394));
  }
  .mini-av {
    position: absolute;
    left: 4px;
    top: 15px;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: var(--bg-3);
    border: 1.5px solid var(--bg-2);
  }
  .oname {
    font-size: var(--fs-tiny);
    color: var(--text-muted);
    padding: 0 6px;
    line-height: 1.25;
  }
  .cfs-foot {
    display: flex;
    justify-content: flex-end;
    gap: var(--sp-2);
    padding: 12px 14px;
    border-top: 1px solid var(--border);
  }
  @media (max-width: 560px) {
    .preview {
      flex-direction: column;
      align-items: stretch;
    }
    .stage {
      width: auto;
    }
  }
</style>
