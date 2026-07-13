<script>
  // A member's profile banner, wherever it appears: the picker's tiles, the
  // editor's live preview, and everyone else's profile card. One component, so
  // the thing you choose is exactly the thing they see.
  //
  // Four sources, in priority order: an animated preset ("preset:galaxy"), an
  // uploaded image (data URI), the member's two theme colors as a gradient, or
  // a solid.
  import FxLayer from "./FxLayer.svelte";
  import { presetOf, isPreset } from "./lib/banners.js";

  let {
    banner = "",
    color = "",
    color2 = "",
    style = null, // { angle, fill } — for the gradient/solid fallbacks
    scale = 1, // <1 shrinks the effect for small tiles
    class: klass = "", // MERGED with .bnr — never let a caller replace it, or
    children, // the box loses its clipping and the weather escapes
    ...rest
  } = $props();

  const preset = $derived(presetOf(banner));
  const angle = $derived(Number.isFinite(style?.angle) ? style.angle : 120);

  const bg = $derived.by(() => {
    if (preset) return preset.base;
    if (banner && !isPreset(banner)) return `url("${banner}") center/cover`;
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
  {@render children?.()}
</div>

<style>
  .bnr {
    position: relative;
    overflow: hidden;
    background: linear-gradient(120deg, var(--accent), var(--accent-hover));
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
