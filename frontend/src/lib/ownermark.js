// Which places wear the owner's crown (OwnerMark.svelte), in one rule.
//
// Everything you can own is marked the same way: a guild, an instant
// meeting's guild (you started it, you hold its keys and its guest link), and
// a group conversation you started (only its starter adds or removes people).
// A one-to-one conversation has no owner worth naming — both people hold it
// equally, and whoever happened to send the first message is not a fact
// anyone needs — and Notes is yours alone by definition, so neither is marked.
export function ownsPlace(g) {
  if (!g || !g.isOwner || g.evicted) return false;
  if (g.kind === "dm") return !g.dmNotes && (g.dmMembers ?? 2) > 2;
  return true;
}

// The words for it, where a mark needs a name.
export function ownerLabel(g) {
  return g?.kind === "dm" ? "You started this group" : "You own this guild";
}
