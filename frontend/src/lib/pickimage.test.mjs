// pickimage.test.mjs — how a picker turns the picked file into a data URI.
//
// Run: node src/lib/pickimage.test.mjs
//
// Node has no FileReader and no canvas, so both are passed in as recorders:
// the test checks which one a file goes to and what it is handed.
import assert from "node:assert/strict";
import { pickedPicture } from "./pickimage.js";

const ascii = (s) => Array.from(s, (c) => c.charCodeAt(0));
const AVIF = new Uint8Array([0, 0, 0, 28, ...ascii("ftypavif"), 0, 0, 0, 0]);
const JPEG = new Uint8Array([0xff, 0xd8, 0xff, 0xe0, 0, 16]);
const GIF = new Uint8Array([...ascii("GIF89a"), 1, 0, 1, 0, 0, 0]);

function recorders() {
  const calls = { read: [], redraw: [] };
  return {
    calls,
    deps: {
      read: async (blob) => (calls.read.push(blob), "read"),
      redraw: async (file, maxChars) => (calls.redraw.push({ file, maxChars }), "redrawn"),
    },
  };
}

let n = 0;

{
  // An AVIF saved as .jpg is drawn again on the canvas, which the core takes,
  // rather than read raw and refused.
  const { calls, deps } = recorders();
  const avif = new File([AVIF], "photo.jpg", { type: "image/jpeg" });
  assert.equal(await pickedPicture(avif, 1234, deps), "redrawn", "an AVIF named .jpg goes to the canvas");
  assert.equal(calls.read.length, 0, "an AVIF is not read raw");
  assert.equal(calls.redraw[0].file, avif, "the canvas gets the picked file");
  assert.equal(calls.redraw[0].maxChars, 1234, "the canvas gets the caller's limit");
  n++;
}

{
  // A JPEG a share source typed image/jpg is read raw, under image/jpeg.
  const { calls, deps } = recorders();
  assert.equal(await pickedPicture(new File([JPEG], "photo.jpg", { type: "image/jpg" }), 1234, deps), "read");
  assert.equal(calls.redraw.length, 0, "a JPEG is not redrawn");
  assert.equal(calls.read[0].type, "image/jpeg", "a JPEG typed image/jpg is read as image/jpeg");
  assert.deepEqual(new Uint8Array(await calls.read[0].arrayBuffer()), JPEG, "its bytes are unchanged");
  n++;
}

{
  // A GIF is read raw, so it keeps animating.
  const { calls, deps } = recorders();
  const gif = new File([GIF], "dance.gif", { type: "image/gif" });
  assert.equal(await pickedPicture(gif, 1234, deps), "read");
  assert.equal(calls.read[0], gif, "a correctly typed GIF is read as it is");
  assert.equal(calls.redraw.length, 0, "a GIF is not redrawn");
  n++;
}

console.log(`pickimage.js: all ${n} tests passed`);
