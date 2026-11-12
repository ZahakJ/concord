// The phone composer's chrome leaves room for the conversation: the + sheet
// stays inside the screen above the composer, and Formatting is one row of
// finger-sized buttons (it wrapped to three rows, ~150px, which with the
// keyboard up left about a hundred pixels of feed). Appearance on a phone does
// not offer the desktop's window layouts, which do nothing there, and a page
// opened from the You tab stays inside that tab.
import { test } from "../harness.mjs";

test("phone: the + sheet fits, Formatting is one row, and Layout is not offered", async ({ world, assert, step, waitFor, shot }) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Pocket", members: [alice] });
  const ch = g.allChannels[0];
  // A keyboard-sized viewport: what is left of a phone with the IME up.
  const a = await alice.open({ viewport: { width: 390, height: 450 }, mobile: true });
  const { page } = a;
  await a.openChannel(ch.name, "Pocket");
  await a.composer().tap();
  await a.composer().fill("washi paper and PVA");

  step("The + sheet opens inside the viewport");
  await page.getByRole("button", { name: /^Attach a file/ }).tap();
  const formatting = page.getByRole("button", { name: "Formatting" });
  await formatting.waitFor({ state: "visible" });
  // Polled: the sheet slides up from below the edge before it settles.
  const inside = async () => {
    const r = await page.locator(".bs-sheet").last().evaluate((e) => {
      const b = e.getBoundingClientRect();
      return { top: b.top, bottom: b.bottom, left: b.left, right: b.right };
    });
    return r.top >= 0 && r.bottom <= 450 + 1 && r.left >= 0 && r.right <= 390 + 1 && r;
  };
  await waitFor(inside, { message: "the + sheet settled inside the viewport" });

  step("Formatting is a single row of 44px buttons");
  await formatting.tap();
  const bar = page.locator(".composer .fmt-bar").first();
  await bar.waitFor({ state: "visible" });
  const m = await bar.evaluate((el) => {
    const btns = [...el.querySelectorAll(".fmtbtn")].map((b) => b.getBoundingClientRect());
    return {
      h: el.getBoundingClientRect().height,
      rows: new Set(btns.map((b) => Math.round(b.top))).size,
      minW: Math.min(...btns.map((b) => b.width)),
      minH: Math.min(...btns.map((b) => b.height)),
    };
  });
  await shot(a, "formatting with keyboard-sized viewport");
  assert.equal(m.rows, 1, `formatting buttons on ${m.rows} rows`);
  assert.ok(m.h <= 64, `formatting bar is ${m.h}px tall`);
  assert.ok(m.minW >= 44 && m.minH >= 44, `smallest formatting button ${m.minW}x${m.minH}`);

  step("Appearance has no Layout choice on a phone");
  await a.close();
  const p = await alice.open({ viewport: "phone" });
  await p.page.getByRole("button", { name: /^You/ }).tap();
  await p.page.getByRole("button", { name: /^Appearance/ }).first().tap();
  await p.page.getByRole("radiogroup", { name: "Theme" }).first().waitFor({ state: "visible" });
  assert.equal(await p.page.getByRole("radiogroup", { name: "Layout" }).count(), 0, "Layout offered on a phone");
  // The theme previews stretch across their card in every engine; the
  // Android WebView's own button stylesheet otherwise squeezes them to slivers.
  const card = p.page.getByRole("radiogroup", { name: "Theme" }).getByRole("radio").first();
  assert.equal(await card.evaluate((e) => getComputedStyle(e).alignItems), "stretch");

  step("A page drilled into from the You tab keeps the tab bar, lit, and has no ×");
  const you = p.page.locator('nav[aria-label="Sections"]').getByRole("button", { name: /^You/ });
  await you.waitFor({ state: "visible" });
  assert.equal(await you.getAttribute("aria-current"), "page");
  assert.equal(await p.page.locator(".dialog").last().getByRole("button", { name: "Close" }).count(), 0, "a × beside the ‹");
  await shot(p, "appearance inside the You tab");
  await you.tap();
  await p.page.getByRole("button", { name: /^Appearance/ }).first().waitFor({ state: "visible" });
});
