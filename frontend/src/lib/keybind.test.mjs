// Zero-dependency test for the held-key binding model (`npm test` runs it).
import {
  bindFromEvent,
  bindLabel,
  pressesBind,
  releasesBind,
  typesCharacter,
  isModifierCode,
  makeRecorder,
} from "./keybind.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};
const eq = (a, b, msg) => assert(JSON.stringify(a) === JSON.stringify(b), `${msg}\n  got ${JSON.stringify(a)}\n  exp ${JSON.stringify(b)}`);

// A keyboard event, as far as this module is concerned.
const ev = (code, mods = {}) => ({
  code,
  ctrlKey: !!mods.ctrl,
  shiftKey: !!mods.shift,
  altKey: !!mods.alt,
  metaKey: !!mods.meta,
});
const B = (code, mods = {}) => ({
  code,
  ctrl: !!mods.ctrl,
  shift: !!mods.shift,
  alt: !!mods.alt,
  meta: !!mods.meta,
});

// ---- recording ----
eq(bindFromEvent(ev("KeyV")), B("KeyV"), "a plain key records with no modifiers");
eq(
  bindFromEvent(ev("Space", { ctrl: true, shift: true })),
  B("Space", { ctrl: true, shift: true }),
  "modifier state is captured at record time",
);
// A bare modifier is the classic push-to-talk key, and it is the key, so it
// must not also demand itself as a modifier (that would never match).
eq(bindFromEvent(ev("AltLeft", { alt: true })), B("AltLeft"), "a bare modifier records with no requirements");
eq(bindFromEvent(ev("Escape")), null, "Escape stays free — it's how you cancel");
eq(bindFromEvent(ev("Tab")), null, "Tab stays free — it's how you leave the control");
eq(bindFromEvent(null), null, "a missing event records nothing");
assert(isModifierCode("ControlRight") && !isModifierCode("KeyV"), "modifier codes are recognised");

// ---- recording, as it actually happens (makeRecorder) ----
// The tests above hand bindFromEvent a finished event. Real keyboards don't:
// they deliver a modifier's own keydown FIRST, so a recorder that commits on
// the first keydown can only ever bind "Left Ctrl". That bug shipped once, past
// a green test suite, because nothing exercised the sequence.
{
  // Ctrl+V: Ctrl arrives alone, then V. The combo must survive.
  const r = makeRecorder();
  eq(r.down(ev("ControlLeft", { ctrl: true })), null, "a modifier alone does not commit");
  assert(r.armed()?.code === "ControlLeft", "...it arms the recorder instead");
  eq(r.down(ev("KeyV", { ctrl: true })), B("KeyV", { ctrl: true }), "the real key completes Ctrl+V");
  eq(r.armed(), null, "committing disarms");
}
{
  // Two modifiers deep, and the preview must know about both.
  const r = makeRecorder();
  r.down(ev("ControlLeft", { ctrl: true }));
  r.down(ev("AltLeft", { ctrl: true, alt: true }));
  assert(r.armed()?.ctrl && r.armed()?.alt, "the armed state keeps every modifier down, not just the last");
  eq(
    r.down(ev("KeyQ", { ctrl: true, alt: true })),
    B("KeyQ", { ctrl: true, alt: true }),
    "Ctrl+Alt+Q records as all three",
  );
}
{
  // Press and release Ctrl with nothing else: that's the classic PTT key.
  const r = makeRecorder();
  r.down(ev("ControlLeft", { ctrl: true }));
  eq(r.up(ev("ControlLeft")), B("ControlLeft"), "letting a lone modifier go binds the modifier itself");
  eq(r.armed(), null, "and disarms");
}
{
  // Holding a modifier autorepeats its keydown; that isn't a decision.
  const r = makeRecorder();
  r.down(ev("AltLeft", { alt: true }));
  eq(r.down({ ...ev("AltLeft", { alt: true }), repeat: true }), null, "autorepeat commits nothing");
  assert(r.armed()?.code === "AltLeft", "and leaves it armed");
}
{
  // A keyup for a key we never armed on must not commit anything.
  const r = makeRecorder();
  eq(r.up(ev("ControlLeft")), null, "a release with nothing armed binds nothing");
  r.down(ev("ControlLeft", { ctrl: true }));
  eq(r.up(ev("ShiftLeft")), null, "releasing a different key does not commit the armed one");
}
{
  // Plain keys still record on the first keydown, as before.
  const r = makeRecorder();
  eq(r.down(ev("F9")), B("F9"), "a function key commits straight away");
  eq(r.down(ev("Escape")), null, "Escape still records nothing");
}

// ---- labels ----
eq(bindLabel(B("KeyV")), "V", "letters lose the Key prefix");
eq(bindLabel(B("Digit4")), "4", "digits lose the Digit prefix");
eq(bindLabel(B("Numpad7")), "Num 7", "numpad keys say so");
eq(bindLabel(B("Backquote")), "`", "punctuation spells itself");
eq(bindLabel(B("F13")), "F13", "function keys pass through");
eq(bindLabel(B("ControlRight")), "Right Ctrl", "modifiers name their side");
eq(bindLabel(B("KeyQ", { ctrl: true, alt: true })), "Ctrl + Alt + Q", "modifiers prefix in a fixed order");
eq(bindLabel(null), "", "no binding, no label");

// ---- press ----
assert(pressesBind(ev("KeyV"), B("KeyV")), "the bound key presses it");
assert(!pressesBind(ev("KeyB"), B("KeyV")), "another key does not");
assert(
  !pressesBind(ev("KeyV", { shift: true }), B("KeyV")),
  "Shift+V must not fire a binding made on plain V",
);
assert(
  pressesBind(ev("KeyV", { ctrl: true }), B("KeyV", { ctrl: true })),
  "a combo presses when its modifiers are held",
);
assert(
  !pressesBind(ev("KeyV"), B("KeyV", { ctrl: true })),
  "a combo does not press without its modifiers",
);
// Ctrl arrives with ctrlKey already true; a bare-modifier binding must ignore
// modifier state entirely or it could never be pressed.
assert(
  pressesBind(ev("ControlLeft", { ctrl: true }), B("ControlLeft")),
  "a bare modifier presses despite its own modifier flag",
);
assert(!pressesBind(ev("KeyV"), null), "no binding is never pressed");

// ---- release ----
// The regression this exists for: let go of Ctrl before Space and the Space
// keyup carries ctrlKey:false. Comparing modifiers there strands the mic open.
assert(
  releasesBind(ev("Space"), B("Space", { ctrl: true })),
  "release matches on the key alone, whatever the modifiers now say",
);
assert(!releasesBind(ev("ControlLeft", { ctrl: true }), B("Space", { ctrl: true })), "releasing only the modifier does not end the hold");
assert(!releasesBind(ev("KeyB"), B("KeyV")), "another key does not release");
assert(!releasesBind(ev("KeyV"), null), "no binding is never released");

// ---- would it also type? ----
assert(typesCharacter(B("KeyV")), "a bare letter types");
assert(typesCharacter(B("Space")), "space types");
assert(typesCharacter(B("Numpad7")), "a numpad digit types");
assert(typesCharacter(B("Slash")), "punctuation types");
assert(!typesCharacter(B("KeyV", { ctrl: true })), "a combo does not type");
assert(!typesCharacter(B("KeyV", { alt: true })), "Alt+key does not type");
assert(!typesCharacter(B("AltRight")), "a bare modifier does not type");
assert(!typesCharacter(B("F13")), "a function key does not type");
assert(!typesCharacter(B("ArrowUp")), "an arrow does not type");
assert(!typesCharacter(null), "no binding does not type");

// Shift+letter DOES type (an uppercase one), so it must stand aside in the
// composer just like the bare letter it shifts.
assert(typesCharacter(B("KeyV", { shift: true })), "Shift+letter still types");

if (failures) {
  console.error(`\n${failures} keybind test(s) failed`);
  process.exit(1);
}
console.log("keybind.js: all tests passed");
