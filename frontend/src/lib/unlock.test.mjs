// Zero-dependency test for the post-passphrase half of an unlock (`npm test`).
//
// The regression it holds down is the one a real user hit: the app accepted the
// passphrase, the init that follows it threw, and the login screen showed
// nothing at all. So the assertions are about REACHING the caller's catch —
// not about the wording. A helper that resolved on failure would satisfy a
// "does it produce a message" test and reproduce the bug exactly.
import { finishUnlock, afterUnlockMessage, UnlockError } from "./unlock.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

// ---- the happy path is transparent -------------------------------------
{
  let ran = false;
  await finishUnlock(async () => {
    ran = true;
  });
  assert(ran, "the init step runs");
}

// ---- a failing init REJECTS, so the caller's catch can render it --------
// This is the whole bug: the old code called the init without awaiting, so a
// rejection here went to window.onunhandledrejection and nowhere else.
{
  let caught = null;
  let finallyRan = false;
  try {
    await finishUnlock(async () => {
      throw new Error("mls: list members: mls: group not found");
    });
    assert(false, "a failing init must not resolve");
  } catch (err) {
    caught = err;
  } finally {
    finallyRan = true;
  }
  assert(caught instanceof UnlockError, "the failure arrives as an UnlockError");
  assert(finallyRan, "the caller's finally still runs, so the form re-enables");
  assert(
    caught.message.includes("passphrase was accepted"),
    `says the passphrase was not the problem — got ${JSON.stringify(caught.message)}`,
  );
  assert(
    caught.message.includes("group not found"),
    `carries the underlying detail — got ${JSON.stringify(caught.message)}`,
  );
  assert(caught.cause instanceof Error, "the original error is kept for the log");
}

// ---- a synchronous throw is caught too ---------------------------------
{
  let caught = null;
  try {
    await finishUnlock(() => {
      throw new Error("boom");
    });
  } catch (err) {
    caught = err;
  }
  assert(caught instanceof UnlockError, "a synchronous throw is still an UnlockError");
}

// ---- the detail goes through the app's humaniser ------------------------
// humanError strips Go package prefixes; the sentence must show the cleaned
// words, not "app: ...".
{
  const humanise = (e) => String(e?.message ?? e).replace(/^app:\s*/, "");
  const msg = afterUnlockMessage(new Error("app: this guild's membership is not readable"), humanise);
  assert(!msg.includes("app:"), `the Go prefix is stripped — got ${JSON.stringify(msg)}`);
  assert(msg.includes("membership is not readable"), "the cleaned detail survives");
}

// ---- a failure with nothing to say still says something -----------------
{
  const msg = afterUnlockMessage(new Error(""), () => "");
  assert(msg.length > 0, "an empty detail still produces a sentence");
  assert(!msg.endsWith(" "), "and does not end in a dangling space");
}

if (failures) {
  console.error(`unlock.test: ${failures} failure(s)`);
  process.exit(1);
}
console.log("unlock.test: ok");
