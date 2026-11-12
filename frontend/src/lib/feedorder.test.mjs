// feedorder.test.mjs — live insertion follows the store's order.
import assert from "node:assert/strict";
import { insertBySent, compareSent } from "./feedorder.js";

const at = (id, sent) => ({ id, sent });
const ids = (l) => l.map((m) => m.id).join(",");

// The common case: newest arrives last, appended.
let l = [at("a", "2026-11-10T10:00:00.000000000Z"), at("b", "2026-11-10T10:00:01.000000000Z")];
assert.equal(ids(insertBySent(l, at("c", "2026-11-10T10:00:02.000000000Z"))), "a,b,c");

// A message written earlier but delivered later goes where the store puts it.
assert.equal(ids(insertBySent(l, at("x", "2026-11-10T10:00:00.500000000Z"))), "a,x,b");
assert.equal(ids(insertBySent(l, at("y", "2026-11-10T09:00:00.000000000Z"))), "y,a,b");

// Sub-millisecond: a burst inside one millisecond still sorts by its nanos.
l = [at("p", "2026-11-10T10:00:00.000000300Z")];
assert.equal(ids(insertBySent(l, at("q", "2026-11-10T10:00:00.000000200Z"))), "q,p");

// Offsets are compared as instants, not as text.
assert.ok(compareSent(at("1", "2026-11-10T12:00:00.000000000+03:00"), at("2", "2026-11-10T10:00:00.000000000Z")) < 0);

// Equal instants: the id decides, the same way on every peer.
const t = "2026-11-10T10:00:00.000000000Z";
assert.equal(ids(insertBySent([at("m", t)], at("k", t))), "k,m");
assert.equal(ids(insertBySent([at("k", t)], at("m", t))), "k,m");

// Arrival order does not matter: every permutation ends up the same.
const msgs = [at("a1", "2026-11-10T10:00:00.1Z"), at("b1", "2026-11-10T10:00:00.15Z"), at("a2", "2026-11-10T10:00:00.2Z"), at("c1", "2026-11-10T10:00:00.2Z"), at("b2", "2026-11-10T10:00:00.3Z")];
const perms = (a) => (a.length <= 1 ? [a] : a.flatMap((x, i) => perms([...a.slice(0, i), ...a.slice(i + 1)]).map((p) => [x, ...p])));
const want = ids([...msgs].sort(compareSent));
for (const p of perms(msgs)) assert.equal(ids(p.reduce((acc, m) => insertBySent(acc, m), [])), want);

// Unparseable timestamps never throw and sort after real ones.
assert.equal(ids(insertBySent([at("z", "")], at("a", t))), "a,z");

console.log("feedorder: ok");
