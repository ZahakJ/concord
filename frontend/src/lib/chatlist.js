// chatlist.js — ordering of the direct-message list: Notes, pinned chats in the
// user's order, everything else, and an Archived bucket.
//
// A "chat" is a direct conversation (1:1 or group), identified by its first
// channel's id. `prefs` is the backend's record map { [chatId]: { p, k, pa, a,
// aa } } (p pinned, k pin order ascending, a archived). The records are the
// account's own and never leave its devices. Pure functions; no state.

import { isNotesLike } from "./notes.js";
import { own } from "./lookup.js";

// Notes, its copies, and the placeholder the list shows before Notes exists.
const notesish = (d) => !!d?.dmNotes || isNotesLike(d);
export const MAX_PINS = 10;

export const chatIdOf = (dm) => dm?.channels?.[0]?.id || "";
// Chat ids are channel ids a DM's creator chose: read by own property only.
const rec = (prefs, dm) => own(prefs, chatIdOf(dm)) || null;
export const isPinned = (prefs, dm) => !!rec(prefs, dm)?.p;
export const isArchived = (prefs, dm) => !!rec(prefs, dm)?.a;

/**
 * Split an already activity-sorted list of DMs into { main, archived, pinned }.
 * main = Notes first, then pinned (by order key, ties by id), then the rest in
 * incoming order. A pinned chat that is also archived is archived: archive
 * wins, so "leaves the main list" always holds. Archived keeps incoming order.
 */
export function arrange(dms, prefs) {
  const notes = [];
  const pinned = [];
  const rest = [];
  const archived = [];
  for (const dm of dms) {
    if (notesish(dm)) notes.push(dm);
    else if (isArchived(prefs, dm)) archived.push(dm);
    else if (isPinned(prefs, dm)) pinned.push(dm);
    else rest.push(dm);
  }
  pinned.sort((a, b) => {
    const d = (rec(prefs, a).k || 0) - (rec(prefs, b).k || 0);
    return d || (chatIdOf(a) < chatIdOf(b) ? -1 : 1);
  });
  return { main: [...notes, ...pinned, ...rest], pinned, archived };
}

/** Number of chats currently pinned, among the ones that exist. */
export function pinCount(dms, prefs) {
  return dms.filter((d) => !notesish(d) && isPinned(prefs, d)).length;
}

/** Whether another chat can be pinned, and the message to show when not. */
export function canPin(dms, prefs, max = MAX_PINS) {
  if (pinCount(dms, prefs) < max) return { ok: true, message: "" };
  return { ok: false, message: `You can pin up to ${max} chats. Unpin one to pin another.` };
}

/** The pinned chat ids after moving chatId one step (-1 up, +1 down). */
export function movePin(pinned, chatId, dir) {
  const ids = pinned.map(chatIdOf);
  const i = ids.indexOf(chatId);
  const j = i + dir;
  if (i < 0 || j < 0 || j >= ids.length) return ids;
  [ids[i], ids[j]] = [ids[j], ids[i]];
  return ids;
}

/** Unread total shown on the Archived entry; unreadOf(dm) -> number. */
export function archivedUnread(archived, unreadOf) {
  return archived.reduce((n, dm) => n + (unreadOf(dm) || 0), 0);
}
