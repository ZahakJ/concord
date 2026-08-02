// events.svelte.js — client layer for the guild calendar, shared by its two
// surfaces (the per-guild panel and the cross-guild "Your calendar" agenda).
// The backend owns the data (api.events); this is a per-guild cache so the
// panels don't refetch on every render. The core emits the ordinary
// guild-updated signal for every calendar change, so consumers refresh on the
// channel they already listen to — no new event stream.
import { api } from "./api.js";
import { S, clockOpts } from "./state.svelte.js";

// guildId -> [events], sorted by startUnix (the backend's order).
export const EV = $state({ byGuild: {} });

export async function loadEvents(guildId) {
  if (!guildId) return;
  try {
    EV.byGuild[guildId] = (await api.events(guildId)) || [];
  } catch {
    // Guild gone mid-fetch (left/deleted) — keep whatever we last had rather
    // than blanking a list the user is looking at.
  }
}

// The aggregated agenda needs every guild, DMs included: a calendar entry is
// only offered in real guilds, but events synced into any kind of room should
// still show up on "your day".
export async function loadAllEvents() {
  await Promise.all(S.guilds.map((g) => loadEvents(g.id)));
}

// Local-day bucket key. toDateString (not toISOString) so an event at 23:30
// lands on the day the user will experience it in, not the UTC one.
export const dayKey = (unix) => new Date(unix * 1000).toDateString();

export function fmtDayHeading(key) {
  const today = new Date().toDateString();
  const tomorrow = new Date(Date.now() + 86400000).toDateString();
  if (key === today) return "Today";
  if (key === tomorrow) return "Tomorrow";
  const d = new Date(key);
  const opts = { weekday: "long", month: "long", day: "numeric" };
  // A different year is the one case "October 12" actively misleads.
  if (d.getFullYear() !== new Date().getFullYear()) opts.year = "numeric";
  return d.toLocaleDateString([], opts);
}

// "6:00 PM", "6:00 – 8:30 PM", or "6:00 PM → Mar 2 1:00 AM" when the end
// crosses midnight (dropping the day there would read as time travel).
export function fmtEventTime(ev) {
  const t = (u) =>
    new Date(u * 1000).toLocaleTimeString([], { hour: "numeric", minute: "2-digit", ...clockOpts() });
  if (!ev.endUnix) return t(ev.startUnix);
  if (dayKey(ev.startUnix) !== dayKey(ev.endUnix)) {
    const d = new Date(ev.endUnix * 1000).toLocaleDateString([], { month: "short", day: "numeric" });
    return `${t(ev.startUnix)} → ${d} ${t(ev.endUnix)}`;
  }
  return `${t(ev.startUnix)} – ${t(ev.endUnix)}`;
}

// Time reasoning (happeningNow / isPast / eventPhase / fmtCountdown) lives in
// eventtime.js — pure functions node can test — re-exported here so consumers
// keep one import for everything calendar.
export { happeningNow, isPast, eventPhase, fmtCountdown } from "./eventtime.js";

export function rsvpBuckets(ev) {
  const b = { going: [], maybe: [], no: [] };
  for (const [fpr, st] of Object.entries(ev.rsvps || {})) if (b[st]) b[st].push(fpr);
  return b;
}

// The same deterministic tint GuildRail gives an icon-less guild, so the
// initials bubble on an aggregated event matches the rail the user knows.
export function guildTint(id) {
  let h = 0;
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0;
  const hue = h % 360;
  return `background:linear-gradient(135deg, hsl(${hue} 42% 34%), hsl(${(hue + 45) % 360} 48% 25%));color:#fff`;
}

export const guildInitials = (name) =>
  (name || "?")
    .split(/\s+/)
    .map((w) => w[0])
    .join("")
    .slice(0, 2);

// Hand RFC 5545 text to the OS as a plain download via a data: URL — the
// user's own calendar app opens it. No server, no blob lifetime to manage.
export function downloadICS(filename, text) {
  const a = document.createElement("a");
  a.href = "data:text/calendar;charset=utf-8," + encodeURIComponent(text);
  a.download = filename;
  a.click();
}

// Filename-safe slug for a single event's .ics.
export const icsName = (title) =>
  (title || "event").replace(/[^\w-]+/g, "_").replace(/^_+|_+$/g, "").slice(0, 40) || "event";

// Reminder presets for an event, wired into lib/scheduled's machinery by the
// card. Only offsets still in the future are offered — "1 day before" an
// event tomorrow morning is an alarm for last night.
export function eventReminderTimes(startUnix) {
  const start = startUnix * 1000;
  return [
    { label: "10 minutes before", at: start - 10 * 60000 },
    { label: "1 hour before", at: start - 60 * 60000 },
    { label: "1 day before", at: start - 24 * 60 * 60000 },
    { label: "When it starts", at: start },
  ].filter((o) => o.at > Date.now());
}
