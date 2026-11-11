// F6 walks the regions in reading order and wraps; Shift+F6 walks back.
import { nextRegion } from "./regions.js";

const fail = (msg) => {
  console.error("  FAIL", msg);
  process.exitCode = 1;
};
const all = ["sidebar", "header", "feed", "composer", "inspector"];
const eq = (a, b, msg) => a !== b && fail(`${msg}: got ${a}, want ${b}`);

eq(nextRegion(all, "", 1), "sidebar", "from nowhere, forward starts at the sidebar");
eq(nextRegion(all, "", -1), "inspector", "from nowhere, back starts at the end");
eq(nextRegion(all, "feed", 1), "composer", "feed → composer");
eq(nextRegion(all, "inspector", 1), "sidebar", "wraps forward");
eq(nextRegion(all, "sidebar", -1), "inspector", "wraps back");
// A closed inspector is simply not in the walk.
eq(nextRegion(all.slice(0, 4), "composer", 1), "sidebar", "composer wraps to the sidebar with no inspector");
eq(nextRegion([], "", 1), "", "nothing on screen");

if (!process.exitCode) console.log("regions.test.mjs: ok");
