// node src/lib/passnudge.test.mjs — the passphrase card is rare and skippable.
import assert from "node:assert/strict";
const m = new Map();
globalThis.localStorage = { getItem: (k) => m.get(k) ?? null, setItem: (k, v) => m.set(k, String(v)), removeItem: (k) => m.delete(k) };
const n = await import("./passnudge.js");
const DAY = 86_400_000;
const t0 = 1_800_000_000_000;

assert.equal(n.nudgeDue(t0), false, "nothing recorded");
n.passphraseUsed(t0);
for (let i = 0; i < n.MIN_UNLOCKS; i++) n.fingerprintUsed();
assert.equal(n.nudgeDue(t0 + (n.GAP_DAYS - 1) * DAY), false, "too soon");
assert.equal(n.nudgeDue(t0 + n.GAP_DAYS * DAY), true, "due");
n.snoozeNudge(t0 + n.GAP_DAYS * DAY);
assert.equal(n.nudgeDue(t0 + (n.GAP_DAYS + 1) * DAY), false, "put off");
n.passphraseUsed(t0 + 30 * DAY);
assert.equal(n.nudgeDue(t0 + 60 * DAY), false, "typed again: counter reset");
console.log("passnudge.test: ok");
