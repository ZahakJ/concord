<script>
  // Paints a scenic frame around the profile card. See lib/cardframes.js for
  // the authoring contract — this file only knows how to draw what that file
  // says, and owns the animation vocabulary those frames name.
  //
  // Two SVGs, not one, for the same reason AvatarDecoration uses two: a frame
  // has to be able to stand BEHIND the card (a tower rising out of the top
  // edge, a moon cut by it) as well as over it (battlements on the brow, a
  // cobweb in a corner), and the card itself is a DOM element between them.
  //
  // Neither SVG clips. They are stretched over the card's own box — x maps 1:1
  // onto its 272px width, y onto whatever height the content gave it — and the
  // art is free to run outside that box. The overhang is the whole effect.
  import { cardFrame } from "./lib/cardframes.js";

  let { id = "", color = "", color2 = "" } = $props();

  const f = $derived(cardFrame(id));
  const back = $derived(f ? f.parts.filter((p) => p.z === "back") : []);
  const front = $derived(f ? f.parts.filter((p) => p.z !== "back") : []);
  // The gradients go in whichever layer is actually drawn first — a frame with
  // no back parts (a proscenium, say) has no back <svg> to hang them off.
  const defsIn = $derived(back.length ? "back" : "front");

  const vars = $derived(
    `--cf-c1:${color || "var(--accent)"};--cf-c2:${color2 || color || "var(--accent)"};`,
  );

  // Colour tokens resolve here, so a frame never carries a raw colour it did
  // not choose deliberately and the wearer's palette flows through it.
  // "@name" reaches for one of the frame's own gradients.
  function paint(v) {
    if (!v) return "none";
    if (v === "c1") return "var(--cf-c1)";
    if (v === "c2") return "var(--cf-c2)";
    if (v === "ink") return "#161a20";
    if (v === "light") return "#f2f5f8";
    if (v[0] === "@") return `url(#cf-${id}-${v.slice(1)})`;
    return v;
  }

  // transform-origin and animation-delay are per-part values in viewBox units;
  // there is no way to express them as classes without one class per part.
  function partStyle(p) {
    let s = "";
    if (p.or) s += `transform-origin:${p.or};`;
    if (p.dl) s += `animation-delay:${p.dl}s;`;
    if (p.op != null) s += `opacity:${p.op};`;
    return s;
  }
</script>

{#if f}
  {#each [["back", back], ["front", front]] as [layer, parts] (layer)}
    {#if parts.length}
      <svg
        class="cf {layer}"
        viewBox="0 0 272 400"
        preserveAspectRatio="none"
        style={vars}
        aria-hidden="true"
        focusable="false"
      >
        {#if layer === defsIn && f.grads}
          <defs>
            {#each f.grads as g (g.id)}
              {#if g.r != null}
                <radialGradient
                  id="cf-{id}-{g.id}"
                  gradientUnits={g.bb ? "objectBoundingBox" : "userSpaceOnUse"}
                  cx={g.cx}
                  cy={g.cy}
                  r={g.r}
                >
                  {#each g.stops as [o, c, a], i (i)}
                    <stop offset={o} stop-color={paint(c)} stop-opacity={a ?? 1} />
                  {/each}
                </radialGradient>
              {:else}
                <!-- `bb` fits the gradient to the SHAPE rather than to the
                     box, which is the only way to shade a cylinder: one
                     definition then rounds off every column, trunk, pillar and
                     curtain fold in the frame, wherever each one stands. A
                     user-space gradient can only ever light one place
                     correctly, so the second column got the first one's
                     shading and both read flat. -->
                <linearGradient
                  id="cf-{id}-{g.id}"
                  gradientUnits={g.bb ? "objectBoundingBox" : "userSpaceOnUse"}
                  x1={g.x1}
                  y1={g.y1}
                  x2={g.x2}
                  y2={g.y2}
                >
                  {#each g.stops as [o, c, a], i (i)}
                    <stop offset={o} stop-color={paint(c)} stop-opacity={a ?? 1} />
                  {/each}
                </linearGradient>
              {/if}
            {/each}
          </defs>
        {/if}
        {#each parts as p, i (i)}
          {#if p.stroke}
            <path
              class="cfp {p.a || ''}"
              d={p.d}
              fill="none"
              stroke={paint(p.stroke)}
              stroke-width={p.sw || 2}
              stroke-linecap="round"
              stroke-linejoin="round"
              style={partStyle(p)}
            />
          {:else}
            <path class="cfp {p.a || ''}" d={p.d} fill={paint(p.fill)} style={partStyle(p)} />
          {/if}
        {/each}
      </svg>
    {/if}
  {/each}
{/if}

<style>
  .cf {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    /* The art runs outside the card on purpose; nothing here may clip it. */
    overflow: visible;
    pointer-events: none;
  }
  .cf.back {
    z-index: 0;
  }
  .cf.front {
    z-index: 3;
  }
  .cfp {
    /* view-box, not fill-box: every transform-origin in the library is written
       in the frame's own coordinates, so a branch rotates about where it joins
       the trunk instead of about its own bounding box. */
    transform-box: view-box;
    transform-origin: 136px 200px;
  }

  /* ── motion ───────────────────────────────────────────────────────────────
     Named by `a` in lib/cardframes.js. Every one of these has to survive being
     stretched vertically (the card's height is content-driven), so they are
     rotations, opacity and gentle translation rather than anything that
     depends on a part's exact pixel size. */

  .sway {
    animation: cf-sway 6s ease-in-out infinite alternate;
  }
  @keyframes cf-sway {
    from { transform: rotate(-3deg); }
    to { transform: rotate(3deg); }
  }
  .sway-slow {
    animation: cf-sway-slow 9.5s ease-in-out infinite alternate;
  }
  @keyframes cf-sway-slow {
    from { transform: rotate(-2deg); }
    to { transform: rotate(2deg); }
  }
  .breeze {
    animation: cf-breeze 8s ease-in-out infinite alternate;
  }
  @keyframes cf-breeze {
    from { transform: translateX(-4px) rotate(-1deg); }
    to { transform: translateX(4px) rotate(1deg); }
  }
  .breeze-slow {
    animation: cf-breeze-slow 13s ease-in-out infinite alternate;
  }
  @keyframes cf-breeze-slow {
    from { transform: translateX(3px) rotate(0.6deg); }
    to { transform: translateX(-3px) rotate(-0.6deg); }
  }
  .bough {
    animation: cf-bough 5.5s ease-in-out infinite alternate;
  }
  @keyframes cf-bough {
    from { transform: rotate(-2.4deg); }
    to { transform: rotate(2.4deg); }
  }
  .bough-slow {
    animation: cf-bough 8.4s ease-in-out infinite alternate;
  }
  .kelp {
    animation: cf-kelp 7s ease-in-out infinite alternate;
  }
  @keyframes cf-kelp {
    from { transform: rotate(-4.5deg); }
    to { transform: rotate(4.5deg); }
  }
  .kelp-slow {
    animation: cf-kelp 10.5s ease-in-out infinite alternate;
  }
  .swing {
    animation: cf-swing 4.4s ease-in-out infinite alternate;
  }
  @keyframes cf-swing {
    from { transform: rotate(-6deg); }
    to { transform: rotate(6deg); }
  }
  .breathe {
    animation: cf-breathe 7s ease-in-out infinite alternate;
  }
  @keyframes cf-breathe {
    from { transform: scaleX(1); }
    to { transform: scaleX(1.04); }
  }
  .wave-flag {
    animation: cf-flag 2.1s ease-in-out infinite;
  }
  @keyframes cf-flag {
    0%, 100% { transform: skewY(0deg) scaleY(1); }
    35% { transform: skewY(-7deg) scaleY(0.92); }
    70% { transform: skewY(5deg) scaleY(1.04); }
  }

  /* Fire and light. */
  .flick {
    animation: cf-flick 1.05s ease-in-out infinite;
  }
  @keyframes cf-flick {
    0%, 100% { transform: scaleY(1) scaleX(1); opacity: 1; }
    30% { transform: scaleY(1.16) scaleX(0.94); opacity: 0.86; }
    55% { transform: scaleY(0.93) scaleX(1.06); opacity: 1; }
    78% { transform: scaleY(1.08) scaleX(0.97); opacity: 0.92; }
  }
  .glow {
    animation: cf-glow 3.4s ease-in-out infinite alternate;
  }
  @keyframes cf-glow {
    from { opacity: 0.42; }
    to { opacity: 1; }
  }
  .twinkle {
    animation: cf-twinkle 2.6s ease-in-out infinite alternate;
  }
  @keyframes cf-twinkle {
    from { opacity: 0.08; }
    to { opacity: 0.85; }
  }
  .coals {
    animation: cf-coals 4.2s ease-in-out infinite alternate;
  }
  @keyframes cf-coals {
    from { opacity: 0.4; }
    to { opacity: 1; }
  }
  .beam {
    animation: cf-beam 9s ease-in-out infinite alternate;
  }
  @keyframes cf-beam {
    from { transform: rotate(-15deg); }
    to { transform: rotate(15deg); }
  }

  /* Water and air. */
  .lap {
    animation: cf-lap 6.5s ease-in-out infinite alternate;
  }
  @keyframes cf-lap {
    from { transform: translateX(-9px); }
    to { transform: translateX(9px); }
  }
  .lap-far {
    animation: cf-lap-far 9.5s ease-in-out infinite alternate;
  }
  @keyframes cf-lap-far {
    from { transform: translateX(7px); }
    to { transform: translateX(-7px); }
  }
  .foam {
    animation: cf-foam 5s ease-in-out infinite alternate;
  }
  @keyframes cf-foam {
    from { transform: scale(0.82) translateY(3px); opacity: 0.35; }
    to { transform: scale(1.12) translateY(-2px); opacity: 0.8; }
  }
  .anemone {
    animation: cf-anemone 4.6s ease-in-out infinite alternate;
  }
  @keyframes cf-anemone {
    from { transform: scale(0.94) rotate(-2deg); }
    to { transform: scale(1.06) rotate(2deg); }
  }
  .aurora {
    animation: cf-aurora 15s ease-in-out infinite alternate;
  }
  @keyframes cf-aurora {
    from { transform: translateX(-16px) scaleY(0.92); }
    to { transform: translateX(16px) scaleY(1.12); }
  }
  .aurora-slow {
    animation: cf-aurora-slow 23s ease-in-out infinite alternate;
  }
  @keyframes cf-aurora-slow {
    from { transform: translateX(11px) scaleY(1.06); }
    to { transform: translateX(-11px) scaleY(0.95); }
  }
  .shimmer {
    animation: cf-shimmer 9s ease-in-out infinite alternate;
  }
  @keyframes cf-shimmer {
    from { transform: translateX(-7px); opacity: 0.3; }
    to { transform: translateX(7px); opacity: 0.7; }
  }
  .heat {
    animation: cf-heat 5.2s ease-in-out infinite alternate;
  }
  @keyframes cf-heat {
    from { transform: skewX(-1.8deg) translateY(1px); opacity: 0.3; }
    to { transform: skewX(1.8deg) translateY(-1px); opacity: 0.65; }
  }

  /* Things that travel: they run the whole width, or the whole height, on a
     linear clock, and are staggered by animation-delay in the library. */
  .cross {
    animation: cf-cross 17s linear infinite;
  }
  @keyframes cf-cross {
    0% { transform: translate(-72px, -54px) scaleY(1); }
    25% { transform: translate(34px, -68px) scaleY(0.66); }
    50% { transform: translate(140px, -46px) scaleY(1); }
    75% { transform: translate(246px, -64px) scaleY(0.66); }
    100% { transform: translate(352px, -50px) scaleY(1); }
  }
  .cross-high {
    animation: cf-cross-high 21s linear infinite;
  }
  @keyframes cf-cross-high {
    0% { transform: translate(-72px, -84px) scaleY(1); }
    30% { transform: translate(54px, -96px) scaleY(0.7); }
    60% { transform: translate(190px, -78px) scaleY(1); }
    100% { transform: translate(352px, -90px) scaleY(0.8); }
  }
  .cross-low {
    animation: cf-cross-low 24s linear infinite;
  }
  @keyframes cf-cross-low {
    0% { transform: translate(-60px, 34px) scaleY(1); }
    35% { transform: translate(56px, 26px) scaleY(0.94); }
    70% { transform: translate(190px, 42px) scaleY(1.06); }
    100% { transform: translate(340px, 30px) scaleY(1); }
  }
  .bubble {
    animation: cf-bubble 9s linear infinite;
  }
  @keyframes cf-bubble {
    0% { transform: translate(0, 0); opacity: 0; }
    12% { opacity: 0.55; }
    88% { opacity: 0.45; }
    100% { transform: translate(6px, -300px); opacity: 0; }
  }
  .float-up {
    animation: cf-float-up 8s linear infinite;
  }
  @keyframes cf-float-up {
    0% { transform: translate(0, 0) scale(0.6); opacity: 0; }
    18% { opacity: 0.9; }
    100% { transform: translate(-7px, -120px) scale(1.1); opacity: 0; }
  }
  .petal {
    animation: cf-petal 11s linear infinite;
  }
  @keyframes cf-petal {
    0% { transform: translate(0, 0) rotate(0deg); opacity: 0; }
    10% { opacity: 0.95; }
    90% { opacity: 0.6; }
    100% { transform: translate(16px, 300px) rotate(260deg); opacity: 0; }
  }
  .hover {
    animation: cf-hover 9.5s ease-in-out infinite;
  }
  @keyframes cf-hover {
    0%, 100% { transform: translate(0, 0); opacity: 0.2; }
    25% { transform: translate(-9px, -16px); opacity: 1; }
    50% { transform: translate(7px, -30px); opacity: 0.4; }
    75% { transform: translate(-4px, -14px); opacity: 0.9; }
  }
  .abseil {
    animation: cf-abseil 6.5s ease-in-out infinite alternate;
  }
  @keyframes cf-abseil {
    from { transform: translateY(-18px); }
    to { transform: translateY(14px); }
  }
  .abseil-line {
    animation: cf-abseil-line 6.5s ease-in-out infinite alternate;
  }
  @keyframes cf-abseil-line {
    from { transform: scaleY(0.7); }
    to { transform: scaleY(1.24); }
  }

  /* The drawing is the thing; the movement is the garnish. */
  @media (prefers-reduced-motion: reduce) {
    .cfp {
      animation: none !important;
    }
  }
</style>
