<script>
  // The drawings of the "What's new" deck: one per kind of feature, chosen by
  // the release notes (a `<!-- hero: … -->` hint under a heading, or a keyword
  // guess — lib/releasenotes.js). Inline SVG in the icon set's language (round
  // caps and joins, one stroke weight) scaled up and inked with the aurora.
  // Nothing is fetched; everything is aria-hidden, the words beside it carry
  // the meaning.
  //
  // Each drawing tells its feature once as the slide arrives (a device walks
  // back into its guild, a profile sheds its decorations, a recent tile is
  // picked, a plugin snaps into place) and then keeps one quiet loop. Motion
  // is transform and opacity on parts that carry no `transform` attribute of
  // their own — a part placed by translate() is wrapped, so the CSS transform
  // moves the wrapper and the placement stays put. Every part's resting style
  // is its final frame, so with motion reduced the drawing is simply there.
  let { kind = "spark", size = "" } = $props();

  // One id per drawing: two decks never coexist, but a drawing is re-keyed on
  // every slide and a stale gradient id would paint from the old node.
  const uid = "wnh" + Math.random().toString(36).slice(2, 8);

  // The icon set's glyphs for the generic drawings (Icon.svelte's 16px grid).
  const GLYPH = {
    call: "M4.4 2.5c.4 0 .8.2.9.6l.9 2.1a1 1 0 0 1-.2 1.1L4.8 7.5a9.8 9.8 0 0 0 3.7 3.7l1.2-1.2a1 1 0 0 1 1.1-.2l2.1.9c.4.1.6.5.6.9v1.4c0 .6-.5 1.1-1.1 1A11.9 11.9 0 0 1 2.5 3.6c-.1-.6.4-1.1 1-1.1z",
    search: "M7 11.5a4.5 4.5 0 1 0 0-9 4.5 4.5 0 0 0 0 9z M10.3 10.3 14 14",
    bell: "M8 2a3.6 3.6 0 0 0-3.6 3.6c0 3-1.4 4.4-1.4 4.4h10s-1.4-1.4-1.4-4.4A3.6 3.6 0 0 0 8 2z M6.8 12.5a1.3 1.3 0 0 0 2.4 0",
    shield: "M8 2.2 13 4v4.1c0 3-2.1 5-5 5.7-2.9-.7-5-2.7-5-5.7V4z",
    spark: "M8 1.5 9.6 6l4.4 1.6L9.6 9.2 8 13.7 6.4 9.2 2 7.6 6.4 6z",
  };
  const generic = $derived(!["rejoin", "profile", "attach", "desktop", "emblem"].includes(kind));

  // The guild of the rejoin drawing: four members on the orbit, one empty
  // seat at the upper right that the returning device walks back into.
  const MEMBERS = [
    [200, 46],
    [110, 98],
    [110, 202],
    [200, 254],
    [290, 202],
  ];
  const TILES = [0, 1, 2, 3, 4];
</script>

<svg
  class="hero {kind} {size}"
  viewBox={kind === "emblem" ? "0 0 320 320" : "0 0 400 300"}
  style="--ink: url(#{uid}-ink)"
  aria-hidden="true"
  focusable="false"
>
  <defs>
    <linearGradient id="{uid}-ink" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" style="stop-color: var(--aurora-1)" />
      <stop offset="0.55" style="stop-color: var(--aurora-2)" />
      <stop offset="1" style="stop-color: var(--aurora-3)" />
    </linearGradient>
    <linearGradient id="{uid}-band" x1="0" y1="0" x2="1" y2="0.4">
      <stop offset="0" style="stop-color: var(--aurora-1); stop-opacity: 0.75" />
      <stop offset="0.6" style="stop-color: var(--aurora-2); stop-opacity: 0.6" />
      <stop offset="1" style="stop-color: var(--aurora-3); stop-opacity: 0.7" />
    </linearGradient>
    <radialGradient id="{uid}-soft" cx="0.35" cy="0.3" r="0.9">
      <stop offset="0" style="stop-color: var(--aurora-1); stop-opacity: 0.34" />
      <stop offset="1" style="stop-color: var(--aurora-3); stop-opacity: 0.12" />
    </radialGradient>
  </defs>

  {#if kind === "emblem"}
    <!-- Three peers and the light between them: Concord's own picture of
         itself, no server in the middle. -->
    <g class="spin-slow"><circle cx="160" cy="160" r="150" class="faint dash" /></g>
    <circle cx="160" cy="160" r="112" class="faint in grow" style="--i: 0" />
    <circle cx="160" cy="160" r="70" class="faint in grow" style="--i: 1" />
    <path d="M160 72 84 204h152z" class="ink thin in" style="--i: 3" />
    {#each [[160, 72], [84, 204], [236, 204]] as [x, y], i (i)}
      <g transform="translate({x} {y})">
        <g class="in pop" style="--i: {4 + i}">
          <circle r="22" fill="url(#{uid}-soft)" class="ink" />
          <circle r="7" class="dot" />
        </g>
      </g>
    {/each}
    <g transform="translate(160 160)">
      <circle r="6" class="spark in pop" style="--i: 8" />
      <circle r="6" class="spark-ring pulse" />
    </g>
  {:else if kind === "rejoin"}
    <circle cx="200" cy="150" r="104" class="faint dash in" style="--i: 0" />
    {#each MEMBERS as [x, y], i (i)}
      <line x1="200" y1="150" x2={x} y2={y} class="faint in" style="--i: {1 + i}" />
    {/each}
    <line x1="200" y1="150" x2="290" y2="98" class="ink thin link" />
    <g transform="translate(200 150)">
      <circle r="40" class="pulse-ring" />
      <g class="in pop" style="--i: 1">
        <circle r="40" fill="url(#{uid}-soft)" class="ink" />
        <path d="M-9 -16-12 16 M8 -16 5 16 M-18 -6H18 M-19 6H16" class="ink" />
      </g>
    </g>
    {#each MEMBERS as [x, y], i (i)}
      <g transform="translate({x} {y})">
        <g class="in pop" style="--i: {3 + i}">
          <circle r="15" fill="url(#{uid}-soft)" class="ink" />
          <circle r="4.5" class="dot" />
        </g>
      </g>
    {/each}
    <circle cx="290" cy="98" r="22" class="faint dash in" style="--i: 2" />
    <g transform="translate(290 98)">
      <g class="travel">
        <rect x="-17" y="-12" width="34" height="21" rx="3" fill="url(#{uid}-soft)" class="ink" />
        <path d="M-22 14H22" class="ink" />
      </g>
    </g>
    <g transform="translate(314 76)">
      <g class="check">
        <circle r="11" class="badge" />
        <path d="M-5 0-1.5 3.5 5-3.5" class="badge-tick" />
      </g>
    </g>
  {:else if kind === "profile"}
    <!-- The card that was: a frame, a ring, sparkles. They leave; the
         picture, the banner, the name and the bio stay. -->
    <rect x="76" y="26" width="248" height="248" rx="32" class="spark-line dash gone" />
    <rect x="96" y="40" width="208" height="220" rx="22" class="card ink in" style="--i: 0" />
    <path d="M96 112V62a22 22 0 0 1 22-22h164a22 22 0 0 1 22 22v50z" fill="url(#{uid}-band)" class="in" style="--i: 1" />
    <circle cx="152" cy="112" r="42" class="spark-line dash gone" />
    <g class="in pop" style="--i: 2">
      <circle cx="152" cy="112" r="30" class="ground ink" />
      <circle cx="152" cy="104" r="9" class="ink" />
      <path d="M136 129a16 14 0 0 1 32 0" class="ink" />
    </g>
    <circle cx="173" cy="133" r="7.5" class="presence in pop" style="--i: 4" />
    <rect x="116" y="162" width="112" height="13" rx="6.5" class="bar-ink in" style="--i: 3" />
    <rect x="116" y="185" width="70" height="8" rx="4" class="bar-mid in" style="--i: 4" />
    <rect x="116" y="210" width="168" height="7" rx="3.5" class="bar-faint in" style="--i: 5" />
    <rect x="116" y="226" width="140" height="7" rx="3.5" class="bar-faint in" style="--i: 6" />
    {#each [[262, 78, 1], [326, 164, 0.8], [84, 196, 0.9], [236, 26, 0.6]] as [x, y, s], i (i)}
      <g transform="translate({x} {y}) scale({s})">
        <path d="M0 -12 3 -3 12 0 3 3 0 12 -3 3 -12 0 -3 -3z" class="spark-fill gone" />
      </g>
    {/each}
  {:else if kind === "attach"}
    <rect x="44" y="34" width="312" height="170" rx="22" class="card ink in" style="--i: 0" />
    <rect x="70" y="58" width="62" height="10" rx="5" class="bar-ink in" style="--i: 1" />
    {#each TILES as t (t)}
      <g transform="translate({70 + t * 56} 84)">
        <g class="in slide" style="--i: {2 + t}">
          <g class:pick={t === 1}>
            <rect width="46" height="46" rx="10" fill="url(#{uid}-soft)" class="ink" />
            {#if t === 2}
              <path d="M16 11h10l6 6v18H16z M26 11v6h6" class="ink thin" />
            {:else if t === 4}
              <path d="M18 14v18l15-9z" class="ink thin" />
            {:else}
              <path d="M9 35 19 23l7 8 5-6 7 10" class="ink thin" />
              <circle cx="31" cy="15" r="3.5" class="ink thin" />
            {/if}
            {#if t === 1}
              <rect x="-5" y="-5" width="56" height="56" rx="14" class="spark-line picked" />
            {/if}
          </g>
        </g>
      </g>
    {/each}
    <rect x="70" y="152" width="200" height="7" rx="3.5" class="bar-faint in" style="--i: 7" />
    <rect x="70" y="170" width="150" height="7" rx="3.5" class="bar-faint in" style="--i: 8" />
    <rect x="44" y="222" width="312" height="44" rx="22" class="faint in" style="--i: 1" />
    <g transform="translate(70 244)">
      <circle r="13" class="ink in pop" style="--i: 2" />
      <path d="M0 -6V6 M-6 0H6" class="ink thin" />
    </g>
    <rect x="96" y="240" width="130" height="8" rx="4" class="bar-faint in" style="--i: 3" />
  {:else if kind === "desktop"}
    <rect x="52" y="40" width="296" height="214" rx="16" class="card ink in" style="--i: 0" />
    <path d="M52 72H348" class="faint in" style="--i: 1" />
    {#each [74, 90, 106] as x, i (x)}
      <circle cx={x} cy="56" r="4.5" class="faint in" style="--i: {1 + i}" />
    {/each}
    <rect x="76" y="92" width="140" height="100" rx="12" fill="url(#{uid}-soft)" class="ink in" style="--i: 2" />
    <path d="M136 124v36l30-18z" class="ink late" />
    <rect x="236" y="98" width="88" height="8" rx="4" class="bar-faint in" style="--i: 3" />
    <rect x="236" y="116" width="64" height="8" rx="4" class="bar-faint in" style="--i: 4" />
    <rect x="236" y="134" width="76" height="8" rx="4" class="bar-faint in" style="--i: 5" />
    <rect x="76" y="210" width="120" height="8" rx="4" class="bar-faint in" style="--i: 6" />
    <!-- The socket and the piece: gst-plugins-good, snapping into place. -->
    <g transform="translate(250 176)">
      <path d="M0 0h64v52H0V34a8 8 0 0 0 0-16z" class="faint dash in" style="--i: 4" />
      <g class="plug">
        <path d="M0 0h64v52H0V34a8 8 0 0 0 0-16z" fill="url(#{uid}-band)" class="ink" />
      </g>
    </g>
    <g transform="translate(316 176)">
      <g class="check">
        <circle r="11" class="badge" />
        <path d="M-5 0-1.5 3.5 5-3.5" class="badge-tick" />
      </g>
    </g>
  {/if}

  {#if generic}
    <g transform="translate(200 150)">
      <circle r="126" class="faint dash in grow" style="--i: 0" />
      <circle r="92" class="faint in grow" style="--i: 1" />
      <circle r="58" class="pulse-ring" />
      <g class="in pop" style="--i: 2">
        <circle r="58" fill="url(#{uid}-soft)" class="ink" />
        <g transform="scale(5.2) translate(-8 -8)">
          <path d={GLYPH[kind] || GLYPH.spark} class="ink glyph" vector-effect="non-scaling-stroke" />
        </g>
      </g>
    </g>
    {#each [[92, 70], [318, 92], [300, 236]] as [x, y], i (i)}
      <circle cx={x} cy={y} r="5" class="dot in pop" style="--i: {4 + i}" />
    {/each}
  {/if}
</svg>

<style>
  .hero {
    display: block;
    width: 100%;
    height: 100%;
    overflow: visible;
  }
  /* Strokes in the aurora; the url is per drawing, so it rides a class on
     each part rather than a property here. */
  .ink {
    fill: none;
    stroke: var(--ink, var(--aurora-2));
    stroke-width: 3;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .thin {
    stroke-width: 2.5;
  }
  .glyph {
    stroke-width: 3.5;
  }
  .faint {
    fill: none;
    stroke: var(--stage-ink-muted);
    stroke-opacity: 0.35;
    stroke-width: 2;
    stroke-linecap: round;
  }
  .dash {
    stroke-dasharray: 2 9;
  }
  .card {
    fill: color-mix(in srgb, var(--stage-ground) 70%, transparent);
  }
  .ground {
    fill: var(--stage-ground);
  }
  .dot {
    fill: var(--aurora-1);
  }
  .presence {
    fill: var(--aurora-1);
    stroke: var(--stage-ground);
    stroke-width: 3;
  }
  .bar-ink {
    fill: var(--stage-ink);
    opacity: 0.85;
  }
  .bar-mid {
    fill: var(--stage-ink-muted);
    opacity: 0.7;
  }
  .bar-faint {
    fill: var(--stage-ink-muted);
    opacity: 0.38;
  }
  .spark {
    fill: var(--aurora-spark);
  }
  .spark-line {
    fill: none;
    stroke: var(--aurora-spark);
    stroke-width: 2.5;
    stroke-linecap: round;
  }
  .spark-fill {
    fill: var(--aurora-spark);
  }
  .spark-ring {
    fill: none;
    stroke: var(--aurora-spark);
    stroke-width: 2;
  }
  .badge {
    fill: var(--aurora-1);
  }
  .badge-tick {
    fill: none;
    stroke: var(--stage-ground);
    stroke-width: 2.5;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .pulse-ring {
    fill: none;
    stroke: var(--aurora-1);
    stroke-width: 2;
    opacity: 0;
  }

  /* ---- motion --------------------------------------------------------------
     Every part pivots on itself. The resting style below each rule is the
     final frame, so a reduced-motion reader (durations and delays zeroed by
     app.css) sees the finished drawing at once. */
  .in,
  .pop,
  .grow,
  .slide,
  .gone,
  .travel,
  .check,
  .plug,
  .late,
  .link,
  .pick,
  .picked,
  .pulse,
  .pulse-ring,
  .spin-slow {
    transform-box: fill-box;
    transform-origin: center;
  }
  .in {
    animation: hero-in var(--dur-reveal) var(--ease-spring) both;
    animation-delay: calc(140ms + var(--i, 0) * var(--stagger) * 1.6);
  }
  .in.pop {
    animation-name: hero-pop;
  }
  .in.grow {
    animation-name: hero-grow;
  }
  .in.slide {
    animation-name: hero-slide;
  }
  @keyframes hero-in {
    from {
      opacity: 0;
      transform: translateY(calc(var(--stage-rise) * 0.6));
    }
  }
  @keyframes hero-pop {
    from {
      opacity: 0;
      transform: scale(0.6);
    }
  }
  @keyframes hero-grow {
    from {
      opacity: 0;
      transform: scale(0.82);
    }
  }
  @keyframes hero-slide {
    from {
      opacity: 0;
      transform: translateX(calc(var(--stage-rise) * -1.4));
    }
  }

  /* The device walks back into its seat, its link lights, a tick. */
  .travel {
    animation: hero-travel calc(var(--dur-reveal) * 2) var(--ease-spring) 700ms both;
  }
  @keyframes hero-travel {
    0% {
      opacity: 0.35;
      transform: translate(64px, -58px) scale(0.86);
    }
    30% {
      opacity: 0.45;
      transform: translate(64px, -58px) scale(0.86);
    }
  }
  .link {
    animation: hero-fade var(--dur-reveal) ease-out 1500ms both;
  }
  .check {
    animation: hero-pop var(--dur-reveal) var(--ease-spring) 1650ms both;
  }
  @keyframes hero-fade {
    from {
      opacity: 0;
    }
  }

  /* The profile's old decorations: there, then gone. Resting: gone. */
  .gone {
    opacity: 0;
    transform: scale(0.9);
    animation: hero-gone calc(var(--dur-reveal) * 1.4) var(--ease-in) 1100ms both;
  }
  @keyframes hero-gone {
    from {
      opacity: 1;
      transform: none;
    }
  }

  /* A recent tile is picked: it lifts, and a ring of the one spark finds it. */
  .pick {
    transform: translateY(-8px);
    animation: hero-lift var(--dur-reveal) var(--ease-spring) 1250ms both;
  }
  @keyframes hero-lift {
    from {
      transform: none;
    }
  }
  .picked {
    animation: hero-pop var(--dur-reveal) var(--ease-spring) 1350ms both;
  }

  /* The plugin snaps into its socket, then the clip can play. */
  .plug {
    animation: hero-plug calc(var(--dur-reveal) * 1.5) var(--ease-spring) 650ms both;
  }
  @keyframes hero-plug {
    from {
      opacity: 0;
      transform: translate(56px, 46px) rotate(9deg);
    }
  }
  .late {
    animation: hero-pop var(--dur-reveal) var(--ease-spring) 1350ms both;
  }

  /* The one loop each drawing keeps: a ring breathing out from its centre. */
  .pulse-ring {
    animation: hero-pulse 3.2s ease-out 1.8s infinite;
  }
  @keyframes hero-pulse {
    0% {
      opacity: 0.55;
      transform: scale(1);
    }
    100% {
      opacity: 0;
      transform: scale(1.7);
    }
  }
  .pulse {
    opacity: 0;
    animation: hero-pulse 2.8s ease-out 1.2s infinite;
  }
  .spin-slow {
    animation: hero-spin 60s linear infinite;
  }
  @keyframes hero-spin {
    to {
      transform: rotate(1turn);
    }
  }
</style>
