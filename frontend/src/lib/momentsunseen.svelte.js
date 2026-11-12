// momentsunseen.svelte.js — which guilds have moments you have not watched.
//
// The member panel's tray used to say so with a ring on each author's tile;
// when moments moved into the guild menu nothing said they existed at all, so
// a moment shared to a guild could go unwatched for its whole life. This keeps
// one number per guild (unseen moments, and how many there are in all) for the
// quiet ring on the guild's mark and for the menu's wording. It refreshes a
// guild when the core says its stories changed, when you share one, and when
// the viewer closes (which is when moments become seen).
import { api, on } from "./api.js";

export const MOM = $state({ unseen: {}, total: {} });

export async function refreshMoments(guildId) {
  if (!guildId) return;
  try {
    const list = (await api.guildStories(guildId)) || [];
    const ids = new Set();
    let unseen = 0;
    for (const s of list) {
      if (!s || ids.has(s.id)) continue;
      ids.add(s.id);
      if (!s.seen) unseen++;
    }
    MOM.unseen[guildId] = unseen;
    MOM.total[guildId] = ids.size;
  } catch {
    /* locked or transport down: keep what was known */
  }
}

// watchMoments(guildIds): refreshes every guild once, then follows changes.
// `guildIds` is a function so the set is read fresh on every event.
export function watchMoments(guildIds) {
  const all = () => guildIds().forEach(refreshMoments);
  all();
  const off = on("story", (u) => (u?.guildId ? refreshMoments(u.guildId) : all()));
  window.addEventListener("concord:stories-changed", all);
  return () => {
    off?.();
    window.removeEventListener("concord:stories-changed", all);
  };
}
