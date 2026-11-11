// While you are in a call and looking anywhere but the call's own room —
// another channel, another guild, a DM, Settings — the floating call window is
// on screen with working controls and a way back, and it shows a peer's shared
// screen live. The sidebar's call bar folds to one line meanwhile, so the call
// is drawn once. Back in the call's room the window goes away.
import { test } from "../harness.mjs";

const dock = (app) => app.page.locator(".dock");

async function dockShown(app, waitFor, where) {
  await waitFor(async () => await dock(app).isVisible(), { timeout: 15000, message: `the call window ${where}` });
}

test("the floating call window follows you out of the call's room and back", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bashir] = await world.peers(2);
  await world.guild({ name: "Studio", members: [alice, bashir], channels: [{ name: "Lounge", type: "voice" }] });
  await world.guild({ name: "Annex", members: [alice, bashir] });
  await world.dm(alice, bashir);
  const a = await alice.open({ viewport: "desktop", watchMedia: true });
  const b = await bashir.open({ viewport: "desktop", watchMedia: true });

  step("both join Lounge from #general: joining leaves you where you were, so the window is up at once");
  for (const app of [a, b]) {
    await app.openChannel("general", "Studio");
    await app.page.locator(".channel", { hasText: "Lounge" }).first().click();
    await dockShown(app, waitFor, `for ${app.peer.name} in #general`);
  }
  await a.waitForMedia("audio");
  await a.page.locator(".voice-line").waitFor({ state: "visible" });
  assert.equal(await a.page.locator(".voice-bar").count(), 0, "the full sidebar bar is folded away");
  assert.match(await a.page.locator(".voice-line").innerText(), /In call · Lounge[\s\S]*Return/);
  await shot(a, "window in general");

  step("its Mute works");
  await dock(a).getByRole("button", { name: "Mute", exact: true }).click();
  await waitFor(async () => (await dock(a).getByRole("button", { name: "Unmute", exact: true }).getAttribute("aria-pressed")) === "true", {
    message: "the window's mute to hold",
  });
  await dock(a).getByRole("button", { name: "Unmute", exact: true }).click();
  await dock(a).getByRole("button", { name: "Mute", exact: true }).waitFor();

  step("another guild, a DM, and Settings");
  await a.openGuild("Annex");
  await a.page.locator(".channel").first().click();
  await dockShown(a, waitFor, "in another guild");
  await a.openDM("Bashir");
  await dockShown(a, waitFor, "in a DM");
  await a.openSettings("Appearance");
  await dockShown(a, waitFor, "over Settings");
  await shot(a, "window over settings");
  await a.closeModals();

  step("Bashir shares his screen: the window plays it, labelled");
  await b.page.getByRole("button", { name: "Share screen" }).first().click();
  await a.waitForMedia("video", { timeout: 60000 });
  await waitFor(
    async () => (await dock(a).locator("video").first().evaluate((v) => v.videoWidth).catch(() => 0)) > 0,
    { timeout: 45000, message: "a live picture in the window" },
  );
  assert.match(await dock(a).locator(".share-tag").innerText(), /Bashir's screen/);
  await shot(a, "window with share");

  step("Return opens the call: no window over the stage, nor over the room's own chat");
  await dock(a).getByRole("button", { name: "Return to call" }).click();
  await waitFor(async () => !(await dock(a).isVisible()), { message: "the window to go with the stage open" });
  await shot(a, "stage");
  await a.page.keyboard.press("Escape");
  await a.openGuild("Studio");
  await a.page.locator(".channel", { hasText: "Lounge" }).first().click({ button: "right" });
  await a.page.getByRole("menuitem", { name: "Open chat" }).click();
  await waitFor(async () => (await a.composer().getAttribute("placeholder"))?.includes("Lounge"), { message: "Lounge's chat" });
  await a.page.locator(".voice-bar").waitFor({ state: "visible" });
  assert.equal(await dock(a).isVisible(), false, "no window in the call's own room");
  await shot(a, "back in lounge");
});
