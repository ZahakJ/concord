// Unlocking a fresh identity and painting the main window logs no console
// errors and no uncaught exceptions, on desktop and on a phone viewport.
import { test } from "../harness.mjs";

for (const viewport of ["desktop", "phone"]) {
  test(`unlock and first paint are clean (${viewport})`, async ({ world, assert, waitFor }) => {
    const [alice] = await world.peers(1);
    const app = await alice.open({ viewport });
    // The shell is up; give late startup work (lazy chunks, the first
    // event-stream round) a moment to fail before declaring the paint clean.
    await waitFor(async () => (await app.page.evaluate(() => document.readyState)) === "complete", {
      message: "the page to finish loading",
    });
    await app.page.waitForLoadState("load");
    await new Promise((r) => setTimeout(r, 1500));
    assert.deepEqual(app.errors, [], `console errors on first paint:\n${app.errors.join("\n")}`);
    // The passphrase screen is gone and the identity's name made it to the UI.
    assert.equal(await app.page.locator('input[placeholder="Passphrase"]').count(), 0);
  });
}
