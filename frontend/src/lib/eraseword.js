// The word typed to erase a device from the lock screen: to start over, or to
// let a recovery phrase or a link code replace the account on it. The core
// checks the same word for starting over (bridge.StartOverWord), trimmed and
// without regard to case. For a replace, the core's own check is the confirm
// token for one incoming account; the word is this screen's, so that erasing
// an account by restoring over it takes no less than erasing it outright.
export const ERASE_WORD = "ERASE";

export function eraseWordMatches(typed) {
  return String(typed ?? "").trim().toLowerCase() === ERASE_WORD.toLowerCase();
}

// replaceEraseReady says whether the replace confirmation may go ahead: the
// word typed, nothing in flight, and for a link a new code pasted (each code
// works once, and the one that brought the question is spent).
export function replaceEraseReady({ typed, busy = false, kind, linkCode = "" }) {
  if (busy || !eraseWordMatches(typed)) return false;
  if (kind === "link" && !String(linkCode ?? "").trim()) return false;
  return true;
}

// replaceEraseLabel is the erase button's text. It names the account on the
// device only when the core has confirmed which one it is (device.verified)
// and the text above says so; otherwise a name read from the device's files
// could be wrong, and the button would erase something it misnamed.
//   hasIdentity false: a keystore is missing and the data cannot be opened.
export function replaceEraseLabel({ match, kind, verified = false, name = "", hasIdentity = true }) {
  const then = kind === "link" ? "link" : "restore";
  if (!hasIdentity) return `Erase this data and ${then}`;
  if (match === "same") return `Erase this device's copy and ${then}`;
  if (verified && name && match !== "unknown") return `Erase ${name} and ${then}`;
  return `Erase the account on this device and ${then}`;
}
