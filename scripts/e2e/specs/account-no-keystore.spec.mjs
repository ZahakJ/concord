// A device that has an account's data but not the keystore that opens it (a
// keystore lost, on a drive not mounted, not copied back yet). An unlock used
// to read that as an erase's leftovers, delete the data and create a new
// account; a restore of another account's phrase used to come up beside it.
// Neither may happen: nothing is deleted on an inference, the account's own
// phrase brings it back with its messages, and another account's phrase is
// asked about before anything is erased.
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { test } from "../harness.mjs";

const dbHash = (peer) =>
  crypto.createHash("sha256").update(fs.readFileSync(path.join(peer.home, "concord.db"))).digest("hex");

// keystoreLost leaves the peer with its data and no keystore, stopped and
// started again (so nothing in the process has unlocked that keystore), and
// returns its database's content hash for the "nothing deleted" checks.
async function keystoreLost(peer) {
  await peer.rpc("Logout");
  await peer.stop();
  fs.rmSync(path.join(peer.home, "keystore.json"));
  await peer.start();
  return dbHash(peer);
}

function untouched(assert, peer, before, what) {
  const db = path.join(peer.home, "concord.db");
  assert.ok(fs.existsSync(db), `${what}: the database is gone`);
  assert.equal(dbHash(peer), before, `${what}: the database's content changed`);
  assert.ok(!fs.existsSync(path.join(peer.home, "keystore.json")), `${what}: a keystore was created`);
}

// waysBack gets by the first screen's choice (account-stranded-first-
// screen.spec.mjs) to the ways back with "Keep it and restore". No screen
// from there leads to the create form while the data is here: Back from the
// restore and re-link screens and Cancel on the erase return to the ways back
// (ui-back.spec.mjs), so the core's refusal to create is asked of it directly.
async function waysBack(page) {
  await page.getByRole("button", { name: "Keep it and restore", exact: true }).click();
  await page.getByRole("button", { name: "Use my recovery phrase" }).waitFor();
}

async function refusedCreate(assert, peer) {
  await assert.rejects(peer.rpc("Login", "a-new-passphrase"), /Nothing was deleted, and no new account was created beside it/);
}

async function fillRestore(page, phrase, pass) {
  await page.getByRole("textbox", { name: "Recovery phrase" }).fill(phrase);
  await page.locator('input[aria-label="New passphrase"]').fill(pass);
  await page.locator('input[aria-label="Confirm passphrase"]').fill(pass);
  await page.getByRole("button", { name: "Restore account" }).click();
}

test("data without a keystore: unlocking deletes nothing, and the account's own phrase brings it back with its messages", async ({ world, assert, waitFor, step }) => {
  const [alice] = await world.peers(1);
  const phrase = await alice.rpc("RevealMnemonic", alice.passphrase);
  const notes = (await alice.rpc("NotesDM")).channels[0].id;
  await alice.send(notes, "a note that has to survive");
  const dbBefore = await keystoreLost(alice);

  step("creating is refused, and the ways back are worded for this data");
  const app = await alice.open({ unlock: false });
  const { page } = app;
  await page.goto(`${alice.url}/?t=${alice.token}`, { waitUntil: "domcontentloaded" });
  await refusedCreate(assert, alice);
  await waysBack(page);
  const text = await page.locator("form").innerText();
  assert.match(text, /holds data from an account it can't open/, "the choices are not worded for this state");
  // Never linked, with a message to check a phrase against: the core says
  // this data can be kept, and only then may the screen promise it.
  assert.match(text, /keeps everything here/, "the screen does not say the account's own phrase keeps the data");
  assert.match(text, /If it is this account's phrase, everything on this device is kept\./, "no keep hint");
  assert.ok(await page.getByRole("button", { name: "Re-link from my other device" }).isVisible(), "no re-link choice");
  assert.ok(await page.getByRole("button", { name: "Erase this data and start over" }).isVisible(), "no erase choice");
  assert.equal(await alice.rpc("Session"), false, "an account was set up beside the data");
  untouched(assert, alice, dbBefore, "after the refused create");

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
  const dbBefore = await keystoreLost(alice);

  step("Bashir's phrase on Alice's data: asked, not restored beside it");
  const app = await alice.open({ unlock: false });
  const { page } = app;
  await page.goto(`${alice.url}/?t=${alice.token}`, { waitUntil: "domcontentloaded" });
  await waysBack(page);
  await page.getByRole("button", { name: "Use my recovery phrase" }).click();
  await fillRestore(page, bashirPhrase, "brand-new-pass");
  const ask = page.getByRole("alertdialog");
  await ask.waitFor();
  const asked = await ask.innerText();
  assert.match(asked, /different account/);
  // The data's messages showed whose it is not; there is no keystore, so
  // nothing about a passphrase proving anything, and no account "here".
  assert.match(asked, /holds data from an account it can't open: its keystore is\s+missing/, `the no-keystore case is not described: ${asked}`);
  assert.match(asked, /Its messages show it is not the account the phrase\s+is for/, `the evidence is not named: ${asked}`);
  assert.doesNotMatch(asked, /passphrase can prove/, "the text speaks of a keystore that is not there");
  assert.equal(await alice.rpc("Session"), false, "restored without asking");

  step("declining leaves everything as it was");
  await page.getByRole("button", { name: "Keep everything here" }).click();
  untouched(assert, alice, dbBefore, "after declining");

  step("agreeing erases Alice's data and restores Bashir");
  await fillRestore(page, bashirPhrase, "brand-new-pass");
  await page.getByRole("alertdialog").waitFor();
  // No account here can be opened, so the button names none, and it waits
  // for the same word as erasing outright.
  const yes = page.getByRole("button", { name: "Erase this data and restore" });
  assert.ok(await yes.isDisabled(), "the erase is enabled before ERASE is typed");
  await page.getByRole("textbox", { name: "Type ERASE to confirm" }).fill("ERASE");
  await yes.click();
  await waitFor(async () => (await alice.rpc("Session")) === true, { message: "the confirmed restore unlocks" });
  const me = await alice.rpc("Identity");
  assert.equal(me.fingerprint, bashir.fingerprint, "the device does not hold the phrase's account");
  assert.notEqual(me.displayName, "Alice", "the restored account came up with Alice's profile");
  const left = await alice.messages(notes).catch(() => []);
  assert.ok(!left.some((m) => m.content === "alice's note"), "Alice's note survived the erase");
});

test("data without a keystore: erasing it takes the typed word and a current confirmation, and then a new account can be set up", async ({ world, assert, waitFor, step }) => {
  const [alice] = await world.peers(1);
  const notes = (await alice.rpc("NotesDM")).channels[0].id;
  await alice.send(notes, "erase me only if asked");
  const dbBefore = await keystoreLost(alice);
  const app = await alice.open({ unlock: false });
  const { page } = app;
  await page.goto(`${alice.url}/?t=${alice.token}`, { waitUntil: "domcontentloaded" });
  await refusedCreate(assert, alice);
  await waysBack(page);

  step("the core refuses an erase without the typed word or a current ticket, whoever calls it");
  const { ticket } = await alice.rpc("BeginStartOver");
  for (const args of [[], [ticket], [ticket, ""], [ticket, "delete"], [ticket, "Alice"]]) {
    await assert.rejects(alice.rpc("ResetIdentity", ...args), /type ERASE/, `ResetIdentity(${JSON.stringify(args)}) was not refused`);
  }
  for (const args of [["", "ERASE"], ["0123456789abcdef", "ERASE"]]) {
    await assert.rejects(alice.rpc("ResetIdentity", ...args), /Nothing was deleted/, `ResetIdentity(${JSON.stringify(args)}) was not refused`);
  }
  untouched(assert, alice, dbBefore, "after erase calls without the word or a ticket");

  step("choosing to erase asks for the word; a wrong word keeps the button disabled");
  await page.getByRole("button", { name: "Erase this data and start over" }).click();
  const word = page.getByRole("textbox", { name: "Type ERASE to confirm" });
  const yes = page.getByRole("button", { name: "Yes, delete and start over" });
  await word.waitFor();
  assert.ok(await yes.isDisabled(), "the erase button is enabled before anything is typed");
  const [c, y] = [await page.getByRole("button", { name: "Cancel" }).boundingBox(), await yes.boundingBox()];
  assert.ok(c.y < y.y, "the erase comes before Cancel");
  assert.ok(y.width < c.width, "the erase is as wide as Cancel");
  for (const wrong of ["ERAS", "delete", "Alice", "ERASE!"]) {
    await word.fill(wrong);
    assert.ok(await yes.isDisabled(), `the erase button is enabled with "${wrong}" typed`);
    await word.press("Enter");
  }
  assert.ok(await word.isVisible(), "Enter with a wrong word left the confirmation (it submitted the form behind it)");
  untouched(assert, alice, dbBefore, "after wrong words");

  step("cancelling keeps everything and returns to the ways back");
  await page.getByRole("button", { name: "Cancel" }).click();
  await page.getByRole("button", { name: "Use my recovery phrase" }).waitFor();
  untouched(assert, alice, dbBefore, "after cancelling the erase");

  step("with the word typed, the data goes and a new account can be set up");
  await page.getByRole("button", { name: "Erase this data and start over" }).click();
  await word.fill(" erase ");
  assert.ok(await yes.isEnabled(), "the erase button stays disabled with the word typed");
  await yes.click();
  await waitFor(async () => !fs.existsSync(path.join(alice.home, "concord.db")), { message: "the confirmed erase removes the database" });
  // The create form puts focus in "Your name" a frame after it shows; a fill
  // before that lands in the wrong field, which no person types fast enough for.
  await page.waitForFunction(() => document.activeElement?.getAttribute("aria-label") === "Your name");
  await page.getByRole("textbox", { name: "Your name" }).fill("Someone new");
  await page.locator('input[aria-label="Choose a passphrase"]').fill("a-new-passphrase");
  await page.locator('input[aria-label="Confirm passphrase"]').fill("a-new-passphrase");
  await page.getByRole("button", { name: "Create account" }).click();
  await waitFor(async () => (await alice.rpc("Session")) === true, { message: "the new account is set up" });
  const me = await alice.rpc("Identity");
  assert.notEqual(me.fingerprint, alice.fingerprint, "the old account came back");
});

// A linked install's data without its keystore: its first screen offers only
// Erase it or Quit (account-stranded-first-screen.spec.mjs).
