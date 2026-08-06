// saved.svelte.js — the reader's private bookmarks. Deliberately its own tiny
// module rather than more state.svelte.js surface: a bookmark never rides any
// wire, so nothing else in the app needs to react to it beyond the menu label
// and the Saved panel.
import { api } from "./api.js";
import { flash } from "./state.svelte.js";

export const saved = $state({ ids: new Set(), loaded: false });

export async function refreshSaved() {
  try {
    saved.ids = new Set((await api.savedMessageIDs()) || []);
    saved.loaded = true;
  } catch {
    /* pre-login or backend hiccup: the menu just shows "Save message" */
  }
}

export function isSaved(id) {
  return saved.ids.has(id);
}

export async function toggleSaved(m) {
  try {
    if (saved.ids.has(m.id)) {
      await api.unbookmarkMessage(m.id);
      saved.ids.delete(m.id);
      saved.ids = new Set(saved.ids);
      flash("Removed from saved");
    } else {
      await api.bookmarkMessage(m.id, m.channelId || "");
      saved.ids = new Set([...saved.ids, m.id]);
      flash("Saved — find it under Saved messages", "success");
    }
  } catch (err) {
    flash(err);
  }
}
