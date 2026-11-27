// Zero-dependency test for the list-edge fades (`npm test`).
import { edgeState, FADE_SLACK } from "./edgefade.js";

let failures = 0;
const eq = (got, want, msg) => {
  if (JSON.stringify(got) !== JSON.stringify(want)) {
    console.error(
      `FAIL: ${msg}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`,
    );
    failures++;
  }
};

// A list that fits has nothing passing under either edge.
eq(edgeState(0, 500, 600), "", "fits: no fade");
// At the top of a long list: content passes under the bottom edge only.
eq(edgeState(0, 3000, 600), "bottom", "at the top: the bottom fades");
// In the middle: both.
eq(edgeState(1200, 3000, 600), "both", "middle: both edges fade");
// At the end (the feed's resting place): only the top.
eq(edgeState(2400, 3000, 600), "top", "at the end: the top fades");
// Within the slack of an edge counts as at it (sub-pixel scroll positions).
eq(
  edgeState(FADE_SLACK - 1, 3000, 600),
  "bottom",
  "a pixel off the top is the top",
);
eq(
  edgeState(2400 - FADE_SLACK + 1, 3000, 600),
  "top",
  "a pixel off the end is the end",
);

if (failures) {
  console.error(`${failures} edgefade test(s) failed`);
  process.exit(1);
}
console.log("edgefade: all tests passed");
