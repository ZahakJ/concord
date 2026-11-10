// A typing line announces a message that has not arrived yet. When it does
// arrive, the line is over: leaving it to its 4-second timer kept "X is
// typing…" on screen under the very message it announced. Only a message's
// first arrival ends it; an edit, a reaction or a backfilled row re-emitted
// under the same id says nothing about what the person is doing now.
export function typistsAfter(list, m, activeChannelId, firstSight) {
  if (!firstSight || m.deleted || m.backfill) return { keep: list, gone: [] };
  if (m.channelId !== activeChannelId || !m.sender) return { keep: list, gone: [] };
  const gone = list.filter((t) => t.from === m.sender);
  if (!gone.length) return { keep: list, gone };
  return { keep: list.filter((t) => t.from !== m.sender), gone };
}

// ---- what we send ----
//
// typingSignal decides what one composer edit tells the channel:
//   "start" — text was inserted, and the last start went out more than
//             `every` ms ago (receivers keep a line alive for 4 s);
//   "stop"  — the draft is now empty and we had said we were typing;
//   null    — nothing to say.
// Deleting is not typing: a single Backspace in an idle composer used to light
// "X is typing…" on everyone's screen for four seconds, and clearing the box
// left the line standing just as long.
export function typingSignal({ inputType = "", draft = "", sentAt = 0, now = Date.now(), every = 2000 }) {
  if (!draft.trim()) return sentAt ? "stop" : null;
  const inserting = !inputType || inputType.startsWith("insert");
  if (!inserting) return null;
  return now - sentAt > every ? "start" : null;
}
