// recurrence.js — one record, many occurrences.
//
// A recurring event is ONE stored record carrying a rule; the repetitions are
// worked out by whoever is looking at the calendar. That is the whole design
// and it is worth saying why, because materialising the occurrences would have
// been the shorter code: a weekly standup is fifty-two records a year, which
// walks straight into the per-guild event ceiling, and "edit the meeting"
// would then mean editing fifty-two things — or inventing a parent link and a
// rule for what an exception is. The seeded world's flagship event is
// literally called "Weekly sync", and it had to be hand-created every week.
//
// Everything here is pure and works in local time on purpose. "Every week at
// 7pm" means seven at night where the organizer lives, across a daylight
// saving change; adding 7×86400 seconds to a timestamp gets that wrong twice a
// year, so the walk steps the DATE and lets the clock come out where it may.

export const REPEATS = [
  { id: "", label: "Never", short: "" },
  { id: "daily", label: "Every day", short: "daily" },
  { id: "weekly", label: "Every week", short: "weekly" },
  { id: "biweekly", label: "Every other week", short: "every 2 weeks" },
  { id: "monthly", label: "Every month", short: "monthly" },
];

// MAX_OCCURRENCES bounds a single expansion. A series with no end date is
// legal and common ("every Tuesday, forever"), so the bound has to live at the
// point of USE — a reader expands a window, never an infinity.
export const MAX_OCCURRENCES = 400;

export function repeatLabel(rule) {
  return REPEATS.find((r) => r.id === rule)?.label || "";
}

// repeatSentence is the line a card prints under the time: "Repeats weekly",
// and the end if there is one.
export function repeatSentence(ev, locale) {
  const r = REPEATS.find((x) => x.id === ev?.repeat);
  if (!r || !r.id) return "";
  let s = `Repeats ${r.short}`;
  if (ev.repeatUntil > 0) {
    const d = new Date(ev.repeatUntil * 1000);
    s += ` until ${d.toLocaleDateString(locale, { month: "short", day: "numeric", year: "numeric" })}`;
  }
  return s;
}

// stepFrom returns the nth occurrence's start as a Date, walking the calendar
// rather than adding seconds.
function stepFrom(first, rule, n) {
  const d = new Date(first);
  switch (rule) {
    case "daily":
      d.setDate(d.getDate() + n);
      return d;
    case "weekly":
      d.setDate(d.getDate() + 7 * n);
      return d;
    case "biweekly":
      d.setDate(d.getDate() + 14 * n);
      return d;
    case "monthly": {
      // The 31st does not exist in every month. JavaScript would roll it
      // forward into the next one, which silently turns "the 31st" into "the
      // 1st" for half the year; RFC 5545's BYMONTHDAY SKIPS those months, and
      // skipping is the behaviour a human expects from "monthly on the 31st".
      const day = d.getDate();
      const m = new Date(d);
      m.setDate(1);
      m.setMonth(m.getMonth() + n);
      const last = new Date(m.getFullYear(), m.getMonth() + 1, 0).getDate();
      if (day > last) return null;
      m.setDate(day);
      m.setHours(d.getHours(), d.getMinutes(), d.getSeconds(), 0);
      return m;
    }
    default:
      return n === 0 ? d : null;
  }
}

// seekIndex is the lowest occurrence index that can land at or after fromMs,
// biased one step early so a daylight-saving hour or a month-length quirk can
// never make it overshoot the true first hit.
function seekIndex(firstMs, rule, fromMs) {
  if (fromMs <= firstMs) return 0;
  const strideDays = { daily: 1, weekly: 7, biweekly: 14 }[rule];
  if (strideDays) {
    const elapsed = Math.floor((fromMs - firstMs) / 86400000);
    return Math.max(0, Math.floor(elapsed / strideDays) - 1);
  }
  if (rule === "monthly") {
    const f = new Date(firstMs);
    const t = new Date(fromMs);
    const months = (t.getFullYear() - f.getFullYear()) * 12 + (t.getMonth() - f.getMonth());
    return Math.max(0, months - 1);
  }
  return 0;
}

// occurrencesIn expands one event into the occurrences that overlap
// [fromMs, toMs). A non-recurring event yields itself when it lands in range,
// so callers never branch on whether a record is a series.
//
// Each occurrence is a SHALLOW COPY with its own times plus a `key` and an
// `occurrence` index. The id is deliberately unchanged: RSVPs, editing and
// deletion all act on the series, which is the one record that exists, and
// giving occurrences distinct ids would mean three call sites learning to map
// back.
export function occurrencesIn(ev, fromMs, toMs) {
  const out = [];
  if (!ev || !ev.startUnix) return out;
  const durSec = ev.endUnix ? ev.endUnix - ev.startUnix : 0;
  const firstMs = ev.startUnix * 1000;
  const rule = ev.repeat || "";
  if (!rule) {
    const endMs = firstMs + durSec * 1000;
    if (endMs >= fromMs && firstMs < toMs) out.push({ ...ev, key: ev.id, occurrence: 0 });
    return out;
  }
  const untilMs = ev.repeatUntil > 0 ? ev.repeatUntil * 1000 : Infinity;
  // SEEK, don't scan. A daily standup started three years ago is 1,100
  // occurrences before today, so walking from zero burns the whole cap in the
  // past and the series disappears from this month's calendar — a series that
  // is plainly still running would simply not be there. The first index that
  // could reach the window is arithmetic, so start there.
  const first = seekIndex(firstMs, rule, fromMs);
  for (let n = first; n < first + MAX_OCCURRENCES; n++) {
    const d = stepFrom(firstMs, rule, n);
    if (d === null) continue; // a month that has no such day
    const ms = d.getTime();
    if (ms > untilMs) break;
    if (ms >= toMs) break;
    const endMs = ms + durSec * 1000;
    if (endMs < fromMs) continue;
    const startUnix = Math.floor(ms / 1000);
    out.push({
      ...ev,
      startUnix,
      endUnix: ev.endUnix ? startUnix + durSec : 0,
      key: `${ev.id}#${n}`,
      occurrence: n,
    });
  }
  return out;
}

// expand runs occurrencesIn over a list and returns them in start order, which
// is the order every calendar surface reads in.
export function expand(events, fromMs, toMs) {
  const out = [];
  for (const ev of events || []) out.push(...occurrencesIn(ev, fromMs, toMs));
  out.sort((a, b) => a.startUnix - b.startUnix || String(a.key).localeCompare(String(b.key)));
  return out;
}
