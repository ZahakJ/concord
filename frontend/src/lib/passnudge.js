// passnudge.js — when to ask a fingerprint user whether they still know their
// passphrase (PassNudge.svelte). Rare and skippable: only after at least
// MIN_UNLOCKS fingerprint unlocks and GAP_DAYS since the passphrase was last
// typed or the card was last put off. Counters live on this device only;
// nothing here is security state.

const K_SEEN = "concord.passSeenAt";
const K_COUNT = "concord.bioUnlocks";
export const MIN_UNLOCKS = 3;
export const GAP_DAYS = 14;
const DAY = 86_400_000;

function get(k) {
  try {
    return localStorage.getItem(k);
  } catch {
    return null;
  }
}
function set(k, v) {
  try {
    localStorage.setItem(k, String(v));
  } catch {
    /* storage unavailable: the card simply never shows */
  }
}

// passphraseUsed: the passphrase was typed (an unlock, or the card's check).
export function passphraseUsed(now = Date.now()) {
  set(K_SEEN, now);
  set(K_COUNT, 0);
}

// fingerprintUsed: an unlock (or the App Lock gate) went by fingerprint.
export function fingerprintUsed() {
  set(K_COUNT, (Number(get(K_COUNT)) || 0) + 1);
}

// snoozeNudge: "Not now" puts it off for another GAP_DAYS.
export function snoozeNudge(now = Date.now()) {
  set(K_SEEN, now);
}

export function nudgeDue(now = Date.now()) {
  const seen = Number(get(K_SEEN));
  const count = Number(get(K_COUNT)) || 0;
  if (!seen) {
    // Never recorded (an install that predates the card): start the clock.
    if (count > 0) set(K_SEEN, now);
    return false;
  }
  return count >= MIN_UNLOCKS && now - seen >= GAP_DAYS * DAY;
}
