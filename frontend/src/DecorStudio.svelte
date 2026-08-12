<script>
  // The one picker for everything WORN on your avatar.
  //
  // There used to be two doors: a gradient ring in here, a drawn piece in
  // there. Nobody thinks in those terms — a halo, a band of runes and a pair of
  // ears are one question, "what am I wearing", and asking it twice produced
  // the obvious outcome, people wearing a ring and a figure that had never been
  // designed to sit together. So it is ONE slot: picking a gradient ring puts
  // the previous drawn piece away, and picking a drawn piece puts the ring
  // away.
  //
  // One slot, and yet BOTH fields still travel. `frame` carries the gradient
  // ring (or one of the twenty-one drawn rings that predate the merge) and
  // `dec` carries the drawn piece, exactly as before — this picker just never
  // sets both at once. That is deliberate and it is why there is no migration:
  // someone who set a ring and a decoration back when they stacked keeps
  // wearing both, and keeps seeing both selected here, until the moment they
  // choose something. Nothing anyone saved is rewritten on their behalf.
  //
  // Two panes ride above the library and only one of them is ever up, because
  // they belong to different kinds of thing: the gradient dials (RingDials)
  // when a gradient ring is selected, the colourway swatches when a drawn piece
  // is. A gradient ring's colour is its palette; a drawn piece's colour is a
  // base that a five-step ramp expands, and those are not the same control.
  //
  // Every tile draws YOUR avatar wearing the thing, in the colour you have
  // chosen, because the whole point of drawing these as paths rather than
  // shipping images is that they adopt the wearer. A generic swatch would sell
  // that short.
  import { registerOverlay } from "./lib/state.svelte.js";
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import RingDials from "./RingDials.svelte";
  import {
    DECORATION_BY_ID,
    DECORATION_GROUPS,
    COLORWAYS,
    COLORWAY_BY_ID,
    CW_OWN,
  } from "./lib/decorations.js";
  import { RING_BY_ID, RING_GROUPS, hasRider, hasPalette } from "./lib/rings.js";

  let {
    decoration = "",
    ring = "",
    dc = "",
    speed = "normal",
    dir = "cw",
    glow = "soft",
    width = 2,
    sat = "",
    pal = "",
    color = "#14a394",
    color2 = "",
    avatar = "",
    emoji = "",
    name = "You",
    onApply,
    onClose,
  } = $props();
  $effect(() => registerOverlay(() => onClose?.()));

  // The two halves of the one slot. Both can arrive set — see the note above —
  // and picking anything at all collapses that back to one.
  let selDec = $state(decoration);
  let selRing = $state(ring);
  let cwSel = $state(dc);
  let sp = $state(speed);
  let dr = $state(dir);
  let gl = $state(glow);
  let w = $state(width);
  let st = $state(sat);
  let pl = $state(pal || "yours");

  const dials = $derived({ speed: sp, dir: dr, glow: gl, width: w, sat: st, pal: pl });
  const drawn = $derived(selDec ? DECORATION_BY_ID[selDec] : null);
  const worn = $derived(
    DECORATION_BY_ID[selDec]?.name ||
      RING_BY_ID[selRing]?.name ||
      DECORATION_BY_ID[selRing]?.name ||
      "None",
  );

  function pickDec(id) {
    selDec = id;
    selRing = "";
  }
  function pickRing(id) {
    selRing = id;
    selDec = "";
  }

  // The swatch shows the ramp, not the base: three of the five steps the
  // painter derives, mixed the same way (oklab) with the same two ends, so what
  // you are choosing between is what you will be wearing rather than a flat
  // dot that all twelve of these would reduce to.
  const swatch = (c) =>
    `background:linear-gradient(135deg, color-mix(in oklab, ${c} 30%, #ffffff), ${c} 52%, color-mix(in oklab, ${c} 38%, #0d0a06));`;
  const cwName = $derived(
    COLORWAY_BY_ID[cwSel]?.name ||
      (cwSel === CW_OWN && drawn?.own ? "As designed" : "Your profile colour"),
  );
</script>

<div class="ds-scrim" role="presentation" onclick={onClose}></div>
<div class="ds" role="dialog" aria-label="Choose what you wear on your avatar">
  <div class="ds-head">
    <button class="icon-btn" onclick={onClose} aria-label="Back"><Icon name="chevron" size={16} /></button>
    <strong>Avatar decoration</strong>
    <span class="tiny muted">{worn}</span>
  </div>

  <div class="preview">
    <Avatar
      {name}
      {emoji}
      {color}
      image={avatar}
      size={84}
      decoration={selDec}
      frame={selRing}
      style={dials}
      dc={cwSel}
      {color2}
      preview
    />
  </div>

  <!-- The tune pane. Whichever kind is selected brings its own controls with
       it; nothing floats here belonging to a thing you are not wearing. -->
  {#if selDec}
    <div class="tune">
      <div class="pane">
        <span class="tiny muted">Colour · {cwName}</span>
        <div class="shelf">
          <button
            type="button"
            class="cw-btn"
            class:on={!COLORWAY_BY_ID[cwSel] && !(cwSel === CW_OWN && drawn?.own)}
            title="Match my profile colour"
            aria-label="Match my profile colour"
            onclick={() => (cwSel = "")}
            style={swatch(color || "var(--accent)")}
          ><span class="mark">You</span></button>
          {#if drawn?.own}
            <button
              type="button"
              class="cw-btn"
              class:on={cwSel === CW_OWN}
              title="As designed"
              aria-label="As designed"
              onclick={() => (cwSel = CW_OWN)}
              style={swatch(drawn.own[0])}
            ><span class="mark">Own</span></button>
          {/if}
          {#each COLORWAYS as c (c.id)}
            <button
              type="button"
              class="cw-btn"
              class:on={cwSel === c.id}
              title={c.name}
              aria-label={c.name}
              onclick={() => (cwSel = c.id)}
              style={swatch(c.c[0])}
            ></button>
          {/each}
        </div>
      </div>
    </div>
  {/if}
  {#if selRing}
    <div class="tune">
      <RingDials
        bind:ring={selRing}
        bind:speed={sp}
        bind:dir={dr}
        bind:glow={gl}
        bind:width={w}
        bind:sat={st}
        bind:pal={pl}
        {color}
        {color2}
      />
    </div>
  {/if}

  <div class="library">
    <button
      class="opt none"
      class:sel={selDec === "" && selRing === ""}
      onclick={() => {
        selDec = "";
        selRing = "";
      }}
    >
      <span class="none-dot"></span>
      <span class="oname">None</span>
    </button>

    <div class="section">Drawn</div>
    {#each DECORATION_GROUPS as g (g.title)}
      <div class="gtitle">{g.title}</div>
      <div class="grid">
        {#each g.ids as id (id)}
          <button
            class="opt"
            class:sel={selDec === id}
            onclick={() => pickDec(id)}
            title={DECORATION_BY_ID[id]?.name}
          >
            <Avatar
              {name}
              {emoji}
              {color}
              image={avatar}
              size={36}
              decoration={id}
              dc={cwSel}
              {color2}
              preview
            />
            <span class="oname">{DECORATION_BY_ID[id]?.name}</span>
          </button>
        {/each}
      </div>
    {/each}

    <div class="section">Gradient rings</div>
    {#each RING_GROUPS as g (g.title)}
      <div class="gtitle">{g.title}</div>
      <div class="grid rings">
        {#each g.ids as id (id)}
          <button
            class="opt"
            class:sel={selRing === id}
            onclick={() => pickRing(id)}
            title={RING_BY_ID[id]?.name}
          >
            <Avatar {name} {emoji} {color} image={avatar} size={36} frame={id} style={dials} {color2} />
            <span class="oname">{RING_BY_ID[id]?.name}</span>
          </button>
        {/each}
      </div>
    {/each}
  </div>

  <div class="ds-foot">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button
      onclick={() =>
        onApply({
          decoration: selDec,
          ring: selRing,
          dc: cwSel,
          speed: sp,
          dir: dr,
          glow: gl,
          width: w,
          // A dial the selection cannot use is not saved. The rider matters:
          // it may be an uploaded 32KB sprite, and leaving it set after you
          // switch to a drawn crown means broadcasting that sprite to everyone
          // who sees your profile, forever, to render nothing.
          sat: hasRider(selRing) ? st : "",
          pal: hasPalette(selRing) ? pl : "",
        })}>Apply</button
    >
  </div>
</div>

<style>
  .ds-scrim {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.55);
    z-index: 60;
  }
  .ds {
    position: fixed;
    inset: 50% auto auto 50%;
    transform: translate(-50%, -50%);
    width: min(560px, 94vw);
    max-height: 88vh;
    display: flex;
    flex-direction: column;
    /* Matches modals/Modal.svelte deliberately. These panels had drifted to
       --bg-2 on a 0.4 scrim with no shadow, which put them BELOW the surface
       every other dialog in the app sits on and left them reading as
       half-transparent — the panel edge fell too close in value to the page
       behind it to register as an edge at all. */
    background: var(--bg-elevated);
    box-shadow: var(--shadow-pop);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    z-index: 61;
    overflow: hidden;
  }
  .ds-head {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px 14px;
    border-bottom: 1px solid var(--border);
  }
  .ds-head .tiny {
    margin-left: auto;
  }
  .preview {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 18px 16px;
    border-bottom: 1px solid var(--border);
  }
  .preview p {
    margin: 0;
    line-height: 1.5;
  }
  .tune {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 12px 14px;
    border-bottom: 1px solid var(--border);
  }
  .pane {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .shelf {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .cw-btn {
    position: relative;
    display: grid;
    place-items: center;
    width: 30px;
    height: 30px;
    padding: 0;
    border: 2px solid transparent;
    border-radius: 50%;
    box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.25);
  }
  .cw-btn.on {
    border-color: var(--accent);
    box-shadow: 0 0 0 2px var(--accent-soft);
  }
  /* The two non-colour choices are not a colour, so they say what they are.
     Nine pixels of text on a 30px dot is small, but the alternative is two
     swatches that look like colourways and are not. */
  .mark {
    font-size: 8px;
    font-weight: 700;
    letter-spacing: 0.02em;
    color: #10131a;
    text-shadow: 0 0 2px rgba(255, 255, 255, 0.75);
    pointer-events: none;
  }
  .library {
    overflow-y: auto;
    /* The scroll container clips: a satellite swinging wide must never toggle a
       horizontal scrollbar (the old "pulsating bar" bug). */
    overflow-x: clip;
    padding: 12px 14px 4px;
  }
  .section {
    font-size: var(--fs-compact);
    font-weight: 600;
    color: var(--text);
    margin: 16px 0 2px;
    padding-bottom: 6px;
    border-bottom: 1px solid var(--border);
  }
  .section:first-of-type {
    margin-top: 10px;
  }
  .gtitle {
    font-size: var(--fs-tiny);
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-muted);
    margin: 14px 0 8px;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(74px, 1fr));
    gap: 8px;
  }
  .opt {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    padding: 10px 4px 8px;
    background: var(--bg-1);
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    color: var(--text);
    font: inherit;
    cursor: pointer;
  }
  .opt:hover {
    border-color: var(--border);
  }
  .opt.sel {
    border-color: var(--accent);
  }
  /* Rings and their weather overflow the avatar by design, so their tiles need
     room and must not clip. Offscreen ones skip rendering entirely — 46 live
     rings is a lot of animation to keep warm. */
  .grid.rings .opt {
    content-visibility: auto;
    contain-intrinsic-size: 88px 66px;
    padding: 12px 6px 8px;
  }
  /* Same deal as the banner picker: rings spin under the cursor and on your
     current pick — not all 46 at once, forever. */
  .grid.rings .opt:not(:hover):not(.sel) :global(.fxfield) {
    display: none;
  }
  .grid.rings .opt:not(:hover):not(.sel) :global(.ring .art),
  .grid.rings .opt:not(:hover):not(.sel) :global(.ring .orbit),
  .grid.rings .opt:not(:hover):not(.sel) :global(.ring .halo),
  .grid.rings .opt:not(:hover):not(.sel) :global(.ring .sat) {
    animation-play-state: paused;
  }
  .opt.none {
    flex-direction: row;
    justify-content: center;
    gap: 8px;
    width: 100%;
    padding: 10px;
  }
  .none-dot {
    width: 22px;
    height: 22px;
    border-radius: 50%;
    border: 1px dashed var(--text-faint);
  }
  .oname {
    font-size: var(--fs-tiny);
    color: var(--text-muted);
    text-align: center;
    line-height: 1.25;
  }
  .ds-foot {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    padding: 12px 14px;
    border-top: 1px solid var(--border);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .cw-btn {
      width: var(--tap-min);
      height: var(--tap-min);
    }
    .mark {
      font-size: 10px;
    }
    .grid {
      grid-template-columns: repeat(auto-fill, minmax(84px, 1fr));
    }
    .ds-foot button {
      flex: 1;
      min-height: var(--tap-min);
    }
  }
</style>
