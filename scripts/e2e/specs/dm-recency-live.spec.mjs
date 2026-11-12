// The DM list follows arrivals while you watch it: a conversation that just
// got a message moves to the top and shows that message as its preview, with
// no reload and no other event to prompt a re-fetch. The list's order and
// preview are written onto the guild in place when a message arrives (the
// whole guild list is no longer rebuilt per message), so this holds the list
// to actually re-sorting on that write.
import { test } from "../harness.mjs";

test("a DM that gets a message moves to the top of the list, live", async ({ world, waitFor, step, assert }) => {
  const [alice, bashir, chen] = await world.peers(3);
  await world.guild({ name: "Mutual", members: [alice, bashir, chen] });
  await world.dm(alice, bashir);
  await world.dm(alice, chen);

  const a = await alice.open();
  await a.openDM("Chen");
  const order = () =>
    a.page.locator(".dm-item .dm-name").evaluateAll((els) => els.map((e) => e.textContent.trim()));
  const rowOf = (name) => a.page.locator(".dm-item").filter({ has: a.page.locator(".dm-name", { hasText: name }) });

  step("Chen spoke last, so Chen is above Bashir");
  await waitFor(async () => {
    const o = await order();
    return o.indexOf("Chen") >= 0 && o.indexOf("Chen") < o.indexOf("Bashir");
  }, { message: "Chen above Bashir" });

  step("Bashir writes; his row climbs above Chen's and previews the line");
  const line = "bashir again, top of the list please";
  const dmId = (await bashir.guilds()).find((g) => g.kind === "dm" && (g.channels || []).length && g.name.includes("Alice"));
  assert.ok(dmId, "Bashir's DM with Alice");
  await bashir.send(dmId.channels[0].id, line);
  await waitFor(async () => {
    const o = await order();
    return o.indexOf("Bashir") >= 0 && o.indexOf("Bashir") < o.indexOf("Chen");
  }, { message: "Bashir's DM to move above Chen's" });
  await waitFor(async () => ((await rowOf("Bashir").first().textContent()) || "").includes(line), {
    message: "Bashir's row to preview his new line",
  });
});
