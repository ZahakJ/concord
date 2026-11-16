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
        "This device was linked to an account, and the key that unlocked its data is gone. Nobody can open this data again, not even with the recovery phrase. Erase it to use Concord here, or quit and leave it as it is.",
      erase: "Erase it",
      other: "Quit",
      keepable: false,
    };
  }
  return {
    title: "This device holds data it can no longer open",
    body:
      "Concord found an account's messages and settings here, but not the key that unlocks them. Erase it to start fresh, or keep it and restore the account with its recovery phrase, which opens it again.",
    erase: "Erase it",
    other: "Keep it and restore from my recovery phrase",
    keepable: true,
  };
}

// erasedAfterUnlink is said when a start finished erasing a device that
// was unlinked from its account.
export const erasedAfterUnlink =
  "This device was unlinked from its account, so Concord erased its copy of your identity, guilds and messages. Nothing was deleted from anyone else. Link it again from one of your other devices to get them back.";

// eraseUnfinished is said when files could not be removed. files: their
// names, as the core reports them.
export function eraseUnfinished(files) {
  const list = (files || []).filter(Boolean);
  const what = list.length === 0 ? "Some of its files are" : list.length === 1 ? `The file ${list[0]} is` : `The files ${list.join(", ")} are`;
  return `Concord couldn't finish erasing this device. ${what} still here, probably because another program is using them. Close any other Concord windows, or restart the computer, and Concord finishes erasing the next time it starts.`;
}

// lockNotice is the lock screen's sentence for why the device is locked,
// from a stored reason (SessionState), or "" when there is nothing to say.
export function lockNotice(reason, files) {
  if (reason === "unlinked") return erasedAfterUnlink;
  if (reason === "unlinked-not-erased") return eraseUnfinished(files);
  return "";
}
