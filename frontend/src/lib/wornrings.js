// The twenty-one decorations that are DRAWN RINGS.
//
// This is the one fact about the decoration table that every avatar in the app
// needs before it can draw itself: a ring means a circular silhouette, and a
// silhouette that changes a frame later is a visible pop on every face on the
// screen. So it cannot wait for the table to load the way the artwork can.
//
// Duplicating it is the cost of not shipping a 125KB table to every session
// that never renders a decoration, and the duplication is GATED: decorations.
// test.mjs asserts this list is exactly the set of entries carrying `ring`, so
// adding a ring to the library without adding it here fails the build.
const WORN_RINGS = new Set([
  "runic-ring",
  "orbit-sigils",
  "eldritch-iris",
  "spellbound-chain",
  "warding-hex",
  "laurel-ring",
  "blossom-crown",
  "riveted-band",
  "gold-filigree",
  "gear-ring",
  "chainmail",
  "hammered-bronze",
  "gem-circlet",
  "frost-shards",
  "prism-halo",
  "diamond-lattice",
  "neon-circuit-ring",
  "neon-holo-segments",
  "neon-target-lock",
  "neon-hex-lattice",
  "sunburst-crown",
]);

// wornRing answers the one question the rest of the app asks about the `ring`
// flag: is this id one of the decorations that reads as a ring around the face?
export function wornRing(id) {
  return WORN_RINGS.has(id);
}

export { WORN_RINGS };
