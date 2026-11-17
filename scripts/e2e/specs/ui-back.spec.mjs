// The browser's Back button inside the web build: it used to leave the app
// (and coming back meant reloading and typing the passphrase again).
import fs from "node:fs";
import path from "node:path";
import { test } from "../harness.mjs";

const back = (page) => page.evaluate(() => history.back());

test("Back closes a dialog, then returns to the previous channel, without leaving the app", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Back", members: [alice], channels: ["random"] });
  const a = await alice.open();
  const url = a.page.url();
  await a.openChannel("general", "Back");
  await a.openChannel("random");

  step("Back with Settings open closes Settings");
  await a.openSettings();
  await back(a.page);
  await waitFor(async () => (await a.page.locator('[role="dialog"]').count()) === 0, { message: "Settings closed by Back" });
  assert.equal(a.page.url(), url);

  step("Back again returns to #general");
  await back(a.page);
  await waitFor(async () => (await a.composer().getAttribute("placeholder"))?.includes("general"), { message: "back in #general" });
  assert.equal(a.page.url(), url, "still on the app's page");
  await a.page.locator('nav[aria-label="Guilds"]').waitFor({ state: "attached" });
  assert.ok(g.id);
});

test("phone: Back from a conversation shows the channel list", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1);
  await world.guild({ name: "Pocket", members: [alice] });
  const a = await alice.open({ viewport: "phone" });
  const url = a.page.url();
  await a.openChannel("general", "Pocket");
  step("Back");
  await back(a.page);
  await waitFor(async () => (await a.page.locator(".channel").filter({ hasText: "general" }).first().isVisible()), {
    message: "the channel list is showing",
  });
  assert.equal(a.page.url(), url, "still on the app's page");
});

// From the ways back, Restore, Re-link and the erase confirmation each return
// there, by their own Back (or Cancel) and by Escape, which pops the same
// stack the phone's back does. They used to
// drop to the screen under the menu: over data without its keystore, a create
// form the core can only refuse.
test("Back from the recovery-phrase, re-link and erase screens returns to the ways back", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1);
  const notes = (await alice.rpc("NotesDM")).channels[0].id;
  await alice.send(notes, "kept");
  await alice.rpc("Logout");
  await alice.stop();
  fs.rmSync(path.join(alice.home, "keystore.json"));
  await alice.start();
  const { page } = await alice.open({ unlock: false });
  await page.goto(`${alice.url}/?t=${alice.token}`, { waitUntil: "domcontentloaded" });
  await page.getByRole("button", { name: "Keep it and restore", exact: true }).click();
  const menu = page.getByRole("button", { name: "Use my recovery phrase" });
  await menu.waitFor();
  const onMenu = async (what) => {
    await waitFor(async () => (await menu.count()) === 1, { message: `${what}: not back on the ways back` });
    assert.equal(await page.getByRole("button", { name: "Create identity" }).count(), 0, `${what}: the create form is showing`);
  };
  const ways = [
    ["Use my recovery phrase", () => page.getByRole("textbox", { name: "Recovery phrase" })],
    ["Re-link from my other device", () => page.getByRole("button", { name: "Link this device" })],
  ];
  for (const [open, shown] of ways) {
    for (const how of ["its Back", "Escape"]) {
      step(`${open}, then ${how}`);
      await page.getByRole("button", { name: open }).click();
      await shown().waitFor();
      if (how === "its Back") await page.getByRole("button", { name: "Back", exact: true }).click();
      else await page.keyboard.press("Escape");
      await onMenu(`${open}, ${how}`);
    }
  }

  for (const how of ["Cancel", "Escape"]) {
    step(`Erase, then ${how}`);
    await page.getByRole("button", { name: "Erase this data and start over" }).click();
    await page.getByRole("textbox", { name: "Type ERASE to confirm" }).waitFor();
    if (how === "Cancel") await page.getByRole("button", { name: "Cancel" }).click();
    else await page.keyboard.press("Escape");
    await onMenu(`erase, ${how}`);
  }

  step("Back from the ways back still returns to the first screen's choice");
  await page.getByRole("button", { name: "Back", exact: true }).click();
  await page.getByRole("button", { name: "Keep it and restore", exact: true }).waitFor();
  assert.equal(await alice.rpc("Session"), false, "an account was opened or created");
});

// From the unlock screen's "Forgot passphrase?", Restore and Re-link return to
// the ways back, and the ways back to the unlock screen. Screens opened from
// the unlock screen itself return to it, even after an erase from the ways
// back was refused as out of date.
test("Back from the unlock screen's own screens returns to the unlock screen", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1);
  await alice.rpc("Logout");
  const { page } = await alice.open({ unlock: false });
  await page.goto(`${alice.url}/?t=${alice.token}`, { waitUntil: "domcontentloaded" });
  const pass = page.locator('input[placeholder="Passphrase"]');
  const menu = page.getByRole("button", { name: "Use my recovery phrase" });
  await pass.waitFor();

  for (const [open, shown] of [
    ["Use my recovery phrase", () => page.getByRole("textbox", { name: "Recovery phrase" })],
    ["Re-link from my other device", () => page.getByRole("button", { name: "Link this device" })],
  ]) {
    step(`Forgot passphrase?, ${open}, then its Back returns to the ways back`);
    await page.getByRole("button", { name: "Forgot passphrase?" }).click();
    await page.getByRole("button", { name: open }).click();
    await shown().waitFor();
    await page.getByRole("button", { name: "Back", exact: true }).click();
    await waitFor(async () => (await menu.count()) === 1, { message: `${open}, its Back: not back on the ways back` });
    step("Back from the ways back returns to the unlock screen");
    await page.getByRole("button", { name: "Back", exact: true }).click();
    await pass.waitFor();
  }

  step("Forgot passphrase?, an erase refused as out of date, then Back");
  await page.getByRole("button", { name: "Forgot passphrase?" }).click();
  await page.getByRole("button", { name: "I have neither — start over (deletes everything)" }).click();
  const word = page.getByRole("textbox", { name: "Type ERASE to confirm" });
  await word.waitFor();
  await alice.rpc("BeginStartOver"); // replaces the confirmation's ticket
  await word.fill("ERASE");
  await page.getByRole("button", { name: "Yes, delete and start over" }).click();
  await menu.waitFor();
  assert.match(await page.getByRole("alert").filter({ hasText: /\S/ }).first().innerText(), /Nothing was deleted/, "the erase was not refused as out of date");
  await page.getByRole("button", { name: "Back", exact: true }).click();
  await pass.waitFor();

  step("Re-link from the unlock screen, then its Back");
  await page.getByRole("button", { name: "Re-link from another device" }).click();
  await page.getByRole("button", { name: "Link this device" }).waitFor();
  await page.getByRole("button", { name: "Back", exact: true }).click();
  await waitFor(async () => (await pass.isVisible()) || (await menu.count()) === 1, { message: "Back left the link screen" });
  assert.equal(await menu.count(), 0, "Back from the unlock screen's re-link landed on the ways back");
  assert.ok(await pass.isVisible(), "not back on the unlock screen");
  assert.equal(await alice.rpc("Session"), false, "an account was opened");

  step("Escape while the erase is running does nothing, as Cancel is disabled then");
  let seen, release;
  const inFlight = new Promise((r) => (seen = r));
  const held = new Promise((r) => (release = r));
  await page.route("**/rpc", async (route) => {
    if (JSON.parse(route.request().postData() || "{}").method === "ResetIdentity") {
      seen();
      await held;
    }
    await route.continue();
  });
  await page.getByRole("button", { name: "Forgot passphrase?" }).click();
  await page.getByRole("button", { name: "I have neither — start over (deletes everything)" }).click();
  await word.fill("ERASE");
  await page.getByRole("button", { name: "Yes, delete and start over" }).click();
  await inFlight;
  await page.keyboard.press("Escape");
  await page.waitForTimeout(300);
  const stayed = await word.isVisible();
  release();
  assert.ok(stayed, "Escape closed the erase confirmation while the erase was running");
  await page.getByRole("button", { name: "Create identity" }).waitFor();
  assert.equal(await menu.count(), 0, "the ways back showed after the erase");
});

// A link code arriving from the phone's camera while the ways back (or a
// screen opened from them) is up opens the link screen; its Back returns to
// the ways back, not to the create form under them.
test("a link code that arrives over the ways back: Back returns to them", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1);
  const notes = (await alice.rpc("NotesDM")).channels[0].id;
  await alice.send(notes, "kept");
  await alice.rpc("Logout");
  await alice.stop();
  fs.rmSync(path.join(alice.home, "keystore.json"));
  await alice.start();
  const { page } = await alice.open({ unlock: false });
  // The phone's app plugin, enough to hand the page a concord:// link.
  await page.addInitScript(() => {
    const on = {};
    window.__openURL = (url) => on.appUrlOpen?.({ url });
    window.Capacitor = {
      Plugins: {
        App: {
          getLaunchUrl: async () => ({}),
          addListener: (name, fn) => ((on[name] = fn), { remove() {} }),
        },
      },
    };
  });
  await page.goto(`${alice.url}/?t=${alice.token}`, { waitUntil: "domcontentloaded" });
  await page.getByRole("button", { name: "Keep it and restore", exact: true }).click();
  const menu = page.getByRole("button", { name: "Use my recovery phrase" });
  await menu.waitFor();
  for (const from of ["the ways back", "Restore opened from them"]) {
    step(`a link code over ${from}, then its Back`);
    if (from !== "the ways back") {
      await menu.click();
      await page.getByRole("textbox", { name: "Recovery phrase" }).waitFor();
    }
    await page.evaluate(() => window.__openURL("concord://link?c=notarealcode"));
    await page.getByRole("button", { name: "Link this device" }).waitFor();
    await page.getByRole("button", { name: "Back", exact: true }).click();
    await waitFor(async () => (await menu.count()) === 1 || (await page.getByRole("button", { name: "Create identity" }).count()) === 1, {
      message: "Back left the link screen",
    });
    assert.equal(await page.getByRole("button", { name: "Create identity" }).count(), 0, `${from}: Back landed on the create form`);
    assert.equal(await menu.count(), 1, `${from}: not back on the ways back`);
  }
  assert.equal(await alice.rpc("Session"), false, "an account was opened or created");
});
