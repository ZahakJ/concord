// appbus.js — the one place that decides whether a message is machine traffic.
//
// Other local apps (sentinel, trove, …) push structured payloads into Concord
// channels over the "app-bus". Those payloads are data, not conversation: they
// must never appear in a human's feed, light an unread badge, ping anybody,
// become a reply target, or turn up in search. They surface only in the Apps
// view, which is the point of keeping them addressable at all.
//
// There are two generations of sender, so there are two rules:
//
//   1. Current senders set `kind: "app"` on the message.
//   2. Older senders — still running, on other machines, on their own release
//      cadence — send an ORDINARY chat message whose first line is
//      "APPBUS:<app>:<schema-version>" with a JSON body after it. We keep
//      matching that prefix, and must: requiring every producer to ship the
//      new kind in lockstep would mean either breaking them or leaving their
//      traffic in #general until the last one upgraded. Matching the prefix
//      also retroactively hides the payloads already sitting in people's
//      channels, which is most of why this exists.
//
// This mirrors domain.Message.IsApp / AppBusApp on the Go side. The prefix
// literal lives here and nowhere else in the frontend — if you find yourself
// typing "APPBUS:" in a component, import from this file instead.

const APPBUS_PREFIX = "APPBUS:";

// isAppMessage reports whether a message is app-plane traffic rather than
// something a person said. Every renderer, counter, notifier and search path
// asks this instead of comparing `kind` directly.
export function isAppMessage(m) {
  if (!m) return false;
  if (m.kind === "app") return true;
  return typeof m.content === "string" && m.content.startsWith(APPBUS_PREFIX);
}

// parseAppMessage pulls an app payload apart for display in the Apps view:
// the producing app's name and schema version from the header line, and the
// body below it.
//
// A message that only carries `kind:"app"` (no header line) is perfectly
// legal — the app name is simply unknown, and the whole content is the
// payload. We report that honestly rather than guessing a name.
export function parseAppMessage(m) {
  const content = typeof m?.content === "string" ? m.content : "";
  if (!content.startsWith(APPBUS_PREFIX)) return { app: "", version: "", payload: content };
  const nl = content.indexOf("\n");
  const header = (nl === -1 ? content : content.slice(0, nl)).slice(APPBUS_PREFIX.length);
  const payload = nl === -1 ? "" : content.slice(nl + 1);
  // "<app>:<version>" — but a producer that sent only "<app>" still parses,
  // with an empty version, rather than losing the name entirely.
  const sep = header.indexOf(":");
  const app = sep === -1 ? header : header.slice(0, sep);
  const version = sep === -1 ? "" : header.slice(sep + 1);
  return { app: app.trim(), version: version.trim(), payload };
}
