// What an unlink did, worded for the screen. The core returns
// { pending: [{kind, id, name, removableBy}], failed: [...] }; an older core
// returns nothing, which reads as a complete unlink.
//
// Pending is not a failure: the device is unlinked, but in a guild or direct
// message where this device may not remove members its entry stays until
// someone there removes it. A removal that was allowed and failed is the only
// error, and unlinking again retries it.

const where = (spot) => (spot.kind === "dm" ? `Direct message with ${spot.name || "someone"}` : spot.name || "A guild");

const who = (spot) =>
  spot.kind === "dm"
    ? `${spot.removableBy || "The person who started it"} can remove it`
    : "An owner or moderator there can remove it";

export function unlinkReport(label, result) {
  const pending = (result?.pending || []).map((s) => ({ key: s.id, where: where(s), who: who(s) }));
  const failed = result?.failed || [];
  return {
    success: `${label} unlinked. It erases itself when it next comes online.`,
    pending,
    pendingTitle: pending.length ? `${label} is still listed in:` : "",
    // The same on one line, for a toast.
    pendingLine: pending.length ? `${label} is still listed in: ${pending.map((p) => p.where).join(", ")}` : "",
    error: failed.length ? `Removing it from ${failed.map(where).join(", ")} failed. Unlink it again to retry.` : "",
  };
}
