// When an admin deletes the channel you are in, you are told, and the message
// you were half-way through comes with you to the channel you land in.
import { test } from "../harness.mjs";

test("deleting the channel someone is in tells them and keeps their draft", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, carol] = await world.peers(2, { names: ["Alice", "Carol"] });
  const g = await world.guild({ name: "Two Rooms", members: [alice, carol], channels: ["side-room"] });
  const side = { id: g.channels["side-room"] };
  const c = await carol.open();
  await c.openChannel("side-room", "Two Rooms");
  await c.composer().click();
  await c.page.keyboard.type("half-typed draft");
  await c.page.waitForTimeout(800);

  step("Alice deletes #side-room");
  await alice.rpc("DeleteChannel", g.id, side.id);
  await c.page.getByText(/#side-room was deleted/).first().waitFor({ state: "visible", timeout: 30000 });
  await shot(c, "deleted under carol");
  await waitFor(async () => (await c.composer().inputValue().catch(() => c.composer().innerText())).includes("half-typed draft"), {
    message: "the draft did not come along",
  });
  assert.match(await c.page.getByText(/#side-room was deleted/).first().innerText(), /unsent message is in #/);
});
