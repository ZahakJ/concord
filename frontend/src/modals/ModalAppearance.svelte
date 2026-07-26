<script>
  // Appearance: theme (Dark / Light / System), accent color, and message
  // density. Everything is device-local (S.prefs) and applies live — no save
  // step. setAppearance persists the pref and stamps <html> (data-theme /
  // data-density / --accent) with a short cross-fade; see state.svelte.js.
  import Modal from "./Modal.svelte";
  import { S, setAppearance } from "../lib/state.svelte.js";

  let { onClose } = $props();

  const THEMES = [
    { id: "dark", label: "Dark" },
    { id: "light", label: "Light" },
    { id: "system", label: "System" },
  ];

  // Six presets + "Profile" (accent pref "" = follow the custom color from
  // Edit profile, which is the pre-Appearance behavior and the default).
  const ACCENTS = [
    { name: "Concord teal", color: "#14a394" },
    { name: "Azure", color: "#3d7dd6" },
    { name: "Iris", color: "#7a6ff0" },
    { name: "Orchid", color: "#b45ecf" },
    { name: "Rose", color: "#d45577" },
    { name: "Ember", color: "#cd6b3a" },
  ];

  const theme = $derived(S.prefs.theme || "dark");
  const accent = $derived(S.prefs.accent || "");
  const density = $derived(S.prefs.density || "cozy");
  const clock = $derived(S.prefs.clock || "system");
  const profileColor = $derived(S.identity.color || "#14a394");
  const themePack = $derived(S.prefs.themePack || "");
  const shape = $derived(S.prefs.shape || "");
  const font = $derived(S.prefs.font || "");

  // Shape + typeface are theme axes of their own; "" defers to the pack, which
  // now carries its own corner radius and UI face (see app.css).
  const SHAPES = [
    { id: "", label: "Theme", r: "10px" },
    { id: "sharp", label: "Sharp", r: "2px" },
    { id: "soft", label: "Soft", r: "8px" },
    { id: "round", label: "Round", r: "16px" },
  ];
  // Four faces, each genuinely different everywhere. (A "grotesk" option was
  // cut: on most Linux installs Helvetica/Arial resolve to the same substitute
  // as system-ui, so it would have been a choice that changes nothing.)
  const MONO = 'ui-monospace, "SF Mono", Menlo, Consolas, monospace';
  const FONTS = [
    { id: "", label: "Theme", stack: "inherit" },
    { id: "system", label: "System", stack: 'system-ui, -apple-system, "Segoe UI", Roboto, sans-serif' },
    { id: "serif", label: "Serif", stack: 'Georgia, "Iowan Old Style", "Times New Roman", serif' },
    { id: "mono", label: "Mono", stack: MONO },
  ];

  // Curated full-palette skins (see app.css [data-theme-pack=…]). The preview
  // colors mirror each pack's bg-1/bg-3/accent tokens.
  const PACKS = [
    { id: "", label: "Default", bg: "#16181c", hi: "#282c33", ac: "#14a394" },
    { id: "midnight", label: "Midnight", bg: "#111527", hi: "#222946", ac: "#6f7cff" },
    { id: "nebula", label: "Nebula", bg: "#191129", hi: "#2e2149", ac: "#a06bff" },
    { id: "sakura", label: "Sakura", bg: "#24141e", hi: "#402637", ac: "#f06ba8" },
    { id: "forest", label: "Forest", bg: "#111c16", hi: "#22342a", ac: "#3fb96e" },
    { id: "abyss", label: "Abyss", bg: "#070709", hi: "#17181d", ac: "#22d3ee" },
    { id: "nord", label: "Nord", bg: "#2e3440", hi: "#434c5e", ac: "#88c0d0" },
    { id: "dracula", label: "Dracula", bg: "#282a36", hi: "#424456", ac: "#bd93f9" },
    { id: "gruvbox", label: "Gruvbox", bg: "#282828", hi: "#3c3836", ac: "#fabd2f" },
    { id: "rose", label: "Rosé", bg: "#1f1d2e", hi: "#2f2b45", ac: "#ebbcba" },
    { id: "oceanic", label: "Oceanic", bg: "#16232b", hi: "#294049", ac: "#5ec8cc" },
  ];

  // Textured packs: a static coloured mesh glows through translucent surfaces —
  // richer than a flat palette, but zero animation cost. `grad` drives the card.
  const TEXTURE_PACKS = [
    { id: "frost", label: "Frost", bg: "#111b26", hi: "#1c2a3a", ac: "#7dd3fc", grad: "radial-gradient(circle at 20% 20%,#38bdf8,transparent 55%),radial-gradient(circle at 85% 70%,#2dd4bf,#0d1a26)" },
    { id: "dusk", label: "Dusk", bg: "#26181c", hi: "#382428", ac: "#fb7185", grad: "radial-gradient(circle at 18% 18%,#fb923c,transparent 55%),radial-gradient(circle at 82% 75%,#a855f7,#241318)" },
    { id: "grape", label: "Grape", bg: "#1e162a", hi: "#2c203c", ac: "#c084fc", grad: "radial-gradient(circle at 20% 20%,#c084fc,transparent 55%),radial-gradient(circle at 85% 72%,#ec4899,#180f24)" },
  ];

  // Animated packs: a live backdrop moves behind translucent surfaces. The mini
  // preview animates too (see .pk.live styles) so the card sells the effect.
  const LIVE_PACKS = [
    { id: "aurora", label: "Aurora", bg: "#06202a", hi: "#123d43", ac: "#39d9b0", grad: "linear-gradient(135deg,#31e0a0,#22d3ee 55%,#7b6cff)" },
    { id: "synthwave", label: "Synthwave", bg: "#180a28", hi: "#360c4e", ac: "#ff4fd8", grad: "linear-gradient(180deg,#ffd873,#ff7ec9 55%,#2de2e6)" },
    { id: "cosmos", label: "Cosmos", bg: "#070818", hi: "#161a32", ac: "#7c8bff", grad: "radial-gradient(circle at 40% 40%,#7c8bff,#be6eff 60%,#0a0b16)" },
    { id: "molten", label: "Molten", bg: "#180c06", hi: "#301a10", ac: "#ff7a2f", grad: "linear-gradient(0deg,#ff5722,#ff7a2f 55%,#ffb35a)" },
  ];
</script>

<Modal title="Appearance" {onClose} wide>
  <section>
    <strong class="label">Theme</strong>
    <div class="theme-row" role="radiogroup" aria-label="Theme">
      {#each THEMES as t (t.id)}
        <button
          class="theme-card"
          class:sel={theme === t.id}
          role="radio"
          aria-checked={theme === t.id}
          onclick={() => setAppearance("theme", t.id)}
        >
          <!-- Mini preview painted with fixed colors, so the Light card shows
               light even while the app is dark (and vice versa). -->
          <span class="pv {t.id}" aria-hidden="true">
            <span class="pv-dot"></span>
            <span class="pv-lines"><span class="l1"></span><span class="l2"></span></span>
          </span>
          {t.label}
        </button>
      {/each}
    </div>
    <p class="muted tiny">System follows your OS setting, live.</p>
  </section>

  <hr />
  <section>
    <strong class="label">Theme pack</strong>
    <div class="pack-row" role="radiogroup" aria-label="Theme pack">
      {#each PACKS as p (p.id)}
        <button
          class="pack-card"
          class:sel={themePack === p.id}
          role="radio"
          aria-checked={themePack === p.id}
          onclick={() => setAppearance("themePack", p.id)}
        >
          <span class="pk" style="--pk-bg:{p.bg};--pk-hi:{p.hi};--pk-ac:{p.ac}" aria-hidden="true">
            <span class="pk-rail"></span>
            <span class="pk-body">
              <span class="pk-ac"></span>
              <span class="pk-line"></span>
              <span class="pk-line short"></span>
            </span>
          </span>
          {p.label}
        </button>
      {/each}
    </div>
    <p class="muted tiny">
      A full palette for the whole app — each pack brings its own accent (an
      accent preset below still overrides it), and several reshape it too:
      Gruvbox and Dracula go monospaced and square, Sakura and Rosé round over.
    </p>

    <div class="live-head">
      <span class="live-tag">✨ Animated</span>
      <span class="muted tiny">A living backdrop drifts behind the app.</span>
    </div>
    <div class="pack-row" role="radiogroup" aria-label="Animated theme pack">
      {#each LIVE_PACKS as p (p.id)}
        <button
          class="pack-card"
          class:sel={themePack === p.id}
          role="radio"
          aria-checked={themePack === p.id}
          onclick={() => setAppearance("themePack", p.id)}
        >
          <span class="pk live" style="--pk-bg:{p.bg};--pk-hi:{p.hi};--pk-ac:{p.ac};--pk-grad:{p.grad}" aria-hidden="true">
            <span class="pk-glow"></span>
            <span class="pk-rail"></span>
            <span class="pk-body">
              <span class="pk-ac"></span>
              <span class="pk-line"></span>
              <span class="pk-line short"></span>
            </span>
          </span>
          {p.label}
        </button>
      {/each}
    </div>

    <div class="live-head">
      <span class="live-tag texture-tag">▦ Textured</span>
      <span class="muted tiny">A soft colour mesh, no animation.</span>
    </div>
    <div class="pack-row" role="radiogroup" aria-label="Textured theme pack">
      {#each TEXTURE_PACKS as p (p.id)}
        <button
          class="pack-card"
          class:sel={themePack === p.id}
          role="radio"
          aria-checked={themePack === p.id}
          onclick={() => setAppearance("themePack", p.id)}
        >
          <span class="pk live textured" style="--pk-bg:{p.bg};--pk-hi:{p.hi};--pk-ac:{p.ac};--pk-grad:{p.grad}" aria-hidden="true">
            <span class="pk-glow"></span>
            <span class="pk-rail"></span>
            <span class="pk-body">
              <span class="pk-ac"></span>
              <span class="pk-line"></span>
              <span class="pk-line short"></span>
            </span>
          </span>
          {p.label}
        </button>
      {/each}
    </div>
  </section>

  <hr />
  <section>
    <strong class="label">Accent</strong>
    <div class="swatches" role="radiogroup" aria-label="Accent color">
      {#each ACCENTS as a (a.color)}
        <button
          class="swatch"
          class:sel={accent === a.color}
          role="radio"
          aria-checked={accent === a.color}
          title={a.name}
          aria-label={a.name}
          style="--sw:{a.color}"
          onclick={() => setAppearance("accent", a.color)}
        ></button>
      {/each}
      <button
        class="swatch profile"
        class:sel={accent === ""}
        role="radio"
        aria-checked={accent === ""}
        title="Your profile color"
        aria-label="Your profile color"
        style="--sw:{profileColor}"
        onclick={() => setAppearance("accent", "")}
      ></button>
    </div>
    <p class="muted tiny">
      The hollow swatch follows your profile's custom color (Edit profile) —
      pick a preset to override it on this device only.
    </p>
  </section>

  <hr />
  <section>
    <strong class="label">Corners</strong>
    <div class="seg four" role="radiogroup" aria-label="Corner style">
      {#each SHAPES as s (s.id)}
        <button
          class:sel={shape === s.id}
          role="radio"
          aria-checked={shape === s.id}
          onclick={() => setAppearance("shape", s.id)}
        >
          <span class="shape-pv" style="--r:{s.r}" aria-hidden="true"></span>
          {s.label}
        </button>
      {/each}
    </div>
    <p class="muted tiny">
      How rounded every panel, button and field is. <em>Theme</em> follows the
      pack you picked above — Gruvbox squares off, Sakura rounds over.
    </p>
  </section>

  <section>
    <strong class="label">Typeface</strong>
    <div class="seg four" role="radiogroup" aria-label="Typeface">
      {#each FONTS as f (f.id)}
        <button
          class:sel={font === f.id}
          role="radio"
          aria-checked={font === f.id}
          onclick={() => setAppearance("font", f.id)}
        >
          <span class="font-pv" style="font-family:{f.stack}" aria-hidden="true">Ag</span>
          {f.label}
        </button>
      {/each}
    </div>
    <p class="muted tiny">
      Only faces already on this machine — Concord never downloads a font, which
      would tell a font host every time you open the app.
    </p>
  </section>

  <hr />
  <section>
    <strong class="label">Message density</strong>
    <div class="seg" role="radiogroup" aria-label="Message density">
      <button
        class:sel={density === "cozy"}
        role="radio"
        aria-checked={density === "cozy"}
        onclick={() => setAppearance("density", "cozy")}
      >
        <span class="rows cozy" aria-hidden="true"><span></span><span></span><span></span></span>
        Cozy
      </button>
      <button
        class:sel={density === "compact"}
        role="radio"
        aria-checked={density === "compact"}
        onclick={() => setAppearance("density", "compact")}
      >
        <span class="rows compact" aria-hidden="true"
          ><span></span><span></span><span></span><span></span></span
        >
        Compact
      </button>
    </div>
    <p class="muted tiny">Compact tightens the space between messages in the feed.</p>
  </section>

  <section>
    <strong class="label">Clock</strong>
    <div class="seg three" role="radiogroup" aria-label="Timestamp format">
      {#each [["system", "Automatic"], ["12", "12-hour"], ["24", "24-hour"]] as [id, label] (id)}
        <button
          class:sel={clock === id}
          role="radio"
          aria-checked={clock === id}
          onclick={() => setAppearance("clock", id)}
        >
          {label}
        </button>
      {/each}
    </div>
    <p class="muted tiny">How message timestamps show the time of day.</p>
  </section>

  <div class="actions">
    <button onclick={onClose}>Done</button>
  </div>
</Modal>

<style>
  section {
    display: flex;
    flex-direction: column;
    gap: 8px;
    text-align: left;
  }
  /* Match the sectioned settings look: small uppercase group labels. */
  .label {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  p {
    margin: 0;
    font-size: 13px;
    line-height: 1.5;
  }
  .tiny {
    font-size: 11px;
  }
  hr {
    border: none;
    border-top: 1px solid var(--border);
    margin: 4px 0;
  }

  /* Theme cards: a mini window per mode, selected one ringed in accent. */
  .theme-row {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 8px;
  }
  .theme-card {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 6px;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--text-muted);
    font-size: 12px;
    align-items: center;
  }
  .theme-card:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .theme-card.sel {
    border-color: var(--accent);
    color: var(--text);
    background: var(--accent-soft);
  }
  .pv {
    width: 100%;
    height: 44px;
    border-radius: 7px;
    border: 1px solid var(--border);
    display: flex;
    gap: 6px;
    padding: 8px;
    overflow: hidden;
    /* Dark preview colors (defaults); the light card overrides them and the
       system card splits itself between the two. */
    --pv-bg: #16181c;
    --pv-line: #3a3f47;
    background: var(--pv-bg);
  }
  .pv.light {
    --pv-bg: #f2f3f6;
    --pv-line: #c9cdd4;
  }
  .pv.system {
    background: linear-gradient(115deg, #16181c 50%, #f2f3f6 50%);
  }
  .pv-dot {
    width: 12px;
    height: 12px;
    border-radius: 50%;
    background: var(--accent);
    flex-shrink: 0;
  }
  .pv-lines {
    display: flex;
    flex-direction: column;
    gap: 5px;
    flex: 1;
    padding-top: 1px;
  }
  .pv-lines span {
    height: 4px;
    border-radius: 2px;
    background: var(--pv-line);
  }
  /* On the split (system) card, keep the bars legible over both halves. */
  .pv.system .pv-lines span {
    background: color-mix(in srgb, #868c98 70%, transparent);
  }
  .pv-lines .l1 {
    width: 85%;
  }
  .pv-lines .l2 {
    width: 55%;
  }

  /* Theme-pack cards: a mini app-window painted in the pack's own palette. */
  .pack-row {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 8px;
  }
  .pack-card {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 6px;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--text-muted);
    font-size: 12px;
    align-items: center;
  }
  .pack-card:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .pack-card.sel {
    border-color: var(--accent);
    color: var(--text);
    background: var(--accent-soft);
  }
  .pk {
    width: 100%;
    height: 40px;
    border-radius: 7px;
    border: 1px solid rgba(255, 255, 255, 0.07);
    background: var(--pk-bg);
    display: flex;
    overflow: hidden;
  }
  .pk-rail {
    width: 12px;
    background: color-mix(in srgb, var(--pk-bg) 55%, black);
    flex: none;
  }
  .pk-body {
    flex: 1;
    padding: 7px 8px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .pk-ac {
    width: 16px;
    height: 5px;
    border-radius: 3px;
    background: var(--pk-ac);
  }
  .pk-line {
    height: 4px;
    border-radius: 2px;
    width: 85%;
    background: var(--pk-hi);
  }
  .pk-line.short {
    width: 55%;
  }

  /* Animated-pack subsection heading + the live preview shimmer. */
  .live-head {
    display: flex;
    align-items: baseline;
    gap: 8px;
    margin-top: 12px;
    flex-wrap: wrap;
  }
  .live-tag {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--accent);
  }
  .pk.live {
    position: relative;
    /* Keep the mini window's internal layering to itself. Without this, the
       z-index below is measured against the whole dialog and the preview cards
       paint OVER the sticky "Appearance" header as you scroll. */
    isolation: isolate;
  }
  /* A soft blob of the pack's own gradient drifting behind the mini window —
     the same idea as the real backdrop, in miniature. */
  .pk-glow {
    position: absolute;
    inset: -30%;
    background: var(--pk-grad);
    opacity: 0.55;
    filter: blur(6px);
    animation: pk-drift 6s ease-in-out infinite alternate;
  }
  .pk.live .pk-rail,
  .pk.live .pk-body {
    position: relative;
    z-index: 1;
  }
  /* Let the glow read through the mini surfaces, like the real translucent UI. */
  .pk.live .pk-rail {
    background: color-mix(in srgb, var(--pk-bg) 78%, transparent);
  }
  .pk.live .pk-line {
    background: color-mix(in srgb, var(--pk-hi) 85%, transparent);
  }
  @keyframes pk-drift {
    0% {
      transform: translate(-8%, -6%) scale(1.05);
    }
    100% {
      transform: translate(8%, 6%) scale(1.25);
    }
  }
  /* Textured previews: the same mesh, but held still (matches the real theme). */
  .pk.textured .pk-glow {
    inset: 0;
    opacity: 0.7;
    filter: blur(3px);
    animation: none;
  }
  @media (prefers-reduced-motion: reduce) {
    .pk-glow {
      animation: none;
    }
  }

  /* Accent swatches: filled dots; the profile one is hollow (a ring of the
     profile color) so it reads as "custom", not just another preset. */
  .swatches {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
  }
  .swatch {
    width: 28px;
    height: 28px;
    padding: 0;
    border-radius: 50%;
    background: var(--sw);
    border: 2px solid transparent;
    transition: transform 0.12s ease;
  }
  .swatch:hover {
    background: var(--sw);
    transform: scale(1.12);
  }
  .swatch.sel {
    /* Ring: gap between the dot and an outline in the swatch's own color. */
    border-color: var(--bg-elevated);
    box-shadow: 0 0 0 2px var(--sw);
  }
  .swatch.profile {
    background: transparent;
    border-color: var(--sw);
    border-width: 3px;
  }
  .swatch.profile:hover {
    background: color-mix(in srgb, var(--sw) 25%, transparent);
  }
  .swatch.profile.sel {
    background: var(--sw);
    border-color: var(--bg-elevated);
  }

  /* Density: segmented pair with a tiny line-rhythm glyph in each option. */
  .seg {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 8px;
  }
  .seg > button {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 12px;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--text-muted);
    font-size: 13px;
  }
  .seg > button:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .seg > button.sel {
    border-color: var(--accent);
    background: var(--accent-soft);
    color: var(--text);
  }
  /* Exact column counts rather than auto-fit: a row of options that wraps to
     leave one orphan on its own line reads as a mistake. Each group states how
     many it has, so every row fills. */
  .seg.three {
    grid-template-columns: repeat(3, 1fr);
  }
  .seg.four {
    grid-template-columns: repeat(4, 1fr);
  }
  /* The option rows are tight at four across; center them and let the label
     shrink before the preview does. */
  .seg.four > button {
    justify-content: center;
    gap: 7px;
    padding: 8px 6px;
  }
  /* Corner preview: a swatch drawn at the radius it's offering. */
  .shape-pv {
    width: 22px;
    height: 22px;
    flex: none;
    border: 1.5px solid currentColor;
    border-radius: var(--r);
    opacity: 0.7;
  }
  /* Type preview, set in the face itself — the only honest sample. */
  .font-pv {
    width: 22px;
    flex: none;
    font-size: 15px;
    line-height: 1;
    text-align: center;
    opacity: 0.85;
  }
  .rows {
    display: flex;
    flex-direction: column;
    justify-content: center;
    width: 26px;
    height: 24px;
  }
  .rows.cozy {
    gap: 5px;
  }
  .rows.compact {
    gap: 2px;
  }
  .rows span {
    height: 3px;
    border-radius: 2px;
    background: currentColor;
    opacity: 0.65;
  }
  .rows span:nth-child(2n) {
    width: 70%;
  }
  /* Finger-sized pickers on touch. */
  @media (pointer: coarse) {
    .swatch {
      width: 36px;
      height: 36px;
    }
    .theme-card {
      padding: 10px 8px;
      font-size: 13px;
    }
    .seg > button {
      min-height: 48px;
    }
  }
</style>
