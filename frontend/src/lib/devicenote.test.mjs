// devicenote.test.mjs — each state of the new-device line and its wording.
import assert from "node:assert/strict";
import { deviceNote, DEVICE_VERIFY, DEVICE_VERIFIED, DEVICE_UNCHECKED } from "./devicenote.js";

const stored = "Ann added a new device (Pixel, 11-14). If this wasn't Ann, verify.";

assert.equal(DEVICE_VERIFY, "Verify");
assert.equal(DEVICE_VERIFIED, "Verified");
assert.equal(DEVICE_UNCHECKED, "New device, not checked");

// Contact not verified: the line as the core wrote it, and the Verify button.
let n = deviceNote({ content: stored, verified: false, name: "Ann" });
assert.deepEqual(n, { text: stored, action: "verify", tag: "", markTitle: "" });

// Unknown here (no member record, so no verified flag): same as not verified.
n = deviceNote({ content: stored });
assert.equal(n.action, "verify");
assert.equal(n.tag, "");

// Contact verified: "Verified" is about the person, and the device is still
// marked new and not checked. The line no longer asks for a check.
n = deviceNote({ content: stored, verified: true, name: "Ann" });
assert.equal(n.action, "verified");
assert.equal(n.text, "Ann added a new device (Pixel, 11-14).");
assert.equal(n.tag, "New device, not checked");
assert.match(n.markTitle, /^You verified Ann\. That checked their account, not this device\.$/);

// Verified never comes without the not-checked tag, whatever the text says.
for (const content of ["", "Something else entirely.", stored, "They added a new device. If this wasn't them, verify."]) {
  const v = deviceNote({ content, verified: true });
  assert.equal(v.tag, DEVICE_UNCHECKED, `no tag for ${JSON.stringify(content)}`);
  assert.equal(v.action, "verified");
}
assert.equal(deviceNote({ content: "They added a new device. If this wasn't them, verify.", verified: true }).text, "They added a new device.");
// Text the core did not write that way is shown as it is.
assert.equal(deviceNote({ content: "Something else entirely.", verified: true }).text, "Something else entirely.");
assert.match(deviceNote({ content: stored, verified: true }).markTitle, /You verified this person\./);

console.log("ok");
