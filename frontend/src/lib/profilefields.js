// profilefields.js — the rules the profile editor shares with the core.
//
// Each of these used to disagree with internal/app between the two ends: the
// editor counted the bio in characters and the core cut it in bytes, so 600
// Arabic characters were accepted, a "Profile updated" toast said yes, and 323
// were kept. A name of three zero-width spaces passed the editor's trim() and
// left the account nameless everywhere. One rule each, the core's, mirrored.

// The core's maxBioBytes: the bio travels in every profile broadcast, and the
// wire counts bytes. internal/app/service.go refuses — never cuts — past it.
export const BIO_MAX_BYTES = 600;

const enc = new TextEncoder();
export const utf8Bytes = (s) => enc.encode(String(s ?? "")).length;

// bioRoom: how much of the budget a bio uses, as the core will count it.
export function bioRoom(bio) {
  const used = utf8Bytes(String(bio ?? "").trim());
  return { used, max: BIO_MAX_BYTES, left: BIO_MAX_BYTES - used, over: used > BIO_MAX_BYTES };
}

// Mirrors internal/app/names.go invisibleRune: whitespace, format controls
// (zero-width characters, bidi controls, soft hyphen), lone combining marks,
// and the blank letters used to look like nothing.
const INVISIBLE = /^[\s\p{Cf}\p{Mn}\p{Me}\p{Cc}ᅟᅠㅤﾠ⠀]$/u;

// hasVisible: at least one character a person can see.
export function hasVisible(s) {
  for (const ch of String(s ?? "")) if (!INVISIBLE.test(ch)) return true;
  return false;
}

// profileDraft holds unsaved profile edits across a pane switch. Settings is a
// rail of instant-apply panes with this one explicit-Save pane among them, and
// moving to Appearance and back silently threw the edits away. Module-level,
// so it lives exactly as long as this page does.
export const profileDraft = { value: null };
