// Appearance → Layout offers Sidebar and Rail; the choice holds across a
// reload, Rail puts the column of guilds beside the list, and the palette
// offers the same two. Compact is a density now, not a layout.
import { test } from "../harness.mjs";

test("a layout picked in Appearance holds across a reload", async ({ world, assert, step, waitFor, shot }) => {
  const [alice] = await world.peers(1);
  await world.guild({ name: "Workshop", members: [alice] });
  const a = await alice.open({ viewport: "desktop" });
  const model = () => a.page.locator(".app").getAttribute("data-nav-model");
  assert.equal(await model(), "sidebar", "Sidebar is the default");

  step("Appearance → Layout → Rail");
  const dlg = await a.openSettings("Appearance");
  await dlg.getByRole("radiogroup", { name: "Layout" }).getByRole("radio", { name: "Rail" }).click();
  await waitFor(async () => (await model()) === "rail", { message: "the rail layout" });
  await a.closeModals();
  await a.page.locator("nav.rail[aria-label='Guilds']").waitFor();
  await a.page.locator("nav.rail").getByRole("button", { name: "Add a guild" }).waitFor();
  await shot(a, "rail");

  step("it holds across a reload");
  await a.reload();
  assert.equal(await model(), "rail");

  step("the palette offers the two layouts and no Compact");
  await a.page.keyboard.press("Control+k");
  await a.page.keyboard.type("layout");
  await a.page.getByRole("option", { name: /Layout: Sidebar/ }).first().waitFor();
  assert.equal(await a.page.getByRole("option", { name: /Layout: Compact/ }).count(), 0, "no Compact layout");
  await a.page.getByRole("option", { name: /Layout: Sidebar/ }).first().click();
  await waitFor(async () => (await model()) === "sidebar", { message: "back to the sidebar model" });

  step("Compact lives in Density, and holds");
  const d = await a.openSettings("Appearance");
  await d.getByRole("radiogroup", { name: "Message density" }).getByRole("radio", { name: "Compact" }).click();
  await waitFor(async () => (await a.page.evaluate(() => document.documentElement.dataset.density)) === "compact", {
    message: "compact density",
  });
  await a.closeModals();
  await a.reload();
  assert.equal(await a.page.evaluate(() => document.documentElement.dataset.density), "compact");
  await shot(a, "compact");
});
