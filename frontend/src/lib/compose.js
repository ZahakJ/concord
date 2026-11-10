// compose.js — composer state that has to outlive the conversation on screen.
//
// Staged attachments used to be one list for the whole composer while the
// text draft was kept per conversation. Attach a photo in a DM, hop to a guild
// channel to check something, answer there — and the photo went to the guild
// with the answer. The tray belongs to the conversation it was filled in, the
// same as the text does. Kept in memory only: a staged file is a data URL of
// up to 25 MB, which is no thing to write into localStorage, and losing a tray
// on reload is a far smaller surprise than sending one to the wrong people.

const held = new Map();

// stashStaged parks a conversation's tray while another one is on screen.
export function stashStaged(channelId, items) {
  if (!channelId) return;
  if (items && items.length) held.set(channelId, items);
  else held.delete(channelId);
}

// takeStaged hands a conversation its parked tray back (and forgets it, since
// from now on the composer holds it).
export function takeStaged(channelId) {
  if (!channelId) return [];
  const items = held.get(channelId) || [];
  held.delete(channelId);
  return items;
}

// addStaged appends to a parked tray. A file still being read when you switch
// conversations finishes into the conversation it was picked in, not the one
// you have since opened.
export function addStaged(channelId, item) {
  if (!channelId || !item) return;
  held.set(channelId, [...(held.get(channelId) || []), item]);
}

// stagedCount is how many files a parked tray holds.
export function stagedCount(channelId) {
  return (held.get(channelId) || []).length;
}
