<script>
  // The stackable-effect layer: snow, rain, a starfield, embers, a CRT grille
  // or tumbling leaves, drawn OVER the app so it composes with every theme
  // pack instead of belonging to one (app.css, "Stackable effects" — that
  // section carries the reasoning for the z-index, the seamless-loop rule and
  // the reduced-motion behaviour).
  //
  // Two mechanisms, usually both at once: tiled gradient sheets for the far
  // field (dense and nearly free, but every speck on a sheet moves in step)
  // and the shared particle engine for the near field (one element per speck,
  // each with its own speed and drift, which is what stops the whole thing
  // reading as a moving wallpaper). lib/themefx.js says which effect gets
  // what.
  //
  // The same component paints the real thing and the Appearance picker's
  // preview cards. `mini` switches it from viewport-fixed to card-absolute and
  // `s` shrinks every periodic length, so a card runs the actual effect at
  // ~40% scale rather than a hand-drawn impression of it.
  import FxLayer from "./FxLayer.svelte";
  import { fxSpec } from "./lib/themefx.js";

  let { fx = "", mini = false, s = 1, scale = 1 } = $props();

  const spec = $derived(fxSpec(fx));
  const sheets = $derived(Array.from({ length: spec.sheets }, (_, i) => i));
</script>

{#if fx}
  <div
    class="fx-overlay fxo-{fx}"
    class:mini
    style={s === 1 ? null : `--fx-s:${s}`}
    aria-hidden="true"
  >
    <!-- `fo` carries the shared geometry. It has to be a class and not
         `.fx-overlay > span`: the particle engine's own root is a <span> too,
         and a descendant selector that catches it inflates the field well past
         the window, so most of a drift falls outside the clip. -->
    {#each sheets as i (i)}
      <span class="fo fo-{i}"></span>
    {/each}
    {#if spec.particle}
      <!-- Seeded by the effect id, so the field is the same on every reload
           and does not reshuffle when an unrelated re-render happens. -->
      <FxLayer fx={spec.particle} seed={fx} {scale} />
    {/if}
  </div>
{/if}
