// keybind.js — recording, labelling and matching a single held key.
//
// Written for push-to-talk, which is the demanding case: the key is HELD, so
// the same binding has to survive a keydown and a keyup that may not look
// alike. Three decisions follow from that, and each one is a bug avoided:
//
//   1. Bindings are on `e.code`, not `e.key`. `code` is the physical key, so a
//      binding made on QWERTY still works on AZERTY, and — the one that
//      actually bites — macOS rewrites `e.key` under Option (Alt+V arrives as
//      "√"), which would make the keyup unrecognisable.
//   2. A keyup is matched on the code ALONE. Holding Ctrl+Space and letting go
//      of Ctrl first means the Space keyup carries ctrlKey:false; comparing
//      modifiers there would strand the key "down" forever.
//   3. A bare modifier (just Alt, just Right Ctrl) is a legitimate binding —
//      it's the classic push-to-talk key — and carries no modifier
//      requirements of its own, since it IS the modifier.

const MODIFIER_CODES = new Set([
  "ControlLeft",
  "ControlRight",
  "ShiftLeft",
  "ShiftRight",
  "AltLeft",
  "AltRight",
  "MetaLeft",
  "MetaRight",
]);

// Codes that don't spell themselves. Everything else is derived by stripping
// the Key/Digit/Numpad prefix, which covers the bulk of the keyboard.
const NAMES = {
  Space: "Space",
  Escape: "Esc",
  Enter: "Enter",
  Tab: "Tab",
  Backspace: "Backspace",
  Backquote: "`",
  Minus: "-",
  Equal: "=",
  BracketLeft: "[",
  BracketRight: "]",
  Backslash: "\\",
  Semicolon: ";",
  Quote: "'",
  Comma: ",",
  Period: ".",
  Slash: "/",
  ArrowUp: "↑",
  ArrowDown: "↓",
  ArrowLeft: "←",
  ArrowRight: "→",
  CapsLock: "Caps Lock",
  ControlLeft: "Left Ctrl",
  ControlRight: "Right Ctrl",
  ShiftLeft: "Left Shift",
  ShiftRight: "Right Shift",
  AltLeft: "Left Alt",
  AltRight: "Right Alt",
  MetaLeft: "Left Meta",
  MetaRight: "Right Meta",
};

// Keys we refuse to bind: Escape is how you cancel the recorder, and Tab is how
// a keyboard user leaves it. Taking either would trap them in the control.
const UNBINDABLE = new Set(["Escape", "Tab"]);

export const isModifierCode = (code) => MODIFIER_CODES.has(code);

// bindFromEvent turns a keydown into a binding, or null if the key can't be
// bound. Recording is the only place modifier state is read off the event.
export function bindFromEvent(e) {
  const code = e?.code;
  if (!code || UNBINDABLE.has(code)) return null;
  if (MODIFIER_CODES.has(code)) return { code, ctrl: false, shift: false, alt: false, meta: false };
  return { code, ctrl: !!e.ctrlKey, shift: !!e.shiftKey, alt: !!e.altKey, meta: !!e.metaKey };
}

export function bindLabel(bind) {
  if (!bind?.code) return "";
  const parts = [];
  if (bind.ctrl) parts.push("Ctrl");
  if (bind.shift) parts.push("Shift");
  if (bind.alt) parts.push("Alt");
  if (bind.meta) parts.push("Meta");
  parts.push(codeLabel(bind.code));
  return parts.join(" + ");
}

function codeLabel(code) {
  if (NAMES[code]) return NAMES[code];
  if (code.startsWith("Key")) return code.slice(3);
  if (code.startsWith("Digit")) return code.slice(5);
  if (code.startsWith("Numpad")) return `Num ${code.slice(6)}`;
  return code;
}

// pressesBind: does this keydown start the binding? Modifiers must match
// exactly, so Shift+V doesn't fire a binding made on plain V.
export function pressesBind(e, bind) {
  if (!bind?.code || e?.code !== bind.code) return false;
  if (MODIFIER_CODES.has(bind.code)) return true;
  return (
    !!e.ctrlKey === !!bind.ctrl &&
    !!e.shiftKey === !!bind.shift &&
    !!e.altKey === !!bind.alt &&
    !!e.metaKey === !!bind.meta
  );
}

// releasesBind: does this keyup end it? Code only — see note 2 up top.
export function releasesBind(e, bind) {
  return !!bind?.code && e?.code === bind.code;
}

// makeRecorder: the state machine behind "press a key to bind it".
//
// It lives here, rather than inline in the settings modal, because the naive
// version — commit on the first keydown — cannot record a combo at all. Holding
// Ctrl+V delivers Ctrl's own keydown first, so every attempt at a combo silently
// binds "Left Ctrl". Keeping this as a testable unit is the point: the bug was
// invisible to a test that synthesised the finished event directly.
//
// A modifier therefore ARMS rather than commits, and what happens next decides:
//   keydown of a real key  -> commit the combo (Ctrl+V)
//   keyup of the modifier  -> commit the modifier alone (the classic PTT key)
//
// Returns { down(e), up(e), armed() }. Both handlers return a binding when one
// is settled, or null; `armed()` reports the modifier state for a live preview.
export function makeRecorder() {
  let held = null;
  return {
    armed: () => held,
    down(e) {
      if (!e?.code || e.repeat) return null; // a held modifier autorepeats
      if (MODIFIER_CODES.has(e.code)) {
        // Keep the whole modifier state, not just this code, so Ctrl *then*
        // Alt previews both rather than only the last key down.
        held = { code: e.code, ctrl: !!e.ctrlKey, shift: !!e.shiftKey, alt: !!e.altKey, meta: !!e.metaKey };
        return null;
      }
      const bind = bindFromEvent(e);
      if (bind) held = null;
      return bind;
    },
    up(e) {
      if (!held || e?.code !== held.code) return null;
      held = null;
      return bindFromEvent(e);
    },
  };
}

// typesCharacter: would this binding also type into a text box? A bare letter,
// digit, punctuation or space would, which means push-to-talk has to stand
// aside while the composer has focus (otherwise the key is either swallowed or
// silently opens your mic mid-sentence). Anything with a modifier, a bare
// modifier, or a function key is safe to hold while typing.
export function typesCharacter(bind) {
  if (!bind?.code) return false;
  if (bind.ctrl || bind.alt || bind.meta) return false;
  if (MODIFIER_CODES.has(bind.code)) return false;
  return !/^(F\d+|Arrow|Home|End|Page|Insert|Delete|Caps|Num[Ll]ock|Scroll|Pause|Context)/.test(
    bind.code,
  );
}
