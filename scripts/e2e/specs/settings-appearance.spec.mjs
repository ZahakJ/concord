// Appearance after the theme cut: a profile saved with a removed pack opens on
// the kept pack closest to it (a light one on a light ground, never on black),
// the ground attribute every light-ground rule keys off is stamped from the
// pack table, Sweeping highlights starts off, the previews in Settings hold
// still until pointed at, and the layout nudge is shown once.
import { test } from "../harness.mjs";

const root = (a) =>
  a.page.evaluate(() => {
    const d = document.documentElement.dataset;
    return { theme: d.theme, pack: d.themePack || "", ground: d.ground, shine: d.themeShine || "" };
  });
const seed = (a, prefs) =>
  a.page.evaluate((prefs) => {
    localStorage.setItem("concord.prefs", JSON.stringify(prefs));
    localStorage.removeItem("concord.bootPaint");
  }, prefs);
const saved = (a) => a.page.evaluate(() => JSON.parse(localStorage.getItem("concord.prefs") || "{}"));

test("a removed theme pack opens as the kept one closest to it", async ({ world, assert, step, waitFor, shot }) => {
  const [alice] = await world.peers(1);
  await world.guild({ name: "Studio", members: [alice] });
  const a = await alice.open({ viewport: "desktop" });

  step("v0.60.2's Porcelain under the Dark theme → the default palette, Light");
  await seed(a, { theme: "dark", themePack: "porcelain", themeShine: true });
  await a.reload();
  await waitFor(async () => (await root(a)).theme === "light", { message: "the light theme" });
  let r = await root(a);
  assert.deepEqual(r, { theme: "light", pack: "", ground: "light", shine: "off" });
  let p = await saved(a);
  assert.equal(p.themePack, "");
  assert.equal(p.theme, "light");
  assert.equal(p.prefsV, 2, "the migration is saved");
  assert.equal(p.themeShine, false, "the stored old default for Sweeping highlights is off now");
  await shot(a, "porcelain to light");

  step("Synthwave under Light → Dracula: its own dark ground, its own fields");
  await seed(a, { theme: "light", themePack: "synthwave", seasonal: true, navModel: "rail", prefsV: 1 });
  await a.reload();
  await waitFor(async () => (await root(a)).pack === "dracula", { message: "the dracula pack stamped" });
  r = await root(a);
  assert.equal(r.ground, "dark", "a dark pack under Light is a dark ground");
  // The white-on-white bug: the light palette's white --bg-input reaching a
  // dark pack under the Light theme.
  const input = await a.page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue("--bg-input").trim());
  assert.notEqual(input.toLowerCase(), "#ffffff", "the field fill is the pack's, not the light theme's white");
  p = await saved(a);
  assert.equal(p.themeFx, "seasonal", "on Rail, Seasonal touches became the Seasonal effect");
  assert.equal("seasonal" in p, false);

  step("Seasonal touches on the Sidebar (where it never drew) and the Compact layout");
  await seed(a, { theme: "dark", seasonal: true, layout: "compact", prefsV: 1 });
  await a.reload();
  await waitFor(async () => (await saved(a)).prefsV === 2, { message: "migrated" });
  p = await saved(a);
  assert.ok(!p.themeFx, "no effect switched on for someone who never saw one");
  assert.equal(p.layout, "sidebar", "the Compact layout became the sidebar");

  step("Paper under Dark: a light pack is a light ground whatever the theme");
  await seed(a, { theme: "dark", themePack: "paper", prefsV: 2 });
  await a.reload();
  await waitFor(async () => (await root(a)).pack === "paper", { message: "paper stamped" });
  assert.equal((await root(a)).ground, "light");

  step("Atrium → Paper");
  await seed(a, { theme: "dark", themePack: "atrium", prefsV: 2 });
  await a.reload();
  await waitFor(async () => (await root(a)).pack === "paper", { message: "atrium arrives as paper" });

  step("a pack this build does not know (a newer build's) draws as the default and stays saved");
  await seed(a, { theme: "light", themePack: "vaporwave2026", prefsV: 2 });
  await a.reload();
  await waitFor(async () => (await root(a)).theme === "light", { message: "the light theme" });
  assert.deepEqual(await root(a), { theme: "light", pack: "", ground: "light", shine: "off" });
  assert.equal((await saved(a)).themePack, "vaporwave2026", "the newer build's choice survives this one");
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});

test("Appearance: one theme picker, previews still until hovered", async ({ world, assert, step, waitFor, shot }) => {
  const [alice] = await world.peers(1);
  await world.guild({ name: "Studio", members: [alice] });
  const a = await alice.open({ viewport: "desktop" });
  const dlg = await a.openSettings("Appearance");

  step("nine packs, no galleries, no filter");
  const packs = dlg.getByRole("radiogroup", { name: "Theme pack" }).getByRole("radio");
  assert.equal(await packs.count(), 9);
  assert.equal(await dlg.locator('input[type="search"]').count(), 0, "no filter box");

  step("picking Aurora: the card's sweep holds still until the pointer is on it");
  const aurora = dlg.locator(".pack-card", { hasText: "Aurora" }).first();
  await aurora.click();
  await waitFor(async () => (await root(a)).pack === "aurora", { message: "aurora stamped" });
  await a.page.mouse.move(5, 5);
  const state = () => aurora.locator(".pk-glow").evaluate((e) => getComputedStyle(e).animationPlayState);
  await waitFor(async () => (await state()) === "paused", { message: "the preview paused at rest" });
  await aurora.hover();
  await waitFor(async () => (await state()) === "running", { message: "the preview runs under the pointer" });
  await shot(a, "aurora picked");

  step("Light, Dark and Match system hand the ground back from a pack");
  await dlg.getByRole("radiogroup", { name: "Theme", exact: true }).getByRole("radio", { name: "Light", exact: true }).click();
  await waitFor(async () => (await root(a)).pack === "", { message: "the pack cleared" });
  assert.equal((await root(a)).ground, "light");

  step("Seasonal is one of the effects");
  await dlg.getByRole("radiogroup", { name: "Visual effect" }).getByRole("radio", { name: /Seasonal/ }).click();
  assert.equal((await saved(a)).themeFx, "seasonal");
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});

test("the layout nudge is shown once", async ({ world, assert, step, waitFor }) => {
  const [alice] = await world.peers(1);
  await world.guild({ name: "Studio", members: [alice] });
  const a = await alice.open({ viewport: "desktop" });
  const nudge = a.page.getByText("Prefer a different layout?");
  step("first time: there, and marked seen without being answered");
  await nudge.waitFor();
  await waitFor(async () => (await saved(a)).layoutHintSeen === true, { message: "seen on show" });
  step("next launch: gone");
  await a.reload();
  await a.page.locator(".channel").first().waitFor();
  assert.equal(await nudge.count(), 0, "not shown again");
});
