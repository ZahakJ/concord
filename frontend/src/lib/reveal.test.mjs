// reveal.test.mjs — where the view offers "Show original" for a deleted message.
import assert from "node:assert/strict";
import { canRevealDeleted, notKept, NOT_KEPT } from "./reveal.js";

const deleted = { deleted: true };
assert.equal(canRevealDeleted(deleted, "", true), true, "a guild moderator is offered the reveal");
assert.equal(canRevealDeleted(deleted, "meeting", true), true, "a meeting's host moderates it");
assert.equal(canRevealDeleted(deleted, undefined, true), true, "a guild without a kind field is a guild");
assert.equal(canRevealDeleted(deleted, "dm", true), false, "a one-to-one DM keeps no deleted body");
assert.equal(canRevealDeleted(deleted, "group", true), false, "a group conversation has no moderators");
assert.equal(canRevealDeleted(deleted, "", false), false, "only a moderator is offered it");
assert.equal(canRevealDeleted({ deleted: true, expired: true }, "", true), false, "an expired message was erased everywhere");
assert.equal(canRevealDeleted({ deleted: false }, "", true), false, "a live message has nothing to reveal");
assert.equal(canRevealDeleted(null, "", true), false);

assert.equal(notKept(new Error("app: this device did not keep the deleted message")), true);
assert.equal(notKept("app: this device did not keep the deleted message"), true);
assert.equal(notKept(new Error("app: only moderators can view deleted messages")), false);
assert.equal(notKept(undefined), false);
assert.match(NOT_KEPT, /Not kept on this device/);
console.log("reveal: ok");
