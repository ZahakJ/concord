<script>
  // Renders a worn decoration around an avatar. See lib/decorations.js for the
  // authoring contract — this file only knows how to draw what that file says.
  //
  // Two SVGs rather than one, because a decoration has to be able to sit BOTH
  // behind the face (wings) and over it (ears breaking the top edge), and the
  // avatar itself is a DOM element between them. One overlay could not do that
  // without compositing the avatar into the SVG, which would lose the image
  // element, the presence dot and the text fallback.
  import { decoration } from "./lib/decorations.js";

  // `preview` overrides the small-size freeze below. A picker is the ONE place
  // motion has to be visible — it is the whole reason to choose one decoration
  // over another — and it shows tens of tiles, not the forty rows of a member
  // list that the freeze exists to protect.
  let { id = "", size = 32, color = "", color2 = "", preview = false } = $props();

  const d = $derived(decoration(id));
  const back = $derived(d ? d.parts.filter((p) => p.z === "back") : []);
  const front = $derived(d ? d.parts.filter((p) => p.z !== "back") : []);

  // Animation is a luxury for something 20 pixels tall: at that size the motion
  // is invisible and a member list would run one timer per row. The threshold
  // is the same reasoning AvatarRing uses for its glow.
  const still = $derived(!preview && size < 40);

  const vars = $derived(
    `--d-c1:${color || "var(--accent)"};--d-c2:${color2 || color || "var(--accent)"};`,
  );

  // Colour tokens resolve here so a decoration never carries a raw colour it
  // did not choose deliberately, and so the wearer's palette flows through.
  function paint(v) {
    if (v === "c1") return "var(--d-c1)";
    if (v === "c2") return "var(--d-c2)";
    if (v === "ink") return "#161a20";
    if (v === "light") return "#f2f5f8";
    return v;
  }

  // A part may name its own animation; otherwise it inherits the decoration's.
  // That is what lets one piece rock on the head while the gems set into it
  // pulse on their own clock.
  const cls = (p) =>
    `p ${p.anim || (d && d.anim) || ""} ${p.a ? "anim" : ""} ${p.o ? "o-" + p.o : ""}`;

  // `pv` is an explicit pivot in viewBox units. Without it a rotation turns a
  // part about its own bounding box, which is right for a gem and wrong for an
  // ear: an ear and its inner shell are two paths that have to swing about the
  // one point where they meet the band, or they come apart mid-flick.
  const styleOf = (p) =>
    p.pv
      ? `transform-box:view-box;transform-origin:${p.pv[0]}px ${p.pv[1]}px`
      : p.glow
        ? `filter:drop-shadow(0 0 ${p.glow / 12}px ${paint(p.stroke || p.fill)})`
        : "";
</script>

{#if d}
  {#each [["back", back], ["front", front]] as [layer, parts] (layer)}
    {#if parts.length}
      <svg
        class="dec {layer}"
        class:still
        class:tilt={d.tilt}
        viewBox="0 0 100 100"
        style={vars}
        aria-hidden="true"
        focusable="false"
      >
        {#each parts as p, i (i)}
          {#if p.el === "ellipse"}
            <ellipse
              class={cls(p)}
              cx={p.attrs.cx}
              cy={p.attrs.cy}
              rx={p.attrs.rx}
              ry={p.attrs.ry}
              fill="none"
              stroke={paint(p.stroke)}
              stroke-width={p.width || 3}
              style={styleOf(p)}
            />
          {:else if p.el === "circle"}
            <circle
              class={cls(p)}
              cx={p.attrs.cx}
              cy={p.attrs.cy}
              r={p.attrs.r}
              fill={paint(p.fill)}
              style={styleOf(p)}
            />
          {:else}
            <path class={cls(p)} d={p.d} fill={paint(p.fill)} style={styleOf(p)} />
          {/if}
        {/each}
      </svg>
    {/if}
  {/each}
{/if}

<style>
  .dec {
    position: absolute;
    /* The figure is authored against a 100-unit box whose middle 72 units are
       the avatar, so the overlay is sized past the avatar to leave room for
       what sticks out. */
    inset: -19%;
    width: 138%;
    height: 138%;
    pointer-events: none;
    overflow: visible;
  }
  .dec.back {
    z-index: 0;
  }
  .dec.front {
    z-index: 3;
  }
  .p {
    transform-box: fill-box;
    transform-origin: center;
  }

  /* ── motion ──────────────────────────────────────────────────────────────
     Each named animation is referenced by `anim` in lib/decorations.js, either
     on the decoration or on one part of it. Parts opt in with `a: true`; `o`
     picks a side or a place in a queue, so a pair can move in opposition and a
     row can move as a wave instead of in lockstep. That difference is what
     reads as alive rather than mechanical.

     Rotations pivot at the bottom of the part by default, because almost
     everything that rotates here is rooted on the arch and swings from where it
     is attached. A part that needs a shared pivot (an ear and its inner shell)
     carries `pv` and gets an inline transform-origin instead. */
  .anim.twitch {
    transform-origin: bottom center;
    animation: dec-twitch 4.5s ease-in-out infinite;
  }
  .anim.twitch.o-r {
    animation-delay: 0.5s;
  }
  @keyframes dec-twitch {
    0%, 82%, 100% { transform: rotate(0deg); }
    86% { transform: rotate(-7deg); }
    91% { transform: rotate(5deg); }
    95% { transform: rotate(-2deg); }
  }

  /* A continuous version of the same swing, for ears and horns that never
     stop moving rather than flicking once in a while. */
  .anim.wag {
    transform-origin: bottom center;
    animation: dec-wag 3.8s ease-in-out infinite;
  }
  .anim.wag.o-r {
    animation-name: dec-wag-r;
  }
  @keyframes dec-wag {
    0%, 100% { transform: rotate(-5deg); }
    50% { transform: rotate(5deg); }
  }
  @keyframes dec-wag-r {
    0%, 100% { transform: rotate(5deg); }
    50% { transform: rotate(-5deg); }
  }

  /* Hanging things swing from the point they hang by. */
  .anim.dangle {
    transform-origin: top center;
    animation: dec-dangle 3.4s ease-in-out infinite;
  }
  .anim.dangle.o-r {
    animation-direction: reverse;
  }
  @keyframes dec-dangle {
    0%, 100% { transform: rotate(-8deg); }
    50% { transform: rotate(8deg); }
  }

  /* Gems, beads, blossoms: a breath rather than a blink. */
  .anim.pulse {
    animation: dec-pulse 2.4s ease-in-out infinite;
  }
  @keyframes dec-pulse {
    0%, 100% { transform: scale(1); opacity: 0.82; }
    50% { transform: scale(1.22); opacity: 1; }
  }

  /* Anything that is more light than object. */
  .anim.shimmer {
    animation: dec-shimmer 2.9s ease-in-out infinite;
  }
  @keyframes dec-shimmer {
    0%, 100% { opacity: 0.4; }
    50% { opacity: 1; }
  }

  /* Sparks and motes: born low and dim, gone by the top of the cycle. */
  .anim.drift {
    animation: dec-drift 3.8s ease-in-out infinite;
  }
  @keyframes dec-drift {
    0% { transform: translate(0, 2px) scale(0.5); opacity: 0; }
    25% { opacity: 1; }
    100% { transform: translate(2px, -9px) scale(1.05); opacity: 0; }
  }

  /* Electricity does not ease. */
  .anim.zap {
    animation: dec-zap 2.8s linear infinite;
  }
  @keyframes dec-zap {
    0%, 40% { opacity: 1; }
    43% { opacity: 0.12; }
    46% { opacity: 1; }
    50% { opacity: 0.25; }
    53%, 100% { opacity: 1; }
  }

  /* A swell passing through, for water and cloth. */
  .anim.wave {
    animation: dec-wave 3.2s ease-in-out infinite;
  }
  @keyframes dec-wave {
    0%, 100% { transform: translateY(0) scaleX(1); }
    50% { transform: translateY(-2px) scaleX(1.05); }
  }

  /* spin's impatient sibling: same shared pivot, orbit-speed rather than
     filigree-speed, for comets and whipping wind. */
  .anim.whirl {
    transform-box: view-box;
    transform-origin: 50px 50px;
    animation: dec-whirl 14s linear infinite;
  }
  .anim.whirl.o-r {
    animation-direction: reverse;
    animation-duration: 19s;
  }
  @keyframes dec-whirl {
    to {
      transform: rotate(360deg);
    }
  }

  .anim.float {
    animation: dec-float 3.6s ease-in-out infinite;
  }
  @keyframes dec-float {
    0%, 100% { transform: translateY(0); }
    50% { transform: translateY(-2.5px); }
  }

  .anim.flap {
    animation: dec-flap 2.4s ease-in-out infinite;
  }
  .anim.flap.o-r {
    animation-name: dec-flap-r;
  }
  @keyframes dec-flap {
    0%, 100% { transform: rotate(0deg) translateX(0); }
    50% { transform: rotate(-11deg) translateX(-1px); }
  }
  @keyframes dec-flap-r {
    0%, 100% { transform: rotate(0deg) translateX(0); }
    50% { transform: rotate(11deg) translateX(1px); }
  }

  .anim.flicker {
    animation: dec-flicker 1.5s ease-in-out infinite;
  }
  @keyframes dec-flicker {
    0%, 100% { transform: scaleY(1) translateY(0); opacity: 1; }
    35% { transform: scaleY(1.08) translateY(-1px); opacity: 0.9; }
    70% { transform: scaleY(0.95) translateY(0.5px); opacity: 1; }
  }

  /* Two jaws that have to meet: the upper drops, the lower rises, on the one
     clock, so the bite closes instead of one half sliding past the other. */
  .anim.chomp {
    animation: dec-chomp 5s ease-in-out infinite;
  }
  .anim.chomp.o-r {
    animation-name: dec-chomp-r;
  }
  @keyframes dec-chomp {
    0%, 70%, 100% { transform: translateY(0); }
    78% { transform: translateY(7px); }
    86% { transform: translateY(1px); }
  }
  @keyframes dec-chomp-r {
    0%, 70%, 100% { transform: translateY(0); }
    78% { transform: translateY(-7px); }
    86% { transform: translateY(-1px); }
  }

  /* The worn rings ask for this one: a slow rotation about the avatar's centre, for
     rings of runes, orbiting gems and turning filigree. transform-origin is
     overridden because the default (the part's own box) would spin each
     fragment on the spot instead of carrying it around the circle. Linear, not
     eased — an eased rotation reads as hesitant. */
  .anim.spin {
    transform-box: view-box;
    transform-origin: 50px 50px;
    animation: dec-spin 26s linear infinite;
  }
  .anim.spin.o-r {
    animation-direction: reverse;
    animation-duration: 34s;
  }
  @keyframes dec-spin {
    to {
      transform: rotate(360deg);
    }
  }

  .anim.sway {
    animation: dec-sway 5s ease-in-out infinite;
  }
  @keyframes dec-sway {
    0%, 100% { transform: rotate(-4deg); }
    50% { transform: rotate(4deg); }
  }

  /* A queue rather than a chorus. `o: 1..8` offsets a part into the cycle that
     is already running, so a row of blossoms opens in a ripple and a row of
     flames never all lean the same way. Negative delays mean the offset costs
     no dead time at mount. The two-class selectors below are deliberately
     weaker than the per-animation `.o-r` rules, which set a direction or a
     mirrored keyframe and must win. */
  /* The right-hand half of a pair, for the animations that have no mirrored
     keyframes of their own: half a cycle behind, so the two sides breathe in
     opposition. Deliberately not applied to chomp or wag, where `o-r` already
     means "the other jaw" or "the other ear" and a delay would break the
     movement they are timed against. */
  .anim.pulse.o-r,
  .anim.shimmer.o-r,
  .anim.flicker.o-r,
  .anim.float.o-r,
  .anim.sway.o-r,
  .anim.drift.o-r {
    animation-delay: -1.2s;
  }

  .anim.o-1 { animation-delay: -0.2s; }
  .anim.o-2 { animation-delay: -0.4s; }
  .anim.o-3 { animation-delay: -0.6s; }
  .anim.o-4 { animation-delay: -0.8s; }
  .anim.o-5 { animation-delay: -1s; }
  .anim.o-6 { animation-delay: -1.2s; }
  .anim.o-7 { animation-delay: -1.4s; }
  .anim.o-8 { animation-delay: -1.6s; }

  /* The whole piece rocking on the head, which no per-part transform can do:
     the parts keep their own motion and the arch they are mounted on tips as
     one object. Both layers carry it and start together, so front and back
     stay registered. */
  .dec.tilt {
    transform-origin: 50% 50%;
    animation: dec-tilt 5.5s ease-in-out infinite;
  }
  @keyframes dec-tilt {
    0%, 100% { transform: rotate(-3deg); }
    50% { transform: rotate(3deg); }
  }

  /* Small avatars and anyone who asked for less motion get the drawing, still.
     The decoration is the point; the movement is the garnish. */
  .dec.still .p,
  .p {
    will-change: transform;
  }
  .dec.still,
  .dec.still .anim {
    animation: none !important;
  }
  @media (prefers-reduced-motion: reduce) {
    .dec,
    .anim {
      animation: none !important;
    }
  }
</style>
