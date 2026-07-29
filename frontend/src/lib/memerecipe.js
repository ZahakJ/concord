// memerecipe.js — remembering how a sent meme was built, so it can be edited.
//
// What goes out on the wire is a flattened JPEG: the captions are burned into
// the pixels and there is no way back. "Edit meme" therefore can't work off the
// picture — it needs the RECIPE (base image, captions, layers) that produced
// it, and that recipe only exists on the machine that did the rendering.
//
// WHERE IT LIVES, and why not localStorage
// ----------------------------------------
// localStorage is a synchronous ~5 MB bucket shared with drafts, prefs, recent
// emoji and everything else this app remembers. A single bring-your-own meme
// with two pasted photos is several megabytes of base64 on its own, so one meme
// could evict the user's entire settings. IndexedDB is the right shelf: it's
// asynchronous, its quota is orders of magnitude larger, and it is a separate
// namespace that can be blown away without touching anything else.
//
// WHAT IT COSTS
// -------------
// A recipe stores template-relative things by REFERENCE — a bundled template's
// base is kept as its "/memes/x.jpg" path, a few dozen bytes, because those
// pixels ship with the app and can always be re-fetched. Only pixels that
// cannot be reconstructed are stored: a bring-your-own base, and each pasted
// layer. Those are data URLs, so they carry base64's 33% overhead on top of the
// original file. The store is bounded twice over (see MAX_RECIPES / MAX_BYTES)
// and evicts oldest-first, so the worst case is fixed and the failure mode is
// "the oldest memes are no longer editable", never "the browser is full".
//
// LOCAL ONLY, deliberately. The recipe is not sent to anyone: it would multiply
// the size of every meme message by an order of magnitude, and the whole point
// of flattening is that recipients need nothing but the picture. So on another
// device, or after eviction, the entry simply isn't offered — see knownRecipe.

const DB_NAME = "concord-meme-recipes";
const DB_VERSION = 1;
const STORE = "recipes";

// Two independent ceilings. The count keeps the index small and the byte
// budget is what actually protects the quota: twenty-four recipes of ONE
// template each is nothing, twenty-four of three pasted photos each is not.
export const MAX_RECIPES = 24;
export const MAX_BYTES = 48 << 20; // 48 MiB of stored data URLs, all recipes together

// Whether we have persistent storage at all. A private window, an ancient
// webview or a node test run has no indexedDB; everything below then degrades
// to "no recipes exist", which is exactly the honest answer.
const available = typeof indexedDB !== "undefined";

let dbp = null;
function db() {
  if (!available) return Promise.reject(new Error("no indexedDB"));
  if (!dbp) {
    dbp = new Promise((res, rej) => {
      const req = indexedDB.open(DB_NAME, DB_VERSION);
      req.onupgradeneeded = () => {
        const d = req.result;
        if (!d.objectStoreNames.contains(STORE)) {
          const os = d.createObjectStore(STORE, { keyPath: "blobId" });
          // Eviction is oldest-first, and walking an index is how that stays
          // cheap when the store holds tens of megabytes of data URLs.
          os.createIndex("at", "at");
        }
      };
      req.onsuccess = () => res(req.result);
      req.onerror = () => rej(req.error || new Error("indexedDB open failed"));
      // A second tab holding an older version open blocks the upgrade forever;
      // failing fast beats a promise that never settles.
      req.onblocked = () => rej(new Error("indexedDB blocked"));
    }).catch((err) => {
      dbp = null; // a transient failure must not poison every later call
      throw err;
    });
  }
  return dbp;
}

function run(mode, fn) {
  return db().then(
    (d) =>
      new Promise((res, rej) => {
        const tx = d.transaction(STORE, mode);
        const out = fn(tx.objectStore(STORE), tx);
        tx.oncomplete = () => res(out && out.value !== undefined ? out.value : out);
        tx.onerror = () => rej(tx.error);
        tx.onabort = () => rej(tx.error || new Error("aborted"));
      }),
  );
}

// ---- the in-memory index ------------------------------------------------
//
// A context menu is built synchronously: by the time an IndexedDB read came
// back the menu would already be on screen without the entry. So the set of
// blob ids we hold a recipe for is mirrored in memory, primed once at boot and
// kept in step by save/drop. It holds ids only — no pixels — so it stays a few
// kilobytes however large the store gets.
const known = new Set();
let priming = null;

export function primeRecipes() {
  if (!available) return Promise.resolve();
  if (!priming) {
    priming = run("readonly", (os) => {
      const req = os.getAllKeys();
      const box = {};
      req.onsuccess = () => (box.value = req.result || []);
      return box;
    })
      .then((ids) => {
        for (const id of ids) known.add(id);
      })
      .catch(() => {
        /* no store, no recipes — every entry stays hidden */
      });
  }
  return priming;
}

// knownRecipe answers the one question a menu needs, synchronously: is this
// picture one we can still reopen? False for someone else's meme, for a meme
// made on another device, and for one that has aged out of the budget.
export function knownRecipe(blobId) {
  return !!blobId && known.has(blobId);
}

// ---- sizing and eviction ------------------------------------------------

// How much of the quota a recipe actually costs. Only the pixel-carrying
// strings are counted — captions and layer geometry are a rounding error next
// to one data URL, and counting them would make the number harder to reason
// about for no benefit. A JS string of base64 is one byte per character.
export function recipeBytes(rec) {
  let n = rec?.base?.startsWith("data:") ? rec.base.length : 0;
  for (const v of Object.values(rec?.assets || {})) n += String(v).length;
  return n;
}

// Which rows have to go for a store of `rows` to fit the budget, oldest first.
// Pure and separately tested: eviction is the part that silently loses a user's
// work if it is wrong, and it is not something a browser test would notice.
//
// `rows` is [{blobId, at, bytes}]; the newest entries are always kept, so a
// single recipe larger than the whole byte budget is still kept when it is the
// only one — being unable to edit the meme you just made is worse than being
// slightly over a self-imposed ceiling.
export function planEvictions(rows, { maxCount = MAX_RECIPES, maxBytes = MAX_BYTES } = {}) {
  const newestFirst = [...rows].sort((a, b) => (b.at || 0) - (a.at || 0));
  const drop = [];
  let total = 0;
  newestFirst.forEach((r, i) => {
    total += r.bytes || 0;
    // `i > 0` is the "always keep the newest" guard.
    if (i >= maxCount || (i > 0 && total > maxBytes)) drop.push(r.blobId);
  });
  return drop;
}

// ---- the store ----------------------------------------------------------

// saveRecipe records how `blobId` was built. Resolves either way: failing to
// remember a recipe must never fail the send that produced it — the picture is
// already in the channel, and the only loss is that this one can't be reopened.
export async function saveRecipe(blobId, session) {
  if (!available || !blobId || !session) return false;
  const rec = { ...session, blobId, at: Date.now() };
  rec.bytes = recipeBytes(rec);
  try {
    await run("readwrite", (os) => os.put(rec));
    known.add(blobId);
    await evict();
    return true;
  } catch {
    return false;
  }
}

export async function loadRecipe(blobId) {
  if (!available || !blobId) return null;
  try {
    const rec = await run("readonly", (os) => {
      const req = os.get(blobId);
      const box = {};
      req.onsuccess = () => (box.value = req.result || null);
      return box;
    });
    // The set is the menu's source of truth, so a miss here has to correct it —
    // otherwise the entry keeps being offered for a recipe that isn't there.
    if (!rec) known.delete(blobId);
    return rec || null;
  } catch {
    return null;
  }
}

// dropRecipe forgets one. Used when an edit supersedes it: the re-rendered
// picture is a new blob, so the old key can never be reached again.
export async function dropRecipe(blobId) {
  known.delete(blobId);
  if (!available || !blobId) return;
  try {
    await run("readwrite", (os) => os.delete(blobId));
  } catch {
    /* it stays until the next eviction sweep */
  }
}

async function evict() {
  const rows = await run("readonly", (os) => {
    const req = os.getAll();
    const box = {};
    req.onsuccess = () =>
      (box.value = (req.result || []).map((r) => ({ blobId: r.blobId, at: r.at || 0, bytes: r.bytes || 0 })));
    return box;
  });
  const drop = planEvictions(rows);
  if (!drop.length) return;
  await run("readwrite", (os) => {
    for (const id of drop) os.delete(id);
  });
  for (const id of drop) known.delete(id);
}

// Prime at import, which is app boot: the first right-click on a meme has to be
// able to answer without waiting for anything.
primeRecipes();
