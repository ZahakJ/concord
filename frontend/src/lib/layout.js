// layout.js — the two arrangements a person can pick in Appearance → Layout,
// the palette and the first-run hint. Each is a bundle of existing device-local
// prefs, so picking one is the same as setting those prefs by hand, and the
// one in force is read back from them.
//
//   sidebar — the default: the guild strip above the list.
//   rail    — the classic four zones: a column of guilds beside the channel
//             list, the conversation, the member list docked.
//
// There was a third, Compact: the sidebar with compact density and the side
// panel closed. It duplicated Appearance › Density, so it is that now; a saved
// "compact" layout reads as the sidebar (lib/packs.js migratePrefs), and the
// density and panel it had set stay as they were.
import { S, setPref } from "./state.svelte.js";

export const LAYOUTS = [
  { id: "sidebar", label: "Sidebar", note: "Guild shortcuts above the channel list. The default." },
  { id: "rail", label: "Rail", note: "A column of guilds beside the channel list, members docked on the right." },
];

export function currentLayout(prefs = S.prefs) {
  return prefs.navModel === "rail" ? "rail" : "sidebar";
}

export function setLayout(id) {
  if (id === currentLayout()) return;
  setPref("layout", id === "rail" ? "rail" : "sidebar");
  setPref("navModel", id === "rail" ? "rail" : "sidebar");
  if (id === "rail") setPref("memberPanel", true);
}
