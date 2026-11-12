// grouping.test.mjs — when a row tucks under the one above it.
import assert from "node:assert/strict";
import { continuesGroup } from "./grouping.js";

const base = "2026-11-10T10:00:00.000Z";
const later = (s) => new Date(Date.parse(base) + s * 1000).toISOString();
const a1 = { id: "1", sender: "A", kind: "", sent: base };
const a2 = { id: "2", sender: "A", kind: "", sent: later(30) };

assert.equal(continuesGroup(a1, a2), true, "same author within the window groups");
assert.equal(continuesGroup(a1, { ...a2, sender: "B" }), false, "another author breaks");
assert.equal(continuesGroup(a1, { ...a2, sent: later(400) }), false, "a long pause breaks");
assert.equal(continuesGroup(a1, a2, { newDay: true }), false, "a day divider breaks");
assert.equal(continuesGroup(a1, a2, { emote: true }), false, "an emote breaks");
assert.equal(continuesGroup(a1, { ...a2, replyTo: "x" }), false, "a reply wears its header");
assert.equal(continuesGroup({ ...a1, deleted: true }, a2), false);
assert.equal(continuesGroup(a1, { ...a2, kind: "system" }), false);
// The first row under the NEW divider shows who wrote it.
assert.equal(continuesGroup(a1, a2, { firstUnread: true }), false, "the NEW line breaks a group");
// Guests relayed by one host are different authors.
const g1 = { ...a1, kind: "guest", senderName: "Sam" };
assert.equal(continuesGroup(g1, { ...a2, kind: "guest", senderName: "Ola" }), false);
assert.equal(continuesGroup(g1, { ...a2, kind: "guest", senderName: "Sam" }), true);
assert.equal(continuesGroup(null, a2), false);

console.log("grouping: ok");
