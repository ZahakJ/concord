// The browser's Back button inside the web build: it used to leave the app
// (and coming back meant reloading and typing the passphrase again).
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
