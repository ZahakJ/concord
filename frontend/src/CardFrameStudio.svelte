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
  import { registerOverlay, S } from "./lib/state.svelte.js";
  import Icon from "./Icon.svelte";
  import CardFrame from "./CardFrame.svelte";
  import { CARD_FRAME_BY_ID, CARD_FRAME_GROUPS, CARD_FRAMES } from "./lib/cardframes.js";

  // `current`, not `frame`: this file has no $effect, but the neighbouring
  // studios were both bitten by a prop shadowing a rune and the convention is
  // cheaper to keep than to re-learn.
  let { current = "", color = "#14a394", color2 = "", onApply, onClose } = $props();
  $effect(() => registerOverlay(() => onClose?.()));

  let sel = $state(current);
</script>

<div class="cfs-scrim" role="presentation" onclick={onClose}></div>
<div class="cfs" role="dialog" aria-label="Choose a card frame" style="--c1:{color};--c2:{color2 || color}">
  <div class="cfs-head">
    <button class="icon-btn" onclick={onClose} aria-label="Back"><Icon name="chevron" size={16} /></button>
    <strong>Card frame</strong>
    <span class="tiny muted">{CARD_FRAME_BY_ID[sel]?.name || "None"}</span>
  </div>

  <div class="preview">
    <div class="stage">
      <div class="card">
        {#if sel}
          <CardFrame id={sel} {color} color2={color2 || color} />
        {/if}
        <div class="pv-banner"></div>
        <div class="pv-av"></div>
        <div class="pv-name"></div>
        <div class="pv-line"></div>
        <div class="pv-line short"></div>
        <div class="pv-input"></div>
      </div>
    </div>
    <p class="tiny muted">
      Scenery around your profile card — it stands above the top edge and reaches
      past the corners. The middle stays clear so what the card actually says is
      still readable.
    </p>
  </div>

  <div class="library">
    <button class="opt none" class:sel={sel === ""} onclick={() => (sel = "")}>None</button>
    {#each CARD_FRAME_GROUPS as g (g.title)}
      <div class="gtitle">{g.title}</div>
      <div class="grid">
        {#each g.ids as id (id)}
          <button class="opt" class:sel={sel === id} onclick={() => (sel = id)}>
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
    <p class="tiny muted foot-note">{CARD_FRAMES.length} frames. Nothing is downloaded — every one of these is drawn.</p>
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
    background: rgba(0, 0, 0, 0.55);
    z-index: 60;
  }
  .cfs {
    position: fixed;
    inset: 50% auto auto 50%;
    transform: translate(-50%, -50%);
    width: min(640px, 94vw);
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
    align-items: center;
    gap: 16px;
    padding: 16px;
    border-bottom: 1px solid var(--border);
  }
  .preview p {
    margin: 0;
    line-height: 1.5;
  }
  /* The stage is what gives the overhang somewhere to be. Without the padding
     the art is drawn, then clipped by the panel, and every frame looks docked. */
  .stage {
    flex: none;
    display: grid;
    align-items: end;
    justify-items: center;
    width: 210px;
    height: 290px;
    padding-bottom: 18px;
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
    border-radius: 7px;
    background: var(--bg-1);
    box-shadow: var(--shadow-pop);
  }
  .pv-banner {
    position: absolute;
    inset: 0 0 auto;
    height: 56px;
    border-radius: 7px 7px 0 0;
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
    border-radius: 4px;
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
    border-radius: 5px;
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
  .opt.none {
    padding: 10px;
    text-align: center;
    font-size: var(--fs-tiny);
    color: var(--text-muted);
  }
  .tile {
    position: relative;
    display: grid;
    align-items: end;
    justify-items: center;
    height: 126px;
    padding-bottom: 8px;
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
  .foot-note {
    margin: 14px 0 4px;
  }
  .cfs-foot {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
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
