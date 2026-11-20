// Tests the one rule that keeps new image posts readable on older peers: an
// image the user has not touched must send options that produce a v1 attachment
// token, because a client predating v2 renders a v2 token as ~190 characters of
// raw text instead of the picture.
//
// This is a regression test with a real history: seeding the staged record's
// `name` from the OS file name looked harmless and silently opted EVERY
// picked/dropped/pasted image into v2.
import { stagedImage, emitsLegacyToken, sentAsIs, fileHead, FILE_HEAD_CHARS, AS_IS_NOTE } from "./attachopts.js";

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

// --- sent as they are: the line shown before the send -----------------------

// The core decides from the bytes (app.FileGoesAsIs); the page sends it the
// start of the file and shows the line only when the answer is yes.
{
  const big = "data:video/mp4;base64," + "A".repeat(FILE_HEAD_CHARS + 400);
  assert(FILE_HEAD_CHARS % 4 === 0, "the head is whole base64 groups");
  assert(fileHead(big).length === FILE_HEAD_CHARS, "only the start of a big file is asked about");
  assert(fileHead("data:application/pdf;base64,JVBERi0=") === "JVBERi0=", "a small file is asked about whole");
  assert(fileHead("data:text/plain,hello") === null, "a data URL that is not base64 has no head");

  const asked = [];
  const core = (answer) => async (head) => {
    asked.push(head);
    return answer;
  };
  assert((await sentAsIs("data:application/pdf;base64,JVBERi0=", core(true))) === true, "the core says a document goes as it is");
  assert(asked.at(-1) === "JVBERi0=", "the core is asked with the file's head");
  assert((await sentAsIs("data:video/mp4;base64,AAAAGGZ0eXA=", core(false))) === false, "a video the core cleans gets no line");
  assert((await sentAsIs("data:image/heic;base64,AAAAGGZ0eXA=", core(false))) === false, "nor a HEIC photo");
  const failing = async () => {
    throw new Error("no core");
  };
  assert((await sentAsIs("data:application/pdf;base64,JVBERi0=", failing)) === true, "when the core cannot answer, the line shows");
  assert((await sentAsIs("data:text/plain,hello", core(false))) === true, "and when there is nothing to ask with");
}
assert(
  AS_IS_NOTE ===
    "Files other than pictures, videos and M4A audio are sent as they are, including any details stored in them, such as a location or an author's name.",
  "the wording",
);

if (failures) {
  console.error(`attachopts.test.mjs: ${failures} failure(s)`);
  process.exit(1);
}
console.log("attachopts.test.mjs: ok");
