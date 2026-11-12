// inputmode.js — focus rings only for people steering with the keyboard.
//
// The browser's own :focus-visible guess paints a ring whenever script moves
// focus and it cannot prove a pointer caused it. Every dialog and settings pane
// moves focus on open (to its first control, so a keyboard user lands inside),
// so opening anything with the mouse painted a 2px accent ring on a random
// Cancel button or switch — which read as an error state. The ring is now
// gated on how the person last touched the app: a key press turns rings on,
// a pointer press turns them off, and app.css hides every :focus-visible
// outline while <html> lacks data-kbd.

// A modifier on its own is not navigation: Shift-click, Ctrl-click and a
// shortcut's first half must not flip the mode. Neither is typing: letters,
// Enter and the caret keys inside a text field are writing, and the unlock
// form, the composer and the palette's query are all fields — counting them
// put a ring back on the next pointer-opened dialog after every message sent.
// The keys that move between controls, and shortcuts, are navigation.
const MODIFIERS = new Set(["Shift", "Control", "Alt", "Meta", "AltGraph", "CapsLock", "Fn", "OS", "Hyper", "Super"]);
const ALWAYS = new Set(["Tab", "F6", "Escape"]);
const LIST_KEYS = new Set(["ArrowUp", "ArrowDown", "PageUp", "PageDown"]);

function isTextField(el) {
  if (!el || el.nodeType !== 1) return false;
  if (el.isContentEditable) return true;
  const tag = el.tagName;
  if (tag === "TEXTAREA") return true;
  if (tag !== "INPUT") return false;
  return !/^(checkbox|radio|range|button|submit|reset|color|file|image)$/i.test(el.type || "");
}

export function keyMeansKeyboard(e) {
  if (!e || !e.key || MODIFIERS.has(e.key)) return false;
  if (ALWAYS.has(e.key) || e.ctrlKey || e.altKey || e.metaKey) return true;
  if (isTextField(e.target)) return LIST_KEYS.has(e.key);
  return true;
}

export function installInputMode(root = document.documentElement, win = window) {
  const on = (e) => {
    if (keyMeansKeyboard(e) && !("kbd" in root.dataset)) root.dataset.kbd = "";
  };
  const off = () => {
    if ("kbd" in root.dataset) delete root.dataset.kbd;
  };
  win.addEventListener("keydown", on, true);
  win.addEventListener("pointerdown", off, true);
  return () => {
    win.removeEventListener("keydown", on, true);
    win.removeEventListener("pointerdown", off, true);
  };
}
