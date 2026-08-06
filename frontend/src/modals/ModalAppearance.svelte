<script>
  // Appearance: theme (Dark / Light / System), accent color, and message
  // density. Everything is device-local (S.prefs) and applies live — no save
  // step. setAppearance persists the pref and stamps <html> (data-theme /
  // data-density / --accent) with a short cross-fade; see state.svelte.js.
  import { slide } from "svelte/transition";
  import Modal from "./Modal.svelte";
  import SettingRow from "./SettingRow.svelte";
  import { S, setAppearance } from "../lib/state.svelte.js";

  let { onClose } = $props();

  // Theme and the pack gallery are the whole point of this dialog, so they're
  // what it opens as. Accent, corners, typeface, density and clock are the
  // adjustments you make once and forget — folded away until asked for.
  let custom = $state(false);

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
  const uiScale = $derived(Number(S.prefs.uiScale) || 1);
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
  // Six faces, each unmistakable from the others at a glance. Five are bundled
  // with the app (public/fonts) so picking one costs no network request — see
  // scripts/prep-fonts.mjs — and `stack` is the SAME stack app.css applies, so
  // the "Ag" sample is an honest preview and not an approximation.
  //
  // Atkinson Hyperlegible, Chakra Petch and Comic Neue are in the bundle too
  // but are not offered: prep-fonts.mjs drops the weight from its filenames, so
  // for those three the 700 file overwrote the 400 and bold is indistinguishable
  // from body text (measured: identical rendered ink at both weights). They go
  // back in the list the moment the bundle is regenerated.
  const FONTS = [
    { id: "", label: "Theme", stack: "inherit" },
    { id: "system", label: "System", stack: 'system-ui, -apple-system, "Segoe UI", Roboto, sans-serif' },
    { id: "inter", label: "Inter", stack: '"Inter", system-ui, sans-serif' },
    { id: "grotesk", label: "Grotesk", stack: '"Space Grotesk", system-ui, sans-serif' },
    { id: "serif", label: "Serif", stack: '"Source Serif 4", Georgia, "Iowan Old Style", serif' },
    { id: "rounded", label: "Rounded", stack: '"Nunito", system-ui, sans-serif' },
    { id: "mono", label: "Mono", stack: '"JetBrains Mono", ui-monospace, Menlo, monospace' },
    { id: "hyper", label: "Legible", stack: '"Atkinson Hyperlegible", system-ui, sans-serif' },
    { id: "cyber", label: "Cyber", stack: '"Chakra Petch", system-ui, sans-serif' },
    { id: "comic", label: "Comic", stack: '"Comic Neue", "Comic Sans MS", system-ui, sans-serif' },
  ];

  // The pack gallery. Each row mirrors what app.css actually gives that pack,
  // because the whole point of these cards is that a theme is no longer a
  // recolour: `font` and `r` are the pack's real UI face and corner radius,
  // `av` its avatar shape, `note` the one-word reason it exists.
  //   bg/hi/ac — its --bg-1 / --bg-3 / --accent
  //   rule     — draws the hairline separator packs that use --msg-rule show
  //   card     — draws the message rows as filled cards, like --msg-surface
  const SYS = 'system-ui, -apple-system, "Segoe UI", Roboto, sans-serif';
  const INTER = '"Inter", system-ui, sans-serif';
  const GROTESK = '"Space Grotesk", system-ui, sans-serif';
  const SERIF = '"Source Serif 4", Georgia, serif';
  const NUNITO = '"Nunito", system-ui, sans-serif';
  const MONO = '"JetBrains Mono", ui-monospace, Menlo, monospace';

  const PACKS = [
    { id: "", label: "Default", bg: "#16181c", hi: "#282c33", ac: "#14a394", font: SYS, r: 6, av: "50%", note: "System face" },
    { id: "midnight", label: "Midnight", bg: "#111527", hi: "#222946", ac: "#6f7cff", font: INTER, r: 8, av: "50%", note: "Inter, deep shadow" },
    { id: "nebula", label: "Nebula", bg: "#191129", hi: "#2e2149", ac: "#a06bff", font: GROTESK, r: 10, av: "50%", note: "Grotesk, big glow" },
    { id: "sakura", label: "Sakura", bg: "#24141e", hi: "#402637", ac: "#f06ba8", font: NUNITO, r: 12, av: "50%", card: true, note: "Rounded, message cards" },
    { id: "forest", label: "Forest", bg: "#111c16", hi: "#22342a", ac: "#3fb96e", font: SERIF, r: 4, av: "6px", rule: true, note: "Serif, ruled" },
    { id: "abyss", label: "Abyss", bg: "#060608", hi: "#14151a", ac: "#22d3ee", font: INTER, r: 1, av: "3px", rule: true, note: "True black, wireframe" },
    { id: "nord", label: "Nord", bg: "#373e4b", hi: "#434c5e", ac: "#88c0d0", font: INTER, r: 3, av: "7px", rule: true, note: "Outlined, no fills" },
    { id: "dracula", label: "Dracula", bg: "#282a36", hi: "#424456", ac: "#bd93f9", font: MONO, r: 3, av: "5px", note: "Monospaced" },
    { id: "gruvbox", label: "Gruvbox", bg: "#282828", hi: "#3c3836", ac: "#fabd2f", font: MONO, r: 0, av: "2px", note: "Terminal, packed tight" },
    { id: "rose", label: "Rosé", bg: "#1f1d2e", hi: "#2f2b45", ac: "#ebbcba", font: SERIF, r: 10, av: "50%", card: true, note: "Serif, airy cards" },
    { id: "oceanic", label: "Oceanic", bg: "#16232b", hi: "#294049", ac: "#5ec8cc", font: INTER, r: 7, av: "40%", note: "Inter, squircles" },
  ];

  // The Daylight set: light-ground packs (every pack above is dark). `day`
  // flips the mini window's ink dark-on-light — the default card paints its
  // sample text by mixing the accent toward WHITE, which vanishes on these.
  // Porcelain previews its pastel mesh with a held-still `grad`, exactly like
  // the textured row (in the app its mesh is painted on the root canvas, not
  // .theme-bg — see app.css).
  const DAY_PACKS = [
    { id: "paper", label: "Paper", bg: "#f0eadc", hi: "#e9e2d1", ac: "#9c4a2c", font: SERIF, r: 5, av: "5px", rule: true, day: true, note: "Serif, warm paper" },
    { id: "solarized", label: "Solarized", bg: "#f3ebd3", hi: "#eee8d5", ac: "#268bd2", font: MONO, r: 3, av: "3px", day: true, note: "Monospaced classic" },
    { id: "meadow", label: "Meadow", bg: "#e4eeda", hi: "#dde9d0", ac: "#2b8347", font: NUNITO, r: 12, av: "50%", card: true, day: true, note: "Rounded, soft greens" },
    { id: "porcelain", label: "Porcelain", bg: "#f4f5f9", hi: "#dde2ec", ac: "#5661d8", font: INTER, r: 12, av: "50%", day: true, still: true, grad: "radial-gradient(circle at 18% 18%,#ffb8d2,transparent 55%),radial-gradient(circle at 82% 75%,#a4c6ff,#eef1f6)", note: "Pastel glass" },
  ];

  // Textured packs: a static coloured mesh glows through translucent surfaces —
  // richer than a flat palette, but zero animation cost. `grad` drives the card.
  const TEXTURE_PACKS = [
    { id: "frost", label: "Frost", bg: "#0f1924", hi: "#1a2838", ac: "#7dd3fc", font: INTER, r: 10, av: "50%", grad: "radial-gradient(circle at 20% 20%,#38bdf8,transparent 55%),radial-gradient(circle at 85% 70%,#2dd4bf,#16303f)", note: "Inter, glass" },
    { id: "dusk", label: "Dusk", bg: "#24161a", hi: "#362226", ac: "#fb7185", font: NUNITO, r: 12, av: "50%", card: true, grad: "radial-gradient(circle at 18% 18%,#fb923c,transparent 55%),radial-gradient(circle at 82% 75%,#a855f7,#3a1f26)", note: "Rounded, cards" },
    { id: "grape", label: "Grape", bg: "#1c1428", hi: "#2a1e3a", ac: "#c084fc", font: GROTESK, r: 8, av: "50%", grad: "radial-gradient(circle at 20% 20%,#c084fc,transparent 55%),radial-gradient(circle at 85% 72%,#ec4899,#291b3a)", note: "Grotesk, glass" },
  ];

  // Animated packs: a live backdrop moves behind translucent surfaces. The mini
  // preview runs the SAME motion the real backdrop does (see .pk.live styles) —
  // `motion` picks which one, so the card sells the effect instead of describing
  // it. Keep in sync with ANIMATED_PACKS in state.svelte.js.
  const LIVE_PACKS = [
    { id: "aurora", label: "Aurora", bg: "#061b21", hi: "#112e34", ac: "#39d9b0", font: GROTESK, r: 10, av: "40%", motion: "sweep", grad: "linear-gradient(103deg,transparent 34%,#29d69e 42%,#7af6d6 48%,#46e2ff 54%,transparent 64%)", base: "linear-gradient(180deg,#16505a,#04161d)", note: "Sweeping curtain" },
    { id: "synthwave", label: "Synthwave", bg: "#16072a", hi: "#260f3e", ac: "#ff4fd8", font: GROTESK, r: 0, av: "2px", motion: "sun", grad: "linear-gradient(180deg,#ffd873,#ff7ec9 55%,#b3379b)", base: "linear-gradient(180deg,#300a50 0%,#4a1069 54%,#12021f 58%)", note: "Sun + rushing grid" },
    { id: "cosmos", label: "Cosmos", bg: "#0a0c1b", hi: "#151930", ac: "#7c8bff", font: INTER, r: 13, av: "50%", motion: "star", grad: "linear-gradient(120deg,transparent 44%,#d6ecff 49.4%,transparent 55%)", base: "radial-gradient(120% 110% at 50% 20%,#1b2059,#04040c)", note: "Shooting stars" },
    { id: "molten", label: "Molten", bg: "#1c0e08", hi: "#2e180e", ac: "#ff7a2f", font: SERIF, r: 4, av: "8px", motion: "heat", grad: "linear-gradient(0deg,transparent 30%,#ff7a2f 42%,#ffc46e 50%,transparent 66%)", base: "radial-gradient(120% 100% at 50% 110%,#7a2708,#0e0704)", note: "Rising heat, serif" },
    { id: "prism", label: "Prism", bg: "#12141a", hi: "#20232c", ac: "#8de0ff", font: GROTESK, r: 14, av: "50%", motion: "sweep", grad: "linear-gradient(100deg,transparent 36%,#ff5c9e 41%,#ffc454 45%,#60f0aa 49%,#60c8ff 53%,#9682ff 57%,transparent 63%)", base: "linear-gradient(180deg,#23262f,#0a0b0e)", note: "Iridescent sweep" },
    { id: "monsoon", label: "Monsoon", bg: "#0b1722", hi: "#162838", ac: "#56b7e8", font: INTER, r: 5, av: "8px", motion: "rain", grad: "repeating-linear-gradient(14deg,rgba(186,226,255,0.55) 0 1.5px,rgba(186,226,255,0.14) 1.5px 3px,transparent 3px 13px)", base: "linear-gradient(178deg,#17384f,#06141f)", note: "Falling rain" },
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

  <!-- One card shape for all three galleries. The mini window is drawn from the
       pack's OWN tokens — its face in the "Ag", its radius on the panel and its
       avatar shape on the chip — so what you see in the card is what the app
       turns into. `still` freezes the motion for the textured row. -->
  {#snippet packCard(p, still = false)}
    <button
      class="pack-card"
      class:sel={themePack === p.id}
      role="radio"
      aria-checked={themePack === p.id}
      onclick={() => setAppearance("themePack", p.id)}
    >
      <span
        class="pk"
        class:still
        class:day={p.day}
        data-motion={p.motion || null}
        style="--pk-bg:{p.bg};--pk-hi:{p.hi};--pk-ac:{p.ac};--pk-r:{p.r}px;--pk-av:{p.av};--pk-font:{p.font};{p.grad
          ? `--pk-grad:${p.grad};`
          : ''}{p.base ? `--pk-base:${p.base};` : ''}"
        aria-hidden="true"
      >
        {#if p.grad}<span class="pk-glow"></span>{/if}
        <span class="pk-rail"><i></i><i></i></span>
        <span class="pk-body" class:card={p.card} class:rule={p.rule}>
          <span class="pk-msg"><span class="pk-av"></span><span class="pk-ag">Ag</span></span>
          <span class="pk-msg alt"><span class="pk-av"></span><span class="pk-line"></span></span>
        </span>
      </span>
      <span class="pk-name">{p.label}</span>
      <span class="pk-note">{p.note}</span>
    </button>
  {/snippet}

  <hr />
  <section>
    <strong class="label">Theme pack</strong>
    <div class="pack-row" role="radiogroup" aria-label="Theme pack">
      {#each PACKS as p (p.id)}{@render packCard(p)}{/each}
    </div>
    <p class="muted tiny">
      A pack is a whole look, not a hue: its own typeface, corner radius, avatar
      shape, shadow depth and feed rhythm come with it. (An accent preset or a
      Corners/Typeface choice under Customize still overrides the pack.)
    </p>

    <div class="live-head">
      <span class="live-tag day-tag">☀ Daylight</span>
      <span class="muted tiny">Bright grounds, dark ink — for light-mode eyes.</span>
    </div>
    <div class="pack-row" role="radiogroup" aria-label="Daylight theme pack">
      {#each DAY_PACKS as p (p.id)}{@render packCard(p, p.still)}{/each}
    </div>

    <div class="live-head">
      <span class="live-tag">✨ Animated</span>
      <span class="muted tiny">A living backdrop moves behind the app.</span>
    </div>
    <div class="pack-row" role="radiogroup" aria-label="Animated theme pack">
      {#each LIVE_PACKS as p (p.id)}{@render packCard(p)}{/each}
    </div>

    <div class="live-head">
      <span class="live-tag texture-tag">▦ Textured</span>
      <span class="muted tiny">A soft colour mesh, no animation.</span>
    </div>
    <div class="pack-row" role="radiogroup" aria-label="Textured theme pack">
      {#each TEXTURE_PACKS as p (p.id)}{@render packCard(p, true)}{/each}
    </div>
  </section>

  <hr />
  <button class="disclose" onclick={() => (custom = !custom)} aria-expanded={custom}>
    <span class="disclose-chev" class:open={custom}>›</span>
    Customize
    <span class="disclose-sub">Accent, corners, typeface, density &amp; clock</span>
  </button>

  {#if custom}
    <div class="custom" transition:slide={{ duration: 240 }}>

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
      <div class="seg faces" role="radiogroup" aria-label="Typeface">
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
        <em>Theme</em> follows the pack you picked above. The rest ship inside the
        app — Concord never downloads a font at runtime, which would tell a font
        host every time you open it.
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
      <strong class="label">UI scale</strong>
      <div class="scale-row">
        <input
          type="range"
          min="0.8"
          max="1.5"
          step="0.05"
          value={uiScale}
          aria-label="UI scale"
          oninput={(e) => setAppearance("uiScale", Number(e.currentTarget.value))}
        />
        <span class="scale-val">{Math.round(uiScale * 100)}%</span>
        <button class="scale-reset" disabled={uiScale === 1} onclick={() => setAppearance("uiScale", 1)}>
          Reset
        </button>
      </div>
      <p class="muted tiny">
        Makes everything bigger or smaller. Ctrl+= and Ctrl+− work anywhere; Ctrl+0 resets.
      </p>
    </section>

    <section>
      <strong class="label">Flair</strong>
      <SettingRow
        icon="diamond"
        title="Use guild colors"
        sub="Each guild tints the app with its banner's hue"
        info="Derived from the banner a guild already chose, so every guild has a color identity for free. Your own accent preset above always wins when set."
        checked={S.prefs.guildAccents !== false}
        onclick={() => setAppearance("guildAccents", S.prefs.guildAccents === false)}
      />
      <SettingRow
        icon="spark"
        title="Seasonal touches"
        sub="Snow in December, petals in spring, leaves in autumn"
        info="A sparse drift over the server rail, driven by this device's clock only — nothing is fetched. Quiet months show nothing."
        checked={S.prefs.seasonal !== false}
        onclick={() => setAppearance("seasonal", S.prefs.seasonal === false)}
      />
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

    </div>
  {/if}

  <div class="actions">
    <button onclick={onClose}>Done</button>
  </div>
</Modal>

<style>
  .scale-row {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .scale-row input[type="range"] {
    flex: 1;
    accent-color: var(--accent);
  }
  .scale-val {
    font-variant-numeric: tabular-nums;
    min-width: 4ch;
    text-align: right;
    font-size: var(--fs-ui);
    color: var(--text-muted);
  }
  .scale-reset {
    font-size: var(--fs-tiny);
    padding: 4px 10px;
  }
  .scale-reset:disabled {
    opacity: 0.45;
  }

  /* Disclosure: the second layer of this dialog. Quiet at rest so the theme
     gallery above it stays the thing you look at. */
  .disclose {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 10px 12px;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--text);
    font-size: var(--fs-ui);
    font-weight: 600;
    text-align: left;
    transition:
      border-color 0.14s ease,
      background 0.14s ease;
  }
  .disclose:hover {
    background: var(--bg-1);
    border-color: var(--accent);
  }
  .disclose-chev {
    color: var(--text-faint);
    font-size: 17px;
    line-height: 1;
    transition: transform 0.22s cubic-bezier(0.34, 1.4, 0.64, 1);
  }
  .disclose-chev.open {
    transform: rotate(90deg);
  }
  .disclose-sub {
    margin-left: auto;
    font-size: var(--fs-compact);
    font-weight: 400;
    color: var(--text-muted);
  }
  .custom {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  @media (prefers-reduced-motion: reduce) {
    .disclose-chev {
      transition: none;
    }
  }
  section {
    display: flex;
    flex-direction: column;
    gap: 8px;
    text-align: left;
  }
  /* Match the sectioned settings look: small uppercase group labels. */
  .label {
    font-size: var(--fs-small);
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  p {
    margin: 0;
    font-size: var(--fs-ui);
    line-height: 1.5;
  }
  .tiny {
    font-size: var(--fs-small);
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
    font-size: var(--fs-compact);
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

  /* Theme-pack cards: a mini app-window built out of the pack's own tokens.
     Three colour swatches were the reason the gallery undersold itself — you
     could not tell a monospaced, square, packed-tight pack from a rounded,
     serif, airy one. Now the card shows the face, the radius, the avatar
     shape, the row treatment and the motion. */
  .pack-row {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 8px;
  }
  .pack-card {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 6px;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--text-muted);
    font-size: var(--fs-compact);
    align-items: stretch;
    text-align: center;
    /* Every live pack card runs an infinite CSS animation (one of them repeats
       at 0.7s) and nine of them exist. Without this they all keep compositing
       while scrolled far off-screen, for the whole time this panel is open —
       a measurable battery cost on a phone. Off-screen cards now render, and
       therefore animate, not at all. */
    content-visibility: auto;
    contain-intrinsic-size: 112px 92px;
  }
  .pack-card:hover,
  .pack-card:active {
    background: var(--bg-3);
    color: var(--text);
  }
  .pack-card.sel {
    border-color: var(--accent);
    color: var(--text);
    background: var(--accent-soft);
  }
  .pk-name {
    font-weight: 600;
  }
  .pk-note {
    font-size: var(--fs-tiny);
    line-height: 1.25;
    color: var(--text-faint);
    /* Two lines max; the note is a hint, not a paragraph. */
    min-height: 12px;
  }
  .pack-card.sel .pk-note {
    color: var(--text-muted);
  }
  .pk {
    position: relative;
    width: 100%;
    height: 58px;
    border-radius: 7px;
    border: 1px solid rgba(255, 255, 255, 0.07);
    background: var(--pk-base, var(--pk-bg));
    display: flex;
    overflow: hidden;
    /* Keep the mini window's internal layering to itself. Without this, the
       z-index below is measured against the whole dialog and the preview cards
       paint OVER the sticky "Appearance" header as you scroll. */
    isolation: isolate;
  }
  .pk-rail {
    position: relative;
    z-index: 1;
    width: 13px;
    flex: none;
    padding: 4px 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 3px;
    background: color-mix(in srgb, var(--pk-bg) 62%, black);
  }
  /* The guild dots pick up the pack's avatar shape — a square-avatar pack is
     recognisable from the rail alone. */
  .pk-rail i {
    width: 7px;
    height: 7px;
    flex: none;
    border-radius: var(--pk-av);
    background: color-mix(in srgb, var(--pk-hi) 85%, white 8%);
  }
  .pk-rail i:first-child {
    background: var(--pk-ac);
  }
  .pk-body {
    position: relative;
    z-index: 1;
    flex: 1;
    min-width: 0;
    margin: 4px 4px 4px 3px;
    padding: 3px 4px;
    border-radius: min(var(--pk-r), 12px);
    background: color-mix(in srgb, var(--pk-bg) 88%, white 4%);
    display: flex;
    flex-direction: column;
    gap: 3px;
    overflow: hidden;
  }
  .pk-msg {
    display: flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
    padding: 1px 2px;
    border-radius: min(var(--pk-r), 9px);
  }
  /* Card packs paint every message row; ruled packs draw a hairline instead. */
  .pk-body.card .pk-msg {
    background: color-mix(in srgb, var(--pk-hi) 55%, transparent);
  }
  .pk-body.rule .pk-msg + .pk-msg {
    border-top: 1px solid color-mix(in srgb, var(--pk-hi) 90%, white 25%);
    padding-top: 3px;
  }
  .pk-av {
    width: 13px;
    height: 13px;
    flex: none;
    border-radius: var(--pk-av);
    background: var(--pk-ac);
  }
  /* The face, actually set in the face. */
  .pk-ag {
    font-family: var(--pk-font);
    font-size: 15px;
    line-height: 1;
    letter-spacing: 0;
    color: color-mix(in srgb, var(--pk-ac) 45%, white);
  }
  .pk-line {
    height: 4px;
    flex: 1;
    border-radius: 2px;
    background: color-mix(in srgb, var(--pk-hi) 90%, white 12%);
  }
  .pk-msg.alt .pk-line {
    max-width: 60%;
  }

  /* Animated-pack subsection heading. */
  .live-head {
    display: flex;
    align-items: baseline;
    gap: 8px;
    margin-top: 12px;
    flex-wrap: wrap;
  }
  .live-tag {
    font-size: var(--fs-small);
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--accent-hover);
  }

  /* The live backdrop, in miniature. Each [data-motion] runs the same kind of
     motion the real pack runs (app.css), so the card is a sample rather than a
     description — and, like the real thing, it is transform/opacity only. */
  .pk-glow {
    position: absolute;
    inset: -40%;
    background: var(--pk-grad);
    opacity: 0.85;
  }
  .pk[data-motion="sweep"] .pk-glow {
    animation: pk-sweep 3.6s linear infinite;
  }
  .pk[data-motion="star"] .pk-glow {
    animation: pk-star 3.4s linear infinite;
  }
  .pk[data-motion="heat"] .pk-glow {
    animation: pk-heat 2.6s linear infinite;
  }
  .pk[data-motion="rain"] .pk-glow {
    inset: -60%;
    opacity: 0.9;
    animation: pk-rain 0.7s linear infinite;
  }
  /* Synthwave's is a sun, not a band: a disc low on the left, breathing. */
  .pk[data-motion="sun"] .pk-glow {
    inset: auto;
    left: -8%;
    top: 26%;
    width: 30px;
    height: 30px;
    border-radius: 50%;
    animation: pk-pulse 2.4s ease-in-out infinite alternate;
  }
  @keyframes pk-sweep {
    0% {
      transform: translate3d(-58%, -20%, 0);
    }
    100% {
      transform: translate3d(58%, 20%, 0);
    }
  }
  @keyframes pk-star {
    0% {
      transform: translate3d(-50%, -34%, 0);
      opacity: 0;
    }
    10% {
      opacity: 0.95;
    }
    36% {
      opacity: 0.95;
    }
    46% {
      transform: translate3d(50%, 34%, 0);
      opacity: 0;
    }
    100% {
      transform: translate3d(50%, 34%, 0);
      opacity: 0;
    }
  }
  @keyframes pk-heat {
    0% {
      transform: translate3d(0, 52%, 0);
    }
    100% {
      transform: translate3d(0, -52%, 0);
    }
  }
  @keyframes pk-rain {
    to {
      transform: translate3d(-3.2px, 13px, 0);
    }
  }
  @keyframes pk-pulse {
    0% {
      transform: scale(1);
      opacity: 0.8;
    }
    100% {
      transform: scale(1.12);
      opacity: 1;
    }
  }
  /* Textured previews: the same mesh, held still — which is what the real
     textured packs do. */
  .pk.still .pk-glow {
    inset: 0;
    opacity: 0.6;
    animation: none;
  }
  /* Translucent mini-surfaces, so the backdrop reads through them exactly the
     way it does in the app. */
  .pk-glow ~ .pk-rail {
    background: color-mix(in srgb, var(--pk-bg) 70%, transparent);
  }
  .pk-glow ~ .pk-body {
    background: color-mix(in srgb, var(--pk-bg) 60%, transparent);
  }
  @media (prefers-reduced-motion: reduce) {
    .pk-glow {
      animation: none !important;
    }
  }
  /* Daylight cards: the base card mixes its inks toward WHITE (fine on every
     dark pack, invisible on a light one), so these flip each mix toward black.
     The rail keeps the app's light-mode convention — chrome darker than page. */
  .pk.day {
    border-color: rgba(0, 0, 0, 0.12);
  }
  .pk.day .pk-rail {
    background: color-mix(in srgb, var(--pk-bg) 82%, black);
  }
  .pk.day .pk-rail i {
    background: color-mix(in srgb, var(--pk-bg) 55%, black);
  }
  .pk.day .pk-rail i:first-child {
    background: var(--pk-ac);
  }
  .pk.day .pk-ag {
    color: color-mix(in srgb, var(--pk-ac) 60%, black);
  }
  .pk.day .pk-line {
    background: color-mix(in srgb, var(--pk-hi) 72%, black);
  }
  .pk.day .pk-body.rule .pk-msg + .pk-msg {
    border-top-color: color-mix(in srgb, var(--pk-hi) 60%, black);
  }
  /* Porcelain: restate the glass translucency — the darkened day rail above
     out-specifies the `.pk-glow ~ .pk-rail` rule and would paint over the mesh. */
  .pk.day .pk-glow ~ .pk-rail {
    background: color-mix(in srgb, var(--pk-bg) 70%, transparent);
  }
  .pk.day .pk-glow ~ .pk-body {
    background: color-mix(in srgb, var(--pk-bg) 60%, transparent);
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
    font-size: var(--fs-ui);
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
  /* Seven options divide into no tidy grid, so "Theme" — the default, and the
     only one that isn't a named face — takes a full-width row of its own and
     the six real faces fall into 3x2 beneath it. */
  .seg.faces {
    grid-template-columns: repeat(3, 1fr);
  }
  .seg.faces > button:first-child {
    grid-column: 1 / -1;
  }
  /* The option rows are tight at three or four across; center them and let the
     label shrink before the preview does. */
  .seg.four > button,
  .seg.faces > button {
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
  @media (pointer: coarse), (max-width: 768px) {
    /* A colour swatch has neighbours on both axes, so 36px left their centres
       under the fingertip minimum in a row you're expected to scan and tap
       precisely. */
    .swatch {
      width: var(--tap-min);
      height: var(--tap-min);
    }
    .theme-card {
      padding: 10px 8px;
      font-size: var(--fs-ui);
    }
    .seg > button {
      min-height: 48px;
    }
    /* The note is what says what a pack actually IS; on a phone the type scale
       already grows it, so all it needs is room to be two lines. */
    .pk-note {
      min-height: 0;
    }
  }
  /* The narrow floor. Four cells across 320px of content box leaves ~33px for
     "Round" beside a fixed 22px preview, so the labels wrapped mid-word; three
     packs across the same width does the same to their names. */
  @media (max-width: 400px) {
    .seg.four,
    .seg.faces,
    .pack-row {
      grid-template-columns: 1fr 1fr;
    }
    .seg.faces > button:first-child {
      grid-column: 1 / -1;
    }
  }
</style>
