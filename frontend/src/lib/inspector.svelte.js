// inspector.svelte.js — which view the inspector shows (Members, Pins, Files,
// Threads). Device-local and per session: the open/closed choice is the
// `memberPanel` pref (Ctrl+U), this is only which tab is up.

export const INSPECTOR_TABS = ["members", "pins", "files", "threads"];

export const insp = $state({ tab: "members" });

export function showInspectorTab(tab) {
  if (INSPECTOR_TABS.includes(tab)) insp.tab = tab;
}
