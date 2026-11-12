// A second device linked to an account joins that account's guilds, and what
// it sends reaches other members as coming from the same person.
import { test } from "../harness.mjs";

test("a linked device joins the account's guilds and speaks as the account", async ({ world, assert, step }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Two Devices", members: [alice, bashir] });
  const ch = g.allChannels[0];

  step("Alice links a second device");
  const phone = await world.linkDevice(alice, { name: "AlicePhone" });
  await world.settleGuild({ id: g.id, name: g.name }, [alice, phone]);

  step("the second device sends; Bashir sees it as Alice");
  await phone.send(ch.id, "hello from my other device");
  const got = await bashir.waitForMessage(ch.id, "hello from my other device");
  assert.equal(got.senderName, "Alice");

  step("and the second device's UI shows the guild and the conversation");
  const p = await phone.open({ viewport: "phone" });
  await p.openChannel(ch.name, "Two Devices");
  await bashir.send(ch.id, "both your devices should get this");
  await p.waitForMessage("both your devices should get this");
  await alice.waitForMessage(ch.id, "both your devices should get this");
});
