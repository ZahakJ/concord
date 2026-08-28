<script>
  // A banner, wherever it appears: a member's profile card, the picker tiles of
  // both studios, and a guild's channel-list header. One component, so the thing
  // you choose is exactly the thing everyone else sees.
  //
  // Four sources, in priority order: an animated preset ("preset:galaxy"), an
  // uploaded image (data URI), the member's two theme colors as a gradient, or
  // a solid. A preset id is resolved against BOTH catalogues — profile
  // (lib/banners.js) and guild (lib/guildbanners.js) — because "preset:<id>" is
  // one wire format and this is the one component that has to paint it.
  import FxLayer from "./FxLayer.svelte";
  import { presetOf, isPreset } from "./lib/banners.js";
  import { guildPresetOf } from "./lib/guildbanners.js";
  import { isSafeImageDataURI } from "./lib/images.js";

  let {
    banner = "",
    color = "",
    color2 = "",
    style = null, // { angle, fill } — for the gradient/solid fallbacks
    scale = 1, // <1 shrinks the effect for small tiles
    scrim = "", // "light"|"dark" — see below; only for banners that carry text
    tint = false, // blend the art toward the theme pack's own ground — see below
    class: klass = "", // MERGED with .bnr — never let a caller replace it, or
    children, // the box loses its clipping and the weather escapes
    ...rest
  } = $props();

  const preset = $derived(presetOf(banner) || guildPresetOf(banner));
  const angle = $derived(Number.isFinite(style?.angle) ? style.angle : 120);

  // Defense in depth: the backend already restricts a peer's banner to a strict
  // base64 image data-URI (validImageDataURI), but this value lands inside a CSS
  // url("…"). Only ever emit a banner image if it still looks like that exact
  // shape — never interpolate an arbitrary string into CSS.
  const safeImage = $derived(banner && !isPreset(banner) && isSafeImageDataURI(banner) ? banner : "");

  const bg = $derived.by(() => {
    if (preset) return preset.base;
    if (safeImage) return `url("${safeImage}") center/cover`;
    if (!color) return ""; // the CSS default accent gradient
    if (style?.fill === "solid" || !color2) return color;
    return `linear-gradient(${angle}deg, ${color}, ${color2})`;
  });
</script>

<div
  class="bnr {klass}"
  class:drift={preset?.drift}
  style={bg ? `background:${bg};` : ""}
  {...rest}
>
  {#if preset?.fx}
    <FxLayer fx={{ ...preset.fx, tumble: preset.tumble }} seed={preset.id} {scale} />
  {/if}
  {#if tint}
    <span class="tint" aria-hidden="true"></span>
  {/if}
  {#if scrim}
    <span class="scrim" class:ink-dark={scrim === "dark"} aria-hidden="true"></span>
  {/if}
  {@render children?.()}
</div>

<style>
  .bnr {
    position: relative;
    overflow: hidden;
    background: linear-gradient(120deg, var(--accent), var(--accent-hover));
  }
  /* A banner that CARRIES text (a guild header prints a name and a 26px icon
     straight onto the art) needs a floor under that text, not a hope. The scrim
     ships with the art path so every template — and every uploaded image —
     inherits the same guarantee, over the fx layer so a bright particle can't
     drift under the name either. Its strength where the text sits is
     SCRIM_ALPHA in lib/guildbanners.js, which guildbanners.test.mjs composites
     over every colour a template can put back there to prove 4.5:1. Change one
     and change the other. */
  .scrim {
    position: absolute;
    inset: 0;
    pointer-events: none;
    background: linear-gradient(rgba(0, 0, 0, 0.1), rgba(0, 0, 0, 0.5) 62%, rgba(0, 0, 0, 0.62));
  }
  /* The one place a banner is a whole PACK's problem: the guild header is the
     largest saturated block on the screen and it was the one element that never
     got the memo, staying a deep indigo in Gruvbox's warm terminal palette and
     in Paper's cream. This blends it a third of the way toward the pack's own
     --bg-1, which keeps the guild's identity while letting it sit in the room.

     UNDER the scrim, deliberately. Over it, the tint would land on the finished
     text floor and drag it toward the pack — a light pack would pull a dark
     scrim up to ~3:1 under white text. Under it, the scrim still covers the
     text band at 0.62, so the tint reaches that band at 0.38 of its strength:
     the worst case either way (a dark template in a cream pack, a pale one in a
     charcoal pack) stays above 4.5:1. SCRIM_ALPHA and guildbanners.test.mjs are
     untouched, because the layer they reason about is unchanged. */
  .tint {
    position: absolute;
    inset: 0;
    pointer-events: none;
    background: var(--bg-1);
    opacity: 0.3;
  }
  /* Pale templates print DARK text, so their scrim is white. */
  .scrim.ink-dark {
    background: linear-gradient(
      rgba(255, 255, 255, 0.12),
      rgba(255, 255, 255, 0.5) 62%,
      rgba(255, 255, 255, 0.64)
    );
  }
  /* Gradient presets breathe: a slow pan, no repaint cost. */
  .drift {
    background-size: 170% 170% !important;
    animation: bnr-drift 18s ease-in-out infinite alternate;
  }
  @keyframes bnr-drift {
    from {
      background-position: 0% 50%;
    }
    to {
      background-position: 100% 50%;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .drift {
      animation: none;
    }
  }
</style>
