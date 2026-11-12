import { landsOnWelcome } from "./firstrun.js";

let failures = 0;
const check = (what, ok) => {
  if (!ok) {
    console.error(`  FAIL ${what}`);
    failures++;
  }
};

check("nothing at all", landsOnWelcome([]));
check("only Notes", landsOnWelcome([{ id: "n", kind: "dm", dmNotes: true }]));
check("a guild", !landsOnWelcome([{ id: "n", kind: "dm", dmNotes: true }, { id: "g", kind: "" }]));
check("a DM with somebody", !landsOnWelcome([{ id: "n", kind: "dm", dmNotes: true }, { id: "d", kind: "dm" }]));

console.log(failures === 0 ? "firstrun.test.mjs: OK" : `firstrun.test.mjs: ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
