// typetofocus.js — when a stray keystroke may pull the caret into the composer.
//
// "Start typing anywhere and it lands in the message box" is a good default
// for a chat app and a bad one for everything else on the page, and it was
// written as "any key one character long". Two keys broke that rule:
//
//   • Space. It is how a keyboard user presses a focused button. The browser
//     activates a <button> on Space's keyUP, and the composer took focus on
//     the keyDOWN — so the button lost focus before it could fire, nothing
//     opened, and a stray space was typed into the draft instead. Every
//     button in the main view was unpressable with Space.
//   • A letter bound as push-to-talk. The binding refuses to open the mic
//     while a text field has focus (the letter belongs to the message there),
//     and the first press MOVED focus into the text field — so the second and
//     every later press typed the letter instead of talking.
//
// The rule is now: a printable key that nothing else claims, with focus on
// something that does not take keys of its own.

// Surfaces that take letters for themselves: typeahead in a menu or a list,
// a popover's own controls. A letter typed in one stays there.
const KEY_SURFACES = '[role="menu"], [role="listbox"], [role="grid"], [role="tree"], [role="combobox"], [role="dialog"]';
// Controls whose keys are their own (Space toggles them, letters may rove).
// A plain button or link is NOT one for letters: a letter there means "I want
// to type", which is the whole feature.
const OWN_CONTROLS =
  'select, [role="menuitem"], [role="menuitemcheckbox"], [role="menuitemradio"], [role="checkbox"], ' +
  '[role="switch"], [role="radio"], [role="tab"], [role="option"], [role="slider"], [role="spinbutton"]';

// `target` is the keydown's target. `ptt` reports whether the key is the
// push-to-talk binding while a call is up.
export function wantsTypeToFocus(e, target, { ptt = false } = {}) {
  if (e.defaultPrevented || e.isComposing) return false;
  if (e.ctrlKey || e.metaKey || e.altKey) return false;
  if (!e.key || e.key.length !== 1) return false;
  if (ptt) return false;
  if (!target || target.nodeType !== 1) return true;
  const tag = target.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || target.isContentEditable) return false;
  // Space never types into the box from somewhere else: on a control it is
  // that control's key, and on bare page it would start a message with a
  // blank.
  if (e.key === " ") return false;
  if (typeof target.closest === "function" && (target.closest(KEY_SURFACES) || target.closest(OWN_CONTROLS))) return false;
  return true;
}
