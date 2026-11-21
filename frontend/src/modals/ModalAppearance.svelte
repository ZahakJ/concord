<script>
  // Appearance: everything about how the app looks on THIS device, in one
  // place and in five blocks — the theme, colour and shape, reading, the
  // window's arrangement, motion and effects. Everything is device-local
  // (S.prefs) and applies live — no save step. setAppearance persists the pref
  // and stamps <html> with a short cross-fade; see state.svelte.js.
  //
  // What other people see of you (picture, banner, profile colours) is
  // Your profile, linked from the foot of this page.
  import { motionInView } from "../lib/inview.js";
  import RailShell from "./RailShell.svelte";
  import SettingRow from "./SettingRow.svelte";
  import Segmented from "../Segmented.svelte";
  import Select from "../Select.svelte";
  import SettingGroup from "./SettingGroup.svelte";
  import FxOverlay from "../FxOverlay.svelte";
  import { THEME_FX, seasonFx } from "../lib/themefx.js";
  import { PACKS, packById, packIdOrDefault } from "../lib/packs.js";
  import { S, setAppearance } from "../lib/state.svelte.js";
  import { LAYOUTS, currentLayout, setLayout } from "../lib/layout.js";
  import { ACCENT_PROFILE, profileAccent } from "../lib/guildaccent.js";
  import { rangefill } from "../lib/rangefill.js";

  let { onClose } = $props();

  const THEMES = [
    { id: "light", label: "Light" },
    { id: "dark", label: "Dark" },
    { id: "system", label: "Match system" },
  ];
  // Which ground is actually on screen: System resolves to one of the two.
  const shownLight = $derived(
    (S.prefs.theme || "dark") === "light" ||
      ((S.prefs.theme || "dark") === "system" &&
        typeof window !== "undefined" &&
        !window.matchMedia?.("(prefers-color-scheme: dark)")?.matches),
  );

  // Teal (accent pref "" — the default, and the theme's own teal, so it stays
  // legible in light), five presets, and the opt-in "match my profile colour"
  // ("profile"; red–orange hues are toned there, lib/guildaccent.js).
  const ACCENTS = [
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
  const profileColor = $derived(profileAccent(S.identity.color) || "#14a394");
  // A saved pack this build does not know is drawn, and shown, as none.
  const themePack = $derived(packIdOrDefault(S.prefs.themePack));
  const currentPack = $derived(packById(themePack));
  // The first swatch is "no accent of my own": the brand teal, or under a pack
  // the pack's own accent — which is what is on screen, so that is what the
  // selected swatch shows.
  const baseAccent = $derived(
    currentPack ? { name: `${currentPack.label}'s own colour`, color: currentPack.ac } : { name: "Concord teal", color: "#14a394" },
  );
  // Picking a theme card is asking for that ground on Concord's own palette,
  // which a pack overrides; so it hands the ground back from the pack.
  function pickTheme(id) {
    if (themePack) setAppearance("themePack", "");
    setAppearance("theme", id);
  }
  const themeFx = $derived(S.prefs.themeFx || "");
  // The specular pass on a live backdrop. Off unless it is literally on.
  const shine = $derived(S.prefs.themeShine === true);
  const shape = $derived(S.prefs.shape || "");
  const font = $derived(S.prefs.font || "");

  // Three densities. Compact used to be a Layout as well (the sidebar plus
  // compact density plus the side panel closed); it is only this now.
  const DENSITIES = [
    { id: "compact", label: "Compact", note: "Tight rows, smaller reading size — more of the conversation per screen." },
    { id: "cozy", label: "Cosy", note: "The default: room between messages, comfortable reading size." },
    { id: "comfortable", label: "Comfortable", note: "Wide rows and larger type — easiest to read across the room." },
  ];

  // Shape + typeface are theme axes of their own; "" defers to the pack, which
  // carries its own corner radius and UI face (see themepacks.css).
  const SHAPES = [
    { id: "", label: "Theme" },
    { id: "sharp", label: "Sharp" },
    { id: "soft", label: "Soft" },
    { id: "round", label: "Round" },
  ];
  // Every face is bundled with the app (public/fonts, scripts/prep-fonts.mjs),
  // so picking one costs no network request.
  const FONTS = [
    { id: "", label: "Theme" },
    { id: "system", label: "System" },
    { id: "inter", label: "Inter" },
    { id: "grotesk", label: "Grotesk" },
    { id: "serif", label: "Serif" },
    { id: "rounded", label: "Rounded" },
    { id: "mono", label: "Mono" },
    { id: "hyper", label: "Legible" },
    { id: "cyber", label: "Cyber" },
    { id: "comic", label: "Comic" },
  ];

  // Device-local display prefs read by the feed and the shell. Their defaults
  // are off / "sidebar", so a missing key reads as the default.
  const roleNames = $derived(S.prefs.roleNameColors === true);
  const underline = $derived(S.prefs.underlineLinks === true);
  const osReduced = typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;
  // What is in force, not what the pref says: when the OS asks for less
  // motion it wins, and the control shows that rather than a "Full" that is
  // not happening.
  const motion = $derived(osReduced || S.prefs.motion === "reduced" ? "reduced" : "full");
  const layout = $derived(currentLayout(S.prefs));
  // Seasonal's card shows this month's season; in a quiet month it says so.
  const season = seasonFx();
</script>

<RailShell title="Appearance" here="appearance" {onClose}>
  <!-- A pack card is a mini window drawn from the pack's OWN tokens — its face
       in the "Ag", its radius on the panel and its avatar shape on the chip —
       so what you see in the card is what the app turns into. -->
  {#snippet packCard(p)}
    <button
      class="pack-card"
      class:sel={themePack === p.id}
      role="radio"
      aria-checked={themePack === p.id}
      onclick={() => setAppearance("themePack", p.id)}
      use:motionInView
    >
      <span
        class="pk"
        class:day={p.day}
        class:dimshine={p.shine && !shine}
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

  <SettingGroup label="Theme">
    <div class="cards">
      <div class="theme-row" role="radiogroup" aria-label="Theme">
        {#each THEMES as t (t.id)}
          <button
            class="theme-card"
            class:sel={!themePack && theme === t.id}
            role="radio"
            aria-checked={!themePack && theme === t.id}
            onclick={() => pickTheme(t.id)}
          >
            <!-- A miniature of each ground, painted from the two themes' own
                 surface values (the .pv palette below), so the Light card shows
                 paper even while the app is graphite. -->
            <span class="pv {t.id}" aria-hidden="true">
              <span class="pv-side"><i></i><i></i></span>
              <span class="pv-main"><i class="l1"></i><i class="l2"></i></span>
              {#if t.id === "system"}
                <span class="pv-half"><span class="pv-side"><i></i><i></i></span><span class="pv-main"><i class="l1"></i><i class="l2"></i></span></span>
              {/if}
            </span>
            <span class="cap">{t.label}</span>
          </button>
        {/each}
      </div>
      <div class="pack-row" role="radiogroup" aria-label="Theme pack">
        {#each PACKS as p (p.id)}{@render packCard(p)}{/each}
      </div>
      <p class="note">
        {#if currentPack}
          {currentPack.label} brings its own colours, typeface and corners. Light, Dark and Match system go back to
          Concord's own palette.
        {:else}
          A pack brings its own colours, typeface and corners{shownLight ? ", and the dark ones bring a dark ground" : ""}.
          Accent, corners and typeface below still override it.
        {/if}
      </p>
    </div>
  </SettingGroup>

  <SettingGroup label="Colour and shape">
    <SettingRow title="Accent" sub="Your colour for focus, selection, mentions of you and your reactions. The ring follows your profile colour.">
      <div class="swatches" role="radiogroup" aria-label="Accent colour">
        <button
          class="swatch"
          class:sel={accent === ""}
          role="radio"
          aria-checked={accent === ""}
          title={baseAccent.name}
          aria-label={baseAccent.name}
          style="--sw:{baseAccent.color}"
          onclick={() => setAppearance("accent", "")}
        ></button>
        {#each ACCENTS as a (a.name)}
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
          class:sel={accent === ACCENT_PROFILE}
          role="radio"
          aria-checked={accent === ACCENT_PROFILE}
          title="Match my profile colour"
          aria-label="Match my profile colour"
          style="--sw:{profileColor}"
          onclick={() => setAppearance("accent", ACCENT_PROFILE)}
        ></button>
      </div>
    </SettingRow>
    <SettingRow title="Corners" sub="How rounded panels, buttons and fields are. Theme follows the pack.">
      <Segmented
        label="Corner style"
        value={shape}
        onChange={(v) => setAppearance("shape", v)}
        options={SHAPES.map((x) => ({ value: x.id, label: x.label }))}
      />
    </SettingRow>
    <SettingRow title="Typeface" sub="Theme follows the pack. Every face ships inside the app — none is downloaded.">
      <Select
        label="Typeface"
        value={font}
        onPick={(v) => setAppearance("font", v)}
        options={FONTS.map((f) => ({ value: f.id, label: f.label }))}
      />
    </SettingRow>
  </SettingGroup>

  <SettingGroup label="Reading">
    <SettingRow title="Density" sub={DENSITIES.find((d) => d.id === density)?.note}>
      <Segmented
        label="Message density"
        value={density}
        options={DENSITIES.map((d) => ({ value: d.id, label: d.label }))}
        onChange={(v) => setAppearance("density", v)}
      />
    </SettingRow>
    <SettingRow title="Interface scale" sub="Everything in the window. Ctrl+= and Ctrl+− work anywhere; Ctrl+0 resets.">
      <div class="scale-row">
        <input
          type="range"
          min="0.8"
          max="1.5"
          step="0.05"
          value={uiScale}
          aria-label="Interface scale"
          oninput={(e) => setAppearance("uiScale", Number(e.currentTarget.value))}
          use:rangefill={uiScale}
        />
        <span class="scale-val">{Math.round(uiScale * 100)}%</span>
        <button class="ghost sm" disabled={uiScale === 1} onclick={() => setAppearance("uiScale", 1)}>Reset</button>
      </div>
    </SettingRow>
    <SettingRow
      title="Underline links"
      sub="Always underline links in messages."
      checked={underline}
      onclick={() => setAppearance("underlineLinks", !underline)}
    />
    <SettingRow
      title="Colour names by role"
      sub="Tint names with their top role's colour, kept readable."
      checked={roleNames}
      onclick={() => setAppearance("roleNameColors", !roleNames)}
    />
    <SettingRow title="Clock" sub="How message timestamps show the time of day.">
      <Segmented
        label="Timestamp format"
        value={clock}
        onChange={(v) => setAppearance("clock", v)}
        options={[
          { value: "system", label: "Automatic" },
          { value: "12", label: "12-hour" },
          { value: "24", label: "24-hour" },
        ]}
      />
    </SettingRow>
  </SettingGroup>

  <!-- How the window is arranged. Each card draws the arrangement in the app's
       own surfaces. Not on a phone: the phone has one arrangement. -->
  {#if !S.isMobile}
    <SettingGroup label="Layout">
      <div class="cards">
        <div class="layout-row" role="radiogroup" aria-label="Layout">
          {#each LAYOUTS as l (l.id)}
            <button
              class="theme-card"
              class:sel={layout === l.id}
              role="radio"
              aria-checked={layout === l.id}
              onclick={() => setLayout(l.id)}
            >
              <span class="lv {l.id}" aria-hidden="true">
                {#if l.id === "rail"}
                  <span class="lv-rail"><b></b><b></b><b></b></span>
                {/if}
                <span class="lv-side">
                  {#if l.id !== "rail"}<span class="lv-strip"><b></b><b></b><b></b></span>{/if}
                  <i></i><i></i><i></i>
                </span>
                <span class="lv-main"><i></i><i class="s"></i><i></i></span>
                <span class="lv-insp"><i></i><i></i></span>
              </span>
              <span class="cap">{l.label}</span>
            </button>
          {/each}
        </div>
        <p class="note">{LAYOUTS.find((l) => l.id === layout)?.note}</p>
      </div>
    </SettingGroup>
  {/if}

  <SettingGroup label="Motion and effects">
    <!-- One switch for movement. The OS setting turns it down too, and wins:
         asking for less motion anywhere is asking for less motion here. -->
    <SettingRow
      title="Motion"
      sub={osReduced
        ? "Your system asks for reduced motion, so the app keeps still."
        : motion === "reduced"
          ? "Things appear and change in place: no lifts, slides or settles."
          : "Tiles lift, dialogs settle, the guild strip glides. Small and quick."}
    >
      <Segmented
        label="Motion"
        value={motion}
        disabled={osReduced}
        onChange={(v) => setAppearance("motion", v)}
        options={[
          { value: "full", label: "Full" },
          { value: "reduced", label: "Reduced" },
        ]}
      />
    </SettingRow>
    <SettingRow
      title="Sweeping highlights"
      sub="The band of light that crosses the window on Aurora."
      info="Aurora's curtain carries one bright layer that travels over the whole window, over what you are reading. Off, the pack keeps its colour, its scenery and the rest of its motion, and loses only that band. Other packs have none."
      checked={shine}
      onclick={() => setAppearance("themeShine", !shine)}
    />
    <div class="cards fx-block">
      <span class="sub-label">Effect</span>
      <div class="fx-row" role="radiogroup" aria-label="Visual effect">
        {#each THEME_FX as f (f.id)}
          <button
            class="pack-card"
            class:sel={themeFx === f.id}
            role="radio"
            aria-checked={themeFx === f.id}
            onclick={() => setAppearance("themeFx", f.id)}
            use:motionInView
          >
            <!-- The card runs the real effect, scaled down — same component and
                 same CSS the app mounts, so nothing here can drift out of sync
                 with what picking it actually does. -->
            <span class="fxpv" aria-hidden="true">
              <FxOverlay fx={f.id === "seasonal" ? season : f.id} mini s={0.18} scale={0.3} />
              <span class="fxpv-ink"></span>
              <span class="fxpv-ink short"></span>
            </span>
            <span class="pk-name">{f.label}</span>
            <span class="pk-note">{f.id === "seasonal" && !season ? "Nothing this month" : f.note}</span>
          </button>
        {/each}
      </div>
      <p class="note">
        An effect layers over whatever theme you picked. Drawn on this device, never downloaded, and off entirely
        when Motion is Reduced. It starts off on each device.
      </p>
    </div>
  </SettingGroup>

  <SettingGroup label="What others see">
    <SettingRow
      title="Your profile's look"
      sub="Picture, banner and profile colours live with your profile."
      to="profile"
      from="appearance"
    />
  </SettingGroup>
</RailShell>

<style>
  /* Appearance: five groups. Card grids sit inside a group with the same
     air as rows (--sp-3 above and below), and a note under a grid is set
     apart from it by --sp-2. */
  .cards {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    padding: var(--sp-3) 0;
  }
  .cards + :global(.row) {
    border-top: 1px solid var(--hairline);
  }
  :global(.row) + .cards {
    border-top: 1px solid var(--hairline);
  }
  .sub-label {
    font-size: var(--fs-ui);
    font-weight: 500;
  }
  .note {
    margin: 0;
    font-size: var(--fs-compact);
    line-height: 1.5;
    color: var(--text-muted);
  }
  .cap {
    font-size: var(--fs-compact);
  }

  /* Previews hold still until you point at one. A dozen little windows all
     moving at once is the busiest thing in Settings, and nobody is reading
     more than one of them. Hover or keyboard focus wakes the card under you; the
     reduced-motion rules and lib/inview.js (off-screen) still win. */
  .pack-card:not(:hover):not(:focus-visible) :global(*),
  .pack-card:not(:hover):not(:focus-visible) :global(*::before),
  .pack-card:not(:hover):not(:focus-visible) :global(*::after) {
    /* !important: an `animation` shorthand on a preview part resets the
       play state, and would otherwise win on source order. */
    animation-play-state: paused !important;
  }

  /* The pack and effect grids: three across for the nine packs, four for the
     eight effects (two on a phone). Chosen = inset ring. */
  .pack-row {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: var(--sp-3);
  }
  .fx-row {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: var(--sp-3);
  }
  .pack-card {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 0;
    background: transparent;
    color: var(--text);
    font-size: var(--fs-compact);
    align-items: stretch;
    text-align: start;
    content-visibility: auto;
    contain-intrinsic-size: 112px 92px;
  }
  /* The layout cards: the same card as a theme's, drawn in this theme's own
     surfaces — the arrangement is the point, not the colours. */
  .lv {
    display: flex;
    gap: 2px;
    height: 76px;
    padding: 0;
    border-radius: var(--radius-md);
    border: 1px solid var(--border);
    overflow: hidden;
    background: var(--bg-2);
    position: relative;
  }
  /* The selected ring is drawn OVER the drawing, on the card's own edge: as an
     inset shadow on .lv it painted under the sidebar and inspector panes, so
     only the middle pane's top and bottom showed it. */
  .lv::after {
    content: "";
    position: absolute;
    inset: 0;
    border-radius: inherit;
    pointer-events: none;
  }
  .theme-card.sel .lv::after {
    box-shadow: inset 0 0 0 2px var(--accent-hover);
  }
  .theme-card:focus-visible .lv {
    outline: var(--focus-ring);
    outline-offset: 2px;
  }
  .lv-rail,
  .lv-side,
  .lv-insp {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    padding: 6px 5px;
    background: var(--bg-1);
  }
  .lv-rail {
    width: 14px;
    align-items: center;
    padding: 6px 2px;
  }
  .lv-side {
    width: 30%;
  }
  .lv.rail .lv-side {
    width: 24%;
  }
  .lv-insp {
    width: 18%;
  }
  .lv-main {
    flex: 1;
    display: flex;
    flex-direction: column;
    justify-content: flex-end;
    gap: 5px;
    padding: 8px 6px;
  }
  .lv-strip {
    display: flex;
    gap: 2px;
    margin-bottom: 2px;
  }
  .lv b {
    display: block;
    width: 7px;
    height: 7px;
    border-radius: 2px;
    background: var(--text-faint);
  }
  .lv i {
    display: block;
    height: 4px;
    border-radius: 2px;
    background: var(--bg-3);
  }
  .lv-main i:not(.s) {
    width: 80%;
  }
  .lv-main i.s {
    width: 45%;
    background: var(--accent);
  }
  /* Three equal theme cards: a miniature of each ground, one indicator — an
     inset ring in the accent ink — and the label under it. */
  .theme-row,
  .layout-row {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: var(--sp-3);
  }
  .layout-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .theme-card {
    display: flex;
    flex-direction: column;
    /* Said outright: the Android WebView (124) gives a button
       align-items: flex-start in its own stylesheet, so the previews, which
       have no width of their own, came out as 30px slivers on the phone. */
    align-items: stretch;
    gap: var(--sp-2);
    padding: 0;
    background: transparent;
    color: var(--text-muted);
    font-size: var(--fs-compact);
    font-weight: 500;
    text-align: start;
  }
  .theme-card.sel .cap {
    color: var(--text);
    font-weight: 600;
  }
  .theme-card:focus-visible {
    outline: none;
  }
  .theme-card:focus-visible .pv {
    outline: var(--focus-ring);
    outline-offset: 2px;
  }
  /* The two grounds, as the theme blocks in app.css define them. These are the
     one place the previews spell colours out: a preview of the light theme has
     to show paper while the dark theme is the one on screen, and no token can
     answer "what is --bg-1 in the OTHER theme". */
  .pv {
    --pv-dark-1: #121516;
    --pv-dark-2: #171a1c;
    --pv-dark-3: #212528;
    --pv-light-1: #eaece6;
    --pv-light-2: #fcfcfb;
    --pv-light-3: #dcdfd9;
    --pv-1: var(--pv-dark-1);
    --pv-2: var(--pv-dark-2);
    --pv-3: var(--pv-dark-3);
    position: relative;
    display: flex;
    height: 76px;
    border-radius: var(--radius-md);
    border: 1px solid var(--border);
    overflow: hidden;
    background: var(--pv-2);
  }
  .pv.light {
    --pv-1: var(--pv-light-1);
    --pv-2: var(--pv-light-2);
    --pv-3: var(--pv-light-3);
  }
  .theme-card.sel .pv {
    box-shadow: inset 0 0 0 2px var(--accent-hover);
  }
  .pv-side {
    width: 34%;
    padding: 8px 6px;
    display: flex;
    flex-direction: column;
    gap: 5px;
    background: var(--pv-1);
  }
  .pv-main {
    flex: 1;
    padding: 10px 8px;
    display: flex;
    flex-direction: column;
    justify-content: flex-end;
    gap: 6px;
  }
  .pv i {
    display: block;
    height: 5px;
    border-radius: 3px;
    background: var(--pv-3);
  }
  .pv-side i:first-child {
    width: 70%;
  }
  .pv-main .l1 {
    width: 80%;
  }
  .pv-main .l2 {
    width: 34%;
    background: var(--accent);
  }
  /* Match system: the right half is the light ground. */
  .pv-half {
    position: absolute;
    inset: 0 0 0 50%;
    display: flex;
    --pv-1: var(--pv-light-1);
    --pv-2: var(--pv-light-2);
    --pv-3: var(--pv-light-3);
    background: var(--pv-2);
  }
  .pack-card:focus-visible {
    outline: none;
  }
  .pack-card:focus-visible .pk,
  .pack-card:focus-visible .fxpv {
    outline: var(--focus-ring);
    outline-offset: 2px;
  }
  .pack-card.sel .pk,
  .pack-card.sel .fxpv {
    box-shadow: inset 0 0 0 2px var(--accent-hover);
  }
  .pk-name {
    margin-top: var(--sp-1);
    font-weight: 600;
  }
  .pk-note {
    font-size: var(--fs-tiny);
    line-height: 1.3;
    color: var(--text-muted);
    min-height: 2.6em;
  }
  .pk {
    position: relative;
    width: 100%;
    height: 52px;
    border-radius: var(--radius-md);
    border: 1px solid var(--border);
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
    gap: var(--sp-1);
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
    font-size: var(--fs-body);
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

  /* Effect cards. The mini window is deliberately NOT painted from a fixed
     palette the way the pack cards are: an effect has no colours of its own,
     it inherits the ground it falls on, and --fx-ink flips with that ground.
     Painting these from the live tokens is therefore the only honest preview —
     and it makes the row re-skin itself the moment you pick a pack above. */
  .fxpv {
    position: relative;
    width: 100%;
    height: 52px;
    border-radius: var(--radius-md);
    border: 1px solid var(--border);
    background: var(--bg-1);
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: var(--sp-2);
    padding: 0 var(--sp-3);
    overflow: hidden;
    isolation: isolate;
  }
  /* Two bars of body text under the weather — the thing the effect must not
     make harder to read. If a card looks busy here, it will look busy over the
     feed. */
  .fxpv-ink {
    height: 5px;
    border-radius: 3px;
    background: color-mix(in srgb, var(--text) 55%, transparent);
  }
  .fxpv-ink.short {
    width: 58%;
  }

  .pk-glow {
    position: absolute;
    inset: -40%;
    background: var(--pk-grad);
    opacity: 0.85;
  }
  .pk[data-motion="sweep"] .pk-glow {
    animation: pk-sweep 3.6s linear infinite;
  }
  @keyframes pk-sweep {
    0% {
      transform: translate3d(-58%, -20%, 0);
    }
    100% {
      transform: translate3d(58%, 20%, 0);
    }
  }
  /* With sweeping highlights off, Aurora's card says so. It damps rather than
     deletes, because that is what the pack itself does. */
  .pk.dimshine .pk-glow {
    opacity: 0.28;
  }
  /* Translucent mini-surfaces, so the backdrop reads through them exactly the
     way it does in the app. */
  .pk-glow ~ .pk-rail {
    background: color-mix(in srgb, var(--pk-bg) 70%, transparent);
  }
  .pk-glow ~ .pk-body {
    background: color-mix(in srgb, var(--pk-bg) 60%, transparent);
  }
  :global(:root[data-motion="reduced"]) .pk-glow {
    animation: none !important;
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

  /* Accent swatches: filled dots; the profile one is hollow (a ring of the
     profile color) so it reads as "custom", not just another preset. */
  .swatches {
    display: flex;
    gap: var(--sp-2);
    flex-wrap: wrap;
  }
  .swatch {
    width: 22px;
    height: 22px;
    padding: 0;
    border-radius: 50%;
    background: var(--sw);
    border: none;
  }
  .swatch.sel {
    box-shadow:
      0 0 0 2px var(--bg-elevated),
      0 0 0 4px var(--text);
  }
  .swatch.profile {
    background: transparent;
    box-shadow: inset 0 0 0 3px var(--sw);
  }
  .swatch.profile.sel {
    background: var(--sw);
    box-shadow:
      0 0 0 2px var(--bg-elevated),
      0 0 0 4px var(--text);
  }
  .scale-row {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    min-width: 200px;
  }
  .scale-row input[type="range"] {
    width: 200px;
  }
  .scale-val {
    min-width: 40px;
    text-align: end;
    font-size: var(--fs-compact);
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
  }
  @media (max-width: 768px) {
    /* The phone's tap floor (Modal.svelte) makes every button --tap-min tall;
       a 32px-wide swatch under it was a lozenge. Square it to the floor. */
    .swatches {
      gap: var(--sp-1);
      flex-wrap: nowrap;
      width: 100%;
    }
    /* Seven to a row, each as large as the floor allows and round. */
    .swatches .swatch {
      flex: 1 1 0;
      width: auto;
      max-width: var(--tap-min);
      height: auto;
      min-height: 0;
      aspect-ratio: 1;
    }
    .fx-row {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .pk-note {
      min-height: 0;
    }
    .theme-row {
      gap: var(--sp-2);
    }
    .pv {
      height: 64px;
    }
    .scale-row {
      width: 100%;
    }
    .scale-row input[type="range"] {
      flex: 1;
      width: auto;
    }
  }
  /* A forced-colours mode (Windows high contrast, and the GTK/Chromium
     equivalent) throws away every colour on the page and repaints it from the
     OS palette. On every other screen in this app that is exactly right — it is
     someone telling their machine they cannot read our greys. Here it deletes
     the content, because on this screen the content IS colour: the pack
     previews and three theme cards flatten into identical empty
     rectangles, and the one thing that said which one you were on — a border
     tinted with the accent — flattens with them, so the panel becomes a wall of
     unlabelled boxes offering a choice it can no longer show you.

     Swatches are the case `forced-color-adjust` exists for, and these qualify
     twice over: they are aria-hidden decoration, so a screen reader loses
     nothing, and their whole job is to be looked at. Only the previews opt out;
     every word on the screen still takes the user's palette. The selection then
     needs a marker the OS keeps, which is what the outline is. */
  @media (forced-colors: active) {
    .pk,
    .pk *,
    .pv,
    .pv *,
    .swatch {
      forced-color-adjust: none;
    }
    .pack-card.sel .pk,
    .theme-card.sel .pv,
    .swatch.sel {
      outline: 3px solid Highlight;
      outline-offset: 2px;
    }
  }
</style>
