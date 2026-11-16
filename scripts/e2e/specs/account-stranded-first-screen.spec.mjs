// A device that holds an account's data but not the keystore that opens it
// asks first what happens to that data: its first screen is the choice, and
// nothing is opened, kept up or created until one is made. Data a restore of
// its own account can open again is offered "Erase it" or "Keep it and
// restore"; a linked device's data, which nobody can open again, is offered
// "Erase it" or "Quit", and so is a never-linked install with no message to
// check a phrase against, which is not told it was linked. Quit leaves by the
// shell's own way out, and where the window cannot be closed the screen stays
// without a button.
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { test } from "../harness.mjs";

const dbHash = (peer) =>
  crypto.createHash("sha256").update(fs.readFileSync(path.join(peer.home, "concord.db"))).digest("hex");

// keystoreLost leaves the peer with its data and no keystore, stopped and
// started again, and returns its database's content hash.
async function keystoreLost(peer) {
  await peer.rpc("Logout");
  await peer.stop();
  fs.rmSync(path.join(peer.home, "keystore.json"));
  await peer.start();
  return dbHash(peer);
}

async function untouched(assert, peer, before, what) {
  assert.equal(await peer.rpc("Session"), false, `${what}: an account was opened or created`);
  assert.ok(fs.existsSync(path.join(peer.home, "concord.db")), `${what}: the database is gone`);
  assert.equal(dbHash(peer), before, `${what}: the database's content changed`);
  assert.ok(!fs.existsSync(path.join(peer.home, "keystore.json")), `${what}: a keystore was created`);
}

const NEVER_LINKED = {
  title: "This device holds data it can no longer open",
  other: "Keep it and restore",
};
const LINKED = { title: "This device holds data nobody can open", other: "Quit" };
const UNCHECKED = { title: "This device holds data it can no longer open", other: "Quit" };
const UNCHECKED_BODY =
  "Concord found an earlier account's settings here, but not the key that unlocks them. There are no messages a recovery phrase could bring back. Erase them to use Concord on this device, or quit and leave them as they are.";
const CLOSE_NOW = "You can close this window now.";

// firstScreen loads the app on a fresh page and checks that what it shows
// first is the choice, with its two buttons and nothing else to do.
async function firstScreen(peer, assert, want, opts = {}) {
  const app = await peer.open({ unlock: false, ...opts });
  const { page } = app;
  await page.goto(`${peer.url}/?t=${peer.token}`, { waitUntil: "domcontentloaded" });
  await page.getByRole("heading", { name: want.title, exact: true }).waitFor();
  const erase = page.getByRole("button", { name: "Erase it", exact: true });
  const other = page.getByRole("button", { name: want.other, exact: true });
  assert.ok(await erase.isVisible(), "no Erase it button");
  assert.ok(await other.isVisible(), `no ${want.other} button`);
  assert.equal(await page.getByRole("button", { name: "Create identity" }).count(), 0, "the create form is shown beside the choice");
  assert.equal(await page.getByRole("textbox", { name: "Your name" }).count(), 0, "a name can be typed before the choice");
  assert.equal(await page.locator('input[type="password"]').count(), 0, "a passphrase can be typed before the choice");
  return { page, erase, other };
}

// eraseThroughTheConfirmation types the word and erases; the data goes and a
// new account can be set up.
async function eraseThroughTheConfirmation(page, peer, assert, waitFor) {
  const word = page.getByRole("textbox", { name: "Type ERASE to confirm" });
  const yes = page.getByRole("button", { name: "Yes, delete and start over" });
  await word.waitFor();
  assert.ok(await yes.isDisabled(), "the erase is enabled before the word is typed");
  await word.fill("ERASE");
  await yes.click();
  await waitFor(async () => !fs.existsSync(path.join(peer.home, "concord.db")), { message: "the confirmed erase removes the database" });
  await page.getByRole("textbox", { name: "Your name" }).fill("Someone new");
  await page.getByPlaceholder("Choose a passphrase").fill("a-new-passphrase");
  await page.getByPlaceholder("Confirm passphrase").fill("a-new-passphrase");
  await page.getByRole("button", { name: "Create identity" }).click();
  await waitFor(async () => (await peer.rpc("Session")) === true, { message: "the new account is set up" });
  const me = await peer.rpc("Identity");
  assert.notEqual(me.fingerprint, peer.fingerprint, "the old account came back");
}

async function linkedPhoneWithoutKeystore(world) {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Stranded", members: [alice, bashir] });
  const phone = await world.linkDevice(alice, { name: "AlicePhone" });
  await world.settleGuild({ id: g.id, name: g.name }, [alice, bashir, phone]);
  await bashir.send(g.allChannels[0].id, "a message the phone holds");
  await phone.waitForMessage(g.allChannels[0].id, "a message the phone holds");
  return { phone, before: await keystoreLost(phone) };
}

test("Erase it, never-linked data: the choice comes first, and the data goes only after the typed confirmation", async ({ world, assert, waitFor, step }) => {
  const [alice] = await world.peers(1);
  const notes = (await alice.rpc("NotesDM")).channels[0].id;
  await alice.send(notes, "a note on this device");
  const before = await keystoreLost(alice);

  step("the choice is the first screen, and nothing is opened or created while it is shown");
  const { page, erase } = await firstScreen(alice, assert, NEVER_LINKED);
  await untouched(assert, alice, before, "while the choice is shown");

  step("Erase it asks for the word; Cancel comes back to the choice with everything kept");
  await erase.click();
  await page.getByRole("textbox", { name: "Type ERASE to confirm" }).waitFor();
  await page.getByRole("button", { name: "Cancel" }).click();
  await page.getByRole("heading", { name: NEVER_LINKED.title, exact: true }).waitFor();
  await untouched(assert, alice, before, "after cancelling the erase");

  step("Erase it with the word typed erases the data, and a new account can be set up");
  await erase.click();
  await eraseThroughTheConfirmation(page, alice, assert, waitFor);
});

test("Keep it, never-linked data: the choice leads to the restore, keeps everything, and the account's own phrase opens it again", async ({ world, assert, waitFor, step }) => {
  const [alice] = await world.peers(1);
  const phrase = await alice.rpc("RevealMnemonic", alice.passphrase);
  const notes = (await alice.rpc("NotesDM")).channels[0].id;
  await alice.send(notes, "a note that has to survive");
  const before = await keystoreLost(alice);
  const { page, other } = await firstScreen(alice, assert, NEVER_LINKED);

  step("Back from the ways back returns to the choice, not to the create form");
  await other.click();
  await page.getByRole("button", { name: "Use my recovery phrase" }).waitFor();
  await page.getByRole("button", { name: "Back", exact: true }).click();
  await page.getByRole("heading", { name: NEVER_LINKED.title, exact: true }).waitFor();
  assert.equal(await page.getByRole("button", { name: "Create identity" }).count(), 0, "Back led to the create form");
  await untouched(assert, alice, before, "after Back");

  step("Keep it opens the ways back, and nothing is erased or created");
  await other.click();
  await page.getByRole("button", { name: "Use my recovery phrase" }).waitFor();
  assert.match(await page.locator("form").innerText(), /If it is this account's phrase, everything on this device is kept\./, "the keep is not explained");
  await untouched(assert, alice, before, "after Keep it");

  step("the account's own phrase opens it again, messages and all");
  await page.getByRole("button", { name: "Use my recovery phrase" }).click();
  await page.getByRole("textbox", { name: "Recovery phrase" }).fill(phrase);
  await page.getByPlaceholder("New passphrase").fill("the-restored-passphrase");
  await page.getByPlaceholder("Confirm passphrase").fill("the-restored-passphrase");
  await page.getByRole("button", { name: "Restore account" }).click();
  await waitFor(async () => (await alice.rpc("Session")) === true, { message: "the own-phrase restore unlocks" });
  assert.equal((await alice.rpc("Identity")).fingerprint, alice.fingerprint, "a different account came back");
  const kept = (await alice.messages(notes)).map((m) => m.content);
  assert.ok(kept.includes("a note that has to survive"), `the messages did not survive: ${JSON.stringify(kept)}`);
});

test("Erase it, linked device's data: the choice is Erase it or Quit, and the data goes after the typed confirmation", async ({ world, assert, waitFor, step }) => {
  const { phone, before } = await linkedPhoneWithoutKeystore(world);

  step("the choice is the first screen, promises nothing about keeping, and opens nothing");
  const { page, erase } = await firstScreen(phone, assert, LINKED, { viewport: "phone" });
  const text = await page.locator("form").innerText();
  assert.match(text, /Nobody can open this data again, not even with the recovery phrase\./, `the linked case is not said: ${text}`);
  assert.doesNotMatch(text, /Keep it and restore|everything on this device is kept/, "the screen offers to keep data nobody can open");
  await untouched(assert, phone, before, "while the choice is shown");

  step("Erase it with the word typed erases the data, and a new account can be set up");
  await erase.click();
  await eraseThroughTheConfirmation(page, phone, assert, waitFor);
});

// quitBy clicks Quit and returns which ways out the app called. android:
// a Capacitor App plugin is present, as on the phone. close is always a
// no-op, as in a browser tab the page did not open.
async function quitBy(page, other, { android }) {
  await page.evaluate((android) => {
    window.__calls = [];
    window.close = () => window.__calls.push("close");
    if (android) window.Capacitor = { Plugins: { App: { exitApp: async () => window.__calls.push("exitApp") } } };
  }, android);
  await other.click();
  return page.evaluate(() => window.__calls.join());
}

// inert checks the screen after a Quit the window survived: the choice's
// heading and the closing sentence, no button, nothing to type.
async function inert(page, assert, want) {
  await page.getByText(CLOSE_NOW, { exact: true }).waitFor();
  assert.ok(await page.getByRole("heading", { name: want.title, exact: true }).isVisible(), "the choice's heading went");
  const form = page.locator("form");
  assert.equal(await form.getByRole("button").count(), 0, "a button is left after Quit");
  assert.equal(await form.getByRole("textbox").count(), 0, "something can be typed after Quit");
  assert.equal(await page.locator('input[type="password"]').count(), 0, "a passphrase can be typed after Quit");
}

test("Quit, linked device's data: the phone exits, a browser keeps a screen with nothing on it, and the next start asks again", async ({ world, assert, step }) => {
  const { phone, before } = await linkedPhoneWithoutKeystore(world);

  step("on the phone, Quit exits through the app plugin and changes nothing");
  let { page, other } = await firstScreen(phone, assert, LINKED, { viewport: "phone" });
  assert.equal(await quitBy(page, other, { android: true }), "exitApp", "Quit did not exit through the phone's app plugin");
  await untouched(assert, phone, before, "after Quit on the phone");

  step("in a browser that keeps the window, the screen says to close it and offers nothing else");
  ({ page, other } = await firstScreen(phone, assert, LINKED, { viewport: "phone" }));
  assert.equal(await quitBy(page, other, { android: false }), "close", "Quit did not ask the browser to close");
  await inert(page, assert, LINKED);
  await untouched(assert, phone, before, "after Quit in a browser");

  step("the next start asks again");
  await phone.stop();
  await phone.start();
  await firstScreen(phone, assert, LINKED, { viewport: "phone" });
  await untouched(assert, phone, before, "after the next start");
});

test("Never-linked data with no message: not told it was linked, offered Erase it or Quit, and Quit opens nothing", async ({ world, assert, waitFor, step }) => {
  const [cy] = await world.peers(1);
  const before = await keystoreLost(cy);

  step("the choice says what is here without saying it was linked");
  const { page, other } = await firstScreen(cy, assert, UNCHECKED);
  const text = await page.locator("form").innerText();
  assert.ok(text.includes(UNCHECKED_BODY), `the body is not the approved one: ${text}`);
  assert.doesNotMatch(text, /linked|Nobody can open|Keep it and restore/, `the screen says what is not so: ${text}`);
  await untouched(assert, cy, before, "while the choice is shown");

  step("Quit in a browser leaves a screen with nothing on it");
  assert.equal(await quitBy(page, other, { android: false }), "close", "Quit did not ask the browser to close");
  await inert(page, assert, UNCHECKED);
  await untouched(assert, cy, before, "after Quit");

  step("Erase it on the next start erases the data, and a new account can be set up");
  const again = await firstScreen(cy, assert, UNCHECKED);
  await again.erase.click();
  await eraseThroughTheConfirmation(again.page, cy, assert, waitFor);
});
