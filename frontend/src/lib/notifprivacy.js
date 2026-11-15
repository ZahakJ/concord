// notifprivacy.js — what an OS notification is allowed to say.
//
// One device-local setting, S.prefs.notifyShows, decides how much of a message
// leaves the app and lands on the lock screen and in the notification shade:
//
//   "nothing"      (default)  "Concord" / "New message" — no sender, no place, no text
//   "name"                    who and where, but not what ("New message")
//   "full"                    who, where and the text
//
// A missing or unknown value is "nothing": a stale or hand-edited pref must
// never fail open. Every path that posts an OS notification builds its title
// and body here, so there is exactly one rule.

export const NOTIFY_MODES = [
  { id: "nothing", label: "Nothing" },
  { id: "name", label: "Name only" },
  { id: "full", label: "Name and message" },
];

export const DEFAULT_NOTIFY_MODE = "nothing";

const IDS = new Set(NOTIFY_MODES.map((m) => m.id));

export const normalizeNotifyMode = (v) => (IDS.has(v) ? v : DEFAULT_NOTIFY_MODE);

const GENERIC_TITLE = "Concord";

// kind: "message" | "mention" | "request" (DM request) | "call"
// title/body are what the caller would show with full disclosure.
export function redactNotification(mode, kind, { title = "", body = "" } = {}) {
  const m = normalizeNotifyMode(mode);
  const generic = kind === "call" ? "Incoming call" : "New message";
  if (m === "full") return { title, body };
  if (m === "name") return { title, body: generic };
  return { title: GENERIC_TITLE, body: generic };
}

// The mode in force on this device. Held here (set by App.svelte whenever the
// pref changes) so the notification doors need not import app state.
let current = DEFAULT_NOTIFY_MODE;
// Notifications still on screen, built under the mode that was in force when
// they were posted. A change of mode closes them, so nothing one showed
// outlives the setting that allowed it.
const shown = new Set();
export const trackNotification = (n) => {
  shown.add(n);
  n.addEventListener?.("close", () => shown.delete(n));
  return n;
};
export const setNotifyMode = (v) => {
  const next = normalizeNotifyMode(v);
  if (next !== current) {
    for (const n of shown) {
      try {
        n.close();
      } catch {
        /* already gone */
      }
    }
    shown.clear();
  }
  current = next;
  return current;
};
export const getNotifyMode = () => current;
