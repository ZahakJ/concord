// The lock screen's erase word, and the replace confirmation that needs it:
// restoring or linking over the account on a device used to erase it in one
// tap, while starting over took the word typed.
import assert from "node:assert/strict";
import { ERASE_WORD, eraseWordMatches, replaceEraseLabel, replaceEraseReady } from "./eraseword.js";

assert.equal(ERASE_WORD, "ERASE");
for (const ok of ["ERASE", "erase", " Erase ", "ERASE\n"]) assert.ok(eraseWordMatches(ok), ok);
for (const no of ["", "ERAS", "ERASE!", "delete", "Alice", null, undefined, "E R A S E"]) assert.ok(!eraseWordMatches(no), String(no));

// Nothing typed, nothing goes ahead: the one-tap erase.
assert.equal(replaceEraseReady({ typed: "", kind: "restore" }), false);
assert.equal(replaceEraseReady({ typed: "ERAS", kind: "restore" }), false);
assert.equal(replaceEraseReady({ typed: "ERASE", kind: "restore" }), true);
assert.equal(replaceEraseReady({ typed: "ERASE", kind: "restore", busy: true }), false);
// A link needs a new code as well as the word.
assert.equal(replaceEraseReady({ typed: "ERASE", kind: "link", linkCode: "" }), false);
assert.equal(replaceEraseReady({ typed: "ERASE", kind: "link", linkCode: "  " }), false);
assert.equal(replaceEraseReady({ typed: "", kind: "link", linkCode: "abc" }), false);
assert.equal(replaceEraseReady({ typed: "erase", kind: "link", linkCode: "abc" }), true);

// The button names an account only when the core confirmed it.
assert.equal(replaceEraseLabel({ match: "different", kind: "restore", verified: true, name: "Alice" }), "Erase Alice and restore");
assert.equal(replaceEraseLabel({ match: "different", kind: "link", verified: true, name: "Alice" }), "Erase Alice and link");
assert.equal(
  replaceEraseLabel({ match: "different", kind: "restore", verified: false, name: "Alice" }),
  "Erase the account on this device and restore",
);
assert.equal(replaceEraseLabel({ match: "unknown", kind: "restore", verified: true, name: "Alice" }), "Erase the account on this device and restore");
assert.equal(replaceEraseLabel({ match: "unknown", kind: "link", name: "Alice" }), "Erase the account on this device and link");
assert.equal(replaceEraseLabel({ match: "different", kind: "restore", verified: true, name: "" }), "Erase the account on this device and restore");
assert.equal(replaceEraseLabel({ match: "same", kind: "restore", verified: true, name: "Alice" }), "Erase this device's copy and restore");
assert.equal(replaceEraseLabel({ match: "same", kind: "link", verified: true, name: "Alice" }), "Erase this device's copy and link");
// Data whose keystore is missing: no account can be opened, so none is named.
assert.equal(replaceEraseLabel({ match: "unknown", kind: "restore", name: "Chen", hasIdentity: false }), "Erase this data and restore");

console.log("eraseword: ok");
