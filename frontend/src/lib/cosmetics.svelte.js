// Lazy access to the cosmetic tables.
//
// The four libraries behind decorations, rings, card frames and card scenes are
// the largest thing this app ships: a quarter of the boot bundle, and pure data
// — thousands of path strings that exist so that the studios have something to
// offer and a wearer has something to wear. Almost every session renders none
// of them, and the ones that do render one or two, so paying for all four
// before the first message appears is the wrong trade in every case.
//
// Each getter returns the module namespace, or null until it has arrived. The
// callers are all painters guarded by `{#if art}`, and every one of them draws
// into an overlay that is positioned over something else — so "not yet" costs a
// frame of a cosmetic not being there, and never a line of text moving.
//
// Reading a getter inside a $derived does two things at once: it starts the
// fetch on first read, and it subscribes that derived to its arrival. Nothing
// has to poll and nothing has to be told twice.

function lazy(load) {
  const box = $state({ m: null });
  let started = false;
  return () => {
    if (!started) {
      started = true;
      // A table that fails to load leaves box.m null, which every caller
      // already renders as "no cosmetic" — the same as an id this build does
      // not recognise. Nothing else in the page depends on it.
      load().then(
        (m) => (box.m = m),
        () => {},
      );
    }
    return box.m;
  };
}

export const decorationsTable = lazy(() => import("./decorations.js"));
export const ringsTable = lazy(() => import("./rings.js"));
export const cardFramesTable = lazy(() => import("./cardframes.js"));
export const cardScenesTable = lazy(() => import("./cardscenes.js"));
export const cardFxTable = lazy(() => import("./cardfx.js"));

// Fetch all four without waiting to be asked. Called once the app has painted:
// a member list full of decorations is the common case on a guild that uses
// them, and one round trip during the idle moment after boot is better than
// four separate ones the first time somebody scrolls a roster.
export function precacheCosmetics() {
  decorationsTable();
  ringsTable();
  cardFramesTable();
  cardScenesTable();
  cardFxTable();
}
