// What the browser's Back does inside the web build (`npm test` runs it).
import { backStep, makeTrail } from "./webback.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

assert(backStep({ inPost: true, phoneChat: true, prevChannel: "a" }) === "closePost", "a forum post closes first");
assert(backStep({ phoneChat: true, prevChannel: "a" }) === "toList", "a phone goes back to the channel list");
assert(backStep({ prevChannel: "a" }) === "prevChannel", "a desktop goes back to the previous conversation");
assert(backStep({}) === "", "nothing left: let the browser leave");

const t = makeTrail(3);
t.visit("gen");
t.visit("gen");
assert(t.previous() === "", "one conversation: nowhere to go back to");
t.visit("rnd");
assert(t.previous() === "gen", "previous is the one before");
assert(t.pop() === "gen", "pop returns where back lands");
assert(t.previous() === "", "and the trail shrinks");
t.visit("a");
t.visit("b");
t.visit("c");
t.visit("d");
assert(t.pop() === "c" && t.pop() === "b" && t.pop() === "", "the trail is capped");

if (failures) {
  console.error(`${failures} webback test(s) failed`);
  process.exit(1);
}
console.log("webback: all tests passed");
