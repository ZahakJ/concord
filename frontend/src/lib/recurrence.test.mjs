// Recurrence is worked out on the reading side, so it has to be right on the
// reading side. These are the cases that are wrong if you add seconds instead
// of stepping the calendar.
import { occurrencesIn, expand, repeatSentence, MAX_OCCURRENCES } from "./recurrence.js";

let fails = 0;
const ok = (cond, msg) => {
  if (!cond) {
    console.error("  FAIL " + msg);
    fails++;
  }
};
const unix = (s) => Math.floor(new Date(s).getTime() / 1000);
// Window bounds as LOCAL midnight, so the assertions do not depend on which
// side of UTC the machine running them sits on.
const at = (y, m, d) => new Date(y, m - 1, d).getTime();
const days = (list) => list.map((o) => new Date(o.startUnix * 1000).toDateString());

// A plain event yields itself, so callers never branch on "is this a series".
{
  const ev = { id: "a", startUnix: unix("2026-03-05T18:00:00"), endUnix: 0 };
  const got = occurrencesIn(ev, at(2026, 3, 1), at(2026, 4, 1));
  ok(got.length === 1 && got[0].key === "a", "one-off yields itself with its own id as the key");
  ok(occurrencesIn(ev, at(2026, 5, 1), at(2026, 6, 1)).length === 0, "one-off outside the window yields nothing");
}

// Weekly steps the DATE, so it survives a daylight-saving change with the
// clock intact. Adding 7*86400 seconds is what gets this wrong.
{
  // Starts BEFORE the northern-hemisphere clock change and runs past it.
  const ev = { id: "w", startUnix: unix("2026-03-01T19:00:00"), endUnix: 0, repeat: "weekly" };
  const got = occurrencesIn(ev, at(2026, 3, 1), at(2026, 4, 1));
  const hours = got.map((o) => new Date(o.startUnix * 1000).getHours());
  ok(hours.every((h) => h === 19), `weekly keeps its wall clock across a DST boundary (got ${hours})`);
  ok(got.length === 5, `five Sundays from Mar 1 through March (got ${got.length})`);
  ok(new Set(got.map((o) => o.key)).size === got.length, "occurrence keys are unique");
  ok(got.every((o) => o.id === "w"), "occurrences keep the SERIES id — RSVP and edit act on one record");
}

// Biweekly is weekly with a stride, not "twice a week".
{
  const ev = { id: "b", startUnix: unix("2026-01-05T10:00:00"), endUnix: 0, repeat: "biweekly" };
  const got = occurrencesIn(ev, at(2026, 1, 1), at(2026, 2, 15));
  ok(days(got).join(",") === "Mon Jan 05 2026,Mon Jan 19 2026,Mon Feb 02 2026", `biweekly strides 14 days (got ${days(got)})`);
}

// Monthly on the 31st SKIPS the months that have no 31st, the way RFC 5545's
// BYMONTHDAY does. Rolling forward would silently turn "the 31st" into "the
// 1st" for half the year.
{
  const ev = { id: "m", startUnix: unix("2026-01-31T09:00:00"), endUnix: 0, repeat: "monthly" };
  const got = occurrencesIn(ev, at(2026, 1, 1), at(2026, 6, 1));
  ok(
    days(got).join(",") === "Sat Jan 31 2026,Tue Mar 31 2026,Sun May 31 2026",
    `monthly on the 31st skips February, April and June (got ${days(got)})`,
  );
}

// The end condition is inclusive of an occurrence landing exactly on it, and
// nothing after.
{
  const ev = {
    id: "u",
    startUnix: unix("2026-02-02T12:00:00"),
    endUnix: 0,
    repeat: "weekly",
    repeatUntil: unix("2026-02-16T12:00:00"),
  };
  const got = occurrencesIn(ev, at(2026, 1, 1), at(2026, 12, 1));
  ok(got.length === 3, `an UNTIL on an occurrence includes it (got ${got.length})`);
}

// A series that started years ago must still show up in THIS week. Walking
// from occurrence zero burns the whole cap in the past and the series simply
// vanishes from the calendar, which is the failure mode the seek exists to
// prevent — 2,200 daily occurrences separate 2020 from 2026.
{
  const ev = { id: "f", startUnix: unix("2020-01-01T08:00:00"), endUnix: 0, repeat: "daily" };
  const got = occurrencesIn(ev, at(2026, 1, 1), at(2026, 1, 8));
  ok(got.length <= MAX_OCCURRENCES, "expansion is bounded");
  ok(got.length === 7, `a six-year-old daily series still fills this week (got ${got.length})`);
  ok(days(got)[0] === "Thu Jan 01 2026", `and starts on the window's first day (got ${days(got)[0]})`);
}

// The same for a long-running monthly and weekly series.
{
  const m = { id: "m2", startUnix: unix("2019-06-15T09:00:00"), endUnix: 0, repeat: "monthly" };
  ok(occurrencesIn(m, at(2026, 4, 1), at(2026, 5, 1)).length === 1, "a seven-year-old monthly series still lands in April");
  const w = { id: "w2", startUnix: unix("2019-06-03T09:00:00"), endUnix: 0, repeat: "weekly" };
  ok(occurrencesIn(w, at(2026, 4, 1), at(2026, 5, 1)).length === 4, "a seven-year-old weekly series still fills April");
}

// A long-running series still reaches a window inside its cap.
{
  const ev = { id: "g", startUnix: unix("2026-01-06T08:00:00"), endUnix: 3600 + unix("2026-01-06T08:00:00"), repeat: "weekly" };
  const got = occurrencesIn(ev, at(2026, 3, 1), at(2026, 4, 1));
  ok(got.length === 5, `a weekly series reaches a later window (got ${got.length})`);
  ok(got.every((o) => o.endUnix - o.startUnix === 3600), "every occurrence keeps the series duration");
}

// expand sorts by start across records.
{
  const a = { id: "a", startUnix: unix("2026-05-02T10:00:00"), repeat: "" };
  const b = { id: "b", startUnix: unix("2026-05-01T10:00:00"), repeat: "weekly" };
  const got = expand([a, b], at(2026, 5, 1), at(2026, 5, 10));
  ok(got[0].id === "b" && got[1].id === "a", "expand returns start order across records");
}

ok(repeatSentence({ repeat: "weekly" }, "en-US") === "Repeats weekly", "sentence for an endless series");
ok(
  repeatSentence({ repeat: "biweekly", repeatUntil: unix("2026-09-01T00:00:00") }, "en-US").startsWith("Repeats every 2 weeks until"),
  "sentence names the end",
);
ok(repeatSentence({ repeat: "" }, "en-US") === "", "a one-off says nothing");

if (fails) {
  console.error(`recurrence: ${fails} failure(s)`);
  process.exit(1);
}
console.log("recurrence.js: all tests passed");
