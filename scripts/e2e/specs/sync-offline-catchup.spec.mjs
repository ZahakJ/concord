// A member who is offline while others talk gets every message once they
// come back, in order, without anyone resending.
import { test } from "../harness.mjs";

test("an offline member catches up on return", async ({ world, assert, step, seedMessages }) => {
  const [alice, bashir, chen] = await world.peers(3);
  const g = await world.guild({ name: "Catch-up", members: [alice, bashir, chen] });
  const ch = g.allChannels[0];

  step("Chen goes offline");
  await chen.stop();

  step("Alice and Bashir talk while Chen is away");
  const fromAlice = await seedMessages(alice, ch.id, 4, { prefix: "while chen was out (alice)" });
  const fromBashir = await seedMessages(bashir, ch.id, 3, { prefix: "while chen was out (bashir)" });
  for (const t of fromAlice) await bashir.waitForMessage(ch.id, t);

  step("Chen comes back and catches up");
  await chen.start();
  await chen.unlock();
  for (const t of [...fromAlice, ...fromBashir]) await chen.waitForMessage(ch.id, t, { timeout: 120000 });

  // Each author's messages keep their order on the returning peer.
  const got = (await chen.messages(ch.id)).map((m) => m.content);
  for (const list of [fromAlice, fromBashir]) {
    const idx = list.map((t) => got.indexOf(t));
    assert.deepEqual(idx, [...idx].sort((x, y) => x - y), `out of order on Chen: ${list.join(" | ")}`);
  }

  step("and sees them in the UI");
  const c = await chen.open();
  await c.openChannel(ch.name, "Catch-up");
  await c.waitForMessage(fromAlice.at(-1));
  await c.waitForMessage(fromBashir.at(-1));
});
