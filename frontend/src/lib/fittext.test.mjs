// Zero-dependency test for the one-line ellipsis (`npm test`).
import { cut, fitPrefix } from "./fittext.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};
const eq = (a, b, msg) => assert(a === b, `${msg}\n  got ${JSON.stringify(a)}\n  exp ${JSON.stringify(b)}`);

const topic = "the couch. kick your shoes off";
// A width of n characters: the measurement fitPrefix is built around.
const chars = (n) => (s) => s.length <= n;

eq(fitPrefix(topic, chars(99)), topic, "a topic that fits is left alone");
// 27 characters ends the prefix exactly after "shoes " — the case the header
// drew as "kick your shoes …".
eq(fitPrefix(topic, chars(27)), "the couch. kick your shoes…", "the space before the cut is dropped");
eq(fitPrefix(topic, chars(26)), "the couch. kick your shoe…", "one less and the word is cut mid-way");
eq(fitPrefix(topic, chars(21)), "the couch. kick your…", "and again at the next space");
eq(fitPrefix(topic, chars(0)), "…", "no room at all leaves the ellipsis");
for (let n = 1; n < topic.length; n++) {
  const got = fitPrefix(topic, chars(n));
  assert(!/\s…$/.test(got), `width ${n}: "${got}" ends in a space before the ellipsis`);
  assert(got.length <= n || got === "…", `width ${n}: "${got}" does not fit`);
}
eq(cut("a  b", 3), "a…", "a run of spaces trims as one");
eq(cut(topic, topic.length), topic, "the whole text carries no ellipsis");

if (failures) {
  console.error(`fittext: ${failures} failure(s)`);
  process.exit(1);
}
console.log("fittext: ok");
