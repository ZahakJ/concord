// unlock.js — when an unlock is actually over, and what to say when it isn't.
//
// THE BUG THIS EXISTS FOR. Entering the right passphrase is the middle of the
// unlock, not the end of it. `api.login()` resolving means the identity opened
// and the Go service started; the app is still not on screen until the shell
// has fetched the identity, the guild list, the first channel's history and the
// member panel. Login.svelte used to treat the first half as the whole thing —
// it called the second half without awaiting it — so every failure in that
// second half escaped the try/catch it looked like it was inside, became an
// unhandled promise rejection, and reached nobody. The window was still the
// login screen, the form re-enabled itself, the error slot stayed empty, and
// the passphrase you had just typed correctly appeared to do nothing at all.
//
// Reproduced on a real install whose guild rows had outlived their MLS group
// state: the member-panel fetch failed with "mls: group not found", and that
// was the entire user-visible consequence — a login screen that would not
// leave. Not a hypothetical. The account was intact the whole time.
//
// So: the second half runs THROUGH here, one await, one rejection, and the
// caller's existing catch renders it. The wrapper is what makes the difference
// sayable — "wrong passphrase" and "the passphrase was right and something
// afterwards broke" are opposite instructions to the person reading them, and
// before this they were the same blank screen.

// UnlockError marks a failure that happened AFTER the passphrase was accepted.
// The distinction is the point: it tells the reader not to go looking for a
// typo, and it tells us, from a screenshot alone, which half of the unlock to
// look at.
export class UnlockError extends Error {
  constructor(message, cause) {
    super(message);
    this.name = "UnlockError";
    // Kept alongside the rendered sentence so a caller that logs rather than
    // renders still has the original words.
    this.cause = cause;
  }
}

// afterUnlockMessage builds what the login screen says. `humanise` is the app's
// humanError — passed in rather than imported so this module stays a leaf with
// no dependencies and the test can drive it directly.
export function afterUnlockMessage(cause, humanise = (x) => String(x?.message ?? x ?? "")) {
  const detail = String(humanise(cause) ?? "").trim();
  const head = "Your passphrase was accepted, but Concord couldn't finish opening.";
  // A detail that already ends in punctuation should not collect a second one,
  // and one that is empty must not leave a dangling space.
  return detail ? `${head} ${detail}` : head;
}

// finishUnlock runs the post-passphrase half of an unlock and converts anything
// it throws into an UnlockError.
//
// It deliberately does NOT swallow: the caller's catch is what puts the message
// on screen, and a helper here that quietly returned would rebuild the exact
// bug this file is named after.
export async function finishUnlock(open, humanise) {
  try {
    await open();
  } catch (cause) {
    throw new UnlockError(afterUnlockMessage(cause, humanise), cause);
  }
}
