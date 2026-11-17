// inspector.svelte.js — which view the inspector shows (Members, Pins, Media,
// Threads). Device-local and per session: the open/closed choice is the
// `memberPanel` pref (Ctrl+U), this is only which tab is up.

export const INSPECTOR_TABS = ["members", "pins", "media", "threads"];

// In a DM the inspector shows only the Media tab, and opens on its own
// switch (the header's Media button) rather than the guild inspector's pref.
export const dmMedia = $state({ open: false });

export const insp = $state({ tab: "members" });

export function showInspectorTab(tab) {
  if (INSPECTOR_TABS.includes(tab)) insp.tab = tab;
}
