// Concord never asks for photo-library or storage access (the owner's rule;
// the "+" sheet uses the system photo picker). scripts/check-media-permissions.mjs
// is the guard CI runs on the source manifest and on the merged one a Gradle
// build produces; this pins what it catches, and that the source manifest
// passes it today.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { forbiddenIn, FORBIDDEN } from "../../../scripts/check-media-permissions.mjs";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

const wrap = (body) => `<?xml version="1.0"?><manifest xmlns:android="http://schemas.android.com/apk/res/android" xmlns:tools="http://schemas.android.com/tools">${body}</manifest>`;
for (const p of FORBIDDEN) {
  assert(forbiddenIn(wrap(`<uses-permission android:name="${p}" />`)).includes(p), `${p} is caught`);
}
assert(forbiddenIn(wrap(`<uses-permission android:name="android.permission.READ_EXTERNAL_STORAGE" android:maxSdkVersion="32"/>`)).length === 1, "a capped request is still a request");
assert(forbiddenIn(wrap(`<uses-permission-sdk-23 android:name="android.permission.READ_MEDIA_IMAGES"/>`)).length === 1, "the sdk-23 form is caught");
assert(forbiddenIn(wrap(`<uses-permission android:name="android.permission.READ_MEDIA_VIDEO" tools:node="remove"/>`)).length === 0, "a removal is not a request");
assert(forbiddenIn(wrap(`<!-- <uses-permission android:name="android.permission.READ_MEDIA_IMAGES"/> -->`)).length === 0, "a comment is not a request");
assert(forbiddenIn(wrap(`<uses-permission android:name="android.permission.CAMERA"/><uses-permission android:name="android.permission.INTERNET"/>`)).length === 0, "other permissions pass");

const here = path.dirname(fileURLToPath(import.meta.url));
const manifest = path.join(here, "../../../apps/mobile/android/app/src/main/AndroidManifest.xml");
const hits = forbiddenIn(fs.readFileSync(manifest, "utf8"));
assert(hits.length === 0, `the app's manifest asks for ${hits.join(", ")}`);

if (failures) {
  console.error(`mediaperm: ${failures} failure(s)`);
  process.exit(1);
}
console.log("mediaperm: ok");
