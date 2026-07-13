<script>
  // The avatar-ring editor: pick from the library — weather, cosmic, fire,
  // orbiting friends — then tune it (speed, direction, glow, thickness) with a
  // live preview at the top. Pick "Your satellite" and an emoji (or your own
  // uploaded picture) orbits your face.
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import { RING_BY_ID, RING_GROUPS, SATELLITES, PALETTES, hasRider, hasPalette } from "./lib/rings.js";

  let {
    ring = "",
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

  let sel = $state(ring);
  let sp = $state(speed);
  let dr = $state(dir);
  let gl = $state(glow);
  let w = $state(width);
  let st = $state(sat); // the rider: "" = a plain dot
  let pl = $state(pal || "yours"); // the Gradient ring's colorway
  let fileInput;

  const dials = $derived({ speed: sp, dir: dr, glow: gl, width: w, sat: st, pal: pl });
  // Configs appear only for the rings they belong to: the rider shelf for
  // anything that orbits, the palette shelf for the Gradient ring.
  const rider = $derived(hasRider(sel));
  const palette = $derived(hasPalette(sel));
  const animated = $derived(sel !== "" && sel !== "theme-solid");

  // Your own satellite: downscale to a 64px sprite so it costs ~2KB on the
  // wire, not a megabyte — it orbits at ~14px on screen.
  function loadSat(file) {
    if (!file || !file.type.startsWith("image/")) return;
    const img = new Image();
    const url = URL.createObjectURL(file);
    img.onload = () => {
      URL.revokeObjectURL(url);
      const cv = document.createElement("canvas");
      cv.width = cv.height = 64;
      const ctx = cv.getContext("2d");
      const s = Math.min(img.naturalWidth, img.naturalHeight);
      ctx.drawImage(img, (img.naturalWidth - s) / 2, (img.naturalHeight - s) / 2, s, s, 0, 0, 64, 64);
      st = cv.toDataURL("image/png");
      if (!hasRider(sel)) sel = "orbit"; // your picture needs something to ride
    };
    img.onerror = () => URL.revokeObjectURL(url);
    img.src = url;
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="rs-scrim" onclick={(e) => e.target === e.currentTarget && onClose()}>
  <div class="rs" role="dialog" aria-label="Avatar ring">
    <div class="rs-head">
      <strong>Avatar ring</strong>
      <button class="x" onclick={onClose} aria-label="Close"><Icon name="close" size={14} /></button>
    </div>

    <div class="stage">
      <Avatar {name} {emoji} {color} image={avatar} size={76} frame={sel} style={dials} {color2} />
      <span class="stage-name">{RING_BY_ID[sel]?.name || "None"}</span>
    </div>

    {#if palette}
      <div class="sat-pane">
        <span class="tiny muted">Colorway</span>
        <div class="sats">
          {#each PALETTES as p (p.id)}
            <button
              class="pal-btn"
              class:on={pl === p.id}
              title={p.name}
              onclick={() => (pl = p.id)}
              style={`background:conic-gradient(from 0deg, ${p.stops.join(", ")});--c1:${color};--c2:${color2 || color}`}
              aria-label={p.name}
            ></button>
          {/each}
        </div>
        <span class="tiny muted">{PALETTES.find((p) => p.id === pl)?.name}</span>
      </div>
    {/if}

    {#if rider}
      <div class="sat-pane">
        <span class="tiny muted">Rider — who goes around with you?</span>
        <div class="sats">
          <button class="sat-btn" class:on={st === ""} onclick={() => (st = "")} title="Just a dot">
            <span class="dot-sat"></span>
          </button>
          <button class="sat-btn upload" onclick={() => fileInput.click()} title="Upload your own">
            <Icon name="attach" size={14} />
          </button>
          {#if st.startsWith("data:")}
            <button class="sat-btn on" title="Your picture">
              <img src={st} alt="" />
            </button>
          {/if}
          {#each SATELLITES as s (s)}
            <button class="sat-btn" class:on={st === s} onclick={() => (st = s)}>{s}</button>
          {/each}
        </div>
        <input
          type="file"
          accept="image/*"
          bind:this={fileInput}
          style="display:none"
          onchange={(e) => {
            loadSat(e.target.files?.[0]);
            e.target.value = "";
          }}
        />
      </div>
    {/if}

    {#if animated}
      <div class="dials">
        <div class="dg">
          <span class="tiny muted">Speed</span>
          <div class="seg">
            {#each ["slow", "normal", "fast"] as s (s)}
              <button class:on={sp === s} onclick={() => (sp = s)}>{s}</button>
            {/each}
          </div>
        </div>
        <div class="dg">
          <span class="tiny muted">Direction</span>
          <div class="seg">
            <button class:on={dr === "cw"} onclick={() => (dr = "cw")}>↻ cw</button>
            <button class:on={dr === "ccw"} onclick={() => (dr = "ccw")}>↺ ccw</button>
          </div>
        </div>
        <div class="dg">
          <span class="tiny muted">Glow</span>
          <div class="seg">
            {#each ["off", "soft", "strong"] as g (g)}
              <button class:on={gl === g} onclick={() => (gl = g)}>{g}</button>
            {/each}
          </div>
        </div>
        <label class="dg">
          <span class="tiny muted">Thickness · {w}px</span>
          <input type="range" min="1" max="5" step="1" bind:value={w} />
        </label>
      </div>
    {/if}

    <div class="library">
      <button class="opt none" class:sel={sel === ""} onclick={() => (sel = "")}>
        <span class="none-dot"></span>
        <span class="oname">None</span>
      </button>
      {#each RING_GROUPS as g (g.title)}
        <div class="gtitle">{g.title}</div>
        <div class="grid">
          {#each g.ids as id (id)}
            <button class="opt" class:sel={sel === id} onclick={() => (sel = id)} title={RING_BY_ID[id]?.name}>
              <!-- Your own face in every tile: you're choosing how YOU look. -->
              <Avatar {name} {emoji} {color} image={avatar} size={36} frame={id} style={dials} {color2} />
              <span class="oname">{RING_BY_ID[id]?.name}</span>
            </button>
          {/each}
        </div>
      {/each}
    </div>

    <div class="rs-foot">
      <button class="ghost" onclick={onClose}>Cancel</button>
      <button
        onclick={() =>
          onApply({
            ring: sel,
            speed: sp,
            dir: dr,
            glow: gl,
            width: w,
            sat: rider ? st : "",
            pal: palette ? pl : "",
          })}
      >
        Apply
      </button>
    </div>
  </div>
</div>

<style>
  .rs-scrim {
    position: fixed;
    inset: 0;
    z-index: 400;
    /* See BannerStudio: no full-screen backdrop blur over live animation. */
    background: rgba(0, 0, 0, 0.68);
    display: grid;
    place-items: center;
    padding: 4vh 4vw;
  }
  .rs {
    width: 600px;
    max-width: 94vw;
    max-height: 88vh;
    display: flex;
    flex-direction: column;
    gap: 10px;
    background: var(--bg-elevated, var(--bg-1));
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
    padding: 14px;
  }
  .rs-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .x {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    padding: 0;
    line-height: 0;
    border: none;
    border-radius: 50%;
    background: transparent;
    color: var(--text-muted);
  }
  .x:hover {
    background: var(--bg-3);
  }
  .stage {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    padding: 18px 0 8px;
    background: var(--bg-0);
    border-radius: var(--radius-md);
    overflow: clip; /* rings and their weather overflow by design */
  }
  .stage-name {
    font-size: 12px;
    color: var(--text-muted);
  }
  .sat-pane {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }
  .sats {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }
  .sat-btn {
    display: grid;
    place-items: center;
    width: 30px;
    height: 30px;
    padding: 0;
    font-size: 16px;
    line-height: 1;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: transparent;
  }
  .sat-btn:hover {
    background: var(--bg-3);
  }
  .sat-btn.on {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .sat-btn img {
    width: 22px;
    height: 22px;
    border-radius: 50%;
    object-fit: cover;
  }
  .sat-btn.upload {
    color: var(--text-muted);
    border-style: dashed;
  }
  .dot-sat {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--text-muted);
  }
  .pal-btn {
    width: 30px;
    height: 30px;
    padding: 0;
    border: 2px solid transparent;
    border-radius: 50%;
  }
  .pal-btn.on {
    border-color: var(--accent);
    box-shadow: 0 0 0 2px var(--accent-soft);
  }
  .dials {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 8px;
  }
  .dg {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .seg {
    display: flex;
    gap: 4px;
  }
  .seg button {
    flex: 1;
    padding: 5px 4px;
    font-size: 11px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
    background: transparent;
    color: var(--text-muted);
    text-transform: capitalize;
  }
  .seg button.on {
    border-color: var(--accent);
    background: var(--accent-soft);
    color: var(--text);
  }
  .library {
    overflow-y: auto;
    /* The scroll container clips the decoration, so a satellite swinging wide
       never toggles a horizontal scrollbar (the old "pulsating bar" bug). */
    overflow-x: clip;
    padding: 2px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .gtitle {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-faint);
    margin-top: 6px;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(6, 1fr);
    gap: 6px;
  }
  /* Room to breathe: rings, weather and riders all overflow the avatar by
     design, so the tile must NOT clip them. Offscreen tiles skip rendering
     entirely (46 live rings is a lot of animation to keep warm). */
  .opt {
    content-visibility: auto;
    contain-intrinsic-size: 88px 66px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    padding: 12px 6px 8px;
    background: transparent;
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
  }
  .opt:hover {
    background: var(--bg-3);
  }
  .opt.sel {
    border-color: var(--accent);
    background: var(--accent-soft);
    color: var(--text);
  }
  /* Same deal as the banner picker: rings spin under the cursor and on your
     current pick — not all 46 at once, forever. */
  .opt:not(:hover):not(.sel) :global(.fxfield) {
    display: none;
  }
  .opt:not(:hover):not(.sel) :global(.ring .art),
  .opt:not(:hover):not(.sel) :global(.ring .orbit),
  .opt:not(:hover):not(.sel) :global(.ring .halo),
  .opt:not(:hover):not(.sel) :global(.ring .sat) {
    animation-play-state: paused;
  }
  .opt.none {
    flex-direction: row;
    justify-content: center;
    gap: 8px;
    padding: 7px;
  }
  .none-dot {
    width: 20px;
    height: 20px;
    border-radius: 50%;
    border: 1px dashed var(--border);
  }
  .oname {
    font-size: 10px;
    line-height: 1.15;
    text-align: center;
  }
  .tiny {
    font-size: 11px;
  }
  .rs-foot {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
</style>
