// People with a working second device reached for "restore from recovery
// phrase", and a phrase restore beside a running device makes a copy that can
// see neither it nor its guilds. Linking must be offered first, and the restore
// screen must point back to it.
import { test } from "../harness.mjs";

test("a fresh install offers adding this device ahead of restoring from the phrase", async ({ world, assert, step }) => {
  const [dev] = await world.peers(1, { names: ["Fresh"], start: false });
  await dev.start();
  const app = await dev.open({ unlock: false });
  const { page } = app;
  await page.goto(`${dev.url}/?t=${dev.token}`, { waitUntil: "domcontentloaded" });
  const add = page.getByRole("button", { name: "Add this device to my account" });
  const lost = page.getByRole("button", { name: "I lost my devices — restore from my recovery phrase" });
  await add.waitFor();
  const [a, l] = [await add.boundingBox(), await lost.boundingBox()];
  assert.ok(a.y < l.y, "adding this device comes before restoring");
  step("the restore screen sends someone with a running device back to linking");
  await lost.click();
  await page.getByRole("button", { name: "Add this device to my account instead" }).click();
  await page.getByRole("heading", { name: "Link this device" }).waitFor();
});
