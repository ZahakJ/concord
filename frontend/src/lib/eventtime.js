// eventtime.js — pure time reasoning for calendar events, shared by the cards
// and the calendars. Split from events.svelte.js so plain node can test it
// (runes files can't run outside the compiler, and none of this needs state).

// happeningNow drives the live state. An event with no stated end counts as
// "now" for an hour — long enough to matter, short enough that yesterday's
// standup isn't still throbbing on the card.
export function happeningNow(ev, now = Date.now() / 1000) {
  const end = ev.endUnix || ev.startUnix + 3600;
  return ev.startUnix <= now && now < end;
}

// isPast: the event is over (same one-hour assumption for open-ended events).
export function isPast(ev, now = Date.now() / 1000) {
  return (ev.endUnix || ev.startUnix + 3600) <= now;
}

// The card's four temperatures, in one word. "soon" is the T-60m window where
// the kicker starts counting down and Join promotes early — never make people
// wait for the clock.
//   "upcoming" | "soon" | "live" | "ended"
export function eventPhase(ev, now = Date.now() / 1000) {
  if (happeningNow(ev, now)) return "live";
  if (isPast(ev, now)) return "ended";
  return ev.startUnix - now <= 3600 ? "soon" : "upcoming";
}

// Kicker copy for the "soon" window, MINUTES-GRAINED on purpose: a calm
// glance-line, not a stopwatch. CSS uppercases it — sentence case here so
// screen readers don't spell it out letter by letter.
export function fmtCountdown(startUnix, now = Date.now() / 1000) {
  const mins = Math.ceil((startUnix - now) / 60);
  if (mins <= 0) return "Starting now";
  if (mins === 1) return "Starts in 1 min";
  return `Starts in ${mins} min`;
}
