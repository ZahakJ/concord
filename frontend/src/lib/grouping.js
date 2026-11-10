// grouping.js — whether a feed row tucks under the one above it (no avatar,
// no name) or starts a new group with its author's header.

// continuesGroup(prev, m, ctx): ctx carries what only the list knows —
// newDay (a day divider sits between them), emote (either row is an emote),
// firstUnread (m is the first row under the NEW line) and windowMs.
export function continuesGroup(prev, m, ctx = {}) {
  if (!prev || !m) return false;
  if (ctx.newDay) return false;
  // Grouping follows the AUTHOR, not the signing key: a relayed guest is a
  // different author even though the host signed their message, so their
  // words never tuck under the host's name (and two lines from the same
  // guest still group together).
  const sameAuthor =
    prev.sender === m.sender && prev.kind === m.kind && (m.kind !== "guest" || prev.senderName === m.senderName);
  if (!sameAuthor) return false;
  if (m.kind !== "" && m.kind !== "guest") return false;
  // An emote never groups. Its whole grammar is "this person is doing
  // something", and a row with no name header cannot say who — which is
  // exactly what tucking "*Amina Sadiq waves*" under the message above it
  // did. Neither does the row AFTER one, or a plain sentence would inherit
  // the emote's header.
  if (ctx.emote) return false;
  if (m.replyTo || prev.deleted) return false;
  // The first unread row wears its author: it sits under the NEW line, and a
  // headless continuation there left you scrolling up past the divider to
  // learn who started the part you had not read.
  if (ctx.firstUnread) return false;
  return new Date(m.sent) - new Date(prev.sent) < (ctx.windowMs ?? 5 * 60 * 1000);
}
