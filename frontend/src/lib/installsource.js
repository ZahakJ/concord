// Where this copy of Concord came from, and therefore whether it is allowed to
// update itself.
//
// Concord's self-update is real: it fetches a signed build from the release
// feed or straight from a peer and hands it to the OS installer. For someone
// who downloaded the APK from a GitHub release that is the only way they will
// ever see a fix, so it has to keep working. For a copy installed from Play it
// is a Device and Network Abuse violation, and the penalty there is suspension,
// not a rejected upload.
//
// Both are the same binary. Only the running install knows which it is, so the
// question is asked at runtime, through the native shell.

const PLAY = "com.android.vending";

/** The Capacitor platform: "android", "ios", or "web" (which covers the
 *  desktop window too — Wails is a webview, not Capacitor). */
export function platform() {
  return window.Capacitor?.getPlatform?.() ?? "web";
}

/**
 * Whether to offer in-app updating on this install.
 *
 * - web/desktop: yes. These builds swap their own binary and always have.
 * - iOS: no. The App Store is the only channel; there is nothing to offer.
 * - Android: only when the installer is demonstrably not Play. A null
 *   installer is a plain sideload (adb, a file manager, a browser download)
 *   and keeps the feature; any other package — F-Droid, a vendor store — is
 *   likewise not Play and keeps it.
 *
 * Anything that goes wrong on Android answers "no". That is the opposite of
 * how the rest of this file's siblings fail, and deliberately so: the cost of
 * wrongly hiding the card is that one sideloader downloads an APK by hand, and
 * the cost of wrongly showing it is the app coming off the store.
 */
export async function selfUpdateAllowed() {
  const p = platform();
  if (p === "ios") return false;
  if (p !== "android") return true;

  const core = window.Capacitor?.Plugins?.ConcordCore;
  if (!core?.installerSource) return false;
  try {
    const { installer } = (await core.installerSource()) ?? {};
    return installer !== PLAY;
  } catch {
    return false;
  }
}
