// biometric.js — fingerprint unlock on the phone, as the page sees it.
//
// Everything secret stays native (BioUnlock.java) and in the Go core: a
// random secret U, wrapped by an Android Keystore key that a strong biometric
// must release, opens the keystore's device slot in process. The page never
// holds U or the passphrase for it; it asks, and hears an outcome code:
//   ok | cancelled | lockout | invalidated | no_slot | unavailable | failed
//
// 0.63 and earlier used capacitor-native-biometric, which stored the
// passphrase itself where any script in the page could read it. The phone
// deletes that on the first 0.64 start; the flag it kept here goes too.

const LEGACY_FLAG = "concord.bioEnrolled";
try {
  localStorage.removeItem(LEGACY_FLAG);
} catch {
  /* storage unavailable: nothing to clear */
}

function core() {
  return (typeof window !== "undefined" && window.Capacitor?.Plugins?.ConcordCore) || null;
}

function hasBio(p) {
  return !!(p && typeof p.bioState === "function");
}

// bioState reports {available, enrolled}: a strong biometric on this phone,
// and fingerprint unlock turned on (both halves of it present). Off the
// phone, or on a shell without it, both are false.
export async function bioState() {
  const p = core();
  if (!hasBio(p)) return { available: false, enrolled: false };
  try {
    const r = await p.bioState();
    return { available: !!r?.available, enrolled: !!r?.enrolled };
  } catch {
    return { available: false, enrolled: false };
  }
}

export async function bioAvailable() {
  return (await bioState()).available;
}

async function run(method, args) {
  const p = core();
  if (!hasBio(p)) return "unavailable";
  try {
    const r = await p[method](args || {});
    return r?.code || "failed";
  } catch {
    return "failed";
  }
}

// enableBiometric turns fingerprint unlock on. passphrase is the one typed in
// Settings; leave it out right after a passphrase unlock, which the core
// accepts as proof for a minute, once.
export function enableBiometric(passphrase = "") {
  return run("bioEnrol", passphrase ? { passphrase } : {});
}

// unlockWithBiometric unlocks with the fingerprint, or, with a session open,
// opens the App Lock gate. "ok" means the core did it; there is nothing to
// hand to login().
export function unlockWithBiometric() {
  return run("bioUnlock");
}

// disableBiometric turns fingerprint unlock off (in the keystore and on the
// phone). It throws when it could not, e.g. behind the App Lock gate.
export async function disableBiometric() {
  const p = core();
  if (!hasBio(p)) return;
  await p.bioDisable();
}

// bioMessage is what the lock screen says for an outcome, or "" for none.
export function bioMessage(code) {
  switch (code) {
    case "invalidated":
      return "Fingerprints on this phone changed, so Concord turned fingerprint unlock off. Unlock with your passphrase, then turn it on again in Settings.";
    case "lockout":
      return "Too many tries. Unlock with your passphrase, or wait and try your fingerprint again.";
    case "no_slot":
      return "Fingerprint unlock isn't set up on this phone any more. Unlock with your passphrase.";
    case "unavailable":
      return "This phone can't use fingerprint unlock right now. Unlock with your passphrase.";
    case "failed":
      return "Fingerprint unlock didn't work this time. Try again, or use your passphrase.";
    default:
      return "";
  }
}
