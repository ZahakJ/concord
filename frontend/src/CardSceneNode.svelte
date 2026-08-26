<script>
  // One node of a drawn scene, and the whole motion vocabulary.
  //
  // The component recurses into itself for groups, because a scene's motion
  // COMPOUNDS: a twig on a bough on a trunk has to inherit the two rotations
  // above it or the tree reads as a cut-out swinging on a pin. Flattening the
  // tree into a list would lose exactly that.
  //
  // Every keyframe here moves `transform` or `opacity` and nothing else, and
  // every one of them lives on a REAL element. A transform animation on a
  // ::before/::after is not composited in Chromium — it repaints the whole
  // layer, measured at 85× the cost elsewhere in this app — so the painter has
  // no pseudo-elements at all.
  import Self from "./CardSceneNode.svelte";

  let { node, uid = "cs", full = true } = $props();

  // A scene's colour is either its own (a nebula is not the wearer's teal) or
  // the wearer's palette, chosen per shape. `@name` reaches a gradient or
  // filter in this instance's <defs>.
  function paint(v) {
    if (!v) return undefined;
    if (v === "c1") return "var(--c1)";
    if (v === "c2") return "var(--c2)";
    if (v[0] === "@") return `url(#${uid}-${v.slice(1)})`;
    return v;
  }

  // Animation dials travel as custom properties so one keyframe serves a whole
  // field of elements at different speeds, phases and amplitudes. A NEGATIVE
  // delay starts an element part-way through its cycle, which is what stops a
  // dozen snowflakes falling in lockstep.
  function styleOf(n) {
    let s = "";
    if (n.origin) s += `transform-origin:${n.origin[0]}px ${n.origin[1]}px;`;
    if (n.amp != null) s += `--amp:${n.amp}deg;`;
    if (n.dur != null) s += `--dur:${n.dur}s;`;
    if (n.dl != null) s += `--dl:${n.dl}s;`;
    if (n.tx != null) s += `--tx:${n.tx}px;`;
    if (n.ty != null) s += `--ty:${n.ty}px;`;
    if (n.x0 != null) s += `--x0:${n.x0}px;`;
    if (n.x1 != null) s += `--x1:${n.x1}px;`;
    if (n.a != null) s += `--a:${n.a};`;
    if (n.sc != null) s += `--sc:${n.sc};`;
    if (n.filter) s += `filter:url(#${uid}-${n.filter});`;
    return s || undefined;
  }

  // The two AMBIENT motions — a frond rocking, a star breathing — are what a
  // scene has most of and what a 96×62 thumbnail shows least of. A picker
  // holds a dozen scenes at once, and every animated element costs a style
  // recalc per frame whatever its size, so at tile scale those two are drawn
  // still and the scene's headline motion (a wraith crossing, a beam turning,
  // a canopy in a gust) is what animates.
  const QUIET = new Set(["sway", "twinkle"]);
  const cls = $derived(
    `n ${
      full
        ? node.cls || ""
        : (node.cls || "")
            .split(" ")
            .filter((c) => c && !QUIET.has(c))
            .join(" ")
    }`,
  );
  const st = $derived(styleOf(node));
  // A node carrying `tr` must not also carry `cls`: the CSS transform property
  // wins outright over the transform ATTRIBUTE, so an animated node would
  // silently drop its static placement. Scenes wrap instead of combining.
  const tr = $derived(node.tr || undefined);
  const op = $derived(node.o ?? undefined);
</script>

{#if full || !node.hi}
  {#if node.el === "g"}
    <g class={cls} style={st} transform={tr} opacity={op}>
      {#each node.children as c, i (i)}
        <Self node={c} {uid} {full} />
      {/each}
    </g>
  {:else if node.el === "rect"}
    <rect
      class={cls}
      style={st}
      transform={tr}
      x={node.x}
      y={node.y}
      width={node.w}
      height={node.h}
      rx={node.rx}
      fill={paint(node.fill)}
      opacity={op}
    />
  {:else if node.el === "circle"}
    <circle
      class={cls}
      style={st}
      transform={tr}
      cx={node.cx}
      cy={node.cy}
      r={node.r}
      fill={paint(node.fill)}
      opacity={op}
    />
  {:else if node.el === "ellipse"}
    <ellipse
      class={cls}
      style={st}
      transform={tr}
      cx={node.cx}
      cy={node.cy}
      rx={node.rx}
      ry={node.ry}
      fill={paint(node.fill)}
      opacity={op}
    />
  {:else}
    <path
      class={cls}
      style={st}
      transform={tr}
      d={node.d}
      fill={node.fill ? paint(node.fill) : "none"}
      stroke={node.stroke ? paint(node.stroke) : undefined}
      stroke-width={node.sw}
      stroke-linecap={node.cap}
      stroke-linejoin={node.cap ? "round" : undefined}
      opacity={op}
    />
  {/if}
{/if}

<style>
  /* SVG elements default to `transform-origin: 0 0`, which would rotate every
     bough about the top-left corner of the card. Pinning transform-box to the
     view box lets a scene name its own pivot in the same coordinates it drew
     the shape in. */
  .n {
    transform-box: view-box;
  }

  /* ── wind ─────────────────────────────────────────────────────────────
     `sway` is a metronome — kelp, fronds, reeds under water or still air.
     `gust` is weather: mostly at rest, then a shove that overshoots and
     settles in two decaying swings. It is the difference between a tree that
     is being blown and a tree on a pendulum. */
  .sway {
    animation: cs-sway var(--dur, 7s) ease-in-out var(--dl, 0s) infinite;
  }
  @keyframes cs-sway {
    0%,
    100% {
      transform: rotate(calc(var(--amp, 3deg) * -1));
    }
    50% {
      transform: rotate(var(--amp, 3deg));
    }
  }

  .gust {
    animation: cs-gust var(--dur, 9s) ease-in-out var(--dl, 0s) infinite;
  }
  @keyframes cs-gust {
    0%,
    100% {
      transform: rotate(0deg);
    }
    16% {
      transform: rotate(var(--amp, 3deg));
    }
    31% {
      transform: rotate(calc(var(--amp, 3deg) * -0.5));
    }
    46% {
      transform: rotate(calc(var(--amp, 3deg) * 0.72));
    }
    62% {
      transform: rotate(calc(var(--amp, 3deg) * -0.24));
    }
    78% {
      transform: rotate(calc(var(--amp, 3deg) * 0.3));
    }
  }

  /* ── travel ───────────────────────────────────────────────────────────
     `cross` walks something the width of the card and fades it at both ends,
     so nothing ever pops in at an edge. `shift` is a there-and-back slide for
     mist and water, wide enough that no seam is ever on screen. */
  .cross {
    animation: cs-cross var(--dur, 24s) linear var(--dl, 0s) infinite;
  }
  @keyframes cs-cross {
    0% {
      transform: translate3d(var(--x0, -120px), 0, 0);
      opacity: 0;
    }
    14%,
    82% {
      opacity: var(--a, 0.5);
    }
    100% {
      transform: translate3d(var(--x1, 240px), 0, 0);
      opacity: 0;
    }
  }

  .shift {
    animation: cs-shift var(--dur, 26s) ease-in-out var(--dl, 0s) infinite;
  }
  @keyframes cs-shift {
    0%,
    100% {
      transform: translate3d(calc(var(--tx, 20px) * -1), 0, 0);
    }
    50% {
      transform: translate3d(var(--tx, 20px), 0, 0);
    }
  }

  .bob {
    animation: cs-bob var(--dur, 6s) ease-in-out var(--dl, 0s) infinite;
  }
  @keyframes cs-bob {
    0%,
    100% {
      transform: translate3d(0, calc(var(--ty, 4px) * -1), 0);
    }
    50% {
      transform: translate3d(0, var(--ty, 4px), 0);
    }
  }

  /* Falling and rising fields: snow, ash, bubbles, sparks, running droplets.
     Both fade at the ends of the run so a particle never blinks out mid-card,
     and both take a sideways component so nothing travels dead straight. */
  .fall {
    animation: cs-fall var(--dur, 9s) linear var(--dl, 0s) infinite;
  }
  @keyframes cs-fall {
    0% {
      transform: translate3d(0, 0, 0);
      opacity: 0;
    }
    12%,
    72% {
      opacity: var(--a, 0.6);
    }
    100% {
      transform: translate3d(var(--tx, 12px), var(--ty, 130px), 0);
      opacity: 0;
    }
  }

  .rise {
    animation: cs-rise var(--dur, 9s) linear var(--dl, 0s) infinite;
  }
  @keyframes cs-rise {
    0% {
      transform: translate3d(0, 0, 0);
      opacity: 0;
    }
    18%,
    70% {
      opacity: var(--a, 0.5);
    }
    100% {
      transform: translate3d(var(--tx, 8px), var(--ty, -110px), 0);
      opacity: 0;
    }
  }

  /* ── light ────────────────────────────────────────────────────────────
     `twinkle` is a star; `flicker` is a lit window seen from far off, which
     is not a sine wave — it is mostly on, with brief dips as someone crosses
     the room. `breathe` is a halo. `pulse` opens a ring outward. */
  .twinkle {
    animation: cs-twinkle var(--dur, 5s) ease-in-out var(--dl, 0s) infinite;
  }
  @keyframes cs-twinkle {
    0%,
    100% {
      opacity: calc(var(--a, 0.7) * 0.25);
    }
    50% {
      opacity: var(--a, 0.7);
    }
  }

  .flicker {
    animation: cs-flicker var(--dur, 7s) steps(1, end) var(--dl, 0s) infinite;
  }
  @keyframes cs-flicker {
    0%,
    41%,
    47%,
    88%,
    100% {
      opacity: var(--a, 0.8);
    }
    43%,
    92% {
      opacity: calc(var(--a, 0.8) * 0.12);
    }
  }

  .breathe {
    animation: cs-breathe var(--dur, 8s) ease-in-out var(--dl, 0s) infinite;
  }
  @keyframes cs-breathe {
    0%,
    100% {
      opacity: calc(var(--a, 0.6) * 0.55);
    }
    50% {
      opacity: var(--a, 0.6);
    }
  }

  .pulse {
    animation: cs-pulse var(--dur, 7s) var(--ease-out) var(--dl, 0s) infinite;
  }
  @keyframes cs-pulse {
    0% {
      transform: scale(0.15);
      opacity: 0;
    }
    18% {
      opacity: var(--a, 0.4);
    }
    100% {
      transform: scale(var(--sc, 1));
      opacity: 0;
    }
  }

  /* ── rotation ─────────────────────────────────────────────────────────
     Linear, always: an eased rotation reads as hesitant rather than orbital.
     `spin` carries moons and coronas; `sweep` is a lighthouse, whose beam is
     the same rotation with the lamp's own flare kept in phase. */
  .spin,
  .sweep {
    animation: cs-spin var(--dur, 20s) linear var(--dl, 0s) infinite;
  }
  @keyframes cs-spin {
    to {
      transform: rotate(360deg);
    }
  }

  /* A shimmering surface — water catching light, condensation on glass. Wide
     opacity swing, long period, so it never resolves into a blink. */
  .shimmer {
    animation: cs-shimmer var(--dur, 11s) ease-in-out var(--dl, 0s) infinite;
  }
  @keyframes cs-shimmer {
    0%,
    100% {
      opacity: calc(var(--a, 0.5) * 0.3);
      transform: translate3d(calc(var(--tx, 8px) * -1), 0, 0);
    }
    50% {
      opacity: var(--a, 0.5);
      transform: translate3d(var(--tx, 8px), 0, 0);
    }
  }

  /* The picture is the point; the movement is the garnish. Stopping the
     animations leaves every scene in its authored resting frame — which is
     why nothing is authored off-canvas or at zero opacity. */
  @media (prefers-reduced-motion: reduce) {
    .n {
      animation: none !important;
    }
  }
</style>
