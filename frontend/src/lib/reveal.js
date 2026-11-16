// reveal.js — when the view offers a moderator the original of a deleted
// message, and what it shows when this device has none to give.
//
// The core keeps a deleted body only where it can be revealed: a guild (or a
// meeting) in which this account holds Manage Messages. A direct message, one
// to one or with several people, has no moderators and keeps no body, so the
// view never offers the reveal there (RevealDeleted refuses it too). An
// expired message was erased on every device.

// Kinds of conversation with no moderators: the core's noModerators.
const NO_MODERATORS = new Set(["dm", "group"]);

export function canRevealDeleted(m, guildKind, canDeleteOthers) {
  return Boolean(m?.deleted && !m.expired && canDeleteOthers && !NO_MODERATORS.has(guildKind ?? ""));
}

// The line shown in place of the original when this device did not keep it
// (it could not reveal it when the delete was applied, or it was cleared).
export const NOT_KEPT = "Not kept on this device";

const NOT_KEPT_ERR = /this device did not keep the deleted message/;

// notKept reports whether a reveal's error is the core saying this device holds
// no body for the message, which asking again will not change.
export function notKept(err) {
  return NOT_KEPT_ERR.test(String(err?.message ?? err ?? ""));
}
