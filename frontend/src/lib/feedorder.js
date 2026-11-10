// feedorder.js — where an arriving message goes in the loaded feed.
//
// History comes back from the store sorted by the message's own `sent` time
// (ORDER BY sent). Live messages used to be appended in the order they reached
// this device, which over a network of peers is not the order they were
// written: three people typing at once each saw a different interleaving, and
// a reload re-sorted everyone's feed under them — rows visibly swapped places,
// and with them who "owned" a group header. A live message is placed by the
// same key the store sorts by, so what you see live is what you see after a
// reload, on every device.
//
// `sent` is the wire's fixed-width RFC 3339 with nanoseconds; the instant is
// compared, not the string, because the offset suffix is the sender's and two
// peers in different zones would otherwise sort by their clocks' spelling.
// Equal instants fall back to the id so the answer is the same everywhere.

function nanoOf(iso) {
  const s = String(iso || "");
  const ms = Date.parse(s);
  if (Number.isNaN(ms)) return null;
  const frac = /\.(\d+)/.exec(s);
  // Date.parse keeps milliseconds; the rest of the fraction breaks ties
  // inside one millisecond, which is exactly when a burst needs it.
  const sub = frac ? Number((frac[1] + "000000000").slice(3, 9)) : 0;
  return [ms, sub];
}

export function compareSent(a, b) {
  const x = nanoOf(a?.sent);
  const y = nanoOf(b?.sent);
  if (x && y) {
    if (x[0] !== y[0]) return x[0] - y[0];
    if (x[1] !== y[1]) return x[1] - y[1];
  } else if (x || y) {
    return x ? -1 : 1;
  }
  const ia = String(a?.id || "");
  const ib = String(b?.id || "");
  return ia < ib ? -1 : ia > ib ? 1 : 0;
}

// insertBySent returns a new array with m placed after every message that
// sorts at or before it. Searched from the end: a live message is nearly
// always the newest, so the common case touches one element.
export function insertBySent(list, m) {
  let i = list.length;
  while (i > 0 && compareSent(list[i - 1], m) > 0) i--;
  if (i === list.length) return [...list, m];
  return [...list.slice(0, i), m, ...list.slice(i)];
}
