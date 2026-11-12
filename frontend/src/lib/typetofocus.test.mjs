// Which keystrokes may pull the caret into the composer (`npm test` runs it).
import { wantsTypeToFocus } from "./typetofocus.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

// Just enough of an element for closest(): a tag, a role, a parent.
function el(tagName, role = "", parent = null, extra = {}) {
  const node = { nodeType: 1, tagName, role, parentElement: parent, isContentEditable: false, ...extra };
  const matchOne = (n, sel) => {
    sel = sel.trim();
    const m = sel.match(/^\[role="([^"]+)"\]$/);
    if (m) return n.role === m[1];
    return n.tagName.toLowerCase() === sel.toLowerCase();
  };
  node.closest = (sels) => {
    for (let n = node; n; n = n.parentElement) if (sels.split(",").some((s) => matchOne(n, s))) return n;
    return null;
  };
  return node;
}
const key = (k, mods = {}) => ({ key: k, defaultPrevented: false, ...mods });

const body = el("BODY");
const feed = el("DIV", "log", body);
const row = el("DIV", "article", feed);
const button = el("BUTTON", "", body);
const menu = el("DIV", "menu", body);
const menuItem = el("BUTTON", "menuitem", menu);
const popover = el("DIV", "dialog", body);
const popButton = el("BUTTON", "", popover);
const sw = el("BUTTON", "switch", body);
const input = el("INPUT", "", body);

// The feature itself.
assert(wantsTypeToFocus(key("h"), body), "a letter on the bare page goes to the composer");
assert(wantsTypeToFocus(key("h"), row), "a letter on a message row goes to the composer");
assert(wantsTypeToFocus(key("h"), button), "a letter on a plain button goes to the composer");

// Space is a button's key, never the start of a message.
assert(!wantsTypeToFocus(key(" "), button), "Space on a focused button activates it");
assert(!wantsTypeToFocus(key(" "), sw), "Space on a switch toggles it");
assert(!wantsTypeToFocus(key(" "), body), "Space on the bare page does not start a message with a blank");

// Push-to-talk on a letter.
assert(!wantsTypeToFocus(key("v"), body, { ptt: true }), "the push-to-talk key is not typing");
assert(!wantsTypeToFocus(key("v", { defaultPrevented: true }), body), "a key another handler consumed is left alone");

// Surfaces with their own use for letters.
assert(!wantsTypeToFocus(key("d"), menuItem), "typeahead in a menu stays in the menu");
assert(!wantsTypeToFocus(key("d"), popButton), "a letter inside a popover stays there");
assert(!wantsTypeToFocus(key("x"), input), "a field keeps its own keys");

// Chords and non-printables.
assert(!wantsTypeToFocus(key("k", { ctrlKey: true }), body), "chords are not typing");
assert(!wantsTypeToFocus(key("Enter"), body), "Enter is not a character");
assert(!wantsTypeToFocus(key("a", { isComposing: true }), body), "an IME composition is left alone");

if (failures) {
  console.error(`${failures} typetofocus test(s) failed`);
  process.exit(1);
}
console.log("typetofocus: all tests passed");
