// Deleting a category puts its channels after the uncategorized ones, in their
// old order, on every member's sidebar.
import { test } from "../harness.mjs";

test("deleted category's channels go to the end", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bob] = await world.peers(2, { names: ["Alice", "Bob"] });
  const g = await world.guild({ name: "Shelves", members: [alice, bob] });
  const general = g.allChannels[0];
  const cat = await alice.rpc("CreateCategory", g.id, "Staff Room");
  const second = await alice.rpc("CreateChannel", g.id, "lounge", "", "");
  const news = await alice.rpc("CreateChannel", g.id, "news", "", "");
  const ideas = await alice.rpc("CreateChannel", g.id, "ideas", "", "");
  await alice.rpc("SetChannelMeta", g.id, general.id, "", "", 0, "");
  await alice.rpc("SetChannelMeta", g.id, second.id, "", "", 1, "");
  await alice.rpc("SetChannelMeta", g.id, news.id, "", cat.id, 0, "");
  await alice.rpc("SetChannelMeta", g.id, ideas.id, "", cat.id, 1, "");
  await waitFor(async () => (await bob.guilds()).find((x) => x.id === g.id).channels.filter((c) => c.category === cat.id).length === 2, {
    message: "Bob never saw the category",
  });

  step("Alice deletes the category");
  await alice.rpc("DeleteCategory", g.id, cat.id);
  const b = await bob.open();
  await b.openChannel(general.name, "Shelves");
  const order = () => b.page.locator('nav[aria-label="Guilds and channels"] .scroll').first().innerText();
  await waitFor(async () => !/Staff Room/i.test(await order()), { message: "the category is still listed" });
  await shot(b, "after category delete");
  const text = await order();
  const idx = (n) => text.search(new RegExp(`\\b${n}\\b`));
  assert.ok(idx(general.name) < idx("lounge") && idx("lounge") < idx("news") && idx("news") < idx("ideas"), text);
});
