// Pinned messages are one click from the conversation header on a wide
// window, and a row in ⋯ below 1024px.
import { test } from "../harness.mjs";

test("pins one click from the header, folding into ⋯ on a narrow window", async ({ world, assert, step }) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Workshop", members: [alice] });
  const a = await alice.open({ viewport: "desktop" });
  await a.openChannel(g.allChannels[0].name || "general", "Workshop");

  step("Pinned messages opens the inspector on Pins");
  await a.page.getByRole("button", { name: "Pinned messages" }).click();
  await a.page.getByRole("tab", { name: /Pins/, selected: true }).waitFor();

  step("below 1024px it is a row in ⋯ instead");
  await a.page.setViewportSize({ width: 960, height: 800 });
  await a.page.waitForTimeout(300);
  assert.equal(await a.page.getByRole("button", { name: "Pinned messages" }).count(), 0);
  await a.page.locator(".chat-head").getByRole("button", { name: "More" }).click();
  await a.page.getByRole("menuitem", { name: "Pinned messages" }).waitFor();
});
