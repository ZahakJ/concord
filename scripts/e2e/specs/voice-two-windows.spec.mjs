// Two windows of one node are one participant: the second window is told the
// call is already up in the first instead of joining it again (which made the
// two take the node's one connection from each other, and closing either pull
// the node out under the other), and closing it leaves the call alone.
import { test } from "../harness.mjs";

const room = (app, name) => app.page.locator(".channel", { hasText: name }).first();
// The sidebar's call bar — whole in the call's own room, folded to one line
// while the floating call window carries the call elsewhere.
const bar = (app) => app.page.locator(".voice-bar, .voice-line");

test("a second window of the same node does not join the call twice", async ({ world, assert, step, waitFor, sleep, shot }) => {
  const [alice, bashir] = await world.peers(2);
  await world.guild({ name: "Studio", members: [alice, bashir], channels: [{ name: "Lounge", type: "voice" }] });
  const a1 = await alice.open({ viewport: "laptop", watchMedia: true });
  const b = await bashir.open({ viewport: "laptop", watchMedia: true });
  for (const app of [a1, b]) {
    await app.openGuild("Studio");
    await room(app, "Lounge").click();
    await bar(app).waitFor({ timeout: 45000 });
  }
  await b.waitForMedia("audio");
  await a1.waitForMedia("audio");

  step("a second window on Alice's node clicks Lounge: it is told, and stays out");
  const a2 = await alice.open({ viewport: "laptop", watchMedia: true });
  await a2.openGuild("Studio");
  await room(a2, "Lounge").click();
  // The toast itself, not the copy the live region holds for screen readers.
  await a2.page
    .getByText("You're already in this call in another window")
    .and(a2.page.locator(":not(.sr-only)"))
    .waitFor({ timeout: 20000 });
  await shot(a2, "second window told");
  assert.equal(await bar(a2).count(), 0, "the second window did not join");

  step("closing the second window leaves the call carrying in the first");
  await a2.close();
  // Past the node's ten-second hang-up grace for a closed window.
  await sleep(14000);
  assert.equal(await bar(a1).count(), 1, "window A is still in the call");
  await waitFor(async () => (await b.inbound()).connected === 1, { timeout: 30000, message: "Bashir to hold one connection" });
  await b.waitForMedia("audio");
  await a1.waitForMedia("audio");
  assert.equal(await b.page.getByText("Joining…").count(), 0, "no ghost tile");
});
