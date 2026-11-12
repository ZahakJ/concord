// On a phone viewport a member can open a guild channel from Home, send and
// receive, and get back to the channel list.
import { test } from "../harness.mjs";

test("phone: open a channel, message both ways, back to channels", async ({ world, assert, step }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Pocket", members: [alice, bashir] });
  const ch = g.allChannels[0];

  const a = await alice.open({ viewport: "phone" });
  const b = await bashir.open();
  assert.ok(a.isPhone);
  await b.openChannel(ch.name, "Pocket");

  step("Alice opens the channel from Home on her phone");
  await a.openChannel(ch.name, "Pocket");
  await a.send("sent from a phone");
  await b.waitForMessage("sent from a phone");

  await b.send("received on a phone");
  await a.waitForMessage("received on a phone");

  step("Back returns to the channel list");
  await a.back();
  await a.page.locator(".channel").filter({ hasText: ch.name }).first().waitFor({ state: "visible" });
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});
