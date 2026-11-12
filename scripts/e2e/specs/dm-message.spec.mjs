// A direct message started by one member reaches the other, who can open it
// from Home and answer.
import { test } from "../harness.mjs";

test("a DM arrives and can be answered from Home", async ({ world }) => {
  const [alice, bashir] = await world.peers(2);
  await world.guild({ name: "Mutual", members: [alice, bashir] });
  const dm = await world.dm(alice, bashir);

  const b = await bashir.open();
  await b.openDM("Alice");
  await b.waitForMessage(dm.greeting);
  await b.send("hi alice, got your DM");
  await alice.waitForMessage(dm.channelId, "hi alice, got your DM");
});
