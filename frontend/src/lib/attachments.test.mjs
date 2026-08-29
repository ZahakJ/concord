// What a reader is told when an attachment will not load.
//
// A blob only exists on the peers that have held its bytes, so a fetch that
// finds nothing is not a broken link — it is "everyone holding this is offline
// right now". The UI used to render that as a spinner that never stopped, and
// then as "no one online has this image yet", which is true but leaves the
// reader with no idea what they are missing or whether waiting helps. These
// tests pin the two decisions that replaced it: which sentence gets used, and
// which presence change is worth refetching on.
import {
  unavailableNote,
  arrived,
  worthRetrying,
  RETRY_COOLDOWN_MS,
  fmtBytes,
  parseAttachTokens,
  parseFileTokens,
  placeholderName,
} from "./attachments.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};
const eq = (got, want, msg) => assert(got === want, `${msg}\n   got:  ${got}\n   want: ${want}`);

const NOT_FOUND = new Error("app: attachment not found on any reachable peer");

// --- naming the person, and promising the right thing ------------------------

eq(
  unavailableNote(NOT_FOUND, { name: "Amina" }),
  "Amina isn't reachable right now — it'll load when they're back",
  "the sender is named, and waiting is promised to be enough",
);

// The roster keeps a hard-killed peer marked online for the best part of a
// minute (its connections are never closed), so a note that leans on that flag
// prints "Amina is online, try again" about a process that has already exited.
// The note must say the same true thing whatever the roster currently claims.
eq(
  unavailableNote(NOT_FOUND, { name: "Amina", online: true }),
  unavailableNote(NOT_FOUND, { name: "Amina", online: false }),
  "the note must not depend on the roster's online flag, which lags a kill",
);

// No roster row (a DM, a guild we're no longer in) means we cannot name them.
eq(
  unavailableNote(NOT_FOUND, { name: "" }),
  "nobody who has it is reachable right now — it'll load when they're back",
  "an unidentifiable sender gets the general sentence",
);

// Our OWN message whose blob we no longer hold: nobody is coming to fix that,
// and blaming an absent peer would be nonsense.
assert(
  /this device/.test(unavailableNote(NOT_FOUND, { name: "You", self: true })),
  "a blob missing from our own store is described as ours",
);

// Any other failure — a bad key, a decode error — is not a reachability problem
// and must not be dressed up as one.
assert(
  !/reachable/.test(unavailableNote(new Error("app: attachment key invalid"), { name: "Amina" })),
  "a non-'not found' failure must not claim the sender is unreachable",
);

// --- when a roster refresh is worth another fetch ----------------------------

const set = (...fps) => new Set(fps);

assert(arrived(set("a"), set("a", "b")), "somebody joining is an arrival");
assert(!arrived(set("a", "b"), set("a")), "somebody leaving is not an arrival");
assert(!arrived(set("a", "b"), set("a", "b")), "an unchanged roster is not an arrival");
assert(arrived(set("a"), set("b")), "a swap contains an arrival");
assert(!arrived(null, set("a", "b")), "the first observation is never an arrival");

// An arrival is worth a fetch immediately, cooldown or not.
assert(worthRetrying(set("a"), set("a", "b"), 0), "an arrival retries at once");

// THE CASE THAT DROVE THIS. A peer killed rather than closed keeps its
// connections, so it keeps its "online" row for up to a minute. Kill it and
// restart it inside that window and the roster never once shows it leaving —
// the set is identical before and after, and an arrival-only rule would leave
// the picture broken forever. The cooldown is what rescues it.
assert(
  worthRetrying(set("a", "b"), set("a", "b"), RETRY_COOLDOWN_MS),
  "an unchanged roster still retries once the cooldown has passed",
);
assert(
  !worthRetrying(set("a", "b"), set("a", "b"), RETRY_COOLDOWN_MS - 1),
  "an unchanged roster does not retry before the cooldown",
);
// The first observation is not a change, however long the page has been open.
assert(!worthRetrying(null, set("a"), 10 * RETRY_COOLDOWN_MS), "the first observation never retries");

// --- sizes -------------------------------------------------------------------

eq(fmtBytes(0), "", "an unknown size renders as nothing, not '0 B'");
eq(fmtBytes(512), "512 B", "bytes");
eq(fmtBytes(5008), "5 KB", "kilobytes");
eq(fmtBytes(26214400), "25.0 MB", "the file cap renders as 25.0 MB");

// --- the placeholder has something concrete to say about the missing file ----

// A v2 image token carries the sender's file name and both tokens carry the
// dimensions, so the error box is never reduced to "something didn't load".
const v2 = parseAttachTokens(
  `![image](concord://attach/v2/${"a".repeat(64)}/${"A".repeat(75)}/png/600x400/0/${Buffer.from("sunset-cliff.png").toString("base64url")}/)`,
)[0];
eq(v2.name, "sunset-cliff.png", "a v2 token yields the file name for the placeholder");
eq(`${v2.w} × ${v2.h}`, "600 × 400", "a token yields dimensions for the placeholder");

const file = parseFileTokens(
  `[file](concord://file/v1/${"b".repeat(64)}/${"A".repeat(75)}/5008/${Buffer.from("text/markdown").toString("base64url")}/${Buffer.from("meeting-notes.md").toString("base64url")})`,
)[0];
eq(file.name, "meeting-notes.md", "a file token yields the file name");
eq(fmtBytes(file.size), "5 KB", "a file token yields a real size");

// ...but a spoiler must stay a spoiler even when it fails. A placeholder that
// prints THE-KILLER-IS-THE-BUTLER.png has leaked the thing the sender covered.
eq(placeholderName({ name: "sunset-cliff.png" }), "sunset-cliff.png", "an ordinary image is named");
eq(
  placeholderName({ name: "THE-KILLER-IS-THE-BUTLER.png", spoiler: true }),
  "Spoiler",
  "a spoiler that will not load must not print the name that gives it away",
);
eq(placeholderName({ name: "" }), "", "a v1 token has no name and gets no line");

if (failures) {
  console.error(`${failures} failure(s)`);
  process.exit(1);
}
console.log("attachments.test.mjs: ok");
