<script>
  // The play mark on every video poster: a round dark lens with a white
  // triangle, drawn on whole pixels so it stays crisp at any density, over any
  // picture (light or dark). `brand` gives it YouTube's red, for a poster
  // that says where it plays.
  let { size = 64, brand = false } = $props();
</script>

<span class="pg" class:brand style:--pg="{size}px" aria-hidden="true">
  <svg viewBox="0 0 64 64" width={size} height={size} shape-rendering="geometricPrecision">
    <circle cx="32" cy="32" r="31" class="lens" />
    <circle cx="32" cy="32" r="30.5" class="rim" />
    <path d="M26 20.6v22.8a1.6 1.6 0 0 0 2.4 1.4l18.6-11.4a1.6 1.6 0 0 0 0-2.8L28.4 19.2a1.6 1.6 0 0 0-2.4 1.4z" class="tri" />
  </svg>
</span>

<style>
  .pg {
    display: inline-grid;
    width: var(--pg);
    height: var(--pg);
    filter: drop-shadow(0 4px 14px rgb(0 0 0 / 0.35));
  }
  .lens {
    fill: rgb(12 14 16 / 0.62);
  }
  .brand .lens {
    fill: #e8202a;
  }
  .rim {
    fill: none;
    stroke: rgb(255 255 255 / 0.28);
    stroke-width: 1;
  }
  .tri {
    fill: #fff;
  }
</style>
