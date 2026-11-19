// On a phone a reaction is added from the long-press sheet: its quick row, or
// its "+" into the full picker. Both called through a {@const} over the menu
// state AFTER closing the menu had nulled it, so the tap threw and no reaction
// ever reached the message ("emojis don't seem to apply").
import { test } from "../harness.mjs";

test("phone: reacting from the long-press sheet applies the reaction", async ({ world, assert, step, waitFor }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Reacts", members: [alice, bashir] });
  const ch = g.allChannels[0];
  const a = await alice.open({ viewport: "phone" });
  await a.openChannel(ch.name, "Reacts");
  await a.settled();
  const p = a.page;

  const target = await bashir.send(ch.id, "react to this one");
  await a.waitForMessage("react to this one");
  const reactions = async () => (await alice.messages(ch.id)).find((m) => m.id === target.id)?.reactions || {};

  step("the quick row's 👍");
  await a.longPress(a.message("react to this one"));
  await p.getByRole("button", { name: "React 👍", exact: true }).tap();
  await waitFor(async () => (await reactions())["👍"]?.length === 1, { timeout: 10000, message: "👍 on the message" });
  await bashir.waitForMessage(ch.id, (m) => m.id === target.id && m.reactions?.["👍"]?.length === 1);

  step("the + into the full picker");
  await a.longPress(a.message("react to this one"));
  await p.getByRole("button", { name: "More reactions", exact: true }).tap();
  await p.getByRole("button", { name: "Search emoji" }).or(p.getByPlaceholder("Search emoji…")).first().waitFor();
  await p.getByPlaceholder("Search emoji…").fill("fire");
  await p.locator(".picker .grid .cell", { hasText: "🔥" }).first().tap();
  await waitFor(async () => (await reactions())["🔥"]?.length === 1, { timeout: 10000, message: "🔥 on the message" });
  // Picking for a message closes the picker; it does not drop into the draft.
  await p.locator(".picker").waitFor({ state: "detached", timeout: 5000 });
  assert.equal(await a.composer().inputValue(), "");
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});
