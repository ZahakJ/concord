// The lazy-chunk rule (`npm test` runs it): asking for a surface whose chunk
// fails to load must not leave an unhandled rejection, must tell the caller,
// and must let the next ask fetch again.
import { loadOnce, want } from "./loadonce.js";

let failures = 0;
const check = (name, ok) => {
  if (!ok) {
    console.error("  FAIL " + name);
    failures++;
  }
};

let unhandled = 0;
process.on("unhandledRejection", () => unhandled++);
const settle = () => new Promise((r) => setTimeout(r, 0));

let fetches = 0;
let online = false;
let loaded = null;
const loader = loadOnce(
  () => {
    fetches++;
    return online ? Promise.resolve({ default: "Stage" }) : Promise.reject(new Error("offline"));
  },
  (m) => (loaded = m.default),
);

// Two asks at once share one fetch.
let failed = 0;
const a = want(loader, () => failed++);
const b = want(loader, () => failed++);
check("concurrent asks share one fetch", fetches === 1);
const [ra, rb] = await Promise.all([a, b]);
await settle();
check("a failed load resolves false", ra === false && rb === false);
check("each asker is told", failed === 2);
check("no unhandled rejection", unhandled === 0);
check("nothing was installed", loaded === null);

// The next ask is the retry.
online = true;
const ok = await want(loader, () => failed++);
check("the next ask fetches again", fetches === 2);
check("and succeeds", ok === true && loaded === "Stage");

// Once loaded, asks are free.
await want(loader);
check("a loaded chunk is not fetched again", fetches === 2);

await settle();
check("still no unhandled rejection", unhandled === 0);

console.log(failures === 0 ? "loadonce.test.mjs: OK" : `loadonce.test.mjs: ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
