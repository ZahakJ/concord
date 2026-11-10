// linkwatch.js — telling the device that issued a link code that it worked.
//
// The issuing side had no word back: the new device linked in two seconds and
// the issuer kept showing the same, now spent, QR under a warning that anyone
// scanning it "within the next couple of minutes" could add a device. The only
// thing that changes on this side when a link lands is the device list, so the
// dialog watches it and calls the first new key the newcomer.

// newlyLinked returns the device in `now` whose key was not in `before` (and is
// not this one), or null.
export function newlyLinked(before, now) {
  const known = new Set((before || []).map((d) => d.key));
  return (now || []).find((d) => !known.has(d.key) && !d.thisOne && !d.revokedAt) || null;
}
