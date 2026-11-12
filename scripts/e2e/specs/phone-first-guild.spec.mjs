// A new account on a phone, with no guild yet: the Guilds tab is where you
// make or join one. The tab used to do nothing at all, and the switcher (from
// the strip's tile) raised the keyboard over its Create and Join rows by
// putting the caret in its search field.
import { test } from "../harness.mjs";

test("phone: with no guilds, the Guilds tab opens Create / Join without a keyboard", async ({ world, assert, step, waitFor, shot }) => {
  const [alice] = await world.peers(1);
  const a = await alice.open({ viewport: "phone" });
  const { page } = a;

  step("Tap Guilds");
  await page.locator('nav[aria-label="Sections"]').getByRole("button", { name: /^Guilds/ }).tap();
  const create = page.getByRole("button", { name: "Create a guild" }).first();
  await create.waitFor({ state: "visible" });
  await shot(a, "guilds tab with no guilds");

  step("Nothing has taken the caret, so no keyboard covers the sheet");
  const focused = await waitFor(async () => {
    const f = await page.evaluate(() => {
      const el = document.activeElement;
      return { tag: el?.tagName || "", label: el?.getAttribute("aria-label") || "" };
    });
    return f;
  });
  assert.notEqual(focused.label, "Find a guild", "the search field took the caret on a phone");
  assert.notEqual(focused.tag, "INPUT");

  step("Create a guild opens the form");
  await create.tap();
  await page.getByRole("button", { name: "Create guild" }).first().waitFor({ state: "visible" });
});
