// focusOnMount's deferred focus (`npm test` runs it): it lands last, but not
// over a text field somebody moved to while it waited.
import { focusOnMount } from "./focus.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

const frames = [];
globalThis.requestAnimationFrame = (f) => frames.push(f);
globalThis.cancelAnimationFrame = () => {};
const doc = { activeElement: null };
globalThis.document = doc;

function el(tagName, type = "") {
  const node = { tagName, type, isConnected: true, isContentEditable: false, value: "" };
  node.focus = () => (doc.activeElement = node);
  return node;
}
const settle = async () => {
  await new Promise((r) => setTimeout(r, 0));
  while (frames.length) frames.shift()();
};

const body = el("BODY");
const composer = el("TEXTAREA");
const other = el("INPUT", "text");
const button = el("BUTTON");

// Focus on the page body: the field takes it.
doc.activeElement = body;
let field = el("INPUT", "text");
focusOnMount(field);
await settle();
assert(doc.activeElement === field, "a field mounted over nothing focused did not take focus");

// The composer had focus before the mount (an edit box, a picker's search):
// the new field still takes over.
doc.activeElement = composer;
field = el("INPUT", "text");
focusOnMount(field);
await settle();
assert(doc.activeElement === field, "a field mounted over the focused composer did not take focus");

// A context menu's focus restore to a row (not a text field) after the mount:
// the field still lands last.
doc.activeElement = body;
field = el("INPUT", "text");
focusOnMount(field);
doc.activeElement = button;
await settle();
assert(doc.activeElement === field, "a focus restore to a button won over the mounted field");

// Somebody moved to another text field while the field waited: it stays there.
doc.activeElement = body;
field = el("INPUT", "text");
focusOnMount(field);
doc.activeElement = other;
await settle();
assert(doc.activeElement === other, "the deferred focus took the caret out of a field moved to in the meantime");

// skip declines; update re-arms.
doc.activeElement = body;
field = el("INPUT", "text");
const a = focusOnMount(field, { skip: true });
await settle();
assert(doc.activeElement === body, "skip still focused");
a.update({});
await settle();
assert(doc.activeElement === field, "update without skip did not focus");

if (failures) {
  console.error(`${failures} failure(s)`);
  process.exit(1);
}
console.log("focus: ok");
