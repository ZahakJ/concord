// The decorations that are DRAWN RINGS.
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
import { decorId } from "./cosmeticalias.js";

const WORN_RINGS = new Set([
  "runic-ring",
  "laurel-ring",
  "gold-filigree",
  "frost-shards",
  "neon-circuit-ring",
]);

// wornRing answers the one question the rest of the app asks about the `ring`
// flag: is this id one of the decorations that reads as a ring around the face?
//
// A drawn ring this build no longer has counts as the kept ring it is drawn as
// (lib/cosmeticalias.js maps rings only to rings), so a profile still wearing
// one keeps its circular silhouette.
export function wornRing(id) {
  return WORN_RINGS.has(decorId(id));
}

export { WORN_RINGS };
