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
  const profileColor = $derived(S.identity.color || "#14a394");
</script>

<Modal title="Appearance" {onClose}>
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
