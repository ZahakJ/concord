// The Join dialog's failure copy, per cause (`npm test`). The raw core string
// must never be the message, and an offline inviter must not be blamed on the
// code.
import { joinFailure } from "./joinerror.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

const banned = joinFailure(new Error("app: invite rejected: you are banned from this guild"));
assert(/banned/.test(banned.title) && !/fresh|new code/.test(banned.title), "ban says banned, no fresh-code advice");
assert(!banned.field, "a ban is not the field's fault");

const removed = joinFailure("app: invite rejected: you were removed from this guild");
assert(/removed/.test(removed.title), "removal says removed");

const bad = joinFailure("bad invite code");
assert(bad.field && /whole invite code/.test(bad.title), "a malformed code marks the field");

const offline = joinFailure("Couldn't reach them — they're probably offline, or on a network this device can't get to from here.");
assert(/code is fine/.test(offline.title) && /online/.test(offline.detail), "offline inviter: the code is fine, they need to be online");
const dial = joinFailure("net: open invite stream: connection failed");
assert(dial.title === offline.title, "a raw dial failure reads as offline");

const gone = joinFailure("app: invite rejected: unknown guild");
assert(/isn't in the guild/.test(gone.title), "inviter who left");

const raw = "app: invite rejected: something nobody planned for";
const other = joinFailure(raw);
assert(!other.title.includes("app:") && !other.detail.includes("app:"), "no raw core string");

if (failures) {
  console.error(`\n${failures} joinerror test(s) failed`);
  process.exit(1);
}
console.log("joinerror.test.mjs: all checks passed");
