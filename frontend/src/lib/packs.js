// packs.js — the one description of the theme packs.
//
// There used to be fifty, filed in five galleries, and the list of which of
// them were LIGHT grounds was written out by hand in seven places: the light
// palette's scope in app.css, the daylight token block, the pressed-button
// rule, Message and MemberPanel, the daylight block in themepacks.css and the
// contrast gate. Miss one and a dark pack under the Light theme drew white
// fields with near-white ink in them, which is a bug this app has shipped.
//
// Now there are nine, and the stylesheets name no pack in order to ask
// "is this a light ground?". applyAppearance asks groundOf() and stamps the
// answer as [data-ground] on <html>; CSS keys off that one attribute. Adding a
// light pack is setting `day: true` here, and packs.test.mjs fails if a pack's
// own --bg-1 is bright and this table does not say so (or the reverse).
//
// Plain JS, no Svelte: the node tests import it.

// Preview data for the Appearance cards (`bg`/`hi`/`ac` are the pack's own
// --bg-1/--bg-3/--accent; `font`, `r` and `av` its face, corner radius and
// avatar shape; `rule`/`card` how it draws message rows).
const SERIF = '"Source Serif 4", Georgia, serif';
const INTER = '"Inter", system-ui, sans-serif';
const GROTESK = '"Space Grotesk", system-ui, sans-serif';
const MONO = '"JetBrains Mono", ui-monospace, Menlo, monospace';

export const PACKS = [
  { id: "midnight", label: "Midnight", bg: "#111527", hi: "#222946", ac: "#6f7cff", font: INTER, r: 8, av: "50%", note: "Deep blue, Inter" },
  { id: "abyss", label: "Abyss", bg: "#060608", hi: "#14151a", ac: "#22d3ee", font: INTER, r: 1, av: "3px", rule: true, note: "True black" },
  { id: "nord", label: "Nord", bg: "#373e4b", hi: "#434c5e", ac: "#88c0d0", font: INTER, r: 3, av: "7px", rule: true, note: "Cool grey-blue" },
  { id: "dracula", label: "Dracula", bg: "#282a36", hi: "#424456", ac: "#bd93f9", font: MONO, r: 3, av: "5px", note: "Purple, monospaced" },
  { id: "gruvbox", label: "Gruvbox", bg: "#282828", hi: "#3c3836", ac: "#fabd2f", font: MONO, r: 0, av: "2px", note: "Warm, terminal" },
  { id: "rose", label: "Rosé", bg: "#1f1d2e", hi: "#2f2b45", ac: "#ebbcba", font: SERIF, r: 10, av: "50%", card: true, note: "Rose, serif cards" },
  { id: "paper", label: "Paper", bg: "#f0eadc", hi: "#e9e2d1", ac: "#9c4a2c", font: SERIF, r: 5, av: "5px", rule: true, day: true, note: "Warm paper, serif" },
  { id: "solarized", label: "Solarized", bg: "#f3ebd3", hi: "#eee8d5", ac: "#268bd2", font: MONO, r: 3, av: "3px", day: true, note: "Light, monospaced" },
  {
    id: "aurora",
    label: "Aurora",
    bg: "#061b21",
    hi: "#112e34",
    ac: "#39d9b0",
    font: GROTESK,
    r: 10,
    av: "40%",
    live: true,
    motion: "sweep",
    grad: "linear-gradient(103deg,transparent 34%,#29d69e 42%,#7af6d6 48%,#46e2ff 54%,transparent 64%)",
    base: "linear-gradient(180deg,#16505a,#04161d)",
    shine: true,
    note: "A moving curtain",
  },
];

export const PACK_IDS = new Set(PACKS.map((p) => p.id));
// Light-ground packs. The ONLY list of them.
export const LIGHT_PACKS = new Set(PACKS.filter((p) => p.day).map((p) => p.id));
// Packs with a live backdrop behind translucent surfaces ([data-anim-bg]).
export const ANIMATED_PACKS = new Set(PACKS.filter((p) => p.live).map((p) => p.id));

export const packById = (id) => PACKS.find((p) => p.id === id) || null;

// The pack to DRAW for a saved id: a pack this build has, or "" (the default
// palette) for one it does not know, which stays saved untouched.
export const packIdOrDefault = (id) => (typeof id === "string" && PACK_IDS.has(id) ? id : "");

// groundOf answers "is the app painted on a light or a dark ground", for the
// base theme as resolved ("light" | "dark", System already decided) and the
// pack actually stamped on the document ("" when none, or when its stylesheet
// has not arrived yet). A pack brings its own ground whatever the base theme
// says; with no pack, the theme decides.
export function groundOf(theme, pack) {
  if (pack) return LIGHT_PACKS.has(pack) ? "light" : "dark";
  return theme === "light" ? "light" : "dark";
}

// ---- migration ------------------------------------------------------------
//
// The forty-one packs that were cut, each sent to the kept pack closest to it
// in ground and hue. `theme` is set too where the old pack was a light ground
// that now maps to the default palette: under the Dark theme the default is
// dark, and someone who chose a light pack must not wake up on a black app.
export const PACK_MIGRATION = {
  nebula: { pack: "dracula" },
  grape: { pack: "dracula" },
  synthwave: { pack: "dracula" },
  skyline: { pack: "dracula" },
  sakura: { pack: "rose" },
  dusk: { pack: "rose" },
  blossom: { pack: "rose" },
  hearth: { pack: "gruvbox" },
  harvest: { pack: "gruvbox" },
  molten: { pack: "gruvbox" },
  vinyl: { pack: "gruvbox" },
  dunes: { pack: "gruvbox" },
  sonar: { pack: "gruvbox" },
  bloom: { pack: "gruvbox" },
  oceanic: { pack: "nord" },
  tundra: { pack: "nord" },
  harbor: { pack: "nord" },
  frost: { pack: "nord" },
  glacier: { pack: "nord" },
  storm: { pack: "nord" },
  monsoon: { pack: "nord" },
  meridian: { pack: "nord" },
  silicon: { pack: "nord" },
  observatory: { pack: "midnight" },
  cosmos: { pack: "midnight" },
  eclipse: { pack: "midnight" },
  lantern: { pack: "midnight" },
  orbit: { pack: "midnight" },
  radiant: { pack: "midnight" },
  phosphor: { pack: "abyss" },
  datastream: { pack: "abyss" },
  uptime: { pack: "abyss" },
  fathom: { pack: "aurora" },
  canopy: { pack: "aurora" },
  zellige: { pack: "aurora" },
  forest: { pack: "" },
  prism: { pack: "" },
  meadow: { pack: "", theme: "light" },
  porcelain: { pack: "", theme: "light" },
  daybreak: { pack: "", theme: "light" },
  atrium: { pack: "paper" },
};

// The prefs version this build writes. 2: the pack cut, Seasonal folded into
// Effects, Compact folded into Density, Sweeping highlights off by default.
export const PREFS_VERSION = 2;

// migratePrefs takes whatever concord.prefs held and returns { prefs, changed }.
// Pure, idempotent, and safe on any shape (a newer build's prefs, a hand edit).
//
// The pack, layout and seasonal mappings run on every load, not once: a
// device that goes back to an older build can write an old value again, and
// coming forward must translate it again. The Sweeping-highlights flip runs
// once (PREFS_VERSION): the old default was ON, and every install that ever
// saved a pref saved that default with it, so a stored `true` was never a
// choice anybody made — the only choice there was to make was off.
export function migratePrefs(raw) {
  const p = raw && typeof raw === "object" && !Array.isArray(raw) ? { ...raw } : {};
  const before = JSON.stringify(p);

  // Only the ids this build REMOVED are rewritten. Any other unknown id is
  // left where it is: it may be a pack a newer build added, and a device that
  // runs this build between two newer ones must not lose that choice. Until
  // then it draws as the default palette (packIdOrDefault, applyAppearance).
  if (typeof p.themePack === "string" && Object.hasOwn(PACK_MIGRATION, p.themePack)) {
    const to = PACK_MIGRATION[p.themePack];
    p.themePack = to.pack;
    if (to.theme) p.theme = to.theme;
  } else if (p.themePack != null && typeof p.themePack !== "string") {
    p.themePack = "";
  }

  // Layout › Compact was the Sidebar layout plus compact density plus the side
  // panel closed. Density and the panel already hold what it set.
  if (p.layout === "compact") p.layout = "sidebar";

  // Seasonal touches became one of the Effects. It only ever drew on the Rail
  // layout's guild column, so only someone on Rail ever saw it: they get the
  // Seasonal effect, unless they already picked an effect, which is never
  // replaced. Everyone else never saw it, and gets nothing new.
  if (p.seasonal === true && p.navModel === "rail" && !p.themeFx) p.themeFx = "seasonal";
  delete p.seasonal;

  // Keys nothing reads (see the inventory, settings-keep-remove.md).
  delete p.colChannels;
  delete p.colMembers;
  delete p.guildAccents;

  if (!(Number(p.prefsV) >= 2)) {
    p.themeShine = false;
  }
  p.prefsV = Math.max(PREFS_VERSION, Number(p.prefsV) || 0);

  return { prefs: p, changed: JSON.stringify(p) !== before };
}
