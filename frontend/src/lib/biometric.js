// biometric.js — optional Face ID / fingerprint unlock. Wraps the
// capacitor-native-biometric plugin (reached via the runtime global so the
// web/desktop bundle carries no Capacitor dependency). The passphrase is stored
// in the iOS Keychain / Android Keystore; verifyIdentity() gates retrieval, so
// unlocking still requires the user's biometric. All functions no-op / return
// false off-device.

const SERVER = "app.concord.mobile";
// Set once the user opts in, so the unlock screen knows to offer biometric
// without having to prompt the OS just to probe for stored credentials.
const FLAG = "concord.bioEnrolled";

function plugin() {
  return window.Capacitor?.Plugins?.NativeBiometric || null;
}

// available reports whether the device has usable biometric hardware enrolled.
export async function bioAvailable() {
  const p = plugin();
  if (!p) return false;
  try {
    const r = await p.isAvailable();
    return !!r?.isAvailable;
  } catch {
    return false;
  }
}

// bioEnrolled reports whether the user has stored their passphrase for biometric
// unlock on this device.
export function bioEnrolled() {
  try {
    return localStorage.getItem(FLAG) === "1";
  } catch {
    return false;
  }
}

// enableBiometric stores the passphrase behind the device biometric. Returns
// true on success. Called after a successful password unlock when the user opts
// in.
export async function enableBiometric(passphrase) {
  const p = plugin();
  if (!p || !passphrase) return false;
  try {
    await p.setCredentials({ username: "concord", password: passphrase, server: SERVER });
    localStorage.setItem(FLAG, "1");
    return true;
  } catch {
    return false;
  }
}

// disableBiometric forgets the stored passphrase.
export async function disableBiometric() {
  const p = plugin();
  try {
    localStorage.removeItem(FLAG);
    await p?.deleteCredentials({ server: SERVER });
  } catch {
    /* ignore */
  }
}

// unlockWithBiometric prompts for the biometric, then returns the stored
// passphrase (or "" if it failed / was cancelled). The caller feeds it to
// api.login exactly like a typed passphrase.
export async function unlockWithBiometric() {
  const p = plugin();
  if (!p) return "";
  try {
    await p.verifyIdentity({
      reason: "Unlock Concord",
      title: "Unlock Concord",
      subtitle: "",
      description: "",
    });
    const c = await p.getCredentials({ server: SERVER });
    return c?.password || "";
  } catch {
    // Verification cancelled/failed, or credentials were cleared.
    return "";
  }
}
