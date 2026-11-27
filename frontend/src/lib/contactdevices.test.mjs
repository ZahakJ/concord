// Zero-dependency test for the contact devices line (`npm test`).
import { deviceLine } from "./contactdevices.js";

let failures = 0;
const ok = (c, msg) => {
  if (!c) {
    console.error("FAIL: " + msg);
    failures++;
  }
};
const now = new Date(2026, 10, 26).getTime();
ok(deviceLine({ count: 1, added: [] }, now) === "", "one device and nothing added is no line");
ok(deviceLine(null, now) === "", "nothing known is no line");
ok(deviceLine({ count: 2, added: [] }, now) === "2 devices", "a count alone");
const l = deviceLine({ count: 2, added: [{ name: "Pixel", issued: new Date(2026, 10, 20).getTime() / 1000 }] }, now);
ok(/^2 devices · Pixel added /.test(l) && /20/.test(l) && !/2026/.test(l), `the newest added, this year: ${l}`);
ok(/2025/.test(deviceLine({ count: 3, added: [{ name: "", seen: new Date(2025, 0, 2).getTime() / 1000 }] }, now)), "another year is dated with it");
ok(deviceLine({ count: 2, added: [{ name: " " }] }, now) === "2 devices · A device added", "no name, no date");
if (failures) {
  console.error(`${failures} contactdevices test(s) failed`);
  process.exit(1);
}
console.log("contactdevices: all tests passed");
