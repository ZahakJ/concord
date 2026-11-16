// Plain words for what the core reports about this device's data before
// anything is unlocked: data it can no longer open, an erase that has not
// finished, and an erase that an unlink ran. The core sends codes; these are
// the only sentences the screens show for them.

// strandedChoice is the first screen for a device holding account data with
// no keystore. linked: the data was a linked device's, which nobody can
// open again, so there is nothing to keep it for.
export function strandedChoice(linked) {
  if (linked) {
    return {
      title: "This device holds data nobody can open",
      body:
        "This device was linked to an account, and the key that unlocked its data is gone. Nobody can open this data again, not even with the recovery phrase. Erase it to use Concord on this device, or quit and leave it as it is.",
      erase: "Erase it",
      other: "Quit",
      keepable: false,
    };
  }
  return {
    title: "This device holds data it can no longer open",
    body:
      "Concord found an account's messages and settings here, but not the key that unlocks them. You can erase them and start fresh, or keep them and restore the account with its recovery phrase.",
    erase: "Erase it",
    other: "Keep it and restore",
    keepable: true,
  };
}

// erasedAfterUnlink is said when a start finished erasing a device that
// was unlinked from its account.
export const erasedAfterUnlink =
  "This device was unlinked from its account, so Concord erased everything it held: your identity, guilds and messages. Your other devices still have them, and nothing changed for anyone else. To use this device again, link it from one of your other devices.";

// eraseUnfinished is said when files could not be removed. files: their
// names, as the core reports them.
export function eraseUnfinished(files) {
  const list = (files || []).filter(Boolean);
  const one = list.length === 1;
  const what = list.length === 0 ? "Some of its files are" : one ? `The file ${list[0]} is` : `The files ${list.join(", ")} are`;
  return `Concord couldn't finish erasing this device. ${what} still here, probably because another program is using ${one ? "it" : "them"}. Close any other Concord windows or restart the computer. Concord finishes erasing the next time it starts.`;
}

// lockNotice is the lock screen's sentence for why the device is locked,
// from a stored reason (SessionState), or "" when there is nothing to say.
export function lockNotice(reason, files) {
  if (reason === "unlinked") return erasedAfterUnlink;
  if (reason === "unlinked-not-erased") return eraseUnfinished(files);
  return "";
}

// quitClosed is said once Quit was asked for and the window is still there
// (a browser tab the page cannot close). The screen keeps no button: nothing
// is opened or created from it.
export const quitClosed = "You can close this window now.";

// quitApp ends the app the way its shell can: Capacitor's App plugin on
// Android, Wails on the desktop, or else window.close, which a browser
// ignores for a tab the page did not open. Returns the way it took.
export function quitApp(win) {
  const app = win?.Capacitor?.Plugins?.App;
  if (typeof app?.exitApp === "function") {
    try {
      Promise.resolve(app.exitApp()).catch(() => {});
      return "android";
    } catch {}
  }
  if (typeof win?.runtime?.Quit === "function") {
    try {
      win.runtime.Quit();
      return "desktop";
    } catch {}
  }
  try {
    win?.close?.();
  } catch {}
  return "browser";
}
