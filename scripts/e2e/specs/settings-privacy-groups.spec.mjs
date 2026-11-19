// The notification-content setting stays in Privacy & safety, among the other
// choices about what leaves this device, and not under a one-row
// "Notifications" heading that read like a second Notifications & sounds.
import { test } from "../harness.mjs";

for (const viewport of ["desktop", "phone"]) {
  test(`privacy keeps the notification row with what leaves this device (${viewport})`, async ({ world, assert }) => {
    const [alice] = await world.peers(1);
    const app = await alice.open({ viewport });
    await app.openSettings("Privacy");
    const dlg = app.dialog();
    await dlg.locator("section.grp", { hasText: "Notification shows" }).first().waitFor();
    const groups = await dlg.locator("section.grp").evaluateAll((els) =>
      els.map((g) => ({
        // The label's own words: the heading also holds the InfoDot button,
        // whose "i" is not part of the name.
        label: [...(g.querySelector(".sec-label")?.childNodes || [])]
          .filter((n) => n.nodeType === Node.TEXT_NODE)
          .map((n) => n.textContent)
          .join("")
          .trim(),
        text: g.textContent,
      })),
    );
    const labels = groups.map((g) => g.label);
    assert.ok(!labels.includes("Notifications"), `Privacy groups: ${labels.join(" | ")}`);
    const home = groups.find((g) => g.text.includes("Notification shows"));
    assert.ok(home, `no group holds the row: ${labels.join(" | ")}`);
    assert.equal(home.label, "What leaves this device");
    assert.deepEqual(app.errors, [], app.errors.join("\n"));
  });
}
