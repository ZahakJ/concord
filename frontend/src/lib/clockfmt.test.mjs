// clockfmt.test.mjs — the kept formatter says exactly what toLocaleTimeString
// said, for every clock setting, and is built once per setting.
import assert from "node:assert/strict";
import { clockFormatter, formatClock } from "./clockfmt.js";

const iso = "2026-11-10T19:07:00.000000000Z";
const d = new Date(iso);
for (const [pref, extra] of [["12", { hour12: true }], ["24", { hour12: false }], ["", {}]]) {
  assert.equal(formatClock(iso, pref), d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit", ...extra }));
}
// Reused while the setting holds, rebuilt when it changes.
const a = clockFormatter("24");
assert.equal(clockFormatter("24"), a);
assert.notEqual(clockFormatter("12"), a);
// Not a time: nothing, rather than "Invalid Date" in the gutter.
assert.equal(formatClock("not a date", "24"), "");
assert.equal(formatClock(undefined, "24"), "");
console.log("clockfmt ok");
