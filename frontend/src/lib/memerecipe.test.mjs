// Tests for the parts of the recipe store that decide what to THROW AWAY.
// Everything else in memerecipe.js is IndexedDB plumbing that only means
// anything in a browser; eviction is arithmetic, and getting it wrong silently
// loses the user's ability to edit memes they still care about.
//
// Importing the module in node exercises its "no indexedDB" path too: if that
// guard regressed, this file would throw on import.
import { planEvictions, recipeBytes, knownRecipe, MAX_RECIPES } from "./memerecipe.js";

let failures = 0;
function check(name, got, want) {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) {
    failures++;
    console.error(`FAIL ${name}\n  got:  ${g}\n  want: ${w}`);
  }
}

// A store inside both budgets loses nothing.
check("nothing to evict", planEvictions([{ blobId: "a", at: 2, bytes: 10 }, { blobId: "b", at: 1, bytes: 10 }]), []);

// Over the count: the oldest go, newest survive.
{
  const rows = [];
  for (let i = 0; i < MAX_RECIPES + 3; i++) rows.push({ blobId: `r${i}`, at: i, bytes: 1 });
  const drop = planEvictions(rows);
  check("count budget drops the excess", drop.length, 3);
  check("count budget drops the OLDEST", drop.sort(), ["r0", "r1", "r2"]);
}

// Over the byte budget: walking newest-first, everything past the line goes,
// regardless of how few rows there are.
check(
  "byte budget drops the oldest",
  planEvictions(
    [
      { blobId: "new", at: 3, bytes: 60 },
      { blobId: "mid", at: 2, bytes: 60 },
      { blobId: "old", at: 1, bytes: 60 },
    ],
    { maxBytes: 100 },
  ),
  ["mid", "old"],
);

// The newest recipe is kept even when it alone blows the whole budget: you have
// just made that meme, and refusing to remember it is the one failure the user
// would actually notice.
check(
  "the newest is never evicted",
  planEvictions([{ blobId: "huge", at: 9, bytes: 999 }], { maxBytes: 10 }),
  [],
);

// planEvictions must not disturb its input (it is called with a live list).
{
  const rows = [{ blobId: "a", at: 1, bytes: 1 }, { blobId: "b", at: 2, bytes: 1 }];
  planEvictions(rows);
  check("input order untouched", rows.map((r) => r.blobId), ["a", "b"]);
}

// Only pixels are counted. A bundled template's base is a path, not data, so it
// costs nothing — that is the whole reason a template meme is cheap to keep.
check("a template base is free", recipeBytes({ base: "/memes/drake.jpg", assets: {} }), 0);
check("a pasted base is counted", recipeBytes({ base: "data:image/png;base64,AAAA", assets: {} }), 26);
check(
  "layer assets are counted",
  recipeBytes({ base: "/memes/x.jpg", assets: { a1: "0123456789", a2: "01234" } }),
  15,
);
check("a missing recipe costs nothing", recipeBytes(null), 0);

// Without IndexedDB (this process) nothing is ever claimed to be editable.
check("no store, no recipes", knownRecipe("deadbeef"), false);
check("no id, no recipe", knownRecipe(""), false);

if (failures) {
  console.error(`\n${failures} memerecipe test(s) failed`);
  process.exit(1);
}
console.log("memerecipe.js: all tests passed");
