// The first screen on a phone: the heading top-left, the one action at the
// bottom in thumb reach and above the keyboard, fields named by their labels
// without a placeholder repeating them, and a single filled button on the
// Link screen. On a desktop the same column sits centred in the window.
import { test } from "../harness.mjs";

const PHONE = { viewport: { width: 412, height: 915 }, mobile: true };

test("login on a phone: one action at the bottom, above the keyboard; one filled button to link", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1, { names: ["Alice"] });
  await alice.rpc("Logout");
  const app = await alice.open({ unlock: false, ...PHONE });
  const page = app.page;
  await page.goto(`${alice.url}/?t=${alice.token}`, { waitUntil: "domcontentloaded" });
  const pass = page.locator('input[aria-label="Passphrase"]');
  await pass.waitFor();
  const unlock = page.getByRole("button", { name: "Unlock", exact: true });
  const box = async (l) => l.boundingBox();

  step("The heading sits at the top, the action at the bottom");
  const h1 = await box(page.locator("h1"));
  const btn = await box(unlock);
  assert.ok(h1.y < 200, `heading near the top (y=${h1.y})`);
  assert.ok(915 - (btn.y + btn.height) < 140, `Unlock in thumb reach (bottom gap ${915 - (btn.y + btn.height)})`);
  assert.equal(await pass.getAttribute("placeholder"), null, "no placeholder repeating the label");

  step("Disabled, the action is the accent faded, not the grey chip");
  const bg = await unlock.evaluate((b) => getComputedStyle(b).backgroundColor);
  const grey = await page.evaluate(() => {
    const d = document.createElement("div");
    d.style.background = "var(--bg-3)";
    document.body.append(d);
    const c = getComputedStyle(d).backgroundColor;
    d.remove();
    return c;
  });
  assert.ok(bg !== grey && /rgba|color\(/.test(bg), `a translucent accent (${bg})`);

  step("With the keyboard up the action rides above it");
  await page.evaluate(() => document.documentElement.style.setProperty("--kb", "320px"));
  await waitFor(async () => {
    const b = await box(unlock);
    return b.y + b.height <= 915 - 320;
  }, { message: "Unlock above a 320px keyboard" });
  await page.evaluate(() => document.documentElement.style.removeProperty("--kb"));

  step("Link: one filled button, the scan is tonal");
  await page.getByRole("button", { name: "Other options", exact: true }).click();
  await page.getByRole("button", { name: "Re-link from another device", exact: true }).click();
  await page.getByRole("heading", { name: /Link this device/ }).waitFor();
  assert.equal(await page.locator(".card button.primary:visible").count(), 1, "one primary on the Link screen");
});

test("login on a desktop: the column is centred in the window", async ({ world, assert }) => {
  const [alice] = await world.peers(1, { names: ["Alice"] });
  await alice.rpc("Logout");
  const app = await alice.open({ unlock: false });
  const page = app.page;
  await page.goto(`${alice.url}/?t=${alice.token}`, { waitUntil: "domcontentloaded" });
  await page.locator('input[aria-label="Passphrase"]').waitFor();
  const vp = page.viewportSize();
  const b = await page.getByRole("button", { name: "Unlock", exact: true }).boundingBox();
  const mid = b.x + b.width / 2;
  assert.ok(Math.abs(mid - vp.width / 2) < 4, `centred (button middle ${mid} of ${vp.width})`);
});
