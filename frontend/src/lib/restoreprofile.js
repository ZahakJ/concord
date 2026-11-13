// The restore screen waits for the account's profile to come back from its
// other devices or its guild-mates (internal/app/profilerecord.go). These are
// the decisions it makes while it waits, kept out of the component so they can
// be checked without one.

// shortFp is the first block of a fingerprint: what the core reports as the
// display name of an account that has none.
export const shortFp = (fp) => (fp ? String(fp).split(" ")[0] : "");

// foundName is the account's real name once its profile has arrived, "" while
// the identity still answers with the fingerprint stub (or nothing).
export function foundName(id) {
  const n = String(id?.displayName ?? "").trim();
  if (!n || n === shortFp(id?.fingerprint)) return "";
  return n;
}

// restoreStep says what the card shows. found: the name the core now reports
// for a profile that came back from elsewhere ("" while nothing has); typed:
// what the user has put in the name field; provisional: the name this screen
// itself saved, which must not be mistaken for a found profile.
//
//   "waiting"  nothing yet — say so, offer a name for now and a skip
//   "found"    the profile is back and nothing typed disagrees with it
//   "conflict" it came back while the user had typed a different name: ask,
//              never overwrite what they typed and never discard what came back
export function restoreStep({ found, typed, provisional = "" }) {
  const f = String(found ?? "").trim();
  if (!f || (provisional && f === provisional)) return "waiting";
  const t = String(typed ?? "").trim();
  if (!t || t === f) return "found";
  return "conflict";
}

// keepTypedArgs is SetProfile's argument list for "keep the name I typed": the
// profile that came back with only its name replaced. SetProfile takes the whole
// profile, so a list built from the name alone would erase the avatar, bio and
// status that just arrived — on this device and, as the newest edit, on every
// other device of the account.
export function keepTypedArgs(id, name) {
  const f = (k) => id?.[k] ?? "";
  return [
    name,
    f("status"),
    f("emoji"),
    f("color"),
    f("avatar"),
    f("banner"),
    f("presence"),
    f("bio"),
    f("color2"),
    f("frame"),
    f("effect"),
    id?.style ? JSON.stringify(id.style) : "",
  ];
}
