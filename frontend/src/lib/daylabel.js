// daylabel.js — the words on a feed's day divider.
//
// "Today", "Yesterday", the weekday for the rest of the last week, then the
// date — and the year whenever the day is not from this year. Without the year
// a message from last March and one from this March wore the same label, so an
// old thread scrolled into view read as recent.

const DAY = 86400000;

// dayLabel(day, now, locale): `day` is anything Date accepts (the feed passes
// Date#toDateString keys); `now` is injectable for tests.
export function dayLabel(day, now = new Date(), locale = []) {
  const d = new Date(day);
  if (isNaN(d)) return "";
  const start = (x) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const diff = Math.round((start(now) - start(d)) / DAY);
  if (diff === 0) return "Today";
  if (diff === 1) return "Yesterday";
  if (diff > 1 && diff < 7) return d.toLocaleDateString(locale, { weekday: "long" });
  const opts = { day: "numeric", month: "long" };
  if (d.getFullYear() !== now.getFullYear()) opts.year = "numeric";
  return d.toLocaleDateString(locale, opts);
}
