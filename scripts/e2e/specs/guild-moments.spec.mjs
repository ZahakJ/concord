// A guild's moments say they are waiting (a ring on the guild's tile and on
// the place header), and a guild that already has one still lets you share
// yours: from its menu, and from inside the viewer.
import { test } from "../harness.mjs";

test("a moment someone shared rings the guild, and you can share one too", async ({ world, step, waitFor, shot }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Lanterns", members: [alice, bashir] });
  const a = await alice.open({ viewport: "desktop" });
  await a.openChannel(g.allChannels[0].name || "general", "Lanterns");

  step("Bashir shares a moment; Alice's tile and header ring");
  await bashir.rpc("PostStory", [g.id], "preset:galaxy", "first lantern lit");
  const tile = a.page.locator('nav[aria-label="Guilds"] .tile.moments');
  await waitFor(async () => (await tile.count()) > 0, { timeout: 60000, message: "the unseen ring on the strip tile" });
  await a.page.locator("[data-place-head] .ph-icon.moments").waitFor();
  await shot(a, "unseen ring");

  step("the guild menu offers both View and Share");
  await a.page.locator("[data-place-head] .ph-main").click({ button: "right" });
  await a.page.getByRole("menuitem", { name: /^View moments/ }).waitFor();
  await a.page.getByRole("menuitem", { name: "Share a moment" }).waitFor();
  await a.page.getByRole("menuitem", { name: /^View moments/ }).click();

  step("played to its end, the moment is seen and the ring goes");
  await a.page.getByText("first lantern lit").waitFor();
  await shot(a, "viewer");
  await waitFor(async () => (await tile.count()) === 0, { timeout: 20000, message: "the ring to clear once watched" });
  await shot(a, "ring gone");

  step("the viewer has Share a moment, which opens the composer for this guild");
  await a.page.locator("[data-place-head] .ph-main").click({ button: "right" });
  await a.page.getByRole("menuitem", { name: /^View moments/ }).click();
  await a.page.getByRole("button", { name: "Share a moment" }).click();
  // One guild to share to: the composer names it rather than offering a list.
  await a.dialog().getByText(/Sharing to:\s*Lanterns/).waitFor();
  await a.closeModals();
});
