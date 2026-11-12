// The removal band's wording, per reason (`npm test`). A ban must never promise
// a way back, and a kick must not promise one without the invite it needs.
import { evictedCopy } from "./evicted.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

const banned = evictedCopy("banned");
assert(banned.lead.includes("banned"), "a ban says banned");
assert(!/let you back in from|allow it/.test(banned.next), "a ban does not promise readmission");

const kicked = evictedCopy("removed");
assert(kicked.lead.includes("removed"), "a kick says removed");
assert(kicked.next.includes("invite"), "a kick says a new invite is needed to come back");

const unseated = evictedCopy("unseated");
assert(!unseated.lead.includes("removed"), "a never-admitted newcomer is not told they were removed");
assert(unseated.next.includes("new one"), "a never-admitted newcomer is told to get a real invite");

assert(evictedCopy("").lead === kicked.lead, "an unknown reason reads as a removal");

if (failures) {
  console.error(`\n${failures} evicted test(s) failed`);
  process.exit(1);
}
console.log("evicted.test.mjs: all checks passed");
