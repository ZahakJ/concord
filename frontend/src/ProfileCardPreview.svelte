<script>
  // Your card, at whatever size the surface has room for.
  //
  // Three studios and the Edit-profile hero all had to show "what this will
  // look like" and all four answered differently: the card-frame studio drew a
  // grey circle and three grey bars, the effect studio a bare 22px disc beside
  // your name, and the hero card knew about your banner and your decoration but
  // not about your card frame or your card effect. So the two cosmetics whose
  // entire selling point is how they sit around a card were the two you could
  // not see on one.
  //
  // This is the card's face, built from the same components the real popover
  // mounts — CardFrame, Banner, Avatar, CardScene, FxLayer — in the same order,
  // at the same 272px width. Not the popover itself: that component is a
  // positioned singleton driven by S.profilePopover, carrying a nav-stack
  // layer, hover-close timers and eight interactive rows, none of which a
  // preview wants. It is the face, and the face is what these pickers are
  // choosing.
  //
  // `scale` is a zoom, not a font size, so the proportions are exactly the ones
  // the real card has and every part shrinks together.
  import Avatar from "./Avatar.svelte";
  import Banner from "./Banner.svelte";
  import CardFrame from "./CardFrame.svelte";
  import CardScene from "./CardScene.svelte";
  import FxLayer from "./FxLayer.svelte";
  import { cardFxTable, cardScenesTable } from "./lib/cosmetics.svelte.js";

  let {
    name = "You",
    emoji = "",
    color = "",
    color2 = "",
    avatar = "",
    banner = "",
    style = null,
    ring = "", // the avatar's gradient/drawn ring
    dec = "", // the worn decoration
    effect = "", // a card scene or particle field
    frame = "", // the card frame
    status = "",
    scale = 1,
  } = $props();

  // Same lazy tables, same fail-closed lookups as the popover: an id this build
  // does not know renders as no effect rather than as a broken one.
  const fxTbl = $derived(cardFxTable());
  const sceneTbl = $derived(cardScenesTable());
  const fxOf = $derived(effect && fxTbl ? fxTbl.cardEffect(effect) : null);
  const sceneOf = $derived(effect && sceneTbl ? sceneTbl.cardScene(effect) : null);
  const legacy = $derived(effect && fxTbl && sceneTbl && !fxOf && !sceneOf ? `card-effect-${effect}` : "");

</script>

<div class="pcp-wrap" style="zoom:{scale}">
  <div class="pcp-scale">
    {#if frame}
      <CardFrame id={frame} {color} color2={color2 || color} />
    {/if}
    <div class="pcp {legacy}" class:framed={!!frame}>
      {#if fxOf}
        <span class="pcp-fx"><FxLayer fx={fxOf.fx} seed={effect} /></span>
      {/if}
      <!-- `banner` in the class list is load-bearing, not decoration. The four
           legacy card effects — aurora, sheen, sparkle, nebula — are pure CSS
           written as `.card-effect-<id> .banner`, so a banner that is not
           classed `banner` wears the overlay's ancestor and paints none of it.
           That is exactly the bug 728c1df fixed on the settings hero, and it
           came back the day this component was extracted: the overlay showed in
           the Edit-profile preview and on nobody's actual card. -->
      <Banner {banner} {color} color2={color2} {style} class="pcp-banner banner" />
      {#if sceneOf}
        <span class="pcp-fx"><CardScene id={effect} {color} color2={color2} /></span>
      {/if}
      <div class="pcp-head">
        <Avatar
          {name}
          {emoji}
          {color}
          image={avatar}
          size={72}
          frame={ring}
          decoration={dec}
          {style}
          {color2}
          preview
        />
      </div>
      <div class="pcp-body">
        <strong>{name}</strong>
        {#if status}<span class="pcp-status">{status}</span>{/if}
      </div>
    </div>
  </div>
</div>

<style>
  /* `zoom`, not `transform: scale()`. A transform shrinks the paint and leaves
     the layout box at full size, so a 0.8 card reserves a full card's worth of
     room and its own container has to be told the scaled height by hand.
     `zoom` scales the layout too, which is why the UI-scale setting uses it,
     and both engines this app ships on honour it. */
  .pcp-wrap {
    flex: none;
  }
  .pcp-scale {
    position: relative;
    width: 272px;
    /* Frame art overhangs the card on purpose — towers above the top edge,
       branches past the corners — so the frame is a SIBLING of the card and
       nothing HERE clips it. Whoever hosts this preview owns the clipping,
       because only they know how much room the overhang may have. */
  }
  .pcp {
    position: relative;
    width: 272px;
    border-radius: var(--radius-lg);
    background: var(--bg-2);
    box-shadow: var(--shadow-pop);
    overflow: hidden;
  }
  .pcp.framed {
    /* A frame draws its own edge; a second one underneath reads as a seam. */
    box-shadow: none;
  }
  .pcp :global(.pcp-banner) {
    height: 84px;
  }
  .pcp-fx {
    position: absolute;
    inset: 0;
    pointer-events: none;
    z-index: 0;
  }
  .pcp-head {
    position: relative;
    z-index: 1;
    margin: -32px 0 0 14px;
  }
  .pcp-body {
    position: relative;
    z-index: 1;
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 6px 14px 14px;
  }
  .pcp-body strong {
    font-size: var(--fs-title);
    font-weight: 700;
  }
  .pcp-status {
    font-size: var(--fs-compact);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
