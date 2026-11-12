// A disappearing message is erased on a device that never opens its channel:
// the core sweeps it, so search there has nothing to return after the timer.
import { test } from "../harness.mjs";

test("disappearing message expires on a peer that never viewed it", async ({ world, assert, step, waitFor }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Night Shift", members: [alice, bashir] });
  const ch = g.allChannels[0];

  step("Alice sends a message that disappears in 20 seconds");
  const at = Math.floor(Date.now() / 1000) + 20;
  await alice.send(ch.id, `[eph](concord://eph/v1/${at})this message should vanish`);
  await waitFor(async () => (await bashir.rpc("SearchMessages", "should vanish", "", "")).length === 1, {
    message: "Bashir never received the message",
  });

  step("nobody opens the channel; after the timer search is empty on both");
  await waitFor(
    async () =>
      (await bashir.rpc("SearchMessages", "should vanish", "", "")).length === 0 &&
      (await alice.rpc("SearchMessages", "should vanish", "", "")).length === 0,
    { timeout: 75000, interval: 2000, message: "the message is still searchable after its timer" },
  );
  const rows = (await bashir.messages(ch.id)).filter((m) => m.content?.includes("should vanish"));
  assert.equal(rows.length, 0);
});
