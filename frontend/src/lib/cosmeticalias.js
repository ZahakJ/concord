// cosmeticalias.js — where the removed avatar rings and decorations went.
//
// The ring library had 46 gradient rings and the decoration library 61 drawn
// pieces. They are 12 and 20 now. These ids are not device settings: they ride
// on profiles, so a peer on an older build keeps SENDING a removed one, and a
// profile saved before the cut keeps carrying it. So nothing here rewrites
// anybody's profile. A removed id is drawn as the kept item nearest to it, on
// this build, at the moment it is drawn; the profile keeps the id it had, so an
// older build still draws the original, and saving from this build's editor
// writes the kept id, which every build since v0.60.0 has.
//
// Kept small and table-free on purpose: Avatar.svelte needs `wornRing` before
// the 125 KB decoration table has loaded, and that answer depends on these.
//
// A drawn RING only ever maps to a drawn ring, and a piece that is not a ring
// only to one that is not: a ring changes the avatar's silhouette to a circle,
// and a mapping that flipped it would visibly reshape someone's avatar.
// cosmeticalias.test.mjs holds every entry to that and to "maps to a kept id".

// Gradient rings (lib/rings.js), worn in `frame`.
export const RING_ALIAS = {
  "theme-solid": "theme",
  satellite: "orbit",
  binary: "orbit",
  "theme-orbit": "orbit",
  "theme-duo": "orbit",
  blizzard: "snow",
  ash: "snow",
  thunder: "rain",
  bubbles: "rain",
  "matrix-ring": "rain",
  autumn: "sakura",
  meteor: "galaxy",
  starfall: "galaxy",
  warp: "galaxy",
  "eclipse-ring": "galaxy",
  stardust: "sparkles",
  fireflies: "sparkles",
  hearts: "sparkles",
  notes: "sparkles",
  confetti: "sparkles",
  "theme-spark": "sparkles",
  solar: "embers",
  inferno: "embers",
  candle: "embers",
  moon: "pulse",
  ripple: "pulse",
  sonar: "pulse",
  scanner: "pulse",
  glitch: "holo-ring",
  toxic: "spin",
  "candy-ring": "spin",
  barber: "spin",
  checker: "spin",
  sparkline: "spin",
};

// Drawn decorations (lib/decorations.js), worn in `style.dec` — and, for the
// drawn rings that predate the merge, in `frame`.
export const DECOR_ALIAS = {
  // Arch
  "cat-circlet": "flower-circlet",
  "antler-circlet": "flower-circlet",
  // Creature
  "bunny-ears": "cat-ears",
  shark: "fox-ears",
  "ram-horns": "devil-horns",
  // Regalia
  "laurel-wreath": "tiara",
  diadem: "tiara",
  "royal-collar": "royal-crown",
  "velvet-mantle": "royal-crown",
  // Ethereal
  "orbit-ring": "halo-gold",
  "aura-crown": "radiant-nimbus",
  "spectral-shroud": "radiant-nimbus",
  // Elemental
  "ember-rise": "flame-crown",
  "storm-bolts": "frost-crown",
  "tide-crest": "frost-crown",
  "gale-swirl": "frost-crown",
  "stone-mantle": "royal-crown",
  // Cosmic
  comet: "constellation",
  orbits: "ringed-world",
  "solar-corona": "radiant-nimbus",
  // Tech
  antennae: "headphones",
  "goggles-up": "headphones",
  "circuit-traces": "circuit-crown",
  "holo-rim": "circuit-crown",
  signal: "circuit-crown",
  // Drawn rings → drawn rings
  "orbit-sigils": "runic-ring",
  "eldritch-iris": "runic-ring",
  "spellbound-chain": "runic-ring",
  "warding-hex": "runic-ring",
  "blossom-crown": "laurel-ring",
  "riveted-band": "gold-filigree",
  "gear-ring": "gold-filigree",
  chainmail: "gold-filigree",
  "hammered-bronze": "gold-filigree",
  "sunburst-crown": "gold-filigree",
  "gem-circlet": "frost-shards",
  "prism-halo": "frost-shards",
  "diamond-lattice": "frost-shards",
  "neon-holo-segments": "neon-circuit-ring",
  "neon-target-lock": "neon-circuit-ring",
  "neon-hex-lattice": "neon-circuit-ring",
};

// The id to draw for a stored one (own properties only, see lib/lookup.js).
export const ringId = (id) => (typeof id === "string" && Object.hasOwn(RING_ALIAS, id) ? RING_ALIAS[id] : id);
export const decorId = (id) => (typeof id === "string" && Object.hasOwn(DECOR_ALIAS, id) ? DECOR_ALIAS[id] : id);
