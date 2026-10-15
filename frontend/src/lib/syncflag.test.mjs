// raisedFor holds a flag for a call's synchronous prefix only (`npm test` runs
// it). The case it exists for: a guild browse is awaiting its channel load
// when the user taps a channel row; the tap must not read the browse's flag.
import { raisedFor } from "./syncflag.js";

let failures = 0;
const check = (name, ok) => {
  if (!ok) {
    console.error("  FAIL " + name);
    failures++;
  }
};

let browsing = false;
const set = (v) => (browsing = v);
const seen = [];
let release;
const gate = new Promise((r) => (release = r));
// Shaped like selectChannel: reads the flag before its first await, then waits.
async function select(id, wait) {
  seen.push([id, browsing]);
  if (wait) await gate;
}

const browse = raisedFor(set, true, () => select("resume", true));
check("the browsing call saw its own flag", seen[0][1] === true);
check("the flag is down as soon as the call has started", browsing === false);
// A tap while the browse is still awaiting.
await select("tapped", false);
check("a tap during the browse is not treated as a browse", seen[1][1] === false);
release();
await browse;
check("the flag is still down once the browse resolves", browsing === false);

// A throw in the synchronous part still lowers the flag.
try {
  raisedFor(set, true, () => {
    throw new Error("boom");
  });
} catch {
  /* expected */
}
check("a synchronous throw lowers the flag", browsing === false);

console.log(failures === 0 ? "syncflag.test.mjs: OK" : `syncflag.test.mjs: ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
