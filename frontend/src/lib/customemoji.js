// customemoji.js — a guild's custom emoji, by name.
import { own } from "./lookup.js";

// customEmojiTable: {name -> image} with no prototype, so a name that is not
// a guild emoji finds nothing.
export function customEmojiTable(list) {
  const m = Object.create(null);
  for (const e of list || []) if (typeof e?.name === "string") m[e.name] = e.image;
  return m;
}

// reactionImage: the image for a reaction key of the form ":name:", or null.
export function reactionImage(table, emoji) {
  const m = /^:([a-z0-9_]{2,32}):$/.exec(typeof emoji === "string" ? emoji : "");
  return (m && own(table, m[1])) || null;
}
