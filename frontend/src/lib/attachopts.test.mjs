// Tests the one rule that keeps new image posts readable on older peers: an
// image the user has not touched must send options that produce a v1 attachment
// token, because a client predating v2 renders a v2 token as ~190 characters of
// raw text instead of the picture.
//
// This is a regression test with a real history: seeding the staged record's
// `name` from the OS file name looked harmless and silently opted EVERY
// picked/dropped/pasted image into v2.
import { stagedImage, emitsLegacyToken } from "./attachopts.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

// --- the contract ------------------------------------------------------------

const plain = stagedImage({ id: "a", dataUrl: "data:image/png;base64,AA", w: 4, h: 4 });
assert(emitsLegacyToken(plain), "an image staged with no file name must emit v1");

// The regression itself: a file name is always present in practice (the file
// picker, drag-and-drop and Chromium's clipboard all supply one), so this is the
// ordinary case, not an edge case.
const fromDisk = stagedImage({
  id: "b",
  dataUrl: "data:image/png;base64,AA",
  w: 4,
  h: 4,
  fileName: "Screenshot 2026-07-30.png",
});
assert(
  emitsLegacyToken(fromDisk),
  "an unedited image picked from disk must still emit v1 — a prefilled name forces v2",
);
assert(fromDisk.name === "", "the OS file name must not land in `name`");
assert(
  fromDisk.origName === "Screenshot 2026-07-30.png",
  "the OS file name must be kept as `origName` for the rename placeholder",
);
assert(fromDisk.spoiler === false && fromDisk.desc === "", "no option may default to set");
assert(fromDisk.isImage === true, "staged images must be marked as images");

// --- and the other direction: opting in must still reach v2 ------------------

for (const [label, patch] of [
  ["spoiler", { spoiler: true }],
  ["a user-typed name", { name: "cat.png" }],
  ["a description", { desc: "my cat" }],
]) {
  assert(
    !emitsLegacyToken({ ...plain, ...patch }),
    `${label} must opt into v2 — otherwise the option is silently dropped`,
  );
}

// Mirrors internal/app/attach.go: all three together is still just v2.
assert(
  !emitsLegacyToken({ ...plain, spoiler: true, name: "x", desc: "y" }),
  "every option set at once must still be v2",
);

if (failures) {
  console.error(`attachopts.test.mjs: ${failures} failure(s)`);
  process.exit(1);
}
console.log("attachopts.test.mjs: ok");
