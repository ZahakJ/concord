// Focus rings follow the last input: a key turns them on, a pointer press off,
// and a lone modifier changes nothing.
import { keyMeansKeyboard, installInputMode } from "./inputmode.js";

const fail = (msg) => {
  console.error("  FAIL", msg);
  process.exitCode = 1;
};

for (const key of ["Tab", "ArrowDown", "Enter", "Escape", "F6", "a", " "]) {
  if (!keyMeansKeyboard({ key })) fail(`${key} should count as keyboard input`);
}
for (const key of ["Shift", "Control", "Alt", "Meta"]) {
  if (keyMeansKeyboard({ key })) fail(`${key} alone must not count as keyboard input`);
}
// Typing in a field is writing, not navigating; leaving it, or a shortcut, is.
const field = { nodeType: 1, tagName: "TEXTAREA" };
const input = { nodeType: 1, tagName: "INPUT", type: "password" };
const box = { nodeType: 1, tagName: "INPUT", type: "checkbox" };
for (const key of ["a", "Enter", "Backspace", "ArrowLeft", " "]) {
  if (keyMeansKeyboard({ key, target: field })) fail(`typing ${JSON.stringify(key)} in a field is not navigation`);
  if (keyMeansKeyboard({ key, target: input })) fail(`typing ${JSON.stringify(key)} in a password field is not navigation`);
}
for (const key of ["Tab", "Escape", "F6", "ArrowDown"]) {
  if (!keyMeansKeyboard({ key, target: field })) fail(`${key} in a field moves focus or a list`);
}
if (!keyMeansKeyboard({ key: "k", ctrlKey: true, target: field })) fail("a shortcut from a field is navigation");
if (!keyMeansKeyboard({ key: " ", target: box })) fail("Space on a checkbox is a control key");

// The wiring, against a fake window and root.
const handlers = {};
const win = { addEventListener: (t, f) => (handlers[t] = f), removeEventListener: (t) => delete handlers[t] };
const root = { dataset: {} };
const undo = installInputMode(root, win);
if ("kbd" in root.dataset) fail("rings must start off: a dialog opened before any input paints none");
handlers.keydown({ key: "Shift" });
if ("kbd" in root.dataset) fail("a modifier alone must not turn rings on");
handlers.keydown({ key: "Tab" });
if (!("kbd" in root.dataset)) fail("Tab must turn rings on");
handlers.pointerdown({});
if ("kbd" in root.dataset) fail("a pointer press must turn rings off");
undo();
if (handlers.keydown || handlers.pointerdown) fail("uninstall removes both listeners");

if (!process.exitCode) console.log("inputmode.test.mjs: ok");
