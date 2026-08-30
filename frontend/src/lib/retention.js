// retention.js — one list of how long a guild or a channel keeps messages.
//
// Guild-wide Message history and per-channel Channel settings used to offer
// different sets of the same idea: the guild had "Keep everything" and 1 year,
// the channel had "Forever" and stopped at 90 days. A guild set to 1 year
// could not have a channel explicitly set to 1 year, and the two labels for
// "never prune" read as two different policies. Both screens read this table.

export const RETAIN_FOREVER = 0;

export const RETAIN_OPTIONS = [
  { secs: 0, label: "Keep everything", sub: "No messages are ever removed by age" },
  { secs: 86400, label: "24 hours", sub: "" },
  { secs: 7 * 86400, label: "7 days", sub: "" },
  { secs: 30 * 86400, label: "30 days", sub: "" },
  { secs: 90 * 86400, label: "90 days", sub: "" },
  { secs: 365 * 86400, label: "1 year", sub: "" },
];

export function retainLabel(secs) {
  return RETAIN_OPTIONS.find((r) => r.secs === secs)?.label || `${secs}s`;
}
