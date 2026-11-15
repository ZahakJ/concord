// receipts.js — what the delivery / read ticks under your own DM messages say.
//
// Pure, so it is testable with `node --test`. The backend (internal/app/
// receipts.go) has already applied the mutual rule: it reports a read only
// while this account's read receipts are on, and drops receipts it was sent
// while they are off. tickOf asks again anyway, so a stale view can never draw
// a read tick the person has switched off.

/** "read" | "delivered" | null for one backend ReceiptView. */
export function tickOf(view, readReceiptsOn) {
  if (!view) return null;
  if (readReceiptsOn && view.read) return "read";
  if (view.delivered) return "delivered";
  return null;
}

/** Accessible text for a tick. Group DMs say how many members it covers. */
export function tickLabel(view, kind) {
  if (!kind) return "";
  const group = view && view.of > 1;
  if (kind === "read") return group ? `Read by all ${view.of}` : "Read";
  if (!group) return "Delivered";
  return `Delivered to all ${view.of}`;
}

/** Partial progress in a group DM ("Delivered to 1 of 3"), or "" when none/all. */
export function partialLabel(view, readReceiptsOn) {
  if (!view || !(view.of > 1)) return "";
  if (readReceiptsOn && view.readBy > 0 && !view.read) return `Read by ${view.readBy} of ${view.of}`;
  if (view.deliveredBy > 0 && !view.delivered) return `Delivered to ${view.deliveredBy} of ${view.of}`;
  return "";
}
