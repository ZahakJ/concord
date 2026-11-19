// hidden.test.mjs — what the UI does when a message is hidden or restored.
import assert from "node:assert/strict";
import { dropMessage, withHidden, replyState, HIDE_BODY, HIDDEN_QUOTE } from "./hidden.js";

const feed = [{ id: "a" }, { id: "b" }, { id: "c" }];

assert.deepEqual(dropMessage(feed, "b").map((m) => m.id), ["a", "c"]);
assert.equal(dropMessage(feed, "zzz"), feed, "an unknown id leaves the feed untouched (same array)");
assert.equal(feed.length, 3, "the input is not mutated");

let set = {};
set = withHidden(set, "a", true);
assert.deepEqual(set, { a: true });
const before = set;
set = withHidden(set, "b", true);
assert.notEqual(set, before);
assert.deepEqual(before, { a: true }, "earlier snapshot untouched");
set = withHidden(set, "a", false);
assert.deepEqual(set, { b: true });

assert.equal(replyState("x", { id: "x", content: "hi" }, {}), "ok");
assert.equal(replyState("x", null, {}), "missing");
assert.equal(replyState("x", { deleted: true }, {}), "deleted");
assert.equal(replyState("x", { id: "x", content: "secret" }, { x: true }), "hidden", "hidden beats a copy still in memory");
assert.equal(replyState("x", null, { x: true }), "hidden");
assert.equal(replyState("", null, { x: true }), "missing");

// A reply names any id its author chose: names every object answers for are
// not hidden ids, and a hidden id is one this user hid, nothing inherited.
for (const id of ["constructor", "__proto__", "toString", "hasOwnProperty"]) {
  assert.equal(replyState(id, null, {}), "missing", `${id} reads as hidden`);
  assert.equal(replyState(id, null, withHidden({}, "a", true)), "missing", `${id} reads as hidden`);
}
assert.equal(replyState("x", null, Object.create({ x: true })), "missing", "an inherited entry is not a hidden id");
let odd = withHidden({}, "__proto__", true);
assert.equal(Object.getPrototypeOf(odd), Object.prototype, "a hide cannot change the set's prototype");
odd = withHidden(withHidden({}, "constructor", true), "constructor", false);
assert.equal(replyState("constructor", null, odd), "missing");

assert.match(HIDE_BODY, /your devices only\. Others still see it\./);
// The quote names the action the person took, as the toast does.
assert.equal(HIDDEN_QUOTE, "Deleted for you");
console.log("ok");
