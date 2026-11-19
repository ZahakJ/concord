// Every section of settings opens, on desktop and on a phone, without a
// console error or an uncaught exception.
import { test } from "../harness.mjs";

for (const viewport of ["desktop", "phone"]) {
  test(`every settings section opens cleanly (${viewport})`, async ({ world, assert, step }) => {
    const [alice] = await world.peers(1);
    const app = await alice.open({ viewport });
    await app.openSettings();
    const sections = await app.settingsSections();
    assert.ok(sections.length >= 5, `only ${sections.length} settings sections: ${sections.join(", ")}`);
    for (const title of sections) {
      step(title);
      await app.settingsSection(title);
    }
    await app.closeModals();
    assert.deepEqual(app.errors, [], app.errors.join("\n"));
  });
}

// On a phone the open conversation keeps its closed right page (a DM's Media
// page) mounted as an aria-hidden role="dialog", also while the home tabs are
// shown. Settings still open over it, and the harness's dialog() is Settings,
// not that closed page.
test("settings open on a phone with a DM's Media page closed", async ({ world, assert }) => {
  const [alice, bashir] = await world.peers(2);
  await world.guild({ name: "Shared", members: [alice, bashir] });
  await world.dm(alice, bashir);
  const app = await bashir.open({ viewport: "phone" });
  await app.openDM("Alice");
  await app.settled();
  await app.back();
  const closed = app.page.locator('aside[role="dialog"][aria-label="Members"][aria-hidden="true"]');
  assert.equal(await closed.count(), 1, "the DM's closed right page is mounted");
  await app.openSettings();
  const sections = await app.settingsSections();
  assert.ok(sections.includes("Appearance"), `sections: ${sections.join(", ")}`);
  await app.settingsSection("Appearance");
  assert.notEqual(await app.dialog().getAttribute("aria-label"), "Members", "dialog() is Settings");
  assert.equal(await closed.count(), 1, "the closed right page is still there");
  assert.deepEqual(app.errors, [], app.errors.join("\n"));
});
