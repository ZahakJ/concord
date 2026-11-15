// The forgotten-passphrase restore used to erase the locked account even when
// the recovery phrase belonged to somebody else, with no question asked. Then
// the question's yes erased it in one tap, while starting over took ERASE
// typed; now the yes takes the same word, and keeping comes first.
import { test } from "../harness.mjs";

async function toRestoreForm(app) {
  const { page } = app;
  await page.getByRole("button", { name: "Forgot passphrase?" }).click();
  await page.getByRole("button", { name: "Use my recovery phrase" }).click();
}

// The replace question's yes: the erase button is disabled until ERASE is
// typed, then pressed.
async function typeEraseAndGo(page, assert, name) {
  const yes = page.getByRole("button", { name });
  await yes.waitFor();
  assert.ok(await yes.isDisabled(), "the erase is enabled before the word is typed");
  await page.getByRole("textbox", { name: "Type ERASE to confirm" }).fill("ERASE");
  await yes.click();
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
  await app.page.getByRole("button", { name: "Keep everything here" }).click();
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
  await typeEraseAndGo(app.page, assert, "Erase Alice and restore");
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
  await app.page.getByRole("button", { name: "Keep everything here" }).click();
  await fillRestore(app, alicePhrase, "brand-new-pass");
  await app.page.getByRole("alertdialog").waitFor();
  assert.equal(await alice.rpc("Session"), false, "nothing was restored without a confirmation");
});

test("the replace question erases nothing in one tap: keeping comes first, and erasing takes ERASE typed", async ({ world, assert, waitFor, step }) => {
  const [alice, bashir] = await world.peers(2);
  const bashirPhrase = await bashir.rpc("RevealMnemonic", bashir.passphrase);
  await alice.rpc("Logout");
  const app = await alice.open({ unlock: false, viewport: "phone" });
  const { page } = app;
  await page.goto(`${alice.url}/?t=${alice.token}`, { waitUntil: "domcontentloaded" });
  await toRestoreForm(app);
  await fillRestore(app, bashirPhrase, "brand-new-pass");
  await page.getByRole("alertdialog").waitFor();

  step("the erase is disabled with nothing typed; keep is first and full width, the erase after it and narrower");
  const keep = page.getByRole("button", { name: "Keep everything here" });
  const yes = page.getByRole("button", { name: "Erase Alice and restore" });
  const word = page.getByRole("textbox", { name: "Type ERASE to confirm" });
  await yes.waitFor();
  assert.ok(await yes.isDisabled(), "the erase is enabled with nothing typed: one tap erases Alice");
  const [k, y] = [await keep.boundingBox(), await yes.boundingBox()];
  assert.ok(k.y < y.y, "the erase comes before keeping");
  assert.ok(y.width < k.width, "the erase is as wide as the safe choice");

  step("a tap, a wrong word, or Enter with it erase nothing");
  await yes.click({ force: true });
  for (const wrong of ["ERAS", "Alice", "delete"]) {
    await word.fill(wrong);
    assert.ok(await yes.isDisabled(), `the erase is enabled with "${wrong}" typed`);
    await word.press("Enter");
  }
  assert.ok(await word.isVisible(), "the question went away without a yes");
  assert.equal(await alice.rpc("Session"), false, "something was restored without the word");
  assert.equal((await alice.rpc("LockedAccount")).fingerprint, alice.fingerprint, "the account here changed without the word");

  step("with the word typed, Enter is the yes");
  await word.fill(" erase ");
  assert.ok(await yes.isEnabled(), "the erase stays disabled with the word typed");
  await word.press("Enter");
  await waitFor(async () => (await alice.rpc("Session")) === true, { message: "the confirmed restore unlocks" });
  assert.equal((await alice.rpc("Identity")).fingerprint, bashir.fingerprint);
});

test("a link code for another account: keep first, and the erase waits for the word and a new code", async ({ world, assert, waitFor, step }) => {
  const [alice, bashir] = await world.peers(2);
  await alice.rpc("Logout");
  const app = await alice.open({ unlock: false, viewport: "phone" });
  const { page } = app;
  await page.goto(`${alice.url}/?t=${alice.token}`, { waitUntil: "domcontentloaded" });

  step("a wrong passphrase points at the ways back in");
  const pass = page.locator('input[placeholder="Passphrase"]');
  await pass.fill("not-alices-passphrase");
  await pass.press("Enter");
  await page.getByText("below has the ways back in").waitFor();

  step("Bashir's link code on Alice's locked device is asked about");
  await page.getByRole("button", { name: "Re-link from another device" }).click();
  await page.getByRole("textbox", { name: "Link code" }).fill(await bashir.rpc("LinkOffer"));
  await page.getByPlaceholder("Passphrase for this device").fill("brand-new-pass");
  await page.getByPlaceholder("Confirm passphrase").fill("brand-new-pass");
  await page.getByRole("button", { name: "Link this device" }).click();
  await page.getByRole("alertdialog").waitFor({ timeout: 60000 });
  const keep = page.getByRole("button", { name: "Keep everything here" });
  const yes = page.getByRole("button", { name: /^Erase .* and link$/ });
  const word = page.getByRole("textbox", { name: "Type ERASE to confirm" });
  const code = page.getByRole("textbox", { name: "New link code" });
  assert.ok(await yes.isDisabled(), "the erase is enabled with nothing typed");
  const [k, y] = [await keep.boundingBox(), await yes.boundingBox()];
  assert.ok(k.y < y.y && y.width < k.width, "the erase is not after and narrower than keeping");

  step("the word alone, or a new code alone, is not a yes");
  await word.fill("ERASE");
  assert.ok(await yes.isDisabled(), "the erase is enabled without a new code");
  await code.fill(await bashir.rpc("LinkOffer"));
  await word.fill("");
  assert.ok(await yes.isDisabled(), "the erase is enabled without the word");
  await word.press("Enter");
  assert.equal((await alice.rpc("LockedAccount")).fingerprint, alice.fingerprint, "the account here changed without a yes");

  step("both, and the device holds Bashir's account");
  await word.fill("ERASE");
  await yes.click();
  await waitFor(async () => (await alice.rpc("Session")) === true, { timeout: 90000, message: "the confirmed link unlocks" });
  assert.equal((await alice.rpc("Identity")).fingerprint, bashir.fingerprint, "the device does not hold the code's account");
});

test("the core takes no yes without the word, whoever calls it", async ({ world, assert, step }) => {
  const [alice, bashir] = await world.peers(2);
  const bashirPhrase = await bashir.rpc("RevealMnemonic", bashir.passphrase);
  await alice.rpc("Logout");
  const chk = await alice.rpc("CheckRecoveryPhrase", bashirPhrase);
  assert.ok(chk.confirm, "no ticket for a restore that erases");
  for (const typed of ["", "Alice", "ERAS"]) {
    await assert.rejects(alice.rpc("RestoreOverExisting", bashirPhrase, "brand-new-pass", chk.confirm, typed), /type ERASE/, `a yes with "${typed}" typed`);
  }
  assert.equal((await alice.rpc("LockedAccount")).fingerprint, alice.fingerprint, "a yes without the word erased Alice");
});
