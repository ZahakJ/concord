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
check("never linked: erase first, keep for a restore second", kept.erase === "Erase it" && /recovery phrase/.test(kept.other) && kept.keepable);
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
check("lock notice after a restart", lockNotice("unlinked") === erasedAfterUnlink && lockNotice("unlinked-not-erased", ["x"]).includes("x") && lockNotice("restarted") === "");
for (const s of [kept.title, kept.body, kept.other, gone.title, gone.body, erasedAfterUnlink, held]) check(`plain: ${s.slice(0, 30)}`, plain(s));

if (failures) {
  console.error(`strandeddata: ${failures} failed`);
  process.exit(1);
}
console.log("strandeddata: ok");
