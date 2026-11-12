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
