// guildbanners.js — the GUILD banner template library.
//
// Same machinery as banners.js (a template is DATA: a painted `base` plus an
// `fx` layer from lib/fx.js, travelling as "preset:<id>" — a dozen bytes, not a
// 256KB image, and it moves because it's drawn rather than decoded). A separate
// catalogue because a guild banner is a different design problem from a profile
// banner:
//
//   • It is CHROME, not art. A name and a 26px icon are printed on top of it in
//     the channel-list header, so a template that wins is one you can still
//     read a name over. Contrast is a requirement here, not a preference —
//     Banner.svelte lays SCRIM_ALPHA over the art and guildbanners.test.mjs
//     proves every template clears 4.5:1 under it.
//   • It is a guild's IDENTITY, so the shelves are shaped like the guilds
//     people actually run — a clan, a dev team, a music room, a study corner —
//     not like a colour wheel.
//   • It is short and wide (a ~240×56 header), so the motifs are big and
//     low-frequency: bands, grids, spotlights, one moon. Fine detail dies at
//     that size.
//
// Rendered by Banner.svelte, which resolves ids from this catalogue and the
// profile one, so the tile you click in guild settings is exactly what every
// member's sidebar shows.
import { isPreset, presetId } from "./banners.js";
import { isSafeImageDataURI } from "./images.js";

// How hard the header's legibility scrim presses at the TEXT line, per ink.
// Banner.svelte paints the gradient; the test composites this alpha over every
// colour a template can put behind the name and checks the ratio. The two must
// stay in lockstep — weaken the CSS and the test stops meaning anything.
export const SCRIM_ALPHA = { light: 0.5, dark: 0.5 };

// The ink each template expects the header to print in: "light" (white text,
// dark scrim) unless a template says `ink: "dark"`. One deliberately does — a
// book club or a work guild does not want a neon coliseum — and it flips the
// header to dark text over a WHITE scrim instead.
export const GUILD_BANNERS = [
  // ---------- Arena: competitive rooms. Loud on purpose, dark where the name sits. ----------
  {
    // Two rival spotlights on the floor of an esports stage, grid rushing away.
    id: "neon-coliseum",
    name: "Neon Coliseum",
    group: "Arena",
    base:
      "radial-gradient(55% 130% at 16% 120%, rgba(255,45,150,.55) 0%, transparent 62%)," +
      "radial-gradient(55% 130% at 84% 120%, rgba(45,205,255,.5) 0%, transparent 62%)," +
      "linear-gradient(180deg, #08040f 0%, #150a28 55%, #1d0c33 100%)",
    fx: { kind: "grid", colors: ["rgba(255,60,200,.45)"], dur: [3.2, 3.2], opacity: [0.5, 0.5] },
  },
  {
    // A chequered flag strip along the TOP only — the bottom stays asphalt, with a
    // lane dash across it, so the guild name never lands on a white square. The
    // 8px tile (4px squares) is deliberate: 12px read as a chessboard, not a flag.
    id: "victory-lap",
    name: "Victory Lap",
    group: "Arena",
    base:
      "repeating-conic-gradient(from 90deg at 50% 50%, #c4c4c8 0 25%, #14161a 0 50%) left top/8px 8px repeat-x," +
      "repeating-linear-gradient(90deg, rgba(240,240,245,.5) 0 14px, transparent 14px 36px) center bottom 9px/100% 2px no-repeat," +
      "radial-gradient(130% 130% at 50% 58%, #35393f 0%, #1b1d22 55%, #0b0c0f 100%)",
    fx: { kind: "streak", n: 7, colors: ["#ffffff", "#ffe3b0"], size: [1.2, 2], dur: [0.5, 1.2], glow: 6 },
  },
  {
    // Hazard chevrons and a radar sweep: ops rooms, milsim squads, raid nights.
    id: "war-room",
    name: "War Room",
    group: "Arena",
    base:
      "repeating-linear-gradient(135deg, rgba(255,176,32,.1) 0 9px, transparent 9px 26px)," +
      "radial-gradient(125% 125% at 50% 38%, #171b21 0%, #10141a 60%, #080a0d 100%)",
    fx: { kind: "scan", colors: ["rgba(255,186,60,.34)"], dur: [4.6, 4.6], opacity: [0.75, 0.75] },
  },
  {
    id: "hot-drop",
    name: "Hot Drop",
    group: "Arena",
    base: "radial-gradient(95% 135% at 50% 120%, #ff8a1f 0%, #c1370a 30%, #4a1206 62%, #15090a 100%)",
    fx: { kind: "rise", n: 12, colors: ["#ffb84d", "#ff7a1a", "#ff5722"], size: [1.4, 3.2], dur: [1.6, 4], drift: 34, glow: 8 },
  },

  // ---------- Guild hall: heraldry. Clans, houses, anything with a crest. ----------
  {
    // A house banner hanging in a stone hall: gold trim top and bottom, a woven
    // warp in the cloth, light sliding across it.
    id: "crimson-oath",
    name: "Crimson Oath",
    group: "Guild hall",
    base:
      "linear-gradient(#c9a227, #c9a227) center top/100% 2px no-repeat," +
      "linear-gradient(#c9a227, #c9a227) center bottom/100% 2px no-repeat," +
      "repeating-linear-gradient(90deg, rgba(0,0,0,.16) 0 3px, transparent 3px 9px)," +
      "radial-gradient(130% 150% at 50% 0%, #8c1220 0%, #5c0a16 55%, #2a050c 100%)",
    fx: { kind: "sheen", colors: ["rgba(255,231,170,.35)"], dur: [6.5, 6.5], opacity: [0.8, 0.8] },
  },
  {
    // The medallion glow sits at 13% — right behind the guild icon in the
    // header, so the icon reads as the crest on the seal.
    id: "gilded-crest",
    name: "Gilded Crest",
    group: "Guild hall",
    base:
      "radial-gradient(closest-side circle at 13% 50%, rgba(255,205,110,.22) 0 62%, transparent 66%)," +
      "radial-gradient(75% 130% at 50% 45%, #4a3a12 0%, #17130a 58%, #08070a 100%)",
    fx: { kind: "rays", colors: ["rgba(255,205,110,.3)"], dur: [34, 34], opacity: [0.35, 0.35] },
  },
  {
    // Basalt with something still burning under it. The glow has to sit at 118%,
    // not 135%: a 56px header crops anything further out and the template
    // collapses into a grey hatch.
    id: "ashen-throne",
    name: "Ashen Throne",
    group: "Guild hall",
    base:
      "radial-gradient(140% 70% at 50% 106%, rgba(240,92,30,.62) 0%, rgba(130,30,10,.42) 42%, transparent 80%)," +
      "repeating-linear-gradient(75deg, rgba(255,255,255,.02) 0 6px, transparent 6px 17px)," +
      "linear-gradient(180deg, #17110f 0%, #241b18 58%, #0c0908 100%)",
    fx: { kind: "fall", n: 13, colors: ["#9aa0a8", "#d0d4d9"], size: [1.2, 2.4], dur: [5, 11], drift: 30, glow: 2, opacity: [0.25, 0.6] },
  },

  // ---------- Terminal: dev teams, homelabs, anyone whose guild has a repo. ----------
  {
    // Log rules behind falling code. Kept dim: a dev room reads text all day.
    id: "commit-log",
    name: "Commit Log",
    group: "Terminal",
    base:
      "repeating-linear-gradient(0deg, rgba(0,255,110,.055) 0 1px, transparent 1px 8px)," +
      "linear-gradient(180deg, #04120b 0%, #071a10 60%, #030b07 100%)",
    fx: { kind: "matrix", n: 18, colors: ["#00ff6e", "#8fffc4"], size: [2, 5], dur: [1.4, 3.6], drift: 0, glow: 5, opacity: [0.3, 0.8] },
  },
  {
    // Cyanotype drafting paper: 8px minor grid, 40px majors, one slow light pass
    // as if someone tilted the sheet.
    id: "blueprint",
    name: "Blueprint",
    group: "Terminal",
    base:
      "repeating-linear-gradient(90deg, rgba(170,225,255,.22) 0 1px, transparent 1px 40px)," +
      "repeating-linear-gradient(0deg, rgba(170,225,255,.22) 0 1px, transparent 1px 40px)," +
      "repeating-linear-gradient(90deg, rgba(170,225,255,.1) 0 1px, transparent 1px 8px)," +
      "repeating-linear-gradient(0deg, rgba(170,225,255,.1) 0 1px, transparent 1px 8px)," +
      "linear-gradient(135deg, #0a2a4a 0%, #0d3a63 55%, #072038 100%)",
    fx: { kind: "sheen", colors: ["rgba(210,240,255,.22)"], dur: [7.5, 7.5], opacity: [0.85, 0.85] },
  },
  {
    // The only theme that matters: a dot grid, one accent hairline along the
    // bottom, and a sweep so slow you notice it twice a day.
    id: "dark-mode",
    name: "Dark Mode",
    group: "Terminal",
    base:
      "radial-gradient(rgba(255,255,255,.07) 1px, transparent 1.4px) 0 0/13px 13px," +
      "linear-gradient(90deg, #22d3ee, #a855f7) center bottom/100% 2px no-repeat," +
      "linear-gradient(180deg, #0c0d10 0%, #14161b 100%)",
    fx: { kind: "scan", colors: ["rgba(140,220,255,.14)"], dur: [9, 9], opacity: [0.6, 0.6] },
  },

  // ---------- Studio: music rooms, listening parties, podcasts. ----------
  {
    id: "after-hours",
    name: "After Hours",
    group: "Studio",
    base: "radial-gradient(85% 130% at 50% 115%, #3b1160 0%, #1a0730 55%, #0a0416 100%)",
    // Bars fade to transparent at their base, so the floor the name sits on
    // stays dark no matter how hard the equalizer dances.
    fx: { kind: "bars", n: 20, colors: ["#a855f7", "#22d3ee", "#f472b6"], size: [5, 9], dur: [0.35, 1], opacity: [0.4, 0.75] },
  },
  {
    // A record under a warm lamp: grooves are one repeating-radial, the label is
    // a closest-side circle. Parked at 78% so the name gets the left half.
    id: "vinyl-night",
    name: "Vinyl Night",
    group: "Studio",
    base:
      "repeating-radial-gradient(circle at 78% 50%, rgba(0,0,0,.45) 0 2px, rgba(255,240,214,.05) 2px 4px)," +
      "radial-gradient(closest-side circle at 78% 50%, #e0a33c 0 7%, transparent 8%)," +
      "linear-gradient(120deg, #241609 0%, #33200f 55%, #150d07 100%)",
    fx: { kind: "sheen", colors: ["rgba(255,224,170,.28)"], dur: [8, 8], opacity: [0.8, 0.8] },
  },
  {
    id: "open-mic",
    name: "Open Mic",
    group: "Studio",
    base:
      "radial-gradient(46% 140% at 27% -12%, rgba(255,236,180,.44) 0%, rgba(255,214,140,.12) 45%, transparent 68%)," +
      "linear-gradient(180deg, #1a0f22 0%, #2a1533 55%, #0d0714 100%)",
    fx: { kind: "rise", n: 11, glyphs: ["♪", "♫", "♬"], colors: ["#ffd9a8", "#c4a8ff", "#8fd0ff"], size: [2, 4], dur: [3, 7], drift: 30, glow: 5 },
  },

  // ---------- Hearth: cosy. Study rooms, friend groups, late-night chat. ----------
  {
    // One desk lamp in the corner and dust hanging in it. The motes are slow and
    // dim on purpose — this is the shelf people leave on all day.
    id: "study-lamp",
    name: "Study Lamp",
    group: "Hearth",
    base:
      "radial-gradient(44% 155% at 18% -20%, rgba(255,206,120,.5) 0%, rgba(255,180,90,.14) 46%, transparent 74%)," +
      "linear-gradient(160deg, #1c1410 0%, #2b1f16 55%, #14100c 100%)",
    fx: { kind: "twinkle", n: 12, colors: ["#ffe0a8", "#fff3d6"], size: [1, 2.2], dur: [4, 9], drift: 16, glow: 4, opacity: [0.15, 0.5] },
  },
  {
    // Rain on glass, with the window's mullion at 66% — off to the right, clear
    // of the name. It gets a lit edge on its far side, without which a dark bar
    // in a dark banner reads as a rendering seam rather than a window frame.
    id: "rainy-window",
    name: "Rainy Window",
    group: "Hearth",
    base:
      "linear-gradient(90deg, transparent 64.8%, rgba(10,16,22,.88) 64.8% 66.4%, rgba(226,240,250,.16) 66.4% 67.2%, transparent 67.2%)," +
      "radial-gradient(120% 130% at 40% 30%, rgba(190,225,240,.1) 0%, transparent 70%)," +
      "linear-gradient(180deg, #141f2c 0%, #21313d 55%, #2c414d 100%)",
    fx: { kind: "rain", n: 14, colors: ["#a8d8f0", "#d0e8f7"], size: [3, 7], dur: [0.7, 1.4], drift: 8, glow: 2, opacity: [0.3, 0.75] },
  },
  {
    id: "mossbank",
    name: "Mossbank",
    group: "Hearth",
    base:
      "repeating-linear-gradient(115deg, rgba(180,255,180,.04) 0 4px, transparent 4px 13px)," +
      "radial-gradient(75% 130% at 24% 115%, #1f5c36 0%, #113523 48%, #06140f 100%)",
    fx: { kind: "twinkle", n: 13, colors: ["#eaff8f", "#c8ff5a"], size: [1.4, 3], dur: [3, 7], drift: 30, glow: 9, opacity: [0.3, 0.85] },
  },

  // ---------- Arcade: retro. Cabinets, speedruns, emulator guilds. ----------
  {
    id: "insert-coin",
    name: "Insert Coin",
    group: "Arcade",
    base:
      "repeating-linear-gradient(0deg, rgba(0,0,0,.42) 0 2px, transparent 2px 4px)," +
      "radial-gradient(125% 135% at 50% 50%, #3a1d6b 0%, #1a0b33 60%, #090418 100%)",
    fx: { kind: "scan", colors: ["rgba(150,255,230,.32)"], dur: [3, 3], opacity: [0.8, 0.8] },
  },
  {
    // Outrun, but header-shaped: sun parked right of centre, horizon high, and
    // everything below the grid falls back to near-black.
    id: "sunset-highway",
    name: "Sunset Highway",
    group: "Arcade",
    base:
      "radial-gradient(closest-side circle at 72% 58%, #ffd25c 0 13%, #ff5ea8 13% 21%, transparent 22%)," +
      "linear-gradient(180deg, #16053a 0%, #4a1170 38%, #b3247e 56%, #ff6a3d 64%, #1a0730 76%, #0a0320 100%)",
    fx: { kind: "grid", colors: ["rgba(110,231,255,.5)"], dur: [3.4, 3.4], opacity: [0.55, 0.55] },
  },
  // ---------- Signature: quiet. Work guilds, book clubs, anything that has to
  // look like it means it. One dark, one pale — no padding. ----------
  {
    id: "graphite",
    name: "Graphite",
    group: "Signature",
    drift: true,
    base:
      "repeating-linear-gradient(135deg, rgba(255,255,255,.045) 0 2px, transparent 2px 6px)," +
      "linear-gradient(120deg, #13161a 0%, #23272e 50%, #13161a 100%)",
  },
  {
    // Ink on laid paper. The header flips to dark text for this one; the weave is
    // two 4px rules crossing, which is all you can see at header size anyway.
    id: "linen-press",
    name: "Linen Press",
    group: "Signature",
    ink: "dark",
    base:
      "repeating-linear-gradient(90deg, rgba(120,95,60,.09) 0 1px, transparent 1px 4px)," +
      "repeating-linear-gradient(0deg, rgba(120,95,60,.09) 0 1px, transparent 1px 4px)," +
      "linear-gradient(160deg, #f2e7d3 0%, #e6d9c1 55%, #d7c7aa 100%)",
  },
  // ---------- Seasons: swap it in for a month, swap it out. ----------
  {
    id: "harvest-moon",
    name: "Harvest Moon",
    group: "Seasons",
    base:
      "radial-gradient(closest-side circle at 76% 32%, #ffd98a 0 9%, rgba(255,200,120,.22) 10% 17%, transparent 18%)," +
      "linear-gradient(180deg, #101a33 0%, #1e2b4d 45%, #3a2a4a 75%, #130f1e 100%)",
    tumble: true,
    fx: { kind: "fall", n: 11, glyphs: ["🍂", "🍁"], size: [3, 5.5], dur: [4, 9], drift: 46, glow: 0 },
  },
  {
    id: "all-hallows",
    name: "All Hallows",
    group: "Seasons",
    base:
      "radial-gradient(90% 130% at 50% 122%, #ff7a18 0%, #8a2d06 34%, #2a0c14 72%, transparent 100%)," +
      "linear-gradient(180deg, #0b0512 0%, #1a0b20 100%)",
    // Fewer, bigger bats: at 3px a 🦇 is a smudge, and a smudge looks like a bug.
    fx: { kind: "fall", n: 7, glyphs: ["🦇"], size: [3.8, 6.2], dur: [4, 9], drift: 62, glow: 0, opacity: [0.6, 0.95] },
  },
  {
    // Deep pine with warm light coming from below (the tree, the fire, whatever
    // you like) and snow across it.
    id: "yule",
    name: "Yule",
    group: "Seasons",
    base:
      "repeating-linear-gradient(90deg, rgba(255,255,255,.05) 0 1px, transparent 1px 26px)," +
      "radial-gradient(75% 125% at 50% 118%, rgba(255,90,60,.35) 0%, transparent 62%)," +
      "linear-gradient(160deg, #061a12 0%, #0d2b1c 55%, #05130d 100%)",
    fx: { kind: "fall", n: 12, glyphs: ["❄", "❅", "•"], colors: ["#eaf3ff", "#ffe3b0"], size: [1.6, 3.4], dur: [4, 10], drift: 26, glow: 3 },
  },
  {
    // Ice, not snow: a shard hatch at 115° plus one bright fracture across the
    // middle, so it reads as a frozen surface rather than weather.
    id: "frostline",
    name: "Frostline",
    group: "Seasons",
    base:
      "linear-gradient(62deg, transparent 46%, rgba(214,240,255,.16) 47% 48.5%, transparent 49.5%)," +
      "repeating-linear-gradient(115deg, rgba(190,230,255,.1) 0 2px, transparent 2px 18px)," +
      "linear-gradient(180deg, #0a1a2b 0%, #163350 55%, #23506f 100%)",
    fx: { kind: "fall", n: 12, glyphs: ["✦", "❅"], colors: ["#eaf7ff", "#bfe3ff"], size: [1.2, 2.6], dur: [3, 8], drift: 34, glow: 4, opacity: [0.4, 0.9] },
  },
];

export const GUILD_BANNER_BY_ID = Object.fromEntries(GUILD_BANNERS.map((b) => [b.id, b]));

// The picker's shelves, in the order a guild owner is likely to shop them.
export const GUILD_BANNER_GROUPS = [
  "Arena",
  "Guild hall",
  "Terminal",
  "Studio",
  "Hearth",
  "Arcade",
  "Signature",
  "Seasons",
];

export const guildPresetOf = (banner = "") =>
  (isPreset(banner) && GUILD_BANNER_BY_ID[presetId(banner)]) || null;

// What a header should actually PAINT for a stored guild banner value — and the
// ink to print over it. Returns null when there is nothing safe to paint: an
// empty value, an unknown preset id (a template we retired, or a peer running a
// newer build), or a string that isn't a plain image data URI. Callers then draw
// no banner at all, which is the only correct degradation — never a broken box,
// and never a raw string on its way into a CSS declaration.
export function guildBannerArt(banner = "") {
  if (isPreset(banner)) {
    const t = GUILD_BANNER_BY_ID[presetId(banner)];
    return t ? { kind: "preset", ink: t.ink || "light", template: t } : null;
  }
  if (isSafeImageDataURI(banner)) return { kind: "image", ink: "light", template: null };
  return null;
}
