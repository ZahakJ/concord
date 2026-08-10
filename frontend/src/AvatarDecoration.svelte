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
  import { drawnFrame } from "./lib/frames.js";

  // kind picks which library the id is resolved against. Both are authored to
  // the same contract and painted identically; only the picker distinguishes
  // "worn on the avatar" from "encircling it".
  let { id = "", kind = "decoration", size = 32, color = "", color2 = "" } = $props();

  const d = $derived(kind === "frame" ? drawnFrame(id) : decoration(id));
  const back = $derived(d ? d.parts.filter((p) => p.z === "back") : []);
  const front = $derived(d ? d.parts.filter((p) => p.z !== "back") : []);

  // Animation is a luxury for something 20 pixels tall: at that size the motion
  // is invisible and a member list would run one timer per row. The threshold
  // is the same reasoning AvatarRing uses for its glow.
  const still = $derived(size < 40);

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
</script>

{#if d}
  {#each [["back", back], ["front", front]] as [layer, parts] (layer)}
    {#if parts.length}
      <svg
        class="dec {layer}"
        class:still
        viewBox="0 0 100 100"
        style={vars}
        aria-hidden="true"
        focusable="false"
      >
        {#each parts as p, i (i)}
          {#if p.el === "ellipse"}
            <ellipse
              class="p {d.anim || ''} {p.a ? 'anim' : ''}"
              cx={p.attrs.cx}
              cy={p.attrs.cy}
              rx={p.attrs.rx}
              ry={p.attrs.ry}
              fill="none"
              stroke={paint(p.stroke)}
              stroke-width={p.width || 3}
              style={p.glow ? `filter:drop-shadow(0 0 ${p.glow / 12}px ${paint(p.stroke)})` : ""}
            />
          {:else if p.el === "circle"}
            <circle
              class="p {d.anim || ''} {p.a ? 'anim' : ''}"
              cx={p.attrs.cx}
              cy={p.attrs.cy}
              r={p.attrs.r}
              fill={paint(p.fill)}
            />
          {:else}
            <path
              class="p {d.anim || ''} {p.a ? 'anim' : ''} {p.o ? 'o-' + p.o : ''}"
              d={p.d}
              fill={paint(p.fill)}
            />
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
     Each named animation is referenced by `anim` in lib/decorations.js. Parts
     opt in with `a: true`; `o` picks a side so a pair can move in opposition
     instead of in lockstep, which is the difference between alive and mechanical. */
  .anim.twitch {
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

  .anim.chomp {
    animation: dec-chomp 5s ease-in-out infinite;
  }
  @keyframes dec-chomp {
    0%, 70%, 100% { transform: translateY(0); }
    78% { transform: translateY(-9px); }
    86% { transform: translateY(-1px); }
  }

  /* Frames ask for this one: a slow rotation about the avatar's centre, for
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

  /* Small avatars and anyone who asked for less motion get the drawing, still.
     The decoration is the point; the movement is the garnish. */
  .dec.still .p,
  .p {
    will-change: transform;
  }
  .dec.still .anim {
    animation: none !important;
  }
  @media (prefers-reduced-motion: reduce) {
    .anim {
      animation: none !important;
    }
  }
</style>
