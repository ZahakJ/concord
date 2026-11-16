import { strandedChoice, erasedAfterUnlink, eraseUnfinished, lockNotice } from "./strandeddata.js";

let failures = 0;
const check = (what, ok) => {
  if (!ok) {
    console.error(`  FAIL ${what}`);
    failures++;
  }
};
const plain = (s) => !/\bapp:|keystore|marker|record|erase-in-progress/i.test(s);

const kept = strandedChoice(false);
check("never linked: erase first, keep for a restore second", kept.erase === "Erase it" && /restore/.test(kept.other) && kept.keepable);
check("never linked: the second option is not a cancel", !/cancel/i.test(kept.other));
check("never linked: asks to erase", /can no longer open/.test(kept.title));
const gone = strandedChoice(true);
check("linked: erase or quit", gone.erase === "Erase it" && gone.other === "Quit" && !gone.keepable);
check("linked: says nobody can open it", /Nobody can open this data/.test(gone.body));
check("unlink notice says it was erased", /erased/.test(erasedAfterUnlink));
const held = eraseUnfinished(["concord.db-wal"]);
check("held file is named", held.includes("concord.db-wal"));
check("held file says how to fix it", /Close any other Concord windows/.test(held) && /restart/.test(held));
check("two files are both named", eraseUnfinished(["a", "b"]).includes("a, b"));
check("one file is an it", held.includes("The file concord.db-wal is still here, probably because another program is using it."));
check("two files are a them", eraseUnfinished(["a", "b"]).includes("The files a, b are still here, probably because another program is using them."));
check("unknown files are a them", eraseUnfinished([]).includes("Some of its files are still here, probably because another program is using them."));
check("never linked: the body as approved", kept.body === "Concord found an account's messages and settings here, but not the key that unlocks them. You can erase them and start fresh, or keep them and restore the account with its recovery phrase.");
check("never linked: the keep button as approved", kept.other === "Keep it and restore");
check("linked: the body as approved", gone.body === "This device was linked to an account, and the key that unlocked its data is gone. Nobody can open this data again, not even with the recovery phrase. Erase it to use Concord on this device, or quit and leave it as it is.");
check("unlink notice as approved", erasedAfterUnlink === "This device was unlinked from its account, so Concord erased everything it held: your identity, guilds and messages. Your other devices still have them, and nothing changed for anyone else. To use this device again, link it from one of your other devices.");
check("held file as approved", held === "Concord couldn't finish erasing this device. The file concord.db-wal is still here, probably because another program is using it. Close any other Concord windows or restart the computer. Concord finishes erasing the next time it starts.");
check("lock notice after a restart", lockNotice("unlinked") === erasedAfterUnlink && lockNotice("unlinked-not-erased", ["x"]).includes("x") && lockNotice("restarted") === "");
for (const s of [kept.title, kept.body, kept.other, gone.title, gone.body, erasedAfterUnlink, held]) check(`plain: ${s.slice(0, 30)}`, plain(s));

if (failures) {
  console.error(`strandeddata: ${failures} failed`);
  process.exit(1);
}
console.log("strandeddata: ok");
