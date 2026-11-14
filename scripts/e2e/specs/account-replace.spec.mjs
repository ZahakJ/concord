// The forgotten-passphrase restore used to erase the locked account even when
// the recovery phrase belonged to somebody else, with no question asked.
import { test } from "../harness.mjs";

async function toRestoreForm(app) {
  const { page } = app;
  await page.getByRole("button", { name: "Forgot passphrase?" }).click();
  await page.getByRole("button", { name: "Use my recovery phrase" }).click();
}

async function fillRestore(app, phrase, pass) {
  const { page } = app;
  await page.getByRole("textbox", { name: "Recovery phrase" }).fill(phrase);
  await page.getByPlaceholder("New passphrase").fill(pass);
  await page.getByPlaceholder("Confirm passphrase").fill(pass);
  await page.getByRole("button", { name: "Restore account" }).click();
}

test("a recovery phrase for another account asks before erasing the one here", async ({ world, assert, waitFor, step }) => {
  const [alice, bashir] = await world.peers(2);
  const bashirPhrase = await bashir.rpc("RevealMnemonic", bashir.passphrase);
  const alicePhrase = await alice.rpc("RevealMnemonic", alice.passphrase);
  await alice.rpc("Logout");

  step("Alice's locked device, Bashir's phrase");
  const app = await alice.open({ unlock: false });
  await app.page.goto(`${alice.url}/?t=${alice.token}`, { waitUntil: "domcontentloaded" });
  await toRestoreForm(app);
  await fillRestore(app, bashirPhrase, "brand-new-pass");
  const ask = app.page.getByRole("alertdialog");
  await ask.waitFor();
  const text = await ask.innerText();
  assert.match(text, /different account/);
  assert.match(text, /Alice/);
  assert.match(text, /Nothing has been changed yet/);
  const locked = await alice.rpc("LockedAccount");
  assert.equal(locked.fingerprint, alice.fingerprint, "the refused restore left the device as it was");

  step("Back, and her own phrase keeps everything");
  await app.page.getByRole("button", { name: "Go back" }).click();
  await fillRestore(app, alicePhrase, "brand-new-pass");
  await waitFor(async () => (await alice.rpc("Session")) === true, { message: "own-phrase restore unlocks" });
  const me = await alice.rpc("Identity");
  assert.equal(me.fingerprint, alice.fingerprint);
  assert.equal(me.displayName, "Alice");
});

test("agreeing in the dialog erases the account it named and restores the other", async ({ world, assert, waitFor, step }) => {
  const [alice, bashir] = await world.peers(2);
  const bashirPhrase = await bashir.rpc("RevealMnemonic", bashir.passphrase);
  await alice.rpc("Logout");

  step("Alice's locked device, Bashir's phrase, and the yes");
  const app = await alice.open({ unlock: false });
  await app.page.goto(`${alice.url}/?t=${alice.token}`, { waitUntil: "domcontentloaded" });
  await toRestoreForm(app);
  await fillRestore(app, bashirPhrase, "brand-new-pass");
  const ask = app.page.getByRole("alertdialog");
  await ask.waitFor();
  assert.match(await ask.innerText(), /Alice/);
  await app.page.getByRole("button", { name: /^Erase .* and restore$/ }).click();
  await waitFor(async () => (await alice.rpc("Session")) === true, { message: "the confirmed restore unlocks" });
  const me = await alice.rpc("Identity");
  assert.equal(me.fingerprint, bashir.fingerprint, "the device now holds the phrase's account");
});

test("a device nothing has unlocked since it started says what its files claim, and asks even for its own phrase", async ({ world, assert, step }) => {
  const [alice, bashir] = await world.peers(2);
  const bashirPhrase = await bashir.rpc("RevealMnemonic", bashir.passphrase);
  const alicePhrase = await alice.rpc("RevealMnemonic", alice.passphrase);
  await alice.rpc("Logout");
  // A fresh process: the keystore's clear account key is now only a claim,
  // and Alice has no messages for Concord to check it against.
  await alice.stop();
  await alice.start();

  step("another account's phrase: the file's claim, marked as one");
  const app = await alice.open({ unlock: false });
  await app.page.goto(`${alice.url}/?t=${alice.token}`, { waitUntil: "domcontentloaded" });
  await toRestoreForm(app);
  await fillRestore(app, bashirPhrase, "brand-new-pass");
  const ask = app.page.getByRole("alertdialog");
  await ask.waitFor();
  const text = await ask.innerText();
  assert.match(text, /can't confirm which account is on this device/);
  assert.match(text, /its files say Alice/);
  await app.screenshot(`${world.dir}/unconfirmed-device.png`);

  step("her own phrase is asked about too: nothing can prove it is hers");
  await app.page.getByRole("button", { name: "Go back" }).click();
  await fillRestore(app, alicePhrase, "brand-new-pass");
  await app.page.getByRole("alertdialog").waitFor();
  assert.equal(await alice.rpc("Session"), false, "nothing was restored without a confirmation");
});
