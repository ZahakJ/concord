import assert from "node:assert/strict";
import { showUnsignedMark, UNSIGNED_TIP } from "./unsigned.js";

const me = "AAAA BBBB";
assert.equal(showUnsignedMark({ unverified: true, sender: "CCCC" }, me), true, "a relayed unsigned row from someone else is marked");
assert.equal(showUnsignedMark({ unverified: false, sender: "CCCC" }, me), false, "a proven row is not marked");
assert.equal(showUnsignedMark({ unverified: true, sender: me }, me), false, "a row this account wrote is never marked");
assert.equal(showUnsignedMark({ unverified: true, sender: me, kind: "guest" }, me), true, "a guest line relayed under our key is not ours");
assert.equal(showUnsignedMark(null, me), false);
assert.ok(!/nobody proved/i.test(UNSIGNED_TIP), "the tip describes the copy, it does not accuse");
assert.match(UNSIGNED_TIP, /author's own device/);
console.log("unsigned: ok");
