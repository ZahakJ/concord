// The phone's in-call strip sits over the message list; its tint is
// translucent, so it has to be laid over an opaque ground or scrolled
// messages show through it and collide with its labels.
import { test } from "../harness.mjs";

test("phone: the in-call strip is opaque over the messages", async ({ world, assert, step, waitFor, seedMessages, shot }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Studio", members: [alice, bashir], channels: [{ name: "Lounge", type: "voice" }] });
  const general = g.allChannels.find((c) => c.name === "general");
  await seedMessages(bashir, general.id, 30, { prefix: "line" });
  const a = await alice.open({ viewport: "phone" });

  step("Alice joins Lounge on her phone, then reads #general under the strip");
  await a.openChannel("general", "Studio");
  await a.back();
  await a.page.locator(".channel", { hasText: "Lounge" }).first().click();
  const strip = a.page.locator(".phone-call .callbar");
  await waitFor(async () => (await a.page.locator(".phone-call").count()) > 0 || (await a.page.locator(".voice-bar").count()) > 0, {
    timeout: 45000,
    message: "the call to come up",
  });
  await a.openChannel("general", "Studio");
  await strip.waitFor({ timeout: 30000 });
  await shot(a, "strip over messages");

  // Computed colours come back as rgb(), rgba() or color(srgb … / a); the
  // alpha is the fourth number or whatever follows the slash.
  const bg = await strip.evaluate((el) => getComputedStyle(el).backgroundColor);
  const slash = bg.match(/\/\s*([\d.]+)/);
  const nums = bg.match(/[\d.]+/g) || [];
  const alpha = slash ? Number(slash[1]) : /^rgba/.test(bg) ? Number(nums[3]) : 1;
  step(`strip background: ${bg}`);
  assert.equal(alpha, 1, "the strip's ground is opaque");
});
