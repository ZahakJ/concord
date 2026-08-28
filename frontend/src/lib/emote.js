// emote.js — recognising a /me line without inventing a wire field.
//
// `/me waves at everyone` used to expand to `*waves at everyone*`, which is an
// ordinary italic message: it grouped under the previous one like any other, so
// the actor — the entire point of the grammar — was often not on screen at all.
// In IRC, and in every client that inherited the convention, an emote reads
// "* Amina Sadiq waves at everyone".
//
// The fix stays entirely in the text. The command now expands to
// `*<display name> <action>*`, so the actor travels with the message and every
// build that already exists — including one that never heard of this — renders
// it correctly and completely. A new `kind` on the wire would have been the
// other option and a much worse one: govOpsFor-style re-marshalling aside, a
// field an older peer does not know is a field it drops.
//
// Presentation is then a LOCAL question, answered by shape: a body that is
// wrapped in single asterisks and begins with its own sender's name is an
// emote. It fails closed — no sender name, no match — and its only false
// positive is somebody hand-typing exactly that, which is an emote anyway.
//
// `**bold**` is excluded explicitly: it also starts and ends with an asterisk.
export function isEmote(body, senderName) {
  const name = (senderName || "").trim();
  if (!name) return false;
  const t = String(body || "").trim();
  if (t.length < name.length + 3) return false;
  if (t[0] !== "*" || t[1] === "*" || !t.endsWith("*") || t[t.length - 2] === "*") return false;
  return t.slice(1).startsWith(name + " ");
}

// The text `/me` sends. Kept here rather than inline in the composer so the
// producer and the recogniser sit in one file and cannot drift.
export function emoteText(name, action) {
  return `*${(name || "").trim()} ${action.trim()}*`;
}
