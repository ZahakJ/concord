// A reply made from the message toolbar is stored as a reply to that message
// on the other member's device. (Also the worked example in README.md.)
import { test } from "../harness.mjs";

test("a reply quotes the message it answers", async ({ world, assert, step }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Replies", members: [alice, bashir] });
  const general = g.allChannels[0];

  const a = await alice.open();
  const b = await bashir.open();
  await a.openChannel(general.name, "Replies");
  await b.openChannel(general.name, "Replies");

  step("Alice asks, Bashir replies from the message toolbar");
  await a.send("lunch at noon?");
  await b.waitForMessage("lunch at noon?");
  await b.messageAction("lunch at noon?", "Reply");
  await b.send("yes, see you there");

  const reply = await alice.waitForMessage(general.id, "yes, see you there");
  const asked = await alice.waitForMessage(general.id, "lunch at noon?");
  assert.equal(reply.replyTo, asked.id);
});
