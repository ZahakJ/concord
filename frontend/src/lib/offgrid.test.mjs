import { fmtCost, fmtRate, statusLine, usable } from "./offgrid.js";

let failures = 0;
const eq = (name, got, want) => {
  if (got !== want) {
    console.error(`  FAIL ${name}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
    failures++;
  }
};

eq("free", fmtCost(0), "free");
eq("cents", fmtCost(40000), "4¢");
eq("dollars", fmtCost(1500000), "$1.50");
eq("bps", fmtRate(300), "300 bps");
eq("kbps", fmtRate(1200), "1.2 kbps");
const ham = { caps: { encryption: false }, status: { state: "available" } };
eq("ham never usable", usable(ham), false);
eq("ham explained", statusLine(ham).startsWith("Off limits"), true);
eq("missing hardware not usable", usable({ caps: { encryption: true }, status: { state: "no-hardware" } }), false);
eq("enabled stays switchable off", usable({ caps: { encryption: true }, enabled: true, status: { state: "no-hardware" } }), true);

if (failures) process.exit(1);
console.log("offgrid: ok");
