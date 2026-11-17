// The "+" sheet's photo picker (lib/pickmedia.js): only on the Android app
// whose build has the native method; every pick staged through the
// composer's own attachFile; the private copies always deleted; anything
// refused said in one line.
import { nativePicker, pickAndStage, PICK_MAX } from "./pickmedia.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

// ---- where the tile exists ----
const core = { pickMedia() {}, releaseShared() {} };
assert(nativePicker({ getPlatform: () => "android", Plugins: { ConcordCore: core } }) === core, "the Android app with pickMedia");
assert(nativePicker({ getPlatform: () => "android", Plugins: { ConcordCore: { releaseShared() {} } } }) === null, "an older APK without the method");
assert(nativePicker({ getPlatform: () => "ios", Plugins: { ConcordCore: core } }) === null, "not iOS");
assert(nativePicker(undefined) === null && nativePicker({}) === null, "not a browser or the desktop app");
assert(PICK_MAX === 10, "ten items, the owner's limit");

// ---- one pick ----
function fakeCore(result) {
  const calls = { pick: [], released: [] };
  return {
    calls,
    pickMedia: async (opts) => {
      calls.pick.push(opts);
      return result;
    },
    releaseShared: async ({ paths }) => {
      calls.released.push(...paths);
    },
  };
}
const cap = { convertFileSrc: (p) => `https://localhost/_capacitor_file_${p}` };
const okFetch = (failPath) => async (url) => ({
  ok: !url.endsWith(failPath),
  status: url.endsWith(failPath) ? 404 : 200,
  blob: async () => new Blob([new Uint8Array([1, 2, 3])], { type: "image/jpeg" }),
});

{
  const c = fakeCore({
    files: [
      { path: "/c/share-in/1", name: "a.jpg", type: "image/jpeg", size: 3 },
      { path: "/c/share-in/2", name: "b.mp4", type: "video/mp4", size: 3 },
      { path: "/c/share-in/3", name: "c.jpg", type: "image/jpeg", size: 3 },
    ],
    skipped: [{ name: "huge.mov", reason: "size" }],
  });
  const attached = [];
  const r = await pickAndStage({ core: c, cap, attach: async (f) => attached.push(f), fetchImpl: okFetch("/c/share-in/3") });
  assert(c.calls.pick.length === 1 && c.calls.pick[0].max === 10, "the picker is asked for at most ten");
  assert(attached.length === 2 && attached[0].name === "a.jpg" && attached[0].type === "image/jpeg", "each readable pick is staged as a File with its name and type");
  assert(attached[1].name === "b.mp4" && attached[1].type === "video/mp4", "a video is staged too");
  assert(r.picked === 3 && r.staged === 2, `counts ${JSON.stringify(r)}`);
  assert(/over 25 MB/.test(r.note) && /couldn't be read/.test(r.note), `the note names both refusals: ${r.note}`);
  assert(["/c/share-in/1", "/c/share-in/2", "/c/share-in/3"].every((p) => c.calls.released.includes(p)), "every copy is released, staged or not");
}

{
  // Backing out of the picker: nothing staged, nothing said.
  const c = fakeCore({ files: [] });
  const r = await pickAndStage({ core: c, cap, attach: async () => assert(false, "nothing to stage"), fetchImpl: okFetch("x") });
  assert(r.staged === 0 && r.note === "" && c.calls.released.length === 0, "a cancel is silent");
}

{
  // A composer that throws (over its own limits) still gets the copies deleted.
  const c = fakeCore({ files: [{ path: "/c/share-in/9", name: "z.jpg", type: "image/jpeg", size: 3 }] });
  const r = await pickAndStage({ core: c, cap, attach: async () => { throw new Error("full"); }, fetchImpl: okFetch("x") });
  assert(r.staged === 0 && c.calls.released.includes("/c/share-in/9"), "a refused stage still releases its copy");
  assert(/couldn't be read/.test(r.note), `and says so: ${r.note}`);
}

{
  // A malformed entry from the native side (no type, a path outside media)
  // is refused by the share parser, reported, and its copy released.
  const c = fakeCore({ files: [{ path: "/c/share-in/7", name: "x.pdf", type: "application/pdf", size: 3 }] });
  const r = await pickAndStage({ core: c, cap, attach: async () => assert(false, "a pdf is not staged from the photo picker"), fetchImpl: okFetch("x") });
  assert(r.staged === 0 && /not a picture or video/.test(r.note) && c.calls.released.includes("/c/share-in/7"), `non-media refused: ${r.note}`);
}

if (failures) {
  console.error(`pickmedia: ${failures} failure(s)`);
  process.exit(1);
}
console.log("pickmedia: ok");
