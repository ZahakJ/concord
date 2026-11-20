// A profile wearing a ring or a decoration this build no longer offers is
// drawn as the kept one nearest to it, on everyone else's screen, without
// rewriting the profile: the ids it carries (and the card effect and frame
// fields nothing draws any more) are exactly what was saved.
import { test } from "../harness.mjs";

test("a removed ring and decoration are drawn as the kept ones", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Wardrobe", members: [alice, bashir] });
  const ch = g.allChannels[0];
  const id = await bashir.rpc("Identity");
  const style = JSON.stringify({ dec: "chainmail", dc: "gold", cf: "old-card", sat: "🚀" });
  await bashir.rpc("SetProfile", id.displayName || "Bashir", "", "", "#3366cc", "", "", "online", "", "", "binary", "sparkle-card", style, "");
  await bashir.send(ch.id, "wearing old things");

  const a = await alice.open({ viewport: "desktop" });
  await a.openChannel(ch.name, "Wardrobe");
  await a.waitForMessage("wearing old things");

  step("Alice sees the Orbit ring (for Binary stars) and Gold filigree (for Chainmail)");
  await waitFor(async () => (await a.page.locator('[data-ring="orbit"]').count()) > 0, { message: "an orbit ring drawn" });
  await waitFor(async () => (await a.page.locator('[data-dec="gold-filigree"]').count()) > 0, { message: "gold filigree drawn" });
  assert.equal(await a.page.locator('[data-ring="binary"], [data-dec="chainmail"]').count(), 0);
  await shot(a, "mapped");

  step("nothing was rewritten: the profile still carries what it was saved with");
  const me = await bashir.rpc("Identity");
  assert.equal(me.frame, "binary");
  assert.equal(me.effect, "sparkle-card");
  assert.equal(me.style?.dec, "chainmail");
  assert.equal(me.style?.cf, "old-card");

  step("a drawn ring that predates the merge, in `frame`, keeps its circle");
  await bashir.rpc("SetProfile", id.displayName || "Bashir", "", "", "#3366cc", "", "", "online", "", "", "sunburst-crown", "", "", "");
  await bashir.send(ch.id, "a legacy frame");
  await a.waitForMessage("a legacy frame");
  await waitFor(async () => (await a.page.locator('[data-dec="gold-filigree"]').count()) > 0, { message: "drawn as gold filigree" });
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});
