// The app with no mouse at all: unlock, move between channels, send, react,
// edit, open Settings, walk its panes, and Escape back out to where you were.
// Also the one-press Escape out of a railed dialog after a pane switch, which
// used to leave the dialog's layer off the navigation stack every other click.
import { test } from "../harness.mjs";

const focused = (page) =>
  page.evaluate(() => {
    const el = document.activeElement;
    if (!el || el === document.body) return { tag: "BODY", label: "", rail: false, row: "" };
    return {
      tag: el.tagName,
      label: (el.getAttribute("aria-label") || el.textContent || el.placeholder || "").trim().replace(/\s+/g, " "),
      rail: !!el.closest("[data-nav-rail]"),
      row: el.closest("[data-msg-id]")?.dataset.msgId || "",
      draft: el.classList.contains("draft"),
    };
  });

// Shift+Tab (or Tab) until `want(focus)` holds; the walk is bounded.
async function tabTo(page, want, { back = false, max = 40 } = {}) {
  for (let i = 0; i < max; i++) {
    const f = await focused(page);
    if (want(f)) return f;
    await page.keyboard.press(back ? "Shift+Tab" : "Tab");
  }
  throw new Error(`no focus stop matched after ${max} presses; last: ${JSON.stringify(await focused(page))}`);
}

test("Escape closes Settings after any number of pane switches", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1);
  const a = await alice.open();
  const gear = a.page.locator('button[aria-label="Settings"]').last();
  for (const panes of [["Appearance"], ["Appearance", "Connection"], ["Appearance", "Connection", "Notifications"]]) {
    step(`Settings → ${panes.join(" → ")} → Escape`);
    await gear.focus();
    await a.page.keyboard.press("Enter");
    await a.dialog().waitFor({ state: "visible" });
    for (const p of panes) {
      await a.page.locator("[data-nav-rail] button").filter({ hasText: p }).first().click();
      await a.page.locator('[role="dialog"] h3').filter({ hasText: p }).first().waitFor();
    }
    await a.page.keyboard.press("Escape");
    await waitFor(async () => (await a.page.locator('[role="dialog"]').count()) === 0, {
      message: `one Escape to close Settings after ${panes.join(" → ")}`,
    });
    const f = await waitFor(async () => {
      const x = await focused(a.page);
      return x.label === "Settings" && x;
    }, { message: "focus back on the Settings button" });
    assert.equal(f.label, "Settings");
  }
});

test("the core loop works from the keyboard alone", async ({ world, assert, step, waitFor }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Keys", members: [alice, bashir], channels: ["random"] });
  const general = g.channel("general");
  const random = g.channel("random");
  await bashir.send(general, "hello alice");

  step("unlock by typing the passphrase");
  // Locked, the way a person finds the app after closing it.
  await alice.stop();
  await alice.start();
  const a = await alice.open({ unlock: false });
  const page = a.page;
  await page.goto(`${alice.url}/?t=${alice.token}`, { waitUntil: "domcontentloaded" });
  const pass = page.locator('input[placeholder="Passphrase"]');
  await pass.waitFor({ state: "visible" });
  await waitFor(async () => (await focused(page)).tag === "INPUT", { message: "focus starts in the passphrase field" });
  await page.keyboard.type(alice.passphrase);
  await page.keyboard.press("Enter");
  await page.locator('nav[aria-label="Guilds"]').waitFor({ state: "attached" });
  await a.closeModals();

  step("Ctrl+K to the guild's #general, Alt+↓ to #random");
  await page.keyboard.press("Control+k");
  await page.keyboard.type("general");
  await page.keyboard.press("Enter");
  await waitFor(async () => (await a.composer().getAttribute("placeholder"))?.includes("general"), { message: "in #general" });
  await page.keyboard.press("Alt+ArrowDown");
  await waitFor(async () => (await a.composer().getAttribute("placeholder"))?.includes("random"), { message: "Alt+↓ to #random" });

  step("send");
  await waitFor(async () => (await focused(page)).draft, { message: "the composer has focus after a channel switch" });
  await page.keyboard.type("typed without a mouse");
  await page.keyboard.press("Enter");
  const sent = await alice.waitForMessage(random, "typed without a mouse");

  step("Escape into the feed, Tab into the row's actions, react");
  await page.keyboard.press("Escape");
  const row = await waitFor(async () => {
    const f = await focused(page);
    return f.row && f.tag === "DIV" && f;
  }, { message: "Escape from the empty composer lands on a message row" });
  assert.equal(row.row, sent.id, "the row is the newest message");
  // The feed is one tab stop: only the current row's own controls can be tabbed to.
  const outside = await page.evaluate(() => {
    const cur = document.activeElement.closest("[data-msg-id]");
    return [...document.querySelectorAll('[role="log"] [data-msg-id] button, [role="log"] [data-msg-id] a[href]')].filter(
      (el) => el.tabIndex >= 0 && el.closest("[data-msg-id]") !== cur,
    ).length;
  });
  assert.equal(outside, 0, "no other row's controls are in the Tab order");
  const react = await tabTo(page, (f) => /^React /.test(f.label), { max: 15 });
  await page.keyboard.press("Enter");
  const emoji = react.label.replace(/^React /, "");
  await waitFor(async () => {
    const m = (await alice.messages(random)).find((x) => x.id === sent.id);
    return m?.reactions?.[emoji]?.length;
  }, { message: `the ${emoji} reaction was stored` });

  step("type to get back to the composer, ↑ to edit the last message");
  await page.keyboard.type("x");
  await waitFor(async () => (await focused(page)).draft, { message: "a letter pulls focus to the composer" });
  await page.keyboard.press("Backspace");
  await page.keyboard.press("ArrowUp");
  await waitFor(async () => (await focused(page)).tag === "TEXTAREA" && !(await focused(page)).draft, { message: "the edit box has focus" });
  await page.keyboard.press("Control+a");
  await page.keyboard.type("edited without a mouse");
  await page.keyboard.press("Enter");
  await waitFor(async () => (await alice.messages(random)).find((x) => x.id === sent.id)?.content === "edited without a mouse", {
    message: "the edit was stored",
  });

  step("Ctrl+, opens Settings; walk to a pane on the rail; Escape closes; focus comes back");
  await waitFor(async () => (await focused(page)).tag !== "BODY", { message: "focus is somewhere after saving an edit" });
  const before = await focused(page);
  await page.keyboard.press("Control+,");
  await a.dialog().waitFor({ state: "visible" });
  await tabTo(page, (f) => f.rail && /Appearance/.test(f.label), { back: true });
  await page.keyboard.press("Enter");
  await page.locator('[role="dialog"] h3').filter({ hasText: "Appearance" }).waitFor();
  await tabTo(page, (f) => f.rail && /Notifications/.test(f.label), { back: true });
  await page.keyboard.press("Enter");
  await page.locator('[role="dialog"] h3').filter({ hasText: "Notifications" }).waitFor();
  await page.keyboard.press("Escape");
  await waitFor(async () => (await page.locator('[role="dialog"]').count()) === 0, { message: "one Escape closes Settings" });
  const after = await waitFor(async () => {
    const f = await focused(page);
    return f.tag !== "BODY" && f;
  }, { message: "focus is not dropped on the page body" });
  assert.equal(after.label, before.label, "focus returns to where it was before Settings opened");
});

// The channel list and the member list are one Tab stop each. Their rows used
// to be three and two stops (the row, then hover-only chevron, bell and ⋯
// buttons), with no arrow keys at all, so getting from the inspector to the
// composer took about forty Tab presses.
test("the channel and member lists are one Tab stop each, with arrows and the row menu", async ({ world, assert, step, waitFor }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Lists", members: [alice, bashir], channels: ["random", "tools"] });
  await bashir.send(g.channel("general"), "hello");
  const a = await alice.open();
  const page = a.page;
  await a.openChannel("general", "Lists");

  const where = () =>
    page.evaluate(() => {
      const el = document.activeElement;
      return {
        channel: el?.classList.contains("channel") ? el.querySelector(".ch-name")?.textContent.trim() : "",
        member: el?.classList.contains("member") ? el.querySelector(".mname")?.textContent.trim() : "",
        inChannels: !!el?.closest(".channel-row, .cat-head"),
        inMembers: !!el?.closest(".member-row"),
      };
    });
  // How many of a list's controls the Tab key can land on.
  const stops = (sel) =>
    page.evaluate((sel) => [...document.querySelectorAll(sel)].filter((el) => el.tabIndex >= 0 && el.offsetParent !== null).length, sel);

  step("F6 to the sidebar lands on the open channel; the list is one stop");
  for (let i = 0; i < 6 && (await where()).channel !== "general"; i++) await page.keyboard.press("F6");
  assert.equal((await where()).channel, "general", "F6 lands on the open channel's row");
  assert.equal(
    await stops(".channel-row button, .channel-row .trigger, .cat-head button"),
    1,
    "one control in the channel list is in the Tab order",
  );

  step("↓ moves to the next channel without opening it; a letter jumps by name");
  await page.keyboard.press("ArrowDown");
  await waitFor(async () => (await where()).channel === "random", { message: "↓ moved to #random" });
  assert.ok((await a.composer().getAttribute("placeholder"))?.includes("general"), "the arrow did not switch channels");
  await page.keyboard.press("t");
  await waitFor(async () => (await where()).channel === "tools", { message: "typing t jumped to #tools" });
  await page.keyboard.press("Home");
  await waitFor(async () => (await where()).channel === "general", { message: "Home went to the first row" });

  step("Shift+F10 opens the row's menu; Escape closes it and focus stays on the row");
  await page.keyboard.press("ArrowDown");
  await waitFor(async () => (await where()).channel === "random", { message: "back on #random" });
  await page.keyboard.press("Shift+F10");
  const menu = page.locator('[role="menu"]').filter({ hasText: "Mark as read" });
  await menu.first().waitFor({ state: "visible" });
  assert.ok(await menu.getByText("Nothing", { exact: true }).count(), "the menu holds the mute level the bell used to toggle");
  await page.keyboard.press("Escape");
  await waitFor(async () => (await page.locator('[role="menu"]').count()) === 0, { message: "Escape closed the menu" });
  await waitFor(async () => (await where()).channel === "random", { message: "focus came back to the row" });

  step("one Tab leaves the channel list; Shift+Tab comes back to the row you were on");
  await page.keyboard.press("Tab");
  await waitFor(async () => !(await where()).inChannels, { message: "a single Tab left the channel list" });
  await page.keyboard.press("Shift+Tab");
  await waitFor(async () => (await where()).channel === "random", { message: "Shift+Tab returned to #random" });

  step("the member list: one stop, arrows, and the row menu");
  if (!(await page.locator(".member").count())) await page.keyboard.press("Control+u");
  await page.locator(".member").first().waitFor({ state: "visible" });
  // Focus moves a frame after the key, so each press is given that frame
  // before the next one is decided on: pressing ahead of it walked past the
  // roster on a loaded machine.
  const settle = () => page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
  for (let i = 0; i < 6 && !(await where()).inMembers; i++) {
    await page.keyboard.press("F6");
    await settle();
    if ((await where()).inMembers) break;
    // F6 lands on the inspector's first control; the roster is the next stop.
    for (let j = 0; j < 4 && !(await where()).inMembers; j++) {
      if (!(await page.evaluate(() => !!document.activeElement?.closest('aside[aria-label="Inspector"]')))) break;
      await page.keyboard.press("Tab");
      await settle();
    }
  }
  assert.ok((await where()).inMembers, "Tab from the inspector's head reached the roster");
  assert.equal(await stops(".member-row button"), 1, "one control in the member list is in the Tab order");
  const first = (await where()).member;
  await page.keyboard.press("ArrowDown");
  await waitFor(async () => {
    const w = await where();
    return w.member && w.member !== first;
  }, { message: "↓ moved to the next member" });
  await page.keyboard.press("Shift+F10");
  await page.locator('[role="menu"]').filter({ hasText: "View profile" }).first().waitFor({ state: "visible" });
  await page.keyboard.press("Escape");
  await waitFor(async () => (await page.locator('[role="menu"]').count()) === 0, { message: "Escape closed the member menu" });
  await page.keyboard.press("Tab");
  await waitFor(async () => !(await where()).inMembers, { message: "a single Tab left the member list" });
});
