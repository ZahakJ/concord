// The avatar-cosmetics cut, checked (`npm test` runs it).
//
// Rings went from 46 to 12 and drawn decorations from 61 to 20. These ids ride
// on PROFILES: an older build keeps sending a removed one, and a profile saved
// before the cut keeps carrying it. So the rules are:
//
//   - every id v0.60.2 had is either kept or drawn as a kept one (one
//     assertion per removed id, each written out here, not derived);
//   - a drawn ring stays a ring and a piece that is not a ring stays not one,
//     or the avatar's silhouette changes shape;
//   - everything this build's editor can write is an id v0.60.2 has, so a peer
//     still on v0.60.2 draws a profile edited here exactly as intended.
import { RING_ALIAS, DECOR_ALIAS, ringId, decorId } from "./cosmeticalias.js";
import { RINGS, RING_BY_ID, RING_GROUPS, ringArt, hasRider } from "./rings.js";
import { DECORATIONS, DECORATION_BY_ID, decoration, wornRing as tableRing, decorColors, CW_OWN } from "./decorations.js";
import { wornRing as listRing, WORN_RINGS } from "./wornrings.js";
import { BANNERS, BANNER_GROUPS, PROFILE_BANNERS } from "./banners.js";

let failures = 0;
const fail = (m) => {
  console.error("  FAIL " + m);
  failures++;
};
const eq = (got, want, m) => {
  if (JSON.stringify(got) !== JSON.stringify(want)) fail(`${m}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
};

// v0.60.2, frozen (git show v0.60.2:frontend/src/lib/{rings,decorations}.js;
// unchanged from v0.60.0 through dc4fbd1).
const V0602_RINGS = ["", "spin", "theme", "theme-solid", "orbit", "comet", "satellite", "binary", "theme-orbit", "theme-duo", "snow", "blizzard", "rain", "thunder", "sakura", "autumn", "ash", "meteor", "starfall", "stardust", "warp", "galaxy", "solar", "eclipse-ring", "moon", "embers", "inferno", "candle", "bubbles", "ripple", "sonar", "toxic", "fireflies", "hearts", "notes", "confetti", "sparkles", "theme-spark", "matrix-ring", "scanner", "glitch", "holo-ring", "pulse", "candy-ring", "barber", "checker", "sparkline"];
const V0602_DECOR = ["flower-circlet", "cat-circlet", "antler-circlet", "star-circlet", "cat-ears", "fox-ears", "bunny-ears", "ram-horns", "devil-horns", "shark", "royal-crown", "laurel-wreath", "tiara", "diadem", "royal-collar", "velvet-mantle", "halo-gold", "orbit-ring", "aura-crown", "spectral-shroud", "radiant-nimbus", "flame-crown", "frost-crown", "storm-bolts", "tide-crest", "stone-mantle", "ember-rise", "gale-swirl", "ringed-world", "comet", "constellation", "orbits", "solar-corona", "headphones", "antennae", "circuit-traces", "holo-rim", "goggles-up", "signal", "circuit-crown", "runic-ring", "orbit-sigils", "eldritch-iris", "spellbound-chain", "warding-hex", "laurel-ring", "blossom-crown", "riveted-band", "gold-filigree", "gear-ring", "chainmail", "hammered-bronze", "gem-circlet", "frost-shards", "prism-halo", "diamond-lattice", "neon-circuit-ring", "neon-holo-segments", "neon-target-lock", "neon-hex-lattice", "sunburst-crown"];
const V0602_DRAWN_RINGS = new Set(["runic-ring", "orbit-sigils", "eldritch-iris", "spellbound-chain", "warding-hex", "laurel-ring", "blossom-crown", "riveted-band", "gold-filigree", "gear-ring", "chainmail", "hammered-bronze", "gem-circlet", "frost-shards", "prism-halo", "diamond-lattice", "neon-circuit-ring", "neon-holo-segments", "neon-target-lock", "neon-hex-lattice", "sunburst-crown"]);

// ---- the kept sets ----------------------------------------------------------
const KEPT_RINGS = ["", "spin", "theme", "orbit", "comet", "snow", "rain", "sakura", "galaxy", "embers", "sparkles", "holo-ring", "pulse"];
const KEPT_DECOR = ["flower-circlet", "star-circlet", "cat-ears", "fox-ears", "devil-horns", "royal-crown", "tiara", "halo-gold", "radiant-nimbus", "flame-crown", "frost-crown", "ringed-world", "constellation", "headphones", "circuit-crown", "runic-ring", "laurel-ring", "gold-filigree", "frost-shards", "neon-circuit-ring"];
eq(RINGS.map((r) => r.id), KEPT_RINGS, "the rings offered");
eq(DECORATIONS.map((d) => d.id), KEPT_DECOR, "the decorations offered");
eq(RING_GROUPS.flatMap((g) => g.ids).sort(), KEPT_RINGS.filter(Boolean).sort(), "every kept ring on a shelf, once");
eq([...WORN_RINGS].sort(), DECORATIONS.filter((d) => d.ring).map((d) => d.id).sort(), "wornrings.js lists the kept drawn rings");
for (const id of KEPT_RINGS) if (!V0602_RINGS.includes(id)) fail(`kept ring "${id}" is not in v0.60.2: a v0.60.2 peer could not draw it`);
for (const id of KEPT_DECOR) if (!V0602_DECOR.includes(id)) fail(`kept decoration "${id}" is not in v0.60.2: a v0.60.2 peer could not draw it`);

// ---- one written expectation per removed ring --------------------------------
const RING_EXPECT = {
  "theme-solid": "theme", satellite: "orbit", binary: "orbit", "theme-orbit": "orbit", "theme-duo": "orbit",
  blizzard: "snow", ash: "snow", thunder: "rain", bubbles: "rain", "matrix-ring": "rain", autumn: "sakura",
  meteor: "galaxy", starfall: "galaxy", warp: "galaxy", "eclipse-ring": "galaxy",
  stardust: "sparkles", fireflies: "sparkles", hearts: "sparkles", notes: "sparkles", confetti: "sparkles", "theme-spark": "sparkles",
  solar: "embers", inferno: "embers", candle: "embers",
  moon: "pulse", ripple: "pulse", sonar: "pulse", scanner: "pulse",
  glitch: "holo-ring", toxic: "spin", "candy-ring": "spin", barber: "spin", checker: "spin", sparkline: "spin",
};
{
  const removed = V0602_RINGS.filter((id) => !KEPT_RINGS.includes(id));
  eq(removed.length, 34, "rings removed since v0.60.2");
  eq(Object.keys(RING_EXPECT).sort(), [...removed].sort(), "a written expectation for every removed ring");
  eq(Object.keys(RING_ALIAS).sort(), [...removed].sort(), "an alias for every removed ring");
  for (const id of removed) {
    const to = RING_EXPECT[id];
    eq(ringId(id), to, `ring ${id}`);
    if (!RING_BY_ID[to]) fail(`ring ${id} maps to "${to}", which this build does not have`);
    // Drawn exactly as the kept ring is: same art, same effect, same rider slot.
    const a = ringArt(id, "#112233", "#445566", "🐄", "gold");
    const b = ringArt(to, "#112233", "#445566", "🐄", "gold");
    if (!a) fail(`ring ${id} draws nothing`);
    else eq({ art: a.art, fx: a.fx, orbit: a.orbit, halo: a.halo }, { art: b.art, fx: b.fx, orbit: b.orbit, halo: b.halo }, `ring ${id} is drawn as ${to}`);
  }
  // A removed orbit ring keeps its rider: the rider rides the kept orbit.
  for (const id of ["satellite", "binary", "theme-orbit", "theme-duo"]) {
    if (!hasRider(id)) fail(`${id}: its rider has nothing to ride`);
    eq(ringArt(id, "", "", "🚀").orbit?.sat, "🚀", `${id} keeps its rider`);
  }
}

// ---- one written expectation per removed decoration ---------------------------
const DECOR_EXPECT = {
  "cat-circlet": "flower-circlet", "antler-circlet": "flower-circlet",
  "bunny-ears": "cat-ears", shark: "fox-ears", "ram-horns": "devil-horns",
  "laurel-wreath": "tiara", diadem: "tiara", "royal-collar": "royal-crown", "velvet-mantle": "royal-crown",
  "orbit-ring": "halo-gold", "aura-crown": "radiant-nimbus", "spectral-shroud": "radiant-nimbus",
  "ember-rise": "flame-crown", "storm-bolts": "frost-crown", "tide-crest": "frost-crown", "gale-swirl": "frost-crown", "stone-mantle": "royal-crown",
  comet: "constellation", orbits: "ringed-world", "solar-corona": "radiant-nimbus",
  antennae: "headphones", "goggles-up": "headphones", "circuit-traces": "circuit-crown", "holo-rim": "circuit-crown", signal: "circuit-crown",
  "orbit-sigils": "runic-ring", "eldritch-iris": "runic-ring", "spellbound-chain": "runic-ring", "warding-hex": "runic-ring",
  "blossom-crown": "laurel-ring",
  "riveted-band": "gold-filigree", "gear-ring": "gold-filigree", chainmail: "gold-filigree", "hammered-bronze": "gold-filigree", "sunburst-crown": "gold-filigree",
  "gem-circlet": "frost-shards", "prism-halo": "frost-shards", "diamond-lattice": "frost-shards",
  "neon-holo-segments": "neon-circuit-ring", "neon-target-lock": "neon-circuit-ring", "neon-hex-lattice": "neon-circuit-ring",
};
{
  const removed = V0602_DECOR.filter((id) => !KEPT_DECOR.includes(id));
  eq(removed.length, 41, "decorations removed since v0.60.2");
  eq(Object.keys(DECOR_EXPECT).sort(), [...removed].sort(), "a written expectation for every removed decoration");
  eq(Object.keys(DECOR_ALIAS).sort(), [...removed].sort(), "an alias for every removed decoration");
  for (const id of removed) {
    const to = DECOR_EXPECT[id];
    eq(decorId(id), to, `decoration ${id}`);
    if (!DECORATION_BY_ID[to]) fail(`decoration ${id} maps to "${to}", which this build does not have`);
    if (decoration(id) !== DECORATION_BY_ID[to]) fail(`decoration ${id} is not drawn as ${to}`);
    // The silhouette: a drawn ring stays a ring, anything else stays not one,
    // asked both of the full table and of the small list Avatar reads first.
    const was = V0602_DRAWN_RINGS.has(id);
    eq(tableRing(id), was, `decoration ${id} keeps its silhouette (table)`);
    eq(listRing(id), was, `decoration ${id} keeps its silhouette (wornrings.js)`);
    // "As designed" colours follow the piece it is drawn as.
    eq(decorColors(id, CW_OWN, "#1", "#2"), decorColors(to, CW_OWN, "#1", "#2"), `decoration ${id} own colours`);
  }
}

// ---- the names both libraries share, and ids off the wire ----------------------
// "comet" is a gradient ring (kept, in `frame`) AND a decoration (removed, in
// `dec`). In `frame` it must stay the ring; in `dec` it is the constellation.
eq(ringId("comet"), "comet", "frame comet is still the gradient comet");
eq(ringArt("comet")?.orbit?.trail, true, "the gradient comet still has its trail");
eq(listRing("comet"), false, "frame comet is not a drawn ring");
eq(decoration("comet")?.id, "constellation", "dec comet is drawn as the constellation");
for (const id of ["constructor", "toString", "__proto__", "hasOwnProperty"]) {
  eq(ringId(id), id, `ringId("${id}") is not an Object member`);
  eq(decorId(id), id, `decorId("${id}") is not an Object member`);
  eq(decoration(id), null, `decoration("${id}") fails closed`);
  eq(ringArt(id), null, `ringArt("${id}") fails closed`);
}
// The legacy ids that predate v0.60 keep resolving as before.
eq(ringArt("orbit-cow")?.orbit?.sat, "🐄", "orbit-cow still rides a cow");
eq(!!ringArt("gold")?.art?.includes("#f5e08a"), true, "the old gold preset is still the gold palette");
eq(ringArt("not-a-ring"), null, "an unknown ring draws nothing");

// ---- banners: the profile picker's twelve -------------------------------------
eq(PROFILE_BANNERS.length, 12, "twelve banners offered on a profile");
for (const id of PROFILE_BANNERS) if (!BANNERS.some((b) => b.id === id)) fail(`profile banner "${id}" is not a preset`);
for (const g of BANNER_GROUPS) {
  eq(BANNERS.filter((b) => b.group === g && PROFILE_BANNERS.includes(b.id)).length, 2, `two profile banners from ${g}`);
}
eq(BANNERS.length, 43, "the preset library itself is untouched (stories and forums use it)");

if (failures) {
  console.error(`cosmeticalias: ${failures} failure(s)`);
  process.exit(1);
}
console.log(`cosmeticalias: ${Object.keys(RING_ALIAS).length} rings and ${Object.keys(DECOR_ALIAS).length} decorations checked`);
