<script module>
  // Gradient and filter ids have to be unique per PAINTED INSTANCE, not per
  // decoration: `url(#id)` binds to the first match in the DOCUMENT, and the
  // picker shows sixty tiles at once beside a live preview. Two instances
  // sharing an id would leave one of them painted in the other's colours,
  // because c1/c2 resolve from the element that actually got referenced. The
  // scene painter learnt this the same way; the counter is the same counter.
  let seq = 0;

  // SMIL is not CSS, so `prefers-reduced-motion` does not reach it and the
  // stylesheet below cannot switch it off. The one animated filter in the
  // library (flame's turbulence seed) therefore asks the media query directly,
  // once per module rather than once per avatar.
  const REDUCED =
    typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;

  // The def kinds that cost an offscreen buffer, as opposed to the gradients,
  // which cost nothing worth measuring. Only these are dropped at tile size.
  const COSTLY = new Set(["blur", "turb"]);
</script>

<script>
  // Renders a worn decoration around an avatar. See lib/decorations.js for the
  // authoring contract — this file only knows how to draw what that file says.
  //
  // Two SVGs rather than one, because a decoration has to be able to sit BOTH
  // behind the face (wings) and over it (ears breaking the top edge), and the
  // avatar itself is a DOM element between them. One overlay could not do that
  // without compositing the avatar into the SVG, which would lose the image
  // element, the presence dot and the text fallback.
  // The table arrives on its own chunk (lib/cosmetics.svelte.js). Until it
  // does, `d` is null and this component draws nothing — which is exactly what
  // it already did for an id this build does not recognise, and costs nothing
  // in layout because both layers are overlays around a face that is already
  // there.
  import { decorationsTable } from "./lib/cosmetics.svelte.js";

  // `preview` overrides the small-size freeze below. A picker is the ONE place
  // motion has to be visible — it is the whole reason to choose one decoration
  // over another — and it shows tens of tiles, not the forty rows of a member
  // list that the freeze exists to protect.
  //
  // `cw` is the wearer's chosen colourway id (lib/decorations.js COLORWAYS).
  // Empty — and anything this build does not recognise — means their profile
  // colour, which is what every decoration was painted in before the choice
  // existed.
  let { id = "", size = 32, color = "", color2 = "", cw = "", preview = false } = $props();

  const uid = `dc${++seq}`;
  const tbl = $derived(decorationsTable());
  const d = $derived(tbl && id ? tbl.decoration(id) : null);
  const back = $derived(d ? d.parts.filter((p) => p.z === "back") : []);
  const front = $derived(d ? d.parts.filter((p) => p.z !== "back") : []);

  // ── the tier ladder ───────────────────────────────────────────────────────
  // Two dials, not one, because the two costs are different in kind.
  //
  // MOTION costs a style recalculation per animated element per frame whatever
  // the element's size, so a member list of forty rows pays forty times over
  // for movement nobody can see at 20 pixels. That is the freeze that was
  // already here, and a picker tile opts out of it because choosing between
  // decorations is exactly the moment the movement is the information.
  //
  // FILTERS cost an offscreen buffer and a re-rasterisation, and unlike motion
  // that cost does NOT fall away when the element is small — a 36px tile still
  // allocates and blurs a surface. Sixty tiles of turbulence is a hitch on
  // every open of the picker, and the texture is invisible at that size
  // anyway. So filters follow the pixels, not the preview flag: gradients (and
  // therefore all of the form) survive everywhere, filters only where the
  // detail they add can actually be seen.
  //
  // And a third rung above both, for a filter primitive that ANIMATES. That is
  // a different animal from a filter that merely exists: a static filter is
  // rasterised once and cached, and measures at zero repaint however many of
  // them are on screen — twelve fox-ears at 84px produced no Paint event at all
  // over ten seconds. An animated one re-rasterises, and because the SMIL clock
  // is a DOCUMENT timeline it drags a style recalculation of the whole page
  // with it every frame: one flame tile among sixty-one cost 203ms of paint and
  // 731ms of style per ten seconds, and removing that one attribute took both
  // to zero. So the flicker is reserved for the sizes where a profile is being
  // looked at, and there are one or two of those on screen, not sixty.
  const still = $derived(!preview && size < 40);
  const flat = $derived(size < 40);
  const lively = $derived(size >= 64);

  const defs = $derived(!d ? [] : flat ? d.defs?.filter((x) => !COSTLY.has(x.t)) || [] : d.defs || []);
  const filters = $derived(!flat);
  // One <defs> for both layers — `url(#id)` resolves against the DOCUMENT, so
  // a back part reaches a gradient defined in the front SVG perfectly well, and
  // defining it twice would give two elements the same id. It goes in whichever
  // layer is rendered, so a decoration that is nothing but wings still has it.
  const defsLayer = $derived(back.length ? "back" : "front");

  // ── colour ────────────────────────────────────────────────────────────────
  // A decoration may carry its OWN colourway (`own`) — gold on a crown, fox
  // orange on a pair of ears — and it is the DEFAULT, not a lock: a wearer who
  // has chosen a profile colour wears the piece in it. That is the whole point
  // of the ramp below. Two flat tokens could only ever produce a wash, which is
  // why sixty decorations all looked like the same teal object; five steps
  // derived from one base give a wearer's own colour real form instead.
  //
  // The steps are mixed in oklab so hue survives: mixing a saturated red
  // toward white in sRGB slides it pink, and a crown that goes pink in the
  // highlight is not made of anything.
  //
  // A colourway replaces the base pair outright; with none chosen the pair is
  // the wearer's own profile colours and the chain below is untouched, so a
  // profile saved before any of this renders byte for byte as it did.
  const base = $derived(tbl ? tbl.decorColors(id, cw, color, color2) : []);
  const vars = $derived(
    `--d-c1:${base[0] || d?.own?.[0] || "var(--accent)"};` +
      `--d-c2:${base[1] || base[0] || d?.own?.[1] || d?.own?.[0] || "var(--accent)"};`,
  );

  // Colour tokens resolve here so a decoration never carries a raw colour it
  // did not choose deliberately, and so the wearer's palette flows through.
  // `@name` reaches a gradient or filter in THIS instance's <defs>.
  function paint(v) {
    if (v == null) return v;
    if (v === "c1" || v === "c2") return `var(--d-${v})`;
    if (v === "ink") return "#161a20";
    if (v === "light") return "#f2f5f8";
    if (v[0] === "@") return `url(#${uid}-${v.slice(1)})`;
    const m = /^(c1|c2)-(glint|lit|shade|deep)$/.exec(v);
    if (m) return `var(--d-${m[1]}-${m[2]})`;
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
  function styleOf(p) {
    let s = "";
    if (p.pv) s += `transform-box:view-box;transform-origin:${p.pv[0]}px ${p.pv[1]}px;`;
    else if (p.glow) s += `filter:drop-shadow(0 0 ${p.glow / 12}px ${paint(p.stroke || p.fill)});`;
    if (p.filter && filters) s += `filter:url(#${uid}-${p.filter});`;
    return s || undefined;
  }

  // A part flagged `hi` is fine detail — a fur strand, a stray ember — that is
  // sub-pixel on a member row and only adds elements to composite there.
  const drawn = (parts) => (flat ? parts.filter((p) => !p.hi) : parts);
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
        {#if defs.length && layer === defsLayer}
          <defs>
            {#each defs as g (g.id)}
              {#if g.t === "lg"}
                <linearGradient
                  id="{uid}-{g.id}"
                  gradientUnits="userSpaceOnUse"
                  x1={g.x1}
                  y1={g.y1}
                  x2={g.x2}
                  y2={g.y2}
                >
                  {#each g.stops as s, i (i)}
                    <stop offset={s[0]} stop-color={paint(s[1])} stop-opacity={s[2] ?? 1} />
                  {/each}
                </linearGradient>
              {:else if g.t === "rg"}
                <radialGradient
                  id="{uid}-{g.id}"
                  gradientUnits="userSpaceOnUse"
                  cx={g.cx}
                  cy={g.cy}
                  r={g.r}
                  fx={g.fx}
                  fy={g.fy}
                >
                  {#each g.stops as s, i (i)}
                    <stop offset={s[0]} stop-color={paint(s[1])} stop-opacity={s[2] ?? 1} />
                  {/each}
                </radialGradient>
              {:else if g.t === "rgb"}
                <!-- No gradientUnits: the default is objectBoundingBox, so one
                     definition fits every shape that references it. -->
                <radialGradient id="{uid}-{g.id}" fx={g.fx} fy={g.fy}>
                  {#each g.stops as s, i (i)}
                    <stop offset={s[0]} stop-color={paint(s[1])} stop-opacity={s[2] ?? 1} />
                  {/each}
                </radialGradient>
              {:else if g.t === "blur"}
                <filter
                  id="{uid}-{g.id}"
                  x="-30%"
                  y="-30%"
                  width="160%"
                  height="160%"
                  color-interpolation-filters="sRGB"
                >
                  <feGaussianBlur stdDeviation={g.std} />
                </filter>
              {:else if g.t === "turb"}
                <!-- Material, not shape. A displacement map driven by noise is
                     the one thing in SVG that can make an outline stop reading
                     as vector: fur that frays, flame that has a body.
                     color-interpolation-filters="sRGB" because the default,
                     linearRGB, washes a saturated flame out to salmon. -->
                <filter
                  id="{uid}-{g.id}"
                  x="-25%"
                  y="-25%"
                  width="150%"
                  height="150%"
                  color-interpolation-filters="sRGB"
                >
                  <feTurbulence
                    type="fractalNoise"
                    baseFrequency={g.freq}
                    numOctaves={g.oct || 2}
                    seed={g.seed || 1}
                    result="n"
                  >
                    <!-- The seed, stepped rather than tweened. A flame does not
                         ease between two shapes — it is a different flame from
                         one instant to the next — and stepping also means the
                         filter re-rasterises a handful of times a second
                         instead of sixty. This is the only SMIL in the app;
                         a CSS keyframe cannot reach a filter primitive's
                         attributes at all, and the alternative (a rewrite from
                         JS on a timer) is a timer per avatar. -->
                    {#if g.flick && lively && !REDUCED}
                      <animate
                        attributeName="seed"
                        values={g.flick}
                        dur="{g.dur || 1.8}s"
                        calcMode="discrete"
                        repeatCount="indefinite"
                      />
                    {/if}
                  </feTurbulence>
                  <feDisplacementMap
                    in="SourceGraphic"
                    in2="n"
                    scale={g.scale}
                    xChannelSelector="R"
                    yChannelSelector="G"
                    result="d"
                  />
                  {#if g.blur}
                    <feGaussianBlur in="d" stdDeviation={g.blur} />
                  {/if}
                </filter>
              {/if}
            {/each}
          </defs>
        {/if}
        {#each drawn(parts) as p, i (i)}
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
              opacity={p.op}
              style={styleOf(p)}
            />
          {:else if p.el === "circle"}
            <circle
              class={cls(p)}
              cx={p.attrs.cx}
              cy={p.attrs.cy}
              r={p.attrs.r}
              fill={paint(p.fill)}
              opacity={p.op}
              style={styleOf(p)}
            />
          {:else}
            <path
              class={cls(p)}
              d={p.d}
              fill={paint(p.fill)}
              opacity={p.op}
              style={styleOf(p)}
            />
          {/if}
        {/each}
      </svg>
    {/if}
  {/each}
{/if}

<style>
  /* ── the ramp ─────────────────────────────────────────────────────────────
     Five steps derived from one base, so a wearer's colour arrives as a
     MATERIAL rather than a fill. `--d-c1` and `--d-c2` are set inline from the
     wearer's profile (or the piece's own colourway); everything else is
     computed here once and referenced by name from the data.

     Mixed in oklab, never sRGB: sRGB carries a saturated red toward white
     through pink, and a gold crown whose highlight is pink is not made of
     anything. oklab keeps the hue and moves only the lightness, which is what
     a highlight physically is.

     The two ends are deliberately far apart. A ramp that only spans ±15% is
     the flat wash this library already had; a shaded object needs to be able
     to go nearly black in the crease and nearly white on the edge that faces
     the light, or the form does not read at 36 pixels. */
  .dec {
    --d-c1-glint: color-mix(in oklab, var(--d-c1) 30%, #ffffff);
    --d-c1-lit: color-mix(in oklab, var(--d-c1) 66%, #fff6e2);
    --d-c1-shade: color-mix(in oklab, var(--d-c1) 68%, #120b04);
    --d-c1-deep: color-mix(in oklab, var(--d-c1) 38%, #0d0a06);
    --d-c2-glint: color-mix(in oklab, var(--d-c2) 30%, #ffffff);
    --d-c2-lit: color-mix(in oklab, var(--d-c2) 66%, #fff6e2);
    --d-c2-shade: color-mix(in oklab, var(--d-c2) 68%, #120b04);
    --d-c2-deep: color-mix(in oklab, var(--d-c2) 38%, #0d0a06);
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

  /* Small avatars get the drawing, still — until you point at one.
     The decoration is the point; the movement is the garnish.

     PAUSED rather than removed, which is the whole trick. `animation: none`
     tears the animation down, so there is nothing left to start again when a
     row is hovered and the only way back was to re-render the subtree. A
     paused animation costs what the freeze was protecting against anyway:
     nothing. It generates no frames, so it drags no style recalculation per
     row per frame — the forty-row member list pays for forty animations that
     are not running, which is not a measurable thing.

     What that buys is the behaviour people expect from a decoration: it sits
     quietly in a list of forty and comes alive under the pointer, one at a
     time, which is also the only moment the movement carries any information.
     `.avatar` is Avatar.svelte's own root — :global because the hover happens
     on an element this component does not own. */
  .dec.still .p,
  .p {
    will-change: transform;
  }
  .dec.still,
  .dec.still .anim {
    animation-play-state: paused;
  }
  :global(.avatar:hover) .dec.still,
  :global(.avatar:hover) .dec.still .anim {
    animation-play-state: running;
  }

  /* Asked for less motion means less motion, hover or not. This has to stay
     `animation: none` rather than a pause: reduced-motion is a statement about
     what may move at all, not about what may move right now. */
  @media (prefers-reduced-motion: reduce) {
    .dec,
    .anim,
    :global(.avatar:hover) .dec.still,
    :global(.avatar:hover) .dec.still .anim {
      animation: none !important;
    }
  }
</style>
