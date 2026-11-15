// chatlist.test.mjs — pinned and archived chat ordering.
import assert from "node:assert/strict";
import { arrange, canPin, movePin, archivedUnread, MAX_PINS, chatIdOf } from "./chatlist.js";

const dm = (id, extra = {}) => ({ id: "g" + id, channels: [{ id }], ...extra });
const ids = (l) => l.map((d) => d.dmNotes ? "notes" : chatIdOf(d)).join(",");
const notes = { id: "n", dmNotes: true };
const list = [notes, dm("a"), dm("b"), dm("c"), dm("d")];

// No prefs: unchanged.
assert.equal(ids(arrange(list, {}).main), "notes,a,b,c,d");

// Pinned float up in their own order, below Notes; others keep activity order.
let r = arrange(list, { c: { p: true, k: 2 }, d: { p: true, k: 1 } });
assert.equal(ids(r.main), "notes,d,c,a,b");

// Archived leave the main list and keep order; archive beats pin.
r = arrange(list, { a: { a: true }, c: { p: true, a: true, k: 1 }, d: { p: true, k: 1 } });
assert.equal(ids(r.main), "notes,d,b");
assert.equal(ids(r.archived), "a,c");

// Notes can never be archived or pinned away.
r = arrange(list, { n: { a: true } });
assert.equal(ids(r.main), "notes,a,b,c,d");

// Equal keys are ordered deterministically.
assert.equal(ids(arrange(list, { b: { p: true, k: 1 }, a: { p: true, k: 1 } }).main), "notes,a,b,c,d");

// Cap.
const many = Array.from({ length: MAX_PINS + 2 }, (_, i) => dm("x" + i));
const prefs = {};
many.slice(0, MAX_PINS).forEach((d, i) => (prefs[chatIdOf(d)] = { p: true, k: i }));
assert.equal(canPin(many, prefs).ok, false);
assert.match(canPin(many, prefs).message, /up to 10/);
delete prefs.x0;
assert.equal(canPin(many, prefs).ok, true);

// Moving a pin one step.
const pins = arrange(list, { a: { p: true, k: 1 }, b: { p: true, k: 2 }, c: { p: true, k: 3 } }).pinned;
assert.deepEqual(movePin(pins, "c", -1), ["a", "c", "b"]);
assert.deepEqual(movePin(pins, "a", -1), ["a", "b", "c"]);
assert.deepEqual(movePin(pins, "c", 1), ["a", "b", "c"]);

// Chat ids are chosen by a DM's creator: only the record map's own entries
// count, never one it inherits.
const inherited = Object.create({ e: { p: true, k: 1 }, f: { a: true } });
r = arrange([notes, dm("e"), dm("f"), dm("constructor"), dm("__proto__")], inherited);
assert.equal(ids(r.main), "notes,e,f,constructor,__proto__");
assert.equal(r.archived.length, 0);

// Archived unread total.
assert.equal(archivedUnread([dm("a"), dm("b")], (d) => (chatIdOf(d) === "a" ? 3 : 0)), 3);
console.log("chatlist.test.mjs ok");
