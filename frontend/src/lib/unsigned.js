// The "unsigned" caveat on a message row (Message.svelte).
//
// The core marks a row unverified when it reached this device through another
// member's history with no signature from its author (internal/app/recordsig.go).
// That is a fact about how the COPY travelled, not a finding about the words,
// and the wording has to say so: a sender on a build that predates signatures
// writes honest messages that can still arrive this way. The earlier wording
// read as an accusation and was reported as one.

export const UNSIGNED_TIP =
  "This copy came from another member's history and carries no signature from its author. " +
  "It will be confirmed if the author's own device sends it.";

export const UNSIGNED_LABEL = "Unsigned copy from another member's history";

// showUnsignedMark: the row is marked, and it is not one this account wrote.
// Your own words need no proof shown to you — you know whether you wrote them —
// and flagging them only makes an honest history look tampered with.
export function showUnsignedMark(m, selfFingerprint) {
  if (!m || !m.unverified) return false;
  if (selfFingerprint && m.sender === selfFingerprint && m.kind !== "guest") return false;
  return true;
}
