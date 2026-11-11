// The shell stays calm under a pointer: no focus ring on a dialog opened with
// the mouse (rings wait for the keyboard), the accent stays teal whatever the
// profile colour, a grouped row's hover time never runs into its text, and the
// feed shows one message action bar at a time.
import { test } from "../harness.mjs";

const activeOutline = (page) =>
  page.evaluate(() => {
    const el = document.activeElement;
    return { label: (el?.getAttribute("aria-label") || el?.textContent || "").trim().slice(0, 40), outline: getComputedStyle(el).outlineStyle };
  });

test("a dialog opened with the mouse paints no focus ring until a key is pressed", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1);
  const a = await alice.open();
  step("open Settings from the palette: clicked open, a word typed, Enter");
  // Typing is writing, not navigating: the browser's own guess rings the
  // dialog's first button here, because the last thing pressed was a key.
  await a.page.getByRole("button", { name: /Jump to/ }).first().click();
  await a.page.keyboard.type("Open settings");
  await a.page.keyboard.press("Enter");
  await a.dialog().waitFor({ state: "visible" });
  const f = await waitFor(async () => {
    const x = await activeOutline(a.page);
    return x.label && x.label !== "Open settings" && x;
  }, { message: "focus moved into the dialog" });
  assert.equal(f.outline, "none", `no ring on "${f.label}"`);

  step("Tab: now the ring shows");
  await a.page.keyboard.press("Tab");
  const g = await activeOutline(a.page);
  assert.notEqual(g.outline, "none", `the keyboard's focus on "${g.label}" is ringed`);

  step("a click turns rings off again");
  await a.page.mouse.click(5, 5);
  assert.equal(await a.page.evaluate(() => "kbd" in document.documentElement.dataset), false);
});

test("the accent is the brand teal, not the profile colour, unless the user asks", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1);
  const id = await alice.rpc("Identity");
  await alice.rpc("SetProfile", id.displayName || "Alice", "", "", "#e8743b", "");
  const a = await alice.open();
  const accent = () => a.page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue("--accent").trim());
  step("an orange profile colour leaves the accent teal");
  assert.equal(await accent(), "#14a394");

  step("Appearance → Match my profile colour: the orange is toned, never the alert hue itself");
  await a.openSettings("Appearance");
  await a.page.getByRole("radio", { name: "Match my profile colour" }).click();
  const got = await waitFor(async () => {
    const c = await accent();
    return c !== "#14a394" && c;
  }, { message: "the accent follows the profile colour once chosen" });
  assert.notEqual(got.toLowerCase(), "#e8743b", "a warm profile colour is toned before it becomes the accent");
  const sat = await a.page.evaluate((hex) => {
    const [r, g, b] = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255);
    const max = Math.max(r, g, b);
    const min = Math.min(r, g, b);
    const l = (max + min) / 2;
    return max === min ? 0 : (max - min) / (1 - Math.abs(2 * l - 1));
  }, got);
  assert.ok(sat <= 0.34, `the toned accent ${got} is quiet (saturation ${sat.toFixed(2)})`);

  step("Concord teal puts it back");
  await a.page.getByRole("radio", { name: "Concord teal" }).click();
  await waitFor(async () => (await accent()) === "#14a394", { message: "teal again" });
});

test("a grouped row's hover time hangs into the gutter, never into the text", async ({ world, assert, step }) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Clock", members: [alice] });
  const a = await alice.open();
  await a.page.evaluate(() => {
    const p = JSON.parse(localStorage.getItem("concord.prefs") || "{}");
    localStorage.setItem("concord.prefs", JSON.stringify({ ...p, clock: "12" }));
  });
  await a.reload();
  await a.openChannel(g.allChannels[0].name, "Clock");
  await a.send("first of two");
  await a.send("second, grouped under the first");
  step("hover the grouped row with the gutter narrower than its time");
  // Force the overflow whatever the clock reads: "12:49 PM" is wider than the
  // avatar column, "1:05 PM" may not be.
  await a.page.addStyleTag({ content: ".gutter-time { width: 16px !important }" });
  const row = a.messageRow("second, grouped");
  await row.locator(".msg-main").first().hover();
  const geo = await row.evaluate((el) => {
    const t = el.querySelector(".gutter-time");
    const range = document.createRange();
    range.selectNodeContents(t);
    const text = range.getBoundingClientRect();
    return { text: t.textContent, textRight: text.right, boxRight: t.getBoundingClientRect().right, bodyLeft: el.querySelector(".body").getBoundingClientRect().left };
  });
  assert.ok(/[AP]M/i.test(geo.text), `a 12-hour time (${geo.text})`);
  assert.ok(geo.textRight <= geo.boxRight + 0.5, `the overhang goes left: text ends at ${geo.textRight}, its box at ${geo.boxRight}`);
  assert.ok(geo.textRight <= geo.bodyLeft, `the time ends before the text starts (${geo.textRight} vs ${geo.bodyLeft})`);
});

test("the feed shows one message action bar: the focused row's or the hovered row's", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Bars", members: [alice] });
  const ch = g.allChannels[0];
  for (const t of ["one bar please", "a second row", "and a third"]) await alice.send(ch.id, t);
  const a = await alice.open();
  await a.openChannel(ch.name, "Bars");
  await a.waitForMessage("and a third");
  const visibleBars = () =>
    a.page.evaluate(
      () =>
        [...document.querySelectorAll('[role="toolbar"][aria-label="Message actions"]')].filter((el) => {
          const s = getComputedStyle(el);
          return s.visibility !== "hidden" && Number(s.opacity) > 0;
        }).length,
    );

  step("Escape from the empty composer focuses the newest row (nothing unread to mark)");
  await a.composer().click();
  await a.page.keyboard.press("Escape");
  await waitFor(async () => a.page.evaluate(() => !!document.activeElement?.closest("[data-msg-id]")), { message: "a row has focus" });

  step("hover another row: its bar replaces the focused row's");
  await a.messageRow("one bar please").locator(".msg-main").first().hover();
  await waitFor(async () => (await visibleBars()) === 1, { message: "exactly one action bar", timeout: 3000 });
  assert.equal(
    await a.messageRow("one bar please").evaluate((el) => getComputedStyle(el.querySelector('[role="toolbar"]')).visibility),
    "visible",
    "the hovered row has it",
  );

  step("arrow keys move focus: the bar follows the keyboard");
  await a.page.keyboard.press("ArrowUp");
  await waitFor(async () => (await visibleBars()) === 1, { message: "still exactly one", timeout: 3000 });
});

test("F6 walks the regions, and Shift+F10 opens a focused message's menu", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Regions", members: [alice] });
  const ch = g.allChannels[0];
  await alice.send(ch.id, "a row to open");
  const a = await alice.open();
  await a.openChannel(ch.name, "Regions");
  await a.waitForMessage("a row to open");
  const region = () =>
    a.page.evaluate(() => {
      const el = document.activeElement;
      if (el?.closest('nav[aria-label="Guilds and channels"]')) return "sidebar";
      if (el?.closest(".chat-head")) return "header";
      if (el?.closest('[role="log"]')) return "feed";
      if (el?.classList.contains("draft")) return "composer";
      if (el?.closest('aside[aria-label="Inspector"]')) return "inspector";
      return el?.tagName || "";
    });

  step("from the composer, F6 visits each region and comes back");
  await a.composer().click();
  const seen = [];
  for (let i = 0; i < 6; i++) {
    await a.page.keyboard.press("F6");
    seen.push(await region());
  }
  for (const r of ["sidebar", "header", "feed", "composer"]) assert.ok(seen.includes(r), `F6 reached the ${r}: ${seen.join(" → ")}`);
  const back = await region();
  await a.page.keyboard.press("Shift+F6");
  assert.notEqual(await region(), back, "Shift+F6 moves too");

  step("Shift+F10 on a focused row opens its menu");
  await a.composer().click();
  await a.page.keyboard.press("Escape");
  await waitFor(async () => (await region()) === "feed", { message: "a row has focus" });
  await a.page.keyboard.press("Shift+F10");
  await a.page.locator('[role="menu"]').waitFor({ state: "visible", timeout: 3000 });
});
