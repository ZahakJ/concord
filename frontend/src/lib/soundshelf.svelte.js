// soundshelf.svelte.js — the sounds you have made or kept, on this device.
//
// A shelf entry is nothing but the recipe payload: the same base64url string
// that rides in a message token or a voice trigger. That is deliberate. There
// is no id to resolve, no record to sync, and no authority to publish under —
// a sound spreads by being HEARD. Press one in a room and everybody there can
// keep it; send one to a channel and anybody reading can keep it. Which is a
// distribution model that only works because the sound IS the message.
//
// Deliberately local. Publishing a sound to a guild's board is the proposal's
// own shape for this (a self-signed record on a guild-meta lane, gated on
// manage-guild) and it is a bigger thing than a shelf: it needs a new arm, a
// signature, a permission and a sync path. Keeping is not publishing, costs
// nothing on the wire, and needs nobody's permission — so that is what this is.

import { decodeRecipe, encodeRecipe, STARTER_SHELF } from "./sfxrecipe.js";

const KEY = "concord.soundshelf";
// Enough for a guild's worth of in-jokes and small enough that the list stays
// a shelf rather than a filing cabinet. At ~90 characters a payload the whole
// thing is under 3 KB either way; the cap is about the UI, not the storage.
export const MAX_SHELF = 32;

function load() {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) || "null");
    if (Array.isArray(raw)) return raw.filter((p) => typeof p === "string" && decodeRecipe(p)).slice(0, MAX_SHELF);
  } catch {
    /* private mode, or somebody else's JSON */
  }
  // First run: the starter shelf, so the studio opens onto twelve sounds
  // instead of a row of sliders at zero.
  return STARTER_SHELF.map(encodeRecipe).filter(Boolean);
}

// One reactive list, shared by the studio and the voice room's soundboard.
const shelf = $state({ items: load() });

// Entries come back decoded, so no caller has to remember to validate. A
// payload that stopped decoding (a build that tightened a bound) simply drops
// out of the list rather than rendering a chip that plays nothing.
export function shelfSounds() {
  return shelf.items.map((payload) => ({ payload, recipe: decodeRecipe(payload) })).filter((s) => s.recipe);
}

export function onShelf(payload) {
  return shelf.items.includes(payload);
}

function persist() {
  try {
    localStorage.setItem(KEY, JSON.stringify(shelf.items));
  } catch {
    /* the in-memory list still holds for this session */
  }
}

// Newest first, and never twice: keeping a sound you already have moves it to
// the front rather than filling the shelf with the same joke.
export function keepSound(payload) {
  if (!payload || !decodeRecipe(payload)) return false;
  shelf.items = [payload, ...shelf.items.filter((p) => p !== payload)].slice(0, MAX_SHELF);
  persist();
  return true;
}

export function dropSound(payload) {
  shelf.items = shelf.items.filter((p) => p !== payload);
  persist();
}

export function shelfFull() {
  return shelf.items.length >= MAX_SHELF;
}
