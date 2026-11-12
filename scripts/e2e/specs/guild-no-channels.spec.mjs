// A guild whose channels are all gone says so inside the guild, and offers
// Create a channel to whoever may, instead of the first-run welcome page.
import { test } from "../harness.mjs";

test("a guild with no channels has its own empty state", async ({ world, assert, step, waitFor, shot }) => {
  const [alice] = await world.peers(1, { names: ["Alice"] });
  const g = await world.guild({ name: "Empty Hall", members: [alice] });
  await alice.rpc("DeleteChannel", g.id, g.allChannels[0].id);
  const a = await alice.open();
  await a.page.locator('nav[aria-label="Guilds"]').getByRole("button", { name: "Empty Hall", exact: true }).first().click();
  await a.page.getByText("No channels yet", { exact: true }).waitFor({ state: "visible" });
  await shot(a, "empty guild");
  assert.equal(await a.page.getByText("Welcome to Concord").count(), 0);
  await a.page.getByRole("button", { name: "Create a channel" }).click();
  await a.dialog().waitFor({ state: "visible" });
});
