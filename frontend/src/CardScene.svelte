<script module>
  // Gradient and filter ids have to be unique per PAINTED INSTANCE, not per
  // scene: `url(#id)` binds to the first match in the document, and the picker
  // shows a dozen scenes at once next to a live preview card. Two instances
  // sharing an id would leave one of them painted with the other's colours,
  // because c1/c2 resolve from the element that actually got referenced.
  let seq = 0;
</script>

<script>
  // The scene painter. Draws one entry of lib/cardscenes.js into the 272×400
  // box the profile card occupies. Everything is paths, gradients and filters
  // — nothing is fetched, ever.
  import { cardScene } from "./lib/cardscenes.js";
  import CardSceneNode from "./CardSceneNode.svelte";

  // `scale` is the same signal the particle engine takes: below 0.7 this is a
  // picker tile, and the parts a scene flagged as fine detail are dropped
  // rather than shrunk into mush.
  let { id = "", color = "", color2 = "", scale = 1 } = $props();

  const uid = `cs${++seq}`;
  const sc = $derived(cardScene(id));
  const full = $derived(scale >= 0.7);
  const vars = $derived(
    `--c1:${color || "var(--accent)"};--c2:${color2 || color || "var(--accent)"}`,
  );
</script>

{#if sc}
  <!-- slice, not meet: the card is 272 wide but its height depends on how much
       profile there is to show, and a scene must fill it rather than letterbox
       into it. YMin, not YMid: every scene puts its subject in the top third,
       so a short card — or a 96×50 picker tile — has to crop from the BOTTOM.
       Centred cropping showed tiles the empty middle band and nothing else. -->
  <svg
    class="cs"
    viewBox="0 0 272 400"
    preserveAspectRatio="xMidYMin slice"
    style={vars}
    aria-hidden="true"
    focusable="false"
  >
    <defs>
      {#each sc.defs || [] as d (d.id)}
        {#if d.t === "lg"}
          <linearGradient
            id="{uid}-{d.id}"
            gradientUnits="userSpaceOnUse"
            x1={d.x1}
            y1={d.y1}
            x2={d.x2}
            y2={d.y2}
          >
            {#each d.stops as s, i (i)}
              <stop offset={s[0]} stop-color={s[1] === "c1" ? "var(--c1)" : s[1] === "c2" ? "var(--c2)" : s[1]} stop-opacity={s[2] ?? 1} />
            {/each}
          </linearGradient>
        {:else if d.t === "rg"}
          <radialGradient id="{uid}-{d.id}" gradientUnits="userSpaceOnUse" cx={d.cx} cy={d.cy} r={d.r}>
            {#each d.stops as s, i (i)}
              <stop offset={s[0]} stop-color={s[1] === "c1" ? "var(--c1)" : s[1] === "c2" ? "var(--c2)" : s[1]} stop-opacity={s[2] ?? 1} />
            {/each}
          </radialGradient>
        {:else if d.t === "rgb"}
          <!-- No gradientUnits: the default is objectBoundingBox, so this one
               fits every shape that references it. -->
          <radialGradient id="{uid}-{d.id}">
            {#each d.stops as s, i (i)}
              <stop offset={s[0]} stop-color={s[1] === "c1" ? "var(--c1)" : s[1] === "c2" ? "var(--c2)" : s[1]} stop-opacity={s[2] ?? 1} />
            {/each}
          </radialGradient>
        {:else if d.t === "blur"}
          <!-- Blur is only ever applied to a STILL element. A blurred layer
               that also moves has to be re-rasterised every frame, which is
               how a decoration turns into a fan. -->
          <filter id="{uid}-{d.id}" x="-40%" y="-40%" width="180%" height="180%">
            <feGaussianBlur stdDeviation={d.std} />
          </filter>
        {/if}
      {/each}
    </defs>
    {#each sc.parts as p, i (i)}
      <CardSceneNode node={p} {uid} {full} />
    {/each}
  </svg>
{/if}

<style>
  .cs {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    display: block;
    pointer-events: none;
  }
</style>
