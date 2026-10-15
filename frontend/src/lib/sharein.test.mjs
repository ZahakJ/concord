// Tests the page's reading of an Android "shareIn" event: what gets staged,
// what is reported as not attached, and that nothing malformed slips through
// as a file the composer would then try to fetch.
import { parseShare, fileSrc, skippedNote, SHARE_MAX_BYTES } from "./sharein.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};
const eq = (a, b, msg) => assert(JSON.stringify(a) === JSON.stringify(b), `${msg}: got ${JSON.stringify(a)}, want ${JSON.stringify(b)}`);

// --- the text-only share keeps working exactly as before ---------------------
eq(parseShare({ text: "  https://example.com  " }), { text: "https://example.com", files: [], skipped: [] }, "text share");
eq(parseShare(null), { text: "", files: [], skipped: [] }, "null event");
eq(parseShare("nope"), { text: "", files: [], skipped: [] }, "non-object event");

// --- pictures and videos are staged -------------------------------------------
{
  const s = parseShare({
    files: [
      { path: "/c/share-in/a", name: "IMG_1.jpg", type: "image/jpeg", size: 1000 },
      { path: "/c/share-in/b", name: "clip.mp4", type: "video/mp4", size: 2000 },
    ],
  });
  eq(s.files.map((f) => f.name), ["IMG_1.jpg", "clip.mp4"], "both media staged");
  eq(s.skipped, [], "nothing skipped");
}

// --- a caption shared alongside a picture still lands in the draft ------------
eq(parseShare({ text: "look", files: [{ path: "/p", name: "x.png", type: "image/png", size: 1 }] }).text, "look", "caption kept");

// --- anything the composer cannot stage is reported, never silently lost ------
{
  const s = parseShare({
    files: [
      { path: "/c/doc", name: "doc.pdf", type: "application/pdf", size: 10 },
      { path: "/c/big", name: "big.mp4", type: "video/mp4", size: SHARE_MAX_BYTES + 1 },
      { name: "nopath.png", type: "image/png", size: 1 }, // malformed: no path — dropped
      null,
    ],
    skipped: [{ name: "far.mov", reason: "size" }, { reason: "count" }, "junk"],
  });
  eq(s.files, [], "nothing stageable");
  eq(
    s.skipped.map((x) => x.reason),
    ["type", "size", "size", "count"],
    "skips carry their reasons",
  );
  // The page-side refusals carry their copy's path, so it can be released.
  eq(s.skipped.filter((x) => x.path).map((x) => x.path), ["/c/doc", "/c/big"], "refused copies are releasable");
}

// --- exactly at the limit is allowed -------------------------------------------
eq(parseShare({ files: [{ path: "/p", name: "a.png", type: "image/png", size: SHARE_MAX_BYTES }] }).files.length, 1, "25 MB exactly fits");

// --- a missing name gets one -------------------------------------------------
eq(parseShare({ files: [{ path: "/p", name: "  ", type: "image/png", size: 1 }] }).files[0].name, "shared", "blank name");

// --- the local file route ----------------------------------------------------
eq(fileSrc("/data/x", { convertFileSrc: (p) => `cap:${p}` }), "cap:/data/x", "uses Capacitor's convertFileSrc");
assert(fileSrc("/data/x", null).endsWith("/_capacitor_file_/data/x"), "falls back to the same route by hand");

// --- the one-line note -------------------------------------------------------
eq(skippedNote([]), "", "no note when nothing skipped");
eq(skippedNote([{ reason: "size" }]), "Not attached: 1 file over 25 MB", "one too large");
eq(
  skippedNote([{ reason: "size" }, { reason: "size" }, { reason: "type" }, { reason: "count" }, { reason: "weird" }]),
  "Not attached: 2 files over 25 MB, 1 file not a picture or video, 1 more past the 10-item limit, 1 file couldn't be read",
  "mixed reasons",
);

if (failures) {
  console.error(`${failures} sharein test(s) failed`);
  process.exit(1);
}
console.log("sharein: ok");
