// hidden.js — pure logic for "Delete for me" (hiding a message on this
// account's devices only). The core filters hidden messages out of every read;
// this is what the UI does with the news that one more was hidden or restored.

// dropMessage removes one message from a feed. Returns the SAME array when the
// message is not in it, so callers can skip a reassignment (and a re-render).
export function dropMessage(messages, id) {
  const i = messages.findIndex((m) => m.id === id);
  if (i < 0) return messages;
  return messages.slice(0, i).concat(messages.slice(i + 1));
}

// withHidden returns the hidden-id set after a hide/unhide event, as a new
// object (state is replaced, not mutated, so readers re-derive).
export function withHidden(set, id, hidden) {
  const next = { ...set };
  if (hidden) next[id] = true;
  else delete next[id];
  return next;
}

// replyState says what a "replying to ..." line may show for its target:
//   "hidden"  the target is one this user hid ("Message hidden", no content)
//   "missing" not loaded here
//   "deleted" deleted for everyone
//   "ok"      show the author and snippet
// Hidden wins over everything: a hidden message's content is never shown, even
// if a stale copy is still in memory.
export function replyState(replyTo, ref, hiddenIds) {
  if (replyTo && hiddenIds && hiddenIds[replyTo]) return "hidden";
  if (!ref) return "missing";
  if (ref.deleted) return "deleted";
  return "ok";
}

// hideConfirm is the dialog's wording, kept in one place so the UI and its
// test agree.
export const HIDE_TITLE = "Delete for me?";
export const HIDE_BODY = "Delete for me: removes this message from your devices only. Others still see it.";
