// drafts.svelte.js — which conversations have something unsent in them.
//
// A draft already survived a channel switch and a reload, correctly, and was
// completely invisible: a channel you had half-written a message in looked
// exactly like every other read channel. Half-written messages are how people
// lose thoughts, and a per-device draft nobody can see from the list is a
// thought you will not remember you had.
//
// The drafts themselves stay where they were — localStorage, per channel,
// written by Composer.svelte. This is only the index: a reactive mirror the
// sidebar can read without touching storage on every render, seeded once from
// what is already on disk so a reload shows the marks immediately.

const PREFIX = "concord.draft.";

const index = $state({ map: {} });

function seed() {
  try {
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i);
      if (!k?.startsWith(PREFIX)) continue;
      const v = localStorage.getItem(k);
      if (v) index.map[k.slice(PREFIX.length)] = v;
    }
  } catch {
    /* private mode / storage blocked: no marks, everything else still works */
  }
}
seed();

// noteDraft mirrors one save. Called by the composer's own saveDraft, so the
// index cannot drift from the storage it describes.
export function noteDraft(channelId, text) {
  if (!channelId) return;
  const t = String(text || "");
  if (t) index.map[channelId] = t;
  else delete index.map[channelId];
}

// The unsent text for a conversation, or "".
export function draftIn(channelId) {
  return (channelId && index.map[channelId]) || "";
}

// How many conversations are holding one.
export function draftCount() {
  return Object.keys(index.map).length;
}
