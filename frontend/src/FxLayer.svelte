<script>
  // One effect layer: takes an fx descriptor (from rings.js / banners.js) and
  // paints it. Particle kinds get a deterministic field from lib/fx.js; layered
  // kinds (ripple, bolt, bars, blobs, grid, waves, sheen, scan, rays) get a
  // fixed number of shaped spans. All the motion lives in app.css.
  import { particles, LAYERED, layers } from "./lib/fx.js";

  let { fx, seed = "", scale = 1 } = $props();

  const isField = $derived(!LAYERED.has(fx.kind));
  const ps = $derived(isField ? particles(seed, fx, scale) : []);
  const ls = $derived(isField ? [] : particles(seed, { ...fx, n: layers(fx) }, scale));
  // rain/matrix reuse the "fall" motion with a drop-shaped speck.
  const cls = $derived(
    fx.kind === "rain" || fx.kind === "matrix" ? `fx-fall fx-${fx.kind}` : `fx-${fx.kind}`,
  );
</script>

<span class="fxfield {cls}" class:fx-tumble={fx.tumble} aria-hidden="true">
  {#if isField}
    {#each ps as p, i (i)}
      <span class="fxp" style={p.style} data-g={p.g || undefined}></span>
    {/each}
  {:else if fx.kind === "ripple"}
    {#each ls as p, i (i)}
      <span class="fxr" style={p.style}></span>
    {/each}
  {:else if fx.kind === "bolt"}
    {#each ls as p, i (i)}
      <span class="fxb" class:strike={i === 1} style={p.style}></span>
    {/each}
  {:else if fx.kind === "bars"}
    {#each ls as p, i (i)}
      <span class="fxbar" style={p.style}></span>
    {/each}
  {:else if fx.kind === "blobs"}
    {#each ls as p, i (i)}
      <span class="fxblob" style={p.style}></span>
    {/each}
  {:else if fx.kind === "waves"}
    {#each ls as p, i (i)}
      <span class="fxw" style={p.style}></span>
    {/each}
  {:else if fx.kind === "grid"}
    <span class="fxg" style={ls[0]?.style}></span>
  {:else}
    <span class="fxs" style={ls[0]?.style}></span>
  {/if}
</span>
