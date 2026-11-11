<script>
  // Appearance: theme (Dark / Light / System), accent color, and message
  // density. Everything is device-local (S.prefs) and applies live — no save
  // step. setAppearance persists the pref and stamps <html> (data-theme /
  // data-density / --accent) with a short cross-fade; see state.svelte.js.
  import { slide } from "svelte/transition";
  import { motionInView } from "../lib/inview.js";
  import RailShell from "./RailShell.svelte";
  import SettingRow from "./SettingRow.svelte";
  import Segmented from "../Segmented.svelte";
  import Select from "../Select.svelte";
  import SettingGroup from "./SettingGroup.svelte";
  import FxOverlay from "../FxOverlay.svelte";
  import { THEME_FX } from "../lib/themefx.js";
  import { S, setAppearance } from "../lib/state.svelte.js";
  import { ACCENT_PROFILE, profileAccent } from "../lib/guildaccent.js";
  import { rangefill } from "../lib/rangefill.js";

  let { onClose } = $props();


  // Gallery filter. Fifty-one packs in five galleries is more than a list you
  // read; it is a list you look something up in.
  let q = $state("");
  const nq = $derived(q.trim().toLowerCase());
  const sift = (list) =>
    nq ? list.filter((p) => `${p.label} ${p.note}`.toLowerCase().includes(nq)) : list;

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
    { name: "Concord teal", color: "" },
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
  const themePack = $derived(S.prefs.themePack || "");
  // Picking a theme is asking for that ground, which a pack overrides; it used
  // to do nothing visible. Hand the ground back to the theme.
  function pickTheme(id) {
    if (themePack) setAppearance("themePack", "");
    setAppearance("theme", id);
  }
  const themeFx = $derived(S.prefs.themeFx || "");
  // The specular pass on a live backdrop (see app.css, "Shine off"). Only the
  // literal false is off, so a pref written by a newer build reads as the
  // default look rather than as some third state.
  const shine = $derived(S.prefs.themeShine !== false);
  const shape = $derived(S.prefs.shape || "");
  const font = $derived(S.prefs.font || "");

  // Three densities, not two. [data-density] was already a real attribute, but
  // only a pack could reach it and only "compact" did anything; "cosy" was the
  // name of not-compact. It is now a first-class setting with a rung on either
  // side of the default, and the default itself sits where a mainstream chat
  // app sits rather than where a dense one does.
  const DENSITIES = [
    { id: "compact", label: "Compact", note: "Tight rows, smaller reading size — more of the conversation per screen." },
    { id: "cozy", label: "Cosy", note: "The default: room between messages, comfortable reading size." },
    { id: "comfortable", label: "Comfortable", note: "Wide rows and larger type — easiest to read across the room." },
  ];

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

  // Effect-paired packs: flat, opaque palettes designed as the ground for one
  // of the Effects below. `note` names the effect rather than describing the
  // colours, because the pairing is the reason each of these exists — but
  // selecting one only changes the pack. Nothing here turns an effect on.
  const PAIR_PACKS = [
    { id: "tundra", label: "Tundra", bg: "#131c25", hi: "#25333f", ac: "#8fd3f4", font: INTER, r: 10, av: "40%", rule: true, note: "Built for Snow" },
    { id: "harbor", label: "Harbor", bg: "#171e23", hi: "#2a343c", ac: "#e0a458", font: GROTESK, r: 3, av: "5px", rule: true, note: "Built for Rain" },
    { id: "observatory", label: "Observatory", bg: "#0b0d17", hi: "#1b1f2e", ac: "#e8c46a", font: INTER, r: 12, av: "50%", note: "Built for Starfield" },
    { id: "hearth", label: "Hearth", bg: "#1a1613", hi: "#2e2721", ac: "#e59b4d", font: NUNITO, r: 14, av: "50%", card: true, note: "Built for Embers" },
    { id: "phosphor", label: "Phosphor", bg: "#060c08", hi: "#132119", ac: "#3ef08c", font: MONO, r: 0, av: "0px", note: "Built for CRT" },
    { id: "harvest", label: "Harvest", bg: "#1c1a11", hi: "#322e1f", ac: "#cf7b3f", font: SERIF, r: 5, av: "8px", rule: true, note: "Built for Leaves" },
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
    { id: "aurora", label: "Aurora", bg: "#061b21", hi: "#112e34", ac: "#39d9b0", font: GROTESK, r: 10, av: "40%", motion: "sweep", grad: "linear-gradient(103deg,transparent 34%,#29d69e 42%,#7af6d6 48%,#46e2ff 54%,transparent 64%)", base: "linear-gradient(180deg,#16505a,#04161d)", shine: true, note: "Sweeping curtain" },
    { id: "synthwave", label: "Synthwave", bg: "#16072a", hi: "#260f3e", ac: "#ff4fd8", font: GROTESK, r: 0, av: "2px", motion: "sun", grad: "linear-gradient(180deg,#ffd873,#ff7ec9 55%,#b3379b)", base: "linear-gradient(180deg,#300a50 0%,#4a1069 54%,#12021f 58%)", note: "Sun + rushing grid" },
    { id: "cosmos", label: "Cosmos", bg: "#0a0c1b", hi: "#151930", ac: "#7c8bff", font: INTER, r: 13, av: "50%", motion: "star", grad: "linear-gradient(120deg,transparent 44%,#d6ecff 49.4%,transparent 55%)", base: "radial-gradient(120% 110% at 50% 20%,#1b2059,#04040c)", note: "Shooting stars" },
    { id: "molten", label: "Molten", bg: "#1c0e08", hi: "#2e180e", ac: "#ff7a2f", font: SERIF, r: 4, av: "8px", motion: "heat", grad: "linear-gradient(0deg,transparent 30%,#ff7a2f 42%,#ffc46e 50%,transparent 66%)", base: "radial-gradient(120% 100% at 50% 110%,#7a2708,#0e0704)", shine: true, note: "Rising heat, serif" },
    { id: "prism", label: "Prism", bg: "#12141a", hi: "#20232c", ac: "#8de0ff", font: GROTESK, r: 14, av: "50%", motion: "sweep", grad: "linear-gradient(100deg,transparent 36%,#ff5c9e 41%,#ffc454 45%,#60f0aa 49%,#60c8ff 53%,#9682ff 57%,transparent 63%)", base: "linear-gradient(180deg,#23262f,#0a0b0e)", shine: true, note: "Iridescent sweep" },
    { id: "monsoon", label: "Monsoon", bg: "#0b1722", hi: "#162838", ac: "#56b7e8", font: INTER, r: 5, av: "8px", motion: "rain", grad: "repeating-linear-gradient(14deg,rgba(186,226,255,0.55) 0 1.5px,rgba(186,226,255,0.14) 1.5px 3px,transparent 3px 13px)", base: "linear-gradient(178deg,#17384f,#06141f)", note: "Falling rain" },
    { id: "fathom", label: "Fathom", bg: "#04202c", hi: "#0c2d3a", ac: "#35d0e8", font: NUNITO, r: 12, av: "50%", card: true, motion: "bubbles", grad: "radial-gradient(circle at 30% 22%,rgba(200,248,255,0.85) 0 1.6px,transparent 2px),radial-gradient(circle at 74% 70%,rgba(180,240,255,0.7) 0 1.2px,transparent 1.8px) 0 0/26px 26px repeat", base: "radial-gradient(120% 100% at 50% -10%,#0e5468,#01131c)", note: "Light shafts, bubbles" },
    { id: "skyline", label: "Skyline", bg: "#0e0a1e", hi: "#1e1436", ac: "#35e0ff", font: GROTESK, r: 0, av: "2px", motion: "city", grad: "linear-gradient(90deg,#2c1c52 0 8px,transparent 8px) 0 100%/28px 58% repeat-x,linear-gradient(90deg,transparent 0 13px,#08040f 13px 21px,transparent 21px) 0 100%/28px 100% repeat-x", base: "linear-gradient(180deg,#150c33 0%,#4b1550 58%,#7d1f4e 68%,#07040f 74%)", note: "Neon city, parallax" },
    { id: "eclipse", label: "Eclipse", bg: "#120a20", hi: "#241638", ac: "#f0d68a", font: SERIF, r: 14, av: "50%", motion: "rays", grad: "repeating-conic-gradient(from 0deg,transparent 0 5deg,rgba(255,236,190,0.55) 6deg 8deg,transparent 9deg 15deg)", base: "radial-gradient(120% 110% at 50% 36%,#2a1746,#05030c)", note: "Turning corona" },
    { id: "daybreak", label: "Daybreak", bg: "#dceaf6", hi: "#c4d5e8", ac: "#1d6fbf", font: NUNITO, r: 10, av: "50%", day: true, motion: "clouds", grad: "radial-gradient(24% 15% at 18% 28%,rgba(255,255,255,0.95),transparent 72%),radial-gradient(18% 11% at 62% 60%,rgba(255,255,255,0.85),transparent 72%) 0 0/64px 40px repeat", base: "linear-gradient(180deg,#7cbdec,#b7dcf6 45%,#ffe9c9 80%,#ffd6a0)", note: "Morning sky, clouds" },
    { id: "dunes", label: "Dunes", bg: "#261609", hi: "#3e2714", ac: "#e8a33f", font: SERIF, r: 4, av: "7px", rule: true, motion: "heat", grad: "linear-gradient(0deg,transparent 26%,#ffbe6e 40%,#ffe0aa 48%,transparent 68%)", base: "linear-gradient(180deg,#9c531a 0%,#d98b33 28%,#7a4416 40%,#150b04 78%)", shine: true, note: "Desert heat, dust" },
    { id: "canopy", label: "Canopy", bg: "#0a1a0f", hi: "#17321e", ac: "#62c46a", font: INTER, r: 9, av: "40%", motion: "bubbles", grad: "radial-gradient(circle at 32% 26%,rgba(255,246,190,0.9) 0 1.8px,transparent 2.4px),radial-gradient(circle at 76% 66%,rgba(210,250,170,0.8) 0 1.3px,transparent 1.9px) 0 0/26px 26px repeat", base: "radial-gradient(120% 100% at 50% -10%,#2f6b33,#050f08 75%)", note: "Dappled light, pollen" },
    { id: "datastream", label: "Datastream", bg: "#020c06", hi: "#092011", ac: "#35f08a", font: MONO, r: 0, av: "0px", motion: "code", grad: "radial-gradient(2px 20px at 22% 30%,rgba(60,255,150,0.75),transparent),radial-gradient(2px 14px at 68% 70%,rgba(120,255,190,0.6),transparent) 0 0/30px 44px repeat", base: "linear-gradient(180deg,#04160b,#000402)", note: "Falling code" },
    { id: "sonar", label: "Sonar", bg: "#041414", hi: "#0b2826", ac: "#ffb347", font: MONO, r: 2, av: "50%", rule: true, motion: "scope", grad: "conic-gradient(from 0deg,rgba(255,179,71,0.55) 0deg,rgba(255,179,71,0.16) 26deg,transparent 62deg)", base: "radial-gradient(90% 90% at 50% 50%,#073030,#010a0a)", shine: true, note: "Turning scope" },
    { id: "lantern", label: "Lantern", bg: "#081422", hi: "#13283e", ac: "#ff9d4d", font: NUNITO, r: 13, av: "50%", card: true, motion: "bubbles", grad: "radial-gradient(circle at 34% 30%,rgba(255,186,110,0.95) 0 2.6px,transparent 3.4px),radial-gradient(circle at 74% 72%,rgba(255,210,150,0.8) 0 1.8px,transparent 2.6px) 0 0/28px 28px repeat", base: "linear-gradient(180deg,#0a1e35,#14304c 70%,#060e18)", note: "Lanterns rising" },
    { id: "glacier", label: "Glacier", bg: "#102230", hi: "#1f3c50", ac: "#9fe0ff", font: GROTESK, r: 2, av: "4px", rule: true, motion: "facets", grad: "repeating-linear-gradient(64deg,transparent 0 9px,rgba(215,245,255,0.5) 11px,#ffffff 12px,transparent 14px 22px)", base: "linear-gradient(158deg,#2b5f7d,#0a2130 72%,#050f17)", shine: true, note: "Ice, hard glint" },
    { id: "vinyl", label: "Vinyl", bg: "#20160e", hi: "#362719", ac: "#d9a05b", font: SERIF, r: 8, av: "50%", motion: "scope", grad: "repeating-radial-gradient(circle at 50% 50%,rgba(0,0,0,0.5) 0 2px,rgba(255,214,160,0.16) 2px 4px)", base: "radial-gradient(110% 100% at 26% 34%,#4a3520,#100a05)", note: "A record, turning" },
    { id: "storm", label: "Storm", bg: "#14171b", hi: "#272b31", ac: "#9fb4c9", font: INTER, r: 3, av: "5px", rule: true, motion: "clouds", grad: "radial-gradient(30% 20% at 22% 26%,rgba(226,234,244,0.65),transparent 74%),radial-gradient(22% 14% at 66% 58%,rgba(190,202,214,0.5),transparent 74%) 0 0/70px 44px repeat", base: "linear-gradient(180deg,#2b3238,#0c0f12 80%)", note: "Cloud, distant strike" },
    { id: "blossom", label: "Blossom", bg: "#241016", hi: "#3b1b25", ac: "#ff7aa2", font: NUNITO, r: 14, av: "50%", card: true, motion: "petals", grad: "radial-gradient(4px 2.6px at 30% 24%,rgba(255,190,210,0.95),transparent),radial-gradient(3px 2px at 72% 66%,rgba(255,160,190,0.85),transparent) 0 0/30px 38px repeat", base: "linear-gradient(180deg,#7a2437 0%,#b8455c 34%,#3a1220 58%,#150609)", note: "Petals at dusk" },
    { id: "meridian", label: "Meridian", bg: "#081a20", hi: "#112f37", ac: "#ff9a63", font: SERIF, r: 6, av: "50%", rule: true, motion: "path", grad: "repeating-linear-gradient(0deg,rgba(255,186,118,0.7) 0 2px,transparent 2px 7px)", base: "linear-gradient(180deg,#d9673a 0%,#f0a05a 20%,#123039 34%,#040c10)", note: "Sun on the water" },
    { id: "bloom", label: "Bloom", bg: "#101408", hi: "#1f270f", ac: "#b6e830", font: GROTESK, r: 16, av: "50%", motion: "heat", grad: "radial-gradient(38% 26% at 40% 50%,rgba(182,232,48,0.75),transparent 70%),radial-gradient(30% 20% at 72% 50%,rgba(255,176,60,0.55),transparent 72%)", base: "radial-gradient(120% 100% at 50% 110%,#3d4a10,#080a03 70%)", note: "Slow rising blobs" },
    { id: "orbit", label: "Orbit", bg: "#0a1120", hi: "#1a2440", ac: "#e2ecff", font: INTER, r: 10, av: "50%", rule: true, motion: "limb", grad: "radial-gradient(1.6px 1.6px at 8px 5px,rgba(255,200,140,0.95),transparent) 0 0/34px 26px repeat,radial-gradient(1.3px 1.3px at 22px 14px,rgba(255,186,120,0.8),transparent) 0 0/34px 26px repeat", base: "radial-gradient(150% 96% at 50% 152%,#12365c 0 96%,rgba(130,210,255,0.9) 98.5%,transparent 100%),radial-gradient(120% 90% at 50% 4%,#0c1330,#01030a 78%)", note: "The night side, turning" },
    { id: "radiant", label: "Radiant", bg: "#080e20", hi: "#161f3c", ac: "#5b9dff", font: MONO, r: 16, av: "40%", motion: "fly", grad: "radial-gradient(circle at 24% 30%,rgba(226,240,255,0.95) 0 1.3px,transparent 1.9px) 0 0/34px 34px repeat,radial-gradient(circle at 68% 62%,rgba(180,214,255,0.85) 0 1px,transparent 1.6px) 0 0/34px 34px repeat,radial-gradient(circle at 82% 22%,#fff 0 1.2px,transparent 1.8px) 0 0/34px 34px repeat", base: "radial-gradient(110% 110% at 62% 26%,#0d1a3c,#010206 72%)", note: "Meteors, coming at you" },
    { id: "silicon", label: "Silicon", bg: "#0d1728", hi: "#1a2c44", ac: "#7cf3e0", font: GROTESK, r: 3, av: "3px", card: true, motion: "bus", grad: "radial-gradient(9px 1.2px at 10px 23px,rgba(170,255,246,0.95),transparent) 0 0/46px 69px repeat,radial-gradient(7px 1.2px at 34px 46px,rgba(130,240,255,0.8),transparent) 0 0/46px 69px repeat", base: "repeating-linear-gradient(90deg,transparent 0 22px,rgba(124,243,224,0.14) 22px 23px),repeating-linear-gradient(0deg,transparent 0 22px,rgba(124,243,224,0.12) 22px 23px),radial-gradient(120% 100% at 28% 8%,#17253f,#05080f 76%)", note: "Traffic on a die" },
    { id: "uptime", label: "Uptime", bg: "#0e151c", hi: "#1d2833", ac: "#2fd18b", font: NUNITO, r: 6, av: "4px", rule: true, motion: "blink", grad: "radial-gradient(4px 4px at 7px 7px,rgba(80,255,176,0.95) 0 26%,rgba(60,230,160,0.2) 48%,transparent 78%) 0 0/38px 39px repeat,radial-gradient(4px 4px at 7px 20px,rgba(80,255,176,0.8) 0 26%,rgba(60,230,160,0.18) 48%,transparent 78%) 0 0/38px 39px repeat,radial-gradient(3.6px 3.6px at 13px 33px,rgba(255,200,104,0.9) 0 26%,rgba(255,170,60,0.18) 48%,transparent 78%) 0 0/38px 39px repeat", base: "repeating-linear-gradient(90deg,rgba(0,0,0,0.5) 0 3px,transparent 3px 38px),linear-gradient(180deg,#121a21,#05080c 82%)", note: "A wall of status lights" },
    { id: "zellige", label: "Zellige", bg: "#082426", hi: "#103436", ac: "#2ec4b6", font: INTER, r: 18, av: "50%", card: true, motion: "swing", grad: "radial-gradient(48% 46% at 50% 4%,rgba(255,206,130,0.55),transparent 72%)", base: "repeating-linear-gradient(45deg,rgba(226,200,140,0.18) 0 1px,transparent 1px 22px),repeating-linear-gradient(-45deg,rgba(226,200,140,0.18) 0 1px,transparent 1px 22px),repeating-conic-gradient(rgba(30,140,132,0.22) 0 25%,transparent 0 50%) 0 0/44px 44px,linear-gradient(180deg,#0e403c,#030f11 88%)", note: "Tilework, a swinging lamp" },
    { id: "atrium", label: "Atrium", bg: "#f2efe6", hi: "#ded8c8", ac: "#0f8a86", font: SERIF, r: 24, av: "50%", card: true, day: true, motion: "bars", grad: "repeating-linear-gradient(76deg,rgba(64,84,116,0.16) 0 1.5px,transparent 1.5px 19.5px),repeating-linear-gradient(166deg,rgba(64,84,116,0.1) 0 1.5px,transparent 1.5px 33px)", base: "linear-gradient(180deg,#eef4f8,#f7f3ea 46%,#e6ddcc)", note: "Noon under glass" },
  ];

  const ALL_PACKS = [...PACKS, ...DAY_PACKS, ...LIVE_PACKS, ...TEXTURE_PACKS, ...PAIR_PACKS];
  // Which pack you are ON, said above the fold. The gallery used to answer that
  // only by scrolling until you found the ring.
  const currentPack = $derived(ALL_PACKS.find((p) => p.id === themePack) || ALL_PACKS[0]);
  // "Default" is not a pack: it is whichever base theme is on, so its card is
  // drawn in that theme. Under Light it showed the dark ground while ringed as
  // current, which read as "this would make it dark".
  const DEFAULT_LIGHT = { bg: "#eaece6", hi: "#dcdfd9", ac: "#0e7c70", day: true };
  // Under Light, Default is drawn light, so it is filed first among the
  // light grounds rather than under "Dark grounds — these bring their own".
  const packsF = $derived(sift(shownLight ? PACKS.filter((p) => p.id !== "") : PACKS));
  const dayF = $derived(
    sift(shownLight ? [{ ...PACKS.find((p) => p.id === ""), ...DEFAULT_LIGHT }, ...DAY_PACKS] : DAY_PACKS),
  );
  const liveF = $derived(sift(LIVE_PACKS));
  const textureF = $derived(sift(TEXTURE_PACKS));
  const pairF = $derived(sift(PAIR_PACKS));
  const hits = $derived(packsF.length + dayF.length + liveF.length + textureF.length + pairF.length);

  // "Jump" scrolls the selected card into the middle of the dialog. The ring is
  // already drawn; this is only about finding it.
  function jumpToCurrent() {
    document
      .querySelector(".dialog .pack-card.sel")
      ?.scrollIntoView({ block: "center", behavior: "smooth" });
  }

  // Device-local display prefs read by the feed and the shell. Their defaults
  // are off / "sidebar", so a missing key reads as the default.
  const roleNames = $derived(S.prefs.roleNameColors === true);
  const underline = $derived(S.prefs.underlineLinks === true);
  const navModel = $derived(S.prefs.navModel === "rail" ? "rail" : "sidebar");
</script>

<RailShell title="Appearance" here="appearance" {onClose}>
  <!-- One card shape for all five galleries. The mini window is drawn from the
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
      use:motionInView
    >
      <span
        class="pk"
        class:still
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
      <!-- A dark pack while Light is on screen: picking it brings its own
           dark ground, and the card says so before you find out. -->
      <span class="pk-note">{shownLight && p.id && !p.day ? "Dark only — brings its own dark ground" : p.note}</span>
    </button>
  {/snippet}

  {#snippet gallery(tag, blurb, list, still = false)}
    {#if list.length}
      <div class="live-head">
        <span class="live-tag">{tag}</span>
        <span class="muted tiny">{blurb}</span>
      </div>
      <div class="pack-row" role="radiogroup" aria-label="{tag} theme pack">
        {#each list as p (p.id)}{@render packCard(p, still || p.still)}{/each}
      </div>
    {/if}
  {/snippet}

  <section class="blk">
    <h4 class="lbl">Theme</h4>
    <div class="theme-row" role="radiogroup" aria-label="Theme">
      {#each THEMES as t (t.id)}
        <button
          class="theme-card"
          class:sel={theme === t.id}
          role="radio"
          aria-checked={theme === t.id}
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
    {#if themePack}
      <!-- A pack brings its own colours, so Light/Dark/System changed nothing
           while one was chosen. Say so, and make picking one mean it. -->
      <p class="muted tiny">
        {currentPack.label} brings its own colours, so it is used instead. Picking a theme here
        switches the pack back to Default.
      </p>
    {/if}
  </section>

  <section class="blk">
    <div class="lbl-row">
      <h4 class="lbl">Theme pack</h4>
      <span class="gal-cur">{currentPack.label}</span>
      <button class="link gal-jump" onclick={jumpToCurrent}>Show current</button>
    </div>
    <input
      class="gal-q"
      type="search"
      bind:value={q}
      placeholder="Filter {ALL_PACKS.length} packs"
      aria-label="Filter theme packs"
    />

    <!-- In Light the packs that have a light ground come first: eleven
         "Dark only" cards above the fold answered the wrong question. -->
    {#if shownLight}
      {@render gallery("Daylight", "Bright grounds, dark ink.", dayF)}
      {@render gallery("Flat", "Dark grounds — these bring their own.", packsF)}
    {:else}
      {@render gallery("Flat", "Opaque palettes, no backdrop.", packsF)}
      {@render gallery("Daylight", "Bright grounds, dark ink.", dayF)}
    {/if}
    {@render gallery("Animated", "A living backdrop moves behind the app.", liveF)}
    {#if liveF.length}
      <!-- Nested, because it changes the Animated packs and nothing else. -->
      <SettingRow
        title="Sweeping highlights"
        sub="The band of light that crosses the window on a live pack"
        info="Most animated packs carry one bright layer that travels over the whole window — a curtain, an iridescent bar, a rolling scanline, a wave of heat, ice catching the light. Turning this off leaves every pack its colour, its scenery and the rest of its motion, and takes away only the highlight that passes over what you are reading. Packs without one are unaffected."
        checked={shine}
        onclick={() => setAppearance("themeShine", !shine)}
      />
    {/if}
    {@render gallery("Textured", "A soft colour mesh, no animation.", textureF, true)}
    {@render gallery("Effect-paired", "Flat palettes drawn as the ground for one effect.", pairF)}

    {#if !hits}
      <p class="muted tiny">No pack matches “{q.trim()}”.</p>
    {/if}
    <p class="muted tiny">
      A pack is a whole look, not a hue: its own typeface, corner radius, avatar shape, shadow
      depth and feed rhythm come with it. Accent, corners and typeface below still override it.
    </p>
  </section>

  <SettingGroup label="Colour and type">
    <SettingRow title="Accent" sub="Your colour for focus, selection, mentions of you and your reactions. The ring follows your profile colour.">
      <div class="swatches" role="radiogroup" aria-label="Accent colour">
        {#each ACCENTS as a (a.name)}
          <button
            class="swatch"
            class:sel={accent === a.color}
            role="radio"
            aria-checked={accent === a.color}
            title={a.name}
            aria-label={a.name}
            style="--sw:{a.color || "#14a394"}"
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
    <SettingRow title="Density" sub={DENSITIES.find((d) => d.id === density)?.note}>
      <Segmented
        label="Message density"
        value={density}
        options={DENSITIES.map((d) => ({ value: d.id, label: d.label }))}
        onChange={(v) => setAppearance("density", v)}
      />
    </SettingRow>
    <SettingRow
      title="Colour names by role"
      sub="Tint names with their top role's colour, kept readable."
      checked={roleNames}
      onclick={() => setAppearance("roleNameColors", !roleNames)}
    />
    <SettingRow
      title="Underline links"
      sub="Always underline links in messages."
      checked={underline}
      onclick={() => setAppearance("underlineLinks", !underline)}
    />
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
    <SettingRow title="Typeface" sub="Theme follows the pack. Every face ships inside the app — none is downloaded.">
      <Select
        label="Typeface"
        value={font}
        onPick={(v) => setAppearance("font", v)}
        options={FONTS.map((f) => ({ value: f.id, label: f.label }))}
      />
    </SettingRow>
    <SettingRow title="Corners" sub="How rounded panels, buttons and fields are. Theme follows the pack.">
      <Segmented
        label="Corner style"
        value={shape}
        onChange={(v) => setAppearance("shape", v)}
        options={SHAPES.map((x) => ({ value: x.id, label: x.label }))}
      />
    </SettingRow>
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

  <SettingGroup label="Layout">
    <SettingRow
      title="Guilds"
      sub="Where your guilds sit: a strip of shortcuts above the list, or a column beside it."
    >
      <Segmented
        label="Where your guilds sit"
        value={navModel}
        onChange={(v) => setAppearance("navModel", v)}
        options={[
          { value: "sidebar", label: "Strip" },
          { value: "rail", label: "Rail" },
        ]}
      />
    </SettingRow>
  </SettingGroup>

  <SettingGroup label="Effects">
    <div class="pack-row fx-row" role="radiogroup" aria-label="Visual effect">
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
            <FxOverlay fx={f.id} mini s={0.18} scale={0.3} />
            <span class="fxpv-ink"></span>
            <span class="fxpv-ink short"></span>
          </span>
          <span class="pk-name">{f.label}</span>
          <span class="pk-note">{f.note}</span>
        </button>
      {/each}
    </div>
    <p class="muted tiny">
      An effect layers over whatever pack you picked. Drawn on this device, never downloaded, and
      off entirely if your system asks for reduced motion. It starts off on each device.
    </p>
    <SettingRow
      title="Seasonal touches"
      sub="Snow in December, petals in spring, leaves in autumn — on the guild rail."
      info="A sparse drift over the classic guild rail, driven by this device's clock only — nothing is fetched. Quiet months show nothing."
      checked={S.prefs.seasonal === true}
      onclick={() => setAppearance("seasonal", S.prefs.seasonal !== true)}
    />
  </SettingGroup>
</RailShell>

<style>
  /* Appearance: the theme cards and the pack gallery on top, then rows. */
  .blk {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    padding-bottom: var(--sp-4);
    border-bottom: 1px solid var(--hairline);
    margin-bottom: 6px;
  }
  .lbl {
    margin: 0;
    font-size: var(--fs-compact);
    font-weight: 600;
    color: var(--text);
  }
  .lbl-row {
    display: flex;
    align-items: baseline;
    gap: var(--sp-2);
  }
  .gal-cur {
    font-size: var(--fs-compact);
    color: var(--text-muted);
    margin-right: auto;
  }
  .gal-jump {
    font-size: var(--fs-compact);
  }
  .gal-q {
    width: 100%;
    height: 32px;
    padding: 0 var(--sp-3);
  }
  p {
    margin: 0;
    font-size: var(--fs-compact);
    line-height: 1.5;
  }
  .tiny {
    font-size: var(--fs-small);
  }
  .muted {
    color: var(--text-muted);
  }

  /* Three equal theme cards: a miniature of each ground, one indicator — an
     inset ring in the accent ink — and the label under it. */
  .theme-row {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: var(--sp-3);
  }
  .theme-card {
    display: flex;
    flex-direction: column;
    gap: 6px;
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

  /* The pack gallery: four across in the XL pane, two on a phone. A card is a
     mini window, a name and one line of character; chosen = inset ring. */
  .live-head {
    display: flex;
    align-items: baseline;
    gap: var(--sp-2);
    margin-top: var(--sp-2);
  }
  .live-tag {
    font-size: var(--fs-compact);
    font-weight: 600;
    color: var(--text);
  }
  .pack-row {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: var(--sp-3) 10px;
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
    /* Every live pack card runs an infinite CSS animation. Off-screen cards
       render, and therefore animate, not at all. */
    content-visibility: auto;
    contain-intrinsic-size: 112px 92px;
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
  /* Seven options into a three-wide grid leaves an orphan on its own line, so
     "None" — the default, and the only card with nothing to show — takes a
     full-width row and the six real effects fall into 3x2 beneath it. Same
     move the typeface list makes for the same reason. */
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
    gap: 6px;
    padding: 0 10px;
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
  /* Fathom's bubbles rise one tile per loop, the same construction the real
     backdrop uses. */
  .pk[data-motion="bubbles"] .pk-glow {
    inset: -30%;
    animation: pk-rise 2.6s linear infinite;
  }
  /* Skyline: silhouettes stand on the bottom edge and slide one tile sideways,
     so the card shows the parallax rather than a coloured wash. */
  .pk[data-motion="city"] .pk-glow {
    inset: auto -20% 0 -20%;
    height: 62%;
    opacity: 1;
    animation: pk-city 9s linear infinite;
  }
  /* Eclipse: the corona, turning. The disc is drawn by the ring on top of it. */
  .pk[data-motion="rays"] .pk-glow {
    inset: auto;
    left: 50%;
    top: 50%;
    width: 42px;
    height: 42px;
    margin: -21px 0 0 -21px;
    border-radius: 50%;
    mask-image: radial-gradient(closest-side, transparent 0 38%, #000 46%, #000 64%, transparent 96%);
    animation: pk-spin 26s linear infinite;
  }
  /* Daybreak and Storm: cloud masses drifting one tile sideways. */
  .pk[data-motion="clouds"] .pk-glow {
    inset: -30%;
    animation: pk-clouds 7s linear infinite;
  }
  /* Glacier: facets shearing along their own normal. */
  .pk[data-motion="facets"] .pk-glow {
    inset: -40%;
    animation: pk-facets 4.4s linear infinite;
  }
  @keyframes pk-clouds {
    to {
      transform: translate3d(-64px, 0, 0);
    }
  }
  @keyframes pk-facets {
    to {
      transform: translate3d(-19.8px, 9.6px, 0);
    }
  }
  /* Blossom: petals down-and-across, one tile per loop. */
  .pk[data-motion="petals"] .pk-glow {
    inset: -40%;
    animation: pk-petals 4.2s linear infinite;
  }
  /* Meridian: only the sun's path on the water moves, so the card shows just
     that band rather than tinting the whole window. */
  .pk[data-motion="path"] .pk-glow {
    inset: 46% 34% 0 34%;
    animation: pk-path 1.4s linear infinite;
  }
  @keyframes pk-petals {
    to {
      transform: translate3d(-30px, 38px, 0);
    }
  }
  @keyframes pk-path {
    to {
      transform: translate3d(0, 7px, 0);
    }
  }
  /* Datastream: columns falling straight down, one tile per loop. */
  .pk[data-motion="code"] .pk-glow {
    inset: -40%;
    animation: pk-code 1.6s linear infinite;
  }
  /* Sonar and Vinyl: a full disc that turns, rather than the masked ring the
     eclipse card uses. */
  .pk[data-motion="scope"] .pk-glow {
    inset: auto;
    left: 50%;
    top: 50%;
    width: 46px;
    height: 46px;
    margin: -23px 0 0 -23px;
    border-radius: 50%;
    animation: pk-spin 5.5s linear infinite;
  }
  @keyframes pk-code {
    to {
      transform: translate3d(0, 44px, 0);
    }
  }
  @keyframes pk-rise {
    to {
      transform: translate3d(0, -26px, 0);
    }
  }
  @keyframes pk-city {
    to {
      transform: translate3d(-28px, 0, 0);
    }
  }
  @keyframes pk-spin {
    to {
      transform: rotate(360deg);
    }
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
  /* Orbit: the planet, hung off the bottom edge so the card shows the limb and
     nothing else, turning under you. */
  /* Orbit. The limb itself is painted in `base`, on the card's own box: a disc
     big enough to curve across a 112px card is also big enough that rotating
     it as a CHILD threw the mini-window's own layout 70px off its box in
     Chromium. What moves here is what you actually watch move in the app —
     the city lights sliding along the horizon — confined to the band below
     it. */
  .pk[data-motion="limb"] .pk-glow {
    inset: 74% -20% 0 -20%;
    opacity: 1;
    animation: pk-limb 9s linear infinite;
  }
  @keyframes pk-limb {
    to {
      transform: translate3d(-34px, 0, 0);
    }
  }
  /* Radiant: the field expanding out of the radiant point. */
  .pk[data-motion="fly"] .pk-glow {
    inset: -40%;
    transform-origin: 55% 38%;
    animation: pk-fly 3.2s linear infinite;
  }
  /* Silicon: pulses running one cell along the bus. The lattice they run on is
     in `base`, held still, exactly as it is in the app. */
  .pk[data-motion="bus"] .pk-glow {
    inset: -30%;
    animation: pk-bus 2.2s linear infinite;
  }
  /* Uptime: indicator lights, blinking rather than fading. */
  .pk[data-motion="blink"] .pk-glow {
    inset: 0;
    opacity: 1;
    animation: pk-blink 1.1s linear infinite;
  }
  /* Zellige: the lamp swinging over the tiles (which are in `base`). */
  .pk[data-motion="swing"] .pk-glow {
    inset: -20%;
    animation: pk-swing 4.6s ease-in-out infinite alternate;
  }
  /* Atrium: the glazing shadows, moving because the sun does. One period along
     the first family's normal, which is the vector the real pack uses scaled
     to the card's smaller pitch. */
  .pk[data-motion="bars"] .pk-glow {
    inset: -40%;
    opacity: 1;
    animation: pk-bars 26s linear infinite;
  }
  @keyframes pk-fly {
    0% {
      transform: scale(0.5);
      opacity: 0;
    }
    20% {
      opacity: 0.95;
    }
    70% {
      opacity: 0.9;
    }
    100% {
      transform: scale(3);
      opacity: 0;
    }
  }
  @keyframes pk-bus {
    to {
      transform: translate3d(46px, 0, 0);
    }
  }
  @keyframes pk-blink {
    0%,
    48% {
      opacity: 0.95;
    }
    49%,
    100% {
      opacity: 0.3;
    }
  }
  @keyframes pk-swing {
    0% {
      transform: translate3d(-15%, 2.4%, 0);
    }
    50% {
      transform: translate3d(0, 0, 0);
    }
    100% {
      transform: translate3d(15%, 2.4%, 0);
    }
  }
  @keyframes pk-bars {
    to {
      transform: translate3d(-18.9px, 4.7px, 0);
    }
  }
  /* With sweeping highlights turned off, the cards whose motion IS that
     highlight say so. It damps rather than deletes, because that is what the
     packs themselves do — aurora keeps its curtain, prism keeps its bar, both
     without the hot core. */
  .pk.dimshine .pk-glow {
    opacity: 0.28;
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
    .swatch {
      width: 32px;
      height: 32px;
      min-height: 32px;
    }
    .pack-row {
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
     the content, because on this screen the content IS colour: fifty-one
     palette previews and three theme cards flatten into identical empty
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
