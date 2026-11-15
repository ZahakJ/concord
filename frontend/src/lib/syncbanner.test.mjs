// The out-of-sync banner's actions (`npm test`). A device that is its guild's
// only possible committer and stands behind its members must never be offered
// "Continue from here".
import { syncBannerActions, cannotRepairText } from "./syncbanner.js";

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
assert(a.retry && !a.continueHere && a.cannotRepair, "sole committer behind: the sentence shows at once, beside Retry now");

a = syncBannerActions({ alone: true, cannotRepair: false, retriedInVain: false });
assert(!a.retry && a.continueHere && !a.cannotRepair, "alone: Continue from here, as before");

assert(
  cannotRepairText("") ===
    "This copy fell behind and cannot be repaired from here. Start a new guild with the same people.",
  "a guild is called a guild",
);
assert(
  cannotRepairText("dm") ===
    "This copy fell behind and cannot be repaired from here. Start a new conversation with the same people.",
  "a DM is called a conversation",
);

if (failures) {
  console.error(`\n${failures} syncbanner test(s) failed`);
  process.exit(1);
}
console.log("syncbanner.test.mjs: all checks passed");
