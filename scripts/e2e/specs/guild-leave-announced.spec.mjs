// When a member leaves, everyone else's member list drops them and the
// channel says so, instead of listing them online for good.
import { test } from "../harness.mjs";

test("a member who leaves disappears from everyone's list", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bashir, chen] = await world.peers(3, { names: ["Alice", "Bashir", "Chen"] });
  const g = await world.guild({ name: "Open Door", members: [alice, bashir, chen] });
  const ch = g.allChannels[0];
  const a = await alice.open();
  await a.openChannel(ch.name, "Open Door");
  await a.page.locator(".mname", { hasText: "Chen" }).first().waitFor({ state: "visible" });

  step("Chen leaves");
  await chen.rpc("LeaveGuild", g.id);
  await waitFor(async () => (await a.page.locator(".mname", { hasText: "Chen" }).count()) === 0, {
    timeout: 30000,
    message: "Alice's member list still shows Chen",
  });
  await a.page.getByText("left the guild").first().waitFor({ state: "visible" });
  await shot(a, "after chen left");
  const members = await bashir.rpc("Members", g.id);
  assert.equal(members.some((m) => m.fingerprint === chen.fingerprint), false);
});
