// The attach menu's "Recent" strip (lib/recent.js): which gallery items it
// offers, in which order, how many, and how a picked one becomes the File the
// composer stages exactly as if it had been picked from disk.
import { recentStrip, recentFileName, recentMime, recentRequest, dataUrlToFile, RECENT_MAX, recentAllowed, stillOffered } from "./recent.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};
const eq = (a, b, msg) => assert(JSON.stringify(a) === JSON.stringify(b), `${msg}: got ${JSON.stringify(a)}, want ${JSON.stringify(b)}`);

const blob = (n) => n.toString(16).padStart(64, "0");
const keys = "K".repeat(75);
let seq = 0;
const item = (o = {}) => ({
  messageId: `m${++seq}`,
  channelId: "c",
  ord: 0,
  kind: 1,
  blobId: blob(seq),
  keys,
  subtype: "png",
  w: 40,
  h: 30,
  sent: new Date(Date.UTC(2026, 10, 19, 12, 0, 60 - seq)).toISOString(),
  ...o,
});

const now = Date.UTC(2026, 10, 19, 12) / 1000;

// ---- what it offers --------------------------------------------------------
{
  seq = 0;
  const a = item();
  const expired = item({ expires: now - 1 });
  const later = item({ expires: now + 60 });
  const spoiler = item({ spoiler: true });
  const voice = item({ kind: 3, mime: "audio/ogg", name: "voice.ogg" });
  const copy = item({ unverified: true });
  const guest = item({ guest: true });
  const dup = item({ blobId: a.blobId, messageId: "forwarded" });
  const file = item({ kind: 3, mime: "application/pdf", name: "notes.pdf", subtype: "" });
  const got = recentStrip([a, expired, later, spoiler, voice, copy, guest, dup, file], now).map((it) => it.messageId);
  eq(got, [a.messageId, later.messageId, file.messageId], "expired, spoilered, voice, unsigned, guest and repeated blobs are left out");
}
{
  seq = 0;
  const many = Array.from({ length: 20 }, () => item());
  const got = recentStrip(many, now);
  eq(got.length, RECENT_MAX, "capped at twelve");
  eq(RECENT_MAX, 12, "the cap is twelve");
  eq(got[0].messageId, many[0].messageId, "newest first, the order the core gave");
  eq(recentStrip([], now), [], "nothing recent: an empty strip (the menu hides it)");
  eq(recentStrip(null, now), [], "no answer: an empty strip");
}
{
  // Items with no blob id or key are not something the page could fetch.
  seq = 0;
  eq(recentStrip([item({ blobId: "" }), item({ keys: "" })], now), [], "items without a blob or key are dropped");
}

// ---- how a pick is fetched and named ---------------------------------------
{
  seq = 0;
  const p = item({ subtype: "jpeg" });
  eq(recentRequest(p), { method: "fetchAttachment", args: ["c", p.blobId, keys, "jpeg"] }, "a photo is fetched as a picture");
  const v = item({ kind: 2, subtype: "", mime: "video/mp4", name: "clip.mp4" });
  eq(recentRequest(v), { method: "fetchFile", args: ["c", v.blobId, keys, "video/mp4"] }, "a video is fetched as a file");
  const f = item({ kind: 3, subtype: "", mime: "", name: "x" });
  eq(recentRequest(f).args[3], "application/octet-stream", "a file with no type is fetched as bytes");

  eq(recentMime(p), "image/jpeg", "photo mime from its subtype");
  eq(recentMime(v), "video/mp4", "file mime from the token");
  eq(recentFileName(p), "photo.jpg", "an unnamed photo gets a plain name");
  eq(recentFileName(item({ name: "cat.png" })), "cat.png", "a named photo keeps its name");
  eq(recentFileName(v), "clip.mp4", "a file keeps its name");
  eq(recentFileName(item({ kind: 3, subtype: "", name: "" })), "file", "an unnamed file is 'file'");
}

// ---- when the strip may show at all ----------------------------------------
{
  assert(recentAllowed({}), "no call, no share: shown");
  assert(recentAllowed({ callMembers: [{ fingerprint: "ab12" }, { fingerprint: "cd34" }] }), "a call among members: shown");
  assert(!recentAllowed({ sharing: true }), "sharing this screen: hidden");
  assert(!recentAllowed({ callMembers: [{ fingerprint: "ab12" }, { fingerprint: "guest:Dana" }] }), "a guest in the call: hidden");
}

// ---- a pick is checked against a fresh listing -----------------------------
{
  seq = 0;
  const p = item();
  const q = item();
  assert(stillOffered([q, p], p), "still listed: may be picked");
  assert(!stillOffered([q], p), "gone (deleted, hidden, expired): may not be picked");
  assert(!stillOffered([{ ...p, blobId: blob(99) }], p), "the slot now names another blob: may not be picked");
  assert(!stillOffered(null, p), "no answer: may not be picked");
}

// ---- the File the composer stages ------------------------------------------
{
  const bytes = new Uint8Array([137, 80, 78, 71, 1, 2, 3]);
  const url = "data:image/png;base64," + Buffer.from(bytes).toString("base64");
  const f = dataUrlToFile(url, "photo.png", "image/png");
  assert(f instanceof File, "a File, like a picked one");
  eq(f.name, "photo.png", "the File carries the name");
  eq(f.type, "image/png", "the File carries the type");
  eq(f.size, bytes.length, "the File carries the bytes");
  const back = new Uint8Array(await f.arrayBuffer());
  eq([...back], [...bytes], "the bytes are the plaintext, unchanged");
  let threw = false;
  try {
    dataUrlToFile("not a data url", "x", "image/png");
  } catch {
    threw = true;
  }
  assert(threw, "anything but a base64 data URL is refused");
}

if (failures) {
  console.error(`${failures} failure(s)`);
  process.exit(1);
}
console.log("recent ok");
