<script>
  // The ring around an avatar. Data-driven (lib/rings.js): a rotating art disc,
  // a satellite (dot, emoji, or your own uploaded image) orbiting, a breathing
  // halo, and/or a live effect layer in front — snow, embers, meteors,
  // lightning. Every one obeys the wearer's dials (speed / direction / glow /
  // thickness). One component, 80+ rings, one short id on the wire.
  import { ringArt } from "./lib/rings.js";
  import FxLayer from "./FxLayer.svelte";

  let { ring = "", size = 32, style = null, color = "", color2 = "" } = $props();

  const SPEED_MS = { slow: 7000, normal: 3600, fast: 1600 };
  const GLOW_PX = { off: 0, soft: 10, strong: 22 };

  const r = $derived(ringArt(ring, color, color2, style?.sat || "", style?.pal || ""));
  const dur = $derived(SPEED_MS[style?.speed] || SPEED_MS.normal);
  const cw = $derived(style?.dir !== "ccw");
  const glow = $derived(GLOW_PX[style?.glow] ?? GLOW_PX.soft);
  const w = $derived(Math.min(5, Math.max(1, style?.width || 2)));
  const dot = $derived(Math.max(3, w + 1.5));
  // Effects are tuned for a banner-sized canvas; shrink them for tiny avatars.
  const scale = $derived(Math.max(0.45, Math.min(1, size / 64)));
  // A rotating drop-shadow is a per-frame blur. On the profile card (one big
  // avatar) that's gorgeous; on a chat list with forty 32px avatars it's forty
  // animated blurs, so small avatars drop the glow and keep the ring.
  const cheap = $derived(size < 44 || glow === 0);

  const vars = $derived(
    r
      ? `${r.vars}--rdur:${dur}ms;--rdir:${cw ? "normal" : "reverse"};` +
        `--rdir-inv:${cw ? "reverse" : "normal"};--rglow:${glow}px;--rw:${w}px;--rdot:${dot}px;` +
        `--rsat:${Math.round(size * 0.42)}px;`
      : "",
  );
</script>

{#if r}
  <!-- Behind the avatar: art disc, orbit band, halo. -->
  <span class="ring" class:cheap style={vars} aria-hidden="true">
    {#if r.art}
      <span class="art" class:frozen={r.static} style={`background:${r.art};`}></span>
    {/if}
    {#if r.band}
      <span class="band" style={typeof r.band === "string" ? `--band-c:${r.band};` : ""}></span>
    {/if}
    {#if r.halo !== undefined}
      <span class="halo" style={`--dot-c:${r.halo || "var(--c1)"};`}></span>
    {/if}
    {#if r.orbit && !r.front}
      <span class="orbit" class:trail={r.orbit.trail} style={`--dot-c:${r.orbit.dot || "var(--c1)"};`}></span>
      {#if r.orbit.dot2}
        <span class="orbit second" style={`--dot-c:${r.orbit.dot2};`}></span>
      {/if}
    {/if}
  </span>

  <!-- In front of the avatar: weather, sparks, meteors — they fall ACROSS your
       face, not behind your head. A rider goes here too: a cow big enough to
       see must not disappear behind your head halfway round. -->
  {#if r.fx || r.front}
    <span class="ring front" class:cheap class:wide={!!r.fx} style={vars} aria-hidden="true">
      {#if r.front}
        <span class="orbit rider" class:trail={r.orbit.trail} style={`--dot-c:${r.orbit.dot || "var(--c1)"};`}>
          {#if r.isImg}
            <img class="sat" src={r.orbit.sat} alt="" />
          {:else}
            <span class="sat">{r.orbit.sat}</span>
          {/if}
        </span>
        {#if r.orbit.dot2}
          <span class="orbit second" style={`--dot-c:${r.orbit.dot2};`}></span>
        {/if}
      {/if}
      {#if r.fx}
        <FxLayer fx={{ ...r.fx, tumble: r.tumble }} seed={ring} {scale} />
      {/if}
    </span>
  {/if}
{/if}

<style>
  /* The ring never affects layout; its overflow is decorative (scroll
     containers clip it — see ChannelList/MemberPanel `overflow-x: clip`). */
  .ring {
    position: absolute;
    inset: 0;
    border-radius: 50%;
    pointer-events: none;
    z-index: -1;
  }
  /* The effect layer sits above the avatar image, inside its own stacking
     context (Avatar has `isolation: isolate`), and spills slightly past the
     circle so snow drifts in from outside. */
  .ring.front {
    inset: 0;
    z-index: 2;
  }
  /* Weather needs a little room outside the circle to drift in from. */
  .ring.front.wide {
    inset: -18%;
  }
  /* The rider orbits in front, so its band stays behind (it's decoration, not
     the thing you're meant to watch). */
  .orbit.rider {
    background: none;
    filter: none;
  }
  .art,
  .band,
  .halo,
  .orbit {
    position: absolute;
    border-radius: 50%;
  }
  /* Rotating art: a disc a little larger than the avatar; the avatar covers the
     middle, leaving a ring of art visible. */
  .art {
    inset: calc(-1px - var(--rw));
    animation: ring-spin var(--rdur) linear infinite;
    animation-direction: var(--rdir);
    filter: drop-shadow(0 0 var(--rglow) var(--c1));
  }
  .art.frozen {
    animation: none;
  }
  /* No animated blurs on small avatars — see `cheap` above. */
  .cheap .art,
  .cheap .orbit,
  .cheap .sat {
    filter: none;
  }
  .band {
    inset: calc(-1px - var(--rw));
    box-shadow: 0 0 0 var(--rw) color-mix(in srgb, var(--band-c, var(--c1)) 55%, transparent);
  }
  /* Orbit: a satellite riding an invisible circle. */
  .orbit {
    inset: calc(-4px - var(--rw));
    animation: ring-spin var(--rdur) linear infinite;
    animation-direction: var(--rdir);
    background: radial-gradient(circle var(--rdot) at 50% 0%, var(--dot-c) 96%, transparent) no-repeat;
    filter: drop-shadow(0 0 var(--rglow) var(--dot-c));
  }
  /* A satellite with a face (🐄, 🚀, your dog) replaces the dot — and
     counter-rotates so it never flies upside down. */
  .orbit:has(.sat) {
    background: none;
    filter: none;
  }
  .sat {
    position: absolute;
    left: 50%;
    top: 0;
    width: var(--rsat);
    height: var(--rsat);
    margin: calc(var(--rsat) / -2);
    font-size: calc(var(--rsat) * 0.9);
    line-height: var(--rsat);
    text-align: center;
    border-radius: 50%;
    object-fit: cover;
    animation: ring-spin var(--rdur) linear infinite;
    animation-direction: var(--rdir-inv);
    filter: drop-shadow(0 0 var(--rglow) var(--c1));
  }
  .orbit.second {
    animation-delay: calc(var(--rdur) / -2); /* the far side of the orbit */
  }
  .orbit.trail::after {
    content: "";
    position: absolute;
    inset: 0;
    border-radius: 50%;
    background: conic-gradient(from 0deg, var(--dot-c), transparent 28%);
    opacity: 0.32;
  }
  /* Pulse: pure breathing energy, no band. */
  .halo {
    inset: 0;
    animation: ring-pulse var(--rdur) ease-in-out infinite;
    box-shadow: 0 0 0 0 var(--dot-c);
  }
  @keyframes ring-spin {
    to {
      transform: rotate(360deg);
    }
  }
  @keyframes ring-pulse {
    0%,
    100% {
      box-shadow:
        0 0 0 var(--rw) color-mix(in srgb, var(--dot-c) 70%, transparent),
        0 0 var(--rglow) 0 color-mix(in srgb, var(--dot-c) 45%, transparent);
    }
    50% {
      box-shadow:
        0 0 0 var(--rw) var(--dot-c),
        0 0 calc(var(--rglow) + 6px) 5px transparent;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .art,
    .orbit,
    .halo,
    .sat {
      animation: none;
    }
  }
</style>
