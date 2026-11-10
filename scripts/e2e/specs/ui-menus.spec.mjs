// The shared dropdown (Menu.svelte, and Select on top of it): opening one
// inside a form must not submit the form, it must take the keyboard, and it
// must draw above the dialog that opened it.
import { test } from "../harness.mjs";

// Which of the open menu's rows can actually be clicked: a row whose centre
// hit-tests to something else is painted underneath another surface.
async function hiddenRows(page) {
  return page.evaluate(() =>
    [...document.querySelectorAll('[role="menu"] .menu-item')]
      .filter((el) => {
        const r = el.getBoundingClientRect();
        if (!r.width || !r.height) return true;
        return !el.contains(document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2));
      })
      .map((el) => el.textContent.trim()),
  );
}

const focused = (page) =>
  page.evaluate(() => {
    const el = document.activeElement;
    return { role: el?.getAttribute("role") || "", label: (el?.getAttribute("aria-label") || el?.textContent || "").trim() };
  });

test("the composer's + menu keeps the draft and takes the keyboard", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Menus", members: [alice] });
  const a = await alice.open();
  await a.openChannel(g.allChannels[0].name, "Menus");

  step("type a draft, open + with the mouse");
  await a.composer().fill("not finished yet");
  await a.page.getByRole("button", { name: "Add to your message" }).click();
  await a.page.locator('[role="menu"]').waitFor({ state: "visible" });
  assert.equal(await a.composer().inputValue(), "not finished yet", "opening the menu must not send the draft");
  assert.equal((await alice.messages(g.allChannels[0].id)).filter((m) => m.content === "not finished yet").length, 0);

  step("focus is in the menu, arrows rove, Escape gives it back to the trigger");
  const first = await waitFor(async () => {
    const f = await focused(a.page);
    return f.role === "menuitem" && f;
  });
  assert.match(first.label, /Upload a file/);
  await a.page.keyboard.press("ArrowDown");
  assert.match((await focused(a.page)).label, /GIF/);
  await a.page.keyboard.press("End");
  assert.match((await focused(a.page)).label, /Advanced composer/);
  await a.page.keyboard.press("Escape");
  await waitFor(async () => (await focused(a.page)).label === "Add to your message", { message: "focus back on +" });
  assert.equal(await a.page.locator('[role="menu"]').count(), 0);
});

test("the Category picker in Create channel opens instead of creating the channel", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Picker", members: [alice] });
  // The picker only exists once the guild has a category to offer.
  await alice.rpc("CreateCategory", g.id, "Staff Room");
  const a = await alice.open();
  await a.openChannel(g.allChannels[0].name, "Picker");

  await a.page.getByRole("button", { name: "Add channel or category" }).click();
  await a.page.locator('[role="menu"] .menu-item').filter({ hasText: /channel/i }).first().click();
  const dlg = a.dialog();
  await dlg.waitFor({ state: "visible" });
  await dlg.locator('input[type="text"], input:not([type])').first().fill("staff-room");

  step("open the Category picker");
  await dlg.getByRole("button", { name: /^Category/ }).click();
  await a.page.locator('[role="menu"]').waitFor({ state: "visible" });
  assert.equal(await a.page.locator('[role="dialog"]').count(), 1, "the dialog is still open");
  const guild = (await alice.guilds()).find((x) => x.id === g.id);
  assert.ok(!guild.channels.some((c) => c.name === "staff-room"), "no channel was created by opening the picker");
  assert.deepEqual(await hiddenRows(a.page), [], "every option is drawn above the dialog");
  await waitFor(async () => (await focused(a.page)).role === "menuitem", { message: "focus in the picker" });
});

test("device pickers in Settings draw above the settings dialog", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1);
  const a = await alice.open();
  const dlg = await a.openSettings();
  await dlg.locator("[data-nav-rail]").getByText("Voice & video").click();
  const mic = a.page.getByRole("button", { name: /^Microphone/ });
  await mic.waitFor({ state: "visible" });

  step("open the microphone picker with the keyboard");
  await mic.focus();
  await a.page.keyboard.press("Enter");
  await a.page.locator('[role="menu"]').waitFor({ state: "visible" });
  assert.deepEqual(await hiddenRows(a.page), [], "every device row can be clicked");
  await waitFor(async () => (await focused(a.page)).role === "menuitem", { message: "focus on the current device" });
  await a.page.keyboard.press("Escape");
  await waitFor(async () => /^Microphone/.test((await focused(a.page)).label), { message: "focus back on the picker" });
  assert.equal(await a.page.locator('[role="dialog"]').count(), 1, "Escape closed the picker, not Settings");
});
