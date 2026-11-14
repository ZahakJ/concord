// A device that has an account's data but not the keystore that opens it (a
// keystore lost, on a drive not mounted, not copied back yet). An unlock used
// to read that as an erase's leftovers, delete the data and create a new
// account; a restore of another account's phrase used to come up beside it.
// Neither may happen: nothing is deleted on an inference, the account's own
// phrase brings it back with its messages, and another account's phrase is
// asked about before anything is erased.
import fs from "node:fs";
import path from "node:path";
import { test } from "../harness.mjs";

// keystoreLost leaves the peer with its data and no keystore, stopped and
// started again (so nothing in the process has unlocked that keystore), and
// returns the size of its database for the "nothing deleted" checks.
async function keystoreLost(peer) {
  await peer.rpc("Logout");
  await peer.stop();
  fs.rmSync(path.join(peer.home, "keystore.json"));
  await peer.start();
  return fs.statSync(path.join(peer.home, "concord.db")).size;
}

function untouched(assert, peer, dbSize, what) {
  const db = path.join(peer.home, "concord.db");
  assert.ok(fs.existsSync(db), `${what}: the database is gone`);
  assert.equal(fs.statSync(db).size, dbSize, `${what}: the database changed`);
  assert.ok(!fs.existsSync(path.join(peer.home, "keystore.json")), `${what}: a keystore was created`);
}

async function fillRestore(page, phrase, pass) {
  await page.getByRole("textbox", { name: "Recovery phrase" }).fill(phrase);
  await page.getByPlaceholder("New passphrase").fill(pass);
  await page.getByPlaceholder("Confirm passphrase").fill(pass);
  await page.getByRole("button", { name: "Restore account" }).click();
}

test("data without a keystore: unlocking deletes nothing, and the account's own phrase brings it back with its messages", async ({ world, assert, waitFor, step }) => {
  const [alice] = await world.peers(1);
  const phrase = await alice.rpc("RevealMnemonic", alice.passphrase);
  const notes = (await alice.rpc("NotesDM")).channels[0].id;
  await alice.send(notes, "a note that has to survive");
  const dbSize = await keystoreLost(alice);

  step("the screen offers to create; creating is refused and the ways back are offered");
  const app = await alice.open({ unlock: false });
  const { page } = app;
  await page.goto(`${alice.url}/?t=${alice.token}`, { waitUntil: "domcontentloaded" });
  await page.getByRole("textbox", { name: "Your name" }).fill("Someone new");
  await page.getByPlaceholder("Choose a passphrase").fill("a-new-passphrase");
  await page.getByPlaceholder("Confirm passphrase").fill("a-new-passphrase");
  await page.getByRole("button", { name: "Create identity" }).click();
  await page.getByRole("button", { name: "Use my recovery phrase" }).waitFor();
  assert.ok(await page.getByRole("button", { name: "Re-link from my other device" }).isVisible(), "no re-link choice");
  assert.ok(await page.getByRole("button", { name: /start over/ }).isVisible(), "no start-over choice");
  assert.equal(await alice.rpc("Session"), false, "an account was set up beside the data");
  untouched(assert, alice, dbSize, "after the refused create");

  step("the account's own phrase brings it back, messages and all");
  await page.getByRole("button", { name: "Use my recovery phrase" }).click();
  await fillRestore(page, phrase, "the-restored-passphrase");
  await waitFor(async () => (await alice.rpc("Session")) === true, { message: "the own-phrase restore unlocks" });
  const me = await alice.rpc("Identity");
  assert.equal(me.fingerprint, alice.fingerprint, "a different account came back");
  const kept = (await alice.messages(notes)).map((m) => m.content);
  assert.ok(kept.includes("a note that has to survive"), `the messages did not survive: ${JSON.stringify(kept)}`);
});

test("data without a keystore: another account's phrase is asked about, declining keeps everything, agreeing erases and restores it", async ({ world, assert, waitFor, step }) => {
  const [alice, bashir] = await world.peers(2);
  const bashirPhrase = await bashir.rpc("RevealMnemonic", bashir.passphrase);
  const notes = (await alice.rpc("NotesDM")).channels[0].id;
  await alice.send(notes, "alice's note");
  const dbSize = await keystoreLost(alice);

  step("Bashir's phrase on Alice's data: asked, not restored beside it");
  const app = await alice.open({ unlock: false });
  const { page } = app;
  await page.goto(`${alice.url}/?t=${alice.token}`, { waitUntil: "domcontentloaded" });
  await page.getByRole("button", { name: "Restore from a recovery phrase" }).click();
  await fillRestore(page, bashirPhrase, "brand-new-pass");
  const ask = page.getByRole("alertdialog");
  await ask.waitFor();
  assert.match(await ask.innerText(), /different account/);
  assert.equal(await alice.rpc("Session"), false, "restored without asking");

  step("declining leaves everything as it was");
  await page.getByRole("button", { name: "Go back" }).click();
  untouched(assert, alice, dbSize, "after declining");

  step("agreeing erases Alice's data and restores Bashir");
  await fillRestore(page, bashirPhrase, "brand-new-pass");
  await page.getByRole("alertdialog").waitFor();
  await page.getByRole("button", { name: /^Erase .* and restore$/ }).click();
  await waitFor(async () => (await alice.rpc("Session")) === true, { message: "the confirmed restore unlocks" });
  const me = await alice.rpc("Identity");
  assert.equal(me.fingerprint, bashir.fingerprint, "the device does not hold the phrase's account");
  assert.notEqual(me.displayName, "Alice", "the restored account came up with Alice's profile");
});
