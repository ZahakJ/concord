// Zero-dependency test for check-card-readers.mjs.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { problems, DEBUG_CERT } from "./check-card-readers.mjs";

let failures = 0;
const assert = (c, m) => {
  if (!c) {
    console.error("FAIL:", m);
    failures++;
  }
};
const JAYB = "90".repeat(32);
function tree(files) {
  const d = fs.mkdtempSync(path.join(os.tmpdir(), "cards-"));
  for (const [p, body] of Object.entries(files)) {
    fs.mkdirSync(path.dirname(path.join(d, p)), { recursive: true });
    fs.writeFileSync(path.join(d, p), body);
  }
  return d;
}
const arr = (...items) => `<resources><string-array name="card_readers">${items.map((i) => `<item>${i}</item>`).join("")}</string-array></resources>`;

assert(problems(tree({ "src/main/res/values/card_readers.xml": arr(JAYB), "src/debug/res/values/card_readers.xml": arr(JAYB, DEBUG_CERT) })).length === 0, "debug key under src/debug only: ok");
assert(problems(tree({ "src/main/res/values/card_readers.xml": arr(JAYB, DEBUG_CERT) })).length === 1, "debug key in src/main fails");
assert(problems(tree({ "src/main/res/values/card_readers.xml": arr(JAYB), "src/release/res/values/x.xml": arr(DEBUG_CERT.toUpperCase()) })).length === 1, "in any other source set, any case, fails");
assert(problems(tree({ "src/main/res/values/card_readers.xml": arr() })).length === 1, "an empty list fails");
assert(problems(tree({ "src/main/res/values/card_readers.xml": arr(JAYB) }), { merged: true }).some((p) => p.includes("no merged")), "--merged without a build fails");
assert(problems(tree({ "src/main/res/values/card_readers.xml": arr(JAYB), "build/intermediates/merged_res/release/values/values.xml": arr(JAYB, DEBUG_CERT) }), { merged: true }).length === 1, "a merged release with the debug key fails");

// The real tree.
const real = path.join(path.dirname(new URL(import.meta.url).pathname), "../apps/mobile/android/app");
assert(problems(real).length === 0, `the real tree: ${problems(real)}`);

if (failures) process.exit(1);
console.log("check-card-readers: ok");
