// What's new: the deck's dots turn the page on a desktop, and in a
// right-to-left reading the chevrons point the way the pages turn.
import { test, sleep } from "../harness.mjs";

async function openDeck(alice, viewport) {
  const app = await alice.open({ viewport });
  const page = app.page;
  await page.goto(`${alice.url}/?t=${alice.token}`, { waitUntil: "domcontentloaded" });
  await page.evaluate(() => localStorage.setItem("concord.seenVersion", "0.0.1"));
  const pass = page.locator('input[aria-label="Passphrase"]');
  await pass.waitFor({ timeout: 20000 }).catch(() => {});
  if (await pass.isVisible().catch(() => false)) {
    await pass.fill(alice.passphrase);
    await pass.press("Enter");
  }
  await page.locator(".wn").waitFor({ timeout: 20000 });
  await sleep(800);
  return app;
}

// The footer's dots, not the drawings' (the hero art has circles of its own
// called dot).
const current = (page) =>
  page.evaluate(() => [...document.querySelectorAll(".wn .dots .dot")].findIndex((d) => d.getAttribute("aria-current") === "step"));

test("desktop: a dot turns to its page", async ({ world, assert, step }) => {
  const [alice] = await world.peers(1, { names: ["Alice"] });
  const app = await openDeck(alice, { width: 1280, height: 800 });
  const { page } = app;
  const dots = page.locator(".wn .dots .dot");
  const n = await dots.count();
  assert.ok(n >= 3, `a deck of ${n} pages`);
  step("click the third dot, then the second");
  await dots.nth(2).click();
  await sleep(300);
  assert.equal(await current(page), 2, "the third dot shows the third page");
  await dots.nth(1).click();
  await sleep(300);
  assert.equal(await current(page), 1, "and back to the second");
  assert.equal(app.errors.length, 0, `page errors: ${app.errors.join(" | ")}`);
});

test("right to left: forward points left, and the left arrow turns forward", async ({ world, assert }) => {
  const [alice] = await world.peers(1, { names: ["Alice"] });
  const app = await openDeck(alice, { width: 1280, height: 800 });
  const { page } = app;
  await page.evaluate(() => (document.documentElement.dir = "rtl"));
  await page.locator(".wn .dots .dot").nth(1).click();
  await sleep(300);
  // A mirrored chevron's transform is scaleX(-1): matrix(-1, 0, 0, 1, 0, 0).
  const flip = (sel) => page.locator(sel).first().evaluate((el) => getComputedStyle(el).transform);
  const mirrored = "matrix(-1, 0, 0, 1, 0, 0)";
  assert.equal(await flip(".wn .go svg"), mirrored, "Next's chevron points left");
  assert.notEqual(await flip(".wn .navbtn.back svg"), mirrored, "Back's chevron points right");
  await page.keyboard.press("ArrowLeft");
  await sleep(300);
  assert.equal(await current(page), 2, "the left arrow turns forward");
  await page.evaluate(() => (document.documentElement.dir = "ltr"));
  // A short deck (one feature) is on its last page now, and Done has no
  // chevron: go back to one that does.
  await page.locator(".wn .dots .dot").nth(1).click();
  await sleep(300);
  assert.equal(await flip(".wn .go svg"), "none", "left to right, Next's chevron is drawn as it is");
});
