// The out-of-sync banner's actions (`npm test`). A device that is its guild's
// only possible committer and stands behind its members must never be offered
// "Continue from here".
import { syncBannerActions } from "./syncbanner.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

let a = syncBannerActions({ alone: false, cannotRepair: false, retriedInVain: false });
assert(a.retry && !a.continueHere && !a.cannotRepair, "first retries: only Retry now");

a = syncBannerActions({ alone: false, cannotRepair: false, retriedInVain: true });
assert(a.retry && a.continueHere && !a.cannotRepair, "after vain retries: Continue from here appears");

a = syncBannerActions({ alone: false, cannotRepair: true, retriedInVain: true });
assert(a.retry && !a.continueHere && a.cannotRepair, "sole committer behind: no Continue, the plain sentence instead");

a = syncBannerActions({ alone: false, cannotRepair: true, retriedInVain: false });
assert(a.retry && !a.continueHere && !a.cannotRepair, "sole committer behind, before vain retries: only Retry now");

a = syncBannerActions({ alone: true, cannotRepair: false, retriedInVain: false });
assert(!a.retry && a.continueHere && !a.cannotRepair, "alone: Continue from here, as before");

if (failures) {
  console.error(`\n${failures} syncbanner test(s) failed`);
  process.exit(1);
}
console.log("syncbanner.test.mjs: all checks passed");
