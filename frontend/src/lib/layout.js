// layout.js — the three arrangements a person can pick in Appearance → Layout,
// the palette and the first-run hint. Each is a bundle of existing device-local
// prefs, so picking one is the same as setting those prefs by hand, and the
// one in force is read back from them.
//
//   sidebar — the default: the guild strip above the list.
//   rail    — the classic four zones: a column of guilds beside the channel
//             list, the conversation, the member list docked.
//   compact — the sidebar with compact messages and the inspector closed.
import { S, setPref, setAppearance } from "./state.svelte.js";

export const LAYOUTS = [
  { id: "sidebar", label: "Sidebar", note: "Guild shortcuts above the channel list. The default." },
  { id: "rail", label: "Rail", note: "A column of guilds beside the channel list, members docked on the right." },
  { id: "compact", label: "Compact", note: "The sidebar, tighter messages, and the side panel closed until you open it." },
];

export function currentLayout(prefs = S.prefs) {
  if (prefs.navModel === "rail") return "rail";
  return prefs.layout === "compact" ? "compact" : "sidebar";
}

export function setLayout(id) {
  const was = currentLayout();
  if (id === was) return;
  setPref("layout", id);
  setPref("navModel", id === "rail" ? "rail" : "sidebar");
  if (id === "compact") {
    setAppearance("density", "compact");
    setPref("memberPanel", false);
  } else {
    // Leaving Compact puts back what Compact changed; the other two leave the
    // density and the panel as they were.
    if (was === "compact") {
      setAppearance("density", "cozy");
      setPref("memberPanel", true);
    }
    if (id === "rail") setPref("memberPanel", true);
  }
}
