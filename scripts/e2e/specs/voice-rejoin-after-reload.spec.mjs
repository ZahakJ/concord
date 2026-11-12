// Reloading mid-call leaves the call (a reloaded page is a new client), and
// used to say nothing: the page came back on #general as if no call had been
// up. It now offers the way back, and the way back works.
import { test } from "../harness.mjs";

const room = (app, name) => app.page.locator(".channel", { hasText: name }).first();
// The sidebar's call bar — whole in the call's own room, folded to one line
// while the floating call window carries the call elsewhere.
const bar = (app) => app.page.locator(".voice-bar, .voice-line");

test("a reload mid-call offers Rejoin, and Rejoin puts you back", async ({ world, step, shot }) => {
  const [alice, bashir] = await world.peers(2);
  await world.guild({ name: "Studio", members: [alice, bashir], channels: [{ name: "Lounge", type: "voice" }] });
  const a = await alice.open({ viewport: "laptop", watchMedia: true });
  const b = await bashir.open({ viewport: "laptop", watchMedia: true });
  for (const app of [a, b]) {
    await app.openGuild("Studio");
    await room(app, "Lounge").click();
    await bar(app).waitFor({ timeout: 45000 });
  }
  await a.waitForMedia("audio");

  step("Alice reloads: she is out of the call and told so, with Rejoin");
  await a.reload();
  const card = a.page.locator(".ring-card", { hasText: "You left Lounge · Studio" });
  await card.waitFor({ timeout: 30000 });
  await shot(a, "after reload");

  step("Rejoin: back in the call, hearing Bashir");
  await card.getByRole("button", { name: "Rejoin" }).click();
  await bar(a).waitFor({ timeout: 45000 });
  await a.waitForMedia("audio", { timeout: 60000 });
});
