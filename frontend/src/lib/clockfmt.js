// clockfmt.js — the feed's H:MM, from one kept formatter per clock setting.
//
// toLocaleTimeString with options builds a fresh Intl formatter on every call:
// about 35us each in V8, against about 1us for a formatter that is kept. The
// feed formats a time for every row a channel switch mounts, so the difference
// was paid on every switch.
//
// hour:"numeric" rather than "2-digit": a 24-hour clock pads itself anyway
// ("19:28"), while a 12-hour one loses a leading zero nobody writes.

let kept = { key: null, fmt: null };

// pref: "12" | "24" | anything else (the locale's own choice).
export function clockFormatter(pref) {
  if (kept.key !== pref || !kept.fmt) {
    const opts = { hour: "numeric", minute: "2-digit" };
    if (pref === "12") opts.hour12 = true;
    else if (pref === "24") opts.hour12 = false;
    kept = { key: pref, fmt: new Intl.DateTimeFormat([], opts) };
  }
  return kept.fmt;
}

// formatClock: "" for anything that is not a time.
export function formatClock(iso, pref) {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return clockFormatter(pref).format(d);
}
