<script>
  // The gradient ring's tune panel: its colourway, the rider that goes around
  // with you, and the four dials (speed, direction, glow, thickness).
  //
  // This was the whole reason a gradient ring had a door of its own. It does
  // not any more — a ring of runes and a pair of ears are the same question
  // asked twice, so everything worn is picked in one place (DecorStudio) — but
  // the dials still have to be somewhere, and they only mean anything while a
  // gradient ring is the thing selected. So they are a panel that appears for
  // that selection rather than a picker.
  import Icon from "./Icon.svelte";
  import { SATELLITES, PALETTES, hasRider, hasPalette } from "./lib/rings.js";
  import { emojiName } from "./lib/emoji.js";

  let {
    ring = $bindable(""),
    speed = $bindable("normal"),
    dir = $bindable("cw"),
    glow = $bindable("soft"),
    width = $bindable(2),
    sat = $bindable(""),
    pal = $bindable("yours"),
    color = "",
    color2 = "",
  } = $props();

  let fileInput;

  // Each pane appears only for the rings it belongs to: the rider shelf for
  // anything that orbits, the colourway shelf for the Gradient ring.
  const rider = $derived(hasRider(ring));
  const palette = $derived(hasPalette(ring));
  const animated = $derived(ring !== "" && ring !== "theme-solid");

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
      sat = cv.toDataURL("image/png");
      if (!hasRider(ring)) ring = "orbit"; // your picture needs something to ride
    };
    img.onerror = () => URL.revokeObjectURL(url);
    img.src = url;
  }
</script>

{#if palette}
  <div class="pane">
    <span class="tiny muted">Colourway</span>
    <div class="shelf">
      {#each PALETTES as p (p.id)}
        <button
          type="button"
          class="pal-btn"
          class:on={pal === p.id}
          title={p.name}
          onclick={() => (pal = p.id)}
          style={`background:conic-gradient(from 0deg, ${p.stops.join(", ")});--c1:${color};--c2:${color2 || color}`}
          aria-label={p.name}
        ></button>
      {/each}
    </div>
    <span class="tiny muted">{PALETTES.find((p) => p.id === pal)?.name}</span>
  </div>
{/if}

{#if rider}
  <div class="pane">
    <span class="tiny muted">Rider</span>
    <div class="shelf">
      <button type="button" class="sat-btn" class:on={sat === ""} onclick={() => (sat = "")} title="Just a dot">
        <span class="dot-sat"></span>
      </button>
      <button type="button" class="sat-btn upload" onclick={() => fileInput.click()} title="Upload your own">
        <Icon name="attach" size={14} />
      </button>
      {#if sat.startsWith("data:")}
        <button type="button" class="sat-btn on" title="Your picture">
          <img src={sat} alt="" />
        </button>
      {/if}
      {#each SATELLITES as s (s)}
        <!-- The glyph IS the choice, so it has to be the label too — a row of
             thirty-two buttons all announced as "button" is not a chooser. -->
        <button
          type="button"
          class="sat-btn"
          class:on={sat === s}
          aria-label={emojiName(s) || "Satellite"}
          onclick={() => (sat = s)}>{s}</button>
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
      <div class="seg" role="group" aria-label="Speed">
        {#each ["slow", "normal", "fast"] as s (s)}
          <button type="button" class:on={speed === s} onclick={() => (speed = s)}>{s}</button>
        {/each}
      </div>
    </div>
    <div class="dg">
      <span class="tiny muted">Direction</span>
      <div class="seg" role="group" aria-label="Direction">
        <button type="button" class:on={dir === "cw"} onclick={() => (dir = "cw")}>↻ cw</button>
        <button type="button" class:on={dir === "ccw"} onclick={() => (dir = "ccw")}>↺ ccw</button>
      </div>
    </div>
    <div class="dg">
      <span class="tiny muted">Glow</span>
      <div class="seg" role="group" aria-label="Glow">
        {#each ["off", "soft", "strong"] as g (g)}
          <button type="button" class:on={glow === g} onclick={() => (glow = g)}>{g}</button>
        {/each}
      </div>
    </div>
    <label class="dg">
      <span class="tiny muted">Thickness · {width}px</span>
      <input type="range" min="1" max="5" step="1" bind:value={width} />
    </label>
  </div>
{/if}

<style>
  .pane {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }
  .shelf {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-1);
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
    border-radius: var(--radius-md);
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
    gap: var(--sp-2);
  }
  .dg {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .seg {
    display: flex;
    gap: var(--sp-1);
  }
  .seg button {
    flex: 1;
    padding: 5px 4px;
    font-size: var(--fs-small);
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
  .tiny {
    font-size: var(--fs-small);
  }

  /* ---- phone: targets a thumb can hit ---- */
  @media (pointer: coarse), (max-width: 768px) {
    .sat-btn,
    .pal-btn {
      width: var(--tap-min);
      height: var(--tap-min);
    }
    .shelf {
      gap: var(--sp-2);
    }
    /* The emoji riders are a grid of glyphs — the tile grows, the glyph doesn't
       need to. */
    .sat-btn img {
      width: 26px;
      height: 26px;
    }
    .seg button {
      min-height: var(--tap-min);
    }
    /* One dial per row: two 11px segmented controls side by side inside a 360px
       sheet gave each of their three buttons about 50px, which is under a
       fingertip once the label is in there too. */
    .dials {
      grid-template-columns: 1fr;
    }
  }
</style>
