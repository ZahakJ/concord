// Linking a device used to hand the account to whoever dialled with the code:
// a photo of the QR was enough. Now the device showing the code names the
// device that asked and shows six digits, the new device shows the same six,
// and nothing is sent until the owner says yes there. A no leaves the new
// device with nothing. Both layouts: the code on a desktop scanned by a
// phone, and the other way round. LINK_SHOTS=<dir> keeps the screenshots.
import fs from "node:fs";
import path from "node:path";
import { test } from "../harness.mjs";

async function shot(world, app, name) {
  const dir = process.env.LINK_SHOTS || world.dir;
  fs.mkdirSync(dir, { recursive: true });
  await app.page.screenshot({ path: path.join(dir, `${name}.png`) });
}

// The issuer's "Your devices" with a fresh code up, and the code as the
// "Copy code instead" button puts it on the clipboard.
async function showCode(app) {
  const dlg = await app.openSettings("Your devices");
  const copy = dlg.getByRole("button", { name: "Copy code instead" });
  await copy.waitFor();
  await copy.click();
  return { dlg, code: (await app.page.evaluate(() => navigator.clipboard.readText())).trim() };
}

// A fresh device on its first screen, pasting the code and pressing Link.
async function joinWith(world, account, name, viewport, code) {
  const [dev] = await world.peers(1, { names: [name], start: false });
  dev.passphrase = account.passphrase;
  await dev.start();
  const app = await dev.open({ unlock: false, viewport });
  const { page } = app;
  await page.goto(`${dev.url}/?t=${dev.token}`, { waitUntil: "domcontentloaded" });
  await page.getByRole("button", { name: "Add this device to my account" }).click();
  await page.getByRole("textbox", { name: "Link code" }).fill(code);
  await page.getByPlaceholder("Passphrase for this device").fill(dev.passphrase);
  await page.getByPlaceholder("Confirm passphrase").fill(dev.passphrase);
  await page.getByRole("button", { name: "Link this device" }).click();
  return { dev, app };
}

// Both screens up to the question: the issuer's prompt and the new device's
// digits, which must agree.
async function bothAsk(issuerApp, joinApp, assert) {
  const ask = issuerApp.page.getByRole("region", { name: "Confirm the new device" });
  await ask.waitFor({ timeout: 90000 });
  const issuerSAS = (await ask.locator(".sas").innerText()).trim();
  const joinSAS = (await joinApp.page.locator(".link-sas .sas").innerText({ timeout: 30000 })).trim();
  assert.match(issuerSAS, /^\d{3} \d{3}$/, `the issuer shows "${issuerSAS}"`);
  assert.equal(joinSAS, issuerSAS, "the two screens show different digits");
  // The digits must be on screen without scrolling, on a phone too.
  await joinApp.page.waitForTimeout(300);
  const box = await joinApp.page.locator(".link-sas .sas").boundingBox();
  const vp = joinApp.page.viewportSize();
  assert.ok(box && box.y >= 0 && box.y + box.height <= vp.height, `the new device's digits are off screen: ${JSON.stringify(box)} in ${vp.height}px`);
  return ask;
}

test("a desktop's code, scanned by a phone: same digits on both, linked on yes", async ({ world, assert, waitFor, step }) => {
  const [alice] = await world.peers(1);
  const desk = await alice.open({ viewport: "desktop" });

  step("the desktop shows a code; the phone pastes it and asks");
  const { dlg, code } = await showCode(desk);
  const { dev, app: phone } = await joinWith(world, alice, "AlicePhone", "phone", code);

  step("the desktop names the device and shows the digits the phone shows");
  const ask = await bothAsk(desk, phone, assert);
  assert.ok((await ask.innerText()).includes("wants to join your account"), "the prompt does not say what is asked");
  assert.equal(await dev.rpc("HasIdentity"), false, "the phone holds an identity before the yes");
  await shot(world, desk, "desktop-issuer-asks");
  await shot(world, phone, "phone-joiner-waits");

  step("yes on the desktop links the phone");
  await ask.getByRole("button", { name: "Yes, link it" }).click();
  await waitFor(async () => (await dev.rpc("Session")) === true, { timeout: 90000, message: "the phone to be linked and unlocked" });
  assert.equal((await dev.rpc("Identity")).fingerprint, alice.fingerprint, "the phone holds another account");
  await dlg.getByText("is linked.").waitFor({ timeout: 30000 });
  await shot(world, desk, "desktop-issuer-linked");
});

test("a phone's code, scanned by a desktop: no on the phone sends nothing", async ({ world, assert, step }) => {
  const [alice] = await world.peers(1);
  const phone = await alice.open({ viewport: "phone" });

  step("the phone shows a code; the desktop pastes it and asks");
  const { dlg, code } = await showCode(phone);
  const { dev, app: desk } = await joinWith(world, alice, "AliceLaptop", "desktop", code);

  step("both screens show the same digits");
  const ask = await bothAsk(phone, desk, assert);
  await shot(world, phone, "phone-issuer-asks");
  await shot(world, desk, "desktop-joiner-waits");

  step("no on the phone: the desktop is told, holds nothing, and the code is spent");
  await ask.getByRole("button", { name: "No", exact: true }).click();
  await desk.page.getByText("Your other device said no.").waitFor({ timeout: 30000 });
  assert.equal(await dev.rpc("HasIdentity"), false, "the turned-away device holds an identity");
  await dlg.getByText("was turned away. This code no longer works.").waitFor();
  assert.ok(await dlg.getByRole("button", { name: "Show a new code" }).isVisible(), "no way to a new code");
  await shot(world, phone, "phone-issuer-declined");
  await shot(world, desk, "desktop-joiner-declined");
});
